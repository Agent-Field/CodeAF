package workstream

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

// Part A (g): keep/drop support undo; supersede creates a kept replacement with
// both links, and every attempt to change the old record conflicts.
func TestPartARecordLifecycle(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Records")
	r, _, err := s.ProposeRecord(ctx, RecordPropose{WorkstreamID: w.ID, Kind: "question", Text: "Count retries?"})
	if err != nil || r.State != "proposed" || r.By != "agent" {
		t.Fatalf("propose: %+v %v", r, err)
	}
	r, _, err = s.KeepRecord(ctx, r.ID)
	if err != nil || r.State != "kept" || r.DecidedAt.IsZero() {
		t.Fatalf("keep: %+v %v", r, err)
	}
	r, _, err = s.DropRecord(ctx, r.ID)
	if err != nil || r.State != "dropped" {
		t.Fatalf("drop: %+v %v", r, err)
	}
	r, _, err = s.KeepRecord(ctx, r.ID)
	if err != nil || r.State != "kept" {
		t.Fatalf("undo: %+v %v", r, err)
	}
	replacement, old, _, err := s.SupersedeRecord(ctx, r.ID, RecordReplacement{Kind: "decision", Text: "Retries do not count"})
	if err != nil || replacement.State != "kept" || replacement.Supersedes != r.ID || old.State != "superseded" || old.SupersededBy != replacement.ID {
		t.Fatalf("supersede: %+v %+v %v", replacement, old, err)
	}
	if _, _, err := s.KeepRecord(ctx, r.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("keep superseded: %v", err)
	}
	if _, _, err := s.DropRecord(ctx, r.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("drop superseded: %v", err)
	}
	if _, _, _, err := s.SupersedeRecord(ctx, r.ID, RecordReplacement{Text: "Again"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("supersede superseded: %v", err)
	}
	kept, _, err := s.ProposeRecord(ctx, RecordPropose{WorkstreamID: w.ID, Kind: "decision", Text: "My decision", Kept: true})
	if err != nil || kept.By != "you" || kept.State != "kept" || kept.DecidedAt.IsZero() {
		t.Fatalf("kept: %+v %v", kept, err)
	}
	all, err := s.Records(ctx, w.ID, "", "")
	if err != nil || len(all) != 3 {
		t.Fatalf("records: %+v %v", all, err)
	}
	proposed, err := s.Records(ctx, w.ID, "question", "proposed")
	if err != nil || len(proposed) != 0 {
		t.Fatalf("filter: %+v %v", proposed, err)
	}
}

// Part A (h): each malformed grant is refused and stores nothing.
func TestPartAGoAheadBoundsAndTargets(t *testing.T) {
	s, _, now := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Bounds", workspace.Ref{Kind: workspace.FactoryJobKind, ID: "4", Factory: "f"})
	good := GoAheadGrant{WorkstreamID: w.ID, Scope: "job", Factory: "f", Job: 4, CapUSD: 1, EndsAt: now.Add(time.Hour)}
	for _, tc := range []struct {
		name   string
		change func(*GoAheadGrant)
	}{
		{"scope", func(p *GoAheadGrant) { p.Scope = "" }},
		{"cap absent", func(p *GoAheadGrant) { p.CapUSD = 0 }},
		{"negative cap", func(p *GoAheadGrant) { p.CapUSD = -1 }},
		{"large cap", func(p *GoAheadGrant) { p.CapUSD = 10000.01 }},
		{"nan cap", func(p *GoAheadGrant) { p.CapUSD = math.NaN() }},
		{"infinite cap", func(p *GoAheadGrant) { p.CapUSD = math.Inf(1) }},
		{"end absent", func(p *GoAheadGrant) { p.EndsAt = time.Time{} }},
		{"end past", func(p *GoAheadGrant) { p.EndsAt = now.Add(-time.Second) }},
		{"end now", func(p *GoAheadGrant) { p.EndsAt = *now }},
		{"end too far", func(p *GoAheadGrant) { p.EndsAt = now.Add(90*24*time.Hour + time.Millisecond) }},
		{"factory", func(p *GoAheadGrant) { p.Factory = "" }},
		{"job", func(p *GoAheadGrant) { p.Job = 0 }},
		{"not member", func(p *GoAheadGrant) { p.Job = 5 }},
		{"wave missing", func(p *GoAheadGrant) { p.Scope = "wave"; p.Factory = ""; p.Job = 0; p.Wave = 1 }},
		{"milestone missing", func(p *GoAheadGrant) { p.Scope = "milestone"; p.Factory = ""; p.Job = 0; p.MilestoneID = "missing" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolated, _, _ := testStore(t)
			stream := createStream(t, isolated, "Bounds", workspace.Ref{Kind: workspace.FactoryJobKind, ID: "4", Factory: "f"})
			p := good
			p.WorkstreamID = stream.ID
			tc.change(&p)
			_, c, err := isolated.GrantGoAhead(ctx, p)
			if !errors.Is(err, ErrInvalid) || len(c.Parts) != 0 {
				t.Fatalf("grant: %+v %v", c, err)
			}
			all, err := isolated.GoAheads(ctx, stream.ID, true)
			if err != nil || len(all) != 0 {
				t.Fatalf("stored invalid grant: %+v %v", all, err)
			}
		})
	}
	good.CapUSD = 10000
	good.EndsAt = now.Add(90 * 24 * time.Hour)
	if _, _, err := s.GrantGoAhead(ctx, good); err != nil {
		t.Fatalf("inclusive limits: %v", err)
	}
}

// Part A (e): MarkDone ends every scope together, is idempotent, and preserves
// prior revocations. Expiry is derived by reads without mutating storage.
func TestPartAMarkDoneEndsGoAheadsAndExpiry(t *testing.T) {
	s, _, now := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "GoAheads", workspace.Ref{Kind: workspace.FactoryJobKind, ID: "4", Factory: "f"})
	plan, _, err := s.SetPlan(ctx, w.ID, 0, []Milestone{{ID: "ship", Title: "Ship"}}, []Wave{{N: 1}}, Attribution{})
	if err != nil || plan.Version != 1 {
		t.Fatal(err)
	}
	for _, p := range []GoAheadGrant{
		{Scope: "job", Factory: "f", Job: 4}, {Scope: "wave", Wave: 1}, {Scope: "milestone", MilestoneID: "ship"},
	} {
		p.WorkstreamID = w.ID
		p.CapUSD = 20
		p.EndsAt = now.Add(time.Hour)
		if _, _, err := s.GrantGoAhead(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := s.GoAheads(ctx, w.ID, true)
	if _, _, err := s.RevokeGoAhead(ctx, all[0].ID); err != nil {
		t.Fatal(err)
	}
	_, c, err := s.MarkDone(ctx, w.ID, "Shipped")
	if err != nil || len(c.Parts) != 2 {
		t.Fatalf("done: %+v %v", c, err)
	}
	active, _ := s.GoAheads(ctx, w.ID, false)
	if len(active) != 0 {
		t.Fatalf("active: %+v", active)
	}
	all, _ = s.GoAheads(ctx, w.ID, true)
	ended := 0
	for _, g := range all {
		if g.State != "ended" || g.EndedAt.IsZero() {
			t.Fatalf("not ended: %+v", g)
		}
		if g.EndReason == "workstream_done" {
			ended++
		}
	}
	if ended != 2 {
		t.Fatalf("ended %d active scopes", ended)
	}
	_, c, err = s.MarkDone(ctx, w.ID, "Changed")
	if err != nil || len(c.Parts) != 0 {
		t.Fatalf("repeated done: %+v %v", c, err)
	}
	if _, _, err := s.GrantGoAhead(ctx, GoAheadGrant{WorkstreamID: w.ID, Scope: "wave", Wave: 1, CapUSD: 2, EndsAt: now.Add(time.Hour)}); !errors.Is(err, ErrConflict) {
		t.Fatalf("grant on done: %v", err)
	}
	fresh := createStream(t, s, "Expiry", workspace.Ref{Kind: workspace.FactoryJobKind, ID: "9", Factory: "f"})
	g, _, err := s.GrantGoAhead(ctx, GoAheadGrant{WorkstreamID: fresh.ID, Scope: "job", Factory: "f", Job: 9, CapUSD: 2, EndsAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Hour)
	all, err = s.GoAheads(ctx, fresh.ID, true)
	if err != nil || len(all) != 1 || all[0].EndReason != "expired" || !all[0].EndedAt.Equal(g.EndsAt) {
		t.Fatalf("expiry: %+v %v", all, err)
	}
	active, _ = s.GoAheads(ctx, fresh.ID, false)
	if len(active) != 0 {
		t.Fatal("expired shown active")
	}
	g, c, err = s.RevokeGoAhead(ctx, g.ID)
	if err != nil || g.EndReason != "expired" || len(c.Parts) != 0 {
		t.Fatalf("expired revoke: %+v %+v %v", g, c, err)
	}
	if _, _, err := s.MarkDone(ctx, fresh.ID, ""); err != nil {
		t.Fatal(err)
	}
	all, err = s.GoAheads(ctx, fresh.ID, true)
	if err != nil || len(all) != 1 || all[0].EndReason != "expired" {
		t.Fatalf("done changed expiry: %+v %v", all, err)
	}
}

// Part A (j): unknown readings stay absent and measured zero stays present.
func TestPartANumbersUnknownAndMeasuredZero(t *testing.T) {
	jobs := []Job{{Factory: "f", Number: 1}, {Factory: "f", Number: 2}}
	zero := 0.0
	no := false
	yes := true
	n := CountNumbers(jobs, nil, &zero, 3)
	if n.Total != 2 || n.Merged != nil || n.NeedsYou != nil || n.SpendUSD != nil {
		t.Fatalf("unknown: %+v", n)
	}
	facts := map[Job]JobFacts{jobs[0]: {State: "backlog", Merged: &no, CostUSD: &zero}, jobs[1]: {State: "backlog", Merged: &no, CostUSD: &zero}}
	n = CountNumbers(jobs, facts, &zero, 0)
	if n.Merged == nil || *n.Merged != 0 || n.NeedsYou == nil || *n.NeedsYou != 0 || n.SpendUSD == nil || *n.SpendUSD != 0 {
		t.Fatalf("zero: %+v", n)
	}
	cost := 2.5
	agent := 1.0
	facts[jobs[0]] = JobFacts{State: "needs_you", Merged: &yes, CostUSD: &cost}
	n = CountNumbers(jobs, facts, &agent, 3)
	if *n.Merged != 1 || *n.NeedsYou != 4 || *n.SpendUSD != 3.5 {
		t.Fatalf("numbers: %+v", n)
	}
	n = CountNumbers(jobs, facts, nil, 0)
	if n.SpendUSD != nil || n.Merged == nil || n.NeedsYou == nil {
		t.Fatalf("unknown agent: %+v", n)
	}
}

func TestPartANumbersWithPartiallyUnknownFacts(t *testing.T) {
	jobs := []Job{{Factory: "f", Number: 1}, {Factory: "f", Number: 2}}
	zero := 0.0
	no := false
	known := JobFacts{State: "done", Merged: &no, CostUSD: &zero}
	for _, facts := range []map[Job]JobFacts{
		{jobs[0]: known},
		{jobs[0]: known, jobs[1]: {State: "unknown", Merged: &no, CostUSD: &zero}},
	} {
		n := CountNumbers(jobs, facts, &zero, 0)
		if n.Total != 2 || n.Merged != nil || n.NeedsYou != nil || n.SpendUSD != nil {
			t.Fatalf("partial unknown: %+v", n)
		}
	}
	n := CountNumbers(jobs, map[Job]JobFacts{jobs[0]: known, jobs[1]: {State: "done", Merged: &no}}, &zero, 0)
	if n.Merged == nil || n.NeedsYou == nil || n.SpendUSD != nil {
		t.Fatalf("unknown cost: %+v", n)
	}
	n = CountNumbers(nil, nil, &zero, 2)
	if n.Total != 0 || n.Merged == nil || *n.Merged != 0 || n.NeedsYou == nil || *n.NeedsYou != 2 || n.SpendUSD == nil || *n.SpendUSD != 0 {
		t.Fatalf("no jobs: %+v", n)
	}
}
