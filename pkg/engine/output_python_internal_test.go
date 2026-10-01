package engine

import (
	"reflect"
	"sort"
	"testing"

	"github.com/tarikomercehajic/openapi2code/internal/gen/python"
	"github.com/tarikomercehajic/openapi2code/internal/ir"
)

func pyNames(models []python.ModelOutput) []string {
	out := make([]string, len(models))
	for i, m := range models {
		out[i] = m.Name
	}
	return out
}

func pyClass(name string, deps ...string) python.ModelOutput {
	return python.ModelOutput{Name: name, Dependencies: deps}
}

func pyAlias(name string, deps ...string) python.ModelOutput {
	return python.ModelOutput{Name: name, Dependencies: deps, Alias: true}
}

func TestTopoSortModelOutputs_AliasMovesAfterItsClass(t *testing.T) {
	// Name-sorted input: AllPets = list[Pet] would run before class Pet.
	got := pyNames(topoSortModelOutputs([]python.ModelOutput{pyAlias("AllPets", "Pet"), pyClass("Pet")}))
	if want := []string{"Pet", "AllPets"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestTopoSortModelOutputs_MixedChain(t *testing.T) {
	// Lists = list[AllPets]; AllPets = list[Pet]; class Pet has a field of
	// type Owner, but class-to-class edges impose no order (annotations
	// are deferred), so Owner stays put after Pet. ItemsItem is the
	// synthesized class of a top-level array of an inline object, always
	// appended after its own alias Items.
	in := []python.ModelOutput{
		pyAlias("AllPets", "Pet"),
		pyAlias("Items", "ItemsItem"),
		pyAlias("Lists", "AllPets"),
		pyClass("Pet", "Owner"),
		pyClass("Owner", "Pet"),
		pyClass("ItemsItem"),
	}
	got := pyNames(topoSortModelOutputs(in))
	want := []string{"Pet", "AllPets", "Lists", "Owner", "ItemsItem", "Items"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	pos := map[string]int{}
	for i, n := range got {
		pos[n] = i
	}
	for _, m := range in {
		if !m.Alias {
			continue
		}
		for _, dep := range m.Dependencies {
			if pos[dep] > pos[m.Name] {
				t.Errorf("alias %s emitted before its dependency %s: %v", m.Name, dep, got)
			}
		}
	}
}

func TestTopoSortModelOutputs_IndependentKeepRelativeOrder(t *testing.T) {
	in := []python.ModelOutput{pyClass("Zeta"), pyAlias("Alpha", "Zeta"), pyClass("Beta"), pyAlias("Gamma")}
	got := pyNames(topoSortModelOutputs(in))
	if want := []string{"Zeta", "Alpha", "Beta", "Gamma"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestTopoSortModelOutputs_NoDependenciesIsNoOp(t *testing.T) {
	in := []python.ModelOutput{pyClass("C"), pyAlias("A"), pyClass("B", "C", "A"), pyAlias("D", "Unknown")}
	got := pyNames(topoSortModelOutputs(in))
	if want := pyNames(in); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want input order %v", got, want)
	}
}

func TestTopoSortModelOutputs_AliasCycleDegradesToInputOrder(t *testing.T) {
	// Unexpressible in Python anyway; must neither hang nor drop anything.
	in := []python.ModelOutput{pyAlias("A", "B"), pyAlias("B", "A"), pyClass("C"), pyAlias("D", "C")}
	got := pyNames(topoSortModelOutputs(in))
	if want := []string{"C", "D", "A", "B"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func TestCyclicModelNames(t *testing.T) {
	cases := []struct {
		name   string
		models []python.ModelOutput
		want   []string
	}{
		{"two-cycle", []python.ModelOutput{pyClass("A", "B"), pyClass("B", "A"), pyClass("C", "A")}, []string{"A", "B"}},
		{"three-plus cycle", []python.ModelOutput{pyClass("A", "B"), pyClass("B", "C"), pyClass("C", "D"), pyClass("D", "A"), pyClass("E", "A")}, []string{"A", "B", "C", "D"}},
		{"self reference", []python.ModelOutput{pyClass("Tree", "Tree"), pyClass("Leaf")}, []string{"Tree"}},
		{"no cycles", []python.ModelOutput{pyClass("A", "B"), pyClass("B", "C"), pyClass("C"), pyAlias("D", "A", "Missing")}, []string{}},
		{"two independent cycles", []python.ModelOutput{pyClass("A", "B"), pyClass("B", "A"), pyClass("C", "D"), pyClass("D", "C"), pyClass("E", "C")}, []string{"A", "B", "C", "D"}},
		{"empty", nil, []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sortedKeys(cyclicModelNames(c.models)); !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// TestCyclicModelNames_SynthesizedDeclarationClosesCycle runs the real
// python.Generate: Node's inline object field is synthesized as
// NodeChildren, which $refs Node. Neither the IR nor any doc.Models entry
// flags NodeChildren as cyclic; only the generated dependency graph does.
func TestCyclicModelNames_SynthesizedDeclarationClosesCycle(t *testing.T) {
	doc := &ir.Document{Models: []*ir.Model{{
		Name:   "Node",
		Cyclic: true,
		Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{Fields: []*ir.Field{{
			Name:     "children",
			Optional: true,
			Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{Fields: []*ir.Field{{
				Name:     "parent",
				Optional: true,
				Type:     &ir.Node{Kind: ir.KindRef, RefName: "Node"},
			}}}},
		}}}},
	}}}
	models, err := python.Generate(doc, python.Dataclass)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got := pyNames(models); !reflect.DeepEqual(got, []string{"Node", "NodeChildren"}) {
		t.Fatalf("unexpected declarations %v", got)
	}
	if got, want := sortedKeys(cyclicModelNames(models)), []string{"Node", "NodeChildren"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	out := modularPythonOutput(models, cyclicFileMembers(models, cyclicModelNames(models)))
	if _, ok := out.Files["NodeChildren.py"]; ok {
		t.Errorf("NodeChildren must share _cyclic.py with Node, got files %v", sortedFileKeys(out.Files))
	}
}

func TestCyclicFileMembers_PullsInBridgeBetweenCycles(t *testing.T) {
	// {A,B} and {C,D} are unrelated cycles; M bridges A -> M -> C. With
	// both cycles bundled into _cyclic.py, leaving M in M.py would make
	// _cyclic.py and M.py import each other. N only leads into a cycle,
	// and O is only reachable from one, so neither needs moving.
	models := []python.ModelOutput{
		pyClass("A", "B", "M"), pyClass("B", "A"),
		pyClass("C", "D"), pyClass("D", "C"),
		pyClass("M", "C"),
		pyClass("N", "A"),
		pyClass("O"), pyClass("P", "O"),
	}
	models[0].Dependencies = append(models[0].Dependencies, "P")
	cyclic := cyclicModelNames(models)
	if got, want := sortedKeys(cyclic), []string{"A", "B", "C", "D"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cyclicModelNames = %v, want %v", got, want)
	}
	if got, want := sortedKeys(cyclicFileMembers(models, cyclic)), []string{"A", "B", "C", "D", "M"}; !reflect.DeepEqual(got, want) {
		t.Errorf("cyclicFileMembers = %v, want %v", got, want)
	}
}

func TestMergePythonImports(t *testing.T) {
	got := mergePythonImports([]string{
		"from pydantic import BaseModel",
		"from pydantic import BaseModel, ConfigDict, Field",
		"from enum import Enum",
		"from enum import Enum",
		"from ._cyclic import B",
		"from ._cyclic import A",
		"import json",
	})
	want := []string{
		"from ._cyclic import A, B",
		"from enum import Enum",
		"from pydantic import BaseModel, ConfigDict, Field",
		"import json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func sortedFileKeys(files map[string]string) []string {
	out := make([]string, 0, len(files))
	for k := range files {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
