package imports

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func real(scope svc.Scope) error {
	if scope.Data != svc.DataReal {
		return reason(bridge.CodeConflict, "real_only", "imports write real records; keep or apply the app first")
	}
	return nil
}
func (s *Service) Run(ctx context.Context, scope svc.Scope, params bridge.ImportRunParams) (bridge.ImportResult, error) {
	if err := s.ready(bridge.MethodImportsRun); err != nil {
		return bridge.ImportResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := real(scope); err != nil {
		return bridge.ImportResult{}, err
	}
	r, err := s.load(scope, params.ImportID)
	if err != nil {
		return bridge.ImportResult{}, err
	}
	if r.Member != scope.Caller.Member {
		return bridge.ImportResult{}, bridge.Errorf(bridge.CodeDenied, "only the person who previewed this import can run it")
	}
	if r.Status == "done" {
		return s.finish(ctx, scope, r)
	}
	if r.Status != "preview" {
		return bridge.ImportResult{}, bridge.Errorf(bridge.CodeConflict, "import has already started; use Undo")
	}
	app, err := s.definition(ctx, scope, r.Type)
	if err != nil {
		return bridge.ImportResult{}, err
	}
	r.Mapping, err = mergeMapping(r.Mapping, params.Mapping)
	if err != nil {
		return bridge.ImportResult{}, err
	}
	p, err := s.evaluate(ctx, scope, app, r)
	if err != nil {
		return bridge.ImportResult{}, err
	}
	if p.preview.NeedChoice > 0 {
		return bridge.ImportResult{}, reason(bridge.CodeInvalid, bridge.ReasonNeedsChoice, "choose a mapping for every column before importing")
	}
	if p.preview.Count == 0 {
		return bridge.ImportResult{}, invalid("no records to import")
	}
	if err := preflight(app, r, p, scope.Caller.Member); err != nil {
		return bridge.ImportResult{}, err
	}
	r.Mapping, r.Status = p.mapping, "running"
	if err := s.save(r); err != nil {
		return bridge.ImportResult{}, err
	}
	if err := s.createRefs(ctx, scope, app, r, p); err != nil {
		return bridge.ImportResult{}, err
	}
	count := 0
	for row, fields := range p.rows {
		if p.duplicate[row] {
			continue
		}
		replaceRefs(app.Records[r.Type], fields, p.refs)
		if _, err := s.create(ctx, scope, r, r.Type, fields); err != nil {
			return bridge.ImportResult{}, err
		}
		count++
	}
	text, detail := activityText(scope, app, r, count, p.preview.Duplicates, p.preview.NewRefs)
	activity := text
	if detail != "" {
		activity += " · " + detail
	}
	r.Result = &bridge.ImportResult{ImportID: r.ID, Created: count, Skipped: p.preview.Duplicates, NewRefs: p.preview.NewRefs, Activity: activity}
	r.ActivityText, r.ActivityDetail = text, detail
	r.Status = "done"
	if err := s.save(r); err != nil {
		return bridge.ImportResult{}, err
	}
	return s.finish(ctx, scope, r)
}

// Validate every row and new reference before the first write so a bad date,
// missing required field or computed expression cannot leave a partial import.
func preflight(app *spec.App, r *receipt, p *plan, member string) error {
	compiled := map[string]*records.Rules{}
	at := time.Now().UTC()
	validate := func(typ string, fields map[string]any) error {
		rules := compiled[typ]
		if rules == nil {
			var err error
			rules, err = records.NewRules(app.Records[typ])
			if err != nil {
				return err
			}
			compiled[typ] = rules
		}
		return validateCreate(rules, fields, member, at)
	}
	for _, typ := range keys(p.refs) {
		for _, key := range keys(p.refs[typ]) {
			ref := p.refs[typ][key]
			if ref.ID != "" {
				continue
			}
			if err := validate(typ, map[string]any{app.Records[typ].Title: ref.Title}); err != nil {
				return err
			}
		}
	}
	for i, fields := range p.rows {
		if !p.duplicate[i] {
			if err := validate(r.Type, fields); err != nil {
				return err
			}
		}
	}
	return nil
}
func validateCreate(rules *records.Rules, fields map[string]any, member string, at time.Time) error {
	row, err := rules.Prepare(bridge.Record{Fields: fields, CreatedAt: at.Format(time.RFC3339Nano), UpdatedAt: at.Format(time.RFC3339Nano)}, member, at, true)
	if err != nil {
		return err
	}
	for field, value := range fields {
		if value == nil {
			row.Fields[field] = nil
		}
	}
	_, err = rules.Prepare(row, member, at, false)
	return err
}

func (s *Service) createRefs(ctx context.Context, scope svc.Scope, app *spec.App, r *receipt, p *plan) error {
	for _, typ := range keys(p.refs) {
		for _, key := range keys(p.refs[typ]) {
			ref := p.refs[typ][key]
			if ref.ID != "" {
				continue
			}
			row, err := s.create(ctx, scope, r, typ, map[string]any{app.Records[typ].Title: ref.Title})
			if err != nil {
				return err
			}
			ref.ID = row.ID
		}
	}
	return nil
}
func replaceRefs(typ *spec.RecordType, fields map[string]any, refs map[string]map[string]*reference) {
	for name, field := range typ.Fields {
		if fields[name] == nil {
			continue
		}
		switch field.Type {
		case spec.FieldRef:
			fields[name] = refs[field.To][titleKey(fields[name].(string))].ID
		case spec.FieldRefs:
			ids := []any{}
			for _, title := range fields[name].([]any) {
				ids = append(ids, refs[field.To][titleKey(title.(string))].ID)
			}
			fields[name] = ids
		}
	}
}
func (s *Service) create(ctx context.Context, scope svc.Scope, r *receipt, typ string, fields map[string]any) (bridge.Record, error) {
	row, err := s.all.Records.Create(ctx, scope, bridge.CreateRecordParams{Type: typ, Set: fields})
	if err != nil {
		return row, err
	}
	r.Created = append(r.Created, importedRecord{Type: typ, ID: row.ID, Version: row.Version})
	if err := s.save(r); err != nil {
		return row, err
	}
	clear := map[string]any{}
	for name, value := range fields {
		if value == nil && row.Fields[name] != nil {
			clear[name] = nil
		}
	}
	if len(clear) > 0 {
		updated, err := s.all.Records.Update(ctx, scope, bridge.UpdateRecordParams{Type: typ, ID: row.ID, Set: clear})
		if err != nil {
			return row, err
		}
		row = updated.Rows[0]
		r.Created[len(r.Created)-1].Version = row.Version
		if err := s.save(r); err != nil {
			return row, err
		}
	}
	return row, nil
}

func activityText(scope svc.Scope, app *spec.App, r *receipt, count, skipped int, refs map[string]int) (string, string) {
	source := sourceLabel(r.Source)
	text := fmt.Sprintf("@%s imported %d %s into %s from %s", strings.TrimPrefix(scope.Caller.Member, "@"), count, plural(app.Records[r.Type], r.Type), app.Name, source)
	details := []string{}
	if skipped > 0 {
		details = append(details, fmt.Sprintf("%d skipped as duplicates", skipped))
	}
	for _, typ := range keys(refs) {
		if refs[typ] > 0 {
			details = append(details, fmt.Sprintf("%d new %s", refs[typ], plural(app.Records[typ], typ)))
		}
	}
	return text, strings.Join(details, " · ")
}
func (s *Service) finish(ctx context.Context, scope svc.Scope, r *receipt) (bridge.ImportResult, error) {
	if r.Result == nil {
		return bridge.ImportResult{}, bridge.Errorf(bridge.CodeConflict, "import has no result")
	}
	if !r.ActivityWritten {
		entry := ActivityEntry{Scope: scope, Action: "import.run", Object: r.ID, Text: r.ActivityText, Detail: r.ActivityDetail, UndoMethod: bridge.MethodImportsUndo, UndoParams: map[string]any{"import_id": r.ID}}
		if err := s.spaces.Append(ctx, entry); err != nil {
			return bridge.ImportResult{}, err
		}
		r.ActivityWritten = true
		if err := s.save(r); err != nil {
			return bridge.ImportResult{}, err
		}
	}
	// A result must not expose maps owned by a cached receipt.
	result := *r.Result
	result.NewRefs = map[string]int{}
	for typ, count := range r.Result.NewRefs {
		result.NewRefs[typ] = count
	}
	return result, nil
}
