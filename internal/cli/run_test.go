package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixtureSpec = `{
  "openapi": "3.0.3",
  "components": {
    "schemas": {
      "Pet": {
        "type": "object",
        "properties": {"name": {"type": "string"}},
        "required": ["name"]
      }
    }
  }
}`

func TestRun_EndToEndOutFile(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(specPath, []byte(fixtureSpec), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	outPath := filepath.Join(dir, "out.ts")

	var stderr bytes.Buffer
	code := Run([]string{"pull", specPath, "--out-file", outPath}, strings.NewReader(""), &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d, stderr: %s", code, stderr.String())
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(content), "export interface Pet {") {
		t.Errorf("expected Pet interface, got:\n%s", content)
	}
}

func TestRun_ErrorExitsNonZeroWithMessage(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "out.ts")
	var stderr bytes.Buffer
	code := Run([]string{"pull", "/nonexistent/spec.json", "--out-file", outPath}, strings.NewReader(""), &stderr)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if stderr.Len() == 0 {
		t.Error("expected an error message on stderr")
	}
}

func TestRun_EndToEndOutput(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(specPath, []byte(fixtureSpec), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	outDir := filepath.Join(dir, "out")

	var stderr bytes.Buffer
	code := Run([]string{"pull", specPath, "--output", outDir}, strings.NewReader(""), &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d, stderr: %s", code, stderr.String())
	}

	if _, err := os.Stat(filepath.Join(outDir, "index.ts")); err != nil {
		t.Errorf("expected barrel index.ts to exist: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(outDir, "Pet.ts"))
	if err != nil {
		t.Fatalf("expected Pet.ts to exist: %v", err)
	}
	if !strings.Contains(string(content), "export interface Pet {") {
		t.Errorf("expected Pet interface, got:\n%s", content)
	}
}

// TestRun_EndToEndMultiTarget covers the PRD's own headline CLI example
// (`--target ts,zod`), which didn't work at all before comma-separated
// multi-target support existed. Each target writes into its own
// subdirectory of --output (ts/, zod/) rather than a flat directory,
// since GenerateTS and GenerateZod both key their monolithic/barrel
// output "index.ts" — a flat directory would let one target's output
// silently overwrite the other's.
func TestRun_EndToEndMultiTarget(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(specPath, []byte(fixtureSpec), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	outDir := filepath.Join(dir, "out")

	var stderr bytes.Buffer
	code := Run([]string{"pull", specPath, "--target", "ts,zod", "--output", outDir}, strings.NewReader(""), &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d, stderr: %s", code, stderr.String())
	}

	tsContent, err := os.ReadFile(filepath.Join(outDir, "ts", "Pet.ts"))
	if err != nil {
		t.Fatalf("expected out/ts/Pet.ts to exist: %v", err)
	}
	if !strings.Contains(string(tsContent), "export interface Pet {") {
		t.Errorf("expected a TS interface in out/ts/Pet.ts, got:\n%s", tsContent)
	}

	zodContent, err := os.ReadFile(filepath.Join(outDir, "zod", "Pet.ts"))
	if err != nil {
		t.Fatalf("expected out/zod/Pet.ts to exist: %v", err)
	}
	if !strings.Contains(string(zodContent), "PetSchema = z.object({") {
		t.Errorf("expected a Zod schema in out/zod/Pet.ts, got:\n%s", zodContent)
	}
}

// TestRun_UnrecognizedFlagProducesExactlyOneLineOnInjectedWriter pins the
// Global Constraint that every failure prints ONE line to stderr, through
// the writer Run was given — not to the real process os.Stderr. Before the
// fix, flag.FlagSet's default Output() was the real os.Stderr, so an
// unrecognized flag caused the flag package to write its own multi-line
// "flag provided but not defined" + usage dump directly to the real
// process stderr (invisible to and separate from the injected buffer this
// test inspects), which a test built around a bytes.Buffer can't detect
// without redirecting the real file descriptor, as done here.
func TestRun_UnrecognizedFlagProducesExactlyOneLineOnInjectedWriter(t *testing.T) {
	realStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = realStderr }()

	var stderr bytes.Buffer
	code := Run([]string{"pull", "spec.json", "--bogus-flag", "--out-file", "x.ts"}, strings.NewReader(""), &stderr)

	os.Stderr = realStderr
	w.Close()
	var leaked bytes.Buffer
	if _, err := io.Copy(&leaked, r); err != nil {
		t.Fatalf("read leaked pipe: %v", err)
	}

	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}

	lines := strings.Split(strings.TrimRight(stderr.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected exactly one line on the injected stderr writer, got %d lines:\n%s", len(lines), stderr.String())
	}

	if leaked.Len() != 0 {
		t.Errorf("expected nothing written to the real process stderr, got:\n%s", leaked.String())
	}
}

func TestRun_PullHelpNoSourceExitsZeroWithOutput(t *testing.T) {
	var stderr bytes.Buffer
	code := Run([]string{"pull", "--help"}, strings.NewReader(""), &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0 for --help, got %d, stderr: %s", code, stderr.String())
	}
	if stderr.Len() == 0 {
		t.Error("expected non-empty help/usage output")
	}
}

func TestRun_PullHelpAfterSourceExitsZeroWithOutput(t *testing.T) {
	var stderr bytes.Buffer
	code := Run([]string{"pull", "spec.json", "--help"}, strings.NewReader(""), &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0 for --help, got %d, stderr: %s", code, stderr.String())
	}
	if stderr.Len() == 0 {
		t.Error("expected non-empty help/usage output")
	}
}

func TestRun_EndToEndSwift(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(specPath, []byte(fixtureSpec), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	outPath := filepath.Join(dir, "out.swift")

	var stderr bytes.Buffer
	code := Run([]string{"pull", specPath, "--target", "swift", "--out-file", outPath}, strings.NewReader(""), &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d, stderr: %s", code, stderr.String())
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(content), "public struct Pet: Codable {") {
		t.Errorf("expected Pet struct, got:\n%s", content)
	}
}

func TestRun_EndToEndKotlin(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(specPath, []byte(fixtureSpec), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	outPath := filepath.Join(dir, "out.kt")

	var stderr bytes.Buffer
	code := Run([]string{"pull", specPath, "--target", "kotlin", "--out-file", outPath}, strings.NewReader(""), &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d, stderr: %s", code, stderr.String())
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(content), "data class Pet(") {
		t.Errorf("expected Pet data class, got:\n%s", content)
	}
}

func TestRun_EndToEndDart(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(specPath, []byte(fixtureSpec), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	outPath := filepath.Join(dir, "out.dart")

	var stderr bytes.Buffer
	code := Run([]string{"pull", specPath, "--target", "dart", "--out-file", outPath}, strings.NewReader(""), &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d, stderr: %s", code, stderr.String())
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(content), "class Pet {") {
		t.Errorf("expected Pet class, got:\n%s", content)
	}
}

func TestRun_EndToEndZod(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(specPath, []byte(fixtureSpec), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	outPath := filepath.Join(dir, "out.ts")

	var stderr bytes.Buffer
	code := Run([]string{"pull", specPath, "--target", "zod", "--out-file", outPath}, strings.NewReader(""), &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d, stderr: %s", code, stderr.String())
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(content), "export const PetSchema = z.object({") {
		t.Errorf("expected PetSchema, got:\n%s", content)
	}
}
