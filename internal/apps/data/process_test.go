package data

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// opsApp is an app that does work: a case with a lifecycle whose approval
// needs a sign-off and no open work, numbered invoices, and a ledger.
const opsApp = `{
  "id": "ops", "name": "Ops",
  "records": {
    "case": {
      "title": "name",
      "fields": {
        "name": {"type": "text", "required": true},
        "stage": {"type": "enum", "values": ["Open", "Review", "Approved", "Closed"]},
        "amount": {"type": "money"},
        "notes": {"type": "text"}
      },
      "lifecycle": {
        "field": "stage",
        "transitions": [
          {"id": "submit", "label": "Submit for review", "from": ["Open"], "to": "Review", "guard": "amount > 0", "effects": [{"work_item": {"title": "Review the case"}}]},
          {"id": "approve", "label": "Approve", "from": ["Review"], "to": "Approved", "guard": "signed() && open_work() == 0", "by": ["person"]},
          {"id": "close", "from": ["Approved"], "to": "Closed", "by": ["person", "flow", "ai"]},
          {"id": "reopen", "from": ["*"], "to": "Open"}
        ]
      }
    },
    "invoice": {
      "title": "number",
      "fields": {
        "number": {"type": "text", "sequence": {"prefix": "INV-", "pad": 4}},
        "customer": {"type": "text"},
        "total": {"type": "money"}
      }
    },
    "entry": {
      "title": "name",
      "ledger": true,
      "fields": {
        "name": {"type": "text", "required": true},
        "amount": {"type": "money"},
        "qty": {"type": "number"}
      }
    }
  },
  "capabilities": {"records": {"case": "rw", "invoice": "rw", "entry": "rw"}}
}`

func openOps(t *testing.T) (*Store, *clock) {
	t.Helper()
	s, c := openCRM(t)
	if err := s.RegisterApp([]byte(opsApp)); err != nil {
		t.Fatal(err)
	}
	return s, c
}

// EVERYTHING IS BY ID: renaming a field or a type moves no value, keeps the
// old name readable, and the stored row never spells a name.
func TestRenamesKeepEveryValue(t *testing.T) {
	s, _ := openCRM(t)
	_, deals := seedDeals(t, s)
	id := deals["Northwind · renewal"]
	if err := s.RenameField("deal", "amount", "value"); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameType("deal", "opportunity"); err != nil {
		t.Fatal(err)
	}
	row, err := s.Get(person, "opportunity", id, false)
	if err != nil || row["value"] != 48000.0 || row["amount"] != nil {
		t.Fatalf("after rename: %v %v", row, err)
	}
	// The old names still read: the app's capability, a query, a write.
	page, err := s.Query(Scope{App: "crm"}, "deal", "amount>40000", "", 0)
	if err != nil || page.Total != 1 {
		t.Fatalf("old names in a query: %+v %v", page, err)
	}
	if _, err := s.Update(person, "deal", []string{id}, map[string]any{"amount": 50000}, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	var raw string
	_ = s.rdb.QueryRow(`SELECT data_json FROM records WHERE id = ?`, id).Scan(&raw)
	if strings.Contains(raw, "amount") || strings.Contains(raw, "value") || !strings.Contains(raw, "50000") {
		t.Fatalf("stored row spells names: %s", raw)
	}
	// Re-registering the app, which still says deal and amount, changes
	// nothing and makes no second field.
	if err := s.RegisterApp(fixture(t, "crm-app.json")); err != nil {
		t.Fatal(err)
	}
	ty, _ := s.Type(person, "opportunity")
	if ty.Fields["amount"] != nil || ty.Fields["value"] == nil {
		t.Fatalf("re-register made a second field: %v", ty.Order)
	}
}

// THE PERSON IS AN ID: "me" is stored as it, and a draft's sample has the
// same one.
func TestThePersonIsOneID(t *testing.T) {
	home := t.TempDir()
	p := NewPlace(home, Options{NoRefresh: true})
	defer p.Close()
	main, err := p.Main()
	if err != nil {
		t.Fatal(err)
	}
	if !validID(main.Person()) {
		t.Fatalf("person %q", main.Person())
	}
	if err := p.SeedSample("d1", fixture(t, "crm-app.json"), map[string][]map[string]any{"deal": {{"name": "x", "owner": "me"}}}); err != nil {
		t.Fatal(err)
	}
	draft, _ := p.Store("d1")
	if draft.Person() != main.Person() {
		t.Fatalf("draft person %s, main %s", draft.Person(), main.Person())
	}
	page, _ := draft.Query(person, "deal", "owner:me", "", 0)
	if page.Total != 1 || page.Rows[0]["owner"] != main.Person() {
		t.Fatalf("owner:me in the draft: %+v", page)
	}
}

// PERSON WINS BY PROVENANCE: an AI write leaves alone a value the person
// set, or cleared, until the person hands it back; every row says who wrote.
func TestPersonWinsByProvenance(t *testing.T) {
	s, _ := openCRM(t)
	id := mustCreate(t, s, person, "deal", map[string]any{"name": "N", "likely": "Maybe", "close_on": "2027-01-10"})[0].ID()
	ai := Scope{App: "crm", AI: true, Cause: "run:sort#3"}
	res, err := s.Update(ai, "deal", []string{id}, map[string]any{"likely": "Likely", "urgent": true}, UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Held) != 1 || res.Held[0].Field != "likely" || res.Rows[0]["likely"] != "Maybe" || res.Rows[0]["urgent"] != true {
		t.Fatalf("ai over a person's value: %+v", res)
	}
	meta := res.Rows[0]["_meta"].(RowMeta)
	if meta.By["likely"] != ByPerson || meta.By["urgent"] != ByAI || meta.AI["likely"] != SourcePinned {
		t.Fatalf("meta: %+v", meta)
	}
	// A person's clear holds too.
	if _, err := s.Update(person, "deal", []string{id}, map[string]any{"close_on": nil}, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	res, _ = s.Update(ai, "deal", []string{id}, map[string]any{"close_on": "2027-02-01"}, UpdateOptions{})
	if len(res.Held) != 1 || res.Rows[0]["close_on"] != nil {
		t.Fatalf("ai over a person's clear: %+v", res)
	}
	// An agent cannot hand values back; the person can, and then AI writes.
	_, err = s.Update(Scope{App: "crm", Agent: true}, "deal", []string{id}, nil, UpdateOptions{Reset: []string{"likely"}})
	wantCode(t, err, CodeNotAllowed)
	if _, err := s.Update(person, "deal", []string{id}, nil, UpdateOptions{Reset: []string{"likely"}}); err != nil {
		t.Fatal(err)
	}
	res, _ = s.Update(ai, "deal", []string{id}, map[string]any{"likely": "Likely"}, UpdateOptions{})
	if len(res.Held) != 0 || res.Rows[0]["likely"] != "Likely" {
		t.Fatalf("after reset: %+v", res)
	}
	hist, _ := s.History(person, "deal", id)
	if hist[0].ByKind != ByAI || hist[0].Cause != "run:sort#3" || hist[len(hist)-1].ByKind != ByPerson {
		t.Fatalf("history kinds: %+v", hist[0])
	}
}

// A REVISION REFUSES A STALE EDIT.
func TestStaleRevisionIsRefused(t *testing.T) {
	s, _ := openCRM(t)
	row := mustCreate(t, s, person, "deal", map[string]any{"name": "N"})[0]
	rev := row["_rev"].(int64)
	if _, err := s.Update(person, "deal", []string{row.ID()}, map[string]any{"amount": 1}, UpdateOptions{Rev: rev}); err != nil {
		t.Fatal(err)
	}
	_, err := s.Update(person, "deal", []string{row.ID()}, map[string]any{"amount": 2}, UpdateOptions{Rev: rev})
	wantCode(t, err, CodeStale)
}

// THE OUTBOX IS THE COMMIT: a write's events are there exactly when the
// write is, a dry run leaves none, and a claim is taken once.
func TestOutboxIsWrittenWithTheChange(t *testing.T) {
	s, _ := openCRM(t)
	start, _ := s.Cursor()
	id := mustCreate(t, s, person, "deal", map[string]any{"name": "N"})[0].ID()
	_, err := s.Create(person, "deal", []map[string]any{{"name": "ok"}, {"amount": 5}})
	if err == nil {
		t.Fatal("a create missing a required field was taken")
	}
	path := filepath.Join(t.TempDir(), "c.csv")
	_ = os.WriteFile(path, []byte("name\na\nb\n"), 0o600)
	if _, err := s.ImportCSV(person, "deal", ImportOptions{Path: path, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	events, err := s.Outbox(start, 0)
	if err != nil || len(events) != 1 {
		t.Fatalf("events: %+v %v", events, err)
	}
	e := events[0]
	var payload struct {
		ID    string `json:"id"`
		After Row    `json:"after"`
	}
	_ = json.Unmarshal(e.Payload, &payload)
	if e.Event != EventRecordCreated || e.Type != "deal" || e.Record != id || e.ByKind != ByPerson || payload.After["name"] != "N" {
		t.Fatalf("event: %+v %s", e, e.Payload)
	}
	first, err := s.Claim(e.Seq, "trigger.follow_up")
	second, _ := s.Claim(e.Seq, "trigger.follow_up")
	other, _ := s.Claim(e.Seq, "trigger.other")
	if err != nil || !first || second || !other {
		t.Fatalf("claims: %v %v %v %v", first, second, other, err)
	}
}

// A LIFECYCLE OWNS ITS STATE: only a transition moves it, and a transition
// checks its state, its guard, its writer and its revision.
func TestLifecycleOwnsTheState(t *testing.T) {
	s, _ := openOps(t)
	row := mustCreate(t, s, person, "case", map[string]any{"name": "C1"})[0]
	id := row.ID()
	if row["stage"] != "Open" {
		t.Fatalf("starts in %v", row["stage"])
	}
	_, err := s.Create(person, "case", []map[string]any{{"name": "C2", "stage": "Approved"}})
	wantCode(t, err, CodeLifecycle)
	_, err = s.Update(person, "case", []string{id}, map[string]any{"stage": "Review"}, UpdateOptions{})
	if e := wantCode(t, err, CodeLifecycle); e.Field != "stage" || !strings.Contains(e.Message, "Submit for review") {
		t.Fatalf("refusal: %+v", e)
	}
	// The guard: amount > 0.
	_, err = s.Transition(person, "case", id, "submit", TransitionOptions{})
	wantCode(t, err, CodeLifecycle)
	up, _ := s.Update(person, "case", []string{id}, map[string]any{"amount": 120}, UpdateOptions{})
	rev := up.Rows[0]["_rev"].(int64)
	// From a state it does not start from.
	_, err = s.Transition(person, "case", id, "approve", TransitionOptions{})
	wantCode(t, err, CodeLifecycle)
	// A stale revision.
	_, err = s.Transition(person, "case", id, "submit", TransitionOptions{Rev: rev - 1})
	wantCode(t, err, CodeStale)
	res, err := s.Transition(person, "case", id, "submit", TransitionOptions{Rev: rev, Reason: "ready"})
	if err != nil || res.Rows[0]["stage"] != "Review" {
		t.Fatalf("submit: %+v %v", res, err)
	}
	// An agent may not approve; a flow may close but approve is person-only.
	_, err = s.Transition(Scope{App: "ops", Agent: true}, "case", id, "approve", TransitionOptions{})
	if e := wantCode(t, err, CodeNotAllowed); e.Need != "transition.approve.ai" {
		t.Fatalf("need: %q", e.Need)
	}
	hist, _ := s.History(person, "case", id)
	if hist[0].Op != OpTransition || hist[0].Note != "submit: ready" || hist[0].Cause != "transition:submit" || hist[0].Actor != s.Person() {
		t.Fatalf("history: %+v", hist[0])
	}
	// The effects reach the outbox with the transition.
	events, _ := s.Outbox(0, 0)
	last := events[len(events)-1]
	if last.Event != EventTransition || !strings.Contains(string(last.Payload), `"work_item"`) || !strings.Contains(string(last.Payload), `"to":"Review"`) {
		t.Fatalf("transition event: %+v %s", last, last.Payload)
	}
}

// A GUARD READS SIGN-OFFS AND OPEN WORK: approve needs a sign-off on this
// very version and no work left open.
func TestGuardsReadSignoffsAndWork(t *testing.T) {
	s, _ := openOps(t)
	id := mustCreate(t, s, person, "case", map[string]any{"name": "C1", "amount": 10})[0].ID()
	if _, err := s.Transition(person, "case", id, "submit", TransitionOptions{}); err != nil {
		t.Fatal(err)
	}
	work, err := s.CreateWorkItem(person, WorkItem{Title: "Check the figures", Type: "case", Record: id, Due: "+2d"})
	if err != nil || work.Assignee != s.Person() || work.Status != WorkOpen {
		t.Fatalf("work: %+v %v", work, err)
	}
	if _, err := s.Sign(Scope{App: "ops", Agent: true}, "case", id, "Looks right", 0); err == nil {
		t.Fatal("an agent signed")
	}
	if _, err := s.Sign(person, "case", id, "I approve this case as it stands", 0); err != nil {
		t.Fatal(err)
	}
	states, _ := s.Transitions(person, "case", id)
	if len(states) != 2 || states[0].ID != "approve" || states[0].Ready {
		t.Fatalf("with open work: %+v", states)
	}
	done := WorkDone
	if _, err := s.UpdateWorkItem(person, work.ID, WorkPatch{Status: &done, Outcome: map[string]any{"ok": true}}); err != nil {
		t.Fatal(err)
	}
	// A change to the record makes a version the sign-off does not cover.
	if _, err := s.Update(person, "case", []string{id}, map[string]any{"notes": "changed"}, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err = s.Transition(person, "case", id, "approve", TransitionOptions{})
	wantCode(t, err, CodeLifecycle)
	signs, _ := s.Signoffs(person, "case", id)
	if len(signs) != 1 || signs[0].Current {
		t.Fatalf("signoffs: %+v", signs)
	}
	if _, err := s.Sign(person, "case", id, "Approved again", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Transition(person, "case", id, "approve", TransitionOptions{}); err != nil {
		t.Fatal(err)
	}
	items, _ := s.WorkItems(person, WorkFilter{Assignee: "me"})
	if len(items) != 1 || items[0].Status != WorkDone || fmt.Sprint(items[0].Outcome) != "map[ok:true]" {
		t.Fatalf("my work: %+v", items)
	}
}

// TIMERS FIRE INTO THE OUTBOX, catch up after a gap, and recur.
func TestTimersFireAndRecur(t *testing.T) {
	s, c := openOps(t)
	id := mustCreate(t, s, person, "case", map[string]any{"name": "C1"})[0].ID()
	once, err := s.CreateTimer(person, Timer{Type: "case", Record: id, FireAt: "+3d", Kind: "follow_up", Payload: map[string]any{"note": "call back"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTimer(person, Timer{Type: "case", Record: id, FireAt: "+1d", Kind: "daily", Every: "1d"}); err != nil {
		t.Fatal(err)
	}
	gone, _ := s.CreateTimer(person, Timer{Type: "case", Record: id, FireAt: "+1h", Kind: "cancelled"})
	if err := s.CancelTimer(person, gone.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.FireDue(); n != 0 {
		t.Fatalf("fired %d early", n)
	}
	start, _ := s.Cursor()
	c.advance(5 * 24 * time.Hour) // closed for five days
	n, err := s.FireDue()
	if err != nil || n != 2 {
		t.Fatalf("fired %d %v", n, err)
	}
	events, _ := s.Outbox(start, 0)
	fired := 0
	for _, e := range events {
		if e.Event == EventTimerFired {
			fired++
		}
	}
	if fired != 2 {
		t.Fatalf("timer events: %+v", events)
	}
	pending, _ := s.Timers(person, id, true)
	if len(pending) != 1 || pending[0].Kind != "daily" {
		t.Fatalf("pending after catch-up: %+v", pending)
	}
	next, _ := time.Parse(stamp, pending[0].FireAt)
	if !next.After(c.now()) || next.After(c.now().Add(24*time.Hour)) {
		t.Fatalf("the recurrence did not skip the missed days: %s", pending[0].FireAt)
	}
	all, _ := s.Timers(person, id, false)
	if len(all) != 4 || once.FireAt == "" {
		t.Fatalf("timers: %d", len(all))
	}
}

// A SEQUENCE NUMBERS RECORDS once each, around numbers an import brought.
func TestSequencesNumberRecords(t *testing.T) {
	s, _ := openOps(t)
	a := mustCreate(t, s, person, "invoice", map[string]any{"customer": "A"})[0]
	if a["number"] != "INV-0001" {
		t.Fatalf("first: %v", a["number"])
	}
	_, err := s.Create(person, "invoice", []map[string]any{{"number": "INV-9999"}})
	if e := wantCode(t, err, CodeInvalid); e.Field != "number" {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "inv.csv")
	_ = os.WriteFile(path, []byte("number,customer\nINV-0002,old\n"), 0o600)
	if res, err := s.ImportCSV(person, "invoice", ImportOptions{Path: path}); err != nil || res.Created != 1 {
		t.Fatalf("import: %+v %v", res, err)
	}
	b := mustCreate(t, s, person, "invoice", map[string]any{"customer": "B"})[0]
	if b["number"] != "INV-0003" {
		t.Fatalf("after an imported number: %v", b["number"])
	}
	// An undone number is never given again.
	made, _ := s.Create(person, "invoice", []map[string]any{{"customer": "C"}})
	if _, err := s.Undo(person, made.Batch); err != nil {
		t.Fatal(err)
	}
	d := mustCreate(t, s, person, "invoice", map[string]any{"customer": "D"})[0]
	if d["number"] != "INV-0005" {
		t.Fatalf("after an undo: %v", d["number"])
	}
}

// A LEDGER IS NEVER CHANGED, ONLY REVERSED.
func TestLedgerIsReversedNotEdited(t *testing.T) {
	s, _ := openOps(t)
	made, _ := s.Create(person, "entry", []map[string]any{{"name": "Stock in", "amount": 120.5, "qty": 3}})
	id := made.Rows[0].ID()
	_, err := s.Update(person, "entry", []string{id}, map[string]any{"qty": 4}, UpdateOptions{})
	wantCode(t, err, CodeLedger)
	_, err = s.Delete(person, "entry", []string{id}, UpdateOptions{})
	wantCode(t, err, CodeLedger)
	_, err = s.Undo(person, made.Batch)
	wantCode(t, err, CodeLedger)
	rev, err := s.Reverse(person, "entry", id, "booked twice")
	if err != nil {
		t.Fatal(err)
	}
	r := rev.Rows[0]
	if r["amount"] != -120.5 || r["qty"] != -3.0 || r["name"] != "Stock in" || r["_meta"].(RowMeta).Reverses != id {
		t.Fatalf("reversal: %v", r)
	}
	orig, _ := s.Get(person, "entry", id, false)
	if orig["_meta"].(RowMeta).ReversedBy != r.ID() || orig["amount"] != 120.5 {
		t.Fatalf("original: %v", orig)
	}
	_, err = s.Reverse(person, "entry", id, "again")
	wantCode(t, err, CodeStale)
	_, err = s.Reverse(person, "entry", r.ID(), "")
	wantCode(t, err, CodeLedger)
	sum, err := s.Aggregate(person, &QueryIR{Type: "entry", Aggregate: []Agg{{Fn: "sum", Field: "amount"}}})
	if err != nil || sum.Groups[0].Values["sum_amount"] != 0.0 {
		t.Fatalf("the books do not balance: %+v %v", sum, err)
	}
}

// UNIQUE MEANS UNIQUE: across case, and a field made unique over data that
// repeats is refused, not declared.
func TestUniqueIsEnforced(t *testing.T) {
	s, _ := openCRM(t)
	mustCreate(t, s, person, "company", map[string]any{"name": "Acme"})
	_, err := s.Create(person, "company", []map[string]any{{"name": "ACME"}})
	wantCode(t, err, CodeDuplicate)
	mustCreate(t, s, person, "contact", map[string]any{"name": "Ana", "email": "a@x.example"}, map[string]any{"name": "Bo", "email": "a@x.example"})
	strict := []byte(`{"id":"strict","records":{"contact":{"uses":true,"fields":{"email2":{"type":"email"}}}},"capabilities":{"records":{"contact":"r"}}}`)
	if err := s.RegisterApp(strict); err != nil {
		t.Fatal(err)
	}
	crm := strings.Replace(string(fixture(t, "crm-app.json")), `"email": { "type": "email", "index": true }`, `"email": { "type": "email", "unique": true }`, 1)
	err = s.RegisterApp([]byte(crm))
	if e := wantCode(t, err, CodeDuplicate); e.Field != "email" {
		t.Fatal(e)
	}
}

// COMMENTS AND TRANSITIONS MAKE ONE FEED, and a live query on comments is
// told of each.
func TestActivityFeedAndTableLiveQuery(t *testing.T) {
	s, _ := openOps(t)
	id := mustCreate(t, s, person, "case", map[string]any{"name": "C1", "amount": 5})[0].ID()
	rec := newRecorder()
	if _, err := s.Subscribe(person, SubSpec{ID: "notes", Table: TableComments, Type: "case", Record: id}, rec.take); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddComment(Scope{App: "ops", Agent: true}, Comment{Type: "case", Record: id, Body: "The amount looks low", Mentions: []string{"me"}}); err != nil {
		t.Fatal(err)
	}
	d := rec.next(t)
	if len(d.Upserts) != 1 || d.Upserts[0]["author"] != "ops agent" || d.Total != 1 || d.Cursor == 0 {
		t.Fatalf("comment diff: %+v", d)
	}
	if _, err := s.Transition(person, "case", id, "submit", TransitionOptions{Reason: "checked"}); err != nil {
		t.Fatal(err)
	}
	feed, err := s.RecordActivity(person, "case", id)
	if err != nil || len(feed) != 2 {
		t.Fatalf("feed: %+v %v", feed, err)
	}
	if feed[0].Kind != ActivityComment || feed[0].Mentions[0] != s.Person() || feed[1].Kind != ActivityTransition ||
		feed[1].From != "Open" || feed[1].To != "Review" || feed[1].Reason != "checked" || feed[1].Transition != "submit" {
		t.Fatalf("feed: %+v", feed)
	}
}

// A LIVE QUERY RESUMES FROM ITS CURSOR: one diff of everything since, or a
// reset when the outbox no longer reaches back that far.
func TestLiveQueryResumesFromItsCursor(t *testing.T) {
	s, _ := openCRM(t)
	_, deals := seedDeals(t, s)
	rec := newRecorder()
	cursor, err := s.Subscribe(person, SubSpec{ID: "open", Type: "deal", Query: "-stage:Lost"}, rec.take)
	if err != nil || cursor == 0 {
		t.Fatalf("cursor %d %v", cursor, err)
	}
	s.Unsubscribe("open")
	// While away: one deal lost, one new.
	if _, err := s.Update(person, "deal", []string{deals["Initech · pilot"]}, map[string]any{"stage": "Lost"}, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	fresh := mustCreate(t, s, person, "deal", map[string]any{"name": "New one"})[0].ID()
	back := newRecorder()
	if _, err := s.Subscribe(person, SubSpec{ID: "open", Type: "deal", Query: "-stage:Lost", Resume: true, After: cursor}, back.take); err != nil {
		t.Fatal(err)
	}
	d := back.next(t)
	if d.Reset || len(d.Upserts) != 1 || d.Upserts[0].ID() != fresh || !equal(d.Removes, []string{deals["Initech · pilot"]}) || d.Total != 5 {
		t.Fatalf("resume: %+v", d)
	}
	now, _ := s.Cursor()
	if d.Cursor != now {
		t.Fatalf("resume cursor %d, store %d", d.Cursor, now)
	}
	s.Unsubscribe("open")
	if err := s.TrimOutbox(now); err != nil {
		t.Fatal(err)
	}
	gone := newRecorder()
	if _, err := s.Subscribe(person, SubSpec{ID: "open", Type: "deal", Resume: true, After: cursor}, gone.take); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, s, person, "deal", map[string]any{"name": "after trim"})
	if d := gone.next(t); !d.Reset {
		t.Fatalf("resume past a trim: %+v", d)
	}
}

// THE QUERY IR: one hop across a reference, related fields selected, and
// aggregates the server computes, money to the cent.
func TestQueryIRRelatesSelectsAndAggregates(t *testing.T) {
	s, _ := openCRM(t)
	_, deals := seedDeals(t, s)
	ir, err := ParseQuery("company.name:Northwind")
	if err != nil {
		t.Fatal(err)
	}
	ir.Type = "deal"
	ir.Select = []string{"name", "company.name"}
	page, err := s.QueryRows(person, ir, "", 0)
	if err != nil || page.Total != 1 || page.Rows[0].ID() != deals["Northwind · renewal"] || page.Rows[0]["company.name"] != "Northwind" {
		t.Fatalf("relate: %+v %v", page, err)
	}
	if _, ok := page.Rows[0]["amount"]; ok {
		t.Fatalf("a selection carried an unselected field: %v", page.Rows[0])
	}
	mustCreate(t, s, person, "deal", map[string]any{"name": "cents a", "amount": 0.1}, map[string]any{"name": "cents b", "amount": 0.2})
	res, err := s.Aggregate(person, &QueryIR{Type: "deal", Group: []string{"stage"}, Aggregate: []Agg{{Fn: "count"}, {Fn: "sum", Field: "amount"}}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 7 || len(res.Groups) == 0 || res.Groups[0].Key[0] != "Lead" || res.Groups[0].Values["sum_amount"] != 4200.3 || fmt.Sprint(res.Groups[0].Values["count"]) != "3" {
		t.Fatalf("aggregate: %+v", res)
	}
	// An app cannot count through a value the person has not revealed.
	_, err = s.Aggregate(Scope{App: "crm"}, &QueryIR{Type: "contact", Aggregate: []Agg{{Fn: "count", Field: "phone"}}})
	wantCode(t, err, CodeInvalid)
	_, err = s.Aggregate(person, &QueryIR{Type: "deal", Aggregate: []Agg{{Fn: "sum", Field: "name"}}})
	wantCode(t, err, CodeInvalid)
	// A relation into a type the app may not read is refused.
	narrow := []byte(`{"id":"narrow","records":{"deal":{"uses":true,"fields":{}}},"capabilities":{"records":{"deal":"r"}}}`)
	if err := s.RegisterApp(narrow); err != nil {
		t.Fatal(err)
	}
	_, err = s.Query(Scope{App: "narrow"}, "deal", "company.name:Northwind", "", 0)
	wantCode(t, err, CodeNotAllowed)
}

// THE SCHEMA'S OWN SPELLINGS REGISTER: a lifecycle under process with named
// transitions and actors, a sequence's padding, and a ledger that names its
// reversal field.
func TestSchemaSpellingsOfProcessRegister(t *testing.T) {
	s, _ := openCRM(t)
	app := []byte(`{
	  "id": "books", "name": "Books",
	  "records": {
	    "claim": {"title": "name", "fields": {
	      "name": {"type": "text"},
	      "status": {"type": "enum", "values": ["Draft", "Sent", "Paid"]},
	      "ref": {"type": "text", "sequence": {"prefix": "CL-", "padding": 3}}}},
	    "move": {"title": "name", "ledger": {"reversal": "undoes"}, "fields": {
	      "name": {"type": "text"}, "amount": {"type": "money"},
	      "undoes": {"type": "ref", "to": "move"}}}
	  },
	  "process": {"lifecycles": {"claim": {"field": "status", "initial": "Draft", "states": ["Draft", "Sent", "Paid"],
	    "transitions": {
	      "send": {"label": "Send", "from": ["Draft"], "to": "Sent", "actors": ["person", "agent"]},
	      "pay": {"label": "Mark paid", "from": ["Sent"], "to": "Paid", "actors": ["person"], "guard": "signed('payment')"}}}}},
	  "capabilities": {"records": {"claim": "rw", "move": "rw"}}
	}`)
	if err := s.RegisterApp(app); err != nil {
		t.Fatal(err)
	}
	c := mustCreate(t, s, person, "claim", map[string]any{"name": "Taxi"})[0]
	if c["ref"] != "CL-001" || c["status"] != "Draft" {
		t.Fatalf("claim: %v", c)
	}
	if _, err := s.Transition(Scope{App: "books", Agent: true}, "claim", c.ID(), "send", TransitionOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err := s.Transition(person, "claim", c.ID(), "pay", TransitionOptions{})
	wantCode(t, err, CodeLifecycle)
	if _, err := s.Sign(person, "claim", c.ID(), "Paid by card", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Transition(person, "claim", c.ID(), "pay", TransitionOptions{}); err != nil {
		t.Fatal(err)
	}
	m := mustCreate(t, s, person, "move", map[string]any{"name": "In", "amount": 10})[0]
	rev, err := s.Reverse(person, "move", m.ID(), "")
	if err != nil || rev.Rows[0]["undoes"] != m.ID() || rev.Rows[0]["amount"] != -10.0 {
		t.Fatalf("reversal: %+v %v", rev, err)
	}
	both := strings.Replace(string(app), `"ledger": {"reversal": "undoes"}`, `"ledger": {"reversal": "name"}`, 1)
	err = s.RegisterApp([]byte(both))
	wantCode(t, err, CodeInvalid)
}
