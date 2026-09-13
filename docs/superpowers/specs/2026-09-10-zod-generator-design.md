# Design: Zod Generator

**Date:** 2026-09-10
**Status:** Approved for planning
**Sub-project:** 3a of 5 (see `docs/superpowers/specs/2026-09-10-core-engine-ts-generator-design.md` for the full decomposition; sub-project 3 — "additional generators" — further splits into independent per-language slices: 3a Zod, then Swift/Kotlin/Dart)

## Goal

Add a Zod runtime-validation-schema generator that consumes the existing `internal/ir` (unchanged since sub-project 1) and produces idiomatic Zod v3 schemas, matching the PRD's "Runtime Validation Schemas (Zod)" feature. Wire it into `pkg/engine` (a new `GenerateZod`) and the CLI (`--target zod`), following the exact same shape as the existing TypeScript generator.

Per the sub-project 1 design's "Independent outputs" decision: Zod and TS generators are independent — the Zod generator does not consume or depend on the TS generator's output, and vice versa. Each is a standalone, complete consumer of the IR.

## Prerequisite fix: cycle detection through `allOf`-extends edges

`internal/ir/build.go`'s `markCycles`/`reachesSelf` currently walks `Object.Fields` and `Union.Variants`/`Intersection.Members` when searching for a path back to a model's own name, but not `Object.Extends` (the list of model names an object inherits from via `allOf`). A cycle formed purely through `allOf` inheritance (model A `allOf`-extends model B, and B has a field referencing A) is therefore currently undetected — `Model.Cyclic` would incorrectly stay `false`.

This matters now because this sub-project is the first real consumer of `Model.Cyclic`: a missed cycle means a model that actually needs the `z.lazy()`/`z.ZodType<X>` treatment (see below) instead gets the simple `z.infer` treatment, producing generated code that either throws at runtime (Zod building an infinitely-recursive schema) or fails to compile.

**Fix:** in `reachesSelf`'s `KindObject` case, in addition to walking `node.Object.Fields`, also walk `node.Object.Extends` the same way `KindRef` is walked (look up each extended name in `byName`, recurse into that model's `Type` with the same `visiting` guard). This is purely additive — it can only cause `markCycles` to detect cycles it previously missed, never to mis-detect a cycle that isn't real, and since the existing TS generator never consults `Model.Cyclic`, no previously-generated TS output changes as a result of this fix.

## Zod version target

Zod v3. It remains the most widely installed version across existing projects; its API (`z.object`, `z.string()`, `.optional()`, `.nullable()`, `z.lazy()`) is stable and what most consumers' `package.json` will already have.

## Architecture

```
ir.Document  →  internal/gen/zod.Generate(doc) → []ModelOutput
                                                        │
pkg/engine.GenerateZod(doc, ZodOptions{Modular})  ──────┘
    (same monolithic/modular file-layout logic as GenerateTS,
     factored so both generators share the layout helpers)
```

`internal/gen/zod` is a new package, structurally mirroring `internal/gen/ts`: a `ModelOutput{Name, Declaration, Dependencies}` type (same shape as `ts.ModelOutput`), a `Generate(doc *ir.Document) ([]ModelOutput, error)` entry point. It imports and reuses `ts.SanitizeIdentifier` directly (already exported from `internal/gen/ts`) rather than duplicating it, so schema-name collisions get the identical uniquification treatment as the TS generator — both generators must agree on what a given model name sanitizes to, since a spec with a name-colliding schema pair gets suffixed identically regardless of which target is being generated.

`pkg/engine` gains:
```go
type ZodOptions struct {
    Modular bool
}
func GenerateZod(doc *ir.Document, opts ZodOptions) (Output, error)
```
mirroring `GenerateTS`/`TSOptions` exactly, including the same `Output{Files map[string]string}` shape, `"index.ts"` monolithic key, and `"<Name>.ts"` + barrel modular layout.

## Rendering strategy

Each IR `Node` kind renders to a Zod expression:

| IR kind | Zod expression |
|---|---|
| `KindPrimitive` (string/number/boolean) | `z.string()` / `z.number()` / `z.boolean()` |
| `KindPrimitive` (unknown) | `z.unknown()` |
| `KindObject` | `z.object({ field: <fieldExpr>, ... })` |
| `KindArray` | `z.array(<itemExpr>)` |
| `KindEnum` | `z.enum([<quoted values>])` |
| `KindUnion` | `z.union([<variantExpr>, <variantExpr>, ...])` |
| `KindObject` with `Extends` | chained `.extend()`: e.g. `AnimalSchema.extend(DogOwnFieldsShape)` — see below |
| `KindIntersection` | `z.intersection(<memberExpr>, <memberExpr>)`, folded left-to-right for 3+ members |
| `KindRef` | the referenced model's `<Name>Schema` identifier |

Field modifiers compose: `Nullable` adds `.nullable()`, `Optional` adds `.optional()` — both can apply to the same field (e.g. `z.string().nullable().optional()`), matching the TS generator's independent nullable/optional tracking.

**`allOf`-extends rendering detail:** for `ObjectNode{Extends: [A, B], Fields: [...]}`, Zod's idiom is chaining `.extend()` on an existing object schema: `ASchema.extend(BSchema.shape).extend({ ...own fields... })`. This assumes every `Extends` target is itself an object-shaped schema (true by construction — `Extends` is only populated from `allOf` members the IR builder already confirmed were object-shaped; the `KindIntersection` fallback handles the non-object case at the IR level already, per the existing allOf design from sub-project 1).

**Enum values:** `ir.EnumNode.Values []string` are rendered as quoted strings in `z.enum([...])`, consistent with the existing string-only-enum-fidelity limitation already accepted in sub-project 1 (numeric/boolean enum values are stringified, not type-preserved — unchanged, not addressed by this sub-project).

## Naming and type export

For an ordinary (non-cyclic) model `Pet`:
```ts
export const PetSchema = z.object({
  name: z.string(),
});
export type Pet = z.infer<typeof PetSchema>;
```

For a cyclic model `TreeNode` (per `Model.Cyclic`, after the prerequisite fix above):
```ts
export interface TreeNode {
  value: string;
  children?: TreeNode[];
}
export const TreeNodeSchema: z.ZodType<TreeNode> = z.lazy(() => z.object({
  value: z.string(),
  children: z.array(TreeNodeSchema).optional(),
}));
```
The interface is hand-rendered directly from the IR (reusing the same field-rendering logic already used for the schema, just targeting TS type syntax instead of Zod calls) — this stays self-contained within the Zod generator's own output file; it does not import or depend on the separate TS generator's output, preserving the "independent outputs" decision.

## Output modes

Both monolithic (single file, `"index.ts"` key) and modular (one file per model at `"<Name>.ts"` + barrel `"index.ts"`) — identical structure to the TS generator's two modes, including cross-file `import type`-equivalent behavior (a plain `import { XSchema } from "./X";` for modular Zod files, since schema constants are runtime values, not types).

## CLI wiring

`internal/cli/parse.go`'s target validation extends from "only `ts` is accepted" to "`ts` or `zod`". `pkg/engine`'s dispatch (currently implicit in `internal/cli/run.go` always calling `GenerateTS`) becomes a small switch on `opts.Target` calling either `GenerateTS` or `GenerateZod`. Still single-target only (`--target zod`, not comma-separated `--target ts,zod`) — multi-target-per-invocation is out of scope, matching the CLI design's original scoping decision.

## Testing

- `internal/ir`: a new test for the `markCycles` fix — an `allOf`-extends-only cycle (no field-mediated cycle) now correctly marks `Cyclic: true`.
- `internal/gen/zod`: unit tests per IR kind (mirroring `internal/gen/ts`'s test structure) — primitives, objects with required/optional/nullable fields, arrays, enums, unions, allOf-extends, intersection fallback, and the cyclic z.lazy()/z.ZodType path.
- `pkg/engine`: golden-file tests reusing the existing fixture specs in `pkg/engine/testdata/` (petstore, composition, nullable_enum — circular.yaml already covers field-mediated cycles) plus one new fixture specifically for an `allOf`-extends-mediated cycle, since nothing currently exercises that path.
- `internal/cli`: extend existing tests to cover `--target zod` end-to-end.

## Explicitly out of scope for this sub-project

- Swift, Kotlin, Dart generators (separate sub-projects, 3b/3c/3d).
- Comma-separated multi-target CLI invocation (`--target ts,zod`).
- Zod v4 support.
- Any change to the TS generator or its output.
- Fixing the previously-noted `EnumNode.Values []string` numeric-type-fidelity limitation (still deferred, tracked in `docs/superpowers/specs/2026-09-10-future-considerations.md`).
