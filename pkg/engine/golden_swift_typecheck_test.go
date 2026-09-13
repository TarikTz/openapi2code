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

// TestGoldenSwiftOutput_Compiles hands every committed *.swift.txt golden
// to the real Swift compiler. Every other test in this package compares
// generated Swift as Go strings, which cannot tell whether the output is
// valid Swift at all — this is the Swift equivalent of
// golden_zod_typecheck_test.go's tsc check.
//
// It skips (never fails) when swiftc is missing or -short is set — those
// are environment problems, not code defects.
func TestGoldenSwiftOutput_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping swiftc compile check of Swift goldens in -short mode")
	}
	swiftc, err := exec.LookPath("swiftc")
	if err != nil {
		t.Skip("swiftc not found on PATH; skipping compile check of Swift goldens (install Swift to get real compile verification)")
	}

	goldens, err := filepath.Glob(filepath.Join("testdata", "golden", "*.swift.txt"))
	if err != nil {
		t.Fatalf("glob goldens: %v", err)
	}
	if len(goldens) == 0 {
		t.Fatal("no *.swift.txt golden files found to compile")
	}

	dir := t.TempDir()
	var swiftFiles []string
	for _, golden := range goldens {
		content, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("read golden %s: %v", golden, err)
		}
		base := strings.TrimSuffix(filepath.Base(golden), ".txt")
		dest := filepath.Join(dir, base)
		if err := os.WriteFile(dest, content, 0o644); err != nil {
			t.Fatalf("write %s: %v", dest, err)
		}
		swiftFiles = append(swiftFiles, dest)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	args := append([]string{"-typecheck"}, swiftFiles...)
	cmd := exec.CommandContext(ctx, swiftc, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("generated Swift output does not compile (swiftc -typecheck over %d golden files):\n%s",
			len(goldens), strings.TrimSpace(string(out)))
	}
}
