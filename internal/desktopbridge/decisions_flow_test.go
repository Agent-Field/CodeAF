package desktopbridge

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/council"
	"github.com/Agent-Field/codeaf/internal/decide"
	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// THE LEDGER FOLLOWED THROUGH THE DOORS THE DESKTOP USES. The decide package
// pins the rules; these prove the bridge shows what the rules did — the count
// on the way to 18 of 20, the mode after, and the one kind an overturn drops —
// and that a restart changes none of it. The ledger is a real file and the
// undo is a fake the test can inspect.

// answer records one person's answer on a kind through the engine's own door.
func (r *decisionRig) answer(place, class string, agreed bool) {
	r.t.Helper()
	chosen := "y"
	if !agreed {
		chosen = "n"
	}
	if _, err := r.store(place).RecordProposal(decide.ProposalAnswer{AskKind: "permission", SubjectClass: class, ProposedKey: "y", ChosenKey: chosen}); err != nil {
		r.t.Fatal(err)
	}
}

func TestADecisionLedgerFollowsAPlaceFromLearningToOverturnAndRestart(t *testing.T) {
	rig := newDecisionRig(t)
	place := rig.mk("Release")
	status := func() DecideStatusBody {
		var out DecideStatusBody
		if code := rig.do("GET", "/places/"+place+"/decide-status", nil, &out); code != 200 {
			t.Fatalf("status: %d", code)
		}
		return out
	}

	// Seventeen of nineteen: still learning, and the count says so.
	for i := 0; i < 19; i++ {
		rig.answer(place, "shell-read", i < 17)
	}
	if got := status(); got.Mode != "learning" || got.Learning == nil || got.Learning.Agreed != 17 || got.Learning.Of != decide.RingSize {
		t.Fatalf("17 of 19: %+v", got)
	}
	// The twentieth, an agreement, makes 18 of 20.
	rig.answer(place, "shell-read", true)
	if st, _ := rig.store(place).Mode("permission:shell-read"); st.Mode != decide.ModeDeciding {
		t.Fatalf("18 of 20 did not graduate: %+v", st)
	}

	// A second kind is deciding too, and a decision of the first is on file.
	if err := rig.store(place).SetMode("permission:git", decide.ModeDeciding); err != nil {
		t.Fatal(err)
	}
	id := place + ":q1"
	rig.add(place, id, placesEpoch.Add(-time.Hour))

	var got decide.OverturnResult
	if code := rig.do("POST", "/decisions/"+id+"/overturn", map[string]any{"next": "keep"}, &got); code != 200 || got.Mode != decide.ModeLearning {
		t.Fatalf("overturn: %d %+v", code, got)
	}
	shell, _ := rig.store(place).Mode("permission:shell-read")
	git, _ := rig.store(place).Mode("permission:git")
	if shell.Mode != decide.ModeLearning || len(shell.Recent) != 0 || git.Mode != decide.ModeDeciding {
		t.Fatalf("overturn spread past its kind: shell=%+v git=%+v", shell, git)
	}

	// A restart: a fresh store over the same folder sees the same thing.
	reopened, err := decide.Open(rig.dir, place, func() time.Time { return rig.clock })
	if err != nil {
		t.Fatal(err)
	}
	again, _ := reopened.Mode("permission:shell-read")
	kept, _ := reopened.Mode("permission:git")
	rows, _ := reopened.List()
	if again.Mode != decide.ModeLearning || kept.Mode != decide.ModeDeciding || len(rows) != 1 || rows[0].OverturnedAt == nil {
		t.Fatalf("after restart: %+v %+v %+v", again, kept, rows)
	}
}

// A CAPPED COUNCIL IS THE PERSON'S, NOT THE LEDGER'S. The runner hands a
// capped discussion up the graph; it never writes a decision for a place.
func TestACouncilThatHitsItsTurnCapEscalatesAndDecidesNothing(t *testing.T) {
	rig := newDecisionRig(t)
	root := rig.mk("Company")
	a, b := rig.mk("Marketing", root), rig.mk("Software", root)

	store, err := council.Open(council.Options{
		Path:        filepath.Join(t.TempDir(), "councils.json"),
		SessionsDir: filepath.Join(t.TempDir(), "sessions"),
		Places:      rig.p.Store,
		Now:         func() time.Time { return rig.clock },
	})
	if err != nil {
		t.Fatal(err)
	}
	asker := func(context.Context, council.Request) (council.Answer, error) {
		return council.Answer{Text: "not yet", CostUSD: 0.01}, nil
	}
	sink := &escalationSink{}
	run, err := council.NewRunner(store, rig.p.Store, asker, sink)
	if err != nil {
		t.Fatal(err)
	}
	c, err := run.Start(context.Background(), a, b, "when do we promise the fix?")
	if err != nil {
		t.Fatal(err)
	}
	if c.State != council.StateEscalated || c.Turns != council.TurnCap {
		t.Fatalf("council = %+v", c)
	}
	if len(sink.got) != 1 || sink.got[0].ToPlace != root || sink.got[0].Reason != council.ReasonTurns {
		t.Fatalf("escalation = %+v", sink.got)
	}
	for _, p := range []string{root, a, b} {
		if rows, _ := rig.store(p).List(); len(rows) != 0 {
			t.Fatalf("a capped council wrote a decision for %s: %+v", p, rows)
		}
	}
}

type escalationSink struct{ got []council.Escalation }

func (s *escalationSink) Say(string, string, string) error { return nil }
func (s *escalationSink) Escalate(_ context.Context, e council.Escalation) error {
	s.got = append(s.got, e)
	return nil
}

// A superseded line is gone from what a place knows, the way the ledger's
// proposals read it. The strike is the store's; this proves the bridge's
// snapshot, which the gate scores from, agrees.
func TestASupersededKnowsLineLeavesTheLiveSet(t *testing.T) {
	rig := newDecisionRig(t)
	place := rig.mk("Release")
	old, _, err := rig.p.Store.AddLine(placegraph.Line{PlaceID: place, Text: "go test is fine", Source: placegraph.LineSource{Kind: placegraph.LineLearned}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ask, _, err := rig.p.Store.WriteKnowledge(place, "", "ask before any command", old.ID); err != nil || ask != nil {
		t.Fatalf("supersede: %+v %v", ask, err)
	}
	snap, err := rig.p.Store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var struck bool
	for _, l := range snap.Knowledge(place) {
		if l.ID == old.ID {
			struck = l.ReplacedBy != ""
		}
	}
	if !struck {
		t.Fatal("the older line was not replaced")
	}
}
