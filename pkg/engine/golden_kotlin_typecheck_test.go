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

// TestGoldenKotlinOutput_Compiles hands every committed *.kt.txt golden
// to the real Kotlin compiler, mirroring golden_swift_typecheck_test.go.
// Skips (never fails) when kotlinc is missing or -short is set — this
// environment does not have kotlinc installed, so this test skips here
// today but runs for real wherever it is.
func TestGoldenKotlinOutput_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping kotlinc compile check of Kotlin goldens in -short mode")
	}
	kotlinc, err := exec.LookPath("kotlinc")
	if err != nil {
		t.Skip("kotlinc not found on PATH; skipping compile check of Kotlin goldens (install Kotlin to get real compile verification)")
	}

	goldens, err := filepath.Glob(filepath.Join("testdata", "golden", "*.kt.txt"))
	if err != nil {
		t.Fatalf("glob goldens: %v", err)
	}
	if len(goldens) == 0 {
		t.Fatal("no *.kt.txt golden files found to compile")
	}

	dir := t.TempDir()
	var ktFiles []string
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
		ktFiles = append(ktFiles, dest)
	}

	// Each golden is compiled to its own output jar in a subdirectory
	// (named after the golden's base name) so that duplicate top-level
	// declarations across different goldens — every fixture has its own
	// Pet, Category, etc. — never collide in one shared classpath.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	for _, ktFile := range ktFiles {
		outJar := ktFile + ".jar"
		cmd := exec.CommandContext(ctx, kotlinc, ktFile, "-d", outJar)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Errorf("generated Kotlin output does not compile (%s):\n%s", filepath.Base(ktFile), strings.TrimSpace(string(out)))
		}
	}
}
