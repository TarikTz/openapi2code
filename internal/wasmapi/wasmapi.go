// Package wasmapi is the pure-Go bridge between cmd/wasm's syscall/js
// glue and pkg/engine. It has no browser/JS dependencies of its own, so
// it's testable like any other package in this repo — cmd/wasm's thin
// syscall/js layer is the only part of the WASM sub-project that
// actually needs a js/wasm build target.
package wasmapi

import (
	"fmt"

	"github.com/tarikomercehajic/openapi2code/pkg/engine"
)

// Generate parses specText and generates code for target ("ts", "zod",
// "swift", "kotlin", or "dart"), in monolithic or modular layout per
// modular.
func Generate(specText string, target string, modular bool) (engine.Output, error) {
	// Validate the target before parsing. Otherwise, when both the target
	// and the spec are bad, the parse error wins and the caller is never
	// told that the target they picked isn't supported at all — a confusing
	// answer, since no spec would have made that target work.
	switch target {
	case "ts", "zod", "swift", "kotlin", "dart":
	default:
		return engine.Output{}, unsupportedTargetError(target)
	}

	doc, err := engine.Parse([]byte(specText))
	if err != nil {
		return engine.Output{}, err
	}
	switch target {
	case "ts":
		return engine.GenerateTS(doc, engine.TSOptions{Modular: modular})
	case "zod":
		return engine.GenerateZod(doc, engine.ZodOptions{Modular: modular})
	case "swift":
		return engine.GenerateSwift(doc, engine.SwiftOptions{Modular: modular})
	case "kotlin":
		return engine.GenerateKotlin(doc, engine.KotlinOptions{Modular: modular})
	case "dart":
		return engine.GenerateDart(doc, engine.DartOptions{Modular: modular})
	default:
		// Unreachable: target was validated above.
		return engine.Output{}, unsupportedTargetError(target)
	}
}

func unsupportedTargetError(target string) error {
	return fmt.Errorf("target %q not supported (only \"ts\", \"zod\", \"swift\", \"kotlin\", and \"dart\" are currently supported)", target)
}
