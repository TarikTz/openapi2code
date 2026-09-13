package engine

import (
	"strings"
	"testing"

	"github.com/tarikomercehajic/openapi2code/internal/gen/zod"
)

// declIndex returns the byte offset of a schema constant's declaration in
// the monolithic file, or -1 if it is missing.
func declIndex(content, name string) int {
	return strings.Index(content, "export const "+name+"Schema")
}

// TestMonolithicZodOutput_DeclaresDependenciesFirst is the regression test
// for the temporal-dead-zone bug: a non-cyclic model's body runs the moment
// its const is declared, so every schema it names must already exist. Input
// order here is alphabetical (as zod.Generate produces), and the
// alphabetically earlier model is the one that depends on the later one.
func TestMonolithicZodOutput_DeclaresDependenciesFirst(t *testing.T) {
	models := []zod.ModelOutput{
		{
			Name:         "Pet",
			Declaration:  "export const PetSchema = z.object({ tags: z.array(TagSchema).optional(), });\nexport type Pet = z.infer<typeof PetSchema>;\n",
			Dependencies: []string{"Tag"},
		},
		{
			Name:        "Tag",
			Declaration: "export const TagSchema = z.object({ name: z.string(), });\nexport type Tag = z.infer<typeof TagSchema>;\n",
		},
	}
	content := monolithicZodOutput(models).Files["index.ts"]

	tagAt, petAt := declIndex(content, "Tag"), declIndex(content, "Pet")
	if tagAt < 0 || petAt < 0 {
		t.Fatalf("expected both declarations in output, got:\n%s", content)
	}
	if tagAt > petAt {
		t.Errorf("TagSchema (offset %d) must be declared before PetSchema (offset %d), got:\n%s", tagAt, petAt, content)
	}
}

// TestMonolithicZodOutput_TransitiveOrdering checks a three-deep chain that
// alphabetical order gets exactly backwards.
func TestMonolithicZodOutput_TransitiveOrdering(t *testing.T) {
	models := []zod.ModelOutput{
		{Name: "A", Declaration: "export const ASchema = z.object({ b: BSchema, });\n", Dependencies: []string{"B"}},
		{Name: "B", Declaration: "export const BSchema = z.object({ c: CSchema, });\n", Dependencies: []string{"C"}},
		{Name: "C", Declaration: "export const CSchema = z.string();\n", Dependencies: nil},
	}
	content := monolithicZodOutput(models).Files["index.ts"]

	cAt, bAt, aAt := declIndex(content, "C"), declIndex(content, "B"), declIndex(content, "A")
	if !(cAt < bAt && bAt < aAt) {
		t.Errorf("expected declaration order C, B, A; got offsets C=%d B=%d A=%d in:\n%s", cAt, bAt, aAt, content)
	}
}

// TestMonolithicZodOutput_CyclicImposesNoOrdering checks that a cyclic
// model's own dependencies impose no ordering constraint (its body lives
// inside z.lazy's closure), while models that reference the cyclic model
// still get ordered after it — its const binding is read eagerly by them.
func TestMonolithicZodOutput_CyclicImposesNoOrdering(t *testing.T) {
	models := []zod.ModelOutput{
		{
			Name:         "Comment",
			Declaration:  "export const CommentSchema = z.object({ thread: ThreadSchema, });\n",
			Dependencies: []string{"Thread"},
		},
		{
			Name:         "Thread",
			Declaration:  "export const ThreadSchema: z.ZodType<Thread> = z.lazy(() => z.object({ author: UserSchema, parent: ThreadSchema.optional(), }));\n",
			Dependencies: []string{"User"},
			Cyclic:       true,
		},
		{
			Name:        "User",
			Declaration: "export const UserSchema = z.object({ name: z.string(), });\n",
		},
	}
	content := monolithicZodOutput(models).Files["index.ts"]

	threadAt, commentAt, userAt := declIndex(content, "Thread"), declIndex(content, "Comment"), declIndex(content, "User")
	if threadAt > commentAt {
		t.Errorf("ThreadSchema (%d) must precede CommentSchema (%d), which reads it eagerly:\n%s", threadAt, commentAt, content)
	}
	// Thread is cyclic, so its own dependency on User is deferred and must
	// NOT drag User ahead of it — User keeps its input (alphabetical) slot.
	if userAt < threadAt {
		t.Errorf("cyclic Thread's deferred dependency must not force User (%d) before Thread (%d):\n%s", userAt, threadAt, content)
	}
}

// TestTopoSortForMonolithic_StableWhenUnconstrained checks that models with
// no ordering constraints between them keep their input (alphabetical)
// order, so output stays stable for unrelated specs.
func TestTopoSortForMonolithic_StableWhenUnconstrained(t *testing.T) {
	models := []zod.ModelOutput{
		{Name: "Alpha"}, {Name: "Beta"}, {Name: "Gamma"}, {Name: "Delta"},
	}
	got := topoSortForMonolithic(models)
	want := []string{"Alpha", "Beta", "Gamma", "Delta"}
	for i, m := range got {
		if m.Name != want[i] {
			t.Fatalf("order changed: got %v, want %v", names(got), want)
		}
	}
}

// TestTopoSortForMonolithic_UnknownDependencyIgnored checks the defensive
// path: a dependency naming no model in this document must not panic or
// drop a model from the output.
func TestTopoSortForMonolithic_UnknownDependencyIgnored(t *testing.T) {
	models := []zod.ModelOutput{
		{Name: "Only", Dependencies: []string{"NotAModelHere", "Only"}},
	}
	got := topoSortForMonolithic(models)
	if len(got) != 1 || got[0].Name != "Only" {
		t.Errorf("got %v, want [Only]", names(got))
	}
}

func names(models []zod.ModelOutput) []string {
	out := make([]string, len(models))
	for i, m := range models {
		out[i] = m.Name
	}
	return out
}
