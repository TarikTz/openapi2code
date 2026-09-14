# OpenAPI2Code

Turn an OpenAPI or Swagger spec into typed TypeScript, Zod, Swift, Kotlin, and Dart code — from one zero-dependency Go engine that compiles unchanged to a CLI binary and to WebAssembly.

[![CI](https://github.com/TarikTz/openapi2code/actions/workflows/ci.yml/badge.svg)](https://github.com/TarikTz/openapi2code/actions/workflows/ci.yml)
[![Go version](https://img.shields.io/github/go-mod/go-version/TarikTz/openapi2code)](go.mod)
[![License: MIT](https://img.shields.io/github/license/TarikTz/openapi2code)](LICENSE)

**[Try it live at openapi2code.com →](https://openapi2code.com/)** — paste a spec, get code; nothing you paste ever leaves your browser.

## Features

- Parses OpenAPI 3.0, OpenAPI 3.1, and Swagger 2.0, in JSON or YAML, from a local file, an `http(s)://` URL, or stdin
- Full `$ref`, `allOf`/`oneOf`/`anyOf`, type-preserving enum (string, integer, number, and boolean — not just strings), array, `additionalProperties`, nullable/optional, and circular-reference support
- Generates TypeScript, Zod v3, Swift, Kotlin, and Dart — one target or several in a single run, modular or monolithic
- Runs as a CLI, a Go library, or entirely client-side in the browser via WebAssembly
- A single static binary with one third-party dependency (`gopkg.in/yaml.v3`); everything else is the standard library
- Generated output is checked against the real compiler (`tsc`, `swiftc`, `kotlinc`, `dart`) as part of the test suite, not just diffed as text

## Status

Every planned target is implemented and tested — TypeScript, Zod v3, Swift, Kotlin, and Dart, generated from one shared intermediate representation so they can't drift out of sync with each other. Use the `openapi2code` CLI binary (see [CLI usage](#cli-usage)), import `pkg/engine` directly into your own Go code (see [Library usage](#library-usage)), or run the engine client-side in a browser (see [WASM build and playground](#wasm-build-and-playground)).

What works today:
- Parses OpenAPI 3.0, OpenAPI 3.1, and Swagger 2.0 documents, in JSON or YAML, from a local file, an `http(s)://` URL, or stdin. OpenAPI 3.1 dropped the `nullable` keyword in favor of JSON Schema's `type: [T, "null"]`; this is normalized during parsing into the exact same shape `nullable: true` produces, so every generator sees one uniform representation regardless of which OpenAPI version a field's nullability came from.
- Resolves internal `$ref`s (`#/components/schemas/*`, `#/definitions/*`)
- Full support for `allOf` (interface extension / intersection fallback), `oneOf`/`anyOf` (unions), type-preserving enums (string, integer, number, and boolean values render as their own literal type, not stringified), arrays, `additionalProperties` (dictionary/map-shaped fields), nullable vs. optional fields, and circular schema references
- Generates TypeScript interfaces, either as one file per model with a barrel `index.ts` (modular) or as a single file (monolithic)
- Generates Zod v3 validation schemas in the same two layouts, with `z.lazy()`/`z.ZodType<T>` for circular schemas. Generated files `import { z } from "zod"`, so `zod` needs to be a dependency of the *consuming* project — this repo itself stays dependency-free, since it only ever emits schema source text.
- Generates native mobile models: Swift `Codable` structs (classes for cyclic models), Kotlin data classes, and Dart classes — each with hand-written JSON (de)serialization requiring no consumer dependency, idiomatic camelCase field names with an explicit wire-name mapping back to the original JSON key, and a synthesized named type for any inline (non-`$ref`) enum or object a field resolves to. `oneOf`/`anyOf`, allOf's non-object-member fallback, and a `$ref` to an array/map alias whose items are an inline (non-`$ref`) object or enum are not yet supported for these three targets — an affected model or field is skipped with a comment rather than generated incorrectly.
- Compiles unchanged to WebAssembly, so the same engine parses and generates entirely client-side in a browser, with no server round-trip — see the playground in `web/`
- Guards against pathologically deep intra-schema nesting (500+ levels) with a clean error, instead of overflowing the call stack — most likely to matter in the browser, where the JS engine's stack is far smaller than a native Go binary's

`additionalProperties: true` (no value schema) and a schema mixing fixed `properties` with `additionalProperties` are both deliberately out of scope for now.

## Requirements

- Go 1.25 or newer (see [go.mod](go.mod))

No other tools are required — the only third-party dependency is `gopkg.in/yaml.v3`, used for YAML input.

## Getting started

Clone the repo and download dependencies:

```bash
git clone https://github.com/TarikTz/openapi2code.git
cd openapi2code
go mod download
```

Build and vet everything:

```bash
go build ./...
go vet ./...
```

Run the full test suite:

```bash
go test ./...
./web/test.sh   # the WASM pieces, which go test ./... cannot reach — see below
```

`go test ./...` does **not** cover `cmd/wasm`: that package is behind a `//go:build js && wasm` constraint (it imports `syscall/js`, which doesn't exist for any other `GOOS`), so the normal suite silently skips it. `./web/test.sh` is its counterpart — see [WASM build and playground](#wasm-build-and-playground).

The suite includes unit tests for every package plus an end-to-end golden-file suite (`pkg/engine/golden_test.go`) that runs realistic fixture specs — OpenAPI 3.0, Swagger 2.0, schema composition, circular refs, nullable/enum fields, and modular output — through the whole pipeline and diffs the result against committed expected TypeScript output. If you change generator behavior on purpose, regenerate the golden files and inspect the diff before committing:

```bash
go test ./pkg/engine/... -run TestGoldenFixtures -update
go test ./pkg/engine/...
```

## CLI usage

Download a prebuilt binary from [Releases](https://github.com/TarikTz/openapi2code/releases/latest), or build it yourself:

```bash
go build -o openapi2code ./cmd/openapi2code
```

Generate TypeScript from a spec — one file per model plus a barrel `index.ts`:

```bash
./openapi2code pull ./petstore.yaml --target ts --output ./src/types
```

Or a single monolithic file:

```bash
./openapi2code pull ./petstore.yaml --target ts --out-file ./src/types.ts
```

Generate Zod v3 validation schemas instead, in either layout:

```bash
./openapi2code pull ./petstore.yaml --target zod --output ./src/schemas
./openapi2code pull ./petstore.yaml --target zod --out-file ./src/schemas.ts
```

Each generated file starts with `import { z } from "zod";`, so add `zod` to the consuming project's `package.json` (`npm install zod`). Every model is emitted as a `<Model>Schema` constant plus its inferred type — `export const PetSchema = z.object({ ... }); export type Pet = z.infer<typeof PetSchema>;` — so you can both validate and type-annotate from one import.

`<source>` also accepts an `http(s)://` URL (fetched with a 30s timeout) or `-` to read the spec from stdin:

```bash
./openapi2code pull https://dev.example.com/openapi.json --output ./src/types
cat petstore.yaml | ./openapi2code pull - --out-file ./src/types.ts
```

Generate more than one target in a single run with a comma-separated `--target`:

```bash
./openapi2code pull https://dev.example.com/openapi.json --target ts,zod --output ./src/types
# -> ./src/types/ts/*.ts, ./src/types/zod/*.ts
```

`--target` accepts `ts` (default), `zod`, `swift`, `kotlin`, or `dart` — comma-separate more than one (e.g. `--target ts,zod`) to generate several in one run. With a single target, `--output` and `--out-file` both work as shown above; with more than one, `--out-file` isn't allowed (there's no single file to merge multiple languages into) and `--output <dir>` writes one subdirectory per target (`<dir>/ts/`, `<dir>/zod/`, ...), since e.g. `ts` and `zod` both name their monolithic/barrel file `index.ts` and would otherwise collide in a flat directory. Existing files at the exact generated paths are overwritten on rerun — nothing else in the destination is touched, so it's safe to run repeatedly in a dev loop or CI step. Run `openapi2code pull --help` for the full flag list.

## Library usage

You can also use the engine directly as a Go library — this is what the CLI itself calls into. The whole public API is six functions in `pkg/engine` — `Parse`, `GenerateTS`, `GenerateZod`, `GenerateSwift`, `GenerateKotlin`, and `GenerateDart` (the generators take `engine.*Options` and return the same file map, keyed identically):

```go
package main

import (
	"fmt"
	"os"

	"github.com/tarikomercehajic/openapi2code/pkg/engine"
)

func main() {
	spec, err := os.ReadFile("petstore.yaml") // or .json — both are accepted
	if err != nil {
		panic(err)
	}

	doc, err := engine.Parse(spec)
	if err != nil {
		panic(err)
	}

	// Monolithic: a single file at out.Files["index.ts"]
	out, err := engine.GenerateTS(doc, engine.TSOptions{Modular: false})
	if err != nil {
		panic(err)
	}
	fmt.Println(out.Files["index.ts"])

	// Modular: one file per model at out.Files["<Model>.ts"], plus a
	// barrel re-export at out.Files["index.ts"]
	modular, err := engine.GenerateTS(doc, engine.TSOptions{Modular: true})
	if err != nil {
		panic(err)
	}
	for name, content := range modular.Files {
		fmt.Println("---", name, "---")
		fmt.Println(content)
	}
}
```

`engine.Parse`, `engine.GenerateTS`, and `engine.GenerateZod` take and return only in-memory values (`[]byte` in, a `map[string]string` of generated files out) — there's no file or network I/O inside the engine itself, which is what lets the same code compile to WebAssembly unchanged.

## WASM build and playground

The engine also compiles to WebAssembly, so a browser can parse a spec and generate code entirely client-side. `web/` holds the whole site: the playground itself (paste a spec, or pick one from the built-in example gallery, and see live, syntax-highlighted, tabbed output as you type) plus its use-cases, examples, privacy, terms, and contact pages.

Build it (produces `web/main.wasm` and `web/wasm_exec.js`, both gitignored, plus the compiled `web/style.css`):

```bash
./web/build.sh
```

`web/style.css` is committed, not gitignored — every page links it directly with no CDN fallback — so re-run `./web/build.sh` and commit the result after changing `web/tailwind.css` or any page's classes. It's built via the Tailwind CLI (`web/package.json`), a dev dependency of the website only; it doesn't touch the Go module.

Then serve `web/` and open it in a browser. Use a local server rather than opening `web/index.html` as a `file://` URL — browsers apply CORS restrictions to `fetch()` on `file://`, which blocks loading `main.wasm`:

```bash
python3 -m http.server --directory web    # then visit http://localhost:8000/
```

Paste a spec into the textarea (or click one of the example buttons above it) and pick a target and layout — generation happens automatically as you type or change a setting, no button to click. Modular output shows one tab per generated file; click a tab to switch the displayed file. The copy icon above the output pane copies the active tab's raw content to your clipboard, and the sun/moon icon in the header toggles between light and dark themes (remembered across reloads).

Run the WASM-specific tests:

```bash
./web/test.sh
```

That script rebuilds the module and then runs both checks that `go test ./...` can't:

- `GOOS=js GOARCH=wasm GOFLAGS="-exec=$(go env GOROOT)/lib/wasm/go_js_wasm_exec" go test ./cmd/wasm/...` — the `syscall/js` bridge's own Go tests, run under Node via the `go_js_wasm_exec` helper Go ships
- `node web/smoketest.mjs` — loads the compiled `main.wasm` through `wasm_exec.js` exactly the way the browser page does and exercises the registered function, so a module that compiles but doesn't actually work when driven from JS is caught

Both need Node 20.19+ or 22.7+ (they use only built-ins, there's no `package.json` and nothing to install — but `highlight.js`/`highlight_test.mjs` rely on Node's unflagged ES module auto-detection for extensionless `.js` files, which only landed at those versions). `web/test.sh` checks the running Node version and fails fast with a clear message if it's older.

The JS-facing contract is one function registered on `window`:

```js
window.openapi2codeGenerate(specText, target, modular) // -> a JSON string
```

`target` is `"ts"`, `"zod"`, `"swift"`, `"kotlin"`, or `"dart"`, `modular` is a boolean, and the result is `{"files": {"<name>": "<content>", ...}}` on success or `{"error": "<message>"}` on failure — including for wrong argument types, so a bad call returns an error rather than tearing down the module. Getting the spec text into that string happens entirely in JavaScript, outside the WASM module — the playground (`web/`) supports pasting, a built-in example gallery, dropping a file onto the editor, and fetching a spec from a URL client-side (subject to the target server allowing cross-origin requests; the page shows a clear error when it doesn't, since no page-side setting can work around that browser restriction) — the module itself has no I/O either way, so any of these input paths is free to change without touching it.

## Project layout

```
internal/spec/    OpenAPI/Swagger v2 & v3 parsing, version detection, $ref resolution
internal/ir/      Intermediate representation (IR) and the builder that produces it
internal/gen/ts/  TypeScript code generation from the IR
internal/gen/zod/ Zod v3 schema generation from the IR
internal/gen/mobile/  Naming helpers shared by the Swift/Kotlin/Dart generators
internal/gen/swift/   Swift code generation from the IR
internal/gen/kotlin/  Kotlin code generation from the IR
internal/gen/dart/    Dart code generation from the IR
pkg/engine/       Public API — see Library usage above — the only package meant to be imported
internal/cli/     CLI logic: flag parsing, source resolution (file/URL/stdin), output writing
cmd/openapi2code/ The openapi2code binary's entrypoint — a thin wrapper around internal/cli
internal/wasmapi/ Pure-Go generation dispatch for the WASM build (no syscall/js, normal tests)
cmd/wasm/         The js/wasm entrypoint — a thin syscall/js bridge over internal/wasmapi
web/              The website: the WASM playground, its use-cases/examples/legal pages, and their build and test scripts
```

## Development

Project and architecture conventions live in [Claude.md](Claude.md).

## License

[MIT](LICENSE)
