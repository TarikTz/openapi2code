// pkg/engine/golden_pydantic_typecheck_test.go
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

// TestGoldenPydanticOutput_Compiles hands every committed
// *.pydantic.py.txt golden to a real python3 interpreter that actually
// imports the generated module (not just py_compile, which only checks
// syntax) — pydantic's BaseModel machinery can reject annotation shapes
// that are syntactically valid Python, so an import-level check is the
// meaningful one here. Skips (never fails) when python3 is missing, the
// pydantic package isn't importable in that environment, or -short is
// set.
func TestGoldenPydanticOutput_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping pydantic import check of Pydantic goldens in -short mode")
	}
	python3, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not found on PATH; skipping import check of Pydantic goldens")
	}
	if err := exec.Command(python3, "-c", "import pydantic").Run(); err != nil {
		t.Skip("pydantic package not importable from python3; skipping import check of Pydantic goldens (pip install pydantic to get real verification)")
	}

	goldens, err := filepath.Glob(filepath.Join("testdata", "golden", "*.pydantic.py.txt"))
	if err != nil {
		t.Fatalf("glob goldens: %v", err)
	}
	if len(goldens) == 0 {
		t.Fatal("no *.pydantic.py.txt golden files found to import-check")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, golden := range goldens {
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
			cmd := exec.CommandContext(ctx, python3, "generated.py")
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Errorf("generated Pydantic output does not import cleanly (python3 generated.py):\n%s", strings.TrimSpace(string(out)))
			}
		})
	}
}
