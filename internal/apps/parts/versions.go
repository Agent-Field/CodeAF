package parts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func formulasPath(part string) string { return strings.TrimSuffix(part, ".csv") + ".formulas.json" }
func (c *session) sidecar(ctx context.Context, doc, part string) (string, string, error) {
	var status, owner string
	err := c.tx.QueryRowContext(ctx, `SELECT status,owner FROM part_sidecar WHERE data=? AND doc=? AND part=?`, mode(c.scope), doc, part).Scan(&status, &owner)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return status, owner, err
}
func (c *session) get(ctx context.Context, p bridge.GetPartParams, surface bridge.Surface) (bridge.Part, error) {
	result := bridge.Part{Doc: p.Doc, Part: p.Part, Surface: surface}
	if err := validPart(p.Part); err != nil {
		return result, err
	}
	if _, err := c.docPath(p.Doc, p.Part); err != nil {
		return result, err
	}
	if err := c.readVersion(ctx, p, &result); err != nil {
		return result, err
	}
	if p.Version == 0 {
		if err := c.readCurrent(&result); err != nil {
			return result, err
		}
	}
	var err error
	result.Status, result.Owner, err = c.sidecar(ctx, p.Doc, p.Part)
	if p.Version > 0 && surface == bridge.SurfaceCanvas {
		result.Status = canvasStatus(result.Text)
	}
	return result, err
}
func (c *session) readVersion(ctx context.Context, p bridge.GetPartParams, result *bridge.Part) error {
	if p.Version < 0 {
		return invalid("version cannot be negative")
	}
	query := `SELECT version,at,by,by_agent,body,extra FROM part_versions WHERE data=? AND doc=? AND part=?`
	args := []any{mode(c.scope), p.Doc, p.Part}
	if p.Version > 0 {
		query += " AND version=?"
		args = append(args, p.Version)
	}
	query += " ORDER BY version DESC LIMIT 1"
	var extra string
	err := c.tx.QueryRowContext(ctx, query, args...).Scan(&result.Version, &result.UpdatedAt, &result.UpdatedBy, &result.ByAgent, &result.Text, &extra)
	if errors.Is(err, sql.ErrNoRows) {
		return missing("part version not found")
	}
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(extra), &result.Extra)
}
func (c *session) readCurrent(part *bridge.Part) error {
	path, err := c.docPath(part.Doc, part.Part)
	if err != nil {
		return err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return ioError(err)
	}
	part.Text = string(body)
	if part.Surface == bridge.SurfaceGrid {
		part.Extra, err = c.readFormulas(part.Doc, part.Part)
	}
	return err
}
func (c *session) readFormulas(doc, part string) (*bridge.PartExtra, error) {
	path, err := c.docPath(doc, formulasPath(part))
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &bridge.PartExtra{Formulas: map[string]string{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var formulas map[string]string
	if err = json.Unmarshal(b, &formulas); err != nil || formulas == nil {
		return nil, invalid("grid formulas must be a JSON object of strings")
	}
	return &bridge.PartExtra{Formulas: formulas}, nil
}
func (s *Service) Get(ctx context.Context, scope svc.Scope, p bridge.GetPartParams) (bridge.Part, error) {
	c, err := s.open(ctx, scope)
	if err != nil {
		return bridge.Part{}, err
	}
	defer c.close()
	d, err := c.readDoc(p.Doc)
	if err != nil {
		return bridge.Part{}, err
	}
	// Historical bodies remain readable after the part is removed from the order.
	if p.Version == 0 {
		if _, err = c.requirePart(ctx, p.Doc, p.Part); err != nil {
			return bridge.Part{}, err
		}
	}
	return c.get(ctx, p, d.Surface)
}
func (c *session) write(ctx context.Context, p bridge.WritePartParams, surface bridge.Surface) (bridge.Part, error) {
	part, err := c.prepareWrite(ctx, p, surface)
	if err != nil {
		return part, err
	}
	if err = c.recordVersion(ctx, p, part); err != nil {
		return part, err
	}
	if err = c.writeBody(part); err != nil {
		return part, err
	}
	c.event(p.Doc, bridge.DocEvent{Kind: bridge.DocEventKindPart, Part: p.Part, Version: part.Version})
	return part, nil
}
func (c *session) prepareWrite(ctx context.Context, p bridge.WritePartParams, surface bridge.Surface) (bridge.Part, error) {
	part := bridge.Part{Doc: p.Doc, Part: p.Part, Surface: surface, Text: p.Text, Extra: p.Extra}
	if err := validPart(p.Part); err != nil {
		return part, err
	}
	current, err := c.version(ctx, p.Doc, p.Part)
	if err != nil {
		return part, err
	}
	if p.BaseVersion >= 0 && current != p.BaseVersion {
		return part, conflict(fmt.Sprintf("stale base_version: current version is %d", current))
	}
	part.Extra, err = c.writeExtra(p, surface)
	if err != nil {
		return part, err
	}
	if err = validate(surface, p.Text, part.Extra); err != nil {
		return part, err
	}
	part.Status, part.Owner, err = c.sidecar(ctx, p.Doc, p.Part)
	if err != nil {
		return part, err
	}
	if surface == bridge.SurfaceCanvas {
		part.Status = canvasStatus(p.Text)
	}
	part.Version = current + 1
	part.UpdatedAt = timestamp()
	part.UpdatedBy = c.scope.Caller.Member
	part.ByAgent = c.scope.Caller.Agent
	return part, nil
}
func (c *session) writeExtra(p bridge.WritePartParams, surface bridge.Surface) (*bridge.PartExtra, error) {
	if surface != bridge.SurfaceGrid {
		return p.Extra, nil
	}
	if !strings.HasSuffix(p.Part, ".csv") {
		return nil, invalid("grid part must be a .csv file")
	}
	extra := p.Extra
	if extra == nil {
		return c.readFormulas(p.Doc, p.Part)
	}
	if extra.Formulas == nil {
		extra = &bridge.PartExtra{Formulas: map[string]string{}}
	}
	return extra, nil
}
func (c *session) recordVersion(ctx context.Context, p bridge.WritePartParams, part bridge.Part) error {
	_, err := c.tx.ExecContext(ctx, `INSERT INTO part_versions(data,doc,part,version,kind,label,by,by_agent,at,body,extra) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, mode(c.scope), p.Doc, p.Part, part.Version, p.Kind, p.Label, part.UpdatedBy, part.ByAgent, part.UpdatedAt, p.Text, marshal(part.Extra))
	if err != nil {
		return err
	}
	_, err = c.tx.ExecContext(ctx, `INSERT INTO part_sidecar(data,doc,part,status,owner) VALUES(?,?,?,?,?) ON CONFLICT(data,doc,part) DO UPDATE SET status=excluded.status`, mode(c.scope), p.Doc, p.Part, part.Status, part.Owner)
	return err
}
func (c *session) writeBody(part bridge.Part) error {
	path, err := c.docPath(part.Doc, part.Part)
	if err != nil {
		return err
	}
	if err = c.change(path, []byte(part.Text), false); err != nil {
		return err
	}
	if part.Surface != bridge.SurfaceGrid {
		return nil
	}
	path, err = c.docPath(part.Doc, formulasPath(part.Part))
	if err != nil {
		return err
	}
	return c.change(path, []byte(marshal(part.Extra.Formulas)), false)
}
func canvasStatus(text string) string {
	var v struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal([]byte(text), &v)
	if v.Status == "" {
		return "Draft"
	}
	return v.Status
}
func (s *Service) Write(ctx context.Context, scope svc.Scope, p bridge.WritePartParams) (bridge.Part, error) {
	if p.Kind != bridge.PartVersionKindNudge && p.Kind != bridge.PartVersionKindAsk {
		return bridge.Part{}, invalid("write kind must be nudge or ask")
	}
	if p.BaseVersion < 0 {
		return bridge.Part{}, invalid("base_version cannot be negative")
	}
	c, err := s.open(ctx, scope)
	if err != nil {
		return bridge.Part{}, err
	}
	defer c.close()
	d, err := c.requirePart(ctx, p.Doc, p.Part)
	if err != nil {
		return bridge.Part{}, err
	}
	result, err := c.write(ctx, p, d.Surface)
	if err != nil {
		return result, err
	}
	return result, c.commit()
}
func (s *Service) Versions(ctx context.Context, scope svc.Scope, p bridge.ListPartVersionsParams) (bridge.ListPartVersionsResult, error) {
	result := bridge.ListPartVersionsResult{Versions: []bridge.PartVersion{}}
	c, err := s.open(ctx, scope)
	if err != nil {
		return result, err
	}
	defer c.close()
	if _, err = c.readDoc(p.Doc); err != nil {
		return result, err
	}
	if err = validPart(p.Part); err != nil {
		return result, err
	}
	if _, err = c.docPath(p.Doc, p.Part); err != nil {
		return result, err
	}
	rows, err := c.tx.QueryContext(ctx, `SELECT version,kind,label,by,by_agent,at FROM part_versions WHERE data=? AND doc=? AND part=? ORDER BY version DESC`, mode(scope), p.Doc, p.Part)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		v := bridge.PartVersion{Doc: p.Doc, Part: p.Part}
		if err = rows.Scan(&v.Version, &v.Kind, &v.Label, &v.By, &v.ByAgent, &v.At); err != nil {
			return result, err
		}
		result.Versions = append(result.Versions, v)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if len(result.Versions) == 0 {
		return result, missing("part not found")
	}
	return result, nil
}
func (s *Service) Restore(ctx context.Context, scope svc.Scope, p bridge.RestorePartParams) (result bridge.Part, err error) {
	if p.Version < 1 {
		return result, invalid("restore version must be positive")
	}
	if s.all != nil && s.all.Collab != nil {
		if _, err = s.all.Collab.Lock(ctx, scope, bridge.LockPartParams{Doc: p.Doc, Part: p.Part}); err != nil {
			return result, err
		}
		defer func() {
			_, releaseErr := s.all.Collab.Release(context.WithoutCancel(ctx), scope, bridge.ReleasePartParams{Doc: p.Doc, Part: p.Part})
			err = errors.Join(err, releaseErr)
		}()
	}
	c, err := s.open(ctx, scope)
	if err != nil {
		return result, err
	}
	defer c.close()
	d, err := c.requirePart(ctx, p.Doc, p.Part)
	if err != nil {
		return result, err
	}
	old, err := c.get(ctx, bridge.GetPartParams{Doc: p.Doc, Part: p.Part, Version: p.Version}, d.Surface)
	if err != nil {
		return result, err
	}
	result, err = c.write(ctx, bridge.WritePartParams{Doc: p.Doc, Part: p.Part, Text: old.Text, Extra: old.Extra, Kind: bridge.PartVersionKindRestore, Label: fmt.Sprintf("Restore v%d", p.Version), BaseVersion: -1}, d.Surface)
	if err != nil {
		return result, err
	}
	return result, c.commit()
}
