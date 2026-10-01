// pkg/engine/golden_python_typecheck_test.go
package engine_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestGoldenPythonOutput_Compiles hands every committed *.py.txt golden
// (the dataclass target) to python3 -m py_compile — a real syntax check
// that comparing generated Python as Go strings cannot provide. Skips
// (never fails) when python3 is missing or -short is set.
func TestGoldenPythonOutput_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping python3 compile check of Python goldens in -short mode")
	}
	python3, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not found on PATH; skipping compile check of Python goldens (install Python 3.10+ to get real compile verification)")
	}

	goldens, err := filepath.Glob(filepath.Join("testdata", "golden", "*.py.txt"))
	if err != nil {
		t.Fatalf("glob goldens: %v", err)
	}
	// Exclude the Pydantic goldens (*.pydantic.py.txt) — they need the
	// pydantic package importable, checked separately in
	// golden_pydantic_typecheck_test.go.
	var dataclassGoldens []string
	for _, g := range goldens {
		if !strings.HasSuffix(g, ".pydantic.py.txt") {
			dataclassGoldens = append(dataclassGoldens, g)
		}
	}
	if len(dataclassGoldens) == 0 {
		t.Fatal("no *.py.txt golden files found to compile")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, golden := range dataclassGoldens {
		golden := golden
		t.Run(filepath.Base(golden), func(t *testing.T) {
			content, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden %s: %v", golden, err)
			}
			dir := t.TempDir()
			dest := filepath.Join(dir, "generated.py")
			if err := os.WriteFile(dest, content, 0o644); err != nil {
				t.Fatalf("write %s: %v", dest, err)
			}
			cmd := exec.CommandContext(ctx, python3, "-m", "py_compile", dest)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Errorf("generated Python output does not compile (python3 -m py_compile):\n%s", strings.TrimSpace(string(out)))
			}
		})
	}
}
