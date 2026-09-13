package engine_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// Every other test in this package compares generated TypeScript as Go
// strings, which cannot tell whether the output is valid TypeScript at all.
// This one hands the committed Zod goldens to the real compiler.
//
// It needs Node tooling and a one-time `npm install`, so it skips (never
// fails) when npm is missing, when -short is set, or when the install
// itself fails — those are environment problems, not code defects. When the
// tooling IS present it runs automatically and gives a real signal.
//
// The zod/typescript install is cached under the user cache dir and shared
// across every golden file (and across runs), so the cost is paid once.

const (
	typecheckCacheName = "openapi2code-zod-typecheck"
	npmInstallTimeout  = 5 * time.Minute
	tscTimeout         = 3 * time.Minute
)

var (
	typecheckOnce sync.Once
	typecheckDir  string
	typecheckErr  error
)

// typecheckEnv returns a directory with zod and typescript installed under
// node_modules, preparing it at most once per test binary.
func typecheckEnv() (string, error) {
	typecheckOnce.Do(func() { typecheckDir, typecheckErr = prepareTypecheckEnv() })
	return typecheckDir, typecheckErr
}

func prepareTypecheckEnv() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, typecheckCacheName)
	if typecheckEnvReady(dir) {
		return dir, nil
	}

	// Install into a staging directory and rename it into place, so a
	// half-finished or concurrent install can never be mistaken for a
	// usable cache.
	staging, err := os.MkdirTemp(base, typecheckCacheName+"-staging-")
	if err != nil {
		return "", fmt.Errorf("create staging dir: %w", err)
	}
	defer os.RemoveAll(staging)

	pkgJSON := `{"name":"openapi2code-zod-typecheck","private":true,"version":"0.0.0"}`
	if err := os.WriteFile(filepath.Join(staging, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		return "", fmt.Errorf("write package.json: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), npmInstallTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "npm", "install", "--no-audit", "--no-fund", "--loglevel=error", "zod@^3", "typescript@^5")
	cmd.Dir = staging
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("npm install: %w\n%s", err, out)
	}

	if err := os.RemoveAll(dir); err != nil {
		return "", fmt.Errorf("clear cache dir: %w", err)
	}
	if err := os.Rename(staging, dir); err != nil {
		// Another process may have won the race and populated it already.
		if typecheckEnvReady(dir) {
			return dir, nil
		}
		return "", fmt.Errorf("move install into place: %w", err)
	}
	return dir, nil
}

func typecheckEnvReady(dir string) bool {
	for _, pkg := range []string{"zod", "typescript"} {
		if _, err := os.Stat(filepath.Join(dir, "node_modules", pkg, "package.json")); err != nil {
			return false
		}
	}
	return true
}

// TestGoldenZodOutput_Typechecks compiles every committed *.zod.ts golden
// with `tsc --noEmit --strict`. All goldens go through a single tsc
// invocation — each is its own module, so they do not interfere, and one
// process start beats one per fixture.
func TestGoldenZodOutput_Typechecks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping tsc typecheck of Zod goldens in -short mode")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not found on PATH; skipping tsc typecheck of Zod goldens (install Node to get real compile verification)")
	}

	env, err := typecheckEnv()
	if err != nil {
		t.Skipf("could not prepare a Node environment for typechecking (environment issue, not a generator failure): %v", err)
	}

	goldens, err := filepath.Glob(filepath.Join("testdata", "golden", "*.zod.ts"))
	if err != nil {
		t.Fatalf("glob goldens: %v", err)
	}
	if len(goldens) == 0 {
		t.Fatal("no *.zod.ts golden files found to typecheck")
	}

	// Copy the goldens next to the installed node_modules so `import { z }
	// from "zod"` resolves without any tsconfig path mapping.
	srcDir := filepath.Join(env, "src")
	if err := os.RemoveAll(srcDir); err != nil {
		t.Fatalf("clear src dir: %v", err)
	}
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("create src dir: %v", err)
	}

	args := []string{"--noEmit", "--strict", "--target", "es2020", "--module", "esnext", "--moduleResolution", "node", "--skipLibCheck"}
	for _, golden := range goldens {
		content, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("read golden %s: %v", golden, err)
		}
		dest := filepath.Join(srcDir, filepath.Base(golden))
		if err := os.WriteFile(dest, content, 0o644); err != nil {
			t.Fatalf("copy golden %s: %v", golden, err)
		}
		args = append(args, filepath.Join("src", filepath.Base(golden)))
	}

	tsc := filepath.Join(env, "node_modules", ".bin", "tsc")
	if runtime.GOOS == "windows" {
		tsc += ".cmd"
	}
	ctx, cancel := context.WithTimeout(context.Background(), tscTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, tsc, args...)
	cmd.Dir = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("generated Zod output does not compile (tsc --noEmit --strict over %d golden files):\n%s",
			len(goldens), strings.TrimSpace(string(out)))
	}
}
