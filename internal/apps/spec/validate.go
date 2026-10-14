package spec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// Issue is one finding about one file. Check (internal/apps/check) reports
// the same shape, adding kit-law and render rules.
type Issue struct {
	File    string `json:"file"`
	Path    string `json:"path"` // JSON pointer inside the file, "" for the whole file
	Rule    string `json:"rule"` // "schema", "parse", "name", or a check rule like "kit/label"
	Message string `json:"message"`
}

func (i Issue) String() string { return fmt.Sprintf("%s%s: %s: %s", i.File, i.Path, i.Rule, i.Message) }

// InvalidError carries every issue that stopped a load.
type InvalidError struct{ Issues []Issue }

func (e *InvalidError) Error() string {
	lines := make([]string, len(e.Issues))
	for i, issue := range e.Issues {
		lines[i] = issue.String()
	}
	return "invalid app: " + strings.Join(lines, "; ")
}

var (
	compileOnce sync.Once
	compiled    map[string]*jsonschema.Schema
	compileErr  error
)

func schemas() (map[string]*jsonschema.Schema, error) {
	compileOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		compiler.Draft = jsonschema.Draft2020
		for url, file := range schemaFiles {
			data, err := embedded.ReadFile(file)
			if err != nil {
				compileErr = err
				return
			}
			if err := compiler.AddResource(url, bytes.NewReader(data)); err != nil {
				compileErr = err
				return
			}
		}
		compiled = map[string]*jsonschema.Schema{}
		for url := range schemaFiles {
			schema, err := compiler.Compile(url)
			if err != nil {
				compileErr = fmt.Errorf("compile %s: %w", url, err)
				return
			}
			compiled[url] = schema
		}
	})
	return compiled, compileErr
}

// schemaFor maps a file path to its schema URL, or "" when the file has none.
func schemaFor(name string) string {
	dir, _ := splitPath(name)
	switch {
	case name == AppFile:
		return AppSchemaURL
	case name == PlanFile:
		return PlanSchemaURL
	case dir == ScreensDir:
		return ScreenSchemaURL
	case dir == RunsDir:
		return RunSchemaURL
	}
	return ""
}

// Validate checks every file against its JSON Schema, parses agent.md and
// checks that screen and run ids equal their file names. app.json, plan.json
// and agent.md are required. Semantic and kit-law checks belong to
// internal/apps/check.
func Validate(files Files) []Issue {
	var issues []Issue
	for _, required := range []string{AppFile, PlanFile, AgentFile} {
		if _, ok := files[required]; !ok {
			issues = append(issues, Issue{File: required, Rule: "missing", Message: "file is required"})
		}
	}
	for _, name := range files.Paths() {
		issues = append(issues, validateFile(name, files[name])...)
	}
	return issues
}

func validateFile(name string, data []byte) []Issue {
	if name == AgentFile {
		if _, err := ParseAgent(data); err != nil {
			return []Issue{{File: name, Rule: "parse", Message: err.Error()}}
		}
		return nil
	}
	url := schemaFor(name)
	if url == "" {
		return nil
	}
	if strings.HasPrefix(name, RunsDir+"/") {
		converted, err := YAMLToJSON(data)
		if err != nil {
			return []Issue{{File: name, Rule: "parse", Message: err.Error()}}
		}
		data = converted
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return []Issue{{File: name, Rule: "parse", Message: err.Error()}}
	}
	compiledSchemas, err := schemas()
	if err != nil {
		return []Issue{{File: name, Rule: "schema", Message: err.Error()}}
	}
	if err := compiledSchemas[url].Validate(value); err != nil {
		return schemaIssues(name, err)
	}
	issues := idMatchesName(name, value)
	if url == RunSchemaURL {
		issues = append(issues, runReferences(name, value)...)
	}
	return issues
}

func schemaIssues(name string, err error) []Issue {
	validation, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return []Issue{{File: name, Rule: "schema", Message: err.Error()}}
	}
	var issues []Issue
	collectLeaves(validation, func(leaf *jsonschema.ValidationError) {
		issues = append(issues, Issue{File: name, Path: leaf.InstanceLocation, Rule: "schema", Message: leaf.Message})
	})
	sort.SliceStable(issues, func(i, j int) bool { return issues[i].Path < issues[j].Path })
	return issues
}

func collectLeaves(err *jsonschema.ValidationError, visit func(*jsonschema.ValidationError)) {
	if len(err.Causes) == 0 {
		visit(err)
		return
	}
	for _, cause := range err.Causes {
		collectLeaves(cause, visit)
	}
}

func idMatchesName(name string, value any) []Issue {
	dir, id := splitPath(name)
	key := map[string]string{ScreensDir: "screen", RunsDir: "run"}[dir]
	if key == "" {
		return nil
	}
	object, _ := value.(map[string]any)
	if object[key] != id {
		return []Issue{{File: name, Path: "/" + key, Rule: "name", Message: fmt.Sprintf("%s must equal the file name %q", key, id)}}
	}
	return nil
}

// ValidateFile checks one file alone, with the same schema, parse and name
// rules Validate applies to every file. The draft store uses it to judge a
// single screen, run or agent without needing the rest of the app.
func ValidateFile(name string, data []byte) []Issue { return validateFile(name, data) }

// defSchemas caches the compiled sub-schemas ValidateDef asks for, by
// "<schema url>#/$defs/<def>".
var defSchemas sync.Map

// ValidateDef checks one value against a named definition inside an embedded
// schema, for example the field, trigger or command definition of app.json.
// The file argument only labels the issues it returns.
func ValidateDef(file, schemaURL, def string, value any) []Issue {
	schema, err := defSchema(schemaURL, def)
	if err != nil {
		return []Issue{{File: file, Rule: "schema", Message: err.Error()}}
	}
	if err := schema.Validate(value); err != nil {
		return schemaIssues(file, err)
	}
	return nil
}

func defSchema(schemaURL, def string) (*jsonschema.Schema, error) {
	key := schemaURL + "#/$defs/" + def
	if cached, ok := defSchemas.Load(key); ok {
		return cached.(*jsonschema.Schema), nil
	}
	if _, err := schemas(); err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	for url, file := range schemaFiles {
		data, err := embedded.ReadFile(file)
		if err != nil {
			return nil, err
		}
		if err := compiler.AddResource(url, bytes.NewReader(data)); err != nil {
			return nil, err
		}
	}
	schema, err := compiler.Compile(key)
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", key, err)
	}
	defSchemas.Store(key, schema)
	return schema, nil
}
