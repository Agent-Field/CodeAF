package parts

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type descriptor struct {
	Surface bridge.Surface `json:"surface"`
	Title   string         `json:"title"`
	Parts   []string       `json:"parts"`
}

func (c *session) readDoc(doc string) (descriptor, error) {
	var d descriptor
	b, err := c.readFile(doc, "doc.json")
	if err != nil {
		return d, ioError(err)
	}
	if err = json.Unmarshal(b, &d); err != nil {
		return d, invalid("doc.json must contain surface, title and parts")
	}
	if err = checkDescriptor(d); err != nil {
		return d, err
	}
	return d, nil
}
func checkDescriptor(d descriptor) error {
	if !knownSurface(d.Surface) || d.Parts == nil {
		return invalid("doc.json needs a supported surface and a parts array")
	}
	seen := map[string]bool{}
	for _, part := range d.Parts {
		if err := validPart(part); err != nil {
			return err
		}
		if seen[part] {
			return invalid("duplicate part in doc.json")
		}
		seen[part] = true
	}
	return nil
}
func (c *session) saveDoc(doc string, d descriptor) error {
	path, err := c.docPath(doc, "doc.json")
	if err != nil {
		return err
	}
	return c.change(path, []byte(marshal(d)), false)
}
func (c *session) info(ctx context.Context, doc string) (bridge.DocInfo, error) {
	d, err := c.readDoc(doc)
	if err != nil {
		return bridge.DocInfo{}, err
	}
	result := bridge.DocInfo{Doc: doc, Surface: d.Surface, Title: d.Title, Parts: []bridge.PartSummary{}}
	for _, id := range d.Parts {
		part, err := c.get(ctx, bridge.GetPartParams{Doc: doc, Part: id}, d.Surface)
		if err != nil {
			return result, err
		}
		result.Parts = append(result.Parts, bridge.PartSummary{Part: id, Version: part.Version, Status: part.Status, Title: partTitle(part.Text, d.Surface)})
	}
	result.Share, err = c.share(ctx, doc)
	return result, err
}
func partTitle(body string, surface bridge.Surface) string {
	if surface == bridge.SurfaceText {
		for _, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(line, "# ") {
				return strings.TrimSpace(line[2:])
			}
		}
		return ""
	}
	var v struct {
		Title string `json:"title"`
	}
	_ = json.Unmarshal([]byte(body), &v)
	return v.Title
}
func (s *Service) GetDoc(ctx context.Context, scope svc.Scope, p bridge.GetDocParams) (bridge.DocInfo, error) {
	c, err := s.open(ctx, scope)
	if err != nil {
		return bridge.DocInfo{}, err
	}
	defer c.close()
	return c.info(ctx, p.Doc)
}
func (s *Service) ListDocs(ctx context.Context, scope svc.Scope, p bridge.ListDocsParams) (bridge.ListDocsResult, error) {
	result := bridge.ListDocsResult{Docs: []bridge.DocInfo{}}
	if p.Surface != "" && !knownSurface(p.Surface) {
		return result, invalid("unknown surface")
	}
	c, err := s.open(ctx, scope)
	if err != nil {
		return result, err
	}
	defer c.close()
	ids, err := c.documentIDs()
	if err != nil {
		return result, err
	}
	for _, id := range ids {
		doc, err := c.info(ctx, id)
		if err != nil {
			return result, err
		}
		if p.Surface == "" || doc.Surface == p.Surface {
			result.Docs = append(result.Docs, doc)
		}
	}

	return result, err
}
func (s *Service) CreateDoc(ctx context.Context, scope svc.Scope, p bridge.CreateDocParams) (bridge.DocInfo, error) {
	c, err := s.open(ctx, scope)
	if err != nil {
		return bridge.DocInfo{}, err
	}
	defer c.close()
	d := descriptor{Surface: p.Surface, Title: p.Title, Parts: []string{}}
	for _, part := range p.Parts {
		d.Parts = append(d.Parts, part.Part)
	}
	if err = checkDescriptor(d); err != nil {
		return bridge.DocInfo{}, err
	}
	if err = c.create(ctx, p.Doc, d, p.Parts, nil); err != nil {
		return bridge.DocInfo{}, err
	}
	result, err := c.info(ctx, p.Doc)
	if err != nil {
		return result, err
	}
	return result, c.commit()
}
func (c *session) create(ctx context.Context, doc string, d descriptor, parts []bridge.CreateDocPart, extras map[string]*bridge.PartExtra) error {
	_, err := c.readFile(doc, "doc.json")
	if err == nil {
		return conflict("document already exists")
	}
	if !os.IsNotExist(err) {
		return err
	}
	for _, p := range parts {
		if _, err = c.write(ctx, bridge.WritePartParams{Doc: doc, Part: p.Part, Text: p.Text, Extra: extras[p.Part], Kind: bridge.PartVersionKindCreate, Label: "Create", BaseVersion: -1}, d.Surface); err != nil {
			return err
		}
	}
	if err = c.saveDoc(doc, d); err != nil {
		return err
	}
	c.event(doc, bridge.DocEvent{Kind: bridge.DocEventKindOrder})
	return nil
}
func (s *Service) Reorder(ctx context.Context, scope svc.Scope, p bridge.ReorderDocParams) (bridge.DocInfo, error) {
	c, err := s.open(ctx, scope)
	if err != nil {
		return bridge.DocInfo{}, err
	}
	defer c.close()
	d, err := c.readDoc(p.Doc)
	if err != nil {
		return bridge.DocInfo{}, err
	}
	before := slices.Clone(d.Parts)
	after := slices.Clone(p.Parts)
	slices.Sort(before)
	slices.Sort(after)
	if !slices.Equal(before, after) {
		return bridge.DocInfo{}, invalid("reorder must contain every current part exactly once")
	}
	d.Parts = slices.Clone(p.Parts)
	if d.Parts == nil {
		d.Parts = []string{}
	}
	if err = c.saveDoc(p.Doc, d); err != nil {
		return bridge.DocInfo{}, err
	}
	c.event(p.Doc, bridge.DocEvent{Kind: bridge.DocEventKindOrder})
	result, err := c.info(ctx, p.Doc)
	if err != nil {
		return result, err
	}
	return result, c.commit()
}
func (s *Service) Add(ctx context.Context, scope svc.Scope, p bridge.AddPartParams) (bridge.DocInfo, error) {
	c, err := s.open(ctx, scope)
	if err != nil {
		return bridge.DocInfo{}, err
	}
	defer c.close()
	d, err := c.readDoc(p.Doc)
	if err != nil {
		return bridge.DocInfo{}, err
	}
	if slices.Contains(d.Parts, p.Part) {
		return bridge.DocInfo{}, conflict("part already exists")
	}
	if _, err = c.write(ctx, bridge.WritePartParams{Doc: p.Doc, Part: p.Part, Text: p.Text, Kind: bridge.PartVersionKindCreate, Label: "Create", BaseVersion: -1}, d.Surface); err != nil {
		return bridge.DocInfo{}, err
	}
	d.Parts = append(d.Parts, p.Part)
	if err = c.saveDoc(p.Doc, d); err != nil {
		return bridge.DocInfo{}, err
	}
	c.event(p.Doc, bridge.DocEvent{Kind: bridge.DocEventKindOrder})
	result, err := c.info(ctx, p.Doc)
	if err != nil {
		return result, err
	}
	return result, c.commit()
}
func (s *Service) Remove(ctx context.Context, scope svc.Scope, p bridge.RemovePartParams) (bridge.DocInfo, error) {
	c, err := s.open(ctx, scope)
	if err != nil {
		return bridge.DocInfo{}, err
	}
	defer c.close()
	d, err := c.requirePart(ctx, p.Doc, p.Part)
	if err != nil {
		return bridge.DocInfo{}, err
	}
	path, err := c.docPath(p.Doc, p.Part)
	if err != nil {
		return bridge.DocInfo{}, err
	}
	if err = c.change(path, nil, true); err != nil {
		return bridge.DocInfo{}, err
	}
	if d.Surface == bridge.SurfaceGrid {
		path, err = c.docPath(p.Doc, formulasPath(p.Part))
		if err != nil {
			return bridge.DocInfo{}, err
		}
		if err = c.change(path, nil, true); err != nil {
			return bridge.DocInfo{}, err
		}
	}
	d.Parts = slices.DeleteFunc(d.Parts, func(id string) bool { return id == p.Part })
	if err = c.saveDoc(p.Doc, d); err != nil {
		return bridge.DocInfo{}, err
	}
	c.event(p.Doc, bridge.DocEvent{Kind: bridge.DocEventKindOrder, Part: p.Part})
	result, err := c.info(ctx, p.Doc)
	if err != nil {
		return result, err
	}
	return result, c.commit()
}
