package data

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// THE MASK SENT BACK IS NOT A VALUE: a form saving the row it was shown
// keeps the real phone number.
func TestMaskSentBackKeepsTheValue(t *testing.T) {
	s, _ := openCRM(t)
	row := mustCreate(t, s, person, "contact", map[string]any{"name": "Ana", "phone": "+1 555 0100"})[0]
	if _, err := s.Update(Scope{App: "crm"}, "contact", []string{row.ID()}, map[string]any{"name": "Ana R", "phone": row["phone"]}, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(person, "contact", row.ID(), true)
	if got["phone"] != "+1 555 0100" || got["name"] != "Ana R" {
		t.Fatalf("after save: %v", got)
	}
}

// A COMPUTED FIELD MAY NOT UNMASK A MASKED ONE.
func TestComputedCannotReadAMaskedField(t *testing.T) {
	s, _ := openCRM(t)
	leak := []byte(`{"id":"leaky","records":{"contact":{"uses":true,"fields":{"leak":{"type":"text","computed":"phone"}}}},"capabilities":{"records":{"contact":"r"}}}`)
	if err := s.RegisterApp(leak); err == nil || !strings.Contains(err.Error(), "masked") {
		t.Fatalf("a computed copy of a masked field registered: %v", err)
	}
	ok := []byte(`{"id":"leaky","records":{"contact":{"uses":true,"fields":{"leak":{"type":"text","masked":true,"computed":"phone"}}}},"capabilities":{"records":{"contact":"r"}}}`)
	if err := s.RegisterApp(ok); err != nil {
		t.Fatal(err)
	}
}

// UNDO DOES NOT WRITE THROUGH A FREEZE IT DID NOT MAKE, and may undo the
// batch that made it.
func TestUndoAndFreeze(t *testing.T) {
	s, _ := openCRM(t)
	id := mustCreate(t, s, person, "deal", map[string]any{"name": "N", "amount": 10})[0].ID()
	won, err := s.Update(person, "deal", []string{id}, map[string]any{"stage": "Won"}, UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// The batch that froze it can be undone: Mark won, then Undo.
	if _, err := s.Undo(person, won.Batch); err != nil {
		t.Fatal(err)
	}
	// A record frozen at birth cannot be un-created by an undo.
	made, _ := s.Create(person, "deal", []map[string]any{{"name": "closed", "stage": "Lost"}})
	_, err = s.Undo(person, made.Batch)
	wantCode(t, err, CodeFrozen)
	// A change made through a freeze cannot be undone through it.
	touched, err := s.Update(person, "deal", []string{made.Rows[0].ID()}, map[string]any{"amount": 5}, UpdateOptions{Unfreeze: "fix"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Undo(Scope{App: "crm", Agent: true}, touched.Batch)
	wantCode(t, err, CodeFrozen)
}

// A CHANGE OF MORE RECORDS THAN SQLITE HAS PARAMETERS STILL REACHES A LIVE
// QUERY: an import of 40,000 rows, and its undo.
func TestLargeChangesReachSubscriptions(t *testing.T) {
	if testing.Short() {
		t.Skip("writes 40,000 records")
	}
	s, _ := openCRM(t)
	var b strings.Builder
	b.WriteString("name\n")
	for i := 0; i < 40_000; i++ {
		fmt.Fprintf(&b, "c%05d\n", i)
	}
	path := filepath.Join(t.TempDir(), "big.csv")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := newRecorder()
	if _, err := s.Subscribe(person, SubSpec{ID: "all", Type: "contact"}, rec.take); err != nil {
		t.Fatal(err)
	}
	res, err := s.ImportCSV(person, "contact", ImportOptions{Path: path})
	if err != nil || res.Created != 40_000 {
		t.Fatalf("import: %+v %v", res.Created, err)
	}
	if d := rec.next(t); len(d.Upserts) != 40_000 || d.Total != 40_000 {
		t.Fatalf("import diff: %d upserts, total %d", len(d.Upserts), d.Total)
	}
	if _, err := s.Undo(person, res.Batch); err != nil {
		t.Fatal(err)
	}
	if d := rec.next(t); len(d.Removes) != 40_000 || d.Total != 0 {
		t.Fatalf("undo diff: %d removes, total %d", len(d.Removes), d.Total)
	}
}

// A REF QUERY NEVER MATCHES BY THE NAME OF A RECORD THE APP CANNOT READ.
func TestRefTitlesNeedTheTarget(t *testing.T) {
	s, _ := openCRM(t)
	app := []byte(`{"id":"deals-only","records":{"deal":{"uses":true}},"capabilities":{"records":{"deal":"r"}}}`)
	if err := s.RegisterApp(app); err != nil {
		t.Fatal(err)
	}
	seedDeals(t, s)
	page, err := s.Query(Scope{App: "deals-only"}, "deal", "company:Northwind", "", 0)
	if err != nil || page.Total != 0 {
		t.Fatalf("matched by a name it cannot read: %v %v", page, err)
	}
	page, _ = s.Query(Scope{App: "crm"}, "deal", "company:Northwind", "", 0)
	if page.Total != 1 {
		t.Fatal("crm lost its name match")
	}
}
