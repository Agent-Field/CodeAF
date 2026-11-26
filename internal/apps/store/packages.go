package store

import (
	"github.com/Agent-Field/codeaf/internal/apps/apppackage"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
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
