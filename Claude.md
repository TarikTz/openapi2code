# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project status

Every sub-project on the original roadmap is complete: 1 (core engine + TypeScript generator, `pkg/engine`), 2 (CLI, `cmd/openapi2code` + `internal/cli`), 3a (Zod generator, `--target zod`), 4 (WASM build, `internal/wasmapi` + `cmd/wasm`), 5 (the browser playground, `web/`), and 6 (Swift/Kotlin/Dart generators, `internal/gen/swift`, `internal/gen/kotlin`, `internal/gen/dart`). `additionalProperties` (dictionary/map-shaped fields, `ir.KindMap`) is supported across all five targets, and `--target` accepts a comma-separated list to generate several in one run, both as of the post-roadmap hardening pass. oneOf/anyOf support for the mobile targets, `additionalProperties: true` (no value schema), and a schema mixing fixed properties with `additionalProperties` remain as deferred follow-ups. Build/test: `go build ./...`, `go vet ./...`, `go test ./...`, plus `./web/test.sh` for the WASM/playground pieces `go test` can't reach.

`PRD.md` and `docs/` (design specs, implementation plans, deferred-scope notes) are gitignored local planning artifacts, not part of the public repo — present on this machine but not in a fresh clone. Read `PRD.md` in full before starting implementation if it's present; it's the source of truth for scope and behavior.

## Product summary

**OpenAPI2Code** is a zero-dependency, multi-target code generation engine written in Go, shipped two ways:

1. **CLI binary** — ingests an OpenAPI/Swagger (v2/v3) spec from a local file, raw string, or remote URL, and generates code for one or more targets.
2. **WASM build** — the same Go engine compiled to WebAssembly, powering a browser-based playground (`OpenAPI2Code.com`) that does the conversion client-side with no server round-trip.

Generation targets: TypeScript interfaces, Zod runtime validation schemas, and native mobile models (Swift `Codable` structs, Kotlin `data class`, Dart classes).

## Architecture constraints (apply from the first commit)

- **One core engine, two frontends.** The parsing/codegen logic in the Go core must be platform-agnostic — no dependency that only works in a CLI process (e.g. filesystem/network assumptions baked into core logic) or only in a browser. The CLI and the WASM build are thin wrappers around the same engine code; source-fetching (file/string/URL) should be injected/abstracted so the WASM build can supply browser-appropriate fetch behavior.
- **Zero-dependency footprint is a stated goal.** Prefer the Go standard library for parsing and codegen; justify any third-party dependency against the "single static binary" / WASM-size goals before adding it.
- **Multi-target codegen is structurally symmetric.** Each target (TS, Zod, Swift, Kotlin, Dart) should plug into the same intermediate representation of the parsed spec rather than each re-parsing OpenAPI documents independently — expect a shared IR/model layer between the parser and the per-target generators.
- **Two TS output modes**: modular multi-file output with a barrel `index.ts`, and monolithic single-file output via `--out-file`. Both must stay in sync when the TS generator changes.
- **CLI shape**: `openapi2code pull <source> --target <ts,zod,swift,kotlin,dart> --output <dir>` (comma-separated multi-target in one invocation).
