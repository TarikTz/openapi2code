# WASM Build Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Compile `pkg/engine` to WebAssembly and ship a minimal browser test page that can actually parse a spec and generate TypeScript/Zod output client-side, proving the whole pipeline works outside a Go process.

**Architecture:** `internal/wasmapi` (pure Go, normal tests) wraps `pkg/engine.Parse` + a target dispatch to `GenerateTS`/`GenerateZod`. `cmd/wasm/main.go` (`//go:build js && wasm`) is a thin `syscall/js` bridge registering one JS-callable function that calls `internal/wasmapi.Generate` and returns a JSON string. `web/` holds the committed test page (`index.html`, `app.js`) plus a build script that produces the gitignored `main.wasm`/`wasm_exec.js` artifacts.

**Tech Stack:** Standard Go compiler with `GOOS=js GOARCH=wasm` (not TinyGo). No new Go dependencies. No npm dependencies — the test page and its verification script use only browser/Node built-ins (`fetch`, `WebAssembly`), no `package.json`.

**Spec:** `docs/superpowers/specs/2026-09-10-wasm-design.md`

## Global Constraints

- `cmd/wasm/main.go` and any test file in that package MUST start with `//go:build js && wasm` — without it, `go build ./...`/`go vet ./...`/`go test ./...` on the normal dev machine (darwin/arm64, or any non-js/wasm target) would fail outright, since `syscall/js` doesn't exist for other `GOOS` values. Verified during planning: with the tag present, `go build ./...` silently skips the package when other packages exist in the module; without it, the same command fails to compile.
- All three input modes (file/paste/URL) are resolved to a plain string entirely in JavaScript — the WASM module never does I/O and never receives anything but an already-resolved spec string plus the two generation options.
- JS-facing contract: one function, `(specText: string, target: string, modular: boolean) => string` (a JSON string). Success: `{"files": {...}}`. Failure: `{"error": "..."}` (the error's plain message, one line, no stack trace — matching every other error surface in this project).
- `internal/wasmapi` is plain Go with no build tags and no `syscall/js` — it is testable with an ordinary `go test ./internal/wasmapi/...`.
- No change to `pkg/engine`, `internal/gen/ts`, `internal/gen/zod`, `internal/ir`, or `internal/spec`.
- Build artifacts (`web/main.wasm`, `web/wasm_exec.js`) are gitignored; `web/index.html`, `web/app.js`, `web/build.sh`, and `web/smoketest.mjs` are committed source.

---

## Task 1: `internal/wasmapi` — pure-Go generation dispatch

**Files:**
- Create: `internal/wasmapi/wasmapi.go`
- Test: `internal/wasmapi/wasmapi_test.go`

**Interfaces:**
- Consumes: `engine.Parse`, `engine.GenerateTS`, `engine.TSOptions`, `engine.GenerateZod`, `engine.ZodOptions`, `engine.Output` (all pre-existing, unchanged).
- Produces: `wasmapi.Generate(specText string, target string, modular bool) (engine.Output, error)` — the single entry point `cmd/wasm` (Task 2) calls into.

- [ ] **Step 1: Write the failing tests**

Create `internal/wasmapi/wasmapi_test.go`:

```go
package wasmapi_test

import (
	"strings"
	"testing"

	"github.com/tarikomercehajic/openapi2code/internal/wasmapi"
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

func TestGenerate_TSMonolithic(t *testing.T) {
	out, err := wasmapi.Generate(simpleSpec, "ts", false)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	content, ok := out.Files["index.ts"]
	if !ok {
		t.Fatal("expected index.ts in output")
	}
	if !strings.Contains(content, "export interface Pet {") {
		t.Errorf("expected Pet interface, got:\n%s", content)
	}
}

func TestGenerate_ZodModular(t *testing.T) {
	out, err := wasmapi.Generate(simpleSpec, "zod", true)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	content, ok := out.Files["Pet.ts"]
	if !ok {
		t.Fatal("expected Pet.ts in output")
	}
	if !strings.Contains(content, "export const PetSchema = z.object({") {
		t.Errorf("expected PetSchema, got:\n%s", content)
	}
}

func TestGenerate_UnsupportedTarget(t *testing.T) {
	_, err := wasmapi.Generate(simpleSpec, "swift", false)
	if err == nil {
		t.Fatal("expected error for unsupported target")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("expected \"not supported\" in error, got: %v", err)
	}
}

func TestGenerate_ParseError(t *testing.T) {
	_, err := wasmapi.Generate("", "ts", false)
	if err == nil {
		t.Fatal("expected error for an empty/unparseable spec")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/wasmapi/...`
Expected: FAIL — package `wasmapi` does not exist.

- [ ] **Step 3: Write the implementation**

Create `internal/wasmapi/wasmapi.go`:

```go
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

// Generate parses specText and generates code for target ("ts" or
// "zod"), in monolithic or modular layout per modular.
func Generate(specText string, target string, modular bool) (engine.Output, error) {
	doc, err := engine.Parse([]byte(specText))
	if err != nil {
		return engine.Output{}, err
	}
	switch target {
	case "ts":
		return engine.GenerateTS(doc, engine.TSOptions{Modular: modular})
	case "zod":
		return engine.GenerateZod(doc, engine.ZodOptions{Modular: modular})
	default:
		return engine.Output{}, fmt.Errorf("target %q not supported (only \"ts\" and \"zod\" are currently supported)", target)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/wasmapi/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/wasmapi/wasmapi.go internal/wasmapi/wasmapi_test.go
git commit -m "feat: add internal/wasmapi generation dispatch for the WASM bridge"
```

---

## Task 2: `cmd/wasm` — the WASM entrypoint

**Files:**
- Create: `cmd/wasm/main.go`
- Test: `cmd/wasm/main_test.go`

**Interfaces:**
- Consumes: `wasmapi.Generate` (Task 1).
- Produces: a registered JS global function `openapi2codeGenerate` (via `js.Global().Set(...)`), backed by the plain Go function `generateJS(this js.Value, args []js.Value) interface{}` — exposed as a plain function specifically so it can be called directly from a Go test with hand-built `js.Value` arguments, not only from real JS.

**Note on testing this task:** unlike a normal Go package, this one needs a `js/wasm` build target even to run its own tests. This was verified to work during planning: Go ships a `go_js_wasm_exec` helper (at `$(go env GOROOT)/lib/wasm/go_js_wasm_exec`) that runs a compiled `js/wasm` test binary under Node — so `GOOS=js GOARCH=wasm GOFLAGS="-exec=$(go env GOROOT)/lib/wasm/go_js_wasm_exec" go test ./cmd/wasm/...` gives genuine, automated test coverage of the `syscall/js` bridge, without needing a real browser. Use this exact command for this task's RED/GREEN steps instead of a plain `go test`.

- [ ] **Step 1: Write the failing tests**

Create `cmd/wasm/main_test.go`:

```go
//go:build js && wasm

package main

import (
	"encoding/json"
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOOS=js GOARCH=wasm GOFLAGS="-exec=$(go env GOROOT)/lib/wasm/go_js_wasm_exec" go test ./cmd/wasm/...`
Expected: FAIL — `generateJS` undefined (package `main` in `cmd/wasm` doesn't exist yet).

- [ ] **Step 3: Write the implementation**

Create `cmd/wasm/main.go`:

```go
//go:build js && wasm

// Command wasm compiles the OpenAPI2Code core engine to WebAssembly and
// exposes one JS-callable function, openapi2codeGenerate, for the
// browser test page in web/ (and, later, the full playground) to call.
package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/tarikomercehajic/openapi2code/internal/wasmapi"
)

// generateJS is the JS-callable bridge: (specText string, target
// string, modular bool) -> a JSON string. Exposed as a plain Go
// function, not only as a registered js.Func, so it can be exercised
// directly by GOOS=js GOARCH=wasm tests without a real browser.
func generateJS(this js.Value, args []js.Value) interface{} {
	if len(args) != 3 {
		return marshalError("expected 3 arguments: specText, target, modular")
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOOS=js GOARCH=wasm GOFLAGS="-exec=$(go env GOROOT)/lib/wasm/go_js_wasm_exec" go test ./cmd/wasm/...`
Expected: PASS

Also confirm the package builds for its real target and that it does NOT break the normal build:

Run: `GOOS=js GOARCH=wasm go build -o /tmp/openapi2code-wasm-check.wasm ./cmd/wasm`
Expected: success (no output)

Run: `go build ./...`
Expected: success — `cmd/wasm` is silently skipped for the host `GOOS`/`GOARCH` (verified during planning: with other buildable packages present in the module, `go build ./...` does not even warn about the skipped package).

- [ ] **Step 5: Commit**

```bash
git add cmd/wasm/main.go cmd/wasm/main_test.go
git commit -m "feat: add cmd/wasm — the WASM entrypoint exposing openapi2codeGenerate"
```

---

## Task 3: `web/` — build script, test page, and a Node-based mechanism smoke test

**Files:**
- Create: `web/build.sh`
- Create: `web/index.html`
- Create: `web/app.js`
- Create: `web/smoketest.mjs`
- Modify: `.gitignore` (repo root)

**Interfaces:**
- Consumes: `cmd/wasm`'s compiled output (`main.wasm`) and the `openapi2codeGenerate` global function it registers (Task 2).

- [ ] **Step 1: Add build artifacts to `.gitignore`**

Add to the repo root `.gitignore` (alongside the existing `.superpowers/`, `.claude/` entries):

```
web/main.wasm
web/wasm_exec.js
```

- [ ] **Step 2: Write the build script**

Create `web/build.sh` (make it executable: `chmod +x web/build.sh`):

```bash
#!/bin/sh
set -e
cd "$(dirname "$0")/.."
GOOS=js GOARCH=wasm go build -o web/main.wasm ./cmd/wasm
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" web/wasm_exec.js
echo "Built web/main.wasm and copied web/wasm_exec.js"
```

- [ ] **Step 3: Run the build script and verify it produces the expected artifacts**

Run: `./web/build.sh`
Expected: prints the success message; `web/main.wasm` and `web/wasm_exec.js` now exist.

Run: `ls -la web/main.wasm web/wasm_exec.js`
Expected: both files present, `main.wasm` a few MB in size.

- [ ] **Step 4: Write the test page**

Create `web/index.html`:

```html
<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>OpenAPI2Code — WASM test page</title>
  <style>
    body { font-family: system-ui, sans-serif; margin: 2rem; max-width: 900px; }
    textarea { width: 100%; height: 200px; font-family: monospace; box-sizing: border-box; }
    select, button, label { margin: 0.5rem 0.5rem 0.5rem 0; }
    pre { background: #f4f4f4; padding: 1rem; overflow: auto; white-space: pre-wrap; }
    .error { color: #b00020; }
  </style>
</head>
<body>
  <h1>OpenAPI2Code — WASM test page</h1>
  <p>Paste an OpenAPI/Swagger spec (JSON or YAML) below. This is a minimal
     proof-of-concept page for sub-project 4 — the full playground UI is
     sub-project 5.</p>
  <textarea id="spec-input" placeholder="Paste your OpenAPI spec here..."></textarea>
  <div>
    <label>Target:
      <select id="target-select">
        <option value="ts">TypeScript</option>
        <option value="zod">Zod</option>
      </select>
    </label>
    <label><input type="checkbox" id="modular-checkbox"> Modular output</label>
    <button id="generate-button" disabled>Generate</button>
  </div>
  <h2>Output</h2>
  <pre id="output">Loading WASM module...</pre>

  <script src="wasm_exec.js"></script>
  <script src="app.js"></script>
</body>
</html>
```

- [ ] **Step 5: Write the page's JavaScript**

Create `web/app.js`:

```js
// app.js — minimal WASM test page wiring. Loads main.wasm via
// wasm_exec.js (which defines the global Go class) and calls the
// openapi2codeGenerate function it registers.

async function loadWasm() {
  const go = new Go();
  const result = await WebAssembly.instantiateStreaming(fetch("main.wasm"), go.importObject);
  go.run(result.instance); // never resolves — main() runs select{} forever; don't await it
  while (typeof window.openapi2codeGenerate !== "function") {
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
}

const specInput = document.getElementById("spec-input");
const targetSelect = document.getElementById("target-select");
const modularCheckbox = document.getElementById("modular-checkbox");
const generateButton = document.getElementById("generate-button");
const output = document.getElementById("output");

loadWasm()
  .then(() => {
    generateButton.disabled = false;
    output.textContent = "WASM module loaded. Paste a spec and click Generate.";
  })
  .catch((err) => {
    output.textContent = "Failed to load WASM module: " + err;
    output.classList.add("error");
  });

generateButton.addEventListener("click", () => {
  const raw = window.openapi2codeGenerate(specInput.value, targetSelect.value, modularCheckbox.checked);
  const result = JSON.parse(raw);
  output.classList.remove("error");
  if (result.error) {
    output.textContent = "Error: " + result.error;
    output.classList.add("error");
    return;
  }
  output.textContent = JSON.stringify(result.files, null, 2);
});
```

- [ ] **Step 6: Write and run the Node-based mechanism smoke test**

This does NOT exercise `app.js`'s DOM-wiring code (Node has no DOM) — it independently verifies the underlying mechanism `app.js` depends on: that `main.wasm`, loaded via `wasm_exec.js` exactly the way a browser would load it, actually registers and correctly responds to `openapi2codeGenerate`. This was verified to work end-to-end during planning with an equivalent throwaway script.

Create `web/smoketest.mjs`:

```js
// smoketest.mjs — verifies the compiled WASM module works when loaded
// the same way the browser test page (app.js) loads it, without needing
// a real browser. Requires no npm packages (Node's built-in fetch/
// WebAssembly are sufficient). Run after web/build.sh:
//   node web/smoketest.mjs
import { readFile } from "node:fs/promises";
import "./wasm_exec.js";

const go = new globalThis.Go();
const wasmBuffer = await readFile(new URL("./main.wasm", import.meta.url));
const { instance } = await WebAssembly.instantiate(wasmBuffer, go.importObject);
go.run(instance); // fire and forget — main() never returns (select{})

while (typeof globalThis.openapi2codeGenerate !== "function") {
  await new Promise((resolve) => setTimeout(resolve, 10));
}

const spec = JSON.stringify({
  openapi: "3.0.3",
  components: {
    schemas: {
      Pet: {
        type: "object",
        properties: { name: { type: "string" } },
        required: ["name"],
      },
    },
  },
});

function check(label, raw, wantSubstring) {
  const result = JSON.parse(raw);
  if (result.error) {
    console.error(`FAIL (${label}): unexpected error: ${result.error}`);
    process.exit(1);
  }
  // Check Pet.ts before index.ts: in modular mode index.ts is always the
  // non-empty barrel file, so checking it first would short-circuit past
  // the model file that actually has the content we're asserting on. In
  // monolithic mode there is no Pet.ts key at all, so this still falls
  // through to index.ts correctly.
  const content = result.files["Pet.ts"] || result.files["index.ts"] || "";
  if (!content.includes(wantSubstring)) {
    console.error(`FAIL (${label}): output missing ${JSON.stringify(wantSubstring)}:`, result.files);
    process.exit(1);
  }
  console.log(`OK (${label}):`, Object.keys(result.files));
}

check("ts monolithic", globalThis.openapi2codeGenerate(spec, "ts", false), "export interface Pet {");
check("zod modular", globalThis.openapi2codeGenerate(spec, "zod", true), "export const PetSchema = z.object({");

const errRaw = globalThis.openapi2codeGenerate("", "ts", false);
const errResult = JSON.parse(errRaw);
if (!errResult.error) {
  console.error("FAIL (error path): expected an error for an empty spec, got:", errResult);
  process.exit(1);
}
console.log("OK (error path):", errResult.error);

console.log("All smoketest checks passed.");
process.exit(0);
```

Run: `node web/smoketest.mjs`
Expected: three `OK (...)` lines (ts monolithic, zod modular, error path) followed by `All smoketest checks passed.`, exit code 0. If this fails, do not proceed — it means the compiled WASM module doesn't actually work when driven the way a browser would drive it, even if Task 1/2's Go-level tests passed.

- [ ] **Step 7: Commit**

```bash
git add web/build.sh web/index.html web/app.js web/smoketest.mjs .gitignore
git commit -m "feat: add WASM test page, build script, and Node-based mechanism smoke test"
```

---

## Final verification

- [ ] Run `go build ./...` — expect success (cmd/wasm silently skipped for the host target).
- [ ] Run `go vet ./...` — expect no warnings.
- [ ] Run `go test ./...` — expect all packages PASS (this does NOT include `cmd/wasm`, which needs `GOOS=js GOARCH=wasm` — run its tests separately, see below).
- [ ] Run `GOOS=js GOARCH=wasm GOFLAGS="-exec=$(go env GOROOT)/lib/wasm/go_js_wasm_exec" go test ./cmd/wasm/...` — expect PASS.
- [ ] Run `./web/build.sh && node web/smoketest.mjs` — expect all checks to pass.
- [ ] Manually open `web/index.html` in a real browser (e.g. `python3 -m http.server --directory web` and visit `http://localhost:8000/`, since `file://` URLs can hit CORS restrictions on `fetch()` in some browsers) — paste a real OpenAPI spec, try both targets and both modular/monolithic modes, confirm the page actually works end-to-end. This is the one verification step in this plan that must be done by a human with a real browser, not by an agent.
