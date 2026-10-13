package sample

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

const DefaultCount = 12

func epoch() time.Time { return time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC) }

// Service generates repeatable preview content without access to real records.
type Service struct {
	Count int
	Seed  int64
}

var _ svc.Sample = (*Service)(nil)

func New(svc.Config, *svc.Services) *Service { return &Service{Count: DefaultCount, Seed: 1} }
func (s *Service) Generate(ctx context.Context, def *spec.Bundle) (map[string][]bridge.Record, error) {
	if def == nil || def.App == nil {
		return nil, bridge.Errorf(bridge.CodeInvalid, "sample requires an app definition")
	}
	return Generate(ctx, def.App, s.Count, s.Seed)
}

// Generate allocates every id before choosing refs, so cycles and forward
// relations have valid targets. Each field gets its own deterministic stream.
func Generate(ctx context.Context, app *spec.App, count int, seed int64) (map[string][]bridge.Record, error) {
	if app == nil || count < 0 || count > 10000 {
		return nil, bridge.Errorf(bridge.CodeInvalid, "sample count must be between 0 and 10000")
	}
	rows := map[string][]bridge.Record{}
	for _, typ := range keys(app.Records) {
		if app.Records[typ] == nil {
			return nil, bridge.Errorf(bridge.CodeInvalid, "invalid record type "+typ)
		}
		rows[typ] = make([]bridge.Record, count)
		for i := range count {
			at := epoch().Add(-time.Duration(i+1) * 24 * time.Hour).Format(time.RFC3339Nano)
			rows[typ][i] = bridge.Record{ID: fmt.Sprintf("sample_%s_%04d", typ, i+1), Type: typ, Version: 1, Fields: map[string]any{}, CreatedAt: at, UpdatedAt: at, CreatedBy: svc.Owner, UpdatedBy: svc.Owner}
		}
	}
	for _, typ := range keys(app.Records) {
		schema := app.Records[typ]
		for name, f := range schema.Fields {
			if f == nil || !f.Unique {
				continue
			}
			if (f.Type == spec.FieldBool && count > 2) || ((f.Type == spec.FieldPerson || f.Type == spec.FieldPeople) && count > 1) {
				return nil, bridge.Errorf(bridge.CodeInvalid, "sample cannot make distinct values for "+typ+"."+name)
			}
		}
		rules, err := records.NewRules(schema)
		if err != nil {
			return nil, err
		}
		seen := map[string]map[string]bool{}
		for name, f := range schema.Fields {
			if f.Unique {
				seen[name] = map[string]bool{}
			}
		}
		for i := range rows[typ] {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			row := rows[typ][i]
			for _, name := range keys(schema.Fields) {
				f := schema.Fields[name]
				if f == nil {
					return nil, bridge.Errorf(bridge.CodeInvalid, "invalid sample field "+name)
				}
				if f.Computed != "" {
					continue
				}
				rng := random(seed, app.ID, typ, name, fmt.Sprint(i))
				value, err := fieldValue(rows, typ, name, schema.Title, f, i, rng)
				if err != nil {
					return nil, bridge.Errorf(bridge.CodeInvalid, typ+"."+name+": "+err.Error())
				}
				row.Fields[name] = value
				if f.AI != "" {
					row.AI = append(row.AI, name)
				}
			}
			row, err = rules.Prepare(row, svc.Owner, epoch(), true)
			if err != nil {
				return nil, err
			}
			for name, values := range seen {
				value := row.Fields[name]
				if value == nil {
					continue
				}
				data, _ := json.Marshal(value)
				key := string(data)
				if values[key] {
					return nil, bridge.Errorf(bridge.CodeInvalid, "sample cannot make distinct values for "+typ+"."+name)
				}
				values[key] = true
			}
			rows[typ][i] = row
		}
	}
	return rows, nil
}
func random(seed int64, parts ...string) *rand.Rand {
	h := fnv.New64a()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return rand.New(rand.NewSource(int64(h.Sum64()) ^ seed))
}
func keys[T any](values map[string]T) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

var names = []string{"Priya Shah", "Alex Chen", "Mona Patel", "Sam Rivera", "Jordan Lee", "Casey Morgan", "Noah Wilson", "Amara Okafor", "Sofia Garcia", "Theo Martin", "Avery Kim", "Leila Hassan"}
var companies = []string{"Northwind", "Globex", "Initech", "Hooli", "Umbrella", "Vandelay", "Acme Retail", "Stark Labs", "Pied Piper", "Cedar Works", "Atlas Studio", "Juniper Systems"}

func fieldValue(rows map[string][]bridge.Record, typ, name, title string, f *spec.Field, i int, rng *rand.Rand) (any, error) {
	if len(f.Default) > 0 && !f.Unique {
		var value any
		if err := json.Unmarshal(f.Default, &value); err != nil {
			return nil, err
		}
		if value == "$me" {
			value = svc.Owner
		}
		// Enum defaults still vary so the StateMeter and Board show each state.
		if f.Type != spec.FieldEnum {
			return value, nil
		}
	}
	index := (i + rng.Intn(len(names))) % len(names)
	switch f.Type {
	case spec.FieldText:
		value := fmt.Sprintf("%s %d", strings.ReplaceAll(name, "_", " "), i+1)
		if name == title || name == "name" {
			switch {
			case strings.Contains(typ, "company") || strings.Contains(typ, "organisation"):
				value = companies[i%len(companies)]
			case strings.Contains(typ, "contact") || strings.Contains(typ, "person") || strings.Contains(typ, "candidate"):
				value = names[i%len(names)]
			default:
				value = companies[i%len(companies)] + " · " + []string{"renewal", "team plan", "pilot", "expansion"}[i%4]
			}
			if i >= len(names) {
				value += fmt.Sprintf(" %d", i/len(names)+1)
			}
		}
		return value, nil
	case spec.FieldLongtext:
		return fmt.Sprintf("Discuss the next steps with %s. Review the proposal and confirm the timeline for phase %d.", names[index], i+1), nil
	case spec.FieldNumber:
		return float64(1 + i*20 + rng.Intn(20)), nil
	case spec.FieldMoney:
		return float64(3600 + i*1200 + rng.Intn(12)*100), nil
	case spec.FieldDate:
		return epoch().AddDate(0, 0, i+7).Format("2006-01-02"), nil
	case spec.FieldDatetime:
		return epoch().Add(-time.Duration(i+1) * 12 * time.Hour).Format(time.RFC3339Nano), nil
	case spec.FieldDuration:
		return fmt.Sprintf("PT%dH", i+1), nil
	case spec.FieldBool:
		return i%2 == 0, nil
	case spec.FieldEnum:
		if len(f.Values) == 0 || (f.Unique && i >= len(f.Values)) {
			return nil, fmt.Errorf("not enough enum values")
		}
		return f.Values[i%len(f.Values)], nil
	case spec.FieldEnums:
		if len(f.Values) == 0 {
			return nil, fmt.Errorf("enum needs values")
		}
		return []string{f.Values[i%len(f.Values)]}, nil
	case spec.FieldPerson:
		return svc.Owner, nil
	case spec.FieldPeople:
		return []string{svc.Owner}, nil
	case spec.FieldRef, spec.FieldRefs:
		targets := rows[f.To]
		if len(targets) == 0 {
			return nil, fmt.Errorf("no target records for %s", f.To)
		}
		id := targets[i%len(targets)].ID
		if f.Type == spec.FieldRefs {
			return []string{id}, nil
		}
		return id, nil
	case spec.FieldEmail:
		return fmt.Sprintf("%s.%d@%s.example", strings.ToLower(strings.Fields(names[index])[0]), i+1, domain(i)), nil
	case spec.FieldPhone:
		return fmt.Sprintf("+1 416 555 %04d", i+100), nil
	case spec.FieldURL:
		return fmt.Sprintf("https://%s.example/accounts/%d", domain(i), i+1), nil
	case spec.FieldFile:
		return fmt.Sprintf("sample-file-%s-%04d", typ, i+1), nil
	case spec.FieldFiles:
		return []string{fmt.Sprintf("sample-file-%s-%04d", typ, i+1)}, nil
	case spec.FieldPlace:
		return map[string]any{"lat": 43.65 + float64(i)*0.001, "lng": -79.38 + float64(i)*0.001, "label": fmt.Sprintf("Toronto office %d", i+1)}, nil
	default:
		return nil, fmt.Errorf("unknown field type %q", f.Type)
	}
}
func domain(i int) string {
	return strings.ReplaceAll(strings.ToLower(companies[i%len(companies)]), " ", "")
}
