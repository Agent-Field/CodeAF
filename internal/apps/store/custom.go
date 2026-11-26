package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/extensions"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
)

type kitPart struct {
	Manifest json.RawMessage `json:"manifest"`
	Source   string          `json:"source"`
}

func kitPartName(id string, version int) string {
	return id + "-" + strconv.Itoa(version) + ".json"
}

// PromotePart saves the validated, kept revision in the personal Kit.
func (s *Store) PromotePart(app, id string) (schema.Part, error) {
	d, err := s.Get(app)
	if err != nil {
		return schema.Part{}, err
	}
	raw := d.files["custom/"+id+".json"]
	source := d.files["custom/"+id+".tsx"]
	if len(raw) == 0 {
		return schema.Part{}, ErrNotFound
	}
	program, err := schema.CustomPartBundle(id, raw, source)
	if err != nil {
		return schema.Part{}, err
	}
	p := program.Manifest
	if p.Origin == nil {
		digest, err := schema.PartDigest(raw, source)
		if err != nil {
			return schema.Part{}, err
		}
		p.Origin = &schema.PartOrigin{App: d.Meta.ID, Version: d.Meta.Version, Digest: digest}
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			return schema.Part{}, err
		}
		fields["origin"] = p.Origin
		raw, err = json.Marshal(fields)
		if err != nil {
			return schema.Part{}, err
		}
	}
	unlock, err := s.lock(true, true)
	if err != nil {
		return schema.Part{}, err
	}
	defer unlock()
	dir := filepath.Join(s.root, "_extension_kit")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return schema.Part{}, err
	}
	if err := checkDir(dir); err != nil {
		return schema.Part{}, err
	}
	name := kitPartName(id, p.Version)
	if previous, _, err := s.readKitPart(dir, name); err == nil {
		if *previous.Origin != *p.Origin {
			return schema.Part{}, fmt.Errorf("%w: that Kit name and version already exist", ErrConflict)
		}
		return previous, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return schema.Part{}, err
	}
	return p, writeKitRevision(dir, name, kitPart{Manifest: raw, Source: string(source)})
}

func writeKitRevision(dir, name string, bundle any) error {
	tmp, err := os.MkdirTemp(dir, ".promote-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	prepared := filepath.Join(tmp, name)
	if err := writeJSON(prepared, bundle); err != nil {
		return err
	}
	if err := fsreplace.Rename(prepared, filepath.Join(dir, name)); err != nil {
		return err
	}
	return fsreplace.SyncDir(dir)
}

func (s *Store) readKitPart(dir, name string) (schema.Part, kitPart, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return schema.Part{}, kitPart{}, err
	}
	defer root.Close()
	raw, err := readRegular(root, name)
	if err != nil {
		return schema.Part{}, kitPart{}, err
	}
	var bundle kitPart
	var p schema.Part
	if json.Unmarshal(raw, &bundle) != nil || json.Unmarshal(bundle.Manifest, &p) != nil || p.Origin == nil {
		return schema.Part{}, kitPart{}, ErrCorrupt
	}
	id := schema.FileID(p.Source)
	program, err := schema.CustomPartBundle(id, bundle.Manifest, []byte(bundle.Source))
	if err != nil || kitPartName(id, p.Version) != name {
		return schema.Part{}, kitPart{}, ErrCorrupt
	}
	return program.Manifest, bundle, nil
}

// KitParts lists reusable revisions without disclosing source or app records.
func (s *Store) KitParts() ([]schema.Part, error) {
	dir := filepath.Join(s.root, "_extension_kit")
	if err := checkDir(dir); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var parts []schema.Part
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		p, _, err := s.readKitPart(dir, entry.Name())
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	return parts, nil
}

// CopyKitPart returns immutable files for the chosen version.
func (s *Store) CopyKitPart(id string, version int) (map[string]string, error) {
	if !fileID(id) || version < 1 {
		return nil, ErrBadInput
	}
	p, bundle, err := s.readKitPart(filepath.Join(s.root, "_extension_kit"), kitPartName(id, version))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return map[string]string{p.Name + ".json": string(bundle.Manifest), p.Source: bundle.Source}, nil
}

// KitStep is the public descriptor of an immutable step revision.
type KitStep struct {
	Name    string             `json:"name"`
	Version int                `json:"version"`
	Summary string             `json:"summary"`
	Inputs  []extensions.Input `json:"inputs"`
	Output  string             `json:"output"`
	Origin  extensions.Origin  `json:"origin"`
}

func describeKitStep(id string, s extensions.Step) KitStep {
	return KitStep{Name: "custom." + id, Version: s.Version, Summary: s.Summary, Inputs: s.Inputs, Output: s.Output, Origin: *s.Origin}
}

func kitStepName(id string, version int) string {
	return id + "-" + strconv.Itoa(version) + ".step"
}

// PromoteStep saves a kept function under an immutable Kit revision.
func (s *Store) PromoteStep(app, id string) (KitStep, error) {
	if _, ok := extensions.ToolID("custom." + id); !ok {
		return KitStep{}, ErrBadInput
	}
	d, err := s.Get(app)
	if err != nil {
		return KitStep{}, err
	}
	raw, ok := d.CustomStep(id)
	if !ok {
		return KitStep{}, ErrNotFound
	}
	step, err := extensions.Parse(raw)
	if err != nil {
		return KitStep{}, err
	}
	if step.Origin == nil {
		step.Origin = &extensions.Origin{App: d.Meta.ID, Version: d.Meta.Version, Digest: step.Digest()}
	}
	dir := filepath.Join(s.root, "_extension_kit")
	unlock, err := s.lock(true, true)
	if err != nil {
		return KitStep{}, err
	}
	defer unlock()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return KitStep{}, err
	}
	if err := checkDir(dir); err != nil {
		return KitStep{}, err
	}
	name := kitStepName(id, step.Version)
	if previous, err := readKitStep(dir, name); err == nil {
		if *previous.Origin != *step.Origin {
			return KitStep{}, fmt.Errorf("%w: that Kit name and version already exist", ErrConflict)
		}
		return describeKitStep(id, previous), nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return KitStep{}, err
	}
	return describeKitStep(id, step), writeKitRevision(dir, name, step)
}

func readKitStep(dir, name string) (extensions.Step, error) {
	if err := checkDir(dir); err != nil {
		return extensions.Step{}, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return extensions.Step{}, err
	}
	defer root.Close()
	raw, err := readRegular(root, name)
	if err != nil {
		return extensions.Step{}, err
	}
	step, err := extensions.Parse(raw)
	if err != nil || step.Origin == nil {
		return extensions.Step{}, ErrCorrupt
	}
	return step, nil
}

// KitSteps lists the function signatures available to the builder.
func (s *Store) KitSteps() ([]KitStep, error) {
	dir := filepath.Join(s.root, "_extension_kit")
	if err := checkDir(dir); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var steps []KitStep
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".step" {
			continue
		}
		step, err := readKitStep(dir, entry.Name())
		if err != nil {
			return nil, err
		}
		id := strings.TrimSuffix(entry.Name(), "-"+strconv.Itoa(step.Version)+".step")
		if _, ok := extensions.ToolID("custom." + id); !ok || kitStepName(id, step.Version) != entry.Name() {
			return nil, ErrCorrupt
		}
		steps = append(steps, describeKitStep(id, step))
	}
	return steps, nil
}

// CopyKitStep gives the Builder an immutable copy to pin into a definition.
func (s *Store) CopyKitStep(id string, version int) (map[string]string, error) {
	if _, ok := extensions.ToolID("custom." + id); !ok || version < 1 {
		return nil, ErrBadInput
	}
	step, err := readKitStep(filepath.Join(s.root, "_extension_kit"), kitStepName(id, version))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if step.Version != version {
		return nil, ErrCorrupt
	}
	raw, err := json.Marshal(step)
	return map[string]string{"custom/" + id + ".step": string(raw)}, err
}
