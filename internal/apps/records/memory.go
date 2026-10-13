package records

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Service is the walking-skeleton records store: in memory, seeded with the
// golden CRM's sample.json, query = type + "sort:field" only. B2 replaces it
// with SQLite + the query compiler.
type Service struct {
	all  *svc.Services
	mu   sync.Mutex
	next int
	rows map[string]map[string]*bridge.Record // key(app, mode, type) → id → row
}

var _ svc.Records = (*Service)(nil)

// New seeds the golden CRM's sample records in both data modes.
func New(_ svc.Config, all *svc.Services) *Service {
	s := &Service{all: all, rows: map[string]map[string]*bridge.Record{}}
	_, sample := spec.GoldenCRM()
	var seed map[string][]bridge.Record
	if json.Unmarshal(sample, &seed) == nil {
		_ = s.Seed(context.Background(), "crm", seed)
		s.seedMode("crm", svc.DataReal, seed)
	}
	return s
}

func key(app string, mode svc.DataMode, recordType string) string {
	return app + "\x00" + string(mode) + "\x00" + recordType
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

// Seed replaces an app's sample records.
func (s *Service) Seed(_ context.Context, app string, rows map[string][]bridge.Record) error {
	s.seedMode(app, svc.DataSample, rows)
	return nil
}

func (s *Service) seedMode(app string, mode svc.DataMode, rows map[string][]bridge.Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for recordType, list := range rows {
		table := map[string]*bridge.Record{}
		for _, row := range list {
			row.Type, row.Version = recordType, max(row.Version, 1)
			row.CreatedAt, row.UpdatedAt, row.CreatedBy, row.UpdatedBy = now(), now(), svc.Owner, svc.Owner
			table[row.ID] = &row
		}
		s.rows[key(app, mode, recordType)] = table
	}
}

// Query returns rows of one type, sorted by an optional "sort:[-]field" token.
func (s *Service) Query(_ context.Context, scope svc.Scope, p bridge.QueryParams) (bridge.QueryResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	table, ok := s.rows[key(scope.App, scope.Data, p.Type)]
	if !ok {
		return bridge.QueryResult{}, bridge.Errorf(bridge.CodeNotFound, "no record type "+p.Type)
	}
	rows := make([]bridge.Record, 0, len(table))
	counts := map[string]int{}
	for _, row := range table {
		rows = append(rows, *row)
		if p.CountBy != "" {
			counts[fmt.Sprint(row.Fields[p.CountBy])]++
		}
	}
	sortRows(rows, p.Query)
	result := bridge.QueryResult{Rows: rows, Total: len(rows)}
	if p.CountBy != "" {
		result.Counts = counts
	}
	if limit := p.Limit; limit > 0 && limit < len(rows) {
		result.Rows = rows[:limit]
	}
	return result, nil
}

func sortRows(rows []bridge.Record, query string) {
	field, desc := "", false
	for _, token := range strings.Fields(query) {
		if value, ok := strings.CutPrefix(token, "sort:"); ok {
			field, desc = strings.TrimPrefix(value, "-"), strings.HasPrefix(value, "-")
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := sortKey(rows[i], field), sortKey(rows[j], field)
		if desc {
			return less(b, a)
		}
		return less(a, b)
	})
}

func sortKey(row bridge.Record, field string) any {
	if field == "" {
		return row.ID
	}
	return row.Fields[field]
}

func less(a, b any) bool {
	if x, ok := a.(float64); ok {
		y, _ := b.(float64)
		return x < y
	}
	return fmt.Sprint(a) < fmt.Sprint(b)
}

// Get returns one record.
func (s *Service) Get(_ context.Context, scope svc.Scope, p bridge.GetRecordParams) (bridge.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[key(scope.App, scope.Data, p.Type)][p.ID]
	if !ok {
		return bridge.Record{}, bridge.Errorf(bridge.CodeNotFound, "no "+p.Type+" "+p.ID)
	}
	return *row, nil
}

// Create inserts one record.
func (s *Service) Create(_ context.Context, scope svc.Scope, p bridge.CreateRecordParams) (bridge.Record, error) {
	s.mu.Lock()
	table, ok := s.rows[key(scope.App, scope.Data, p.Type)]
	if !ok {
		s.mu.Unlock()
		return bridge.Record{}, bridge.Errorf(bridge.CodeNotFound, "no record type "+p.Type)
	}
	s.next++
	row := &bridge.Record{ID: fmt.Sprintf("%s_%d", p.Type, s.next), Type: p.Type, Version: 1, Fields: p.Set,
		CreatedAt: now(), UpdatedAt: now(), CreatedBy: scope.Caller.Member, UpdatedBy: scope.Caller.Member}
	table[row.ID] = row
	created := *row
	s.mu.Unlock()
	s.publish(scope, bridge.RecordDiff{Op: bridge.DiffUpsert, Type: p.Type, ID: created.ID, Record: &created, By: scope.Caller.Member})
	return created, nil
}

// Update writes fields on one or more records.
func (s *Service) Update(_ context.Context, scope svc.Scope, p bridge.UpdateRecordParams) (bridge.UpdateRecordResult, error) {
	s.mu.Lock()
	table := s.rows[key(scope.App, scope.Data, p.Type)]
	var written []bridge.Record
	for _, id := range ids(p.ID, p.IDs) {
		row, ok := table[id]
		if !ok {
			s.mu.Unlock()
			return bridge.UpdateRecordResult{}, bridge.Errorf(bridge.CodeNotFound, "no "+p.Type+" "+id)
		}
		if row.Fields == nil {
			row.Fields = map[string]any{}
		}
		for field, value := range p.Set {
			row.Fields[field] = value
		}
		row.Version++
		row.UpdatedAt, row.UpdatedBy = now(), scope.Caller.Member
		written = append(written, *row)
	}
	s.mu.Unlock()
	for i := range written {
		s.publish(scope, bridge.RecordDiff{Op: bridge.DiffUpsert, Type: p.Type, ID: written[i].ID, Record: &written[i], By: scope.Caller.Member})
	}
	return bridge.UpdateRecordResult{Rows: written}, nil
}

// Delete removes one or more records.
func (s *Service) Delete(_ context.Context, scope svc.Scope, p bridge.DeleteRecordParams) (bridge.DeleteRecordResult, error) {
	s.mu.Lock()
	table := s.rows[key(scope.App, scope.Data, p.Type)]
	var deleted []string
	for _, id := range ids(p.ID, p.IDs) {
		if _, ok := table[id]; ok {
			delete(table, id)
			deleted = append(deleted, id)
		}
	}
	s.mu.Unlock()
	for _, id := range deleted {
		s.publish(scope, bridge.RecordDiff{Op: bridge.DiffDelete, Type: p.Type, ID: id, By: scope.Caller.Member})
	}
	return bridge.DeleteRecordResult{IDs: deleted}, nil
}

func (s *Service) publish(scope svc.Scope, diff bridge.RecordDiff) {
	if s.all != nil && s.all.Hub != nil {
		s.all.Hub.Publish(bridge.TopicRecords(scope.App, diff.Type), diff)
	}
}

func ids(one string, many []string) []string {
	if one != "" {
		return append([]string{one}, many...)
	}
	return many
}
