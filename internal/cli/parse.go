package cli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

// Options is the fully validated, parsed form of the CLI's arguments.
type Options struct {
	Source  string
	Target  string
	OutDir  string
	OutFile string
}

// errHelp is the sentinel that parseArgs' returned error wraps (via
// helpError.Unwrap) when the "failure" is actually an explicit help request
// ("pull --help" or "pull <source> --help ..."), not a real error. Run
// distinguishes the two with errors.Is(err, errHelp): a help request prints
// its text and exits 0, a real error prints its message and exits 1.
var errHelp = errors.New("help requested")

// helpError carries rendered usage/help text as an error so it can flow
// through parseArgs' existing (Options, error) signature without changing
// it or Run's signature.
type helpError struct {
	text string
}

func (e *helpError) Error() string { return e.text }
func (e *helpError) Unwrap() error { return errHelp }

const usageLine = "usage: openapi2code pull <source> [--target <ts|zod|swift|kotlin|dart>[,<target>...]] (--output <dir> | --out-file <path>)"

// splitTargets splits a --target value on commas into its individual
// target names, trimming surrounding whitespace from each — the single
// source of truth both parseArgs (for validation) and Run (for
// dispatch) use, so the two can never disagree about what one --target
// value means. A single target with no comma returns a one-element
// slice, so callers never need a separate "was this multi-target" check
// beyond len(targets) > 1.
func splitTargets(raw string) []string {
	parts := strings.Split(raw, ",")
	targets := make([]string, 0, len(parts))
	for _, p := range parts {
		targets = append(targets, strings.TrimSpace(p))
	}
	return targets
}

// parseArgs parses and validates the "pull" subcommand's arguments. It
// does not touch the filesystem or network — see resolveSource/writeOutput.
func parseArgs(args []string) (Options, error) {
	if len(args) == 0 || args[0] != "pull" {
		return Options{}, errors.New(usageLine)
	}

	if len(args) < 2 {
		return Options{}, fmt.Errorf("expected exactly one <source> argument, got 0")
	}

	fs := flag.NewFlagSet("pull", flag.ContinueOnError)
	// The flag package writes parse errors and usage text straight to
	// fs.Output(), which defaults to the real os.Stderr — bypassing
	// whatever writer the caller gave Run. Discard it here so the ONLY
	// way anything leaves this function is through the returned error,
	// which Run then writes to its own injected writer exactly once.
	fs.SetOutput(io.Discard)
	target := fs.String("target", "ts", `generation target(s): "ts", "zod", "swift", "kotlin", or "dart" — comma-separated for more than one, e.g. "ts,zod"`)
	outDir := fs.String("output", "", "write modular output (one file per model + barrel index.ts) into this directory")
	outFile := fs.String("out-file", "", "write monolithic output to this single file")

	// "pull --help" (or -h/-help): no source was given, this is a help
	// request, not a missing <source> argument.
	if args[1] == "-h" || args[1] == "-help" || args[1] == "--help" {
		return Options{}, &helpError{text: helpText(fs)}
	}

	source := args[1]

	if err := fs.Parse(args[2:]); err != nil {
		// "pull spec.json --help": args[1] was a real source, but --help
		// appeared later among the flags.
		if errors.Is(err, flag.ErrHelp) {
			return Options{}, &helpError{text: helpText(fs)}
		}
		return Options{}, err
	}

	if len(fs.Args()) != 0 {
		return Options{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}

	targets := splitTargets(*target)
	seen := make(map[string]bool, len(targets))
	for _, t := range targets {
		if t != "ts" && t != "zod" && t != "swift" && t != "kotlin" && t != "dart" {
			return Options{}, fmt.Errorf("target %q not yet implemented (only \"ts\", \"zod\", \"swift\", \"kotlin\", and \"dart\" are currently supported)", t)
		}
		if seen[t] {
			return Options{}, fmt.Errorf("target %q specified more than once in --target %q", t, *target)
		}
		seen[t] = true
	}
	if (*outDir == "") == (*outFile == "") {
		return Options{}, fmt.Errorf("exactly one of --output or --out-file is required")
	}
	if len(targets) > 1 && *outFile != "" {
		return Options{}, fmt.Errorf("--out-file cannot be used with multiple --target values (%q); use --output instead, which writes one subdirectory per target", *target)
	}

	return Options{Source: source, Target: *target, OutDir: *outDir, OutFile: *outFile}, nil
}

// helpText renders fs's usage line plus its flag defaults into a string.
// fs's own Output is kept as io.Discard everywhere else (see parseArgs), so
// this temporarily points it at an in-memory buffer to capture
// PrintDefaults' text instead of letting it escape to a real stream.
func helpText(fs *flag.FlagSet) string {
	var buf bytes.Buffer
	buf.WriteString(usageLine + "\n")
	fs.SetOutput(&buf)
	fs.PrintDefaults()
	fs.SetOutput(io.Discard)
	return buf.String()
}
