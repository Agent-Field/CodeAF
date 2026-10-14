package sample

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math/rand"
	"sort"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

const DefaultCount = 12

// Service generates repeatable preview content without access to real records.
type Service struct {
	Count int
	Seed  int64
	Now   func() time.Time
	all   *svc.Services
	words vocabCache
}

var _ svc.Sample = (*Service)(nil)

func New(_ svc.Config, all *svc.Services) *Service {
	return &Service{Count: DefaultCount, Seed: 1, Now: time.Now, all: all}
}
func (s *Service) Generate(ctx context.Context, def *spec.Bundle) (map[string][]bridge.Record, error) {
	if def == nil || def.App == nil {
		return nil, bridge.Errorf(bridge.CodeInvalid, "sample requires an app definition")
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	at := now()
	// Model vocabulary is a bonus: any failure, or values that break a
	// uniqueness rule, falls back to the deterministic generator.
	if words := s.words.get(ctx, s.model(), def); words != nil {
		if rows, err := generate(ctx, def.App, s.Count, s.Seed, at, words); err == nil {
			return rows, nil
		}
	}
	return GenerateAt(ctx, def.App, s.Count, s.Seed, at)
}

func (s *Service) model() svc.Model {
	if s.all == nil {
		return nil
	}
	return s.all.Model
}

// Generate allocates every id before choosing refs, so cycles and forward
// relations have valid targets. Dates are relative to the current time.
func Generate(ctx context.Context, app *spec.App, count int, seed int64) (map[string][]bridge.Record, error) {
	return GenerateAt(ctx, app, count, seed, time.Now())
}

// GenerateAt fixes the clock as well as the seed for repeatable Check fixtures.
func GenerateAt(ctx context.Context, app *spec.App, count int, seed int64, now time.Time) (map[string][]bridge.Record, error) {
	return generate(ctx, app, count, seed, now, nil)
}

func generate(ctx context.Context, app *spec.App, count int, seed int64, now time.Time, words *vocab) (map[string][]bridge.Record, error) {
	now = now.UTC()
	if app == nil || count < 0 || count > 10000 {
		return nil, bridge.Errorf(bridge.CodeInvalid, "sample count must be between 0 and 10000")
	}
	rows := map[string][]bridge.Record{}
	for _, typ := range keys(app.Records) {
		if app.Records[typ] == nil {
			return nil, bridge.Errorf(bridge.CodeInvalid, "invalid record type "+typ)
		}
	}
	counts := rowCounts(app, count)
	for _, typ := range keys(app.Records) {
		rows[typ] = make([]bridge.Record, counts[typ])
		for i := range counts[typ] {
			at := now.Add(-time.Duration(i%30+1) * 24 * time.Hour).Format(time.RFC3339Nano)
			rows[typ][i] = bridge.Record{ID: fmt.Sprintf("sample_%s_%04d", typ, i+1), Type: typ, Version: 1, Fields: map[string]any{}, CreatedAt: at, UpdatedAt: at, CreatedBy: svc.Owner, UpdatedBy: svc.Owner}
		}
	}
	for _, typ := range keys(app.Records) {
		if err := fillType(ctx, app, rows, typ, seed, now, words); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

func fillType(ctx context.Context, app *spec.App, rows map[string][]bridge.Record, typ string, seed int64, now time.Time, words *vocab) error {
	schema := app.Records[typ]
	rules, err := records.NewRules(schema)
	if err != nil {
		return err
	}
	seen := map[string]map[string]bool{}
	for name, f := range schema.Fields {
		if f.Unique || name == schema.Title && distinctTitle(schema) {
			seen[name] = map[string]bool{}
		}
	}
	for i, row := range rows[typ] {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := fillFields(app, rows, schema, &row, i, seed, now, words, seen); err != nil {
			return err
		}
		row, err = rules.Prepare(row, svc.Owner, now, true)
		if err != nil {
			return err
		}
		if distinctTitle(schema) && !readableTitle(fmt.Sprint(row.Fields[schema.Title])) {
			return bridge.Errorf(bridge.CodeInvalid, "sample needs a readable title for "+typ)
		}
		if err := checkUnique(row, seen); err != nil {
			return err
		}
		rows[typ][i] = row
	}
	return nil
}

func fillFields(app *spec.App, rows map[string][]bridge.Record, schema *spec.RecordType, row *bridge.Record, i int, seed int64, now time.Time, words *vocab, seen map[string]map[string]bool) error {
	c := cell{app: app, rows: rows, typ: row.Type, schema: schema, i: i, now: now, words: words}
	// The title goes first so notes, emails and file names can mention it.
	for _, name := range titleFirst(schema) {
		f := schema.Fields[name]
		if f.Computed != "" {
			continue
		}
		c.name, c.f = name, f
		c.rng = random(seed, app.ID, row.Type, name, fmt.Sprint(i))
		value, err := fieldValue(c)
		if err != nil {
			return bridge.Errorf(bridge.CodeInvalid, row.Type+"."+name+": "+err.Error())
		}
		if text, ok := value.(string); ok && seen[name] != nil && textual(f.Type) {
			value = distinctText(text, seen[name])
		}
		row.Fields[name] = value
		if name == schema.Title {
			c.title, _ = value.(string)
		}
		if f.AI != "" {
			row.AI = append(row.AI, name)
		}
	}
	return nil
}

func titleFirst(schema *spec.RecordType) []string {
	names := keys(schema.Fields)
	sort.SliceStable(names, func(a, b int) bool { return names[a] == schema.Title && names[b] != schema.Title })
	return names
}

func checkUnique(row bridge.Record, seen map[string]map[string]bool) error {
	for name, values := range seen {
		value := row.Fields[name]
		if value == nil {
			continue
		}
		key := quoted(value)
		if values[key] {
			return bridge.Errorf(bridge.CodeInvalid, "sample cannot make distinct values for "+row.Type+"."+name)
		}
		values[key] = true
	}
	return nil
}

func quoted(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
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
