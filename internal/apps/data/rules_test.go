package data

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestBookingRules(t *testing.T) {
	for _, tt := range []struct {
		name, declarations, fields string
		first, conflict, free      map[string]any
	}{
		{"overlap", `"rooms":{"record":"room","label":"Rooms","booking":{"type":"booking","resource":"room","start":"starts","end":"ends","overlap":"reject"}}`,
			`"room":{"type":"ref","to":"room"},"starts":{"type":"datetime"},"ends":{"type":"datetime"}`,
			map[string]any{"starts": "2026-11-25T12:00:00Z", "ends": "2026-11-25T13:00:00Z"},
			map[string]any{"starts": "2026-11-25T13:30:00+01:00", "ends": "2026-11-25T14:30:00+01:00"},
			map[string]any{"starts": "2026-11-25T13:00:00Z", "ends": "2026-11-25T14:00:00Z"}},
		{"capacity", `"people":{"label":"People","booking":{"type":"booking","resource":"worker","day":"day","maxPerDay":1}}`,
			`"worker":{"type":"person"},"day":{"type":"date"}`,
			map[string]any{"worker": "me", "day": "2026-11-25"}, map[string]any{"worker": "$me", "day": "2026-11-25"},
			map[string]any{"worker": "me", "day": "2026-11-26"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := openRuleStore(t)
			registerRules(t, s, `{"id":"ops","records":{"room":{"fields":{"name":{"type":"text"}}},"booking":{"fields":{`+tt.fields+`}}},
				"capabilities":{"records":{"room":"rw","booking":"rw"}},"resources":{`+tt.declarations+`}}`)
			if tt.name == "overlap" {
				room := mustCreate(t, s, person, "room", map[string]any{"name": "Boardroom"})[0].ID()
				for _, values := range []map[string]any{tt.first, tt.conflict, tt.free} {
					values["room"] = room
				}
			}
			first := mustCreate(t, s, person, "booking", tt.first)[0].ID()
			_, err := s.Create(Scope{App: "ops", Agent: true}, "booking", []map[string]any{tt.conflict})
			if e := wantCode(t, err, "booking_conflict"); !strings.Contains(e.Message, first) {
				t.Fatalf("conflict does not name %s: %v", first, e)
			}
			second := mustCreate(t, s, person, "booking", tt.free)[0].ID()
			_, err = s.Update(person, "booking", []string{second}, tt.conflict, UpdateOptions{})
			wantCode(t, err, "booking_conflict")
			lines, err := s.RecordActivity(person, "booking", first)
			if err != nil || len(lines) != 1 || lines[0].Kind != "rule" || lines[0].Reason == "" {
				t.Fatalf("booking activity = %+v, %v", lines, err)
			}
		})
	}
}

func openRuleStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "rules.db"), Options{Now: newClock().now, NoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func registerRules(t *testing.T, s *Store, raw string) {
	t.Helper()
	if err := s.RegisterApp([]byte(raw)); err != nil {
		t.Fatal(err)
	}
}

func TestAssignmentRules(t *testing.T) {
	for _, tt := range []struct {
		name, strategy string
		owners         []string
		transition     bool
	}{
		{"round robin rolls over", `"strategy":"round-robin","candidates":["agent:a","agent:b"]`, []string{"agent:a", "agent:b", "agent:a"}, false},
		{"field match", `"strategy":"field","field":"region","matches":{"East":"agent:e","West":"agent:w"}`, []string{"agent:e", "agent:w", "agent:e"}, false},
		{"fewest open tasks", `"strategy":"load","candidates":["agent:a","agent:b"]`, []string{"agent:b", "agent:a", "agent:b"}, false},
		{"linked record owner on transition", `"strategy":"linked-owner","field":"company","ownerField":"owner","transition":"queue"`, []string{"agent:account", "agent:account", "agent:account"}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := openRuleStore(t)
			registerRules(t, s, `{"id":"ops","records":{
				"company":{"fields":{"owner":{"type":"person"}}},
				"ticket":{"fields":{"region":{"type":"enum","values":["East","West"]},"company":{"type":"ref","to":"company"},"stage":{"type":"enum","values":["New","Queued"]}},
				"lifecycle":{"field":"stage","transitions":[{"id":"queue","to":"Queued","by":["person","ai"]}]}}},
				"capabilities":{"records":{"ticket":"rw","company":"rw"}},
				"process":{"work_items":{"triage":{"title":"Triage","subject":{"type":"ticket","id":"$record.id"},"assignee":"me","form":"triage"}}},
				"rules":{"dispatch":{"kind":"assignment","record":"ticket","workItem":"triage",`+tt.strategy+`}}}`)
			company := mustCreate(t, s, person, "company", map[string]any{"owner": "agent:account"})[0].ID()
			if tt.name == "fewest open tasks" {
				_, err := s.CreateWorkItem(person, WorkItem{Title: "Already open", Assignee: "agent:a"})
				if err != nil {
					t.Fatal(err)
				}
				done, err := s.CreateWorkItem(person, WorkItem{Title: "Already done", Assignee: "agent:b"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := s.CompleteWorkItem(person, done.ID, nil); err != nil {
					t.Fatal(err)
				}
			}
			for i, want := range tt.owners {
				sc := Scope{App: "ops", Agent: i%2 == 0}
				id := mustCreate(t, s, sc, "ticket", map[string]any{"region": []string{"East", "West"}[i%2], "company": company})[0].ID()
				if tt.transition {
					initial, err := s.Process(sc, "ticket", id)
					if err != nil || len(initial.WorkItems) != 0 {
						t.Fatalf("before queue: %+v, %v", initial, err)
					}
					if _, err := s.Transition(sc, "ticket", id, "queue", TransitionOptions{}); err != nil {
						t.Fatal(err)
					}
				}
				p, err := s.Process(sc, "ticket", id)
				if err != nil || len(p.WorkItems) != 1 || p.WorkItems[0].Assignee != want || p.WorkItems[0].Title != "Triage" || p.WorkItems[0].Form != "triage" {
					t.Fatalf("expected triage for %s: %+v, %v", want, p, err)
				}
				if p.Timeline[0].Kind != "rule" || !strings.Contains(p.Timeline[0].Reason, want) {
					t.Fatalf("assignment timeline = %+v", p.Timeline)
				}
			}
		})
	}
}

const approvalApp = `{
	"id":"ops","records":{"expense":{"fields":{"amount":{"type":"money"},"kind":{"type":"enum","values":["travel","office"]},
		"stage":{"type":"enum","values":["Open","Review","Paid"]}},
		"lifecycle":{"field":"stage","transitions":[{"id":"submit","to":"Review","by":["person","ai"]},
			{"id":"pay","from":["Review"],"to":"Paid","guard":"signed()"}]}}},
	"capabilities":{"records":{"expense":"rw"}},
	"roles":{"finance":{"kind":"approver"},"director":{"kind":"approver"}},
	"rules":{
		"standard":{"kind":"approval-routing","record":"expense","transition":"submit","order":"serial","roles":["finance"]},
		"travel":{"kind":"approval-routing","record":"expense","transition":"submit","order":"serial","roles":["finance","director"],
			"when":"kind == 'travel' && amount >= 10000","priority":10,"statement":"Approved for payment"},
		"separate":{"kind":"approval-routing","record":"expense","transition":"submit","order":"serial","roles":["director"],
			"when":"amount >= 100000","priority":20,"makerCannotSign":true}
	}
}`

func TestApprovalRules(t *testing.T) {
	for _, tt := range []struct {
		name, kind, firstRole, rule string
		amount, steps               int
		blocked                     bool
	}{
		{"below threshold", "travel", "finance", "standard", 9999, 1, false},
		{"at threshold", "travel", "finance", "travel", 10000, 2, false},
		{"other kind", "office", "finance", "standard", 10000, 1, false},
		{"maker cannot sign", "travel", "director", "separate", 100000, 1, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := openRuleStore(t)
			registerRules(t, s, approvalApp)
			id := mustCreate(t, s, Scope{App: "ops", Agent: true}, "expense", map[string]any{"amount": tt.amount, "kind": tt.kind})[0].ID()
			if _, err := s.Sign(person, "expense", id, "Approved for payment", 1); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Transition(Scope{App: "ops", Agent: true}, "expense", id, "submit", TransitionOptions{Rev: 1}); err != nil {
				t.Fatal(err)
			}
			_, err := s.Transition(person, "expense", id, "pay", TransitionOptions{Rev: 2})
			wantCode(t, err, CodeLifecycle)
			_, err = s.Sign(Scope{App: "ops", Agent: true}, "expense", id, "Approved for payment", 2)
			wantCode(t, err, CodeNotAllowed)
			if tt.blocked {
				_, err = s.Sign(Scope{Actor: "someone else"}, "expense", id, "Approved for payment", 2)
				e := wantCode(t, err, CodeNotAllowed)
				if !strings.Contains(e.Message, "creator") {
					t.Fatal(e)
				}
				return
			}
			for i := 0; i < tt.steps; i++ {
				if _, err := s.Sign(person, "expense", id, "Approved for payment", 2); err != nil {
					t.Fatal(err)
				}
				if i < tt.steps-1 {
					_, err := s.Transition(person, "expense", id, "pay", TransitionOptions{Rev: 2})
					wantCode(t, err, CodeLifecycle)
				}
			}
			_, err = s.Sign(person, "expense", id, "Approved for payment", 2)
			wantCode(t, err, CodeStale)
			lines, err := s.RecordActivity(person, "expense", id)
			if err != nil {
				t.Fatal(err)
			}
			var picked bool
			for _, line := range lines {
				picked = picked || line.Kind == "rule" && strings.HasPrefix(line.Reason, tt.rule+":")
			}
			if !picked {
				t.Fatalf("route %s missing from activity: %+v", tt.rule, lines)
			}
			if _, err := s.Transition(person, "expense", id, "pay", TransitionOptions{Rev: 2}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

const dailyBookingApp = `{"id":"ops","records":{"booking":{"fields":{"person":{"type":"person"},"day":{"type":"date"}}}},
	"capabilities":{"records":{"booking":"rw"}},
	"resources":{"shifts":{"label":"Shifts","booking":{"type":"booking","resource":"person","day":"day","maxPerDay":2}}}}`

func TestBookingRulesTwoWritersRaceForLastSlot(t *testing.T) {
	s := openRuleStore(t)
	registerRules(t, s, dailyBookingApp)
	values := map[string]any{"person": "me", "day": "2026-11-25"}
	first := mustCreate(t, s, person, "booking", values)[0].ID()
	other, err := Open(s.Path(), Options{NoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Close() })

	type result struct {
		write *WriteResult
		err   error
	}
	start, replies := make(chan struct{}), make(chan result, 2)
	for _, store := range []*Store{s, other} {
		go func() {
			<-start
			write, err := store.Create(Scope{App: "ops", Agent: store == other}, "booking", []map[string]any{values})
			replies <- result{write, err}
		}()
	}
	close(start)
	one, two := <-replies, <-replies
	if one.err != nil {
		one, two = two, one
	}
	if one.err != nil || len(one.write.Rows) != 1 {
		t.Fatalf("want a single winner: %+v, %+v", one, two)
	}
	e := wantCode(t, two.err, CodeBookingConflict)
	if !strings.Contains(e.Message, first) && !strings.Contains(e.Message, one.write.Rows[0].ID()) {
		t.Fatalf("conflict did not name a committed record: %v", e)
	}
	if _, err := s.Delete(person, "booking", []string{first}, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, s, person, "booking", values)
}

func TestBookingRulesRollBackBatchAndUndoChecksCapacity(t *testing.T) {
	s := openRuleStore(t)
	registerRules(t, s, dailyBookingApp)
	values := map[string]any{"person": "me", "day": "2026-11-25"}
	_, err := s.Create(person, "booking", []map[string]any{values, values, values})
	wantCode(t, err, CodeBookingConflict)
	all, err := s.Query(person, "booking", "", "", 10)
	if err != nil || all.Total != 0 {
		t.Fatalf("partial bookings: %+v, %v", all, err)
	}
	id := mustCreate(t, s, person, "booking", values)[0].ID()
	deleted, err := s.Delete(person, "booking", []string{id}, UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(t, s, person, "booking", values, values)
	_, err = s.Undo(person, deleted.Batch)
	wantCode(t, err, CodeBookingConflict)
}

func TestApprovalRulesEditsRestartAChain(t *testing.T) {
	s := openRuleStore(t)
	registerRules(t, s, approvalApp)
	id := mustCreate(t, s, person, "expense", map[string]any{"amount": 10000, "kind": "travel"})[0].ID()
	if _, err := s.Transition(person, "expense", id, "submit", TransitionOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sign(person, "expense", id, "Approved for payment", 2); err != nil {
		t.Fatal(err)
	}
	p, err := s.Process(person, "expense", id)
	if err != nil || p.Approval.Steps[0].Signoff == "" || !p.Approval.Steps[1].Ready {
		t.Fatalf("first step: %+v, %v", p, err)
	}
	_, err = s.Update(person, "expense", []string{id}, map[string]any{"amount": 10001}, UpdateOptions{Rev: 2})
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.Process(person, "expense", id)
	if err != nil || p.Approval.Steps[0].Signoff != "" || !p.Approval.Steps[0].Ready || p.Approval.Steps[1].Ready || p.Signoffs[0].Current {
		t.Fatalf("edited chain: %+v, %v", p, err)
	}
	_, err = s.Sign(person, "expense", id, "A different statement", 3)
	wantCode(t, err, CodeInvalid)
}

func TestBookingRulesRejectTighteningOverExistingConflicts(t *testing.T) {
	s := openRuleStore(t)
	registerRules(t, s, dailyBookingApp)
	values := map[string]any{"person": "me", "day": "2026-11-25"}
	existing := mustCreate(t, s, person, "booking", values, values)
	err := s.RegisterApp([]byte(strings.Replace(dailyBookingApp, `"maxPerDay":2`, `"maxPerDay":1`, 1)))
	e := wantCode(t, err, CodeBookingConflict)
	if !strings.Contains(e.Message, existing[1].ID()) {
		t.Fatal(e)
	}
	if _, err := s.Delete(person, "booking", []string{existing[0].ID()}, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, s, person, "booking", values)
}

func TestAssignmentRulesKeepLinkedOwnersWithinTheirReadGrant(t *testing.T) {
	s := openRuleStore(t)
	registerRules(t, s, `{
		"id":"directory","records":{"company":{"fields":{"owner":{"type":"person"}}}},
		"capabilities":{"records":{"company":"rw"}}
	}`)
	registerRules(t, s, `{
		"id":"ops","records":{"ticket":{"fields":{"company":{"type":"ref","to":"company"}}}},
		"capabilities":{"records":{"ticket":"rw","company":"r"}},
		"process":{"work_items":{"triage":{"title":"Triage","subject":{"type":"ticket","id":"$record.id"},"assignee":"me","form":"triage"}}},
		"rules":{"dispatch":{"kind":"assignment","record":"ticket","workItem":"triage","strategy":"linked-owner","field":"company","ownerField":"owner"}}
	}`)
	registerRules(t, s, `{"id":"intake","records":{"ticket":{"uses":true}},"capabilities":{"records":{"ticket":"rw"}}}`)
	company := mustCreate(t, s, person, "company", map[string]any{"owner": "agent:private"})[0].ID()
	intake := Scope{App: "intake", Agent: true}
	id := mustCreate(t, s, intake, "ticket", map[string]any{"company": company})[0].ID()
	readable, err := s.Process(Scope{App: "ops"}, "ticket", id)
	if err != nil || len(readable.WorkItems) != 1 || readable.WorkItems[0].Assignee != "agent:private" || len(readable.Timeline) != 1 {
		t.Fatalf("source granted: %+v, %v", readable, err)
	}
	narrow, err := s.Process(intake, "ticket", id)
	if err != nil || len(narrow.WorkItems) != 0 || len(narrow.Timeline) != 0 {
		t.Fatalf("source unavailable: %+v, %v", narrow, err)
	}
	_, err = s.ReassignWorkItem(intake, readable.WorkItems[0].ID, "me")
	wantCode(t, err, CodeNotFound)
	err = s.RegisterApp([]byte(`{"id":"directory","records":{"company":{"fields":{"owner":{"type":"person","masked":true}}}},"capabilities":{"records":{"company":"rw"}}}`))
	wantCode(t, err, CodeInvalid)
	if err := s.ForgetApp("ops"); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, s, intake, "ticket", map[string]any{})
}

func TestApprovalRulesParallelStepsAreReadyTogether(t *testing.T) {
	s := openRuleStore(t)
	registerRules(t, s, strings.Replace(approvalApp, `"order":"serial","roles":["finance","director"]`, `"order":"parallel","roles":["finance","director"]`, 1))
	id := mustCreate(t, s, person, "expense", map[string]any{"amount": 10000, "kind": "travel"})[0].ID()
	if _, err := s.Transition(person, "expense", id, "submit", TransitionOptions{}); err != nil {
		t.Fatal(err)
	}
	p, err := s.Process(person, "expense", id)
	if err != nil || len(p.Approval.Steps) != 2 || !p.Approval.Steps[0].Ready || !p.Approval.Steps[1].Ready {
		t.Fatalf("parallel approval = %+v, %v", p, err)
	}
}

func TestAssignmentRulesUpgradeExistingWorkItems(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old-rules.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// Version 3 had assignment work items without a linked source requirement.
	if _, err := db.Exec(migrations[0] + dataPlaneMigration + writeRulesMigration + `
		PRAGMA user_version = 3;
		INSERT INTO work_items(id,title,status,created_by,created_at,updated_at)
		VALUES('01K00000000000000000000001','Existing work','open','','','');
	`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path, Options{NoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	items, err := s.WorkItems(person, WorkFilter{})
	if err != nil || len(items) != 1 || items[0].Title != "Existing work" {
		t.Fatalf("upgraded work items = %+v, %v", items, err)
	}
}
