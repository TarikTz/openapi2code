// Package engine is the public API of the OpenAPI2Code core: parsing an
// OpenAPI/Swagger document into IR, and generating target-language code
// from that IR. It does no file I/O or network access, so it is safe to
// compile unchanged to WASM.
package engine

import (
	"github.com/tarikomercehajic/openapi2code/internal/ir"
	"github.com/tarikomercehajic/openapi2code/internal/spec"
)

// Parse parses raw OpenAPI/Swagger document bytes (JSON or YAML, v2 or
// v3) into the IR.
func Parse(data []byte) (*ir.Document, error) {
	raw, err := spec.Parse(data)
	if err != nil {
		return nil, err
	}
	return ir.Build(raw)
}

// TSOptions controls TypeScript output shape.
type TSOptions struct {
	// Modular selects one-file-per-model output (with a barrel index.ts)
	// instead of a single monolithic file.
	Modular bool
}

// ZodOptions controls Zod output shape.
type ZodOptions struct {
	// Modular selects one-file-per-model output (with a barrel index.ts)
	// instead of a single monolithic file.
	Modular bool
}

// Output is a generated set of files, keyed by relative file path.
type Output struct {
	Files map[string]string
}
