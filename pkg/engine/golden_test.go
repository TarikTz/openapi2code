package engine_test

import (
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tarikomercehajic/openapi2code/pkg/engine"
)

var update = flag.Bool("update", false, "update golden files")

func TestGoldenFixtures(t *testing.T) {
	fixtures := []string{"petstore", "petstore_v2", "composition", "circular", "nullable_enum", "enum_types"}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			out := generateFixture(t, name, engine.TSOptions{Modular: false})
			got, ok := out.Files["index.ts"]
			if !ok {
				t.Fatalf("monolithic output has no index.ts; files: %v", fileNames(out.Files))
			}
			if len(out.Files) != 1 {
				t.Fatalf("expected exactly 1 file for monolithic output, got %v", fileNames(out.Files))
			}
			checkGolden(t, filepath.Join("testdata", "golden", name+".ts"), got)
		})
	}
}

// TestGoldenFixtures_Modular golden-tests the multi-file output mode: one
// file per model plus the barrel, each committed under
// testdata/golden/<fixture>_modular/.
func TestGoldenFixtures_Modular(t *testing.T) {
	const name = "petstore"
	out := generateFixture(t, name, engine.TSOptions{Modular: true})
	dir := filepath.Join("testdata", "golden", name+"_modular")

	if *update {
		if err := os.RemoveAll(dir); err != nil {
			t.Fatalf("clear golden dir: %v", err)
		}
	}
	for _, fileName := range fileNames(out.Files) {
		checkGolden(t, filepath.Join(dir, fileName), out.Files[fileName])
	}
	if *update {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read golden dir (run with -update to create it): %v", err)
	}
	golden := make([]string, 0, len(entries))
	for _, e := range entries {
		golden = append(golden, e.Name())
	}
	sort.Strings(golden)
	if got := strings.Join(fileNames(out.Files), ","); got != strings.Join(golden, ",") {
		t.Errorf("generated file set %v does not match golden file set %v", fileNames(out.Files), golden)
	}
}

func TestGoldenFixtures_Zod(t *testing.T) {
	fixtures := []string{
		"petstore",
		"composition",
		"nullable_enum",
		"allof-extends-cycle",
		// A non-cyclic model extending a cyclic base, a three-deep extends
		// chain over a cyclic base, and a cyclic model that non-cyclic
		// siblings reference from an alphabetically earlier slot.
		"cyclic-base-extends",
		"transitive-extends",
		"cyclic-ordering",
		"enum_types",
	}
	for _, name := range fixtures {
		name := name
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", name+".yaml"))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			doc, err := engine.Parse(data)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			out, err := engine.GenerateZod(doc, engine.ZodOptions{Modular: false})
			if err != nil {
				t.Fatalf("GenerateZod: %v", err)
			}
			got, ok := out.Files["index.ts"]
			if !ok {
				t.Fatal("monolithic output has no index.ts")
			}
			goldenPath := filepath.Join("testdata", "golden", name+".zod.ts")
			if *update {
				if err := os.MkdirAll(filepath.Dir(goldenPath), 0755); err != nil {
					t.Fatalf("mkdir golden dir: %v", err)
				}
				if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden (run with -update to create it): %v", err)
			}
			if strings.TrimRight(got, "\n") != strings.TrimRight(string(want), "\n") {
				t.Errorf("output mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
			}
		})
	}
}

// TestGoldenFixtures_Swift golden-tests Swift monolithic output for the
// same fixtures already used to exercise TS's union/enum/cyclic/allOf
// handling, comparing against testdata/golden/<fixture>.swift.txt.
func TestGoldenFixtures_Swift(t *testing.T) {
	fixtures := []string{"petstore", "circular", "composition", "nullable_enum", "enum_types"}
	for _, name := range fixtures {
		name := name
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", name+".yaml"))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			doc, err := engine.Parse(data)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			out, err := engine.GenerateSwift(doc, engine.SwiftOptions{Modular: false})
			if err != nil {
				t.Fatalf("GenerateSwift: %v", err)
			}
			got, ok := out.Files["Generated.swift"]
			if !ok {
				t.Fatalf("monolithic output has no Generated.swift; files: %v", fileNames(out.Files))
			}
			if len(out.Files) != 1 {
				t.Fatalf("expected exactly 1 file for monolithic output, got %v", fileNames(out.Files))
			}
			checkGolden(t, filepath.Join("testdata", "golden", name+".swift.txt"), got)
		})
	}
}

// TestGoldenFixtures_Kotlin golden-tests Kotlin monolithic output for the
// same fixtures already used to exercise TS's union/enum/cyclic/allOf
// handling, comparing against testdata/golden/<fixture>.kt.txt.
func TestGoldenFixtures_Kotlin(t *testing.T) {
	fixtures := []string{"petstore", "circular", "composition", "nullable_enum", "enum_types"}
	for _, name := range fixtures {
		name := name
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", name+".yaml"))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			doc, err := engine.Parse(data)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			out, err := engine.GenerateKotlin(doc, engine.KotlinOptions{Modular: false})
			if err != nil {
				t.Fatalf("GenerateKotlin: %v", err)
			}
			got, ok := out.Files["Generated.kt"]
			if !ok {
				t.Fatalf("monolithic output has no Generated.kt; files: %v", fileNames(out.Files))
			}
			if len(out.Files) != 1 {
				t.Fatalf("expected exactly 1 file for monolithic output, got %v", fileNames(out.Files))
			}
			checkGolden(t, filepath.Join("testdata", "golden", name+".kt.txt"), got)
		})
	}
}

// TestGoldenFixtures_Dart golden-tests Dart monolithic output for the
// same fixtures already used to exercise TS's union/enum/cyclic/allOf
// handling, comparing against testdata/golden/<fixture>.dart.txt.
func TestGoldenFixtures_Dart(t *testing.T) {
	fixtures := []string{"petstore", "circular", "composition", "nullable_enum", "enum_types"}
	for _, name := range fixtures {
		name := name
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", name+".yaml"))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			doc, err := engine.Parse(data)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			out, err := engine.GenerateDart(doc, engine.DartOptions{Modular: false})
			if err != nil {
				t.Fatalf("GenerateDart: %v", err)
			}
			got, ok := out.Files["generated.dart"]
			if !ok {
				t.Fatalf("monolithic output has no generated.dart; files: %v", fileNames(out.Files))
			}
			if len(out.Files) != 1 {
				t.Fatalf("expected exactly 1 file for monolithic output, got %v", fileNames(out.Files))
			}
			checkGolden(t, filepath.Join("testdata", "golden", name+".dart.txt"), got)
		})
	}
}

func generateFixture(t *testing.T, name string, opts engine.TSOptions) engine.Output {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name+".yaml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	doc, err := engine.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := engine.GenerateTS(doc, opts)
	if err != nil {
		t.Fatalf("GenerateTS: %v", err)
	}
	return out
}

// checkGolden compares got against the golden file at path, or rewrites
// that file when -update is passed.
func checkGolden(t *testing.T, path, got string) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create it): %v", err)
	}
	if strings.TrimRight(got, "\n") != strings.TrimRight(string(want), "\n") {
		t.Errorf("output mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

func fileNames(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
