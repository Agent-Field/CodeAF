package workstream

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

// WP-F2 (a): draft metadata survives storage, while old plan JSON stays optional.
func TestWorkstreamDraftFieldsRoundTripAndOldJSON(t *testing.T) {
	s, db, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Drafts")
	item := PlanItem{Kind: "draft", DraftID: "draft", Title: "Draft", Size: "M", BudgetUSD: 10000, Source: &Source{Kind: "message", ID: "message", ChatID: "chat", Label: "Scope"}}
	waves := []Wave{{N: 1, Items: []PlanItem{item}}}
	p, _, err := s.SetPlan(ctx, w.ID, 0, nil, waves, Attribution{})
	if err != nil || !reflect.DeepEqual(p.Waves, waves) {
		t.Fatalf("set: %+v %v", p, err)
	}
	p, err = s.Plan(ctx, w.ID)
	if err != nil || !reflect.DeepEqual(p.Waves, waves) {
		t.Fatalf("get: %+v %v", p, err)
	}
	err = db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE workstream_plans SET waves=? WHERE workstream_id=?`, `[{"n":1,"auto_start":false,"items":[{"kind":"draft","draft_id":"old","title":"Old"}]}]`, w.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.Plan(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(p.Waves[0].Items[0])
	if err != nil || strings.Contains(string(raw), "size") || strings.Contains(string(raw), "budget_usd") || strings.Contains(string(raw), "source") {
		t.Fatalf("old fields must stay absent: %s %v", raw, err)
	}
}

// WP-F2 (a): each invalid draft field refuses the entire plan without a write.
func TestWorkstreamDraftFieldRefusalsAreAtomic(t *testing.T) {
	for name, item := range map[string]PlanItem{
		"job size":             {Kind: "job", Factory: "f", Job: 7, Size: "S"},
		"job budget":           {Kind: "job", Factory: "f", Job: 7, BudgetUSD: 1},
		"job source":           {Kind: "job", Factory: "f", Job: 7, Source: &Source{Kind: "chat", ID: "chat"}},
		"size XL":              {Kind: "draft", DraftID: "d", Title: "Draft", Size: "XL"},
		"negative":             {Kind: "draft", DraftID: "d", Title: "Draft", BudgetUSD: -1},
		"over cap":             {Kind: "draft", DraftID: "d", Title: "Draft", BudgetUSD: 10000.01},
		"NaN":                  {Kind: "draft", DraftID: "d", Title: "Draft", BudgetUSD: math.NaN()},
		"infinite":             {Kind: "draft", DraftID: "d", Title: "Draft", BudgetUSD: math.Inf(1)},
		"message without chat": {Kind: "draft", DraftID: "d", Title: "Draft", Source: &Source{Kind: "message", ID: "msg"}},
		"chat with factory":    {Kind: "draft", DraftID: "d", Title: "Draft", Source: &Source{Kind: "chat", ID: "chat", Factory: "f"}},
		"page with chat":       {Kind: "draft", DraftID: "d", Title: "Draft", Source: &Source{Kind: "page", ID: "p", ChatID: "chat"}},
		"bad source kind":      {Kind: "draft", DraftID: "d", Title: "Draft", Source: &Source{Kind: "other", ID: "p"}},
		"empty source id":      {Kind: "draft", DraftID: "d", Title: "Draft", Source: &Source{Kind: "chat"}},
		"long source id":       {Kind: "draft", DraftID: "d", Title: "Draft", Source: &Source{Kind: "chat", ID: strings.Repeat("x", 4097)}},
		"multiline label":      {Kind: "draft", DraftID: "d", Title: "Draft", Source: &Source{Kind: "chat", ID: "chat", Label: "a\nb"}},
	} {
		t.Run(name, func(t *testing.T) {
			s, _, _ := testStore(t)
			ctx := context.Background()
			w := createStream(t, s, "Refusals", workspace.Ref{Kind: workspace.FactoryJobKind, Factory: "f", ID: "7"})
			before, _ := s.Plan(ctx, w.ID)
			_, c, err := s.SetPlan(ctx, w.ID, 0, nil, []Wave{{N: 1, Items: []PlanItem{item}}}, Attribution{})
			if !errors.Is(err, ErrInvalid) || len(c.Parts) != 0 {
				t.Fatalf("refusal: %+v %v", c, err)
			}
			after, err := s.Plan(ctx, w.ID)
			row, rowErr := s.Get(ctx, w.ID)
			if err != nil || rowErr != nil || !reflect.DeepEqual(before, after) || !row.UpdatedAt.Equal(w.UpdatedAt) {
				t.Fatalf("refusal changed plan: %+v %v %v", after, err, rowErr)
			}
		})
	}
}

func workstreamProposalFixture(t *testing.T, s *Store, now time.Time) (Workstream, GoAheadProposal) {
	t.Helper()
	w := createStream(t, s, "Proposal", workspace.Ref{Kind: workspace.FactoryJobKind, Factory: "f", ID: "7"})
	if _, _, err := s.SetPlan(context.Background(), w.ID, 0, []Milestone{{ID: "ship", Title: "Ship"}}, []Wave{{N: 1, Items: []PlanItem{}}}, Attribution{}); err != nil {
		t.Fatal(err)
	}
	return w, GoAheadProposal{Scope: "wave", Wave: 1, CapUSD: 12.5, EndsAt: now.Add(time.Hour)}
}

// WP-F2 (d): keeping grants once, dropping revokes, re-keeping replaces the id.
func TestWorkstreamGoAheadRecordLifecycle(t *testing.T) {
	for _, target := range []GoAheadProposal{{Scope: "job", Factory: "f", Job: 7}, {Scope: "wave", Wave: 1}, {Scope: "milestone", MilestoneID: "ship"}} {
		t.Run(target.Scope, func(t *testing.T) {
			s, _, now := testStore(t)
			ctx := context.Background()
			w, p := workstreamProposalFixture(t, s, *now)
			target.CapUSD, target.EndsAt = p.CapUSD, p.EndsAt
			r, _, err := s.ProposeRecord(ctx, RecordPropose{WorkstreamID: w.ID, Kind: "goahead", Text: "Why", GoAhead: &target})
			if err != nil || r.State != "proposed" || r.GoAheadID != "" || !reflect.DeepEqual(r.GoAhead, &target) {
				t.Fatalf("propose: %+v %v", r, err)
			}
			if grants, err := s.GoAheads(ctx, w.ID, true); err != nil || len(grants) != 0 {
				t.Fatalf("proposal granted: %+v %v", grants, err)
			}
			r, c, err := s.KeepRecord(ctx, r.ID)
			if err != nil || r.State != "kept" || r.DecidedAt.IsZero() || !slices.Contains(c.Parts, PartRecords) || !slices.Contains(c.Parts, PartGoAheads) {
				t.Fatalf("keep: %+v %+v %v", r, c, err)
			}
			grants, err := s.GoAheads(ctx, w.ID, false)
			if err != nil || len(grants) != 1 {
				t.Fatalf("grants: %+v %v", grants, err)
			}
			g := grants[0]
			if g.ID != r.GoAheadID || g.Scope != target.Scope || g.Factory != target.Factory || g.Job != target.Job || g.Wave != target.Wave || g.MilestoneID != target.MilestoneID || g.CapUSD != target.CapUSD || !g.EndsAt.Equal(target.EndsAt) {
				t.Fatalf("grant differs: %+v %+v", g, target)
			}
			again, c, err := s.KeepRecord(ctx, r.ID)
			grants, listErr := s.GoAheads(ctx, w.ID, true)
			if err != nil || listErr != nil || !reflect.DeepEqual(again, r) || len(c.Parts) != 0 || len(grants) != 1 {
				t.Fatalf("repeat keep: %+v %+v %v %v", again, c, err, listErr)
			}
			dropped, c, err := s.DropRecord(ctx, r.ID)
			grants, listErr = s.GoAheads(ctx, w.ID, true)
			if err != nil || listErr != nil || dropped.State != "dropped" || dropped.GoAheadID != r.GoAheadID || len(grants) != 1 || grants[0].State != "ended" || grants[0].EndReason != "revoked" || !slices.Contains(c.Parts, PartGoAheads) {
				t.Fatalf("drop: %+v %+v %+v %v %v", dropped, c, grants, err, listErr)
			}
			kept, _, err := s.KeepRecord(ctx, r.ID)
			if err != nil || kept.GoAheadID == r.GoAheadID || kept.GoAheadID == "" {
				t.Fatalf("re-keep: %+v %v", kept, err)
			}
			if _, _, err := s.MarkDone(ctx, w.ID, ""); err != nil {
				t.Fatal(err)
			}
			grants, err = s.GoAheads(ctx, w.ID, true)
			if err != nil || len(grants) != 2 || grants[0].ID != kept.GoAheadID || grants[0].EndReason != "workstream_done" {
				t.Fatalf("done: %+v %v", grants, err)
			}
			records, err := s.Records(ctx, w.ID, "goahead", "")
			if err != nil || len(records) != 1 || !reflect.DeepEqual(records[0], kept) {
				t.Fatalf("done changed kept record: %+v %v", records, err)
			}
			again, c, err = s.KeepRecord(ctx, r.ID)
			if err != nil || len(c.Parts) != 0 || !reflect.DeepEqual(again, kept) {
				t.Fatalf("already kept on done: %+v %+v %v", again, c, err)
			}
		})
	}
}

// WP-F2 (d): keep checks current state in its transaction and rolls back refusal.
func TestWorkstreamGoAheadKeepRefusalsAreAtomic(t *testing.T) {
	for _, reason := range []string{"done", "wave removed", "expired"} {
		t.Run(reason, func(t *testing.T) {
			s, _, now := testStore(t)
			ctx := context.Background()
			w, p := workstreamProposalFixture(t, s, *now)
			r, _, err := s.ProposeRecord(ctx, RecordPropose{WorkstreamID: w.ID, Kind: "goahead", Text: "Why", GoAhead: &p})
			if err != nil {
				t.Fatal(err)
			}
			want := ErrInvalid
			switch reason {
			case "done":
				_, _, err = s.MarkDone(ctx, w.ID, "")
				want = ErrConflict
			case "wave removed":
				_, _, err = s.SetPlan(ctx, w.ID, 1, nil, nil, Attribution{})
			case "expired":
				*now = p.EndsAt
			}
			if err != nil {
				t.Fatal(err)
			}
			before, _ := s.Get(ctx, w.ID)
			_, c, err := s.KeepRecord(ctx, r.ID)
			if !errors.Is(err, want) || len(c.Parts) != 0 {
				t.Fatalf("keep: %+v %v", c, err)
			}
			records, err := s.Records(ctx, w.ID, "goahead", "")
			grants, grantErr := s.GoAheads(ctx, w.ID, true)
			after, rowErr := s.Get(ctx, w.ID)
			if err != nil || grantErr != nil || rowErr != nil || len(records) != 1 || !reflect.DeepEqual(records[0], r) || len(grants) != 0 || !after.UpdatedAt.Equal(before.UpdatedAt) {
				t.Fatalf("refusal wrote: %+v %+v %v %v %v", records, grants, err, grantErr, rowErr)
			}
		})
	}
}

// WP-F2 (d), (f): invalid proposals/supersedes leave records and grants unchanged.
func TestWorkstreamGoAheadProposeRefusalsAndListFilters(t *testing.T) {
	s, _, now := testStore(t)
	ctx := context.Background()
	w, p := workstreamProposalFixture(t, s, *now)
	for _, kind := range []string{"decision", "question", "goahead"} {
		ask := RecordPropose{WorkstreamID: w.ID, Kind: kind, Text: "Why"}
		if kind == "goahead" {
			ask.GoAhead = &p
		}
		if _, _, err := s.ProposeRecord(ctx, ask); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		kind    string
		include bool
		count   int
	}{{"", false, 2}, {"", true, 3}, {"goahead", false, 1}, {"question", true, 1}} {
		records, err := s.Records(ctx, w.ID, tc.kind, "", tc.include)
		if err != nil || len(records) != tc.count {
			t.Fatalf("filter %+v: %+v %v", tc, records, err)
		}
	}
	before, _ := s.Records(ctx, w.ID, "", "", true)
	defaults, err := s.Records(ctx, w.ID, "", "")
	if err != nil || len(defaults) != 2 {
		t.Fatalf("default list included goaheads: %+v %v", defaults, err)
	}
	missing := p
	missing.Wave = 2
	for _, ask := range []RecordPropose{
		{Kind: "goahead"}, {Kind: "goahead", GoAhead: &p, Kept: true},
		{Kind: "decision", GoAhead: &p}, {Kind: "question", GoAhead: &p},
		{Kind: "goahead", GoAhead: &missing},
	} {
		ask.WorkstreamID, ask.Text = w.ID, "Why"
		if _, c, err := s.ProposeRecord(ctx, ask); !errors.Is(err, ErrInvalid) || len(c.Parts) != 0 {
			t.Fatalf("propose %+v: %+v %v", ask, c, err)
		}
	}
	for _, tc := range []struct{ id, kind string }{{before[0].ID, ""}, {before[0].ID, "decision"}, {before[1].ID, "goahead"}} {
		if _, _, c, err := s.SupersedeRecord(ctx, tc.id, RecordReplacement{Kind: tc.kind, Text: "Replacement"}); !errors.Is(err, ErrInvalid) || len(c.Parts) != 0 {
			t.Fatalf("supersede %+v: %+v %v", tc, c, err)
		}
	}
	after, err := s.Records(ctx, w.ID, "", "", true)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("refusals wrote: %+v %v", after, err)
	}
	if _, _, err := s.MarkDone(ctx, w.ID, ""); err != nil {
		t.Fatal(err)
	}
	for _, by := range []string{"", "you", "agent"} {
		if _, _, err := s.ProposeRecord(ctx, RecordPropose{WorkstreamID: w.ID, Kind: "goahead", Text: "Why", GoAhead: &p, By: by}); !errors.Is(err, ErrConflict) {
			t.Fatalf("done attribution %q: %v", by, err)
		}
	}
}

// WP-F2 (d): proposal validation uses the same cap, end and scope rules as grants.
func TestWorkstreamGoAheadProposalsUseGrantBounds(t *testing.T) {
	s, _, now := testStore(t)
	ctx := context.Background()
	w, good := workstreamProposalFixture(t, s, *now)
	for name, mutate := range map[string]func(*GoAheadProposal){
		"cap zero":          func(p *GoAheadProposal) { p.CapUSD = 0 },
		"cap negative":      func(p *GoAheadProposal) { p.CapUSD = -1 },
		"cap too large":     func(p *GoAheadProposal) { p.CapUSD = 10000.01 },
		"cap NaN":           func(p *GoAheadProposal) { p.CapUSD = math.NaN() },
		"cap infinite":      func(p *GoAheadProposal) { p.CapUSD = math.Inf(1) },
		"end zero":          func(p *GoAheadProposal) { p.EndsAt = time.Time{} },
		"end now":           func(p *GoAheadProposal) { p.EndsAt = *now },
		"end too far":       func(p *GoAheadProposal) { p.EndsAt = now.Add(90*24*time.Hour + time.Millisecond) },
		"mixed scope":       func(p *GoAheadProposal) { p.Factory = "f" },
		"missing job":       func(p *GoAheadProposal) { p.Scope = "job"; p.Factory = "f"; p.Job = 8; p.Wave = 0 },
		"missing milestone": func(p *GoAheadProposal) { p.Scope = "milestone"; p.MilestoneID = "gone"; p.Wave = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			p := good
			mutate(&p)
			_, c, err := s.ProposeRecord(ctx, RecordPropose{WorkstreamID: w.ID, Kind: "goahead", Text: "Why", GoAhead: &p})
			if !errors.Is(err, ErrInvalid) || len(c.Parts) != 0 {
				t.Fatalf("proposal: %+v %v", c, err)
			}
		})
	}
	all, err := s.Records(ctx, w.ID, "", "", true)
	grants, grantErr := s.GoAheads(ctx, w.ID, true)
	if err != nil || grantErr != nil || len(all) != 0 || len(grants) != 0 {
		t.Fatalf("refusals wrote: %+v %+v %v %v", all, grants, err, grantErr)
	}
	good.CapUSD, good.EndsAt = 10000, now.Add(90*24*time.Hour)
	if _, _, err := s.ProposeRecord(ctx, RecordPropose{WorkstreamID: w.ID, Kind: "goahead", Text: "Why", GoAhead: &good}); err != nil {
		t.Fatalf("inclusive bounds: %v", err)
	}
}

// WP-F2 (d): a failed record update rolls back the preceding grant or revocation.
func TestWorkstreamGoAheadRecordTransactionRollsBackGrantChanges(t *testing.T) {
	s, db, now := testStore(t)
	ctx := context.Background()
	w, p := workstreamProposalFixture(t, s, *now)
	r, _, err := s.ProposeRecord(ctx, RecordPropose{WorkstreamID: w.ID, Kind: "goahead", Text: "Why", GoAhead: &p})
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"kept", "dropped"} {
		if state == "dropped" {
			r, _, err = s.KeepRecord(ctx, r.ID)
			if err != nil {
				t.Fatal(err)
			}
		}
		before, err := s.GoAheads(ctx, w.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		err = db.Write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TRIGGER workstream_record_fail BEFORE UPDATE ON workstream_records BEGIN SELECT RAISE(ABORT,'fixture refuses record write'); END`)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		var c Change
		if state == "kept" {
			_, c, err = s.KeepRecord(ctx, r.ID)
		} else {
			_, c, err = s.DropRecord(ctx, r.ID)
		}
		if err == nil || len(c.Parts) != 0 {
			t.Fatalf("failed record write: %+v %v", c, err)
		}
		after, err := s.GoAheads(ctx, w.ID, true)
		records, recordErr := s.Records(ctx, w.ID, "goahead", "")
		if err != nil || recordErr != nil || !reflect.DeepEqual(before, after) || len(records) != 1 || !reflect.DeepEqual(records[0], r) {
			t.Fatalf("partial commit: %+v %+v %v %v", after, records, err, recordErr)
		}
		err = db.Write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "DROP TRIGGER workstream_record_fail")
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// WP-F2 (d): expiry and explicit revocation leave a kept record unchanged.
func TestWorkstreamGoAheadDropAfterExpiryOrRevocation(t *testing.T) {
	for _, reason := range []string{"expired", "revoked"} {
		t.Run(reason, func(t *testing.T) {
			s, _, now := testStore(t)
			ctx := context.Background()
			w, p := workstreamProposalFixture(t, s, *now)
			r, _, err := s.ProposeRecord(ctx, RecordPropose{WorkstreamID: w.ID, Kind: "goahead", Text: "Why", GoAhead: &p})
			if err != nil {
				t.Fatal(err)
			}
			r, _, err = s.KeepRecord(ctx, r.ID)
			if err != nil {
				t.Fatal(err)
			}
			if reason == "expired" {
				*now = p.EndsAt
			} else {
				_, _, err = s.RevokeGoAhead(ctx, r.GoAheadID)
			}
			if err != nil {
				t.Fatal(err)
			}
			records, err := s.Records(ctx, w.ID, "goahead", "")
			if err != nil || len(records) != 1 || !reflect.DeepEqual(records[0], r) {
				t.Fatalf("ended changed record: %+v %v", records, err)
			}
			dropped, c, err := s.DropRecord(ctx, r.ID)
			grants, grantErr := s.GoAheads(ctx, w.ID, true)
			if err != nil || grantErr != nil || dropped.GoAheadID != r.GoAheadID || slices.Contains(c.Parts, PartGoAheads) || len(grants) != 1 || grants[0].EndReason != reason {
				t.Fatalf("drop: %+v %+v %+v %v %v", dropped, c, grants, err, grantErr)
			}
		})
	}
}

// WP-F2 (d): a proposal's end is stored at the grant's precision, so keep grants the same end.
func TestWorkstreamGoAheadKeepGrantsTheProposedEndExactly(t *testing.T) {
	s, _, now := testStore(t)
	ctx := context.Background()
	w, p := workstreamProposalFixture(t, s, *now)
	p.EndsAt = p.EndsAt.Add(123456 * time.Microsecond)
	r, _, err := s.ProposeRecord(ctx, RecordPropose{WorkstreamID: w.ID, Kind: "goahead", Text: "Why", GoAhead: &p})
	if err != nil {
		t.Fatal(err)
	}
	r, _, err = s.KeepRecord(ctx, r.ID)
	grants, listErr := s.GoAheads(ctx, w.ID, false)
	if err != nil || listErr != nil || len(grants) != 1 || r.GoAhead == nil {
		t.Fatalf("keep: %+v %+v %v %v", r, grants, err, listErr)
	}
	if !grants[0].EndsAt.Equal(r.GoAhead.EndsAt) || r.GoAhead.EndsAt.Nanosecond() != 123000000 {
		t.Fatalf("proposal end %v, grant end %v", r.GoAhead.EndsAt, grants[0].EndsAt)
	}
}
