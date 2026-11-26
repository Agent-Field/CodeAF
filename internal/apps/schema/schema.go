// Package schema is the one place that says what a codeaf app's files may contain.
//
// An app is files, not code: app.json, plan.json, screens/*.json, runs/*.yaml and
// agent.md, with registry-declared content files. This package carries the JSON Schema (draft 2020-12) of each file, the
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

//go:embed manifests/compat-v0.json
var kitV0Descriptors []byte

//go:generate go run ./cmd/compilecatalog

// KitJSON returns the kit catalog exactly as it is embedded, for the `apps.kit`
// wire method and for anything that must hand the desk the same bytes it tests.
func KitJSON() []byte {
	return KitJSONFor(2)
}

// KitJSONFor returns the wire catalog for the version declared by an app.
func KitJSONFor(version int) []byte {
	switch version {
	case 0:
		return bytes.Clone(kitV0)
	case 2:
		return bytes.Clone(kitV2)
	default:
		return nil
	}
}

// HasAction reports whether the current kit declares the action. Engine
// wire adapters use it to keep executable actions on the authored registry.
func HasAction(name string) bool {
	_, ok := schemas().kits[2].Registries.Actions[name]
	return ok
}

// SchemaJSON returns an embedded schema by its file name so the Builder can
// show the model the real contract.
func SchemaJSON(name string) ([]byte, error) {
	return schemaFiles.ReadFile("schemas/" + name)
}

// Kit is the parsed kit catalog.
type Kit struct {
	Kit        int                 `json:"kit"`
	Parts      []Part              `json:"parts"`
	Registries Registries          `json:"registries,omitempty"`
	Hooks      map[string][]string `json:"hooks,omitempty"`
}

// Part is one kit part: its props schema, whether it takes child nodes, whether a
// person can act on it (and so it needs a label), the bindings it accepts and the
// events its `on` may name.
type Part struct {
	Version         int               `json:"version,omitempty"`
	Example         json.RawMessage   `json:"example,omitempty"`
	Slots           []string          `json:"slots,omitempty"`
	Renderers       map[string]string `json:"renderers,omitempty"`
	Capabilities    string            `json:"capabilities,omitempty"`
	Rules           []ManifestRule    `json:"rules,omitempty"`
	Content         string            `json:"content,omitempty"`
	VariantBinding  string            `json:"variant_binding,omitempty"`
	Navigation      bool              `json:"navigation,omitempty"`
	Selection       bool              `json:"selection,omitempty"`
	BindingRequired bool              `json:"binding_required,omitempty"`
	Source          string            `json:"source,omitempty"`

	Tier        string          `json:"tier,omitempty"`
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

type partKey struct{ variant, name string }

func (p Part) key() partKey { return partKey{p.VariantBinding, p.Name} }

type compiled struct {
	kits      map[int]Kit
	app       *jsonschema.Schema
	plan      *jsonschema.Schema
	screen    *jsonschema.Schema
	run       *jsonschema.Schema
	props     map[int]map[partKey]*jsonschema.Schema
	entries   map[string]*jsonschema.Schema
	resources map[string]*jsonschema.Schema
	err       error
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
	out.kits = map[int]Kit{}
	for _, raw := range [][]byte{kitV0, kitV2} {
		var kit Kit
		if err := json.Unmarshal(raw, &kit); err != nil {
			out.err = fmt.Errorf("embedded kit catalog: %w", err)
			return out
		}
		if kit.Kit == 0 {
			var descriptors map[string]Part
			if err := json.Unmarshal(kitV0Descriptors, &descriptors); err != nil {
				out.err = err
				return out
			}
			for i, part := range kit.Parts {
				d := descriptors[part.Name]
				kit.Parts[i].Rules = d.Rules
				kit.Parts[i].Selection = d.Selection
				kit.Parts[i].Navigation = d.Navigation
			}
		}
		out.kits[kit.Kit] = kit
	}
	c := jsonschema.NewCompiler()
	c.Draft = jsonschema.Draft2020
	c.LoadURL = offlineSchema
	c.AssertFormat = true
	urls := map[string]string{
		"app-1.json":    AppSchemaURL,
		"plan-1.json":   PlanSchemaURL,
		"screen-1.json": ScreenSchemaURL,
		"run-1.json":    RunSchemaURL,
	}
	for _, name := range []string{"app-2.json", "screen-2.json", "run-2.json", "file-1.json", "records-2.json", "process-2.json", "enterprise-2.json", "sample-2.json", "part-2.json", "slides-1.json", "timeline-1.json", "graph-1.json", "places-1.json", "formulas-1.json", "run-binding-2.json"} {
		urls[name] = schemaURL(name)
	}
	for name, url := range urls {
		raw, err := SchemaJSON(name)
		if err != nil {
			out.err = err
			return out
		}
		if err := c.AddResource(url, bytes.NewReader(raw)); err != nil {
			out.err = fmt.Errorf("%s: %w", name, err)
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
	out.props = map[int]map[partKey]*jsonschema.Schema{}
	for version, kit := range out.kits {
		out.props[version] = map[partKey]*jsonschema.Schema{}
		for _, p := range kit.Parts {
			url := fmt.Sprintf("https://codeaf.dev/schema/kit/%d/%s/%s.json", version, p.Tier, p.Name)
			if err := c.AddResource(url, bytes.NewReader(p.Props)); err != nil {
				out.err = fmt.Errorf("kit %d part %s: %w", version, p.Name, err)
				return out
			}
			props, err := c.Compile(url)
			if err != nil {
				out.err = fmt.Errorf("kit %d part %s: %w", version, p.Name, err)
				return out
			}
			if _, exists := out.props[version][p.key()]; exists {
				out.err = fmt.Errorf("duplicate kit %d part %s", version, p.Name)
				return out
			}
			if version == 2 {
				m := Manifest{ID: p.Name, Version: p.Version, Doc: p.Summary, Example: p.Example}
				if err := checkManifest(p.Name, m, props); err != nil {
					out.err = err
					return out
				}
			}
			out.props[version][p.key()] = props
		}
	}
	out.resources = map[string]*jsonschema.Schema{}
	for name, url := range urls {
		out.resources[name], err = c.Compile(url)
		if err != nil {
			out.err = err
			return out
		}
	}
	out.entries = map[string]*jsonschema.Schema{}
	entries := out.kits[2].Registries.schemas()
	for _, key := range sortedKeys(entries) {
		m := entries[key]
		url := "https://codeaf.dev/schema/registry/2/" + key + ".json"
		if err = c.AddResource(url, bytes.NewReader(m.Schema)); err == nil {
			out.entries[key], err = c.Compile(url)
		}
		if err == nil {
			err = checkManifest(key, m, out.entries[key])
		}
		if err != nil {
			out.err = fmt.Errorf("registry entry %s: %w", key, err)
			return out
		}
	}
	return out
}

func schemaURL(name string) string {
	return "https://codeaf.dev/schema/" + name
}

// KitV0 returns the parsed kit catalog.
func KitV0() (Kit, error) {
	s := schemas()
	return s.kits[0], s.err
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
