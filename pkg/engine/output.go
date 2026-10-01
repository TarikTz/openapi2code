package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tarikomercehajic/openapi2code/internal/gen/dart"
	"github.com/tarikomercehajic/openapi2code/internal/gen/kotlin"
	"github.com/tarikomercehajic/openapi2code/internal/gen/python"
	"github.com/tarikomercehajic/openapi2code/internal/gen/swift"
	"github.com/tarikomercehajic/openapi2code/internal/gen/ts"
	"github.com/tarikomercehajic/openapi2code/internal/gen/zod"
	"github.com/tarikomercehajic/openapi2code/internal/ir"
)

// GenerateTS renders doc as TypeScript, laid out per opts.
//
// The layout is the public contract callers (CLI, WASM, anything else)
// build against:
//
//   - Monolithic (opts.Modular false): exactly one file, keyed "index.ts",
//     holding every model's declaration in name-sorted order.
//   - Modular (opts.Modular true): one file per model, keyed
//     "<SanitizedName>.ts", each importing the models it references; plus a
//     barrel keyed "index.ts" that re-exports every model file. So the file
//     count is always len(doc.Models)+1.
//
// <SanitizedName> is the model's name mapped to a valid TypeScript
// identifier, made unique across the document (see ts.SanitizeIdentifier);
// it is also the identifier the model is declared under, so file names and
// exported type names always agree. "index" is reserved for the barrel, so
// no model file can be named index.ts.
func GenerateTS(doc *ir.Document, opts TSOptions) (Output, error) {
	models, err := ts.Generate(doc)
	if err != nil {
		return Output{}, err
	}
	if opts.Modular {
		return modularOutput(models), nil
	}
	return monolithicOutput(models), nil
}

// uniqueFileName returns baseName, or baseName with a numeric suffix
// appended, disambiguated case-INSENSITIVELY against usedLower (which it
// updates with whichever name it returns). This exists because a
// generator's own name-uniquification is correctly case-SENSITIVE — two
// schemas named "Pet" and "pet" are legitimate, distinct identifiers in
// every one of these target languages, and coexist fine as two
// declarations in one monolithic file — but modular output turns each
// into its own file, and macOS's default APFS and Windows' NTFS are
// both case-insensitive filesystems: "Pet.ts" and "pet.ts" name the SAME
// path there. Without this, the second file silently overwrote the
// first's content on disk, while the barrel/import statements generated
// from the (correctly distinct) in-memory names still referenced both
// as though they lived in two separate files — a silent, format-
// specific data-loss bug matching (but broader than) the one already
// fixed for internal/gen/dart's non-injective FileName conversion below
// via uniqueDartFileName. Dart's own file names are unaffected by THIS
// particular fix: dart.FileName lowercases everything, so its output
// never has a case-only collision to resolve in the first place.
func uniqueFileName(baseName string, usedLower map[string]bool) string {
	lower := strings.ToLower(baseName)
	if !usedLower[lower] {
		usedLower[lower] = true
		return baseName
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s_%d", baseName, i)
		candidateLower := strings.ToLower(candidate)
		if !usedLower[candidateLower] {
			usedLower[candidateLower] = true
			return candidate
		}
	}
}

func monolithicOutput(models []ts.ModelOutput) Output {
	var sb strings.Builder
	for _, m := range models {
		sb.WriteString(m.Declaration)
		sb.WriteString("\n")
	}
	return Output{Files: map[string]string{"index.ts": sb.String()}}
}

// modularOutput writes one file per model plus a barrel index.ts.
// ts.Generate has already made model names unique (including against the
// reserved barrel name "index"), but only case-sensitively — two
// distinct schemas named e.g. "Pet" and "pet" pass that check and would
// otherwise both claim "Pet.ts"/"pet.ts", which collide as the same path
// on a case-insensitive filesystem (macOS/Windows). pathByModel resolves
// each model to a disambiguated import path (see uniqueFileName), used
// both for that model's own file key and by every other file's import of
// it — a TS import's module path and its named import don't need to
// match, so a model can keep its own declared name while living at a
// different file path than that name alone would suggest.
func modularOutput(models []ts.ModelOutput) Output {
	pathByModel := make(map[string]string, len(models))
	usedLower := map[string]bool{"index": true}
	for _, m := range models {
		pathByModel[m.Name] = uniqueFileName(m.Name, usedLower)
	}

	files := make(map[string]string, len(models)+1)
	var barrel strings.Builder
	for _, m := range models {
		var content strings.Builder
		for _, dep := range m.Dependencies {
			content.WriteString(fmt.Sprintf("import type { %s } from \"./%s\";\n", dep, pathByModel[dep]))
		}
		if len(m.Dependencies) > 0 {
			content.WriteString("\n")
		}
		content.WriteString(m.Declaration)
		files[pathByModel[m.Name]+".ts"] = content.String()
		barrel.WriteString(fmt.Sprintf("export * from \"./%s\";\n", pathByModel[m.Name]))
	}
	files["index.ts"] = barrel.String()
	return Output{Files: files}
}

// GenerateZod renders doc as Zod v3 schemas, laid out per opts.
//
// The layout mirrors GenerateTS's contract exactly:
//
//   - Monolithic (opts.Modular false): exactly one file, keyed "index.ts",
//     holding every model's declaration. Declarations are ordered so that
//     every schema is declared before the schemas that read it (see
//     topoSortForMonolithic); independent models keep their name-sorted
//     order.
//   - Modular (opts.Modular true): one file per model, keyed
//     "<SanitizedName>.ts", each importing the models it references; plus a
//     barrel keyed "index.ts" that re-exports every model file. So the file
//     count is always len(doc.Models)+1.
//
// <SanitizedName> is the model's name mapped to a valid TypeScript
// identifier, made unique across the document (see ts.SanitizeIdentifier);
// model names are assigned identically to GenerateTS's, so a spec generated
// for both targets gets matching names. "index" is reserved for the barrel,
// so no model file can be named index.ts.
//
// Two things are specific to this target. Every generated file — the
// monolithic file and each modular model file — opens with
// `import { z } from "zod";`. And the output is Zod v3 *source text*: this
// module never executes it and this repo does not depend on the zod npm
// package, so consumers must have `zod` in their own package.json for the
// generated files to compile and run.
func GenerateZod(doc *ir.Document, opts ZodOptions) (Output, error) {
	models, err := zod.Generate(doc)
	if err != nil {
		return Output{}, err
	}
	if opts.Modular {
		return modularZodOutput(models), nil
	}
	return monolithicZodOutput(models), nil
}

func monolithicZodOutput(models []zod.ModelOutput) Output {
	var sb strings.Builder
	sb.WriteString("import { z } from \"zod\";\n\n")
	for _, m := range topoSortForMonolithic(models) {
		sb.WriteString(m.Declaration)
		sb.WriteString("\n")
	}
	return Output{Files: map[string]string{"index.ts": sb.String()}}
}

// topoSortForMonolithic orders models so that no declaration reads a schema
// constant declared further down the file.
//
// This matters only for monolithic output. A single file is one lexical
// scope, so a `const` referenced above its own declaration is a temporal
// dead zone error: tsc rejects it (TS2448) and an ES module throws
// ReferenceError on load, taking the whole file down. Modular output needs
// no equivalent pass — ES module imports are hoisted and each module is
// evaluated after the modules it imports, so declaration order across files
// is irrelevant there.
//
// Ordering edges are deliberately narrow: X must precede Y only when Y is
// NON-cyclic and X is one of Y's dependencies. A cyclic model's body is
// wrapped in z.lazy(() => ...), so it reads its own dependencies only when
// that closure runs, long after the module has finished loading — it
// therefore constrains nothing. Note the asymmetry: a cyclic model still
// has to be declared before a non-cyclic model that references it, because
// that reference reads the cyclic model's const binding eagerly.
//
// This restricted edge set cannot contain a cycle. Every model on such a
// cycle would be reachable from itself purely through non-cyclic models'
// references, which is exactly the condition ir's markCycles marks as
// Cyclic — and cyclic models contribute no outgoing constraints. A
// topological order therefore always exists; the fallback below exists only
// so a future IR change can't turn a broken invariant into an infinite loop.
//
// The sort is stable: Kahn's algorithm, always taking the earliest
// zero-indegree model in input order, so models with no constraint between
// them keep the name-sorted order zod.Generate produced.
func topoSortForMonolithic(models []zod.ModelOutput) []zod.ModelOutput {
	indexOf := make(map[string]int, len(models))
	for i, m := range models {
		indexOf[m.Name] = i
	}

	// dependents[i] lists the models that must be declared after models[i].
	dependents := make([][]int, len(models))
	indegree := make([]int, len(models))
	for i, m := range models {
		if m.Cyclic {
			continue
		}
		for _, dep := range m.Dependencies {
			j, ok := indexOf[dep]
			// A dependency naming no model in this document (or naming the
			// model itself) constrains nothing.
			if !ok || j == i {
				continue
			}
			dependents[j] = append(dependents[j], i)
			indegree[i]++
		}
	}

	sorted := make([]zod.ModelOutput, 0, len(models))
	emitted := make([]bool, len(models))
	for len(sorted) < len(models) {
		next := -1
		for i := range models {
			if !emitted[i] && indegree[i] == 0 {
				next = i
				break
			}
		}
		if next == -1 {
			// Unreachable given the argument above. Emit the remainder in
			// input order rather than spinning or dropping declarations.
			for i := range models {
				if !emitted[i] {
					emitted[i] = true
					sorted = append(sorted, models[i])
				}
			}
			break
		}
		emitted[next] = true
		sorted = append(sorted, models[next])
		for _, d := range dependents[next] {
			indegree[d]--
		}
	}
	return sorted
}

// modularZodOutput writes one file per model plus a barrel index.ts. Each
// model file imports both the schema constant and the plain type from
// every dependency (import { XSchema, type X } from "./X";) — a cyclic
// model's hand-written TS type references the plain type name while its
// Zod body references the schema constant, so importing both uniformly
// avoids tracking which form a given dependent needs.
// modularZodOutput mirrors modularOutput's case-insensitive file-name
// disambiguation (see uniqueFileName and pathByModel's doc comment
// above) — the same "Pet"/"pet" collision applies identically here.
func modularZodOutput(models []zod.ModelOutput) Output {
	pathByModel := make(map[string]string, len(models))
	usedLower := map[string]bool{"index": true}
	for _, m := range models {
		pathByModel[m.Name] = uniqueFileName(m.Name, usedLower)
	}

	files := make(map[string]string, len(models)+1)
	var barrel strings.Builder
	for _, m := range models {
		var content strings.Builder
		content.WriteString("import { z } from \"zod\";\n")
		for _, dep := range m.Dependencies {
			content.WriteString(fmt.Sprintf("import { %sSchema, type %s } from \"./%s\";\n", dep, dep, pathByModel[dep]))
		}
		content.WriteString("\n")
		content.WriteString(m.Declaration)
		files[pathByModel[m.Name]+".ts"] = content.String()
		barrel.WriteString(fmt.Sprintf("export * from \"./%s\";\n", pathByModel[m.Name]))
	}
	files["index.ts"] = barrel.String()
	return Output{Files: files}
}

// SwiftOptions controls GenerateSwift's output layout.
type SwiftOptions struct {
	Modular bool
}

// GenerateSwift renders doc as Swift Codable structs/classes, laid out
// per opts.
//
//   - Monolithic (opts.Modular false): exactly one file, keyed
//     "Generated.swift", holding every declaration in the order
//     swift.Generate produced them. Declaration order never matters in
//     Swift — types may reference each other regardless of which comes
//     first in the file — so no topological sort is needed here, unlike
//     GenerateZod's monolithic output.
//   - Modular (opts.Modular true): one file per declaration, keyed
//     "<Name>.swift". Unlike GenerateTS/GenerateZod, there is no barrel
//     file — Swift files in one compilation target see every other
//     type automatically, so a re-export file would serve no purpose.
func GenerateSwift(doc *ir.Document, opts SwiftOptions) (Output, error) {
	models, err := swift.Generate(doc)
	if err != nil {
		return Output{}, err
	}
	if opts.Modular {
		return modularSwiftOutput(models), nil
	}
	return monolithicSwiftOutput(models), nil
}

func monolithicSwiftOutput(models []swift.ModelOutput) Output {
	var sb strings.Builder
	for _, m := range models {
		sb.WriteString(m.Declaration)
		sb.WriteString("\n")
	}
	return Output{Files: map[string]string{"Generated.swift": sb.String()}}
}

// modularSwiftOutput uniquifies file names case-insensitively (see
// uniqueFileName's doc comment) — unlike TS/Zod, no cross-file
// references need updating to match: Swift files in one compilation
// target see every other type automatically by its declared name, never
// by file path, so disambiguating the file a model's declaration lands
// in never needs to touch that declaration's own name or any other
// file's content.
func modularSwiftOutput(models []swift.ModelOutput) Output {
	files := make(map[string]string, len(models))
	usedLower := make(map[string]bool, len(models))
	for _, m := range models {
		files[uniqueFileName(m.Name, usedLower)+".swift"] = m.Declaration
	}
	return Output{Files: files}
}

// KotlinOptions controls GenerateKotlin's output layout.
type KotlinOptions struct {
	Modular bool
}

// GenerateKotlin renders doc as Kotlin data classes, laid out per opts.
// Mirrors GenerateSwift's contract: no topological sort needed for
// monolithic output (Kotlin, like Swift, resolves types regardless of
// declaration order), and no barrel file for modular output (Kotlin
// files with no package declaration share the default package and see
// each other automatically).
func GenerateKotlin(doc *ir.Document, opts KotlinOptions) (Output, error) {
	models, err := kotlin.Generate(doc)
	if err != nil {
		return Output{}, err
	}
	if opts.Modular {
		return modularKotlinOutput(models), nil
	}
	return monolithicKotlinOutput(models), nil
}

func monolithicKotlinOutput(models []kotlin.ModelOutput) Output {
	var sb strings.Builder
	for _, m := range models {
		sb.WriteString(m.Declaration)
		sb.WriteString("\n")
	}
	return Output{Files: map[string]string{"Generated.kt": sb.String()}}
}

// modularKotlinOutput mirrors modularSwiftOutput's case-insensitive file
// disambiguation — Kotlin files with no package declaration share the
// default package and see each other by declared name, never by file
// path, for the same reason no cross-file content needs updating here.
func modularKotlinOutput(models []kotlin.ModelOutput) Output {
	files := make(map[string]string, len(models))
	usedLower := make(map[string]bool, len(models))
	for _, m := range models {
		files[uniqueFileName(m.Name, usedLower)+".kt"] = m.Declaration
	}
	return Output{Files: files}
}

// DartOptions controls GenerateDart's output layout.
type DartOptions struct {
	Modular bool
}

// GenerateDart renders doc as Dart classes, laid out per opts.
//
//   - Monolithic (opts.Modular false): exactly one file, keyed
//     "generated.dart" (Dart file names are conventionally snake_case
//     even for a single combined file), holding every declaration.
//   - Modular (opts.Modular true): one file per declaration, keyed by
//     dart.FileName(model.Name) (e.g. "tree_node.dart"). Unlike
//     Swift/Kotlin, each file explicitly imports the files it depends on
//     — Dart has no implicit same-package visibility — but there is
//     still no barrel file (see Global Constraints).
func GenerateDart(doc *ir.Document, opts DartOptions) (Output, error) {
	models, err := dart.Generate(doc)
	if err != nil {
		return Output{}, err
	}
	if opts.Modular {
		return modularDartOutput(models), nil
	}
	return monolithicDartOutput(models), nil
}

func monolithicDartOutput(models []dart.ModelOutput) Output {
	var sb strings.Builder
	for _, m := range models {
		sb.WriteString(m.Declaration)
		sb.WriteString("\n")
	}
	return Output{Files: map[string]string{"generated.dart": sb.String()}}
}

// modularDartOutput writes one file per model, each importing the files
// its declaration references.
//
// File names are uniquified the same way dart.Generate already uniquifies
// TYPE names, because dart.FileName is not injective: it lowercases as it
// snake_cases, so two DISTINCT Dart type names can map onto the same file
// name (e.g. "PetNote" and "Pet_note" — the latter the sanitized form of
// a schema named "Pet-note" — both becoming "pet_note.dart"). Without
// this, the second model silently overwrote the first's entry in files
// and vanished from the output entirely, while imports generated from
// Dependencies still pointed at the surviving file as though the lost
// declaration lived there. Both the files map and fileNameByModel are
// keyed off the SAME disambiguated name, so those imports resolve to
// whichever file the dependency actually landed in.
func modularDartOutput(models []dart.ModelOutput) Output {
	fileNameByModel := make(map[string]string, len(models))
	usedFileNames := make(map[string]bool, len(models))
	for _, m := range models {
		fileName := uniqueDartFileName(dart.FileName(m.Name), usedFileNames)
		usedFileNames[fileName] = true
		fileNameByModel[m.Name] = fileName
	}
	files := make(map[string]string, len(models))
	for _, m := range models {
		var content strings.Builder
		for _, dep := range m.Dependencies {
			content.WriteString(fmt.Sprintf("import '%s';\n", fileNameByModel[dep]))
		}
		if len(m.Dependencies) > 0 {
			content.WriteString("\n")
		}
		content.WriteString(m.Declaration)
		files[fileNameByModel[m.Name]] = content.String()
	}
	return Output{Files: files}
}

// uniqueDartFileName returns fileName, or fileName with a numeric suffix
// inserted before its extension ("pet_note_2.dart") if it is already in
// used. This is the filename-aware counterpart of the bare-identifier
// uniquify helpers in internal/gen/{swift,kotlin,dart}: suffixing the
// whole string would produce "pet_note.dart_2", which is not a Dart
// source file name.
func uniqueDartFileName(fileName string, used map[string]bool) string {
	if !used[fileName] {
		return fileName
	}
	base, ext := fileName, ""
	if i := strings.LastIndex(fileName, "."); i >= 0 {
		base, ext = fileName[:i], fileName[i:]
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s_%d%s", base, i, ext)
		if !used[candidate] {
			return candidate
		}
	}
}

// PythonOptions controls GeneratePython's output layout.
type PythonOptions struct {
	Modular bool
}

// GeneratePython renders doc as Python stdlib dataclasses, laid out per
// opts.
//
//   - Monolithic (opts.Modular false): exactly one file, keyed
//     "generated.py", holding every declaration in python.Generate's order
//     (doc.Models name-sorted, then synthesized declarations), except that
//     an alias assignment is moved after every declaration it names (see
//     topoSortModelOutputs); preceded by "from __future__ import
//     annotations" and the merged union of every declaration's Imports.
//   - Modular (opts.Modular true): one file per non-cyclic declaration,
//     keyed "<Name>.py"; every declaration on a dependency cycle (computed
//     from the generated declarations, including synthesized ones — see
//     cyclicModelNames and cyclicFileMembers) is instead grouped into one
//     shared "_cyclic.py" file (see modularPythonOutput's doc comment for
//     why); plus an "__init__.py" barrel re-exporting every declaration
//     from whichever file it landed in.
func GeneratePython(doc *ir.Document, opts PythonOptions) (Output, error) {
	return generatePythonFamily(doc, python.Dataclass, opts.Modular)
}

// PydanticOptions controls GeneratePydantic's output layout.
type PydanticOptions struct {
	Modular bool
}

// GeneratePydantic renders doc as Pydantic BaseModel classes, laid out
// per opts. Layout contract is identical to GeneratePython's (see its
// doc comment) — the two differ only in the Python source python.Generate
// itself produces for each declaration (class header, field defaults,
// alias handling), not in how pkg/engine lays declarations out across
// files.
func GeneratePydantic(doc *ir.Document, opts PydanticOptions) (Output, error) {
	return generatePythonFamily(doc, python.Pydantic, opts.Modular)
}

func generatePythonFamily(doc *ir.Document, style python.Style, modular bool) (Output, error) {
	models, err := python.Generate(doc, style)
	if err != nil {
		return Output{}, err
	}
	if modular {
		return modularPythonOutput(models, cyclicFileMembers(models, cyclicModelNames(models))), nil
	}
	return monolithicPythonOutput(models), nil
}

// cyclicModelNames returns the name of every declaration in models that
// lies on a dependency cycle: a member of a strongly connected component
// of size > 1, or a declaration that depends on itself directly.
//
// Cycle membership is computed from the generated declarations'
// Dependencies rather than from ir.Model.Cyclic because the IR flag only
// exists on a document's own top-level schemas. python.Generate also
// synthesizes declarations for inline objects/enums, and such a
// declaration can be a genuine link in a cycle — e.g. Node has an inline
// object field synthesized as NodeChildren, which itself $refs Node.
// Only the generated dependency graph sees that edge.
//
// Tarjan's algorithm, visiting declarations and their dependencies in
// input order. A dependency naming no declaration in models is ignored.
func cyclicModelNames(models []python.ModelOutput) map[string]bool {
	indexOf := make(map[string]int, len(models))
	for i, m := range models {
		indexOf[m.Name] = i
	}
	const unvisited = -1
	index := make([]int, len(models))
	lowlink := make([]int, len(models))
	onStack := make([]bool, len(models))
	for i := range index {
		index[i] = unvisited
	}
	var stack []int
	next := 0
	cyclic := map[string]bool{}

	var strongConnect func(v int)
	strongConnect = func(v int) {
		index[v] = next
		lowlink[v] = next
		next++
		stack = append(stack, v)
		onStack[v] = true
		for _, dep := range models[v].Dependencies {
			w, ok := indexOf[dep]
			if !ok {
				continue
			}
			if w == v {
				cyclic[models[v].Name] = true
				continue
			}
			if index[w] == unvisited {
				strongConnect(w)
				lowlink[v] = min(lowlink[v], lowlink[w])
			} else if onStack[w] {
				lowlink[v] = min(lowlink[v], index[w])
			}
		}
		if lowlink[v] != index[v] {
			return
		}
		// v is the root of an SCC: pop it off the stack.
		var component []int
		for {
			w := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[w] = false
			component = append(component, w)
			if w == v {
				break
			}
		}
		if len(component) > 1 {
			for _, w := range component {
				cyclic[models[w].Name] = true
			}
		}
	}
	for v := range models {
		if index[v] == unvisited {
			strongConnect(v)
		}
	}
	return cyclic
}

// cyclicFileMembers returns the set of declarations modular output must
// place in the shared "_cyclic.py" file: every declaration in cyclic, plus
// every other declaration that is both reachable from that set and able
// to reach it.
//
// The extra members are needed because bundling every cycle into one
// file contracts all of them into a single node of the file-level import
// graph, which can create a cycle the declaration graph didn't have. With
// two unrelated cycles {A, B} and {C, D} and a non-cyclic M where A -> M
// -> C, _cyclic.py would import M.py while M.py imports _cyclic.py — a
// circular ImportError. Pulling such an M into _cyclic.py as well removes
// it: any remaining file-level cycle would have to pass through the
// contracted node via declarations outside it, and those are exactly the
// ones added here.
func cyclicFileMembers(models []python.ModelOutput, cyclic map[string]bool) map[string]bool {
	if len(cyclic) == 0 {
		return cyclic
	}
	forward := make(map[string][]string, len(models))
	reverse := make(map[string][]string, len(models))
	for _, m := range models {
		for _, dep := range m.Dependencies {
			forward[m.Name] = append(forward[m.Name], dep)
			reverse[dep] = append(reverse[dep], m.Name)
		}
	}
	reachable := func(edges map[string][]string) map[string]bool {
		seen := make(map[string]bool, len(cyclic))
		var queue []string
		for name := range cyclic {
			seen[name] = true
			queue = append(queue, name)
		}
		for len(queue) > 0 {
			n := queue[0]
			queue = queue[1:]
			for _, next := range edges[n] {
				if !seen[next] {
					seen[next] = true
					queue = append(queue, next)
				}
			}
		}
		return seen
	}
	fromCyclic, toCyclic := reachable(forward), reachable(reverse)
	members := make(map[string]bool, len(cyclic))
	for _, m := range models {
		if cyclic[m.Name] || (fromCyclic[m.Name] && toCyclic[m.Name]) {
			members[m.Name] = true
		}
	}
	return members
}

// topoSortModelOutputs orders declarations so that every alias assignment
// (ModelOutput.Alias) comes after each declaration it names — the only
// ordering constraint a Python file has here, since a class's or Enum's
// field annotations are deferred by `from __future__ import annotations`
// but an assignment's right-hand side runs immediately. Without it, a
// name-sorted file like `AllPets = list[Pet]` ahead of `class Pet` raises
// NameError on import; so does a top-level array of an inline object,
// whose synthesized item class is always appended after the alias.
//
// The sort is stable (Kahn's algorithm, always taking the earliest ready
// declaration in input order), so unconstrained declarations keep
// python.Generate's order. Alias-only cycles cannot be expressed in Python
// at all; if one occurs, the unresolvable remainder is emitted in input
// order rather than looping or dropping declarations.
func topoSortModelOutputs(models []python.ModelOutput) []python.ModelOutput {
	indexOf := make(map[string]int, len(models))
	for i, m := range models {
		indexOf[m.Name] = i
	}
	dependents := make([][]int, len(models))
	indegree := make([]int, len(models))
	for i, m := range models {
		if !m.Alias {
			continue
		}
		for _, dep := range m.Dependencies {
			j, ok := indexOf[dep]
			if !ok || j == i {
				continue
			}
			dependents[j] = append(dependents[j], i)
			indegree[i]++
		}
	}

	sorted := make([]python.ModelOutput, 0, len(models))
	emitted := make([]bool, len(models))
	for len(sorted) < len(models) {
		next := -1
		for i := range models {
			if !emitted[i] && indegree[i] == 0 {
				next = i
				break
			}
		}
		if next == -1 {
			for i := range models {
				if !emitted[i] {
					emitted[i] = true
					sorted = append(sorted, models[i])
				}
			}
			break
		}
		emitted[next] = true
		sorted = append(sorted, models[next])
		for _, d := range dependents[next] {
			indegree[d]--
		}
	}
	return sorted
}

func monolithicPythonOutput(models []python.ModelOutput) Output {
	var imports []string
	var sb strings.Builder
	for i, m := range topoSortModelOutputs(models) {
		imports = append(imports, m.Imports...)
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(m.Declaration)
	}
	var header strings.Builder
	header.WriteString("from __future__ import annotations\n")
	for _, imp := range mergePythonImports(imports) {
		header.WriteString(imp)
		header.WriteString("\n")
	}
	header.WriteString("\n")
	header.WriteString(sb.String())
	return Output{Files: map[string]string{"generated.py": header.String()}}
}

// modularPythonOutput writes one file per non-cyclic model (keyed
// "<Name>.py", case-insensitively uniquified like modularSwiftOutput),
// groups every model in cyclic (see cyclicFileMembers) into one shared
// "_cyclic.py" file, and writes an "__init__.py" barrel re-exporting
// every model regardless of which file it landed in.
//
// The cyclic grouping exists because Python raises ImportError on a
// genuine circular top-level import ("from .a import A" at the top of
// b.py, while "from .b import B" sits at the top of a.py, with neither
// module having finished executing far enough to expose the other's
// name yet) — a real restriction Dart's language does not share, so
// modularDartOutput's one-file-per-model approach cannot be copied
// as-is. Bundling every cycle member into one file sidesteps the problem
// entirely: references between declarations in the SAME file need no
// import at all. This is deliberately coarser than one file per cycle
// (every cyclic model in the whole document lands in one shared file,
// even if the document has two unrelated cycles) — correct, just less
// finely split than it could be.
func modularPythonOutput(models []python.ModelOutput, cyclic map[string]bool) Output {
	var cyclicModels, plainModels []python.ModelOutput
	for _, m := range models {
		if cyclic[m.Name] {
			cyclicModels = append(cyclicModels, m)
		} else {
			plainModels = append(plainModels, m)
		}
	}

	fileNameByModel := make(map[string]string, len(models))
	usedLower := map[string]bool{"__init__": true, "_cyclic": true}
	for _, m := range plainModels {
		fileNameByModel[m.Name] = uniqueFileName(m.Name, usedLower) + ".py"
	}
	for _, m := range cyclicModels {
		fileNameByModel[m.Name] = "_cyclic.py"
	}

	files := make(map[string]string, len(models)+1)
	var barrel strings.Builder

	// An Unsupported declaration still gets its own file (holding the
	// explanatory comment, like the other targets' output) but defines no
	// name, so the barrel must not try to import one from it.
	for _, m := range plainModels {
		files[fileNameByModel[m.Name]] = renderPythonModuleFile([]python.ModelOutput{m}, fileNameByModel)
		if !m.Unsupported {
			fmt.Fprintf(&barrel, "from .%s import %s\n", strings.TrimSuffix(fileNameByModel[m.Name], ".py"), m.Name)
		}
	}
	if len(cyclicModels) > 0 {
		files["_cyclic.py"] = renderPythonModuleFile(cyclicModels, fileNameByModel)
		var names []string
		for _, m := range cyclicModels {
			if !m.Unsupported {
				names = append(names, m.Name)
			}
		}
		if len(names) > 0 {
			fmt.Fprintf(&barrel, "from ._cyclic import %s\n", strings.Join(names, ", "))
		}
	}
	files["__init__.py"] = barrel.String()
	return Output{Files: files}
}

// renderPythonModuleFile renders one modular-output file holding every
// model in group, importing whatever non-group dependency each needs
// from its own file (a dependency landing in the SAME file, i.e. two
// cyclic models bundled together, needs no import — fileNameByModel maps
// both to "_cyclic.py", which == thisFile, so it's skipped below). A
// multi-declaration group is ordered by topoSortModelOutputs, for the
// same NameError reason as monolithic output.
func renderPythonModuleFile(group []python.ModelOutput, fileNameByModel map[string]string) string {
	thisFile := fileNameByModel[group[0].Name]
	var imports []string
	var relativeImports []string
	for _, m := range group {
		imports = append(imports, m.Imports...)
		for _, dep := range m.Dependencies {
			depFile, ok := fileNameByModel[dep]
			if !ok || depFile == thisFile {
				continue
			}
			relativeImports = append(relativeImports, fmt.Sprintf("from .%s import %s", strings.TrimSuffix(depFile, ".py"), dep))
		}
	}

	var sb strings.Builder
	sb.WriteString("from __future__ import annotations\n")
	for _, imp := range mergePythonImports(imports) {
		sb.WriteString(imp)
		sb.WriteString("\n")
	}
	for _, imp := range mergePythonImports(relativeImports) {
		sb.WriteString(imp)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	for i, m := range topoSortModelOutputs(group) {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(m.Declaration)
	}
	return sb.String()
}

// mergePythonImports collapses import lines into one line per module:
// every "from M import a, b" line naming the same M is merged into a
// single line importing the sorted union of their names, so a file whose
// declarations need "from pydantic import BaseModel" and "from pydantic
// import BaseModel, ConfigDict, Field" gets only the latter. Any other
// line is kept verbatim (deduplicated). The result is sorted.
func mergePythonImports(lines []string) []string {
	namesByModule := map[string]map[string]bool{}
	other := map[string]bool{}
	for _, line := range lines {
		rest, ok := strings.CutPrefix(line, "from ")
		module, names, ok2 := strings.Cut(rest, " import ")
		if !ok || !ok2 || strings.ContainsAny(names, "()") {
			other[line] = true
			continue
		}
		if namesByModule[module] == nil {
			namesByModule[module] = map[string]bool{}
		}
		for _, name := range strings.Split(names, ",") {
			if name = strings.TrimSpace(name); name != "" {
				namesByModule[module][name] = true
			}
		}
	}
	out := make([]string, 0, len(namesByModule)+len(other))
	for module, set := range namesByModule {
		names := make([]string, 0, len(set))
		for name := range set {
			names = append(names, name)
		}
		sort.Strings(names)
		out = append(out, "from "+module+" import "+strings.Join(names, ", "))
	}
	for line := range other {
		out = append(out, line)
	}
	sort.Strings(out)
	return out
}
