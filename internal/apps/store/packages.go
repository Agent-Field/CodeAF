package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/apppackage"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/cell"
)

type Lineage struct {
	Source             *apppackage.Source `json:"source,omitempty"`
	PackageBaseVersion int                `json:"package_base_version,omitempty"`
	ForkedFrom         *ForkOrigin        `json:"forked_from,omitempty"`
}

type ForkOrigin struct {
	App     string             `json:"app"`
	Version int                `json:"version"`
	Source  *apppackage.Source `json:"source,omitempty"`
}

type PackagePublication struct {
	Lineage  Lineage
	Baseline map[string]string
	starter  map[string][]map[string]any
}

// PublishPackageBuild commits the lineage pointer with the same publication
// as the definition. Later local edits preserve that pointer.
func (s *Store) PublishPackageBuild(id, app string, base int, def schema.Definition, request string, pkg PackagePublication) (Meta, error) {
	return s.publishBuild(id, app, base, def, request, &pkg)
}

// PackageBase returns the unmixed upstream files recorded when the package
// was accepted, including when the person kept a local value on that card.
func (s *Store) PackageBase(app string, version int) (map[string]string, error) {
	unlock, err := s.lockExisting(app, false)
	if err != nil {
		return nil, err
	}
	defer unlock()
	receipt, err := s.readReceipt(app, version)
	if err != nil {
		return nil, err
	}
	if len(receipt.PackageBase) == 0 {
		return nil, ErrNotFound
	}
	return receipt.PackageBase, nil
}

// InstallBuiltins publishes missing bundled apps. An existing publication
// always wins, including an archived or deleted app. New releases go through
// the same update card as any other package.
func (s *Store) InstallBuiltins() error {
	archives, err := apppackage.Builtins()
	if err != nil {
		return err
	}
	unlock, err := s.lock(true, true)
	if err != nil {
		return err
	}
	defer unlock()
	for _, archive := range archives {
		source := archive.Manifest.Source
		if !cell.ValidID(source.ID) {
			return fmt.Errorf("the release package %s needs a stable app ID", source.Name)
		}
		if _, err := s.readMeta(source.ID); err == nil {
			continue
		} else if !errors.Is(err, ErrNotFound) && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		baseline, _, err := archive.Bind(nil, source.ID, "")
		if err != nil {
			return err
		}
		m := Meta{ID: source.ID, CreatedAt: time.Now().UTC()}
		pkg := PackagePublication{Lineage: Lineage{Source: &source}, Baseline: baseline}
		if raw := baseline[schema.SampleFile]; raw != "" {
			var starter schema.Sample
			if err := json.Unmarshal([]byte(raw), &starter); err != nil {
				return fmt.Errorf("read %s examples: %w", source.Name, err)
			}
			pkg.starter = starter.Records
		}
		snapshot := make(files, len(baseline))
		for name, content := range baseline {
			snapshot[name] = []byte(content)
		}
		if _, err := s.publish(m, snapshot, "Install "+source.Name, source.Name, nil, false, &pkg); err != nil {
			return fmt.Errorf("install %s: %w", source.Name, err)
		}
	}
	return nil
}
