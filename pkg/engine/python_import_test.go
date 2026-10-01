package engine_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tarikomercehajic/openapi2code/pkg/engine"
)

// pythonImportCheck imports the generated package and then forces every
// class's annotations to resolve: typing.get_type_hints for dataclasses,
// model_rebuild(force=True) for Pydantic models. Importing alone would
// catch a circular ImportError or an alias NameError, but a missing
// cross-file import only surfaces once annotations are evaluated.
const pythonImportCheck = `
import dataclasses, importlib, typing
pkg = importlib.import_module("pkg")
for name in pkg.__dict__:
    obj = getattr(pkg, name)
    if not isinstance(obj, type):
        continue
    if dataclasses.is_dataclass(obj):
        typing.get_type_hints(obj)
    elif hasattr(obj, "model_rebuild"):
        obj.model_rebuild(force=True)
`

// Inline specs exercising the shapes string-comparison tests cannot
// verify: a synthesized inline object closing a $ref cycle, two unrelated
// cycles bridged by a non-cyclic model, and alias assignments that must
// follow the classes they name.
var pythonImportSpecs = map[string]string{
	"synthesized-cycle": `
openapi: "3.0.3"
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Node:
      type: object
      properties:
        children:
          type: object
          properties:
            parent: {$ref: "#/components/schemas/Node"}
`,
	"bridged-cycles": `
openapi: "3.0.3"
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      type: object
      properties:
        b: {$ref: "#/components/schemas/B"}
        m: {$ref: "#/components/schemas/M"}
    B:
      type: object
      properties:
        a: {$ref: "#/components/schemas/A"}
    C:
      type: object
      properties:
        d: {$ref: "#/components/schemas/D"}
    D:
      type: object
      properties:
        c: {$ref: "#/components/schemas/C"}
    M:
      type: object
      properties:
        c: {$ref: "#/components/schemas/C"}
    Z:
      type: object
      properties:
        a: {$ref: "#/components/schemas/A"}
`,
	"alias-in-cycle": `
openapi: "3.0.3"
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Forest:
      type: array
      items: {$ref: "#/components/schemas/Tree"}
    Tree:
      type: object
      properties:
        forest: {$ref: "#/components/schemas/Forest"}
`,
}

// TestModularPythonOutput_ImportsInPython writes modular Python and
// Pydantic output to a real package directory and imports it with
// python3. Skips (never fails) when python3 is missing or -short is set;
// the Pydantic half also skips when pydantic isn't importable.
func TestModularPythonOutput_ImportsInPython(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping python3 import check of modular Python output in -short mode")
	}
	python3, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not found on PATH; skipping import check of modular Python output (install Python 3.10+ to get real verification)")
	}
	hasPydantic := exec.Command(python3, "-c", "import pydantic").Run() == nil

	specs := map[string][]byte{}
	for _, fixture := range []string{"circular", "petstore", "composition", "enum_types", "nullable_enum", "alias_ordering"} {
		data, err := os.ReadFile(filepath.Join("testdata", fixture+".yaml"))
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}
		specs[fixture] = data
	}
	for name, spec := range pythonImportSpecs {
		specs[name] = []byte(spec)
	}
	specNames := make([]string, 0, len(specs))
	for name := range specs {
		specNames = append(specNames, name)
	}
	sort.Strings(specNames)

	targets := []struct {
		name     string
		generate func([]byte) (engine.Output, error)
		pydantic bool
	}{
		{"python", func(data []byte) (engine.Output, error) {
			doc, err := engine.Parse(data)
			if err != nil {
				return engine.Output{}, err
			}
			return engine.GeneratePython(doc, engine.PythonOptions{Modular: true})
		}, false},
		{"pydantic", func(data []byte) (engine.Output, error) {
			doc, err := engine.Parse(data)
			if err != nil {
				return engine.Output{}, err
			}
			return engine.GeneratePydantic(doc, engine.PydanticOptions{Modular: true})
		}, true},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, target := range targets {
		for _, specName := range specNames {
			target, specName := target, specName
			t.Run(target.name+"/"+specName, func(t *testing.T) {
				if target.pydantic && !hasPydantic {
					t.Skip("pydantic package not importable from python3; skipping (pip install pydantic to get real verification)")
				}
				out, err := target.generate(specs[specName])
				if err != nil {
					t.Fatalf("generate: %v", err)
				}
				root := t.TempDir()
				pkgDir := filepath.Join(root, "pkg")
				if err := os.Mkdir(pkgDir, 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				for fileName, content := range out.Files {
					if err := os.WriteFile(filepath.Join(pkgDir, fileName), []byte(content), 0o644); err != nil {
						t.Fatalf("write %s: %v", fileName, err)
					}
				}
				cmd := exec.CommandContext(ctx, python3, "-c", pythonImportCheck)
				cmd.Dir = root
				if output, err := cmd.CombinedOutput(); err != nil {
					var dump strings.Builder
					for _, fileName := range fileNames(out.Files) {
						dump.WriteString("--- " + fileName + " ---\n" + out.Files[fileName])
					}
					t.Errorf("generated package does not import cleanly:\n%s\n%s", strings.TrimSpace(string(output)), dump.String())
				}
			})
		}
	}
}
