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

// TestGoldenDartOutput_Analyzes hands every committed *.dart.txt golden
// to `dart analyze`, mirroring the Swift/Kotlin typecheck tests. Skips
// (never fails) when the dart CLI is missing or -short is set — this
// environment does not have it installed, so this test skips here today
// but runs for real wherever it is.
func TestGoldenDartOutput_Analyzes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping dart analyze check of Dart goldens in -short mode")
	}
	dartBin, err := exec.LookPath("dart")
	if err != nil {
		t.Skip("dart not found on PATH; skipping analyze check of Dart goldens (install Dart to get real compile verification)")
	}

	goldens, err := filepath.Glob(filepath.Join("testdata", "golden", "*.dart.txt"))
	if err != nil {
		t.Fatalf("glob goldens: %v", err)
	}
	if len(goldens) == 0 {
		t.Fatal("no *.dart.txt golden files found to analyze")
	}

	// dart analyze operates on a package directory (it looks for a
	// pubspec.yaml), so each golden gets its own minimal package
	// directory rather than sharing one — this also sidesteps duplicate
	// top-level declarations across different fixtures' goldens.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	for _, golden := range goldens {
		base := strings.TrimSuffix(filepath.Base(golden), ".txt")
		pkgDir := filepath.Join(t.TempDir(), strings.TrimSuffix(base, ".dart"))
		libDir := filepath.Join(pkgDir, "lib")
		if err := os.MkdirAll(libDir, 0o755); err != nil {
			t.Fatalf("create lib dir: %v", err)
		}
		content, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("read golden %s: %v", golden, err)
		}
		if err := os.WriteFile(filepath.Join(libDir, base), content, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		pubspec := "name: openapi2code_golden_check\nenvironment:\n  sdk: '>=2.17.0 <4.0.0'\n"
		if err := os.WriteFile(filepath.Join(pkgDir, "pubspec.yaml"), []byte(pubspec), 0o644); err != nil {
			t.Fatalf("write pubspec.yaml: %v", err)
		}

		cmd := exec.CommandContext(ctx, dartBin, "analyze", "--fatal-infos", ".")
		cmd.Dir = pkgDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Errorf("generated Dart output does not analyze cleanly (%s):\n%s", base, strings.TrimSpace(string(out)))
		}
	}
}
