package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPullCommand(t *testing.T) {
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "openapi2code")

	build := exec.Command("go", "build", "-o", binPath, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	specPath := filepath.Join(tmpDir, "spec.json")
	spec := `{
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
	if err := os.WriteFile(specPath, []byte(spec), 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outFile := filepath.Join(tmpDir, "out.ts")
	run := exec.Command(binPath, "pull", specPath, "--target", "ts", "--out-file", outFile)
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}

	content, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(content), "export interface Pet {") {
		t.Errorf("expected Pet interface, got:\n%s", content)
	}
}
