# Design: WASM Build

**Date:** 2026-09-10
**Status:** Approved for planning
**Sub-project:** 4 of 5 (see `docs/superpowers/specs/2026-09-10-core-engine-ts-generator-design.md` for the full decomposition)

## Goal

Compile the existing core engine (`pkg/engine`) to WebAssembly so it can run entirely client-side in a browser, per the PRD's "WASM build" architecture goal, and ship a minimal HTML test page proving it actually works end-to-end in a real browser. The polished split-screen playground UI (`OpenAPI2Code.com`) is sub-project 5 and explicitly out of scope here — this sub-project's own deliverable is the WASM module plus just enough of a page to click through it.

Reprioritized ahead of the Swift/Kotlin/Dart generators (originally next in the roadmap) at the user's request, so there's something visually testable to play with while those generators are built later.

## Why this is straightforward

`pkg/engine`'s public API (`Parse`, `GenerateTS`, `GenerateZod`) already takes only `[]byte`/primitive arguments and returns only in-memory values — no file or network I/O anywhere in the core engine, by design, specifically for this moment (see `Claude.md`'s "one core engine, two frontends" constraint, in place since sub-project 1). The WASM module needs zero new engine-side work; it's purely a thin bridging layer.

## Input resolution: entirely in JavaScript

Mirroring the CLI's own separation (`internal/cli` resolves file/URL/stdin *outside* `pkg/engine`), all three input modes for the browser — local file, raw paste, remote URL — resolve to a plain string entirely in JavaScript, using the browser's native capabilities (`FileReader`, a `<textarea>`, `fetch()`). The WASM module is never involved in I/O and never sees anything but an already-resolved spec string. This makes the WASM bridge even simpler than `internal/cli`, which at least has its own `resolveSource` — the WASM side needs no equivalent at all.

## Compiler choice

Standard Go compiler (`GOOS=js GOARCH=wasm go build`), not TinyGo. The already-built, already-tested core engine (including `gopkg.in/yaml.v3`) is guaranteed to compile as-is under the standard toolchain; TinyGo's stdlib/reflection gaps are a real, unquantified risk against working code for an unproven binary-size payoff at this stage. Binary size (~2-3MB typical for a Go WASM build) is an acceptable one-time page-load cost.

## Architecture

```
pkg/engine (unchanged)
    ↑
internal/wasmapi/            — pure Go, no build tags, no syscall/js:
                                Generate(specText, target string, modular bool) (Output, error)
                                wraps engine.Parse + a target dispatch to GenerateTS/GenerateZod.
                                Testable with plain `go test` like everything else in this repo.
    ↑
cmd/wasm/main.go             — //go:build js && wasm
                                Registers one JS-callable global function. Converts js.Value
                                arguments to Go strings/bools, calls internal/wasmapi.Generate,
                                marshals the result to a JSON string, returns it synchronously.
    ↓ (compiled output, gitignored)
web/main.wasm, web/wasm_exec.js
    ↓ (committed)
web/index.html, web/app.js   — minimal test page: a textarea for the spec, a target/mode
                                picker, an output panel. Loads main.wasm via wasm_exec.js,
                                calls the JS-callable function, JSON.parses the result.
```

**Build constraint is load-bearing.** `cmd/wasm/main.go` imports `syscall/js`, which does not exist outside `GOOS=js`. Without `//go:build js && wasm` at the top of the file, adding this package would break `go build ./...`, `go vet ./...`, and `go test ./...` on every other platform — including the commands already documented in `README.md` and used throughout this project's own CI-equivalent (manual) workflow. With the constraint, a normal `go build ./...` on the dev machine simply skips the package, exactly as intended.

## JS-facing contract

One function, registered on `window` (exact name decided at implementation time, e.g. `window.openapi2codeGenerate`), called synchronously — no `Promise`/async wrapping needed, since generation is CPU-bound and fast (the sub-project 1 benchmark measured ~15ms for a 300-schema spec; anything a browser user pastes will be well under any latency budget worth async-ifying for).

Signature: `(specText: string, target: string, modular: boolean) => string` (a JSON string).

- Success: `{"files": {"<name>": "<content>", ...}}` — same shape as `engine.Output.Files`.
- Failure: `{"error": "<message>"}` — the error's `.Error()` text, same as every other error surface in this project (one message, no stack trace).

Returning a JSON string (parsed with `JSON.parse()` on the JS side) rather than constructing a `js.Value` object field-by-field via `syscall/js` calls avoids a meaningful amount of fiddly, hard-to-get-right object-construction code in `cmd/wasm/main.go` for no real benefit — JSON round-tripping is simple, well-understood, and Go's `encoding/json` (already a dependency via `internal/spec`) handles the marshaling for free.

## Testing

- `internal/wasmapi`: normal Go unit tests, no build tags, no js/wasm runtime needed — it's pure Go calling the already-tested `pkg/engine`. Table-driven tests for the target dispatch (`"ts"` → `GenerateTS`, `"zod"` → `GenerateZod`, anything else → a clear error) and for both `modular` values.
- `cmd/wasm/main.go`: not covered by automated Go tests (a `js/wasm` build target has no meaningful way to run `go test` without a browser or a JS test runner like `wasmbrowsertest`, which this sub-project does not set up). Verified instead by actually loading `web/index.html` in a real browser and exercising it manually — consistent with this project's established pattern of trusting real execution (`tsc`/`node`, in the Zod generator sub-project) over speculative unit tests wherever a language/runtime boundary is crossed.

## Explicitly out of scope for this sub-project

- The polished split-screen playground UI (`OpenAPI2Code.com`'s actual design) — sub-project 5.
- Any Go-side WASM automated test runner (`wasmbrowsertest` or similar) — the manual-browser-verification approach is accepted for this sub-project's scope.
- Swift/Kotlin/Dart generators — unaffected, unblocked, resume whenever the roadmap returns to them.
- Any change to `pkg/engine`, `internal/gen/ts`, `internal/gen/zod`, `internal/ir`, or `internal/spec`.
