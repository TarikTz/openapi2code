# Design: Core Engine + IR + TypeScript Generator

**Date:** 2026-09-10
**Status:** Approved for planning
**Sub-project:** 1 of 5 (see Decomposition below)

## Decomposition context

The full OpenAPI2Code product (per `PRD.md`) bundles several largely independent subsystems: an OpenAPI parser/IR, five separate code generators (TS, Zod, Swift, Kotlin, Dart), a CLI, a WASM build, and a web playground UI. Building all of it as one spec/plan would be too large to implement or review coherently, so the project is split into sequential sub-projects:

1. **Core engine + IR + TypeScript generator** (this document)
2. CLI (thin wrapper around the core engine: `pull` command, source input handling, flags)
3. Additional generators (Zod, then Swift/Kotlin/Dart), each an independent slice once the IR exists
4. WASM build (compile the core engine to WASM, define the JS-facing API surface)
5. Web playground UI (OpenAPI2Code.com, consuming the WASM build)

Sub-projects 2–5 depend on 1. Each gets its own design → spec → plan → implementation cycle.

## Goal

Build a Go module that parses OpenAPI/Swagger v2 and v3 specs (JSON or YAML) into an intermediate representation (IR), and generates TypeScript interface declarations from that IR, in both modular (multi-file + barrel `index.ts`) and monolithic (single-file) output modes.

No CLI, no file/URL fetching, no WASM, no other generators — those are later sub-projects.

## Module

Go module path: `github.com/tarikomercehajic/openapi2code`.

## Architecture & data flow

```
[]byte (JSON or YAML)
    -> internal/spec:   unmarshal into raw OpenAPI v2/v3 structs, detect version,
                         resolve internal $refs
    -> internal/ir:     walk resolved spec, build IR (Model, Field, Union, Enum,
                         cycle-annotated graph)
    -> internal/gen/ts: walk IR, emit TS interface declarations
    -> pkg/engine:      write output as modular files (+ barrel index.ts)
                         or one monolithic file
```

`pkg/engine` is the only public surface:

```go
func Parse(data []byte) (*ir.Document, error)
func GenerateTS(doc *ir.Document, opts TSOptions) (Output, error)
```

CLI and WASM wrappers (sub-projects 2 and 4) both call these two functions only — no other coupling to the outside world. This satisfies the "one core engine, two frontends" constraint in `Claude.md`: the core never does file I/O or network access, so it compiles to WASM unchanged.

## Components

### `internal/spec`

- Version-sniffs the document (`swagger: "2.0"` vs `openapi: "3.x"`).
- Unmarshals JSON directly via `encoding/json`; YAML is converted via `gopkg.in/yaml.v3` (the one justified third-party dependency — no YAML parser exists in the Go standard library, and the PRD explicitly requires YAML input support).
- Resolves internal `$ref`s only: `#/components/schemas/*` (v3) and `#/definitions/*` (v2). External file/URL refs are out of scope.
- Lenient parsing: unknown or unsupported fields are ignored, not errors. This matches real-world specs, which are often imperfect.

### `internal/ir`

- Schema-shaped nodes only (paths/operations are out of scope for this sub-project — the product's near-term outputs are data models, not API clients).
- Node kinds: Object, Array, Primitive, Enum, Union, Ref.
- Each node has a stable identity so cycle detection can mark self-referential structures (needed correctness-wise even though TS doesn't need special-casing for cycles — Zod will in sub-project 3, and the IR must carry that information now so it isn't lost).
- Composition keywords, fully supported:
  - `allOf` → merged-fields intersection (the common inheritance pattern).
  - `oneOf` / `anyOf` → Union node.
- Nullable (`nullable: true`) and optional (`required[]` membership) are tracked as distinct properties on a Field — nullability is not collapsed into optionality.

### `internal/gen/ts`

- Pure function: IR in, TS source text out. No knowledge of raw spec types, only IR.
- Enums render as string-literal unions (e.g. `type Status = "active" | "inactive";`), not TS `enum` declarations.
- Nullable fields render as `field: T | null`; optional fields render as `field?: T`; a field can be both.
- Circular references need no special handling for TS — interfaces reference each other by name natively.
- Union nodes render as TS union types (`type X = A | B;`).
- allOf intersections render as TS interface extension where possible, falling back to an intersection type (`A & B`) for non-object members.

### `pkg/engine`

- Orchestrates `internal/spec` → `internal/ir` → `internal/gen/ts`.
- Handles output file layout:
  - **Modular mode:** one file per generated type/model plus a barrel `index.ts` re-exporting everything.
  - **Monolithic mode:** all declarations concatenated into one file (`--out-file` equivalent, though the actual CLI flag is wired up in sub-project 2 — this sub-project just needs the engine to support both modes).
- Sanitizes/derives valid TS identifiers from schema names (handling characters not valid in TS identifiers).

## Error handling

Errors are returned, never panicked, and wrapped with the JSON Pointer path to the offending location in the spec (e.g. `#/components/schemas/Pet/properties/tag`), so failures are traceable back to a specific spec location. Hard errors are reserved for cases that make correct generation genuinely impossible:

- Malformed JSON/YAML that fails to parse at all.
- An unresolvable `$ref` (points to a schema that doesn't exist).
- A schema construct that cannot be modeled at all (rare; lenient parsing absorbs most oddities upstream).

## Testing

- **Golden-file tests:** fixture OpenAPI specs live under `testdata/`, including:
  - A Petstore-style spec (broad, realistic coverage).
  - A spec exercising `allOf`/`oneOf`/`anyOf`.
  - A spec with circular schema references.
  - A spec covering nullable fields and enums.

  Each fixture is run through `pkg/engine` and the generated TypeScript is diffed against committed golden `.ts` files. This catches regressions in exact output formatting, not just structural correctness.

- **Unit tests:** `internal/spec` and `internal/ir` get table-driven unit tests for parsing and IR-building edge cases independently of codegen output (e.g. ref resolution, cycle detection, allOf merge semantics).

## Explicitly out of scope for this sub-project

- CLI, file/URL fetching (sub-project 2).
- Zod, Swift, Kotlin, Dart generators (sub-project 3).
- WASM compilation (sub-project 4).
- Web playground UI (sub-project 5).
- Paths/operations parsing (not currently planned in a specific sub-project; revisit if/when the product needs API-client generation rather than data-model generation).
- External (file/URL) `$ref` resolution.
