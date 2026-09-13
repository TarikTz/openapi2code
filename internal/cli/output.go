package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tarikomercehajic/openapi2code/pkg/engine"
)

// writeOutput writes out to disk: to the single file outFile if set,
// otherwise as one file per out.Files entry inside outDir. Existing files
// at the exact destination paths are overwritten; nothing else at the
// destination is touched.
//
// --out-file requests monolithic output, which every target produces as
// exactly one Output.Files entry — but each target keys it differently
// ("index.ts" for GenerateTS/GenerateZod, "Generated.swift" for
// GenerateSwift), so this reads whichever single entry is present rather
// than assuming a fixed key name.
func writeOutput(out engine.Output, outDir, outFile string) error {
	if outFile != "" {
		if err := os.MkdirAll(filepath.Dir(outFile), 0755); err != nil {
			return err
		}
		if len(out.Files) != 1 {
			names := make([]string, 0, len(out.Files))
			for name := range out.Files {
				names = append(names, name)
			}
			return fmt.Errorf("engine produced %d files for --out-file, expected exactly 1: %v", len(out.Files), names)
		}
		var content string
		for _, c := range out.Files {
			content = c
		}
		return os.WriteFile(outFile, []byte(content), 0644)
	}

	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	for name, content := range out.Files {
		if err := os.WriteFile(filepath.Join(outDir, name), []byte(content), 0644); err != nil {
			return err
		}
	}
	return nil
}
