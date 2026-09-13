//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"syscall/js"
	"testing"
)

const simpleSpec = `{
  "openapi": "3.0.3",
  "components": {
    "schemas": {
      "Pet": {
        "type": "object",
        "properties": {"name": {"type": "string"}},
        "required": ["name"]
      }
    }
  }
}`

type jsResult struct {
	Files map[string]string `json:"files"`
	Error string            `json:"error"`
}

func callGenerateJS(t *testing.T, args []js.Value) jsResult {
	t.Helper()
	result := generateJS(js.Undefined(), args)
	resultStr, ok := result.(string)
	if !ok {
		t.Fatalf("expected string result, got %T (%v)", result, result)
	}
	var parsed jsResult
	if err := json.Unmarshal([]byte(resultStr), &parsed); err != nil {
		t.Fatalf("unmarshal result %q: %v", resultStr, err)
	}
	return parsed
}

func TestGenerateJS_Success(t *testing.T) {
	parsed := callGenerateJS(t, []js.Value{js.ValueOf(simpleSpec), js.ValueOf("ts"), js.ValueOf(false)})
	if parsed.Error != "" {
		t.Fatalf("unexpected error: %s", parsed.Error)
	}
	content, ok := parsed.Files["index.ts"]
	if !ok {
		t.Fatal("expected index.ts in files")
	}
	if !strings.Contains(content, "export interface Pet {") {
		t.Errorf("expected Pet interface, got:\n%s", content)
	}
}

func TestGenerateJS_GenerationError(t *testing.T) {
	parsed := callGenerateJS(t, []js.Value{js.ValueOf(""), js.ValueOf("ts"), js.ValueOf(false)})
	if parsed.Error == "" {
		t.Fatal("expected a non-empty error message for an unparseable spec")
	}
}

func TestGenerateJS_WrongArgCount(t *testing.T) {
	parsed := callGenerateJS(t, []js.Value{js.ValueOf("only one arg")})
	if parsed.Error == "" {
		t.Fatal("expected a non-empty error message for the wrong argument count")
	}
}

// TestGenerateJS_WrongArgTypes guards the bridge's most important
// property: a caller passing a wrong-typed argument must get a clean
// {"error": ...} result back. Before the type checks existed, args[2].Bool()
// panicked on a non-boolean, which does not merely fail that one call — an
// unrecovered panic tears down the whole Go WASM program (main()'s select{}
// included), so every later openapi2codeGenerate call on the page throws
// "Go program has already exited" until a full reload.
func TestGenerateJS_WrongArgTypes(t *testing.T) {
	tests := []struct {
		name string
		args []js.Value
	}{
		{"non-boolean modular", []js.Value{js.ValueOf(simpleSpec), js.ValueOf("ts"), js.ValueOf("not-a-bool")}},
		{"undefined modular", []js.Value{js.ValueOf(simpleSpec), js.ValueOf("ts"), js.Undefined()}},
		{"numeric specText", []js.Value{js.ValueOf(42), js.ValueOf("ts"), js.ValueOf(false)}},
		{"undefined specText", []js.Value{js.Undefined(), js.ValueOf("ts"), js.ValueOf(false)}},
		{"numeric target", []js.Value{js.ValueOf(simpleSpec), js.ValueOf(42), js.ValueOf(false)}},
		{"null target", []js.Value{js.ValueOf(simpleSpec), js.Null(), js.ValueOf(false)}},
		{"all wrong", []js.Value{js.Null(), js.Undefined(), js.ValueOf(1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := callGenerateJS(t, tt.args)
			if parsed.Error == "" {
				t.Fatalf("expected a non-empty error message for %s, got files: %v", tt.name, parsed.Files)
			}
			if parsed.Files != nil {
				t.Errorf("expected no files alongside the error, got: %v", parsed.Files)
			}
		})
	}
}

func TestGenerateJS_TooManyArgs(t *testing.T) {
	parsed := callGenerateJS(t, []js.Value{
		js.ValueOf(simpleSpec), js.ValueOf("ts"), js.ValueOf(false), js.ValueOf("extra"),
	})
	if parsed.Error == "" {
		t.Fatal("expected a non-empty error message for too many arguments")
	}
}

func TestGenerateJS_UnsupportedTarget(t *testing.T) {
	parsed := callGenerateJS(t, []js.Value{js.ValueOf(simpleSpec), js.ValueOf("cobol"), js.ValueOf(false)})
	if parsed.Error == "" {
		t.Fatal("expected a non-empty error message for an unsupported target")
	}
	if !strings.Contains(parsed.Error, "not supported") {
		t.Errorf("expected \"not supported\" in error, got: %s", parsed.Error)
	}
}

// TestGenerateJS_LongRefChainDoesNotOverflowWASMStack guards a
// previously-disclosed limitation (see
// docs/superpowers/specs/2026-09-10-future-considerations.md): a long
// chain of $ref-linked models used to overflow the WASM build's call
// stack with a JS RangeError, because internal/ir's old cycle-detection
// algorithm recursed once per link in the chain. It was rewritten to an
// iterative strongly-connected-components pass specifically to remove
// that recursion; this exercises the exact shape (and roughly the scale)
// that used to fail, against the real compiled WASM module rather than
// just the native Go build, since the JS engine's call stack is what was
// actually overflowing.
func TestGenerateJS_LongRefChainDoesNotOverflowWASMStack(t *testing.T) {
	const n = 5000
	schemas := make(map[string]interface{}, n)
	for i := 0; i < n; i++ {
		props := map[string]interface{}{}
		if i+1 < n {
			props["next"] = map[string]string{"$ref": fmt.Sprintf("#/components/schemas/M%05d", i+1)}
		}
		schemas[fmt.Sprintf("M%05d", i)] = map[string]interface{}{
			"type":       "object",
			"properties": props,
		}
	}
	doc := map[string]interface{}{
		"openapi":    "3.0.3",
		"components": map[string]interface{}{"schemas": schemas},
	}
	specBytes, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}

	parsed := callGenerateJS(t, []js.Value{js.ValueOf(string(specBytes)), js.ValueOf("ts"), js.ValueOf(false)})
	if parsed.Error != "" {
		t.Fatalf("expected a %d-model $ref chain to generate successfully, got error: %s", n, parsed.Error)
	}
	if len(parsed.Files) == 0 {
		t.Fatal("expected at least one generated file")
	}
}
