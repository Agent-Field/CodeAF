// Package apppackage encodes portable app definitions and prepares them for the receiving space.
package apppackage

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

const maxArchive = 16 << 20

type Source struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Publisher string `json:"publisher"`
	Version   string `json:"version"`
	Hash      string `json:"hash"`
}

type Field struct {
	Type     string   `json:"type"`
	Required bool     `json:"required,omitempty"`
	To       string   `json:"to,omitempty"`
	Currency string   `json:"currency,omitempty"`
	Values   []string `json:"values,omitempty"`
}

type Entity struct {
	Common string           `json:"common,omitempty"`
	Uses   bool             `json:"uses,omitempty"`
	Fields map[string]Field `json:"fields"`
}

type Manifest struct {
	Source
	Files       []string            `json:"files"`
	Entities    map[string]Entity   `json:"entities"`
	Actions     []string            `json:"actions"`
	Automations []string            `json:"automations"`
	Triggers    []schema.Trigger    `json:"triggers,omitempty"`
	Permissions schema.Capabilities `json:"permissions"`
	Budget      *schema.Budget      `json:"budget,omitempty"`
	Sample      bool                `json:"sample,omitempty"`
}

type Archive struct {
	Format    int               `json:"format"`
	Manifest  Manifest          `json:"manifest"`
	Files     map[string][]byte `json:"files"`
	Signature []byte            `json:"signature,omitempty"`
}

type PackOptions struct {
	Publisher string `json:"publisher,omitempty"`
	Version   string `json:"version,omitempty"`
	Sample    *bool  `json:"sample,omitempty"`
}

// Pack includes only definition bytes. Live records and per-person grants
// reside outside those files, so they cannot be embedded by the export.
func Pack(files map[string]string, existing schema.TypeSet, p PackOptions) ([]byte, error) {
	if p.Publisher == BuiltinPublisher {
		return nil, fmt.Errorf("the %s publisher requires a release signature", BuiltinPublisher)
	}
	return pack(files, existing, p)
}

func pack(files map[string]string, existing schema.TypeSet, p PackOptions) ([]byte, error) {
	a := Archive{Format: 1, Files: make(map[string][]byte, len(files))}
	for path, raw := range files {
		a.Files[path] = []byte(raw)
	}
	if p.Sample != nil && !*p.Sample {
		delete(a.Files, schema.SampleFile)
	}
	app, err := schema.ParseApp(a.Files[schema.AppFile])
	if err != nil {
		return nil, err
	}
	if p.Publisher == "" {
		p.Publisher = "local"
	}
	if p.Version == "" {
		p.Version = fmt.Sprintf("%d.0.0", app.Version)
	}
	// Export an explicit common key that an owner bound after creating
	// the app. A matching alias by itself conveys no permission to reuse.
	normalized, err := withBoundCommon(a.Files[schema.AppFile], app, existing)
	if err != nil {
		return nil, err
	}
	a.Files[schema.AppFile] = normalized
	a.Manifest, err = describe(a.Files, p.Publisher, p.Version)
	if err != nil {
		return nil, err
	}
	for alias, rec := range app.Records {
		if !rec.Uses {
			continue
		}
		shape, ok := existing.ForRecord(alias, rec)
		if !ok {
			return nil, fmt.Errorf("app.json /records/%s: the type it uses is unavailable", alias)
		}
		for name, field := range shape.Fields {
			if _, local := rec.Fields[name]; !local {
				a.Manifest.Entities[alias].Fields[name] = fieldShape(field)
			}
		}
	}
	a.Manifest.Hash = a.digest()
	raw, err := json.Marshal(a)
	if len(raw) > maxArchive {
		return nil, fmt.Errorf("the app package exceeds %d MiB", maxArchive>>20)
	}
	return raw, err
}

// Read recomputes the content hash and lower-bounds the manifest with the
// actual declarations. A self-consistent hash alone cannot authorize a grant.
func Read(r io.Reader) (Archive, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxArchive+1))
	if err != nil {
		return Archive{}, err
	}
	if len(raw) > maxArchive {
		return Archive{}, fmt.Errorf("the app package exceeds %d MiB", maxArchive>>20)
	}
	var a Archive
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&a) != nil || d.Decode(new(any)) != io.EOF || a.Format != 1 {
		return Archive{}, fmt.Errorf("expected a format 1 app package")
	}
	if a.Manifest.Hash != a.digest() {
		return Archive{}, fmt.Errorf("the app package does not match its content hash")
	}
	if err := a.verifyPublisher(); err != nil {
		return Archive{}, err
	}
	want, err := describe(a.Files, a.Manifest.Publisher, a.Manifest.Version)
	if err != nil {
		return Archive{}, err
	}
	want.Hash = a.Manifest.Hash
	for alias, entity := range want.Entities {
		if entity.Uses {
			for name, field := range a.Manifest.Entities[alias].Fields {
				if _, declared := entity.Fields[name]; !declared {
					entity.Fields[name] = field
				}
			}
		}
	}
	wantJSON, _ := json.Marshal(want)
	actualJSON, _ := json.Marshal(a.Manifest)
	if !bytes.Equal(wantJSON, actualJSON) {
		return Archive{}, fmt.Errorf("the package manifest does not match its definition files")
	}
	return a, nil
}

func (a Archive) digest() string {
	a.Manifest.Hash = ""
	a.Signature = nil
	raw, _ := json.Marshal(a)
	return fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
}

func Newer(upstream, base Source) bool {
	a, validA := parseVersion(upstream.Version)
	b, validB := parseVersion(base.Version)
	return upstream.ID == base.ID && upstream.Publisher == base.Publisher && validA && validB && a.compare(b) > 0
}

func describe(files map[string][]byte, publisher, version string) (Manifest, error) {
	_, validVersion := parseVersion(version)
	if !validVersion ||
		strings.TrimSpace(publisher) != publisher || publisher == "" || len(publisher) > 200 {
		return Manifest{}, fmt.Errorf("the package needs a publisher and a SemVer version such as 1.0.0")
	}
	app, err := schema.ParseApp(files[schema.AppFile])
	if err != nil {
		return Manifest{}, err
	}
	if app.ID == "" || app.Name == "" {
		return Manifest{}, fmt.Errorf("the packaged app needs an ID and a name")
	}
	manifest := Manifest{
		Source: Source{ID: app.ID, Name: app.Name, Publisher: publisher, Version: version},
		Files:  slices.Sorted(maps.Keys(files)), Entities: make(map[string]Entity, len(app.Records)),
		Actions: []string{}, Automations: []string{}, Triggers: app.Triggers, Permissions: app.Capabilities, Budget: app.Budget,
	}
	for _, path := range manifest.Files {
		if path == "." || !fs.ValidPath(path) || strings.ContainsAny(path, "\\:\x00") {
			return Manifest{}, fmt.Errorf("unsupported definition path %q", path)
		}
	}
	_, manifest.Sample = files[schema.SampleFile]
	for alias, record := range app.Records {
		fields := make(map[string]Field, len(record.Fields))
		for name, field := range record.Fields {
			fields[name] = fieldShape(field)
		}
		manifest.Entities[alias] = Entity{Common: record.Common, Uses: record.Uses, Fields: fields}
	}
	def := schema.Definition{Files: files}
	for _, file := range def.RunFiles() {
		manifest.Automations = append(manifest.Automations, schema.FileID(file))
	}
	manifest.Actions, err = actions(def, app)
	return manifest, err
}

func withBoundCommon(raw []byte, app schema.App, existing schema.TypeSet) ([]byte, error) {
	var common map[string]string
	for alias, record := range app.Records {
		if shape := existing[alias]; record.Common == "" && shape.Common != "" && (shape.Owner == app.ID || record.Uses) {
			if common == nil {
				common = map[string]string{}
			}
			common[alias] = shape.Common
		}
	}
	if len(common) == 0 {
		return raw, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	var records map[string]map[string]json.RawMessage
	if err := json.Unmarshal(obj["records"], &records); err != nil {
		return nil, err
	}
	for alias, key := range common {
		records[alias]["common"], _ = json.Marshal(key)
	}
	obj["records"], _ = json.Marshal(records)
	return json.MarshalIndent(obj, "", "  ")
}

func fieldShape(f schema.Field) Field {
	return Field{Type: f.Type, Required: f.Required, To: f.To, Currency: f.Currency, Values: f.Values}
}

func actions(def schema.Definition, app schema.App) ([]string, error) {
	ids := make(map[string]bool)
	for _, cmd := range app.Commands {
		ids["command/"+cmd.ID] = true
	}
	for alias, record := range app.Records {
		if record.Lifecycle != nil {
			for _, transition := range record.Lifecycle.Transitions {
				ids["transition/"+alias+"/"+transition.ID] = true
			}
		}
	}
	if app.Process != nil {
		for alias, lifecycle := range app.Process.Lifecycles {
			for id := range lifecycle.Transitions {
				ids["transition/"+alias+"/"+id] = true
			}
		}
	}
	for _, file := range def.ScreenFiles() {
		screen, err := schema.ParseScreen(def.Files[file])
		if err != nil {
			return nil, err
		}
		var visit func(*schema.Node)
		visit = func(node *schema.Node) {
			if node == nil {
				return
			}
			for event := range node.On {
				ids[screen.Screen+"/"+node.ID+"/"+event] = true
			}
			for _, child := range node.Children {
				visit(child)
			}
		}
		visit(screen.Nav)
		visit(screen.Body)
	}
	out := slices.Sorted(maps.Keys(ids))
	if out == nil {
		out = []string{}
	}
	return out, nil
}
