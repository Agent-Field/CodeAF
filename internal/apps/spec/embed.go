package spec

import "embed"

//go:embed schema/*.schema.json kit.json
var embedded embed.FS

// Schema URLs, as written in each file's $schema.
const (
	AppSchemaURL    = "https://codeaf.dev/schema/app/1.json"
	PlanSchemaURL   = "https://codeaf.dev/schema/plan/1.json"
	ScreenSchemaURL = "https://codeaf.dev/schema/screen/1.json"
	RunSchemaURL    = "https://codeaf.dev/schema/run/1.json"
)

var schemaFiles = map[string]string{
	AppSchemaURL:    "schema/app.schema.json",
	PlanSchemaURL:   "schema/plan.schema.json",
	ScreenSchemaURL: "schema/screen.schema.json",
	RunSchemaURL:    "schema/run.schema.json",
}

// SchemaJSON returns the embedded JSON Schema for a schema URL.
func SchemaJSON(url string) ([]byte, error) { return embedded.ReadFile(schemaFiles[url]) }
