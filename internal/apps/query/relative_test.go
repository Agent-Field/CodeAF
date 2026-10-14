package query

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

var relativeNow = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

func TestRelativeValuesResolveAgainstTheClock(t *testing.T) {
	schema := &spec.RecordType{Fields: map[string]*spec.Field{"due": {Type: spec.FieldDate}, "touched": {Type: spec.FieldDatetime}, "name": {Type: spec.FieldText}}}
	tests := []struct {
		query string
		want  any
	}{
		{"due<0d", "2026-10-13"},
		{"due<+7d", "2026-10-20"},
		{"due>-2w", "2026-09-29"},
		{"touched<-7d", "2026-10-06T12:00:00.000000000Z"},
		{"name:7d", "7d"}, // text keeps its literal value
	}
	for _, tt := range tests {
		a, err := ParseAt(schema, tt.query, "owner", relativeNow)
		if err != nil {
			t.Fatalf("%s: %v", tt.query, err)
		}
		if got := a.Filters[0].Values[0]; got != tt.want {
			t.Errorf("%s resolved to %v, want %v", tt.query, got, tt.want)
		}
	}
}

// One query over real rows: a daily-view filter "due this week" keeps only
// the rows the clock puts in the window.
func TestRelativeDateQueryOverRecords(t *testing.T) {
	schema := &spec.RecordType{Fields: map[string]*spec.Field{"due": {Type: spec.FieldDate}}}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE records_deal (id TEXT PRIMARY KEY, record TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for id, due := range map[string]string{"late": "2026-10-10", "soon": "2026-10-15", "later": "2026-10-30"} {
		data, _ := json.Marshal(map[string]any{"id": id, "type": "deal", "fields": map[string]any{"due": due}})
		if _, err := db.Exec(`INSERT INTO records_deal VALUES (?,?)`, id, string(data)); err != nil {
			t.Fatal(err)
		}
	}
	for query, want := range map[string][]string{
		"due<0d sort:due":         {"late"},
		"due>-1d due<7d sort:due": {"soon"},
		"due<7d sort:due":         {"late", "soon"},
	} {
		a, err := ParseAt(schema, query, "owner", relativeNow)
		if err != nil {
			t.Fatal(err)
		}
		if got := selected(t, db, schema, a); !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v, want %v", query, got, want)
		}
	}
}

// The forms a model actually writes: a colon before the operator, >= and <=,
// day words, and CEL by mistake (which must be named, with the real forms).
func TestLiberalRelativeForms(t *testing.T) {
	schema := &spec.RecordType{Fields: map[string]*spec.Field{"due": {Type: spec.FieldDate}, "starts": {Type: spec.FieldDatetime}}}
	type want struct {
		op    Op
		value any
	}
	tests := map[string][]want{
		"due:<0d":         {{Lt, "2026-10-13"}},
		"due<=0d":         {{Le, "2026-10-13"}},
		"due>=-30d":       {{Ge, "2026-09-13"}},
		"starts>=-0d":     {{Ge, "2026-10-13T12:00:00.000000000Z"}},
		"due:>=today":     {{Ge, "2026-10-13"}},
		"due<tomorrow":    {{Lt, "2026-10-14"}},
		"starts<now":      {{Lt, "2026-10-13T12:00:00.000000000Z"}},
		"due:today":       {{Ge, "2026-10-13"}, {Lt, "2026-10-14"}},
		"due:yesterday":   {{Ge, "2026-10-12"}, {Lt, "2026-10-13"}},
		"starts:tomorrow": {{Ge, "2026-10-14T00:00:00.000000000Z"}, {Lt, "2026-10-15T00:00:00.000000000Z"}},
	}
	for query, wants := range tests {
		a, err := ParseAt(schema, query, "owner", relativeNow)
		if err != nil {
			t.Errorf("%s: %v", query, err)
			continue
		}
		if len(a.Filters) != len(wants) {
			t.Errorf("%s: %d filters, want %d", query, len(a.Filters), len(wants))
			continue
		}
		for i, w := range wants {
			if a.Filters[i].Op != w.op || a.Filters[i].Values[0] != w.value {
				t.Errorf("%s filter %d = %s %v, want %s %v", query, i, a.Filters[i].Op, a.Filters[i].Values[0], w.op, w.value)
			}
		}
	}
	for _, cel := range []string{"due < now", "due < now + 14d"} {
		_, err := ParseAt(schema, cel, "owner", relativeNow)
		if err == nil || !strings.Contains(err.Error(), "not CEL") || !strings.Contains(err.Error(), "field<7d, field>=-30d, field:today") {
			t.Errorf("%q: err = %v, want the token named with the real forms", cel, err)
		}
	}
}
