package data

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// clock is a test's clock, moved by hand.
type clock struct {
	mu sync.Mutex
	at time.Time
}

func newClock() *clock { return &clock{at: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)} }

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(time.Millisecond)
	return c.at
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// openCRM is a fresh store with the CRM registered.
func openCRM(t *testing.T) (*Store, *clock) {
	t.Helper()
	c := newClock()
	s, err := Open(filepath.Join(t.TempDir(), "data.db"), Options{Now: c.now, NoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.RegisterApp(fixture(t, "crm-app.json")); err != nil {
		t.Fatal(err)
	}
	return s, c
}

var person = Scope{}

func mustCreate(t *testing.T, s *Store, sc Scope, typ string, sets ...map[string]any) []Row {
	t.Helper()
	res, err := s.Create(sc, typ, sets)
	if err != nil {
		t.Fatalf("create %s: %v", typ, err)
	}
	return res.Rows
}

func wantCode(t *testing.T, err error, code string) *Error {
	t.Helper()
	e, ok := AsError(err)
	if !ok || e.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
	return e
}

func names(rows []Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i], _ = r["name"].(string)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// seedDeals makes a company and the five deals of design A2's render.
func seedDeals(t *testing.T, s *Store) (company string, deals map[string]string) {
	t.Helper()
	co := mustCreate(t, s, person, "company", map[string]any{"name": "Northwind", "domain": "northwind.example"})[0].ID()
	globex := mustCreate(t, s, person, "company", map[string]any{"name": "Globex"})[0].ID()
	deals = map[string]string{}
	for _, d := range []map[string]any{
		{"name": "Northwind · renewal", "stage": "Proposal", "amount": 48000, "company": co, "tags": []any{"renewal"}},
		{"name": "Globex · platform", "stage": "Negotiation", "amount": 36500, "company": globex, "urgent": true},
		{"name": "Initech · pilot", "stage": "Qualified", "amount": 12000},
		{"name": "Umbrella · expansion", "stage": "Proposal", "amount": 9800, "owner": "raj"},
		{"name": "Hooli · first order", "amount": 4200},
	} {
		row := mustCreate(t, s, person, "deal", d)[0]
		deals[d["name"].(string)] = row.ID()
	}
	return co, deals
}
