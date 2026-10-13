package spec

import (
	"embed"
	"io/fs"
)

// The golden CRM is the contract's worked example and the walking skeleton's
// demo app until the store lane replaces the fixture-backed stub.
//
//go:embed testdata/crm
var goldenFS embed.FS

// GoldenCRM returns the golden CRM's definition files and its sample.json.
func GoldenCRM() (Files, []byte) {
	root, _ := fs.Sub(goldenFS, "testdata/crm")
	files := Files{}
	for _, pattern := range []string{AppFile, PlanFile, AgentFile, ScreensDir + "/*.json", RunsDir + "/*.yaml"} {
		matches, _ := fs.Glob(root, pattern)
		for _, name := range matches {
			files[name], _ = fs.ReadFile(root, name)
		}
	}
	sample, _ := fs.ReadFile(root, "sample.json")
	return files, sample
}
