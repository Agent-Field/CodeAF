package schema

import (
	"embed"
	"io/fs"
	"strings"
)

// The same examples that pin the validator teach the Builder the shipped formats.
//
//go:embed testdata/crm testdata/hiring testdata/draft-review
var builderExamples embed.FS

// BuilderReference renders the schemas and worked examples in file order for
// people reading the complete reference.
func BuilderReference() string {
	var out strings.Builder
	for _, source := range []embed.FS{schemaFiles, builderExamples} {
		_ = fs.WalkDir(source, ".", func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			raw, err := source.ReadFile(path)
			if err == nil {
				out.WriteString("\n--- " + path + " ---\n")
				out.Write(raw)
				out.WriteByte('\n')
			}
			return err
		})
	}
	return out.String()
}

// BuilderExampleFile reads one file from a shipped, synthetic worked example.
func BuilderExampleFile(example, file string) ([]byte, error) {
	return builderExamples.ReadFile("testdata/" + example + "/" + file)
}
