# Future Considerations

Not a design spec — a running note of scope ideas that came up during
work on sub-projects 1–2 but were deliberately deferred, so they aren't
lost and can be brainstormed properly if/when they become worth doing.
Check this file at the start of a new sub-project's brainstorming in case
something here has become relevant.

## OpenAPI 3.1 support

**Status:** explicitly out of scope, not just unimplemented. `DetectVersion`
rejects `3.1+` with a clear error rather than trying to parse it (see the
ruling in the finished CLI plan's ledger and `internal/spec/version.go`).

**Why it's deferred:** `internal/ir`'s builder was designed against 3.0.x
schema idioms. 3.1 changed how nullability is expressed (`type: [T, "null"]`
instead of `nullable: true`) and adopted more of the JSON Schema 2020-12
dialect — supporting it properly needs real design work in `internal/ir`,
not a quick patch.

**Why it's worth revisiting:** the PRD says "OpenAPI/Swagger (v2 & v3)"
generically, and 3.1 is the current OpenAPI version — some real-world
specs are 3.1 already and get a clear-but-unhelpful rejection today.

## `paths`/`operations` typing (type-safe HTTP client generation)

**Status:** out of scope for the whole product as currently defined, not
just deferred. `PRD.md` describes generating types and validation
schemas from data models (`components.schemas`) — nothing in it asks for
request/response/operation types or a generated fetch client.

**Why it came up:** a benchmark against
[openapi-typescript](https://openapi-ts.dev/) (see `../../../benchmarks/`)
showed it generates `paths`/`operations` typing (for its companion
`openapi-fetch` client) in addition to schema types, which
`openapi2code` doesn't do. openapi2code was ~24–65× faster at the
narrower thing it does, but the comparison isn't apples-to-apples because
of this gap.

**Why it's deferred:** it's a different product surface (an HTTP client
generator) than what the PRD scopes today (a data-model/type generator).
Adding it would be a genuine scope expansion, not "finishing the
existing plan" — it deserves its own explicit product decision and a
brainstorming pass, not a bolt-on.

**If this is ever picked up:** it would need its own IR extension (an
operation/parameter/request-body/response model, likely a second IR tree
parallel to the existing schema `ir.Document`, since operations reference
schemas but aren't schemas themselves), and should probably be
brainstormed as its own numbered sub-project after the currently-planned
ones (additional generators, WASM, web playground) rather than inserted
ahead of them — see the decision made in-conversation on 2026-09-10 to
finish the planned roadmap first.

## `internal/ir`'s cycle detection was exponential on densely (not just diamond-shaped) cyclic real specs

**Status:** fixed. Found by an external real-world usability evaluation
run against real, large public API specs (GitHub's and Stripe's official
OpenAPI descriptions), not by a code review or this project's own test
suite. `markCycles` used to run one recursive reachability search per
model ("does this model's own type reach a $ref back to itself"),
memoizing a ref's result only when the search wasn't truncated by
crossing a cycle already on the current path — a real but different
cycle elsewhere in the graph. On a densely, genuinely cyclic real spec
(as opposed to a merely diamond-shaped acyclic one, which that
memoization already handled), a node reachable via many diamond paths
that also crosses such a cycle can never be cached, so its entire
subtree gets fully re-explored on every single path that reaches it.
Measured on Stripe's real 1454-schema spec (812 `anyOf` "expandable
field" sites plus pervasive mutual cross-references): 400+ seconds for
EVERY target (`ts` 402s, `zod` 411s, `swift` 416s, `kotlin` 414s, `dart`
413s — a tight band confirming the cost is paid once in the shared IR
build, not per generator), against well under a second for GitHub's
real, larger (971-schema) but far less interconnected spec. A developer
trying this tool against arguably the single most commonly-integrated
API spec in existence would see what looks like a hang.

**The fix:** reframed as what it actually is — "which models lie on a
cycle in the model-to-model $ref graph" is exactly what strongly
connected components answer, in one O(V+E) pass over the whole document,
with no per-model restart and so no repeated re-exploration of shared
subtrees at all. `markCycles` now builds one edge list per model (a
simple, non-exponential structural walk collecting every $ref name a
model's own type reaches, added by `collectDirectRefs`) and runs Tarjan's
algorithm once over the resulting graph (`stronglyConnectedComponents`);
a model is cyclic iff its component has more than one member, or is a
single model with a direct self-edge. Verified: `TestBuild_StripeScaleDenselyInterconnectedGraphIsFast`
(a synthetic 1500-model graph in the same dense, mutually-cross-
referencing shape) now completes in low single-digit milliseconds, and
`TestBuild_DenselyCyclicDiamondIsNotExponential` pins the exact
mechanism (a diamond fan-out plus one small, unrelated mutual-cycle pair
every node also reaches) at the scale that measured 72ms → 3.4s for just
8 more steps before the fix — extrapolating that curve lands squarely on
the Stripe measurements above.

**Bonus fix:** rewriting `markCycles` iteratively (an explicit frame
stack, no recursion at all) to keep this new algorithm safe at any
document size also closed out the "No depth guard on deeply-nested/
recursive schemas — WASM call-stack overflow" entry below, whose
remaining open gap was specifically `cycleWalk`'s per-model recursion.

## No depth guard on deeply-nested/recursive schemas — WASM call-stack overflow

**Status:** fully fixed. `internal/ir`'s `buildNode` caps *intra-schema*
recursion (array items, object fields, allOf members, union variants) at
500 levels and returns a clean error past that point — see
`docs/superpowers/specs/2026-09-11-web-playground-design.md` (sub-project
5). The remaining gap this entry originally tracked — `markCycles`/
`cycleWalk`'s recursion across models along `$ref` edges, which a long
chain of `$ref`-linked models (reproduced at ~5000 models against the
real compiled WASM module) could still overflow — was closed as a side
effect of the unrelated exponential-blowup performance fix described in
the entry above: `markCycles` was rewritten from a per-model recursive
reachability search into a single-pass, **iterative**
strongly-connected-components computation (no recursion at all).
`cmd/wasm/main_test.go`'s
`TestGenerateJS_LongRefChainDoesNotOverflowWASMStack` now pins the exact
5000-model shape against the real compiled module (93ms, no error).
Originally discovered during the WASM build (sub-project 4) final
review's fix-wave re-review. Kept here as the historical record of the
finding.

**What happens:** a spec with deeply nested (or self-referential-via-array,
i.e. not IR-cyclic but just deep) schema structure — reproduced at roughly
3000 levels of nesting, well under `internal/spec`'s own 10000-level
anti-billion-laughs depth guard — overflows the call stack while the
engine's generators recurse through it. On the native CLI/Go binary this
is harmless (Go's stacks grow, confirmed: the same 3000-deep spec
generates successfully via `openapi2code pull`). Compiled to WASM,
though, the JS engine's call-stack limit is much smaller, and the
overflow surfaces as a JS `RangeError: Maximum call stack size exceeded`
at the WASM call boundary. Confirmed to affect **both** the `ts` and
`zod` targets — it's in the shared recursive-descent shape (the IR
builder and/or per-target renderers walking nested schemas), not
specific to one generator.

**Why it's not a bricking risk (so wasn't treated as a blocker):** unlike
the Critical bug fixed in that same review round, this is a JS-side
exception, not a Go panic — nothing to `recover()` from — and the module
survives it cleanly; a follow-up call still works. The WASM test page's
`app.js` now catches and displays it as a normal error (fixed in that
same round), so today it's a bad user experience (a cryptic
call-stack-size error) rather than a broken one.

**Why it's worth fixing before sub-project 5:** the browser playground
will be driven by arbitrary pasted specs from real users, some of whom
will paste something pathological (deliberately or not) well before
`internal/spec`'s 10000-level guard kicks in. A real fix belongs in the
shared path — most likely a nesting-depth limit in `internal/ir`'s
builder (`buildNode`'s recursion), returning a clean error past some
much lower, WASM-safe threshold — rather than in either generator
individually, since both are affected identically.

## Kotlin and Dart generators: `$ref` to a named array alias whose items are an inline object

**Status:** real, reproduced in both generators, disclosed in code,
not fixed. Discovered during sub-project 6's Kotlin generator
(`internal/gen/kotlin`) fix round 2, while fixing a related bug (a
`$ref` to a named array alias whose items are themselves a `$ref` to a
further alias, e.g. `ItemIds: {type: array, items: {$ref: ItemId}}`,
now correctly resolved), and confirmed present in `internal/gen/dart`
too during that generator's own review.

**What happens:** a named top-level array schema whose `items` are an
*inline* (non-`$ref`) object — e.g. `Things: {type: array, items:
{type: object, properties: {...}}}` — generated as `typealias Things =
List<Things_2>` (`Things_2` being the inline item's synthesized name).
A field elsewhere referencing `Things` via `$ref` gets the correct
declared type, but its `toJson`/`fromJson` serialization code treats
the array's elements as plain values instead of calling
`Things_2.toJson()`/`Things_2.fromJson(...)` — the `fromJson` half is
not just semantically wrong but a genuine Kotlin type-mismatch compile
error (`List<Any?>` where `List<Things_2>` is declared).

**Why it's not fixed:** the inline item's synthesized name (`Things_2`)
was decided in a separate, earlier call when `Things` was first
rendered as its own top-level declaration. The code path that needs to
reference it (`internal/gen/kotlin/generate.go`'s
`resolveRefDispatchNode`) has no access to that name at the point it
runs — re-deriving it independently would risk producing a *different*
name than the one actually used (exactly the "two independent
derivations of the same name disagree" bug class fixed earlier in the
same sub-project, for inline enums/objects generally). Fixing this
properly needs the renderer to remember the mapping from an alias's
array-item synthesis back to its minted name, which is a real (if
modest) architectural change, not a one-line fix.

**Why it's a narrow gap:** the shape is a named array *type alias*
(not just an array-typed field) whose items are an *inline* object
(not a `$ref` to a real model) — real-world OpenAPI specs overwhelmingly
`$ref` a reusable object model into an array's `items` rather than
inlining one, since the whole point of defining a reusable array alias
is usually to reuse the model inside it too. Confirmed absent from
every fixture in `pkg/engine/testdata/`.

**If this is ever picked up:** confirmed present in `internal/gen/dart`
too, reproduced during that generator's own review (a `Things: {type:
array, items: {type: object, ...}}` typedef whose items are inline —
`things: (json["things"] as List<dynamic>).map((e) => e).toList()`
assigned to a declared `List<Things_2>`, which Dart 3's no-implicit-
downcast rule rejects, not merely a semantic mistake). Fix both
generators together rather than one at a time, since the underlying
architecture (and the reason neither can cheaply fix it) is identical.
Swift doesn't have this gap: Swift's `Codable` synthesis doesn't need
any of this alias-resolution machinery in the first place, since a
Swift `typealias` field just inherits `Codable` conformance directly
from its aliased type.

## TS/Zod/Swift/Kotlin modular output: case-only name collisions on case-insensitive filesystems

**Status:** fixed, in the same round as the cycle-detection performance
fix described earlier in this file. Found by an external real-world usability evaluation
(constructed, not seen in any real spec sampled), not by a code review —
two schemas named `Pet` and `pet` are correctly DISTINCT, case-sensitive
identifiers in every one of these four target languages (they coexist
fine as two declarations in one monolithic file), so each generator's
own name-uniquification correctly leaves them alone. But modular output
turns each into its own file — `Pet.ts`/`pet.ts` — and macOS's default
APFS and Windows' NTFS are both case-insensitive filesystems, where
those two paths are the same file. The second `os.WriteFile` in
`internal/cli/output.go`'s `writeOutput` silently overwrote the first on
disk, while the surviving barrel (TS/Zod) still `export`ed both names as
though they lived in two separate files — a silent data-loss bug on the
two most common developer-laptop filesystems, for any spec with two
schema names differing only by case.

**The fix:** a new `uniqueFileName` helper in `pkg/engine/output.go`,
shared by all four affected modular-output functions, disambiguates file
names case-insensitively (a numeric suffix on the second and later
collision, e.g. `pet_2.ts`) — the same pattern as the Dart entry below,
generalized to catch case-only collisions specifically. Swift/Kotlin
only need the file KEY disambiguated (no cross-file imports exist to
update, since same-target files see each other by declared name, not
file path); TS/Zod additionally route every barrel export and
cross-file import through the same disambiguated path map, since a TS
import's module path and its named import don't need to match — a model
keeps its own declared name even when its file lives at a different,
disambiguated path. Dart's own file names are unaffected: `dart.FileName`
always lowercases, so it never produces a case-only collision to
resolve in the first place. Verified by mutation testing (reverting the
fix reproduces the exact silent overwrite on all four targets) and by
an actual `openapi2code pull` run writing both files to a real macOS
(case-insensitive) directory.

## Dart generator: two distinct schema names can collapse to the same file name

**Status:** fixed in sub-project 6's final whole-branch review fix
wave. `pkg/engine/output.go`'s `modularDartOutput` now uniquifies file
names the same way type names already are (a local `usedFileNames`
tracker plus a disambiguating suffix before the `.dart` extension),
verified by independent mutation testing in the fix wave's re-review
(reverting the fix reproduces the original data loss exactly). Kept
here as the historical record of the finding.

**What happened:** modular Dart output names each file via
`dart.FileName`, which converts a model's PascalCase Dart identifier to
snake_case (`TreeNode` → `tree_node.dart`) — but, unlike model type
names (uniquified via `assignNames`/`uniquify` before rendering), file
names were never uniquified against each other. Two schemas whose Dart
identifiers differ (and are therefore correctly distinct top-level
types) could still collapse to the same snake_case file: `PetNote` and
`Pet-note` (sanitized to `Pet_note`) both produced `pet_note.dart`. The
second file silently overwrote the first in the output map, so one
type's declaration was lost entirely while `pet.dart` still emitted
`import 'pet_note.dart';` referencing the now-undefined type.

## Dart generator: a top-level array-of-inline-object typedef imports nothing for its own synthesized item type

**Status:** real, reproduced, disclosed, not fixed. Found during sub-project
6's final whole-branch review fix wave, while fixing the file-name
collision above and porting Swift's array-item naming convention to
Kotlin/Dart (the naming change made the gap easier to notice, but
confirmed pre-existing under either naming scheme).

**What happens:** a top-level array schema whose items are an inline
(non-`$ref`) object — e.g. `Things: {type: array, items: {type:
object, ...}}`, rendered as `typedef Things = List<ThingsItem>;` in its
own `things.dart` — gets no `import 'things_item.dart';` for the
synthesized `ThingsItem` type its own declaration references. The file
does not analyze standalone. This is the mirror image of the
already-tracked "array alias whose items are an inline object" gap
above (which is about a *field elsewhere* referencing the alias); this
one is about the alias's *own declaration file* failing to import its
own item type.

**Why it's not fixed:** `internal/gen/dart/generate.go`'s
`renderDeclaration` computes this declaration's `Dependencies` via
`collectRefDeps(node)`, which only walks `KindRef`/`KindArray` nodes —
an inline `KindObject` item contributes nothing, so the item type's
dependency is never recorded. The fix is narrow and well-understood
(union `collectRefDeps(node)`'s result with the names of whatever
`extra` declarations `resolveType` synthesized for this same
declaration — `resolveFields` already recovers equivalent information
via `refNamesIn(effective)` for the field-level case), but it's a
renderer change, not a `pkg/engine/output.go` change, so it wasn't
folded into the file-name-collision fix above despite being found at
the same time.

**If this is ever picked up:** fix alongside the sibling gap above,
since both concern the same underlying schema shape (a named array
alias with an inline-object item) and the same "the synthesized name
isn't available where it's needed" root cause.

## `additionalProperties`: the bare-boolean and mixed-properties cases

**Status:** deliberately out of scope, not unimplemented by oversight.
`additionalProperties: <schema>` (the overwhelmingly common real-world
case — a dictionary/map-shaped field) is fully supported across all five
targets as `ir.KindMap` (`internal/ir/build.go`'s `isMapShaped`/
`buildMapNode`), rendered as TS `Record<string, V>`, Zod
`z.record(V)`, Swift `[String: V]` (Codable for free), and hand-written
`Map<String, V>`/`Map<String, V>` (de)serialization in Kotlin/Dart. Two
narrower shapes are explicitly excluded from that support:

1. **`additionalProperties: true`** (the bare boolean, with no schema) —
   "any JSON value is allowed here" has no well-defined value type this
   generator can render consistently across all five targets. TS/Zod
   could represent it as `Record<string, unknown>`/`z.record(z.unknown())`,
   and Kotlin/Dart could reuse their existing `Any?`/`dynamic` JSON
   representation, but Swift has no natural Codable "any JSON value" type
   without a hand-rolled wrapper this project doesn't have. Rather than
   have Swift alone treat this as unsupported while the other four
   targets render something, a schema using this form is left as an
   ordinary (typically empty) object, same as before this feature
   existed.
2. **A schema with BOTH fixed `properties` AND `additionalProperties`**
   (a struct with a catch-all bucket for anything else) — a real,
   rarer pattern. Supporting it means adding an "extra fields" bucket to
   every generator's serialization code (toJson/fromJson would need to
   round-trip both the known fields and whatever's left over), which is
   real design and implementation work per generator, not a mechanical
   extension of the map support above. Left rendering only the fixed
   properties, exactly as if `additionalProperties` were absent.

**Why it's worth revisiting:** if a real spec turns up using either
shape in a load-bearing way, both are well-scoped, standalone follow-ups
— the first needs one cross-language design decision (what "any JSON
value" means per target), the second needs a per-generator "extra
fields" design, most naturally modeled as its own field on `ObjectNode`
(e.g. `AdditionalProperties *Node`) rather than folding into `KindMap`.

## oneOf/anyOf support for Swift/Kotlin/Dart

**Status:** explicitly out of scope for sub-project 6, not just
unimplemented. A model or field using `oneOf`/`anyOf` (or allOf's
non-object-member intersection fallback) is skipped from Swift/Kotlin/
Dart output with a comment, rather than generated incorrectly — see
`docs/superpowers/specs/2026-09-11-mobile-generators-design.md`.

**Why it's deferred:** none of the three target languages has a
construct as directly expressible as TS's `A | B` or Zod's
`z.union([...])`. Supporting it idiomatically needs a real per-language
design: an enum-with-associated-values in Swift, a sealed class
hierarchy in Kotlin/Dart, plus hand-written "try each variant" decode
logic for each — three more per-language design-and-implementation
efforts, layered onto everything else sub-project 6 already covers.

**Why it's worth revisiting:** it's the one remaining feature-parity gap
between the mobile generators and TS/Zod. `pkg/engine/testdata/composition.yaml`
already exercises this fixture shape (`StringOrNumber`, and `Pet` via
`anyOf`) for TS/Zod but both are silently absent from the Swift/Kotlin/
Dart golden output for that same fixture — worth being aware of before
treating the mobile generators as fully feature-complete.

## Comma-separated multi-target CLI support

**Status:** fixed. `--target` now accepts a comma-separated list
(`--target ts,zod,swift`), validated member-by-member (each name checked
against the same five-target list a single `--target` already used, and
rejected as a clear error if any name repeats). `--out-file` is rejected
outright when more than one target is given — there's no single file to
merge multiple languages into — so multi-target requires `--output
<dir>`, which writes one subdirectory per target (`<dir>/ts/`,
`<dir>/zod/`, ...): `ts` and `zod` both name their monolithic/barrel
file `index.ts`, so a flat directory would let one target's output
silently overwrite the other's. `splitTargets` in `internal/cli/parse.go`
is the single source of truth both validation (`parseArgs`) and
dispatch (`Run`) use, so the two can never disagree about what one
`--target` value means. A single target (no comma) behaves byte-for-byte
identically to before — no subdirectory, no behavior change — the new
path only activates once a second target is actually present. Entirely
an `internal/cli` change, as originally scoped below; `pkg/engine` and
every generator are untouched. Verified end-to-end against the real CLI
binary with the PRD's own `--target ts,zod` example.
