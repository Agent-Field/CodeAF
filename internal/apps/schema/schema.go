// Package schema is the one place that says what a codeaf app's files may contain.
//
// An app is files, not code: app.json, plan.json, screens/*.json, runs/*.yaml and
// agent.md. This package carries the JSON Schema (draft 2020-12) of each file, the
// kit catalog that names the parts a screen may use, and a validator that runs the
// schemas first and the semantic checks after them. The Builder's Check, the app
// store's load, and every save go through Validate, so a definition that fails here
// is never the one a person is running.
//
// The schemas and the kit catalog are embedded so the binary checks offline. The
// kit catalog is also copied verbatim into the desk, which tests its own copy, so
// the two sides can never disagree about which parts exist.
package schema

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// The schema URLs are the identifiers the files name in their own `$schema` key.
// They are never fetched: every one is served from the embedded copy.
const (
	AppSchemaURL    = "https://codeaf.dev/schema/app/1.json"
	PlanSchemaURL   = "https://codeaf.dev/schema/plan/1.json"
	ScreenSchemaURL = "https://codeaf.dev/schema/screen/1.json"
	RunSchemaURL    = "https://codeaf.dev/schema/run/1.json"
)

//go:embed schemas/*.json
var schemaFiles embed.FS

//go:embed kit-v0.json
var kitV0 []byte

// KitJSON returns the kit catalog exactly as it is embedded, for the `apps.kit`
// wire method and for anything that must hand the desk the same bytes it tests.
func KitJSON() []byte {
	return bytes.Clone(kitV0)
}

// SchemaJSON returns one embedded schema by its file name (app-1.json, plan-1.json,
// screen-1.json, run-1.json), so the Builder can show the model the real contract
// instead of a paraphrase of it.
func SchemaJSON(name string) ([]byte, error) {
	return schemaFiles.ReadFile("schemas/" + name)
}

// Kit is the parsed kit catalog.
type Kit struct {
	Kit   int    `json:"kit"`
	Parts []Part `json:"parts"`
}

// Part is one kit part: its props schema, whether it takes child nodes, whether a
// person can act on it (and so it needs a label), the bindings it accepts and the
// events its `on` may name.
type Part struct {
	Name        string          `json:"name"`
	Summary     string          `json:"summary"`
	Children    string          `json:"children"`
	Interactive bool            `json:"interactive"`
	Bindings    []string        `json:"bindings"`
	Events      []string        `json:"events"`
	Props       json.RawMessage `json:"props"`
}

// Part returns the named part, and false when the kit has no part by that name.
func (k Kit) Part(name string) (Part, bool) {
	for _, p := range k.Parts {
		if p.Name == name {
			return p, true
		}
	}
	return Part{}, false
}

// Names lists every part name, sorted, for messages that say what is allowed.
func (k Kit) Names() []string {
	names := make([]string, 0, len(k.Parts))
	for _, p := range k.Parts {
		names = append(names, p.Name)
	}
	sort.Strings(names)
	return names
}

type compiled struct {
	kit    Kit
	app    *jsonschema.Schema
	plan   *jsonschema.Schema
	screen *jsonschema.Schema
	run    *jsonschema.Schema
	props  map[string]*jsonschema.Schema
	err    error
}

var (
	compileOnce sync.Once
	compiledSet compiled
)

// schemas compiles every embedded schema once. A failure here is a defect in the
// embedded files themselves, which the package's tests catch, so callers see it as
// one problem rather than a panic.
func schemas() compiled {
	compileOnce.Do(func() {
		compiledSet = compileAll()
	})
	return compiledSet
}

func compileAll() compiled {
	var out compiled
	if err := json.Unmarshal(kitV0, &out.kit); err != nil {
		out.err = fmt.Errorf("kit-v0.json: %w", err)
		return out
	}
	c := jsonschema.NewCompiler()
	c.Draft = jsonschema.Draft2020
	for name, url := range map[string]string{
		"app-1.json":    AppSchemaURL,
		"plan-1.json":   PlanSchemaURL,
		"screen-1.json": ScreenSchemaURL,
		"run-1.json":    RunSchemaURL,
	} {
		raw, err := schemaFiles.ReadFile("schemas/" + name)
		if err != nil {
			out.err = err
			return out
		}
		if err := c.AddResource(url, bytes.NewReader(raw)); err != nil {
			out.err = fmt.Errorf("%s: %w", name, err)
			return out
		}
	}
	out.props = map[string]*jsonschema.Schema{}
	for _, p := range out.kit.Parts {
		url := "https://codeaf.dev/schema/kit/0/" + p.Name + ".json"
		if err := c.AddResource(url, bytes.NewReader(p.Props)); err != nil {
			out.err = fmt.Errorf("kit part %s: %w", p.Name, err)
			return out
		}
	}
	var err error
	if out.app, err = c.Compile(AppSchemaURL); err != nil {
		out.err = err
		return out
	}
	if out.plan, err = c.Compile(PlanSchemaURL); err != nil {
		out.err = err
		return out
	}
	if out.screen, err = c.Compile(ScreenSchemaURL); err != nil {
		out.err = err
		return out
	}
	if out.run, err = c.Compile(RunSchemaURL); err != nil {
		out.err = err
		return out
	}
	for _, p := range out.kit.Parts {
		s, err := c.Compile("https://codeaf.dev/schema/kit/0/" + p.Name + ".json")
		if err != nil {
			out.err = fmt.Errorf("kit part %s: %w", p.Name, err)
			return out
		}
		out.props[p.Name] = s
	}
	return out
}

// KitV0 returns the parsed kit catalog.
func KitV0() (Kit, error) {
	s := schemas()
	return s.kit, s.err
}

// schemaProblems turns one validation failure into one problem per leaf cause, so
// a file with three mistakes says three things instead of one vague one.
func schemaProblems(file string, err error) []Problem {
	var invalid *jsonschema.ValidationError
	if !asValidation(err, &invalid) {
		return []Problem{{File: file, Code: CodeSchema, Message: err.Error()}}
	}
	var out []Problem
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			where := e.InstanceLocation
			subject := file
			if where != "" {
				subject = strings.TrimPrefix(where, "/")
			}
			out = append(out, Problem{File: file, Path: where, Code: CodeSchema, Message: subject + ": " + plainSchemaMessage(e.Message)})
			return
		}
		for _, cause := range e.Causes {
			walk(cause)
		}
	}
	walk(invalid)
	return out
}

func asValidation(err error, target **jsonschema.ValidationError) bool {
	v, ok := err.(*jsonschema.ValidationError)
	if ok {
		*target = v
	}
	return ok
}

var (
	notAllowedKey = regexp.MustCompile(`^additionalProperties (.+) not allowed$`)
	missingKeys   = regexp.MustCompile(`^missing properties?: (.+)$`)
)

// plainSchemaMessage rewords the validator's two most common messages, an unknown
// key and a missing one, into words a person or the Builder reads without knowing
// JSON Schema. Anything else is already plain enough to pass through.
func plainSchemaMessage(msg string) string {
	if m := notAllowedKey.FindStringSubmatch(msg); m != nil {
		return "it cannot say " + strings.ReplaceAll(m[1], "'", "\"") + "; that key is not part of this file's format"
	}
	if m := missingKeys.FindStringSubmatch(msg); m != nil {
		return "it needs " + strings.ReplaceAll(m[1], "'", "\"")
	}
	return msg
}
