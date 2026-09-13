# Design: Web Playground (OpenAPI2Code.com UI)

**Date:** 2026-09-11
**Status:** Approved for planning
**Sub-project:** 5 of 5 (see `docs/superpowers/specs/2026-09-10-core-engine-ts-generator-design.md` for the full decomposition)

## Goal

Replace sub-project 4's deliberately minimal WASM test page with a genuinely
polished, split-screen playground: paste (or pick an example) OpenAPI/Swagger
spec on the left, see live-generated, syntax-highlighted code on the right.
This is a dev tool, not a marketing site — no hero section, testimonials, or
pricing, just the tool done well. Deployment/hosting/domain (the actual
`OpenAPI2Code.com`) is out of scope; the deliverable is a static page that
runs the same way sub-project 4's did (`./web/build.sh` + any static file
server).

## Relationship to sub-project 4

Sub-project 4 built the WASM module (`internal/wasmapi`, `cmd/wasm`) and a
minimal test page (`web/index.html`, `web/app.js`) proving the pipeline
works end-to-end in a browser. This sub-project **replaces the page in
place** — same `web/` directory, same build/test scripts — and does not
change the WASM module's JS contract:

```js
window.openapi2codeGenerate(specText, target, modular) // -> a JSON string
```

`web/build.sh`, `web/test.sh`, and `web/smoketest.mjs` are unaffected:
`smoketest.mjs` loads `main.wasm` via `wasm_exec.js` and calls
`openapi2codeGenerate` directly — it never touches `index.html` or the
frontend JS, so replacing the page's UI doesn't risk that test coverage.

## The one non-UI change: a depth guard in `internal/ir`

**Status before this sub-project** (recorded in
`docs/superpowers/specs/2026-09-10-future-considerations.md`): a spec with
very deep nested/self-referential-via-array schema structure — reproduced
at ~3000 levels — overflows the WASM call stack during IR building/codegen,
surfacing as a JS `RangeError` at the call boundary. Harmless on the native
CLI (Go's stacks grow), but a bad experience in the browser. Already caught
and displayed as an inline error by sub-project 4's `app.js`, so not a
crash — but a live, publicly-reachable playground driven by arbitrary
pasted specs is exactly the surface most likely to hit it, so this
sub-project fixes it at the source rather than carrying it forward again.

**Fix:** `internal/ir`'s `buildNode` (or equivalent recursive entry points)
takes a depth counter, incremented on each recursive descent into a nested
schema. At a fixed limit — **500** — building stops and returns a plain
`error` (e.g. `"schema nesting exceeds maximum depth of 500"`) instead of
recursing further. 500 is comfortably past any real-world spec (the
benchmark's synthetic large spec and every fixture in this repo are well
under 50 levels) and comfortably short of a JS stack overflow at the WASM
boundary (reproduced failure was ~3000 levels; native Go has no trouble at
that depth either, so the guard exists purely to give a clean error before
the browser's stack limit, not because Go itself struggles).

This is the only change outside `web/` in this sub-project. It's isolated
to `internal/ir`, doesn't touch `internal/gen/ts`, `internal/gen/zod`, or
`internal/spec`, and both the CLI and the WASM build get the clean error
automatically since they share `pkg/engine`.

## Frontend architecture

Vanilla JS, no framework, no build step beyond the existing
`./web/build.sh` (which only compiles the Go WASM module — it never touches
the frontend files). Consistent with sub-project 4 and this project's
overall zero-dependency ethos for the engine; a framework buys nothing for
a page this size.

**Styling:** Tailwind CSS via the Play CDN script
(`https://cdn.tailwindcss.com`) — no npm install, no build step, matches
the "clone and open a static server" workflow already documented in
`README.md`. Dark mode via Tailwind's class strategy: a `dark` class on
`<html>`, toggled by a button, defaulting to `prefers-color-scheme` and
persisted to `localStorage`.

**Icons:** Lucide, via its CDN UMD build, for the theme toggle (sun/moon),
a "copy to clipboard" button on the output pane, and small icons on the
example-spec buttons/file tabs. Purely decorative/functional UI chrome —
no icon is load-bearing for understanding the tool.

**Files in `web/`:**

```
web/index.html      Markup shell: split-screen layout, textarea, controls,
                     tab bar, output pane. Loads Tailwind CDN, Lucide CDN,
                     wasm_exec.js, then the app scripts below.
web/app.js          Rewritten: WASM loading (reused from sub-project 4),
                     debounced live generation, tab state, theme toggle,
                     copy-to-clipboard, wiring example buttons.
web/examples.js      New: 4 example specs as exported string constants,
                     sourced from existing pkg/engine/testdata fixtures.
web/highlight.js     New: minimal regex-based TS/Zod syntax highlighter,
                     returns HTML with <span> tags for keywords, strings,
                     comments, types.
web/smoketest.mjs    Unchanged.
web/build.sh         Unchanged.
web/test.sh          Unchanged.
```

## Layout and interaction

- **Left pane:** a row of example buttons (Petstore, Swagger 2.0, Circular
  refs, Composition) above a plain `<textarea>` for the spec (no
  JSON/YAML syntax highlighting on the input side — out of scope, this
  tool's job is the generated output). Below the textarea: a target
  toggle (TypeScript / Zod) and a modular/monolithic toggle.
- **Right pane:** a row of file-name tabs (populated from the result's
  `files` map; monolithic output shows a single implicit `index.ts` tab)
  above a syntax-highlighted `<pre>` for the active tab's content, with a
  copy-to-clipboard icon button.
- **Generation is live:** input, target, or modular changes debounce
  ~300ms then call `openapi2codeGenerate` automatically. No explicit
  "Generate" button. Clicking an example button populates the textarea and
  triggers generation immediately (no debounce wait).
- **Errors** (parse errors, the new depth-guard error, or any other
  engine error) replace the output pane's content with an inline error
  message, matching sub-project 4's `try/catch` pattern around the WASM
  call. The tab bar/textarea stay interactive throughout — an error never
  blocks further edits.

## Testing

- **`internal/ir` depth guard:** a new Go unit test in
  `internal/ir` builds a synthetic schema nested past 500 levels (a helper
  that generates nested `object`/`properties` programmatically, not a
  500-level fixture file) and asserts a clean error, plus a table-driven
  check that a schema just under the limit still builds successfully.
- **Frontend:** same manual-verification pattern as sub-project 4 — no
  new browser-automation harness. `web/smoketest.mjs` continues to cover
  the WASM contract itself; the page's own UI (tabs, theme toggle, example
  buttons, debounced generation) is verified by hand in a real browser
  before this sub-project is considered done, the same way sub-project 4's
  test page was.

## Explicitly out of scope

- Actual deployment/hosting/domain registration for `OpenAPI2Code.com`.
- Viewing multiple targets simultaneously (target stays a single toggle).
- Any browser-automation test runner (Playwright, etc.).
- Swift/Kotlin/Dart generators — unaffected, unblocked.
- Any change to `internal/gen/ts`, `internal/gen/zod`, `internal/spec`, or
  the WASM JS contract itself.
