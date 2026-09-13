package cli

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/tarikomercehajic/openapi2code/pkg/engine"
)

// Run executes the CLI: parses args, resolves the source, generates code,
// and writes it out. It returns the process exit code (0 on success, 1 on
// any failure) rather than calling os.Exit itself, and takes stdin/stderr
// as parameters so it's testable without real process I/O.
func Run(args []string, stdin io.Reader, stderr io.Writer) int {
	opts, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		if errors.Is(err, errHelp) {
			// An explicit help request ("pull --help") is not a failure.
			return 0
		}
		return 1
	}

	data, err := resolveSource(opts.Source, stdin)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	doc, err := engine.Parse(data)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	targets := splitTargets(opts.Target)
	// Multiple targets always write into a subdirectory per target (e.g.
	// ./out/ts/, ./out/zod/) rather than a flat directory: two targets
	// commonly share a monolithic file name (GenerateTS and GenerateZod
	// both key their single-file output "index.ts"), so a flat directory
	// would let one target's output silently overwrite another's.
	// parseArgs already rejects --out-file whenever more than one target
	// is given, so opts.OutFile is always "" here in that case.
	multi := len(targets) > 1

	for _, target := range targets {
		var out engine.Output
		modular := opts.OutDir != ""
		switch target {
		case "zod":
			out, err = engine.GenerateZod(doc, engine.ZodOptions{Modular: modular})
		case "swift":
			out, err = engine.GenerateSwift(doc, engine.SwiftOptions{Modular: modular})
		case "kotlin":
			out, err = engine.GenerateKotlin(doc, engine.KotlinOptions{Modular: modular})
		case "dart":
			out, err = engine.GenerateDart(doc, engine.DartOptions{Modular: modular})
		default:
			out, err = engine.GenerateTS(doc, engine.TSOptions{Modular: modular})
		}
		if err != nil {
			fmt.Fprintln(stderr, fmt.Errorf("target %q: %w", target, err))
			return 1
		}

		outDir := opts.OutDir
		if multi {
			outDir = filepath.Join(opts.OutDir, target)
		}
		if err := writeOutput(out, outDir, opts.OutFile); err != nil {
			fmt.Fprintln(stderr, fmt.Errorf("target %q: %w", target, err))
			return 1
		}
	}

	return 0
}
