//go:build js && wasm

// Command wasm compiles the OpenAPI2Code core engine to WebAssembly and
// exposes one JS-callable function, openapi2codeGenerate, for the
// browser playground in web/ to call.
package main

import (
	"encoding/json"
	"fmt"
	"syscall/js"

	"github.com/tarikomercehajic/openapi2code/internal/wasmapi"
)

// generateJS is the JS-callable bridge: (specText string, target
// string, modular bool) -> a JSON string. Exposed as a plain Go
// function, not only as a registered js.Func, so it can be exercised
// directly by GOOS=js GOARCH=wasm tests without a real browser.
//
// Every failure mode must come back as a {"error": ...} JSON string, never
// as a panic: an unrecovered panic here does not just fail one call, it
// terminates the entire Go WASM program (including main()'s select{}), so
// every subsequent call from the page would throw "Go program has already
// exited" until a full reload. Hence two layers of defense — argument type
// validation before any js.Value conversion, plus a recover() below.
func generateJS(this js.Value, args []js.Value) (result interface{}) {
	defer func() {
		if r := recover(); r != nil {
			result = marshalError(fmt.Sprintf("internal error: %v", r))
		}
	}()

	if len(args) != 3 {
		return marshalError("expected 3 arguments: specText, target, modular")
	}
	// Check each value's type before converting: js.Value.Bool panics on a
	// non-boolean, and js.Value.String silently yields placeholder text like
	// "<number: 42>" for non-strings, which would be passed to the parser as
	// if it were a spec.
	if args[0].Type() != js.TypeString || args[1].Type() != js.TypeString || args[2].Type() != js.TypeBoolean {
		return marshalError(fmt.Sprintf(
			"expected (specText: string, target: string, modular: boolean), got (%s, %s, %s)",
			args[0].Type(), args[1].Type(), args[2].Type(),
		))
	}
	specText := args[0].String()
	target := args[1].String()
	modular := args[2].Bool()

	out, err := wasmapi.Generate(specText, target, modular)
	if err != nil {
		return marshalError(err.Error())
	}
	return marshalResult(out.Files)
}

func marshalResult(files map[string]string) string {
	data, err := json.Marshal(struct {
		Files map[string]string `json:"files"`
	}{Files: files})
	if err != nil {
		return marshalError(err.Error())
	}
	return string(data)
}

func marshalError(message string) string {
	data, err := json.Marshal(struct {
		Error string `json:"error"`
	}{Error: message})
	if err != nil {
		return `{"error":"internal error marshaling error message"}`
	}
	return string(data)
}

func main() {
	js.Global().Set("openapi2codeGenerate", js.FuncOf(generateJS))
	select {} // keep the module alive so the registered function stays callable
}
