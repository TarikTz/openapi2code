package wasmapi_test

import (
	"strings"
	"testing"

	"github.com/tarikomercehajic/openapi2code/internal/wasmapi"
)

const simpleSpec = `{
  "openapi": "3.0.3",
  "components": {
    "schemas": {
      "Pet": {
        "type": "object",
        "properties": {"name": {"type": "string"}},
        "required": ["name"]
      }
    }
  }
}`

// TestGenerate_TargetAndLayoutDispatch covers every {target, modular}
// combination, so a swapped flag or a crossed-over dispatch arm can't slip
// through: each case asserts on a file name only the right layout produces
// and on content only the right generator emits.
func TestGenerate_TargetAndLayoutDispatch(t *testing.T) {
	tests := []struct {
		name       string
		target     string
		modular    bool
		wantFile   string
		wantString string
		// absentFile must not be in the output — in modular mode index.ts is
		// a barrel and the model lives in its own file, so the presence or
		// absence of Pet.ts is what actually distinguishes the two layouts.
		absentFile string
	}{
		{
			name: "ts monolithic", target: "ts", modular: false,
			wantFile: "index.ts", wantString: "export interface Pet {", absentFile: "Pet.ts",
		},
		{
			name: "ts modular", target: "ts", modular: true,
			wantFile: "Pet.ts", wantString: "export interface Pet {",
		},
		{
			name: "zod monolithic", target: "zod", modular: false,
			wantFile: "index.ts", wantString: "export const PetSchema = z.object({", absentFile: "Pet.ts",
		},
		{
			name: "zod modular", target: "zod", modular: true,
			wantFile: "Pet.ts", wantString: "export const PetSchema = z.object({",
		},
		{
			name: "swift monolithic", target: "swift", modular: false,
			wantFile: "Generated.swift", wantString: "public struct Pet: Codable {", absentFile: "Pet.swift",
		},
		{
			name: "swift modular", target: "swift", modular: true,
			wantFile: "Pet.swift", wantString: "public struct Pet: Codable {",
		},
		{
			name: "kotlin monolithic", target: "kotlin", modular: false,
			wantFile: "Generated.kt", wantString: "data class Pet(", absentFile: "Pet.kt",
		},
		{
			name: "kotlin modular", target: "kotlin", modular: true,
			wantFile: "Pet.kt", wantString: "data class Pet(",
		},
		{
			name: "dart monolithic", target: "dart", modular: false,
			wantFile: "generated.dart", wantString: "class Pet {", absentFile: "pet.dart",
		},
		{
			name: "dart modular", target: "dart", modular: true,
			wantFile: "pet.dart", wantString: "class Pet {",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := wasmapi.Generate(simpleSpec, tt.target, tt.modular)
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			content, ok := out.Files[tt.wantFile]
			if !ok {
				t.Fatalf("expected %s in output, got files: %v", tt.wantFile, fileNames(out.Files))
			}
			if !strings.Contains(content, tt.wantString) {
				t.Errorf("expected %q in %s, got:\n%s", tt.wantString, tt.wantFile, content)
			}
			if tt.absentFile != "" {
				if _, ok := out.Files[tt.absentFile]; ok {
					t.Errorf("expected no %s in monolithic output, got files: %v", tt.absentFile, fileNames(out.Files))
				}
			}
			// Guard against a target mix-up in either direction: a zod file
			// always imports zod, a plain TS file never does.
			importsZod := strings.Contains(content, `import { z } from "zod"`)
			if tt.target == "zod" && !importsZod {
				t.Errorf("expected the zod import in %s, got:\n%s", tt.wantFile, content)
			}
			if tt.target == "ts" && importsZod {
				t.Errorf("unexpected zod import in a ts target's %s, got:\n%s", tt.wantFile, content)
			}
		})
	}
}

func fileNames(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	return names
}

func TestGenerate_UnsupportedTarget(t *testing.T) {
	_, err := wasmapi.Generate(simpleSpec, "cobol", false)
	if err == nil {
		t.Fatal("expected error for unsupported target")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("expected \"not supported\" in error, got: %v", err)
	}
}

// TestGenerate_UnsupportedTargetWithInvalidSpec pins down which error wins
// when both inputs are bad. The target is validated before the spec is
// parsed, so the caller is told about the thing they can actually fix from
// the target picker rather than being handed a parse error about a spec
// that was never going to be generated for that target anyway.
func TestGenerate_UnsupportedTargetWithInvalidSpec(t *testing.T) {
	for _, specText := range []string{"", "not a spec at all"} {
		_, err := wasmapi.Generate(specText, "cobol", false)
		if err == nil {
			t.Fatalf("expected error for spec %q with an unsupported target", specText)
		}
		if !strings.Contains(err.Error(), "not supported") {
			t.Errorf("expected the unsupported-target error to win for spec %q, got: %v", specText, err)
		}
	}
}

func TestGenerate_ParseError(t *testing.T) {
	_, err := wasmapi.Generate("", "ts", false)
	if err == nil {
		t.Fatal("expected error for an empty/unparseable spec")
	}
}
