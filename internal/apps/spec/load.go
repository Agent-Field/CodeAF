package spec

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Fixed file names inside an app's definition directory.
const (
	AppFile    = "app.json"
	PlanFile   = "plan.json"
	AgentFile  = "agent.md"
	ScreensDir = "screens"
	RunsDir    = "runs"
)

// Files is an app definition as relative slash paths to contents. It is the
// unit the Builder writes, the store versions and Check validates.
type Files map[string][]byte

// Paths returns the file paths in sorted order.
func (f Files) Paths() []string { return sortedKeys(f) }

// Bundle is a decoded app definition.
type Bundle struct {
	Files   Files
	App     *App
	Plan    *Plan
	Agent   *AgentDoc
	Screens map[string]*Screen // by screen id (file name)
	Runs    map[string]*Run    // by run id (file name)
}

// ReadDir reads an app definition directory: app.json, plan.json, agent.md,
// screens/*.json and runs/*.yaml. Other files are ignored. A missing plan or
// app is not an error here; Validate reports it.
func ReadDir(dir string) (Files, error) {
	files := Files{}
	root := os.DirFS(dir)
	for _, pattern := range []string{AppFile, PlanFile, AgentFile, ScreensDir + "/*.json", RunsDir + "/*.yaml", "custom/*.tsx"} {
		matches, err := fs.Glob(root, pattern)
		if err != nil {
			return nil, err
		}
		for _, name := range matches {
			data, err := fs.ReadFile(root, name)
			if err != nil {
				return nil, err
			}
			files[name] = data
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s holds no app files", filepath.Clean(dir))
	}
	return files, nil
}

// Load reads, validates and decodes an app directory. Any schema issue makes
// it fail with an *InvalidError carrying the full report.
func Load(dir string) (*Bundle, error) {
	files, err := ReadDir(dir)
	if err != nil {
		return nil, err
	}
	if issues := Validate(files); len(issues) > 0 {
		return nil, &InvalidError{Issues: issues}
	}
	return Decode(files)
}

// Decode turns files into typed values without schema validation.
func Decode(files Files) (*Bundle, error) {
	b := &Bundle{Files: files, Screens: map[string]*Screen{}, Runs: map[string]*Run{}}
	var errs []error
	for _, name := range files.Paths() {
		if err := b.decodeFile(name, files[name]); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	if b.App == nil {
		errs = append(errs, errors.New(AppFile+" is missing"))
	}
	return b, errors.Join(errs...)
}

func (b *Bundle) decodeFile(name string, data []byte) error {
	switch dir, id := splitPath(name); {
	case name == AppFile:
		b.App = &App{}
		return json.Unmarshal(data, b.App)
	case name == PlanFile:
		b.Plan = &Plan{}
		return json.Unmarshal(data, b.Plan)
	case name == AgentFile:
		doc, err := ParseAgent(data)
		b.Agent = doc
		return err
	case dir == ScreensDir:
		screen := &Screen{}
		b.Screens[id] = screen
		return json.Unmarshal(data, screen)
	case dir == RunsDir:
		run := &Run{}
		b.Runs[id] = run
		asJSON, err := YAMLToJSON(data)
		if err != nil {
			return err
		}
		return json.Unmarshal(asJSON, run)
	}
	return nil
}

// splitPath returns ("screens", "deal") for "screens/deal.json".
func splitPath(name string) (dir, id string) {
	dir, file := path.Split(name)
	return strings.TrimSuffix(dir, "/"), strings.TrimSuffix(file, path.Ext(file))
}

// YAMLToJSON converts one YAML document to JSON. Map keys must be strings.
func YAMLToJSON(data []byte) ([]byte, error) {
	var value any
	if err := yaml.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

// ScreenIDs returns the screen ids in sorted order.
func (b *Bundle) ScreenIDs() []string { return sortedKeys(b.Screens) }

// RunIDs returns the run ids in sorted order.
func (b *Bundle) RunIDs() []string { return sortedKeys(b.Runs) }
