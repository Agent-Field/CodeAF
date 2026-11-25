package data

import (
	"path/filepath"
	"testing"
	"time"
)

// A CREATE FILLS DEFAULTS, COMPUTES, AND CHECKS: $me and the enum default
// land, weighted is amount × the stage's weight, and a missing required
// field, a bad value and a dangling ref are each refused naming the field.
func TestCreateFillsDefaultsAndChecksValues(t *testing.T) {
	s, _ := openCRM(t)
	co := mustCreate(t, s, person, "company", map[string]any{"name": "Northwind"})[0].ID()
	row := mustCreate(t, s, person, "deal", map[string]any{"name": "N", "amount": "48,000.004", "company": co})[0]
	if row["owner"] != Me || row["stage"] != "Lead" {
		t.Fatalf("defaults: %v", row)
	}
	if row["amount"] != 48000.0 || row["weighted"] != 4800.0 {
		t.Fatalf("amount %v weighted %v", row["amount"], row["weighted"])
	}
	if row["age_days"] != 0.0 {
		t.Fatalf("age_days %v", row["age_days"])
	}
	for _, tc := range []struct {
		set   map[string]any
		field string
	}{
		{map[string]any{"amount": 5}, "name"},
		{map[string]any{"name": "x", "stage": "Maybe"}, "stage"},
		{map[string]any{"name": "x", "amount": "lots"}, "amount"},
		{map[string]any{"name": "x", "company": "01JD0000000000000000000000"}, "company"},
		{map[string]any{"name": "x", "weighted": 3}, "weighted"},
		{map[string]any{"name": "x", "nope": 3}, "nope"},
		{map[string]any{"name": "x", "close_on": "next tuesday"}, "close_on"},
	} {
		_, err := s.Create(person, "deal", []map[string]any{tc.set})
		if e := wantCode(t, err, CodeInvalid); e.Field != tc.field {
			t.Fatalf("%v: refused for %q, want %q (%v)", tc.set, e.Field, tc.field, e)
		}
	}
	// A refused record in a batch refuses the batch: nothing half-made.
	_, err := s.Create(person, "deal", []map[string]any{{"name": "ok"}, {"amount": 1}})
	wantCode(t, err, CodeInvalid)
	page, _ := s.Query(person, "deal", "ok", "", 0)
	if page.Total != 0 {
		t.Fatal("a refused batch left a record behind")
	}
}

// A UNIQUE VALUE IS HELD BY ONE LIVE RECORD; a deleted one frees it.
func TestUniqueValues(t *testing.T) {
	s, _ := openCRM(t)
	id := mustCreate(t, s, person, "company", map[string]any{"name": "Acme"})[0].ID()
	_, err := s.Create(person, "company", []map[string]any{{"name": "Acme"}})
	wantCode(t, err, CodeDuplicate)
	_, err = s.Create(person, "company", []map[string]any{{"name": "B"}, {"name": "B"}})
	wantCode(t, err, CodeDuplicate)
	if _, err := s.Delete(person, "company", []string{id}, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, s, person, "company", map[string]any{"name": "Acme"})
}

// AN UPDATE THAT CHANGES NOTHING WRITES NO HISTORY AND NO BATCH, and one that
// changes something recomputes what depends on it.
func TestUpdateRecomputesAndSkipsNoOps(t *testing.T) {
	s, _ := openCRM(t)
	id := mustCreate(t, s, person, "deal", map[string]any{"name": "N", "amount": 1000})[0].ID()
	res, err := s.Update(person, "deal", []string{id}, map[string]any{"stage": "Proposal"}, UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Batch == "" || res.Rows[0]["weighted"] != 500.0 {
		t.Fatalf("update: %+v", res)
	}
	again, err := s.Update(person, "deal", []string{id}, map[string]any{"stage": "proposal"}, UpdateOptions{})
	if err != nil || again.Batch != "" {
		t.Fatalf("a no-op update made batch %q (%v)", again.Batch, err)
	}
	hist, _ := s.History(person, "deal", id)
	if len(hist) != 2 || hist[0].Op != OpUpdate || !equal(hist[0].Changed, []string{"stage", "weighted"}) {
		t.Fatalf("history: %+v", hist)
	}
	// Clearing a required field is refused; clearing an optional one works.
	_, err = s.Update(person, "deal", []string{id}, map[string]any{"name": nil}, UpdateOptions{})
	wantCode(t, err, CodeInvalid)
	res, err = s.Update(person, "deal", []string{id}, map[string]any{"amount": nil}, UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, has := res.Rows[0]["amount"]; has {
		t.Fatal("amount was not cleared")
	}
	if _, has := res.Rows[0]["weighted"]; has {
		t.Fatal("weighted kept a value with no amount: the emptiness law says nothing")
	}
}

// UNDO REVERTS A BATCH ONCE, and refuses when a later change touched the same
// records, rather than throwing that change away.
func TestUndo(t *testing.T) {
	s, _ := openCRM(t)
	_, deals := seedDeals(t, s)
	a, b := deals["Initech · pilot"], deals["Hooli · first order"]

	moved, err := s.Update(person, "deal", []string{a, b}, map[string]any{"stage": "Won"}, UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	undo, err := s.Undo(person, moved.Batch)
	if err != nil {
		t.Fatal(err)
	}
	row, _ := s.Get(person, "deal", a, false)
	if row["stage"] != "Qualified" || row["weighted"] != 3000.0 {
		t.Fatalf("undo left %v", row)
	}
	_, err = s.Undo(person, moved.Batch)
	wantCode(t, err, CodeStale)
	// The undo is a batch too: undoing it is a redo.
	if _, err := s.Undo(person, undo.Batch); err != nil {
		t.Fatal(err)
	}
	row, _ = s.Get(person, "deal", b, false)
	if row["stage"] != "Won" {
		t.Fatalf("redo left %v", row["stage"])
	}

	// A later change to one of the records makes the undo stale, whole.
	first, _ := s.Update(person, "deal", []string{deals["Globex · platform"], deals["Umbrella · expansion"]}, map[string]any{"amount": 1}, UpdateOptions{})
	if _, err := s.Update(person, "deal", []string{deals["Umbrella · expansion"]}, map[string]any{"amount": 2}, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err = s.Undo(person, first.Batch)
	wantCode(t, err, CodeStale)
	row, _ = s.Get(person, "deal", deals["Globex · platform"], false)
	if row["amount"] != 1.0 {
		t.Fatal("a refused undo changed a record")
	}

	// Undoing a delete brings the record back; undoing a create removes it.
	del, err := s.Delete(person, "deal", []string{deals["Globex · platform"]}, UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(person, "deal", deals["Globex · platform"], false); err == nil {
		t.Fatal("a deleted record was found")
	}
	if _, err := s.Undo(person, del.Batch); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(person, "deal", deals["Globex · platform"], false); err != nil {
		t.Fatal(err)
	}
	made, _ := s.Create(person, "deal", []map[string]any{{"name": "brief"}})
	gone, err := s.Undo(person, made.Batch)
	if err != nil || !equal(gone.Removes, []string{made.Rows[0].ID()}) {
		t.Fatalf("undo of a create: %+v %v", gone, err)
	}
	if _, err := s.Get(person, "deal", made.Rows[0].ID(), false); err == nil {
		t.Fatal("an undone create is still there")
	}
}

// UNDOING A DELETE CANNOT BRING BACK A DUPLICATE of a unique value someone
// has reused since.
func TestUndoKeepsUniqueValuesUnique(t *testing.T) {
	s, _ := openCRM(t)
	id := mustCreate(t, s, person, "company", map[string]any{"name": "Acme"})[0].ID()
	del, _ := s.Delete(person, "company", []string{id}, UpdateOptions{})
	mustCreate(t, s, person, "company", map[string]any{"name": "Acme"})
	_, err := s.Undo(person, del.Batch)
	wantCode(t, err, CodeDuplicate)
}

// A FROZEN RECORD IS READ-ONLY for everyone except a person who gives a
// reason; the except fields stay writable, and an agent never unfreezes.
func TestFreezeRules(t *testing.T) {
	s, c := openCRM(t)
	id := mustCreate(t, s, person, "deal", map[string]any{"name": "N", "stage": "Won", "amount": 10})[0].ID()
	row, _ := s.Get(person, "deal", id, false)
	meta := row["_meta"].(RowMeta)
	if !meta.Frozen || !equal(meta.Writable, []string{"last_touch"}) {
		t.Fatalf("meta %+v", meta)
	}
	_, err := s.Update(person, "deal", []string{id}, map[string]any{"amount": 20}, UpdateOptions{})
	if e := wantCode(t, err, CodeFrozen); e.Field != "amount" {
		t.Fatalf("frozen on %q", e.Field)
	}
	if _, err := s.Update(person, "deal", []string{id}, map[string]any{"last_touch": "$now"}, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err = s.Delete(person, "deal", []string{id}, UpdateOptions{})
	wantCode(t, err, CodeFrozen)
	agent := Scope{App: "crm", Agent: true}
	_, err = s.Update(agent, "deal", []string{id}, map[string]any{"amount": 20}, UpdateOptions{Unfreeze: "because"})
	wantCode(t, err, CodeFrozen)
	if _, err := s.Update(person, "deal", []string{id}, map[string]any{"stage": "Negotiation"}, UpdateOptions{Unfreeze: "reopened by the customer"}); err != nil {
		t.Fatal(err)
	}
	hist, _ := s.History(person, "deal", id)
	if hist[0].Note != "unfrozen: reopened by the customer" {
		t.Fatalf("note %q", hist[0].Note)
	}

	// The period rule: a deal closing in Q3 2026 locks five days after the
	// quarter ends, and not before.
	q3 := mustCreate(t, s, person, "deal", map[string]any{"name": "Q3", "close_on": "2026-09-15"})[0].ID()
	c.mu.Lock()
	c.at = time.Date(2026, 10, 3, 12, 0, 0, 0, time.Local)
	c.mu.Unlock()
	if _, err := s.Update(person, "deal", []string{q3}, map[string]any{"amount": 1}, UpdateOptions{}); err != nil {
		t.Fatalf("frozen before the grace ended: %v", err)
	}
	c.advance(5 * 24 * time.Hour)
	_, err = s.Update(person, "deal", []string{q3}, map[string]any{"amount": 2}, UpdateOptions{})
	wantCode(t, err, CodeFrozen)
}

// AN AI VALUE NEVER OVERWRITES A PERSON'S: a person's write pins the value,
// and ai.sort's writes fill only what is not pinned.
func TestAIFieldsKeepPinnedValues(t *testing.T) {
	s, _ := openCRM(t)
	_, deals := seedDeals(t, s)
	a, b := deals["Initech · pilot"], deals["Hooli · first order"]
	ai := Scope{App: "crm", AI: true}
	if _, err := s.Update(person, "deal", []string{a}, map[string]any{"likely": "Unlikely"}, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(ai, "deal", []string{a, b}, map[string]any{"likely": "Likely"}, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	ra, _ := s.Get(person, "deal", a, false)
	rb, _ := s.Get(person, "deal", b, false)
	if ra["likely"] != "Unlikely" || ra["_meta"].(RowMeta).AI["likely"] != SourcePinned {
		t.Fatalf("pinned value lost: %v", ra)
	}
	if rb["likely"] != "Likely" || rb["_meta"].(RowMeta).AI["likely"] != SourceAI {
		t.Fatalf("ai value: %v", rb)
	}
}

// MASKED VALUES READ AS •••• unless a person reveals them; every reveal is
// written down; an agent cannot reveal, and an app cannot search one.
func TestMaskedValues(t *testing.T) {
	s, _ := openCRM(t)
	id := mustCreate(t, s, person, "contact", map[string]any{"name": "Ana", "phone": "+1 555 0100"})[0]
	if id["phone"] != masked {
		t.Fatalf("created row shows %v", id["phone"])
	}
	row, err := s.Get(person, "contact", id.ID(), true)
	if err != nil || row["phone"] != "+1 555 0100" {
		t.Fatalf("reveal: %v %v", row, err)
	}
	var reveals int
	_ = s.rdb.QueryRow(`SELECT count(*) FROM reveals WHERE id = ? AND field = 'phone'`, id.ID()).Scan(&reveals)
	if reveals != 1 {
		t.Fatalf("%d reveal lines", reveals)
	}
	_, err = s.Get(Scope{App: "crm", Agent: true}, "contact", id.ID(), true)
	wantCode(t, err, CodeNotAllowed)
	_, err = s.Query(Scope{App: "crm"}, "contact", "phone:+1*", "", 0)
	wantCode(t, err, CodeInvalid)
	hist, _ := s.History(person, "contact", id.ID())
	if hist[0].After["phone"] != masked {
		t.Fatal("history shows a masked value")
	}
	// A free word never searches a masked field.
	page, _ := s.Query(person, "contact", "555", "", 0)
	if page.Total != 0 {
		t.Fatal("a free word found a masked value")
	}
}

// CAPABILITIES ARE CHECKED ON EVERY CALL: an app reads only the types it
// declared, writes only those it declared rw, and is told what it would need.
func TestCapabilities(t *testing.T) {
	s, _ := openCRM(t)
	if err := s.RegisterApp(fixture(t, "hiring-app.json")); err != nil {
		t.Fatal(err)
	}
	hiring := Scope{App: "hiring"}
	_, err := s.Create(hiring, "contact", []map[string]any{{"name": "x"}})
	if e := wantCode(t, err, CodeNotAllowed); e.Need != "records.contact.rw" {
		t.Fatalf("need %q", e.Need)
	}
	_, err = s.Query(hiring, "deal", "", "", 0)
	if e := wantCode(t, err, CodeNotAllowed); e.Need != "records.deal.r" {
		t.Fatalf("need %q", e.Need)
	}
	_, err = s.Query(Scope{App: "ghost"}, "deal", "", "", 0)
	wantCode(t, err, CodeNotAllowed)
	if _, err := s.Query(hiring, "contact", "", "", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(person, "contact", []map[string]any{{"name": "x", "role_applied": "Engineer"}}); err != nil {
		t.Fatal(err)
	}
	types, _ := s.Types(hiring)
	if len(types) != 2 {
		t.Fatalf("hiring sees %d types", len(types))
	}
}

// DATA BELONGS TO THE PERSON: a used type gains the using app's fields, a
// field an app stops declaring is hidden with its values kept, and forgetting
// an app keeps every record.
func TestTypesMergeAndHideNeverDelete(t *testing.T) {
	s, _ := openCRM(t)
	if err := s.RegisterApp(fixture(t, "hiring-app.json")); err != nil {
		t.Fatal(err)
	}
	id := mustCreate(t, s, person, "contact", map[string]any{"name": "Ana", "role_applied": "Engineer"})[0].ID()
	types, _ := s.Types(person)
	var contact TypeInfo
	for _, ti := range types {
		if ti.Name == "contact" {
			contact = ti
		}
	}
	if contact.OwnerApp != "crm" || !equal(contact.UsedBy, []string{"hiring"}) || contact.Count != 1 {
		t.Fatalf("contact: %+v", contact)
	}
	last := contact.Fields[len(contact.Fields)-1]
	if last.Name != "role_applied" || last.App != "hiring" {
		t.Fatalf("fields: %+v", contact.Fields)
	}
	// Hiring drops role_applied: hidden, its value kept.
	v2 := []byte(`{"id":"hiring","records":{"candidate":{"title":"name","fields":{"name":{"type":"text"}}},"contact":{"uses":true}},"capabilities":{"records":{"candidate":"rw","contact":"r"}}}`)
	if err := s.RegisterApp(v2); err != nil {
		t.Fatal(err)
	}
	row, _ := s.Get(Scope{App: "hiring"}, "contact", id, false)
	if _, has := row["role_applied"]; has {
		t.Fatal("an app sees a hidden field")
	}
	row, _ = s.Get(person, "contact", id, false)
	if row["role_applied"] != "Engineer" {
		t.Fatal("hiding a field lost its value")
	}
	_, err := s.Update(person, "contact", []string{id}, map[string]any{"role_applied": "x"}, UpdateOptions{})
	wantCode(t, err, CodeInvalid)

	// Another app may not own a type that is owned; it may use it.
	clash := []byte(`{"id":"other","records":{"contact":{"fields":{"name":{"type":"text"}}}}}`)
	wantCode(t, s.RegisterApp(clash), CodeInvalid)
	retype := []byte(`{"id":"other","records":{"contact":{"uses":true,"fields":{"email":{"type":"text"}}}}}`)
	wantCode(t, s.RegisterApp(retype), CodeInvalid)
	missing := []byte(`{"id":"other","records":{"invoice":{"uses":true}}}`)
	wantCode(t, s.RegisterApp(missing), CodeInvalid)

	// Forgetting the CRM keeps its records and lets the person read them.
	if err := s.ForgetApp("crm"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(person, "contact", id, false); err != nil {
		t.Fatal(err)
	}
	_, err = s.Query(Scope{App: "crm"}, "contact", "", "", 0)
	wantCode(t, err, CodeNotAllowed)
}

// A CLOCKED COMPUTED FIELD IS COMPUTED AGAIN BY Refresh, which writes no
// history and tells subscribers.
func TestRefreshRecomputesClockedFields(t *testing.T) {
	s, c := openCRM(t)
	id := mustCreate(t, s, person, "deal", map[string]any{"name": "N"})[0].ID()
	diffs := make(chan Diff, 4)
	if err := s.Subscribe(person, SubSpec{ID: "one", Type: "deal", Record: id}, func(d Diff) { diffs <- d }); err != nil {
		t.Fatal(err)
	}
	c.advance(3 * 24 * time.Hour)
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	row, _ := s.Get(person, "deal", id, false)
	if row["age_days"] != 3.0 {
		t.Fatalf("age_days %v", row["age_days"])
	}
	hist, _ := s.History(person, "deal", id)
	if len(hist) != 1 {
		t.Fatalf("refresh wrote history: %+v", hist)
	}
	// The create's own change may reach the subscription first (it was
	// registered just after the create committed); the refresh's follows.
	for {
		select {
		case d := <-diffs:
			if len(d.Upserts) == 1 && d.Upserts[0]["age_days"] == 3.0 {
				return
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no diff for a refresh")
		}
	}
}

// THE FILE SURVIVES A RESTART: types, records and history come back, and
// the migrations do not run twice.
func TestReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.db")
	s, err := Open(path, Options{NoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterApp(fixture(t, "crm-app.json")); err != nil {
		t.Fatal(err)
	}
	id := mustCreate(t, s, person, "deal", map[string]any{"name": "N", "amount": 10})[0].ID()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path, Options{NoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	row, err := s.Get(Scope{App: "crm"}, "deal", id, false)
	if err != nil || row["weighted"] != 1.0 {
		t.Fatalf("after reopen: %v %v", row, err)
	}
	var version int
	_ = s.rdb.QueryRow(`PRAGMA user_version`).Scan(&version)
	if version != len(migrations) {
		t.Fatalf("version %d", version)
	}
	_, err = s.Create(person, "deal", []map[string]any{{"name": "after"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = s.Create(person, "deal", []map[string]any{{"name": "closed"}})
	wantCode(t, err, CodeClosed)
}
