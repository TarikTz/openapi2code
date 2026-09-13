# Benchmark: openapi2code vs. openapi-typescript

A speed, footprint, and output comparison between this project's CLI and
[openapi-typescript](https://openapi-ts.dev/) (v7), the most widely used
OpenAPI → TypeScript generator in the Node ecosystem.

**Read the caveats before the numbers.** This is not an apples-to-apples
feature comparison — see [Feature scope](#feature-scope-not-just-speed)
below. The speed numbers are real and reproducible, but they compare a
narrower tool (schema-only TS interfaces) against a broader one (full
`paths`/`operations`/`components` typing for a type-safe fetch client).

## Environment

- `go version go1.26.0 darwin/arm64` (module targets `go 1.25`)
- `node v24.11.1`
- `openapi-typescript v7.13.0`
- Darwin 25.4.0, arm64 (Apple Silicon)
- Benchmarked with [hyperfine](https://github.com/sharkdp/hyperfine) 1.20.0, `--shell=none`, 5 warmup runs, ≥30 measured runs (fewer for the Stripe multi-target comparison, see below)

## Headline result: the cycle-detection fix, validated on Stripe's real spec

Until this refresh, `openapi2code`'s cycle-detection pass (`internal/ir`'s
`markCycles`) was **exponential** on densely, genuinely cyclic real-world
specs. It was never caught by the synthetic benchmarks below because they
aren't cyclic enough — Stripe's real OpenAPI spec is: it uses `anyOf`
"expandable field" unions pervasively (e.g. an `invoice.customer` field
typed as `string | customer | deleted_customer`, and self-referential
chains like `invoice.latest_revision: string | invoice`), producing a
components graph with 1454 schemas and heavy mutual cross-referencing.

Measured before this fix: **400+ seconds** for a single `openapi2code`
run against Stripe's spec. A same-order-of-magnitude but far less
interconnected real spec — GitHub's 971/975-schema REST API description —
finished in under a second on the old code, which is exactly the
signature of an exponential-not-linear algorithm: it isn't schema count
that mattered, it was cyclic density.

The fix rewrites cycle detection as a single-pass strongly-connected-
components algorithm. Measured after the fix, on the same real Stripe
spec, same machine:

| | Before | After | 
|---|---|---|
| `openapi2code pull stripe.json --target ts` | 400+ s | **74.5 ms** |

That's roughly a **5,000×+** reduction, taking Stripe from "effectively
unusable" to faster than `openapi-typescript` on the same input (which
takes ~919 ms). The fix isn't TS-specific — it lives in the shared
IR-construction step every target goes through, so all five generators
(`ts`, `zod`, `swift`, `kotlin`, `dart`) benefit equally; see
[All targets on Stripe](#all-openapi2code-targets-on-stripes-spec-same-ir-fix-benefits-all-of-them)
below.

## Real-world specs used

| Spec | Schemas | File size | Notes |
|---|---:|---:|---|
| `specs/petstore-v3.json` | 6 | 17 KB | Official live Swagger Petstore v3 demo spec (`petstore3.swagger.io/api/v3/openapi.json`) — small sanity baseline |
| `specs/github.json` | 975 | 12.4 MB | GitHub's real REST API description (`github/rest-api-description`, `api.github.com.json`) — large, but only lightly cross-referenced |
| `specs/stripe.json` | 1454 | 7.7 MB | Stripe's real OpenAPI spec (`stripe/openapi`, `openapi/spec3.json`) — large *and* densely cyclic via `anyOf` expandable-field unions; the spec that used to take 400+ s |
| `specs/azure-translator-v2.json` | 15 definitions | 64 KB | Azure Cognitive Services Translator Text API — real Swagger **2.0** spec, kept for version diversity (bonus, not part of the core comparison below) |

`github.json` and `stripe.json` are excluded from git (see
`benchmarks/.gitignore`) because of their size; `petstore-v3.json` and
`azure-translator-v2.json` are small enough to commit, same as
`large-synthetic.json`. See "Reproducing this benchmark" below for the
download commands.

## Speed: real-world specs, TS target vs. openapi-typescript

### Petstore v3 (6 schemas, 17 KB) — `results-petstore-v3.{md,json}`

| Tool | Mean | Range |
|---|---|---|
| `openapi2code` | **4.0 ms** ± 3.4 ms | 2.8 – 62.5 ms |
| `openapi-typescript` | 242.7 ms ± 16.5 ms | 230.8 – 296.7 ms |

**openapi2code was ~61× faster.**

### GitHub REST API description (975 schemas, 12.4 MB, sparse) — `results-github.{md,json}`

| Tool | Mean | Range |
|---|---|---|
| `openapi2code` | **130.8 ms** ± 4.1 ms | 126.3 – 141.7 ms |
| `openapi-typescript` | 1.4215 s ± 0.0737 s | 1.3644 – 1.6405 s |

**openapi2code was ~11× faster.** (openapi-typescript also emits one
non-fatal warning on this spec — an invalid discriminator mapping on
`content-directory` — unrelated to either tool's timing.)

### Stripe (1454 schemas, 7.7 MB, densely cyclic) — `results-stripe.{md,json}`

| Tool | Mean | Range |
|---|---|---|
| `openapi2code` | **74.5 ms** ± 2.5 ms | 71.3 – 85.0 ms |
| `openapi-typescript` | 919.1 ms ± 26.5 ms | 894.8 – 1024.2 ms |

**openapi2code was ~12× faster** — and, per the headline section above,
this same run took 400+ seconds before today's cycle-detection fix.

### All `openapi2code` targets on Stripe's spec (same IR, fix benefits all of them) — `results-stripe-alltargets.{md,json}`

Lighter sampling (2 warmups, 5 measured runs each — the point here is
order of magnitude, not a precise mean) confirms every target shares the
same fast IR-construction/cycle-detection path:

| Target | Mean |
|---|---|
| `zod` | 141.5 ms ± 17.2 ms |
| `ts` | 144.4 ms ± 19.1 ms |
| `kotlin` | 204.7 ms ± 33.3 ms |
| `dart` | 207.0 ms ± 23.9 ms |
| `swift` | 228.2 ms ± 26.2 ms |

The native-mobile targets run somewhat slower than `ts`/`zod` mainly
because they emit more files per schema (e.g. a separate file per string
enum) — 2632 files for Stripe vs. 1455 for `ts`/`zod` — so more of the
difference is filesystem I/O than generation logic. All five are firmly
in the same "well under a second" order of magnitude that used to be
"6+ minutes" for the cyclic case.

## Speed: synthetic specs (retained from the original benchmark run)

These predate the real-world specs above but are kept for continuity —
they show the same qualitative gap on non-cyclic inputs, at much smaller
scale.

### Small spec (`petstore.yaml`, 4 schemas, 60 lines) — `results-monolithic.{md,json}`

| Tool | Mean | Range |
|---|---|---|
| `openapi2code` | **4.2 ms** ± 1.5 ms | 3.0 – 21.8 ms |
| `openapi-typescript` | 273.7 ms ± 68.4 ms | 229.1 – 623.5 ms |

**openapi2code was ~65× faster.**

### Large synthetic spec (`large-synthetic.json`, 300 schemas, 128 KB, cross-referencing) — `results-large.{md,json}`

| Tool | Mean | Range |
|---|---|---|
| `openapi2code` | **14.9 ms** ± 3.3 ms | 12.0 – 31.6 ms |
| `openapi-typescript` | 353.6 ms ± 31.4 ms | 293.6 – 407.3 ms |

**openapi2code was ~24× faster.**

The gap narrows as schema count grows (65× → 24×) on the synthetic specs
because `openapi2code`'s own generation time scales with input size while
`openapi-typescript`'s per-process overhead is largely fixed. The
real-world numbers above tell a fuller story: on GitHub and Stripe (both
~1,000+ schemas but very different in cross-reference density) the gap
holds steady at ~11–12×, because both real specs are big enough that
Node's fixed startup cost stops dominating for either tool — and,
critically, `openapi2code`'s time no longer blows up on the densely cyclic
one now that the cycle-detection bug is fixed.

## Footprint

| | Size |
|---|---|
| `openapi2code` binary (this repo, `go build`) | 9.5 MB, zero runtime dependencies |
| `openapi-typescript`'s `node_modules/` | 43 MB, requires a Node.js runtime |

## Why the gap

`openapi2code` is a compiled, single-binary Go CLI with no runtime to
start and no third-party dependency graph to load — this is a deliberate
product goal (see [`PRD.md`](../PRD.md)'s "Zero-Dependency Footprint").
`openapi-typescript` runs on Node, loads its own module graph, and does
substantially more work per run (see below) — the comparison mostly
measures "compiled Go binary" vs. "Node.js CLI tool," which is expected
and not a knock against openapi-typescript's design.

## Feature scope: not just speed

This is the important caveat. **The two tools do not do the same job
today:**

| | `openapi2code` (this repo) | `openapi-typescript` |
|---|---|---|
| OpenAPI 3.0.x | ✅ | ✅ |
| OpenAPI 3.1 | ❌ (explicitly rejected with a clear error) | ✅ |
| Swagger 2.0 | ✅ | ❌ |
| `components.schemas` → TS types | ✅ | ✅ |
| `paths`/`operations` → TS types | ❌ (not implemented) | ✅ |
| Type-safe fetch client codegen | ❌ (not implemented) | ✅ (via companion `openapi-fetch`) |
| Output style | flat top-level `export interface Foo { ... }` per model, modular (barrel `index.ts`) or single-file | everything nested under one `components.schemas.Foo` object |
| Zod / Swift / Kotlin / Dart targets | ✅ implemented (`--target zod\|swift\|kotlin\|dart`, or comma-separated for several at once) | ❌ (TS only) |
| `additionalProperties` (map types) | ✅ (`Record<string, T>` in TS, `z.record()` in Zod, `[String: T]` in Swift, `Map<String, T>` in Kotlin and Dart) | ✅ |
| Runtime / install | single static binary, no install | `npm install` + Node.js |

`openapi2code` today only generates types for named schemas
(`components.schemas` / `definitions`) — no request/response/operation
typing, because this repo hasn't built that yet (see
[`PRD.md`](../PRD.md) and [`docs/superpowers/specs/`](../docs/superpowers/specs/)
for the roadmap). `openapi-typescript` generates a complete `paths`
type plus `components`, which is what a real type-safe HTTP client needs.
For a spec with real `paths`, `openapi-typescript`'s output is doing
meaningfully more work than `openapi2code`'s — the speed gap would look
different (smaller, in openapi-typescript's favor on a per-feature basis)
if `openapi2code` generated operation types too. That gap is real and
unchanged by this refresh.

One more honest caveat surfaced while spot-checking real-spec output:
`oneOf`/`anyOf` support for the mobile targets (Swift/Kotlin/Dart) is
still a deferred follow-up (tracked in
[`docs/superpowers/specs/2026-09-10-future-considerations.md`](../docs/superpowers/specs/2026-09-10-future-considerations.md)).
On Stripe's spec, which uses `anyOf` pervasively for "expandable field"
unions, the `ts`/`zod` targets render them correctly as unions (including
the self-referential `invoice.latest_revision: string | invoice` case,
via `z.lazy()` for Zod), while Swift/Kotlin/Dart currently skip those
fields with an explanatory comment
(`// customer: skipped — unsupported schema shape (oneOf/anyOf, ...)`)
rather than emitting something incorrect. That's expected, documented
behavior, not a regression from this refresh.

**In short: today's honest framing is "openapi2code generates schema
types dramatically faster across every target, but openapi-typescript
generates strictly more (`paths`/operations/client code), and
openapi2code's mobile targets don't yet handle `oneOf`/`anyOf`."** As
`openapi2code` grows into the full PRD scope, this comparison is worth
re-running again.

## Output shape (same input schema, both tools)

`openapi2code`:

```ts
export interface Pet {
  category?: Category;
  id?: number;
  name: string;
  photoUrls: string[];
  status?: "available" | "pending" | "sold";
  tags?: Tag[];
}
```

`openapi-typescript`:

```ts
export interface components {
    schemas: {
        Pet: {
            id?: number;
            name: string;
            category?: components["schemas"]["Category"];
            tags?: components["schemas"]["Tag"][];
            photoUrls: string[];
            /** @enum {string} */
            status?: "available" | "pending" | "sold";
        };
        // ...
    };
}
```

`openapi2code` emits flat, directly-importable named types;
`openapi-typescript` nests everything under a single `components` object
(so consumers write `components["schemas"]["Pet"]`, or generate their own
aliases) — a deliberate design choice on their part that keeps every
generated symbol under one root to avoid name collisions across a whole
API surface (including `paths` and `operations`, which `openapi2code`
doesn't generate).

## Reproducing this benchmark

```bash
cd benchmarks
npm install                 # installs openapi-typescript locally
go build -o openapi2code ../cmd/openapi2code

# --- synthetic specs (already committed) ---
hyperfine --shell=none --warmup 5 --min-runs 30 \
  -n "openapi2code" "./openapi2code pull ./petstore.yaml --out-file /tmp/out-o2c.ts" \
  -n "openapi-typescript" "node_modules/.bin/openapi-typescript ./petstore.yaml -o /tmp/out-ots.ts"

# Swap petstore.yaml for large-synthetic.json (regenerate with
# `python3 gen-large-spec.py`) to see how the gap changes with input size.

# --- real-world specs ---
mkdir -p specs
curl -sL -o specs/petstore-v3.json https://petstore3.swagger.io/api/v3/openapi.json
curl -sL -o specs/github.json https://raw.githubusercontent.com/github/rest-api-description/main/descriptions/api.github.com/api.github.com.json
curl -sL -o specs/stripe.json https://raw.githubusercontent.com/stripe/openapi/master/openapi/spec3.json
curl -sL -o specs/azure-translator-v2.json https://raw.githubusercontent.com/Azure/azure-rest-api-specs/main/specification/cognitiveservices/data-plane/TranslatorText/stable/v3.0/TranslatorText.json

hyperfine --shell=none --warmup 5 --min-runs 30 \
  -n "openapi2code" "./openapi2code pull ./specs/petstore-v3.json --target ts --out-file /tmp/petstore-o2c.ts" \
  -n "openapi-typescript" "node_modules/.bin/openapi-typescript ./specs/petstore-v3.json -o /tmp/petstore-ots.ts"

hyperfine --shell=none --warmup 5 --min-runs 30 \
  -n "openapi2code" "./openapi2code pull ./specs/github.json --target ts --out-file /tmp/github-o2c.ts" \
  -n "openapi-typescript" "node_modules/.bin/openapi-typescript ./specs/github.json -o /tmp/github-ots.ts"

# Stripe: fewer runs needed once you've confirmed the fix -- the whole
# point is the order-of-magnitude change, not a precise mean. (This used
# to require --min-runs 3 to keep the pre-fix 400+ s/run case tractable;
# post-fix it's fast enough that --min-runs 30 finishes in well under a
# minute, but a handful of runs is enough to make the point.)
hyperfine --shell=none --warmup 5 --min-runs 30 \
  -n "openapi2code" "./openapi2code pull ./specs/stripe.json --target ts --out-file /tmp/stripe-o2c.ts" \
  -n "openapi-typescript" "node_modules/.bin/openapi-typescript ./specs/stripe.json -o /tmp/stripe-ots.ts"

# openapi2code across all 5 targets on Stripe, to confirm the fix is
# target-agnostic (shared IR/cycle-detection step):
hyperfine --shell=none --warmup 2 --min-runs 5 \
  -n "ts"     "./openapi2code pull ./specs/stripe.json --target ts     --output /tmp/stripe-ts" \
  -n "zod"    "./openapi2code pull ./specs/stripe.json --target zod    --output /tmp/stripe-zod" \
  -n "swift"  "./openapi2code pull ./specs/stripe.json --target swift  --output /tmp/stripe-swift" \
  -n "kotlin" "./openapi2code pull ./specs/stripe.json --target kotlin --output /tmp/stripe-kotlin" \
  -n "dart"   "./openapi2code pull ./specs/stripe.json --target dart   --output /tmp/stripe-dart"
```

Raw hyperfine output (markdown + JSON) for every run above is committed
alongside this file: `results-monolithic.{md,json}`,
`results-large.{md,json}` (synthetic); `results-petstore-v3.{md,json}`,
`results-github.{md,json}`, `results-stripe.{md,json}`,
`results-stripe-alltargets.{md,json}` (real-world).
