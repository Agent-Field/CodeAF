package imports

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type reference struct{ Title, ID string }
type plan struct {
	preview   bridge.ImportPreview
	mapping   []bridge.Mapping
	rows      []map[string]any
	duplicate []bool
	refs      map[string]map[string]*reference
}

func normal(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, value)
}
func titleKey(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

func autoField(app *spec.App, typ, column string) string {
	schema := app.Records[typ]
	name := normal(column)
	candidates := []string{name}
	for _, prefix := range []string{normal(typ), normal(schema.Label)} {
		if prefix != "" && strings.HasPrefix(name, prefix) {
			candidates = append(candidates, strings.TrimPrefix(name, prefix))
			break
		}
	}
	for _, candidate := range candidates {
		for _, field := range keys(schema.Fields) {
			f := schema.Fields[field]
			if f.Computed == "" && (normal(field) == candidate || f.Label != "" && normal(f.Label) == candidate) {
				return field
			}
		}
	}
	aliases := map[string]string{"amount": "value", "createdate": "created"}
	if field := aliases[name]; field != "" {
		f := schema.Fields[field]
		if f != nil && f.Computed == "" && (name != "amount" || f.Type == spec.FieldMoney) {
			return field
		}
	}
	for _, field := range keys(schema.Fields) {
		f := schema.Fields[field]
		target := app.Records[f.To]
		if f.Computed == "" && target != nil && (name == normal(f.To) || name == normal(target.Label) || name == normal(target.Plural)) {
			return field
		}
	}
	return ""
}

func mappings(app *spec.App, r *receipt) ([]bridge.Mapping, error) {
	byColumn := map[string]bridge.Mapping{}
	for _, m := range r.Mapping {
		if _, found := byColumn[m.Column]; found || !slices.Contains(r.Columns, m.Column) {
			return nil, invalid("mapping columns must exist and be distinct")
		}
		byColumn[m.Column] = m
	}
	out := make([]bridge.Mapping, len(r.Columns))
	used := map[string]bool{}
	for i, column := range r.Columns {
		m := byColumn[column]
		m.Column = column
		if m.As != "" && m.As != "field" && m.As != "note" && m.As != "skip" {
			return nil, invalid("unknown mapping choice")
		}
		if m.As == "note" || m.As == "skip" {
			m.Field = ""
			out[i] = m
			continue
		}
		if m.Field == "" {
			m.Field = autoField(app, r.Type, column)
		}
		if m.Field != "" {
			f := app.Records[r.Type].Fields[m.Field]
			if f == nil || f.Computed != "" || used[m.Field] {
				return nil, invalid("mapping must use distinct writable fields")
			}
			used[m.Field] = true
			m.As = "field"
		}
		out[i] = m
	}
	return out, nil
}

func mergeMapping(old, updates []bridge.Mapping) ([]bridge.Mapping, error) {
	out := slices.Clone(old)
	seen := map[string]bool{}
	for _, update := range updates {
		if seen[update.Column] {
			return nil, invalid("duplicate mapping column")
		}
		seen[update.Column] = true
		found := false
		for i, prior := range out {
			if prior.Column != update.Column {
				continue
			}
			found = true
			if update.As == "" {
				update.As = prior.As
			}
			if update.Field == "" && update.As == "field" {
				update.Field = prior.Field
			}
			if update.Values == nil {
				update.Values = prior.Values
			}
			if update.People == nil {
				update.People = prior.People
			}
			out[i] = update
			break
		}
		if !found {
			return nil, invalid("unknown mapping column " + update.Column)
		}
	}
	return out, nil
}

func (s *Service) evaluate(ctx context.Context, scope svc.Scope, app *spec.App, r *receipt) (*plan, error) {
	mapped, err := mappings(app, r)
	if err != nil {
		return nil, err
	}
	members, err := s.spaces.Members(ctx, scope)
	if err != nil {
		return nil, err
	}
	titles, err := s.titles(ctx, scope, r.Type, app.Records[r.Type].Title)
	if err != nil {
		return nil, err
	}
	p := &plan{mapping: mapped, refs: map[string]map[string]*reference{}, rows: make([]map[string]any, len(r.Rows)), duplicate: make([]bool, len(r.Rows)), preview: bridge.ImportPreview{
		ImportID: r.ID, Type: r.Type, Total: len(r.Rows), NewRefs: map[string]int{}, RowsShown: []bridge.Record{}, Columns: []bridge.ColumnMap{},
	}}
	for i := range p.rows {
		p.rows[i] = map[string]any{}
	}
	markDuplicates(app.Records[r.Type].Title, r, p, titles)
	for i := range p.mapping {
		column, err := s.mapColumn(ctx, scope, app, r, p, i, members)
		if err != nil {
			return nil, err
		}
		p.preview.Columns = append(p.preview.Columns, column)
		if column.NeedsChoice {
			p.preview.NeedChoice++
		}
	}
	appendNotes(app.Records[r.Type], r, p)
	for typ, refs := range p.refs {
		for _, ref := range refs {
			if ref.ID == "" {
				p.preview.NewRefs[typ]++
			}
		}
	}
	for _, fields := range p.rows[:min(5, len(p.rows))] {
		p.preview.RowsShown = append(p.preview.RowsShown, bridge.Record{Type: r.Type, Fields: fields})
	}
	p.preview.Count = len(r.Rows) - p.preview.Duplicates
	p.preview.CanImport = p.preview.NeedChoice == 0 && p.preview.Count > 0 && preflight(app, r, p, scope.Caller.Member) == nil
	return p, nil
}

func markDuplicates(title string, r *receipt, p *plan, titles map[string]string) {
	for i, m := range p.mapping {
		if m.Field != title {
			continue
		}
		for row, values := range r.Rows {
			key := titleKey(values[i])
			if key != "" && titles[key] != "" {
				p.duplicate[row] = true
				p.preview.Duplicates++
			}
			// Repeated titles inside the source are skipped as well, so one import
			// cannot manufacture duplicates that a second preview would reject.
			if key != "" {
				titles[key] = "source"
			}
		}
	}
}

func (s *Service) titles(ctx context.Context, scope svc.Scope, typ, title string) (map[string]string, error) {
	out := map[string]string{}
	after := ""
	for {
		page, err := s.all.Records.Query(ctx, scope, bridge.QueryParams{Type: typ, Limit: 100, After: after})
		if err != nil {
			return nil, err
		}
		for _, record := range page.Rows {
			if value, ok := record.Fields[title].(string); ok {
				out[titleKey(value)] = record.ID
			}
		}
		if page.Next == "" {
			return out, nil
		}
		if page.Next == after {
			return nil, fmt.Errorf("records query repeated its cursor")
		}
		after = page.Next
	}
}

func (s *Service) mapColumn(ctx context.Context, scope svc.Scope, app *spec.App, r *receipt, p *plan, index int, members []Member) (bridge.ColumnMap, error) {
	m := &p.mapping[index]
	column := bridge.ColumnMap{Column: m.Column, Field: m.Field, Match: "none", Note: "no matching field · keep as note or skip", NeedsChoice: true, First: ""}
	if len(r.Rows) > 0 {
		column.First = r.Rows[0][index]
	}
	if m.As == "note" || m.As == "skip" {
		column.NeedsChoice, column.Note = false, m.As
		return column, nil
	}
	if m.Field == "" {
		return column, nil
	}
	f := app.Records[r.Type].Fields[m.Field]
	column.Match, column.Options = matchKind(f, members)
	column.Note, column.NeedsChoice = "matched by name", false
	if column.Match == "ref" {
		if err := s.loadRefs(ctx, scope, app, p, f.To); err != nil {
			return column, err
		}
	}
	applyColumn(f, m, r, p, index, members, &column)
	switch column.Match {
	case "enum", "people":
		column.Note = choiceNote(app, f, *m, r, index)
	case "ref":
		column.Note = refNote(app, p.refs[f.To], f.To)
	}
	return column, nil
}

func matchKind(f *spec.Field, members []Member) (string, any) {
	switch f.Type {
	case spec.FieldEnum, spec.FieldEnums:
		return "enum", f.Values
	case spec.FieldPerson, spec.FieldPeople:
		return "people", members
	case spec.FieldRef, spec.FieldRefs:
		return "ref", nil
	default:
		return "name", nil
	}
}
func (s *Service) loadRefs(ctx context.Context, scope svc.Scope, app *spec.App, p *plan, typ string) error {
	if p.refs[typ] != nil {
		return nil
	}
	target := app.Records[typ]
	if target == nil {
		return invalid("unknown reference type " + typ)
	}
	existing, err := s.titles(ctx, scope, typ, target.Title)
	if err != nil {
		return err
	}
	p.refs[typ] = map[string]*reference{}
	for key, id := range existing {
		p.refs[typ][key] = &reference{ID: id}
	}
	return nil
}
func applyColumn(f *spec.Field, m *bridge.Mapping, r *receipt, p *plan, index int, members []Member, column *bridge.ColumnMap) {
	for row, values := range r.Rows {
		value, chosen, err := mapValue(f, values[index], m, members)
		if err != nil {
			column.Note = err.Error()
		}
		if !chosen || err != nil {
			column.NeedsChoice = true
		}
		p.rows[row][m.Field] = value
		if column.Match == "ref" && !p.duplicate[row] {
			addRefs(p.refs[f.To], sourceValues(f.Type, values[index]))
		}
	}
}
func addRefs(refs map[string]*reference, titles []string) {
	for _, title := range titles {
		key := titleKey(title)
		if refs[key] == nil {
			refs[key] = &reference{Title: title}
		}
	}
}
func refNote(app *spec.App, refs map[string]*reference, typ string) string {
	existing, fresh := 0, 0
	for _, ref := range refs {
		if ref.ID != "" {
			existing++
		} else {
			fresh++
		}
	}
	return fmt.Sprintf("matched to %d %s · %d new", existing, plural(app.Records[typ], typ), fresh)
}

func appendNotes(typ *spec.RecordType, r *receipt, p *plan) {
	field := ""
	for _, name := range keys(typ.Fields) {
		if typ.Fields[name].Type == spec.FieldLongtext && typ.Fields[name].Computed == "" {
			field = name
			break
		}
	}
	if field == "" {
		return
	}
	for row, values := range r.Rows {
		notes := []string{}
		if text, ok := p.rows[row][field].(string); ok && text != "" {
			notes = append(notes, text)
		}
		for i, m := range p.mapping {
			if m.As == "note" && values[i] != "" {
				notes = append(notes, m.Column+": "+values[i])
			}
		}
		if len(notes) > 0 {
			p.rows[row][field] = strings.Join(notes, "\n")
		}
	}
}
func plural(typ *spec.RecordType, fallback string) string {
	if typ.Plural != "" {
		return strings.ToLower(typ.Plural)
	}
	return fallback + "s"
}
