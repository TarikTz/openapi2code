package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tarikomercehajic/openapi2code/pkg/engine"
)

func TestWriteOutput_OutFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "index.ts")
	out := engine.Output{Files: map[string]string{"index.ts": "export type X = string;\n"}}

	if err := writeOutput(out, "", path); err != nil {
		t.Fatalf("writeOutput: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(content) != "export type X = string;\n" {
		t.Errorf("got %s", content)
	}
}

func TestWriteOutput_OutDir(t *testing.T) {
	dir := t.TempDir()
	out := engine.Output{Files: map[string]string{
		"Pet.ts":   "export interface Pet {}\n",
		"index.ts": "export * from \"./Pet\";\n",
	}}

	if err := writeOutput(out, dir, ""); err != nil {
		t.Fatalf("writeOutput: %v", err)
	}
	for name, want := range out.Files {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("%s: got %s, want %s", name, got, want)
		}
	}
}

func TestWriteOutput_PreservesUnrelatedFiles(t *testing.T) {
	dir := t.TempDir()
	unrelated := filepath.Join(dir, "README.md")
	if err := os.WriteFile(unrelated, []byte("keep me"), 0644); err != nil {
		t.Fatalf("write unrelated: %v", err)
	}

	out := engine.Output{Files: map[string]string{"Pet.ts": "export interface Pet {}\n"}}
	if err := writeOutput(out, dir, ""); err != nil {
		t.Fatalf("writeOutput: %v", err)
	}

	content, err := os.ReadFile(unrelated)
	if err != nil {
		t.Fatalf("read unrelated: %v", err)
	}
	if string(content) != "keep me" {
		t.Errorf("unrelated file was modified: %s", content)
	}
}

func TestWriteOutput_OutFileAcceptsAnySingleFileKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.swift")
	out := engine.Output{Files: map[string]string{"Generated.swift": "public struct Pet: Codable {}\n"}}

	if err := writeOutput(out, "", path); err != nil {
		t.Fatalf("writeOutput: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(content) != "public struct Pet: Codable {}\n" {
		t.Errorf("got %s", content)
	}
}

func TestWriteOutput_OutFileAmbiguousFileCountErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.ts")
	out := engine.Output{Files: map[string]string{
		"Pet.ts":      "export interface Pet {}\n",
		"Category.ts": "export interface Category {}\n",
	}}

	err := writeOutput(out, "", path)
	if err == nil {
		t.Fatal("expected an error when out.Files has more than one entry for --out-file")
	}

	if _, statErr := os.Stat(path); statErr == nil {
		t.Error("expected no file to be written when the file count is ambiguous")
	}
}

func TestWriteOutput_OverwritesStaleFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Pet.ts")
	if err := os.WriteFile(path, []byte("stale"), 0644); err != nil {
		t.Fatalf("write stale: %v", err)
	}

	out := engine.Output{Files: map[string]string{"Pet.ts": "export interface Pet {}\n"}}
	if err := writeOutput(out, dir, ""); err != nil {
		t.Fatalf("writeOutput: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(content) != "export interface Pet {}\n" {
		t.Errorf("stale content not overwritten: %s", content)
	}
}
