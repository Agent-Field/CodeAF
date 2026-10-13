package parts

import (
	"context"
	"encoding/json"
	"path"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Seed imports explicit fixture files relative to content/ or sample-content/.
// No documents are invented for an app without content.
func (s *Service) Seed(app string, data svc.DataMode, files spec.Files) error {
	if len(files) == 0 {
		return nil
	}
	scope := svc.Scope{App: app, Data: data, Caller: svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}}
	ctx := s.cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	c, err := s.open(ctx, scope)
	if err != nil {
		return err
	}
	defer c.close()
	names := make([]string, 0, len(files))
	for name := range files {
		if !validRelative(name) {
			return invalid("fixture files must be relative content paths")
		}
		if _, err = safePath(c.root, name); err != nil {
			return err
		}
		names = append(names, name)
	}
	sort.Strings(names)
	used := map[string]bool{}
	for _, name := range names {
		if path.Base(name) != "doc.json" {
			continue
		}
		doc := path.Dir(name)
		if doc == "." {
			return invalid("doc.json must be inside a document folder")
		}
		if err = c.seedDoc(ctx, doc, files[name], files, used); err != nil {
			return err
		}
		used[name] = true
	}
	for _, name := range names {
		if used[name] {
			continue
		}
		if err = c.seedSupport(name, files[name], used); err != nil {
			return err
		}
	}
	return c.commit()
}
func (c *session) seedDoc(ctx context.Context, doc string, body []byte, files spec.Files, used map[string]bool) error {
	var d descriptor
	if err := json.Unmarshal(body, &d); err != nil {
		return invalid("invalid fixture doc.json")
	}
	if err := checkDescriptor(d); err != nil {
		return err
	}
	parts := []bridge.CreateDocPart{}
	extras := map[string]*bridge.PartExtra{}
	for _, id := range d.Parts {
		name := path.Join(doc, id)
		text, ok := files[name]
		if !ok {
			return invalid("fixture is missing part " + name)
		}
		parts = append(parts, bridge.CreateDocPart{Part: id, Text: string(text)})
		used[name] = true
		if d.Surface == bridge.SurfaceGrid {
			formulas := map[string]string{}
			name = path.Join(doc, formulasPath(id))
			if body, ok := files[name]; ok {
				if err := json.Unmarshal(body, &formulas); err != nil || formulas == nil {
					return invalid("grid formulas must be a JSON object of strings")
				}
				used[name] = true
			}
			extras[id] = &bridge.PartExtra{Formulas: formulas}
		}
	}
	return c.create(ctx, doc, d, parts, extras)
}
func (c *session) seedSupport(name string, body []byte, used map[string]bool) error {
	parent := path.Dir(name)
	for parent != "." {
		if used[path.Join(parent, "doc.json")] {
			relative := strings.TrimPrefix(name, parent+"/")
			file, err := c.docPath(parent, relative)
			if err != nil {
				return err
			}
			return c.change(file, body, false)
		}
		parent = path.Dir(parent)
	}
	return invalid("fixture file is outside a document: " + name)
}
