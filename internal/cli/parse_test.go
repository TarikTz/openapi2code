package cli

import (
	"errors"
	"strings"
	"testing"
)

func TestParseArgs_ValidWithOutput(t *testing.T) {
	opts, err := parseArgs([]string{"pull", "spec.json", "--output", "./out"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if opts.Source != "spec.json" || opts.Target != "ts" || opts.OutDir != "./out" || opts.OutFile != "" {
		t.Errorf("unexpected opts: %+v", opts)
	}
}

func TestParseArgs_ValidWithOutFile(t *testing.T) {
	opts, err := parseArgs([]string{"pull", "spec.json", "--out-file", "./out/index.ts"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if opts.OutFile != "./out/index.ts" || opts.OutDir != "" {
		t.Errorf("unexpected opts: %+v", opts)
	}
}

func TestParseArgs_MissingPullSubcommand(t *testing.T) {
	if _, err := parseArgs([]string{"spec.json", "--output", "./out"}); err == nil {
		t.Fatal("expected error for missing 'pull' subcommand")
	}
}

func TestParseArgs_MissingSource(t *testing.T) {
	if _, err := parseArgs([]string{"pull", "--output", "./out"}); err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestParseArgs_BothOutputAndOutFile(t *testing.T) {
	if _, err := parseArgs([]string{"pull", "spec.json", "--output", "./out", "--out-file", "./out.ts"}); err == nil {
		t.Fatal("expected error when both --output and --out-file are given")
	}
}

func TestParseArgs_NeitherOutputNorOutFile(t *testing.T) {
	if _, err := parseArgs([]string{"pull", "spec.json"}); err == nil {
		t.Fatal("expected error when neither --output nor --out-file is given")
	}
}

func TestParseArgs_TargetZod(t *testing.T) {
	opts, err := parseArgs([]string{"pull", "spec.json", "--target", "zod", "--output", "./out"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if opts.Target != "zod" {
		t.Errorf("expected Target zod, got %q", opts.Target)
	}
}

func TestParseArgs_UnsupportedTarget(t *testing.T) {
	_, err := parseArgs([]string{"pull", "spec.json", "--target", "python", "--output", "./out"})
	if err == nil {
		t.Fatal("expected error for unsupported target")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("expected \"not yet implemented\" in error, got: %v", err)
	}
}

func TestParseArgs_MultiTargetValid(t *testing.T) {
	opts, err := parseArgs([]string{"pull", "spec.json", "--target", "ts,zod,swift", "--output", "./out"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if opts.Target != "ts,zod,swift" {
		t.Errorf("expected Target to keep the raw comma-separated value, got %q", opts.Target)
	}
}

// TestParseArgs_MultiTargetSpacesAroundCommasTrimmed guards splitTargets'
// whitespace handling, since a shell user might naturally write
// "ts, zod" with a space after the comma.
func TestParseArgs_MultiTargetSpacesAroundCommasTrimmed(t *testing.T) {
	if _, err := parseArgs([]string{"pull", "spec.json", "--target", "ts, zod", "--output", "./out"}); err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
}

func TestParseArgs_MultiTargetWithOutFileRejected(t *testing.T) {
	_, err := parseArgs([]string{"pull", "spec.json", "--target", "ts,zod", "--out-file", "./out.ts"})
	if err == nil {
		t.Fatal("expected error for --out-file with multiple targets")
	}
	if !strings.Contains(err.Error(), "cannot be used with multiple --target values") {
		t.Errorf("expected a clear multi-target/--out-file error, got: %v", err)
	}
}

func TestParseArgs_MultiTargetDuplicateRejected(t *testing.T) {
	_, err := parseArgs([]string{"pull", "spec.json", "--target", "ts,ts", "--output", "./out"})
	if err == nil {
		t.Fatal("expected error for a duplicate target")
	}
	if !strings.Contains(err.Error(), "more than once") {
		t.Errorf("expected a duplicate-target error, got: %v", err)
	}
}

func TestParseArgs_MultiTargetUnsupportedMemberRejected(t *testing.T) {
	_, err := parseArgs([]string{"pull", "spec.json", "--target", "ts,python", "--output", "./out"})
	if err == nil {
		t.Fatal("expected error for an unsupported target within a multi-target list")
	}
	if !strings.Contains(err.Error(), `"python"`) {
		t.Errorf("expected the error to name the specific unsupported member, got: %v", err)
	}
}

func TestParseArgs_HelpNoSource(t *testing.T) {
	for _, flag := range []string{"-h", "-help", "--help"} {
		_, err := parseArgs([]string{"pull", flag})
		if err == nil {
			t.Fatalf("%s: expected a help \"error\"", flag)
		}
		if !errors.Is(err, errHelp) {
			t.Errorf("%s: expected errors.Is(err, errHelp), got: %v", flag, err)
		}
		if err.Error() == "" {
			t.Errorf("%s: expected non-empty help text", flag)
		}
	}
}

func TestParseArgs_HelpAfterSource(t *testing.T) {
	_, err := parseArgs([]string{"pull", "spec.json", "--help"})
	if err == nil {
		t.Fatal("expected a help \"error\"")
	}
	if !errors.Is(err, errHelp) {
		t.Errorf("expected errors.Is(err, errHelp), got: %v", err)
	}
	if err.Error() == "" {
		t.Error("expected non-empty help text")
	}
}
