# Design: CLI

**Date:** 2026-09-10
**Status:** Approved for planning
**Sub-project:** 2 of 5 (see `docs/superpowers/specs/2026-09-10-core-engine-ts-generator-design.md` for the full decomposition)

## Goal

Build a CLI binary, `openapi2code`, that wraps the existing `pkg/engine` (sub-project 1) to let developers generate TypeScript from a local file, a remote URL, or stdin, matching the `pull` command shape sketched in `PRD.md`'s User Flow section:

```
openapi2code pull [https://dev.api.com/swagger.json] --target ts,zod --output ./src/types
```

Only `--target ts` is implemented in this sub-project — Zod/Swift/Kotlin/Dart generators are sub-project 3 and don't exist yet. `--target` accepts other values syntactically (for forward compatibility with example commands like the one above) but errors clearly that they're not yet implemented.

## Command shape

```
openapi2code pull <source> --target ts [--output <dir> | --out-file <path>]
```

- `<source>` (positional, required): a local file path, an `http://`/`https://` URL, or `-` to read from stdin.
- `--target` (optional, defaults to `ts`): the only currently-accepted value is `ts`. Any other value produces a clear "target %q not yet implemented" error rather than a generic flag-parsing error, so example commands from the PRD/docs fail informatively once other targets exist as syntax but not implementation.
- `--output <dir>` / `--out-file <path>`: mutually exclusive, exactly one required. `--output` maps to `engine.TSOptions{Modular: true}` (one file per model + barrel `index.ts`, written into the directory). `--out-file` maps to `engine.TSOptions{Modular: false}` (single file written at that exact path).

## Architecture

```
cmd/openapi2code/main.go  →  internal/cli.Run(args []string) int
                                  ├─ parse flags (stdlib "flag")
                                  ├─ resolveSource(source string) ([]byte, error)
                                  │    ├─ http(s):// prefix → net/http GET
                                  │    ├─ "-"              → read os.Stdin
                                  │    └─ otherwise        → os.ReadFile
                                  ├─ pkg/engine.Parse(data)
                                  ├─ pkg/engine.GenerateTS(doc, opts)
                                  └─ writeOutput(out engine.Output, dest) error
                                       ├─ --out-file: os.WriteFile (MkdirAll parent)
                                       └─ --output:   os.MkdirAll dir, write each out.Files[k] to <dir>/<k>
```

`internal/cli` is the only new package that performs file or network I/O. It depends on `pkg/engine`; `pkg/engine` has no knowledge of `internal/cli` and stays exactly as I/O-free as it is today — this is the concrete instantiation of the "injectable source" note in `Claude.md`: the CLI is one implementation of source-fetching, the future WASM build (sub-project 4) will be a separate, browser-side one, and neither depends on the other.

`cmd/openapi2code/main.go` is a thin entrypoint: it calls `internal/cli.Run(os.Args[1:])`, which returns an int exit code, and calls `os.Exit` with it. All actual logic lives in `internal/cli`, which is unit-testable without spawning a real binary.

## Source resolution

`resolveSource` dispatches purely on the string shape of `<source>`:
- Prefix `http://` or `https://` → `net/http` GET with a fixed 30-second timeout (via a client constructed with that timeout, not the zero-timeout `http.DefaultClient`). A non-2xx response is an error naming the status code.
- Exactly `-` → read all of `os.Stdin`.
- Anything else → treated as a local file path, `os.ReadFile`.

No flag is needed to select the mode — the shape of the argument is unambiguous or is not, in which case Go's `os.ReadFile` will natively fail with an descriptive error.

## Output writing

- `--out-file <path>`: `os.MkdirAll` on the parent directory, then `os.WriteFile` the single generated file (`out.Files["index.ts"]`) at `<path>` (0644).
- `--output <dir>`: `os.MkdirAll` the directory (0755), then for every key/value in `out.Files`, `os.WriteFile` at `<dir>/<key>` (0644).

Existing files at the exact target paths are overwritten; anything else already in the directory (or, for `--out-file`, anything else already alongside the target path) is left untouched. This matches the PRD's stated dev-loop use case — rerunning the command refreshes generated contracts without requiring an empty target directory or a `--force` flag.

## Errors & exit codes

Every failure (flag validation, source resolution, parse, generation, or write) results in a single line on stderr (`err.Error()`, no Go-panic-style stack trace) and exit code 1. Success exits 0. Errors from `pkg/engine.Parse`/`GenerateTS` already carry a traceable path (schema name, field name, etc.) from sub-project 1's error-wrapping convention, so the CLI does not need to add its own wrapping on top — it just surfaces the message as-is.

## Testing

- `internal/cli`: unit tests for flag parsing/validation (missing source; both or neither of `--output`/`--out-file`; unsupported `--target` value produces the specific "not yet implemented" message), `resolveSource` (a real local file, stdin via a piped `*bytes.Reader` substituted for `os.Stdin` in the test, and an HTTP fixture via `httptest.Server`), and output writing (an `--output` run into a directory that already has an unrelated file confirms that file survives; a rerun into a directory with a stale generated file confirms it's overwritten).
- `cmd/openapi2code`: one integration test that builds the real binary (`go build` into a temp location, or `go run` the package) and executes it via `os/exec` against a real fixture file (e.g. reusing `pkg/engine/testdata/petstore.yaml`), asserting the process exits 0 and the expected output file(s) exist with expected content — confirming the whole thing is actually wired together end-to-end, not just each piece in isolation.

## Explicitly out of scope for this sub-project

- Any `--target` other than `ts` actually generating code (sub-project 3).
- A `--help`/usage-only invocation beyond whatever the stdlib `flag` package provides by default (no custom help formatting, no subcommand scaffolding beyond `pull` — there is only one subcommand).
- WASM build, web playground (sub-projects 4 and 5).
- Retry/backoff, custom TLS config, or auth headers for the URL-fetch path — a plain unauthenticated GET with a fixed timeout is all this sub-project provides.
