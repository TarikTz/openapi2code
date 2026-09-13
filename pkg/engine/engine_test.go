package engine_test

import (
	"strings"
	"testing"

	"github.com/tarikomercehajic/openapi2code/pkg/engine"
)

const simpleSpec = `{
  "openapi": "3.0.3",
  "components": {
    "schemas": {
      "Pet": {
        "type": "object",
        "properties": {
          "name": {"type": "string"}
        },
        "required": ["name"]
      }
    }
  }
}`

const twoModelSpec = `{
  "openapi": "3.0.3",
  "components": {
    "schemas": {
      "Category": {
        "type": "object",
        "properties": {"name": {"type": "string"}},
        "required": ["name"]
      },
      "Pet": {
        "type": "object",
        "properties": {
          "category": {"$ref": "#/components/schemas/Category"}
        }
      }
    }
  }
}`

const collidingNameSpec = `{
  "openapi": "3.0.3",
  "components": {
    "schemas": {
      "Pet-Status": {"type": "string", "enum": ["a"]},
      "Pet.Status": {"type": "string", "enum": ["b"]}
    }
  }
}`

const indexNameSpec = `{
  "openapi": "3.0.3",
  "components": {
    "schemas": {
      "index": {"type": "object", "properties": {"id": {"type": "string"}}},
      "Pet": {"type": "object", "properties": {"id": {"type": "string"}}}
    }
  }
}`

// caseCollisionSpec has two schemas whose sanitized identifiers are
// distinct only by case ("Pet" vs "pet") — legitimate, DISTINCT
// identifiers in every target language (they coexist fine in monolithic
// output), but a case-insensitive filesystem (macOS's default APFS,
// Windows' NTFS) treats "Pet.ts" and "pet.ts" as the same path.
const caseCollisionSpec = `{
  "openapi": "3.0.3",
  "components": {
    "schemas": {
      "Pet": {"type": "object", "properties": {"name": {"type": "string"}}},
      "pet": {"type": "object", "properties": {"nickname": {"type": "string"}}}
    }
  }
}`

func TestGenerateTS_Modular_CollidingNamesKeepBothModels(t *testing.T) {
	doc, err := engine.Parse([]byte(collidingNameSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := engine.GenerateTS(doc, engine.TSOptions{Modular: true})
	if err != nil {
		t.Fatalf("GenerateTS: %v", err)
	}
	// Two models plus the barrel: neither model may be overwritten.
	if len(out.Files) != 3 {
		t.Fatalf("expected 3 files, got %d: %v", len(out.Files), fileNames(out.Files))
	}
	barrel := out.Files["index.ts"]
	if strings.Count(barrel, "export * from") != 2 {
		t.Errorf("expected 2 distinct barrel exports, got:\n%s", barrel)
	}
	if strings.Contains(barrel, `export * from "./Pet_Status";`) == false ||
		strings.Contains(barrel, `export * from "./Pet_Status_2";`) == false {
		t.Errorf("expected barrel to export both uniquified models, got:\n%s", barrel)
	}
}

func TestGenerateTS_Modular_ModelNamedIndexDoesNotClobberBarrel(t *testing.T) {
	doc, err := engine.Parse([]byte(indexNameSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := engine.GenerateTS(doc, engine.TSOptions{Modular: true})
	if err != nil {
		t.Fatalf("GenerateTS: %v", err)
	}
	if len(out.Files) != 3 {
		t.Fatalf("expected 3 files (Pet.ts, renamed index model, index.ts), got %d: %v", len(out.Files), fileNames(out.Files))
	}
	barrel, ok := out.Files["index.ts"]
	if !ok {
		t.Fatal("expected barrel index.ts")
	}
	if !strings.Contains(barrel, "export * from") {
		t.Errorf("barrel was clobbered by the model named 'index':\n%s", barrel)
	}
	if strings.Contains(barrel, `export * from "./index";`) {
		t.Errorf("barrel must not re-export itself:\n%s", barrel)
	}
}

// TestGenerateTS_Modular_CaseInsensitiveCollisionKeepsBothModels guards
// against silent data loss on a case-insensitive filesystem (macOS's
// default APFS, Windows' NTFS): before the fix, "Pet" and "pet" both
// generated a file literally named "Pet.ts"/"pet.ts", which write to the
// SAME path there, so the second write silently overwrote the first on
// disk with no error — while the barrel still exported both names as
// though they lived in two separate files.
func TestGenerateTS_Modular_CaseInsensitiveCollisionKeepsBothModels(t *testing.T) {
	doc, err := engine.Parse([]byte(caseCollisionSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := engine.GenerateTS(doc, engine.TSOptions{Modular: true})
	if err != nil {
		t.Fatalf("GenerateTS: %v", err)
	}
	if len(out.Files) != 3 {
		t.Fatalf("expected 3 files (Pet.ts, a disambiguated pet file, index.ts), got %d: %v", len(out.Files), fileNames(out.Files))
	}
	petDecl, ok := out.Files["Pet.ts"]
	if !ok {
		t.Fatalf("expected Pet.ts, got: %v", fileNames(out.Files))
	}
	if !strings.Contains(petDecl, "interface Pet {") || !strings.Contains(petDecl, "name") {
		t.Errorf("expected Pet.ts to hold the 'Pet' model, got:\n%s", petDecl)
	}
	lowerDecl, ok := out.Files["pet_2.ts"]
	if !ok {
		t.Fatalf("expected the second model at a disambiguated path (pet_2.ts), got: %v", fileNames(out.Files))
	}
	if !strings.Contains(lowerDecl, "interface pet {") || !strings.Contains(lowerDecl, "nickname") {
		t.Errorf("expected pet_2.ts to hold the 'pet' model, got:\n%s", lowerDecl)
	}
	barrel := out.Files["index.ts"]
	if !strings.Contains(barrel, `export * from "./Pet";`) || !strings.Contains(barrel, `export * from "./pet_2";`) {
		t.Errorf("expected the barrel to export both disambiguated paths, got:\n%s", barrel)
	}
}

// TestGenerateZod_Modular_CaseInsensitiveCollisionKeepsBothModels is
// TestGenerateTS_Modular_CaseInsensitiveCollisionKeepsBothModels' Zod
// counterpart — modularZodOutput has its own separate (but identically
// shaped) fix.
func TestGenerateZod_Modular_CaseInsensitiveCollisionKeepsBothModels(t *testing.T) {
	doc, err := engine.Parse([]byte(caseCollisionSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := engine.GenerateZod(doc, engine.ZodOptions{Modular: true})
	if err != nil {
		t.Fatalf("GenerateZod: %v", err)
	}
	if len(out.Files) != 3 {
		t.Fatalf("expected 3 files, got %d: %v", len(out.Files), fileNames(out.Files))
	}
	if _, ok := out.Files["Pet.ts"]; !ok {
		t.Fatalf("expected Pet.ts, got: %v", fileNames(out.Files))
	}
	if _, ok := out.Files["pet_2.ts"]; !ok {
		t.Fatalf("expected the second model at a disambiguated path (pet_2.ts), got: %v", fileNames(out.Files))
	}
	barrel := out.Files["index.ts"]
	if !strings.Contains(barrel, `export * from "./Pet";`) || !strings.Contains(barrel, `export * from "./pet_2";`) {
		t.Errorf("expected the barrel to export both disambiguated paths, got:\n%s", barrel)
	}
}

// TestGenerateSwift_Modular_CaseInsensitiveCollisionKeepsBothModels
// covers Swift's simpler case: no cross-file imports need updating
// (Swift files in one target see every declared name automatically), so
// only the file KEY needs disambiguating.
func TestGenerateSwift_Modular_CaseInsensitiveCollisionKeepsBothModels(t *testing.T) {
	doc, err := engine.Parse([]byte(caseCollisionSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := engine.GenerateSwift(doc, engine.SwiftOptions{Modular: true})
	if err != nil {
		t.Fatalf("GenerateSwift: %v", err)
	}
	if len(out.Files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(out.Files), fileNames(out.Files))
	}
	petDecl, ok := out.Files["Pet.swift"]
	if !ok || !strings.Contains(petDecl, "struct Pet:") {
		t.Errorf("expected Pet.swift to hold the 'Pet' model, got: %v", fileNames(out.Files))
	}
	lowerDecl, ok := out.Files["pet_2.swift"]
	if !ok || !strings.Contains(lowerDecl, "struct pet:") {
		t.Errorf("expected pet_2.swift to hold the 'pet' model, got: %v", fileNames(out.Files))
	}
}

// TestGenerateKotlin_Modular_CaseInsensitiveCollisionKeepsBothModels
// mirrors the Swift test above for Kotlin, which has the identical
// no-imports-between-files shape.
func TestGenerateKotlin_Modular_CaseInsensitiveCollisionKeepsBothModels(t *testing.T) {
	doc, err := engine.Parse([]byte(caseCollisionSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := engine.GenerateKotlin(doc, engine.KotlinOptions{Modular: true})
	if err != nil {
		t.Fatalf("GenerateKotlin: %v", err)
	}
	if len(out.Files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(out.Files), fileNames(out.Files))
	}
	petDecl, ok := out.Files["Pet.kt"]
	if !ok || !strings.Contains(petDecl, "data class Pet(") {
		t.Errorf("expected Pet.kt to hold the 'Pet' model, got: %v", fileNames(out.Files))
	}
	lowerDecl, ok := out.Files["pet_2.kt"]
	if !ok || !strings.Contains(lowerDecl, "data class pet(") {
		t.Errorf("expected pet_2.kt to hold the 'pet' model, got: %v", fileNames(out.Files))
	}
}

func TestParseAndGenerateTS_Monolithic(t *testing.T) {
	doc, err := engine.Parse([]byte(simpleSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := engine.GenerateTS(doc, engine.TSOptions{Modular: false})
	if err != nil {
		t.Fatalf("GenerateTS: %v", err)
	}
	content, ok := out.Files["index.ts"]
	if !ok {
		t.Fatal("expected index.ts in output")
	}
	if !strings.Contains(content, "export interface Pet {") {
		t.Errorf("expected Pet interface, got:\n%s", content)
	}
	if len(out.Files) != 1 {
		t.Errorf("expected exactly 1 file for monolithic output, got %d", len(out.Files))
	}
}

// TestParseAndGenerateTS_OpenAPI31NullableTypeArrayMatchesV3Nullable proves
// the actual point of OpenAPI 3.1 support end to end: a 3.1 document using
// `type: ["string", "null"]` (3.1 dropped the `nullable` keyword in favor
// of this JSON Schema 2020-12 form) must generate byte-identical TS to the
// equivalent 3.0 document using `type: "string", nullable: true` — proof
// that internal/spec's UnmarshalJSON normalization means nothing past
// RawSchema needs to know 3.1 exists at all.
func TestParseAndGenerateTS_OpenAPI31NullableTypeArrayMatchesV3Nullable(t *testing.T) {
	const v30Spec = `{
	  "openapi": "3.0.3",
	  "components": {
	    "schemas": {
	      "Pet": {
	        "type": "object",
	        "properties": {
	          "name": {"type": "string", "nullable": true}
	        },
	        "required": ["name"]
	      }
	    }
	  }
	}`
	const v31Spec = `{
	  "openapi": "3.1.0",
	  "components": {
	    "schemas": {
	      "Pet": {
	        "type": "object",
	        "properties": {
	          "name": {"type": ["string", "null"]}
	        },
	        "required": ["name"]
	      }
	    }
	  }
	}`

	genTS := func(spec string) string {
		t.Helper()
		doc, err := engine.Parse([]byte(spec))
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		out, err := engine.GenerateTS(doc, engine.TSOptions{Modular: false})
		if err != nil {
			t.Fatalf("GenerateTS: %v", err)
		}
		return out.Files["index.ts"]
	}

	v30Output := genTS(v30Spec)
	v31Output := genTS(v31Spec)
	if v30Output != v31Output {
		t.Errorf("expected identical output, got:\n--- 3.0 (nullable keyword) ---\n%s\n--- 3.1 (type array) ---\n%s", v30Output, v31Output)
	}
	if !strings.Contains(v31Output, "name: string | null") {
		t.Errorf("expected a required, nullable string field, got:\n%s", v31Output)
	}
}

func TestGenerateTS_Modular(t *testing.T) {
	doc, err := engine.Parse([]byte(twoModelSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := engine.GenerateTS(doc, engine.TSOptions{Modular: true})
	if err != nil {
		t.Fatalf("GenerateTS: %v", err)
	}
	if _, ok := out.Files["Category.ts"]; !ok {
		t.Error("expected Category.ts")
	}
	petFile, ok := out.Files["Pet.ts"]
	if !ok {
		t.Fatal("expected Pet.ts")
	}
	if !strings.Contains(petFile, `import type { Category } from "./Category";`) {
		t.Errorf("expected import statement in Pet.ts, got:\n%s", petFile)
	}
	barrel, ok := out.Files["index.ts"]
	if !ok {
		t.Fatal("expected barrel index.ts")
	}
	if !strings.Contains(barrel, `export * from "./Category";`) || !strings.Contains(barrel, `export * from "./Pet";`) {
		t.Errorf("expected barrel exports for both models, got:\n%s", barrel)
	}
	if len(out.Files) != 3 {
		t.Errorf("expected 3 files (Category.ts, Pet.ts, index.ts), got %d: %v", len(out.Files), out.Files)
	}
}

func TestGenerateZod_Monolithic(t *testing.T) {
	doc, err := engine.Parse([]byte(simpleSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := engine.GenerateZod(doc, engine.ZodOptions{Modular: false})
	if err != nil {
		t.Fatalf("GenerateZod: %v", err)
	}
	content, ok := out.Files["index.ts"]
	if !ok {
		t.Fatal("expected index.ts in output")
	}
	if !strings.Contains(content, `import { z } from "zod";`) {
		t.Errorf("expected zod import, got:\n%s", content)
	}
	if !strings.Contains(content, "export const PetSchema = z.object({") {
		t.Errorf("expected PetSchema, got:\n%s", content)
	}
	if len(out.Files) != 1 {
		t.Errorf("expected exactly 1 file for monolithic output, got %d", len(out.Files))
	}
}

func TestGenerateSwift_Modular_OneFilePerModelNoBarrel(t *testing.T) {
	doc, err := engine.Parse([]byte(simpleSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := engine.GenerateSwift(doc, engine.SwiftOptions{Modular: true})
	if err != nil {
		t.Fatalf("GenerateSwift: %v", err)
	}
	if _, ok := out.Files["index.swift"]; ok {
		t.Error("expected no barrel file for modular Swift output")
	}
	petFile, ok := out.Files["Pet.swift"]
	if !ok {
		t.Fatalf("expected Pet.swift, got files: %v", fileNames(out.Files))
	}
	if !strings.Contains(petFile, "public struct Pet: Codable {") {
		t.Errorf("expected Pet.swift to contain the Pet struct, got:\n%s", petFile)
	}
	if strings.Contains(petFile, "import") {
		t.Errorf("expected no import statements in modular Swift output (same-target visibility needs none), got:\n%s", petFile)
	}
}

func TestGenerateKotlin_Modular_OneFilePerModelNoBarrel(t *testing.T) {
	doc, err := engine.Parse([]byte(simpleSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := engine.GenerateKotlin(doc, engine.KotlinOptions{Modular: true})
	if err != nil {
		t.Fatalf("GenerateKotlin: %v", err)
	}
	if _, ok := out.Files["index.kt"]; ok {
		t.Error("expected no barrel file for modular Kotlin output")
	}
	petFile, ok := out.Files["Pet.kt"]
	if !ok {
		t.Fatalf("expected Pet.kt, got files: %v", fileNames(out.Files))
	}
	if !strings.Contains(petFile, "data class Pet(") {
		t.Errorf("expected Pet.kt to contain the Pet data class, got:\n%s", petFile)
	}
	if strings.Contains(petFile, "import") {
		t.Errorf("expected no import statements in modular Kotlin output (default-package visibility needs none), got:\n%s", petFile)
	}
}

// TestGenerateDart_Modular_OneFilePerModelWithImportsNoBarrel exercises
// modular Dart output's one point of difference from Swift/Kotlin: Dart
// has no implicit same-package visibility, so a file whose declaration
// references another model's type must explicitly import that model's
// file. twoModelSpec already has the needed Pet-references-Category
// shape (see TestGenerateZod_Modular below), so it's reused here rather
// than defining a new one-off spec constant.
func TestGenerateDart_Modular_OneFilePerModelWithImportsNoBarrel(t *testing.T) {
	doc, err := engine.Parse([]byte(twoModelSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := engine.GenerateDart(doc, engine.DartOptions{Modular: true})
	if err != nil {
		t.Fatalf("GenerateDart: %v", err)
	}
	if _, ok := out.Files["index.dart"]; ok {
		t.Error("expected no barrel file for modular Dart output")
	}
	petFile, ok := out.Files["pet.dart"]
	if !ok {
		t.Fatalf("expected pet.dart (snake_case file name), got files: %v", fileNames(out.Files))
	}
	if !strings.Contains(petFile, "import 'category.dart';") {
		t.Errorf("expected pet.dart to import category.dart, got:\n%s", petFile)
	}
	if !strings.Contains(petFile, "class Pet {") {
		t.Errorf("expected pet.dart to contain the Pet class, got:\n%s", petFile)
	}
}

// petAliasSpec gives Pet a field typed as a scalar alias (PetId), a field
// typed as a named array alias over a $ref element (Categories wrapping
// Category), and a field $ref-ing a hyphenated schema name (Pet-Note) —
// the three shapes task-6's review found modular Dart output missing an
// import for.
const petAliasSpec = `{
  "openapi": "3.0.3",
  "components": {
    "schemas": {
      "PetId": {"type": "string"},
      "Category": {
        "type": "object",
        "properties": {"name": {"type": "string"}},
        "required": ["name"]
      },
      "Categories": {
        "type": "array",
        "items": {"$ref": "#/components/schemas/Category"}
      },
      "Pet-Note": {
        "type": "object",
        "properties": {"text": {"type": "string"}},
        "required": ["text"]
      },
      "Pet": {
        "type": "object",
        "properties": {
          "id": {"$ref": "#/components/schemas/PetId"},
          "categories": {"$ref": "#/components/schemas/Categories"},
          "note": {"$ref": "#/components/schemas/Pet-Note"}
        },
        "required": ["id", "categories", "note"]
      }
    }
  }
}`

// TestGenerateDart_Modular_AliasAndHyphenatedNameFieldsGetCorrectImports
// pins the fix for Critical Finding 3 (task-6 review): a field's declared
// Dart type always comes from its own (possibly alias) type, not from
// whatever effectiveRefNode resolves it to for toJson/fromJson dispatch
// purposes, and every dependency name — whichever of those two sources it
// came from — must be routed through the resolved (sanitized) Dart
// identifier before it's used to key pkg/engine's fileNameByModel, or an
// import silently goes missing (scalar/array alias case) or comes out as
// a broken `import ”;` (hyphenated schema name case).
func TestGenerateDart_Modular_AliasAndHyphenatedNameFieldsGetCorrectImports(t *testing.T) {
	doc, err := engine.Parse([]byte(petAliasSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := engine.GenerateDart(doc, engine.DartOptions{Modular: true})
	if err != nil {
		t.Fatalf("GenerateDart: %v", err)
	}
	petFile, ok := out.Files["pet.dart"]
	if !ok {
		t.Fatalf("expected pet.dart, got files: %v", fileNames(out.Files))
	}
	for _, wantImport := range []string{
		"import 'pet_id.dart';",     // the scalar alias itself, PetId's declared field type
		"import 'categories.dart';", // the array alias itself, Categories' declared field type
		"import 'category.dart';",   // the array alias's element type, referenced inside toJson/fromJson
		"import 'pet__note.dart';",  // the hyphenated "Pet-Note" schema, sanitized to Pet_Note (FileName then inserts its own separator before the N, yielding a doubled underscore)
	} {
		if !strings.Contains(petFile, wantImport) {
			t.Errorf("expected pet.dart to contain %q, got:\n%s", wantImport, petFile)
		}
	}
	if strings.Contains(petFile, "import '';") {
		t.Errorf("expected no broken blank import, got:\n%s", petFile)
	}
}

func TestGenerateZod_Modular(t *testing.T) {
	doc, err := engine.Parse([]byte(twoModelSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := engine.GenerateZod(doc, engine.ZodOptions{Modular: true})
	if err != nil {
		t.Fatalf("GenerateZod: %v", err)
	}
	if _, ok := out.Files["Category.ts"]; !ok {
		t.Error("expected Category.ts")
	}
	petFile, ok := out.Files["Pet.ts"]
	if !ok {
		t.Fatal("expected Pet.ts")
	}
	if !strings.Contains(petFile, `import { z } from "zod";`) {
		t.Errorf("expected zod import in Pet.ts, got:\n%s", petFile)
	}
	if !strings.Contains(petFile, `import { CategorySchema, type Category } from "./Category";`) {
		t.Errorf("expected dependency import in Pet.ts, got:\n%s", petFile)
	}
	barrel, ok := out.Files["index.ts"]
	if !ok {
		t.Fatal("expected barrel index.ts")
	}
	if !strings.Contains(barrel, `export * from "./Category";`) || !strings.Contains(barrel, `export * from "./Pet";`) {
		t.Errorf("expected barrel exports for both models, got:\n%s", barrel)
	}
	if len(out.Files) != 3 {
		t.Errorf("expected 3 files (Category.ts, Pet.ts, index.ts), got %d: %v", len(out.Files), out.Files)
	}
}

// collidingDartFileNameSpec gives two schemas whose Dart TYPE names are
// distinct ("PetNote" and "Pet_note", the latter sanitized from the
// hyphenated "Pet-note") but whose dart.FileName outputs both come out as
// "pet_note.dart" — FileName lowercases as it snake_cases, so it is not
// injective. Owner references both, so the disambiguated file names also
// have to be what its imports pick up.
const collidingDartFileNameSpec = `{
  "openapi": "3.0.3",
  "components": {
    "schemas": {
      "PetNote": {
        "type": "object",
        "properties": {"text": {"type": "string"}},
        "required": ["text"]
      },
      "Pet-note": {
        "type": "object",
        "properties": {"memo": {"type": "string"}},
        "required": ["memo"]
      },
      "Owner": {
        "type": "object",
        "properties": {
          "note": {"$ref": "#/components/schemas/PetNote"},
          "memo": {"$ref": "#/components/schemas/Pet-note"}
        },
        "required": ["note", "memo"]
      }
    }
  }
}`

// TestGenerateDart_Modular_CollidingFileNamesKeepBothModels pins the
// final-review Important 2 finding. modularDartOutput keyed `files` by
// dart.FileName(m.Name) with no collision detection, so the second of two
// distinct types mapping to one file name silently overwrote the first —
// that model's declaration then appeared NOWHERE in the output at all,
// while Dependencies-driven imports still referenced it as though it
// existed.
func TestGenerateDart_Modular_CollidingFileNamesKeepBothModels(t *testing.T) {
	doc, err := engine.Parse([]byte(collidingDartFileNameSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := engine.GenerateDart(doc, engine.DartOptions{Modular: true})
	if err != nil {
		t.Fatalf("GenerateDart: %v", err)
	}
	if len(out.Files) != 3 {
		t.Errorf("expected one file per model (3), got %d: %v", len(out.Files), fileNames(out.Files))
	}

	// Both declarations must survive somewhere, under distinct file names.
	declFile := map[string]string{}
	for name, content := range out.Files {
		for _, decl := range []string{"class PetNote {", "class Pet_note {"} {
			if strings.Contains(content, decl) {
				if prev, dup := declFile[decl]; dup {
					t.Errorf("%q appears in both %s and %s", decl, prev, name)
				}
				declFile[decl] = name
			}
		}
	}
	for _, decl := range []string{"class PetNote {", "class Pet_note {"} {
		if declFile[decl] == "" {
			t.Errorf("%q is missing from the modular output entirely; files: %v", decl, fileNames(out.Files))
		}
	}
	if a, b := declFile["class PetNote {"], declFile["class Pet_note {"]; a != "" && a == b {
		t.Errorf("expected the two classes in distinct files, both landed in %s", a)
	}

	// Owner's imports must name the files the dependencies actually
	// landed in, disambiguating suffix included.
	ownerFile, ok := out.Files["owner.dart"]
	if !ok {
		t.Fatalf("expected owner.dart, got files: %v", fileNames(out.Files))
	}
	for _, decl := range []string{"class PetNote {", "class Pet_note {"} {
		want := "import '" + declFile[decl] + "';"
		if !strings.Contains(ownerFile, want) {
			t.Errorf("expected owner.dart to contain %q (where %q lives), got:\n%s", want, decl, ownerFile)
		}
	}
}

// crossGeneratorSpec exercises several behaviors the design spec requires
// all three mobile generators to share: a top-level array of an inline
// object (whose item type each language must synthesize under the same
// "<Name>Item" base name), an inline enum nested inside that item (named
// "<Containing><Field>"), and an allOf chain where the derived schema
// redeclares an inherited field with a different type (own-field-wins).
const crossGeneratorSpec = `{
  "openapi": "3.0.3",
  "components": {
    "schemas": {
      "Things": {
        "type": "array",
        "items": {
          "type": "object",
          "properties": {
            "id": {"type": "string"},
            "kind": {"type": "string", "enum": ["a", "b"]}
          },
          "required": ["id", "kind"]
        }
      },
      "Base": {
        "type": "object",
        "properties": {
          "name": {"type": "string"},
          "shared": {"type": "integer"}
        },
        "required": ["name", "shared"]
      },
      "Derived": {
        "allOf": [
          {"$ref": "#/components/schemas/Base"},
          {
            "type": "object",
            "properties": {"shared": {"type": "string"}},
            "required": ["shared"]
          }
        ]
      }
    }
  }
}`

// TestMobileGenerators_AgreeOnSharedDesignBehaviors is the cross-generator
// consistency test the final review asked for. Every prior test in this
// sub-project looked at exactly one language at a time, which is how the
// Swift-only "<Name>Item" array-item naming convention (Important 1)
// survived three rounds of task-level review while Kotlin and Dart
// silently synthesized "Things_2" instead.
//
// The assertions are deliberately narrow rather than a generic
// cross-language diff: the SET of synthesized type names must be
// identical across the three targets (that is the language-independent
// part of the design), and each language must then spell its own
// declarations for those names in its own syntax.
func TestMobileGenerators_AgreeOnSharedDesignBehaviors(t *testing.T) {
	doc, err := engine.Parse([]byte(crossGeneratorSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	swiftOut, err := engine.GenerateSwift(doc, engine.SwiftOptions{Modular: true})
	if err != nil {
		t.Fatalf("GenerateSwift: %v", err)
	}
	kotlinOut, err := engine.GenerateKotlin(doc, engine.KotlinOptions{Modular: true})
	if err != nil {
		t.Fatalf("GenerateKotlin: %v", err)
	}
	dartOut, err := engine.GenerateDart(doc, engine.DartOptions{Modular: true})
	if err != nil {
		t.Fatalf("GenerateDart: %v", err)
	}

	// Modular Swift and Kotlin key files by the declaration's own type
	// name; Dart snake_cases it. Compare the Swift/Kotlin name sets
	// directly, and check Dart's declarations by name in its file bodies
	// below.
	swiftNames := trimFileSuffixes(fileNames(swiftOut.Files), ".swift")
	kotlinNames := trimFileSuffixes(fileNames(kotlinOut.Files), ".kt")
	if strings.Join(swiftNames, ",") != strings.Join(kotlinNames, ",") {
		t.Errorf("Swift and Kotlin disagree on the generated type-name set:\n  swift:  %v\n  kotlin: %v", swiftNames, kotlinNames)
	}
	wantNames := []string{"Base", "Derived", "Things", "ThingsItem", "ThingsItemKind"}
	if strings.Join(swiftNames, ",") != strings.Join(wantNames, ",") {
		t.Errorf("unexpected type-name set: got %v, want %v", swiftNames, wantNames)
	}

	// Each language spells the same five names in its own syntax. The
	// "ThingsItem"/"ThingsItemKind" entries are what regressing
	// Important 1 (passing bare `name` instead of name+"Item") would
	// break: those become "Things_2"/"Things_2Kind" in the offending
	// language and this test fails on exactly that language.
	dartAll := strings.Join(sortedFileContents(dartOut.Files), "\n")
	swiftAll := strings.Join(sortedFileContents(swiftOut.Files), "\n")
	kotlinAll := strings.Join(sortedFileContents(kotlinOut.Files), "\n")
	for _, tc := range []struct {
		lang string
		all  string
		want []string
	}{
		{"swift", swiftAll, []string{
			"public typealias Things = [ThingsItem]",
			"public struct ThingsItem: Codable {",
			"public enum ThingsItemKind: String, Codable {",
			"public var kind: ThingsItemKind",
			"public var shared: String", // own-field-wins over Base's integer
		}},
		{"kotlin", kotlinAll, []string{
			"typealias Things = List<ThingsItem>",
			"data class ThingsItem(",
			"enum class ThingsItemKind(",
			"val kind: ThingsItemKind",
			"val shared: String", // own-field-wins over Base's Int
		}},
		{"dart", dartAll, []string{
			"typedef Things = List<ThingsItem>;",
			"class ThingsItem {",
			"enum ThingsItemKind {",
			"final ThingsItemKind kind;",
			"final String shared;", // own-field-wins over Base's int
		}},
	} {
		for _, want := range tc.want {
			if !strings.Contains(tc.all, want) {
				t.Errorf("%s output is missing %q; full output:\n%s", tc.lang, want, tc.all)
			}
		}
	}
}

// mapOfInlineEnumSpec exercises additionalProperties/KindMap's naming
// convention specifically: an inline enum as a map's VALUE type must
// synthesize under the same "<Containing><Field>" convention arrays
// already use for an inline item type (see crossGeneratorSpec above),
// not a distinct one — resolveType's KindMap case was written to reuse
// the exact same suggestedName-threading path as KindArray, and this is
// the one behavior no per-language unit test exercises with an inline
// (rather than named/primitive) map value.
const mapOfInlineEnumSpec = `{
  "openapi": "3.0.3",
  "components": {
    "schemas": {
      "Pet": {
        "type": "object",
        "required": ["statusByRegion"],
        "properties": {
          "statusByRegion": {
            "type": "object",
            "additionalProperties": {"type": "string", "enum": ["active", "inactive"]}
          }
        }
      }
    }
  }
}`

// TestAllGenerators_MapOfInlineEnumSharesNamingConvention checks that
// Swift/Kotlin/Dart agree a map's inline enum value synthesizes as
// "PetStatusByRegion" (containingTypeName + PascalCase(fieldName)) — the
// same convention already proven for array items by
// TestMobileGenerators_AgreeOnSharedDesignBehaviors — while TS/Zod
// correctly keep rendering the enum inline (they never synthesize a
// named type for an inline enum anywhere, with or without a map).
func TestAllGenerators_MapOfInlineEnumSharesNamingConvention(t *testing.T) {
	doc, err := engine.Parse([]byte(mapOfInlineEnumSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	tsOut, err := engine.GenerateTS(doc, engine.TSOptions{Modular: false})
	if err != nil {
		t.Fatalf("GenerateTS: %v", err)
	}
	zodOut, err := engine.GenerateZod(doc, engine.ZodOptions{Modular: false})
	if err != nil {
		t.Fatalf("GenerateZod: %v", err)
	}
	swiftOut, err := engine.GenerateSwift(doc, engine.SwiftOptions{Modular: false})
	if err != nil {
		t.Fatalf("GenerateSwift: %v", err)
	}
	kotlinOut, err := engine.GenerateKotlin(doc, engine.KotlinOptions{Modular: false})
	if err != nil {
		t.Fatalf("GenerateKotlin: %v", err)
	}
	dartOut, err := engine.GenerateDart(doc, engine.DartOptions{Modular: false})
	if err != nil {
		t.Fatalf("GenerateDart: %v", err)
	}

	for _, tc := range []struct {
		lang string
		got  string
		want []string
	}{
		// TS and Zod never synthesize a named type for an inline enum
		// anywhere (array items, object fields, or map values) — only the
		// mobile targets need to, since TS/Zod can express an inline
		// literal union/enum directly with no named-type detour.
		{"ts", tsOut.Files["index.ts"], []string{
			`statusByRegion: Record<string, "active" | "inactive">;`,
		}},
		{"zod", zodOut.Files["index.ts"], []string{
			`statusByRegion: z.record(z.enum(["active", "inactive"]))`,
		}},
		{"swift", swiftOut.Files["Generated.swift"], []string{
			"public enum PetStatusByRegion: String, Codable {",
			"public var statusByRegion: [String: PetStatusByRegion]",
		}},
		{"kotlin", kotlinOut.Files["Generated.kt"], []string{
			"enum class PetStatusByRegion(",
			"val statusByRegion: Map<String, PetStatusByRegion>",
		}},
		{"dart", dartOut.Files["generated.dart"], []string{
			"enum PetStatusByRegion {",
			"final Map<String, PetStatusByRegion> statusByRegion;",
		}},
	} {
		for _, want := range tc.want {
			if !strings.Contains(tc.got, want) {
				t.Errorf("%s output is missing %q; full output:\n%s", tc.lang, want, tc.got)
			}
		}
	}
}

func trimFileSuffixes(names []string, suffix string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, strings.TrimSuffix(n, suffix))
	}
	return out
}

func sortedFileContents(files map[string]string) []string {
	out := make([]string, 0, len(files))
	for _, name := range fileNames(files) {
		out = append(out, files[name])
	}
	return out
}
