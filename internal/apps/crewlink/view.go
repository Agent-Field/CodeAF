package crewlink

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func (s *Service) access(ctx context.Context, caller svc.Caller, app, typ, id string) (bridge.CrewLinkAccess, bridge.Record, *spec.Bundle, bridge.AppSummary, error) {
	if s.all == nil || s.all.Store == nil {
		return bridge.CrewLinkAccessOK, bridge.Record{}, nil, bridge.AppSummary{ID: app}, nil
	}
	detail, err := s.all.Store.Get(ctx, app)
	if err != nil {
		if codeOf(err) == bridge.CodeNotFound {
			return bridge.CrewLinkDeleted, bridge.Record{}, nil, bridge.AppSummary{}, nil
		}
		return "", bridge.Record{}, nil, bridge.AppSummary{}, err
	}
	summary := detail.AppSummary
	switch summary.State {
	case bridge.StateArchived:
		return bridge.CrewLinkArchived, bridge.Record{}, nil, summary, nil
	case bridge.StateDeleted:
		return bridge.CrewLinkDeleted, bridge.Record{}, nil, summary, nil
	}
	listed, err := s.listed(ctx, caller, app)
	if err != nil {
		return "", bridge.Record{}, nil, summary, err
	}
	if !listed {
		return bridge.CrewLinkNoAccess, bridge.Record{}, nil, summary, nil
	}
	scope := s.withData(ctx, svc.Scope{App: app, Caller: caller})
	if err := s.allowRead(ctx, scope, typ, id); err != nil {
		if codeOf(err) == bridge.CodeDenied {
			return bridge.CrewLinkNoAccess, bridge.Record{}, nil, summary, nil
		}
		return "", bridge.Record{}, nil, summary, err
	}
	if s.all.Records == nil {
		return bridge.CrewLinkAccessOK, bridge.Record{}, s.definition(ctx, app), summary, nil
	}
	rec, err := s.all.Records.Get(ctx, scope, bridge.GetRecordParams{Type: typ, ID: id})
	if err != nil {
		switch codeOf(err) {
		case bridge.CodeNotFound:
			return bridge.CrewLinkDeleted, bridge.Record{}, nil, summary, nil
		case bridge.CodeDenied:
			return bridge.CrewLinkNoAccess, bridge.Record{}, nil, summary, nil
		default:
			return "", bridge.Record{}, nil, summary, err
		}
	}
	return bridge.CrewLinkAccessOK, rec, s.definition(ctx, app), summary, nil
}

func (s *Service) listed(ctx context.Context, caller svc.Caller, app string) (bool, error) {
	rows, err := s.all.Store.List(ctx, caller)
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		if row.ID == app {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) allowRead(ctx context.Context, scope svc.Scope, typ, id string) error {
	if s.all.Caps == nil {
		return nil
	}
	raw, err := json.Marshal(bridge.GetRecordParams{Type: typ, ID: id})
	if err != nil {
		return err
	}
	return s.all.Caps.Allow(ctx, scope, bridge.MethodRecordsGet, raw)
}

func (s *Service) withData(ctx context.Context, scope svc.Scope) svc.Scope {
	if scope.Data != "" || s.all == nil || s.all.Store == nil {
		if scope.Data == "" {
			scope.Data = svc.DataSample
		}
		return scope
	}
	detail, err := s.all.Store.Get(ctx, scope.App)
	scope.Data = svc.DataSample
	if err == nil && detail.State == bridge.StateKept {
		scope.Data = svc.DataReal
	}
	return scope
}

func (s *Service) definition(ctx context.Context, app string) *spec.Bundle {
	if s.all == nil || s.all.Store == nil {
		return nil
	}
	def, err := s.all.Store.Definition(ctx, app)
	if err != nil {
		return nil
	}
	return def
}

// Present reads the record's title and the live detail line, for example
// "Proposal · $48,000". A zero amount is omitted.
func Present(def *spec.Bundle, rec bridge.Record) (label, detail string) {
	return presentRecord(def, rec)
}

func presentRecord(def *spec.Bundle, rec bridge.Record) (string, string) {
	title, stage, money := "name", "stage", "amount"
	if def != nil && def.App != nil {
		if typ := def.App.Records[rec.Type]; typ != nil {
			if typ.Title != "" {
				title = typ.Title
			}
			if key := pickField(typ.Fields, "stage", func(f *spec.Field) bool { return f.Type == spec.FieldEnum }); key != "" {
				stage = key
			}
			if key := pickField(typ.Fields, "amount", func(f *spec.Field) bool {
				return f.Type == spec.FieldMoney && f.Computed == ""
			}); key != "" {
				money = key
			}
		}
	}
	label := text(rec.Fields[title])
	if label == "" {
		label = rec.ID
	}
	return label, joinDot(text(rec.Fields[stage]), formatMoney(rec.Fields[money]))
}

func pickField(fields map[string]*spec.Field, prefer string, match func(*spec.Field) bool) string {
	if fields[prefer] != nil && match(fields[prefer]) {
		return prefer
	}
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if fields[name] != nil && match(fields[name]) {
			return name
		}
	}
	return ""
}

func joinDot(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " · ")
}

func text(value any) string {
	switch v := value.(type) {
	case string:
		return v
	default:
		return ""
	}
}

func formatMoney(value any) string {
	n, ok := number(value)
	if !ok || n == 0 {
		return ""
	}
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	cents := int64(math.Round(n * 100))
	body := groupDigits(cents / 100)
	if frac := cents % 100; frac != 0 {
		body += fmt.Sprintf(".%02d", frac)
	}
	return sign + "$" + body
}

func number(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	default:
		return 0, false
	}
}

func groupDigits(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	lead := len(s) % 3
	if lead == 0 {
		lead = 3
	}
	var b strings.Builder
	b.WriteString(s[:lead])
	for i := lead; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}
