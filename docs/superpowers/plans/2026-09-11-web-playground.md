# Web Playground (Sub-project 5) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace sub-project 4's minimal WASM test page with a polished, split-screen playground (example gallery, live debounced generation, tabbed multi-file output, syntax highlighting, theme toggle), and fix the deep-nesting stack-overflow bug in `internal/ir` that this public-facing page would otherwise be the most likely place to trigger.

**Architecture:** One isolated backend change (`internal/ir`'s recursive builder gains a depth counter and a clean error past 500 levels) plus a from-scratch rewrite of `web/index.html` and `web/app.js`, split across two new self-contained JS modules (`web/highlight.js`, a regex-based syntax highlighter; `web/examples.js`, a static gallery of specs drawn from this repo's own fixtures). No new Go dependencies, no frontend build step — Tailwind and Lucide load from CDN exactly the way `web/wasm_exec.js` is already used today: `<script>` tags, no npm.

**Tech Stack:** Go 1.25 (stdlib only) for the `internal/ir` change; vanilla ES modules, Tailwind CSS (Play CDN), and Lucide icons (CDN) for the frontend; Node (already required by `web/test.sh`) for the highlighter's automated tests.

**Spec:** `docs/superpowers/specs/2026-09-11-web-playground-design.md`

## Global Constraints

- Max nesting depth in `internal/ir` is **500**; exceeding it returns the error `schema nesting exceeds maximum depth of 500` (exact string, via `fmt.Errorf("schema nesting exceeds maximum depth of %d", maxNestingDepth)`).
- No new Go dependencies — stdlib only, consistent with the project's zero-dependency goal for the engine.
- All new `web/*.js` files are ES modules (`import`/`export`), loaded via `<script type="module">`, except `web/wasm_exec.js` which stays the classic script Go's toolchain generates.
- Styling is Tailwind CSS via the Play CDN (`https://cdn.tailwindcss.com`), configured with `tailwind.config = { darkMode: "class" }`. No PostCSS, no Tailwind CLI, no `node_modules` for the frontend.
- Icons are Lucide via CDN (`https://unpkg.com/lucide@latest`), rendered once via `lucide.createIcons()`.
- `web/index.html` and `web/app.js` are **replaced in place** (same paths), not duplicated alongside the old test page.
- `web/build.sh`, `web/smoketest.mjs`, and `web/test.sh`'s existing steps are unaffected — `web/test.sh` gains exactly one new step (the highlighter test). The WASM JS contract (`window.openapi2codeGenerate(specText, target, modular)`) does not change.

---

### Task 1: Depth guard in `internal/ir`

**Files:**
- Modify: `internal/ir/build.go` (full-file replacement — every recursive function's signature changes)
- Modify: `internal/ir/build_test.go` (add two tests + one helper)
- Modify: `docs/superpowers/specs/2026-09-10-future-considerations.md` (mark the deep-nesting finding resolved)

**Interfaces:**
- Consumes: nothing new — `ir.Build(raw *spec.RawDocument) (*Document, error)`'s public signature is unchanged.
- Produces: `ir.Build` now returns an error containing the substring `"exceeds maximum depth"` for any schema nested past 500 levels (array items / object fields / non-ref allOf members / union variants, counted per level of descent). No other task depends on the internal `depth int` parameter added to `buildNode`/`buildArrayNode`/`buildObjectNode`/`buildAllOfNode`/`buildUnionNode` — that's private to this package.

- [ ] **Step 1: Write the failing tests**

Open `internal/ir/build_test.go` and add `"strings"` to the import block (it currently imports `"fmt"`, `"testing"`, `"time"`, plus the two project packages) — the new tests below need it. Then append these two tests and the helper function at the end of the file (after `modelByName`):

```go
func nestedArraySchema(depth int) *spec.RawSchema {
	s := &spec.RawSchema{Type: "string"}
	for i := 0; i < depth; i++ {
		s = &spec.RawSchema{Type: "array", Items: s}
	}
	return s
}

func TestBuild_ExceedsMaxNestingDepthErrors(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Deep": nestedArraySchema(501),
		},
	}
	_, err := ir.Build(raw)
	if err == nil {
		t.Fatal("expected an error for schema nesting beyond the max depth, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds maximum depth") {
		t.Fatalf("expected a nesting-depth error, got: %v", err)
	}
}

func TestBuild_AtMaxNestingDepthSucceeds(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Deep": nestedArraySchema(500),
		},
	}
	_, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("Build: unexpected error at max depth: %v", err)
	}
}
```

`nestedArraySchema(depth)` builds a schema of `depth` nested `array`s wrapping a `string` leaf — e.g. `nestedArraySchema(2)` is an array of arrays of strings. `buildNode` is called on the top-level schema at depth 0, and each `array -> items` descent increments depth by one, so the leaf schema inside `nestedArraySchema(N)` is built at depth exactly `N`. This makes 500 the last depth that must still succeed and 501 the first that must fail — matching the two tests above exactly.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ir/... -run 'TestBuild_ExceedsMaxNestingDepthErrors|TestBuild_AtMaxNestingDepthSucceeds' -v`
Expected: `TestBuild_ExceedsMaxNestingDepthErrors` FAILS (no error is returned today — recursion just keeps going and eventually succeeds since 501 levels doesn't overflow Go's native stack). `TestBuild_AtMaxNestingDepthSucceeds` passes already (nothing changed for it yet), which is fine — it exists to guard against a future off-by-one, not to fail right now.

- [ ] **Step 3: Replace `internal/ir/build.go` in full**

Replace the entire file with:

```go
package ir

import (
	"fmt"
	"sort"

	"github.com/tarikomercehajic/openapi2code/internal/spec"
)

// maxNestingDepth caps how deeply buildNode will recurse into a schema's
// own structure (array items, object fields, allOf members, union
// variants) before giving up with a clean error. It exists to protect
// the WASM build, where a JS engine's call stack is far smaller than a
// native Go binary's: a spec nested ~3000 levels deep overflows the
// WASM call stack as an uncaught JS RangeError, while the same spec
// builds fine natively (Go's stacks grow). 500 is comfortably past any
// real-world spec (every fixture and benchmark spec in this repo is
// under 50 levels) and comfortably short of that browser failure mode.
const maxNestingDepth = 500

// Build walks a validated RawDocument and produces the IR Document for it.
func Build(raw *spec.RawDocument) (*Document, error) {
	names := make([]string, 0, len(raw.Schemas))
	for name := range raw.Schemas {
		names = append(names, name)
	}
	sort.Strings(names)

	models := make([]*Model, 0, len(names))
	for _, name := range names {
		node, err := buildNode(raw, raw.Schemas[name], 0)
		if err != nil {
			return nil, fmt.Errorf("model %q: %w", name, err)
		}
		models = append(models, &Model{Name: name, Type: node})
	}

	doc := &Document{Models: models}
	markCycles(doc)
	return doc, nil
}

func buildNode(raw *spec.RawDocument, s *spec.RawSchema, depth int) (*Node, error) {
	if depth > maxNestingDepth {
		return nil, fmt.Errorf("schema nesting exceeds maximum depth of %d", maxNestingDepth)
	}
	if s.Ref != "" {
		name, err := raw.ResolveRefName(s.Ref)
		if err != nil {
			return nil, err
		}
		return &Node{Kind: KindRef, RefName: name}, nil
	}
	if len(s.AllOf) > 0 {
		return buildAllOfNode(raw, s, depth)
	}
	if len(s.OneOf) > 0 {
		return buildUnionNode(raw, s.OneOf, depth)
	}
	if len(s.AnyOf) > 0 {
		return buildUnionNode(raw, s.AnyOf, depth)
	}
	if len(s.Enum) > 0 {
		return buildEnumNode(s), nil
	}
	switch s.Type {
	case "array":
		return buildArrayNode(raw, s, depth)
	case "string":
		return &Node{Kind: KindPrimitive, Primitive: PrimitiveString}, nil
	case "number":
		return &Node{Kind: KindPrimitive, Primitive: PrimitiveNumber}, nil
	case "integer":
		return &Node{Kind: KindPrimitive, Primitive: PrimitiveInteger}, nil
	case "boolean":
		return &Node{Kind: KindPrimitive, Primitive: PrimitiveBoolean}, nil
	case "object", "":
		return buildObjectNode(raw, s, depth)
	default:
		return &Node{Kind: KindPrimitive, Primitive: PrimitiveUnknown}, nil
	}
}

// buildEnumNode stringifies enum values generically. Only string-valued
// enums (the overwhelmingly common real-world case) are rendered
// correctly as TS string-literal unions; numeric/boolean enum values are
// stringified as-is rather than given type-specific literal formatting.
func buildEnumNode(s *spec.RawSchema) *Node {
	values := make([]string, 0, len(s.Enum))
	for _, v := range s.Enum {
		values = append(values, fmt.Sprint(v))
	}
	return &Node{Kind: KindEnum, Enum: &EnumNode{Values: values}}
}

func buildArrayNode(raw *spec.RawDocument, s *spec.RawSchema, depth int) (*Node, error) {
	if s.Items == nil {
		return &Node{Kind: KindArray, Array: &ArrayNode{Items: &Node{Kind: KindPrimitive, Primitive: PrimitiveUnknown}}}, nil
	}
	items, err := buildNode(raw, s.Items, depth+1)
	if err != nil {
		return nil, fmt.Errorf("items: %w", err)
	}
	return &Node{Kind: KindArray, Array: &ArrayNode{Items: items}}, nil
}

func buildObjectNode(raw *spec.RawDocument, s *spec.RawSchema, depth int) (*Node, error) {
	requiredSet := make(map[string]bool, len(s.Required))
	for _, r := range s.Required {
		requiredSet[r] = true
	}
	names := make([]string, 0, len(s.Properties))
	for name := range s.Properties {
		names = append(names, name)
	}
	sort.Strings(names)

	fields := make([]*Field, 0, len(names))
	for _, name := range names {
		propSchema := s.Properties[name]
		fieldType, err := buildNode(raw, propSchema, depth+1)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", name, err)
		}
		fields = append(fields, &Field{
			Name:     name,
			Type:     fieldType,
			Optional: !requiredSet[name],
			Nullable: propSchema.IsNullable(),
		})
	}
	return &Node{Kind: KindObject, Object: &ObjectNode{Fields: fields}}, nil
}

func buildAllOfNode(raw *spec.RawDocument, s *spec.RawSchema, depth int) (*Node, error) {
	// Collapse single-ref allOf with no own properties to a plain ref.
	// This handles the common pattern of allOf used to attach a description to a $ref.
	if len(s.AllOf) == 1 && s.AllOf[0].Ref != "" {
		own, err := buildObjectNode(raw, s, depth)
		if err != nil {
			return nil, fmt.Errorf("allOf own properties: %w", err)
		}
		if len(own.Object.Fields) == 0 {
			name, err := raw.ResolveRefName(s.AllOf[0].Ref)
			if err != nil {
				return nil, fmt.Errorf("allOf[0]: %w", err)
			}
			return &Node{Kind: KindRef, RefName: name}, nil
		}
	}

	hasNonObject := false
	memberNodes := make([]*Node, 0, len(s.AllOf))
	var extends []string
	var fields []*Field
	seen := make(map[string]bool)

	addFields := func(fs []*Field) {
		for _, f := range fs {
			if seen[f.Name] {
				continue
			}
			seen[f.Name] = true
			fields = append(fields, f)
		}
	}

	for i, member := range s.AllOf {
		var node *Node
		if member.Ref != "" {
			name, err := raw.ResolveRefName(member.Ref)
			if err != nil {
				return nil, fmt.Errorf("allOf[%d]: %w", i, err)
			}
			node = &Node{Kind: KindRef, RefName: name}
			extends = append(extends, name)
		} else {
			built, err := buildNode(raw, member, depth+1)
			if err != nil {
				return nil, fmt.Errorf("allOf[%d]: %w", i, err)
			}
			node = built
			if node.Kind == KindObject {
				addFields(node.Object.Fields)
				extends = append(extends, node.Object.Extends...)
			} else {
				hasNonObject = true
			}
		}
		memberNodes = append(memberNodes, node)
	}

	own, err := buildObjectNode(raw, s, depth)
	if err != nil {
		return nil, fmt.Errorf("allOf own properties: %w", err)
	}
	addFields(own.Object.Fields)

	if hasNonObject {
		if len(own.Object.Fields) > 0 {
			memberNodes = append(memberNodes, own)
		}
		return &Node{Kind: KindIntersection, Intersection: &IntersectionNode{Members: memberNodes}}, nil
	}
	return &Node{Kind: KindObject, Object: &ObjectNode{Extends: extends, Fields: fields}}, nil
}

func buildUnionNode(raw *spec.RawDocument, variants []*spec.RawSchema, depth int) (*Node, error) {
	nodes := make([]*Node, 0, len(variants))
	for i, v := range variants {
		n, err := buildNode(raw, v, depth+1)
		if err != nil {
			return nil, fmt.Errorf("variant[%d]: %w", i, err)
		}
		nodes = append(nodes, n)
	}
	return &Node{Kind: KindUnion, Union: &UnionNode{Variants: nodes}}, nil
}

// markCycles sets Cyclic on every model that is reachable from itself by
// following Ref edges through other models' type trees.
func markCycles(doc *Document) {
	byName := make(map[string]*Model, len(doc.Models))
	for _, m := range doc.Models {
		byName[m.Name] = m
	}
	for _, m := range doc.Models {
		w := &cycleWalk{
			start:   m.Name,
			byName:  byName,
			onStack: map[string]bool{},
			noReach: map[string]bool{},
		}
		if w.reachesStart(m.Type) {
			m.Cyclic = true
		}
	}
}

// cycleWalk is one model's search for a path back to itself.
//
// Without memoization the search re-explores shared subtrees once per path
// that reaches them, which is exponential in the number of paths on
// diamond-shaped (but perfectly acyclic) ref graphs that are common in real
// specs. noReach records refs already shown not to reach start so each one
// is expanded at most once.
type cycleWalk struct {
	start   string
	byName  map[string]*Model
	onStack map[string]bool

	// noReach holds refs whose full expansion did not reach start and was
	// not truncated by the onStack guard — meaning the result did not
	// depend on the current path and is safe to reuse.
	noReach map[string]bool

	// truncated reports whether the expansion in progress skipped an edge
	// because of the onStack guard. Such a result is path-dependent, so it
	// is not memoized; this keeps the search's answers identical to the
	// unmemoized version while still collapsing the acyclic case.
	truncated bool
}

func (w *cycleWalk) reachesStart(node *Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind {
	case KindRef:
		return w.refReachesStart(node.RefName)
	case KindObject:
		for _, extend := range node.Object.Extends {
			if w.refReachesStart(extend) {
				return true
			}
		}
		for _, f := range node.Object.Fields {
			if w.reachesStart(f.Type) {
				return true
			}
		}
	case KindArray:
		return w.reachesStart(node.Array.Items)
	case KindUnion:
		for _, v := range node.Union.Variants {
			if w.reachesStart(v) {
				return true
			}
		}
	case KindIntersection:
		for _, member := range node.Intersection.Members {
			if w.reachesStart(member) {
				return true
			}
		}
	}
	return false
}

func (w *cycleWalk) refReachesStart(refName string) bool {
	if refName == w.start {
		return true
	}
	if w.onStack[refName] {
		w.truncated = true
		return false
	}
	if w.noReach[refName] {
		return false
	}
	target, ok := w.byName[refName]
	if !ok {
		return false
	}

	outerTruncated := w.truncated
	w.truncated = false
	w.onStack[refName] = true
	found := w.reachesStart(target.Type)
	delete(w.onStack, refName)
	if !found && !w.truncated {
		w.noReach[refName] = true
	}
	w.truncated = w.truncated || outerTruncated
	return found
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/ir/... -v`
Expected: every test in the package PASSES, including the two new ones.

- [ ] **Step 5: Run the full test suite to check for regressions**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all packages pass. (`internal/spec`, `internal/gen/ts`, `internal/gen/zod`, `pkg/engine` all call into `ir.Build` indirectly — none of them construct schemas anywhere near 500 levels deep, so nothing regresses.)

- [ ] **Step 6: Mark the finding resolved in `future-considerations.md`**

Open `docs/superpowers/specs/2026-09-10-future-considerations.md` and change the heading and first line of the "No depth guard on deeply-nested/recursive schemas" section from:

```markdown
## No depth guard on deeply-nested/recursive schemas — WASM call-stack overflow

**Status:** real, reproduced, not fixed. Discovered during the WASM build
(sub-project 4) final review's fix-wave re-review.
```

to:

```markdown
## No depth guard on deeply-nested/recursive schemas — WASM call-stack overflow

**Status:** fixed in sub-project 5 (`internal/ir`'s `buildNode` now caps
recursion at 500 levels and returns a clean error past that point — see
`docs/superpowers/specs/2026-09-11-web-playground-design.md`). Originally
discovered during the WASM build (sub-project 4) final review's fix-wave
re-review.
```

Leave the rest of that section (the "What happens"/"Why it's not a bricking risk"/"Why it's worth fixing before sub-project 5" prose) as-is — it's the historical record of the finding, still accurate as a description of what was found and why it mattered.

- [ ] **Step 7: Commit**

```bash
git add internal/ir/build.go internal/ir/build_test.go docs/superpowers/specs/2026-09-10-future-considerations.md
git commit -m "$(cat <<'EOF'
Add a nesting-depth guard to internal/ir's schema builder

Deeply nested schemas (~3000 levels) overflow the WASM call stack as
an uncaught JS RangeError, a finding from sub-project 4's final
review. buildNode now caps recursion at 500 levels and returns a
clean error past that point, well ahead of the browser failure mode
and well past any real-world spec in this repo.

EOF
)"
```

---

### Task 2: Syntax highlighter (`web/highlight.js`)

**Files:**
- Create: `web/highlight.js`
- Create: `web/highlight_test.mjs`
- Modify: `web/test.sh` (add the new test as a step)

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces: `export function highlightTS(code: string): string` — HTML-escapes `code` and wraps recognized tokens (line comments, string/template literals, numbers, keywords, PascalCase identifiers) in `<span class="tok-*">` markers (`tok-comment`, `tok-string`, `tok-number`, `tok-keyword`, `tok-type`). The result is safe to assign to an element's `innerHTML`. Task 5 imports this from `web/app.js` as `import { highlightTS } from "./highlight.js";`.

- [ ] **Step 1: Write the failing test**

Create `web/highlight_test.mjs`:

```js
// highlight_test.mjs — unit tests for the playground's syntax
// highlighter. No test framework: plain assertions, exit 1 on failure,
// matching the style of web/smoketest.mjs. Run with: node web/highlight_test.mjs
import { highlightTS } from "./highlight.js";

function assertContains(label, haystack, needle) {
  if (!haystack.includes(needle)) {
    console.error(`FAIL (${label}): expected output to contain ${JSON.stringify(needle)}, got:`, haystack);
    process.exit(1);
  }
  console.log(`OK (${label})`);
}

function assertNotContains(label, haystack, needle) {
  if (haystack.includes(needle)) {
    console.error(`FAIL (${label}): expected output NOT to contain ${JSON.stringify(needle)}, got:`, haystack);
    process.exit(1);
  }
  console.log(`OK (${label})`);
}

assertContains(
  "keyword",
  highlightTS("export interface Pet {"),
  '<span class="tok-keyword">export</span>'
);
assertContains(
  "type name",
  highlightTS("export interface Pet {"),
  '<span class="tok-type">Pet</span>'
);
assertContains(
  "string literal",
  highlightTS('const s = "hello";'),
  '<span class="tok-string">"hello"</span>'
);
assertContains(
  "number literal",
  highlightTS("const x = 42;"),
  '<span class="tok-number">42</span>'
);
assertContains(
  "line comment",
  highlightTS("// a comment\nconst x = 1;"),
  '<span class="tok-comment">// a comment</span>'
);
assertContains(
  "zod call still renders plain identifiers",
  highlightTS("z.object({ name: z.string() })"),
  "z.object"
);

// The highlighter's output is assigned to innerHTML by the playground, so
// any "<" or ">" in the source text — plausible in a string literal or
// comment inside generated code — must never reach the DOM unescaped.
const escaped = highlightTS('const s = "<script>alert(1)</script>";');
assertNotContains("HTML-unsafe input is escaped", escaped, "<script>alert(1)</script>");
assertContains("HTML-unsafe input is escaped (check)", escaped, "&lt;script&gt;");

assertContains("empty input", highlightTS(""), "");

console.log("All highlight tests passed.");
process.exit(0);
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `node web/highlight_test.mjs`
Expected: FAIL — `web/highlight.js` doesn't exist yet, so the `import` throws a module-not-found error.

- [ ] **Step 3: Write the implementation**

Create `web/highlight.js`:

```js
// highlight.js — a minimal regex-based syntax highlighter for the
// TypeScript/Zod code this project generates. Not a general-purpose TS
// highlighter: it only needs to handle what internal/gen/ts and
// internal/gen/zod actually emit (interfaces, type aliases, const
// declarations, z.* calls, string/enum literals, comments, numbers).

const KEYWORDS = new Set([
  "export", "import", "interface", "type", "const", "let", "function",
  "return", "from", "extends", "readonly", "typeof", "as", "null",
  "undefined", "true", "false", "void", "namespace",
]);

// Alternation order matters: comments and strings must be matched whole
// before their contents could be mistaken for other tokens (e.g. a
// keyword-like word inside a string).
const TOKEN_RE =
  /(\/\/[^\n]*)|("(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|`(?:[^`\\]|\\.)*`)|(\b\d+(?:\.\d+)?\b)|([A-Za-z_$][A-Za-z0-9_$]*)|([{}()[\]:;,.<>|&=])/g;

function escapeHtml(text) {
  return text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

// highlightTS wraps recognized tokens in `code` with <span class="tok-*">
// markers and HTML-escapes everything else (including token contents),
// so the result is always safe to assign to an element's innerHTML.
export function highlightTS(code) {
  let result = "";
  let lastIndex = 0;
  let match;
  TOKEN_RE.lastIndex = 0;
  while ((match = TOKEN_RE.exec(code)) !== null) {
    result += escapeHtml(code.slice(lastIndex, match.index));
    const [, comment, string, number, word, punct] = match;
    if (comment !== undefined) {
      result += `<span class="tok-comment">${escapeHtml(comment)}</span>`;
    } else if (string !== undefined) {
      result += `<span class="tok-string">${escapeHtml(string)}</span>`;
    } else if (number !== undefined) {
      result += `<span class="tok-number">${escapeHtml(number)}</span>`;
    } else if (word !== undefined) {
      if (KEYWORDS.has(word)) {
        result += `<span class="tok-keyword">${escapeHtml(word)}</span>`;
      } else if (/^[A-Z]/.test(word)) {
        result += `<span class="tok-type">${escapeHtml(word)}</span>`;
      } else {
        result += escapeHtml(word);
      }
    } else if (punct !== undefined) {
      result += escapeHtml(punct);
    }
    lastIndex = TOKEN_RE.lastIndex;
  }
  result += escapeHtml(code.slice(lastIndex));
  return result;
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `node web/highlight_test.mjs`
Expected: every `OK (...)` line prints, then `All highlight tests passed.`, exit code 0.

- [ ] **Step 5: Wire the new test into `web/test.sh`**

Open `web/test.sh` and add a new step after the Node smoke test (before the final `echo "All WASM checks passed."`):

```sh
echo "==> Node syntax-highlighter tests"
node web/highlight_test.mjs
```

- [ ] **Step 6: Run `web/test.sh` to verify the new step passes**

Run: `./web/test.sh`
Expected: all existing steps still pass, plus the new `==> Node syntax-highlighter tests` step prints `All highlight tests passed.`, and the script ends with `All WASM checks passed.`.

- [ ] **Step 7: Commit**

```bash
git add web/highlight.js web/highlight_test.mjs web/test.sh
git commit -m "$(cat <<'EOF'
Add a minimal syntax highlighter for the playground's output pane

Regex-based, no dependency, HTML-escapes everything so its output is
safe to assign to innerHTML. Wired into web/test.sh alongside the
existing WASM smoke test.

EOF
)"
```

---

### Task 3: Example spec gallery (`web/examples.js`)

**Files:**
- Create: `web/examples.js`

**Interfaces:**
- Consumes: nothing from other tasks (the spec text is copied verbatim from `pkg/engine/testdata/*.yaml`, not read at runtime).
- Produces: `export const EXAMPLES` — an array of `{ id: string, label: string, spec: string }`, in display order. Task 5 imports this as `import { EXAMPLES } from "./examples.js";` and renders one button per entry.

- [ ] **Step 1: Create the file**

Create `web/examples.js`:

```js
// examples.js — a small built-in gallery of example specs, copied
// verbatim from this repo's own test fixtures (pkg/engine/testdata/) so
// a first-time visitor can see results without needing their own spec.
// The Swagger 2.0 example omits its source fixture's `paths` section:
// this engine only ever reads `definitions`/`components.schemas`, so
// paths add nothing to what the playground demonstrates.
export const EXAMPLES = [
  {
    id: "petstore",
    label: "Petstore (OpenAPI 3.0)",
    spec: `openapi: "3.0.3"
info:
  title: Petstore-style fixture
  version: "1.0.0"
components:
  schemas:
    Category:
      type: object
      properties:
        id:
          type: integer
        name:
          type: string
      required:
        - name
    Tag:
      type: object
      properties:
        id:
          type: integer
        name:
          type: string
      required:
        - name
    Pet:
      type: object
      properties:
        id:
          type: integer
        name:
          type: string
        category:
          $ref: "#/components/schemas/Category"
        tags:
          type: array
          items:
            $ref: "#/components/schemas/Tag"
        photoUrls:
          type: array
          items:
            type: string
        status:
          type: string
          enum:
            - available
            - pending
            - sold
      required:
        - name
        - photoUrls
    Error:
      type: object
      properties:
        code:
          type: integer
        message:
          type: string
      required:
        - code
        - message
`,
  },
  {
    id: "swagger2",
    label: "Petstore (Swagger 2.0)",
    spec: `swagger: "2.0"
info:
  title: Petstore-style fixture (Swagger 2.0)
  version: "1.0.0"
definitions:
  Category:
    type: object
    properties:
      id:
        type: integer
      name:
        type: string
    required:
      - name
  Tag:
    type: object
    properties:
      id:
        type: integer
      name:
        type: string
    required:
      - name
  Pet:
    type: object
    properties:
      id:
        type: integer
      name:
        type: string
      nickname:
        type: string
        x-nullable: true
      category:
        $ref: "#/definitions/Category"
      tags:
        type: array
        items:
          $ref: "#/definitions/Tag"
      photoUrls:
        type: array
        items:
          type: string
      status:
        type: string
        enum:
          - available
          - pending
          - sold
    required:
      - name
      - photoUrls
      - nickname
  Error:
    type: object
    properties:
      code:
        type: integer
      message:
        type: string
    required:
      - code
      - message
`,
  },
  {
    id: "circular",
    label: "Circular references",
    spec: `openapi: "3.0.3"
info:
  title: Circular fixture
  version: "1.0.0"
components:
  schemas:
    TreeNode:
      type: object
      properties:
        value:
          type: string
        children:
          type: array
          items:
            $ref: "#/components/schemas/TreeNode"
      required:
        - value
    A:
      type: object
      properties:
        b:
          $ref: "#/components/schemas/B"
    B:
      type: object
      properties:
        a:
          $ref: "#/components/schemas/A"
`,
  },
  {
    id: "composition",
    label: "allOf / oneOf / anyOf",
    spec: `openapi: "3.0.3"
info:
  title: Composition fixture
  version: "1.0.0"
components:
  schemas:
    Animal:
      type: object
      properties:
        name:
          type: string
      required:
        - name
    Dog:
      allOf:
        - $ref: "#/components/schemas/Animal"
        - type: object
          properties:
            breed:
              type: string
          required:
            - breed
    StringOrNumber:
      oneOf:
        - type: string
        - type: number
    Pet:
      anyOf:
        - $ref: "#/components/schemas/Dog"
        - $ref: "#/components/schemas/Animal"
`,
  },
];
```

- [ ] **Step 2: Sanity-check the module loads and shapes correctly**

Run:

```bash
node -e '
import("./web/examples.js").then(({ EXAMPLES }) => {
  if (EXAMPLES.length !== 4) throw new Error("expected 4 examples, got " + EXAMPLES.length);
  for (const ex of EXAMPLES) {
    if (!ex.id || !ex.label || !ex.spec) throw new Error("malformed example: " + JSON.stringify(ex));
  }
  console.log("OK:", EXAMPLES.map((e) => e.id).join(", "));
});
'
```

Expected: `OK: petstore, swagger2, circular, composition` (no thrown error). This is a one-off manual check, not a committed test file — `examples.js` is static data with no logic to regression-test; Task 5's manual browser verification is what actually exercises these examples end-to-end.

- [ ] **Step 3: Commit**

```bash
git add web/examples.js
git commit -m "$(cat <<'EOF'
Add a built-in example spec gallery for the playground

Four specs copied verbatim from pkg/engine/testdata/, so a first-time
visitor can see generated output without needing their own spec.

EOF
)"
```

---

### Task 4: Playground markup (`web/index.html`)

**Files:**
- Modify: `web/index.html` (full-file replacement)

**Interfaces:**
- Consumes: nothing (pure markup; loads Tailwind and Lucide from CDN and the existing `web/wasm_exec.js`).
- Produces the exact element IDs Task 5's `web/app.js` requires — this is the load-bearing contract between the two tasks:
  - `#spec-input` (`<textarea>`, starts `disabled`)
  - `#example-buttons` (empty `<div>`, populated by `app.js`)
  - `#target-select` (`<select>` with `value="ts"`/`value="zod"` options, starts `disabled`)
  - `#modular-toggle` (`<button>` with `aria-pressed="false"`, starts `disabled`)
  - `#file-tabs` (empty `<div>`, populated by `app.js`)
  - `#output` (`<pre>`) containing `#output-code` (`<code>`)
  - `#copy-button` containing `#copy-icon` and `#copy-icon-success` (each an `<i data-lucide="...">`, `copy-icon-success` starts with the `hidden` attribute)
  - `#theme-toggle` containing `#theme-icon-sun` and `#theme-icon-moon` (each an `<i data-lucide="...">`, `theme-icon-moon` starts with the `hidden` attribute)

- [ ] **Step 1: Replace `web/index.html` in full**

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>OpenAPI2Code Playground</title>
  <script src="https://cdn.tailwindcss.com"></script>
  <script>
    tailwind.config = { darkMode: "class" };
  </script>
  <script src="https://unpkg.com/lucide@latest"></script>
  <style>
    /* Tailwind's Play CDN only generates classes it can see in the
       markup; the highlighter builds these class names at runtime in
       JS, so their colors are defined here as plain CSS instead. */
    .tok-keyword { color: #a626a4; }
    .tok-string  { color: #50a14f; }
    .tok-number  { color: #986801; }
    .tok-comment { color: #a0a1a7; font-style: italic; }
    .tok-type    { color: #4078f2; }
    .dark .tok-keyword { color: #c678dd; }
    .dark .tok-string  { color: #98c379; }
    .dark .tok-number  { color: #d19a66; }
    .dark .tok-comment { color: #7f848e; font-style: italic; }
    .dark .tok-type    { color: #61afef; }
  </style>
</head>
<body class="bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 min-h-screen">
  <header class="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-gray-700">
    <h1 class="text-lg font-semibold">OpenAPI2Code Playground</h1>
    <button id="theme-toggle" type="button" aria-label="Toggle theme"
            class="p-2 rounded hover:bg-gray-100 dark:hover:bg-gray-800">
      <i data-lucide="sun" id="theme-icon-sun" class="w-5 h-5"></i>
      <i data-lucide="moon" id="theme-icon-moon" class="w-5 h-5" hidden></i>
    </button>
  </header>

  <main class="grid grid-cols-1 md:grid-cols-2 gap-4 p-6">
    <section class="flex flex-col gap-3">
      <div id="example-buttons" class="flex flex-wrap gap-2"></div>
      <textarea id="spec-input" spellcheck="false" disabled
                placeholder="Paste an OpenAPI/Swagger spec (JSON or YAML)..."
                class="w-full h-80 font-mono text-sm p-3 rounded border border-gray-300 dark:border-gray-700 bg-gray-50 dark:bg-gray-800 disabled:opacity-50"></textarea>
      <div class="flex items-center gap-3">
        <select id="target-select" disabled
                class="rounded border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 px-2 py-1 text-sm disabled:opacity-50">
          <option value="ts">TypeScript</option>
          <option value="zod">Zod</option>
        </select>
        <button id="modular-toggle" type="button" aria-pressed="false" disabled
                class="rounded border border-gray-300 dark:border-gray-700 px-3 py-1 text-sm disabled:opacity-50">
          Modular
        </button>
      </div>
    </section>

    <section class="flex flex-col gap-3">
      <div class="flex items-center justify-between">
        <div id="file-tabs" class="flex flex-wrap gap-2"></div>
        <button id="copy-button" type="button" aria-label="Copy to clipboard"
                class="p-2 rounded hover:bg-gray-100 dark:hover:bg-gray-800">
          <i data-lucide="copy" id="copy-icon" class="w-4 h-4"></i>
          <i data-lucide="check" id="copy-icon-success" class="w-4 h-4" hidden></i>
        </button>
      </div>
      <pre id="output" class="w-full h-96 overflow-auto rounded border border-gray-300 dark:border-gray-700 bg-gray-50 dark:bg-gray-800 p-3 text-sm"><code id="output-code">Loading WASM module...</code></pre>
    </section>
  </main>

  <script src="wasm_exec.js"></script>
  <script type="module" src="app.js"></script>
</body>
</html>
```

- [ ] **Step 2: Verify the required element IDs are present**

Run:

```bash
for id in spec-input example-buttons target-select modular-toggle file-tabs output output-code copy-button copy-icon copy-icon-success theme-toggle theme-icon-sun theme-icon-moon; do
  grep -q "id=\"$id\"" web/index.html || echo "MISSING: $id"
done
echo "check complete"
```

Expected: `check complete` with no `MISSING:` lines above it. This is a structural sanity check, not a substitute for the manual browser verification in Task 6 — `web/app.js` doesn't exist yet, so the page can't be exercised end-to-end until Task 5 lands.

- [ ] **Step 3: Commit**

```bash
git add web/index.html
git commit -m "$(cat <<'EOF'
Rewrite the WASM test page into the sub-project 5 playground markup

Split-screen layout (spec input + example gallery on the left, tabbed
output on the right), Tailwind CSS and Lucide icons via CDN, a
theme-toggle button. Behavior wiring is app.js (next task) — this is
markup only, verified structurally until then.

EOF
)"
```

---

### Task 5: Playground behavior (`web/app.js`)

**Files:**
- Modify: `web/app.js` (full-file replacement)

**Interfaces:**
- Consumes:
  - `web/index.html`'s element IDs from Task 4 (listed above).
  - `EXAMPLES` from `web/examples.js` (Task 3): `{ id, label, spec }[]`.
  - `highlightTS(code: string): string` from `web/highlight.js` (Task 2).
  - The unchanged WASM contract: `window.openapi2codeGenerate(specText, target, modular) -> JSON string`, where success is `{"files": {"<name>": "<content>"}}` and failure is `{"error": "<message>"}`.
- Produces: nothing further downstream — this is the last file in the dependency chain.

- [ ] **Step 1: Replace `web/app.js` in full**

```js
// app.js — the OpenAPI2Code playground: loads the WASM module and wires
// up live (debounced) generation, the example gallery, tabbed output,
// syntax highlighting, theme toggling, and copy-to-clipboard.
import { EXAMPLES } from "./examples.js";
import { highlightTS } from "./highlight.js";

const DEBOUNCE_MS = 300;
const THEME_STORAGE_KEY = "openapi2code-theme";

// Lucide's createIcons() replaces each <i data-lucide="..."> with a new
// <svg>, carrying over id/class/other attributes. It must run before the
// getElementById calls below, or those calls capture the original <i>
// elements, which are then discarded from the DOM when createIcons runs.
window.lucide.createIcons();

const specInput = document.getElementById("spec-input");
const exampleButtons = document.getElementById("example-buttons");
const targetSelect = document.getElementById("target-select");
const modularToggle = document.getElementById("modular-toggle");
const fileTabs = document.getElementById("file-tabs");
const outputCode = document.getElementById("output-code");
const copyButton = document.getElementById("copy-button");
const copyIcon = document.getElementById("copy-icon");
const copyIconSuccess = document.getElementById("copy-icon-success");
const themeToggle = document.getElementById("theme-toggle");
const themeIconSun = document.getElementById("theme-icon-sun");
const themeIconMoon = document.getElementById("theme-icon-moon");

let currentFiles = null; // the last successful generation's { "<name>": "<content>" }
let activeFile = null; // filename of the currently displayed tab
let debounceHandle = null;

// --- WASM loading (same pattern as sub-project 4's test page) ---

async function instantiate(importObject) {
  try {
    return await WebAssembly.instantiateStreaming(fetch("main.wasm"), importObject);
  } catch (err) {
    // Re-fetch rather than reusing the first response: instantiateStreaming
    // may already have consumed its body, which would make arrayBuffer()
    // throw "body already read" and mask the real problem.
    const bytes = await (await fetch("main.wasm")).arrayBuffer();
    return await WebAssembly.instantiate(bytes, importObject);
  }
}

async function loadWasm() {
  const go = new window.Go();
  const result = await instantiate(go.importObject);
  go.run(result.instance); // never resolves — main() runs select{} forever; don't await it
  while (typeof window.openapi2codeGenerate !== "function") {
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
}

function setControlsEnabled(enabled) {
  specInput.disabled = !enabled;
  targetSelect.disabled = !enabled;
  modularToggle.disabled = !enabled;
  for (const button of exampleButtons.querySelectorAll("button")) {
    button.disabled = !enabled;
  }
}

// --- Example gallery ---

function renderExampleButtons() {
  for (const example of EXAMPLES) {
    const button = document.createElement("button");
    button.type = "button";
    button.textContent = example.label;
    button.disabled = true;
    button.className =
      "rounded border border-gray-300 dark:border-gray-700 px-3 py-1 text-sm hover:bg-gray-100 dark:hover:bg-gray-800 disabled:opacity-50";
    button.addEventListener("click", () => {
      specInput.value = example.spec;
      generate();
    });
    exampleButtons.appendChild(button);
  }
}

// --- Generation ---

function showError(message) {
  currentFiles = null;
  activeFile = null;
  fileTabs.innerHTML = "";
  outputCode.className = "text-red-600 dark:text-red-400";
  outputCode.textContent = "Error: " + message;
}

function showPlaceholder(message) {
  currentFiles = null;
  activeFile = null;
  fileTabs.innerHTML = "";
  outputCode.className = "text-gray-500 dark:text-gray-400";
  outputCode.textContent = message;
}

function renderActiveFile() {
  outputCode.className = "";
  outputCode.innerHTML = highlightTS(currentFiles[activeFile]);
}

function renderFileTabs() {
  fileTabs.innerHTML = "";
  const names = Object.keys(currentFiles);
  if (!names.includes(activeFile)) {
    activeFile = names[0];
  }
  for (const name of names) {
    const tab = document.createElement("button");
    tab.type = "button";
    tab.textContent = name;
    const isActive = name === activeFile;
    tab.className = isActive
      ? "rounded px-3 py-1 text-sm bg-blue-600 text-white"
      : "rounded px-3 py-1 text-sm border border-gray-300 dark:border-gray-700 hover:bg-gray-100 dark:hover:bg-gray-800";
    tab.addEventListener("click", () => {
      activeFile = name;
      renderFileTabs();
      renderActiveFile();
    });
    fileTabs.appendChild(tab);
  }
}

function generate() {
  const specText = specInput.value;
  if (specText.trim() === "") {
    showPlaceholder("Paste a spec or pick an example above to get started.");
    return;
  }
  const modular = modularToggle.getAttribute("aria-pressed") === "true";
  let raw;
  try {
    raw = window.openapi2codeGenerate(specText, targetSelect.value, modular);
  } catch (err) {
    // A deeply nested spec that somehow still slips past internal/ir's
    // depth guard (or any other unexpected WASM-side panic) surfaces as
    // a thrown JS exception rather than a {"error": ...} result.
    showError(err && err.message ? err.message : String(err));
    return;
  }
  const result = JSON.parse(raw);
  if (result.error) {
    showError(result.error);
    return;
  }
  currentFiles = result.files;
  renderFileTabs();
  renderActiveFile();
}

function scheduleGenerate() {
  if (debounceHandle !== null) {
    clearTimeout(debounceHandle);
  }
  debounceHandle = setTimeout(() => {
    debounceHandle = null;
    generate();
  }, DEBOUNCE_MS);
}

// --- Modular toggle ---

function setModular(active) {
  modularToggle.setAttribute("aria-pressed", String(active));
  modularToggle.classList.toggle("bg-blue-600", active);
  modularToggle.classList.toggle("text-white", active);
  modularToggle.classList.toggle("border-blue-600", active);
  scheduleGenerate();
}

modularToggle.addEventListener("click", () => {
  setModular(modularToggle.getAttribute("aria-pressed") !== "true");
});

// --- Copy to clipboard ---

copyButton.addEventListener("click", async () => {
  if (currentFiles === null || activeFile === null) {
    return;
  }
  await navigator.clipboard.writeText(currentFiles[activeFile]);
  copyIcon.hidden = true;
  copyIconSuccess.hidden = false;
  setTimeout(() => {
    copyIcon.hidden = false;
    copyIconSuccess.hidden = true;
  }, 1200);
});

// --- Theme ---

function applyTheme(isDark) {
  document.documentElement.classList.toggle("dark", isDark);
  themeIconSun.hidden = isDark;
  themeIconMoon.hidden = !isDark;
}

function initTheme() {
  const stored = localStorage.getItem(THEME_STORAGE_KEY);
  const isDark = stored ? stored === "dark" : window.matchMedia("(prefers-color-scheme: dark)").matches;
  applyTheme(isDark);
}

themeToggle.addEventListener("click", () => {
  const isDark = !document.documentElement.classList.contains("dark");
  localStorage.setItem(THEME_STORAGE_KEY, isDark ? "dark" : "light");
  applyTheme(isDark);
});

// --- Wiring ---

specInput.addEventListener("input", scheduleGenerate);
targetSelect.addEventListener("change", scheduleGenerate);

renderExampleButtons();
initTheme();
showPlaceholder("Loading WASM module...");

loadWasm()
  .then(() => {
    setControlsEnabled(true);
    showPlaceholder("Paste a spec or pick an example above to get started.");
  })
  .catch((err) => {
    showError("Failed to load WASM module: " + (err && err.message ? err.message : String(err)));
  });
```

- [ ] **Step 2: Check the file for syntax errors**

Run: `node --check web/app.js`
Expected: no output, exit code 0. (`--check` parses the module without executing it, so it catches typos without needing a browser or DOM — it cannot catch DOM/runtime issues, which is what the manual browser verification in Task 6 is for.)

- [ ] **Step 3: Rebuild the WASM module and re-run the existing automated checks**

Run: `./web/test.sh`
Expected: all steps pass, ending with `All WASM checks passed.` — confirms this rewrite didn't disturb the WASM build or the Go-side/Node-side tests, none of which touch `app.js` or `index.html` directly.

- [ ] **Step 4: Manually verify in a real browser**

Run: `python3 -m http.server --directory web` and open `http://localhost:8000/` (per this project's established pattern — see `docs/superpowers/specs/2026-09-10-wasm-design.md` — this sub-project's frontend behavior is verified by hand rather than with a browser-automation harness). Confirm:
- The page loads with all controls disabled, "Loading WASM module..." shown, then controls become enabled.
- Clicking each of the 4 example buttons populates the textarea and immediately shows generated output with syntax-highlighted colors.
- Typing in the textarea regenerates output ~300ms after you stop typing (not on every keystroke).
- Switching the target select (TypeScript/Zod) and clicking the Modular toggle both regenerate.
- With Modular on and a multi-model example loaded (e.g. Petstore), the file-tabs row shows one tab per generated file; clicking a tab switches the displayed content without a new WASM call (instant, no debounce delay).
- The copy button copies the active tab's raw (non-highlighted) text to the clipboard and briefly shows a checkmark.
- The theme toggle switches between light and dark styling and persists across a page reload.
- Pasting clearly invalid text (e.g. `not a spec`) shows a red inline error in the output pane without disturbing the textarea or other controls.

- [ ] **Step 5: Commit**

```bash
git add web/app.js
git commit -m "$(cat <<'EOF'
Wire up the sub-project 5 playground's behavior

Live debounced generation, the example gallery, tabbed multi-file
output with syntax highlighting, a persisted theme toggle, and
copy-to-clipboard. Manually verified end-to-end in a browser per this
project's established WASM-frontend testing pattern.

EOF
)"
```

---

### Task 6: Documentation and final verification

**Files:**
- Modify: `README.md`

**Interfaces:**
- Consumes: nothing new — this task documents what Tasks 1-5 built.
- Produces: nothing further downstream — this is the final task.

- [ ] **Step 1: Update the WASM/playground section of `README.md`**

Find this paragraph (currently the last paragraph before "## Project layout", inside "## WASM build and browser test page"):

```markdown
The engine also compiles to WebAssembly, so a browser can parse a spec and generate code entirely client-side. `web/` holds a deliberately minimal test page for it — enough to click through every option and confirm the pipeline works outside a Go process. The polished playground UI is sub-project 5.
```

Replace it with:

```markdown
The engine also compiles to WebAssembly, so a browser can parse a spec and generate code entirely client-side. `web/` holds the playground: paste a spec (or pick one from the built-in example gallery) on the left, and see live, syntax-highlighted, tabbed output on the right as you type.
```

And retitle the section heading and its final paragraph. Change:

```markdown
## WASM build and browser test page
```

to:

```markdown
## WASM build and playground
```

Then find this later paragraph in the same section:

```markdown
Paste a spec into the textarea, pick a target and layout, and click Generate.
```

Replace it with:

```markdown
Paste a spec into the textarea (or click one of the example buttons above it) and pick a target and layout — generation happens automatically as you type or change a setting, no button to click. Modular output shows one tab per generated file; click a tab to switch the displayed file. The copy icon above the output pane copies the active tab's raw content to your clipboard, and the sun/moon icon in the header toggles between light and dark themes (remembered across reloads).
```

Then find, in the "What works today" bulleted list near the top of the file, this bullet:

```markdown
- Compiles unchanged to WebAssembly, so the same engine parses and generates entirely client-side in a browser, with no server round-trip
```

Replace it with:

```markdown
- Compiles unchanged to WebAssembly, so the same engine parses and generates entirely client-side in a browser, with no server round-trip — see the playground in `web/`
- Guards against pathologically deep schema nesting (500+ levels) with a clean error, instead of overflowing the call stack — most likely to matter in the browser, where the JS engine's stack is far smaller than a native Go binary's
```

- [ ] **Step 2: Run the complete verification suite**

Run each of these in order and confirm all pass:

```bash
go build ./...
go vet ./...
go test ./... -count=1
./web/test.sh
```

Expected: every Go package `ok`, and `./web/test.sh` ends with `All WASM checks passed.` (now including the new highlighter test step from Task 2).

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "$(cat <<'EOF'
Update README for the sub-project 5 playground

Describes the live, example-gallery-backed, tabbed playground that
replaced sub-project 4's minimal test page, and notes the new
nesting-depth guard from internal/ir.

EOF
)"
```
