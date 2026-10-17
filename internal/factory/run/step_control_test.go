package run

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestStepRerunExecutesTailAndKeepsSpend(t *testing.T) {
	for _, target := range []string{"plan", "write"} {
		t.Run(target, func(t *testing.T) {
			calls := make(chan Job, 4)
			exec := ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
				calls <- job
				return factory.StageResult{Done: true, Output: job.Stage.Name, Spent: .5}, nil
			})
			g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: exec}, nil)
			id := g.add("rerun", chat("plan"), chat("write"))
			clock := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
			err := g.st.Update(id, func(it *factory.Item) error {
				it.State = factory.StateRunning
				it.Stream = &factory.Stream{Cur: 1, Spent: 1.25, Started: clock, Phases: []factory.Phase{
					{Name: "plan", Kind: factory.StageChat, State: factory.PhaseDone, Result: &factory.StageResult{Done: true, Output: "previous plan", Notes: []string{"keep this note"}}},
					{Name: "write", Kind: factory.StageChat, State: factory.PhaseFailed},
				}}
				if err := StepControl(it, target, true, clock); err != nil {
					return err
				}
				// A pulled worker revives any begun stream through Hold(false).
				it.State, it.Stream.Paused = factory.StateRunning, true
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := g.r.Hold(id, false); err != nil {
				t.Fatal(err)
			}
			deadline := time.NewTimer(5 * time.Second)
			defer deadline.Stop()
			for {
				select {
				case event := <-g.events:
					if event.Kind != EventLanded {
						continue
					}
					it, err := g.st.Get(id)
					if err != nil {
						t.Fatal(err)
					}
					wantCalls, wantSpend := 1, 1.75
					if target == "plan" {
						wantCalls, wantSpend = 2, 2.25
					}
					if len(calls) != wantCalls || it.Stream.Spent != wantSpend || it.State != factory.StateLanded {
						t.Fatalf("rerun %s: calls=%d spend=%f state=%s", target, len(calls), it.Stream.Spent, it.State)
					}
					first := <-calls
					if first.Stage.Name != target || (target == "write" && (len(first.Prior) == 0)) {
						t.Fatalf("wrong starting step: %+v", first)
					}
					return
				case <-deadline.C:
					t.Fatal("rerun did not land")
				}
			}
		})
	}
}

// ok is a passing sheet row with its evidence.
var ok = factory.Claim{Text: "the tests pass", OK: true, Evidence: "go test ok", Medium: "test"}

// rerunItem is a recorded run of stages, every phase done unless pending names
// it, with each phase's result claiming what claims says for it, in stage
// order, onto the sheets the way foldResult puts them there.
func rerunItem(stages []factory.Stage, pending map[string]bool, claims map[string][]factory.Claim) *factory.Item {
	it := &factory.Item{State: factory.StateNeedsYou, Stages: stages, Stream: &factory.Stream{Spent: 2, Started: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)}}
	for _, st := range stages {
		ph := factory.Phase{Name: st.Name, Kind: st.Kind, State: factory.PhaseDone, Round: 1}
		if pending[st.Name] {
			ph.State, ph.Round = factory.PhasePending, 0
		}
		if cl := claims[st.Name]; cl != nil {
			ph.Result = &factory.StageResult{Done: true, Claims: cl}
			for _, c := range cl {
				switch {
				case c.Medium == "policy":
					it.Policy = mergeClaim(it.Policy, c)
				case makesSheet(st):
					it.Proof = mergeClaim(it.Proof, c)
				}
			}
		}
		it.Stream.Phases = append(it.Stream.Phases, ph)
	}
	it.Stream.Cur = len(it.Stream.Phases) - 1
	return it
}

func sheetTexts(cl []factory.Claim) []string {
	var out []string
	for _, c := range cl {
		out = append(out, c.Text)
	}
	return out
}

// A rerun that rewinds nothing proof ran keeps the sheet that proof made: the
// proof step stays done and is not run again, so its claims are still what the
// approve card, the pull request and landing read. A rerun that rewinds a
// proof that ran clears it, because that proof runs again and writes it anew.
// Proof is whatever factory.IsProofStage says — the step named proof, a prove*
// step, a step that runs until proven under any name — read from the phase's
// own name or the configured stage it names.
func TestRerunAfterProofKeepsTheSheet(t *testing.T) {
	policy := factory.Claim{Text: "no secrets in the diff", OK: true, Evidence: "scan clean", Medium: "policy"}
	cases := []struct {
		name   string
		stages []factory.Stage
		proof  string
	}{
		{"named proof", []factory.Stage{chat("plan"), chat("write"), chat("proof"), approveStep()}, "proof"},
		{"prove step", []factory.Stage{chat("plan"), chat("write"), chat("prove-it"), approveStep()}, "prove-it"},
		{"until proven", []factory.Stage{chat("plan"), chat("write"), {Name: "verify", Kind: factory.StageChat, Ask: "show it works", Until: factory.UntilProven}, approveStep()}, "verify"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			claims := map[string][]factory.Claim{c.proof: {ok, policy}}
			it := rerunItem(c.stages, nil, claims)
			if err := StepControl(it, factory.ApproveName, true, time.Now()); err != nil {
				t.Fatal(err)
			}
			if len(it.Proof) != 1 || len(it.Policy) != 1 {
				t.Fatalf("rerun of approve lost the sheet: proof=%v policy=%v", it.Proof, it.Policy)
			}
			if got := it.Stream.Phases[2]; got.State != factory.PhaseDone || got.Result == nil {
				t.Fatalf("proof phase changed: %+v", got)
			}
			if it.Stream.Spent != 2 || it.Stream.Started.IsZero() || it.Stream.Phases[3].State != factory.PhaseRunning {
				t.Fatalf("rerun lost spend or its begun marker: %+v", it.Stream)
			}

			for _, from := range []string{c.proof, "write"} {
				it := rerunItem(c.stages, nil, claims)
				if err := StepControl(it, from, true, time.Now()); err != nil {
					t.Fatal(err)
				}
				if it.Proof != nil || it.Policy != nil {
					t.Fatalf("rerun from %s kept a sheet its proof will rewrite: proof=%v policy=%v", from, it.Proof, it.Policy)
				}
			}
		})
	}
}

// A phase whose configured stage is gone still reads as proof by its own name,
// so an old run's prove step rewound by a rerun clears the sheet it made.
func TestRerunFromAHistoricalProvePhaseClearsTheSheet(t *testing.T) {
	it := rerunItem([]factory.Stage{chat("plan"), chat("prove-again"), approveStep()}, nil, map[string][]factory.Claim{"prove-again": {ok}})
	it.Stages = []factory.Stage{chat("plan"), approveStep()}
	if err := StepControl(it, "prove-again", true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if it.Proof != nil {
		t.Fatalf("historical prove phase rewound but its sheet stayed: %v", it.Proof)
	}
}

// A proof step that has not run yet has written nothing, so rewinding past it
// does not take away the sheet an earlier proof made.
func TestRerunPastAnUnrunProofKeepsTheEarlierSheet(t *testing.T) {
	stages := []factory.Stage{chat("write"), chat("proof"), approveStep(), chat("prove-again")}
	it := rerunItem(stages, map[string]bool{"prove-again": true}, map[string][]factory.Claim{"proof": {ok}})
	if err := StepControl(it, factory.ApproveName, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(it.Proof) != 1 {
		t.Fatalf("unrun proof in the rewound range cleared the sheet: %v", it.Proof)
	}
}

// Checks and posts after proof write sheet rows too, and any step can write a
// policy row. A rerun from such a step drops exactly the rows its rewound
// steps wrote, unless a step that stays done also wrote them, so the sheet
// never shows a row no finished step stands behind; the rerun writes them anew.
func TestRerunAfterProofDropsTheRewoundStepsRows(t *testing.T) {
	tidy := factory.Stage{Name: "tidy", Kind: factory.StagePost, Ask: "neaten it", Until: factory.UntilDone}
	shared := factory.Claim{Text: "no secrets in the diff", OK: true, Evidence: "scan clean", Medium: "policy"}
	claims := map[string][]factory.Claim{
		"proof": {ok, shared},
		"tidy": {
			{Text: "the page is neat", OK: true, Evidence: "screenshot", Medium: "screenshot"},
			{Text: "no new dependencies", OK: true, Evidence: "go.mod unchanged", Medium: "policy"},
			shared,
		},
	}
	it := rerunItem([]factory.Stage{chat("write"), chat("proof"), tidy, approveStep()}, nil, claims)
	if err := StepControl(it, "tidy", true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := sheetTexts(it.Proof); len(got) != 1 || got[0] != ok.Text {
		t.Fatalf("proof sheet after rerun of tidy = %v", got)
	}
	if got := sheetTexts(it.Policy); len(got) != 1 || got[0] != shared.Text {
		t.Fatalf("policy sheet after rerun of tidy = %v", got)
	}
}

// sheetRow is the row a sheet keeps for text, read the way mergeClaim reads it.
func sheetRow(t *testing.T, sheet []factory.Claim, text string) factory.Claim {
	t.Helper()
	var found []factory.Claim
	for _, c := range sheet {
		if claimKey(c.Text) == claimKey(text) {
			found = append(found, c)
		}
	}
	if len(found) != 1 {
		t.Fatalf("sheet has %d rows for %q: %+v", len(found), text, sheet)
	}
	return found[0]
}

// When a rewound step overwrote a row a step that stays done recorded, the
// rerun puts back what the done step recorded, its result and its evidence,
// and never keeps the rewound step's version under the done step's name. The
// unsafe direction is a rewound pass standing over a failure proof recorded:
// nothing claims that row again, so the job would land on a pass no finished
// step stands behind. A step after approve is rewound by a rerun of approve
// and must not leak its row onto the approve card either. The held text may
// differ from the rewound one in case and punctuation; they are one row.
func TestRerunAfterProofRestoresARowARewoundStepOverwrote(t *testing.T) {
	tidy := factory.Stage{Name: "tidy", Kind: factory.StagePost, Ask: "neaten it", Until: factory.UntilDone}
	pass := factory.Claim{Text: "no secrets in the diff", OK: true, Evidence: "scan clean", Medium: "policy"}
	fail := factory.Claim{Text: "No secrets in the diff.", OK: false, Evidence: "key found", Medium: "policy"}
	cases := []struct {
		name         string
		stages       []factory.Stage
		held, rewind factory.Claim
		from         string
	}{
		{"rewound failure over a held pass", []factory.Stage{chat("write"), chat("proof"), tidy, approveStep()}, pass, fail, "tidy"},
		{"rewound pass over a held failure", []factory.Stage{chat("write"), chat("proof"), tidy, approveStep()}, fail, pass, "tidy"},
		{"a step after approve", []factory.Stage{chat("write"), chat("proof"), approveStep(), tidy}, pass, fail, factory.ApproveName},
		{"a step after approve over a held failure", []factory.Stage{chat("write"), chat("proof"), approveStep(), tidy}, fail, pass, factory.ApproveName},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			it := rerunItem(c.stages, nil, map[string][]factory.Claim{"proof": {ok, c.held}, "tidy": {c.rewind}})
			if got := sheetRow(t, it.Policy, c.held.Text); got != c.rewind {
				t.Fatalf("setup: the rewound step did not overwrite the row: %+v", got)
			}
			if err := StepControl(it, c.from, true, time.Now()); err != nil {
				t.Fatal(err)
			}
			if got := sheetRow(t, it.Policy, c.held.Text); got != c.held {
				t.Fatalf("rerun from %s kept %+v, want proof's %+v", c.from, got, c.held)
			}
			if got := sheetRow(t, it.Proof, ok.Text); got != ok {
				t.Fatalf("proof row changed: %+v", got)
			}
			if clean(*it) != c.held.OK {
				t.Fatalf("clean = %v after restoring %+v", clean(*it), c.held)
			}
		})
	}
}

// A plan or write step's claims go to the log, never the sheet, so a rewound
// one that says the same words as a proof row neither takes that row away nor
// restores it. And the two sheets are kept apart: a done step's Proof row of
// some words does not hold up a Policy row of the same words a rewound step
// wrote, nor the other way round.
func TestRerunAfterProofReadsEachSheetsOwnSteps(t *testing.T) {
	tidy := factory.Stage{Name: "tidy", Kind: factory.StagePost, Ask: "neaten it", Until: factory.UntilDone}
	said := factory.Claim{Text: "The tests pass!", OK: false, Evidence: "I think they broke", Medium: "test"}
	t.Run("a done chat claim does not mask proof", func(t *testing.T) {
		broke := factory.Claim{Text: ok.Text, OK: false, Evidence: "exit 1", Medium: "test"}
		stages := []factory.Stage{chat("write"), chat("proof"), chat("explain"), tidy, approveStep()}
		it := rerunItem(stages, nil, map[string][]factory.Claim{"proof": {ok}, "explain": {said}, "tidy": {broke}})
		if err := StepControl(it, "tidy", true, time.Now()); err != nil {
			t.Fatal(err)
		}
		if got := sheetRow(t, it.Proof, ok.Text); got != ok {
			t.Fatalf("rerun of tidy restored %+v, want proof's %+v", got, ok)
		}
	})
	t.Run("a rewound chat claim does not hold a row up", func(t *testing.T) {
		// The chat step's words match a row only a worker's report wrote. The
		// chat step's claim went to the log, so it cannot account for the row,
		// and the rerun refuses rather than guess whose row it is.
		refusedUnchanged(t, func() *factory.Item {
			stages := []factory.Stage{chat("write"), chat("proof"), approveStep(), chat("explain")}
			it := rerunItem(stages, nil, map[string][]factory.Claim{"explain": {said}})
			it.Proof = []factory.Claim{ok}
			return it
		}, factory.ApproveName)
	})
	t.Run("a proof row does not protect a policy row", func(t *testing.T) {
		rule := factory.Claim{Text: ok.Text, OK: false, Evidence: "policy says no", Medium: "policy"}
		it := rerunItem([]factory.Stage{chat("write"), chat("proof"), tidy, approveStep()}, nil, map[string][]factory.Claim{"proof": {ok}, "tidy": {rule}})
		if err := StepControl(it, "tidy", true, time.Now()); err != nil {
			t.Fatal(err)
		}
		if it.Policy != nil {
			t.Fatalf("proof's row of the same words kept the rewound policy row: %v", it.Policy)
		}
		if got := sheetRow(t, it.Proof, ok.Text); got != ok {
			t.Fatalf("proof row changed: %+v", got)
		}
	})
	t.Run("a policy row does not protect a proof row", func(t *testing.T) {
		rule := factory.Claim{Text: ok.Text, OK: true, Evidence: "policy holds", Medium: "policy"}
		check := factory.Claim{Text: ok.Text, OK: false, Evidence: "exit 1", Medium: "test"}
		it := rerunItem([]factory.Stage{chat("write"), chat("proof"), tidy, approveStep()}, nil, map[string][]factory.Claim{"proof": {rule}, "tidy": {check}})
		if err := StepControl(it, "tidy", true, time.Now()); err != nil {
			t.Fatal(err)
		}
		if it.Proof != nil {
			t.Fatalf("proof's policy row of the same words kept the rewound check row: %v", it.Proof)
		}
		if got := sheetRow(t, it.Policy, ok.Text); got != rule {
			t.Fatalf("policy row changed: %+v", got)
		}
	})
}

// refusedUnchanged runs a rerun of from on the item build makes and demands
// the refusal that asks for a rerun from proof, with EVERY field of the item
// as it was: a refused rerun resets no step, clears no failure, moves no lease
// and touches neither sheet.
func refusedUnchanged(t *testing.T, build func() *factory.Item, from string) {
	t.Helper()
	it, want := build(), build()
	err := StepControl(it, from, true, time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC))
	var refusal StepControlRefusal
	if !errors.As(err, &refusal) || !strings.Contains(err.Error(), "rerun from proof") {
		t.Fatalf("rerun from %s = %v, want the refusal that asks for a rerun from proof", from, err)
	}
	if !reflect.DeepEqual(it, want) {
		t.Fatalf("a refused rerun changed the item:\n got %+v\nwant %+v", it, want)
	}
}

// A phase only keeps its last round. An earlier round of a rewound step wrote
// a pass over the failure proof recorded, and the step's last round claims
// nothing, so no step result can say whose row that is. The rerun must not
// leave the pass on the approve card, and it must not quietly run proof again
// either: it refuses, changes nothing, and says to rerun from proof.
func TestRerunAfterProofDoesNotKeepAnEarlierRoundPassFromARewoundStep(t *testing.T) {
	bad := factory.Claim{Text: "no secrets in the diff", OK: false, Evidence: "proof found a key", Medium: "policy"}
	good := factory.Claim{Text: "No secrets in the diff.", OK: true, Evidence: "tidy round one passed", Medium: "policy"}
	tidy := factory.Stage{Name: "tidy", Kind: factory.StagePost, Ask: "tidy", Until: factory.UntilDone, On: true}
	refusedUnchanged(t, func() *factory.Item {
		it := rerunItem([]factory.Stage{chat("write"), chat("proof"), approveStep(), tidy}, nil, map[string][]factory.Claim{"proof": {ok, bad}, "tidy": {good}})
		// Round two did not repeat the claim; foldResult leaves round one's
		// aggregate row in Policy while Phase.Result now holds round two only.
		it.Stream.Phases[3].Round = 2
		it.Stream.Phases[3].Result = &factory.StageResult{Done: true, Output: "round two done"}
		return it
	}, factory.ApproveName)
}

// A worker's report can put a row on a sheet with no step result behind it.
// When the rerun rewinds a step that could have sent it, the row is not
// provably proof's, so the rerun refuses rather than keep or drop it.
func TestRerunAfterProofRefusesAReportRowARewoundStepMayHaveSent(t *testing.T) {
	tidy := factory.Stage{Name: "tidy", Kind: factory.StagePost, Ask: "tidy", Until: factory.UntilDone}
	reported := factory.Claim{Text: "the page loads", OK: true, Evidence: "worker said so", Medium: "screenshot"}
	for _, stages := range [][]factory.Stage{
		{chat("write"), chat("proof"), tidy, approveStep()},
		{chat("write"), chat("proof"), approveStep(), tidy},
	} {
		from := stages[2].Name
		t.Run(from, func(t *testing.T) {
			refusedUnchanged(t, func() *factory.Item {
				it := rerunItem(stages, nil, map[string][]factory.Claim{"proof": {ok}})
				it.Proof = append(it.Proof, reported)
				it.Failure = &factory.Failure{Step: "tidy", Code: "stopped", Detail: "Stopped by you"}
				return it
			}, from)
		})
	}
}

// A rerun of a gate after proof rewinds nothing that could write a row, so the
// whole sheet stays as it is — a row only a worker's report wrote, and a row
// only an earlier round of proof wrote, included — exactly as it stood before.
func TestRerunOfAGateAfterProofKeepsTheWholeSheet(t *testing.T) {
	early := factory.Claim{Text: "the build is green", OK: true, Evidence: "round one", Medium: "test"}
	reported := factory.Claim{Text: "the page loads", OK: true, Evidence: "worker said so", Medium: "screenshot"}
	policy := factory.Claim{Text: "no secrets in the diff", OK: false, Evidence: "key found", Medium: "policy"}
	stages := []factory.Stage{chat("write"), chat("proof"), approveStep(), chat("prove-again")}
	it := rerunItem(stages, map[string]bool{"prove-again": true}, map[string][]factory.Claim{"proof": {ok, policy}})
	it.Stream.Phases[1].Round = 2
	it.Proof = append(it.Proof, early, reported)
	proof, pol := append([]factory.Claim(nil), it.Proof...), append([]factory.Claim(nil), it.Policy...)
	if err := StepControl(it, factory.ApproveName, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(it.Proof, proof) || !reflect.DeepEqual(it.Policy, pol) {
		t.Fatalf("rerun of approve changed the sheet: proof=%v policy=%v", it.Proof, it.Policy)
	}
}

// Before any proof has run there is no sheet proof stands behind, so a rerun
// clears both sheets the way it always did: a done check's row before the
// rerun point does not newly survive a rerun that once cleared it.
func TestRerunBeforeAnyProofClearsTheSheet(t *testing.T) {
	lint := factory.Stage{Name: "lint", Kind: factory.StageCheck, Ask: "lint"}
	vet := factory.Stage{Name: "vet", Kind: factory.StageCheck, Ask: "vet"}
	linted := factory.Claim{Text: "lint is clean", OK: true, Evidence: "0 issues", Medium: "test"}
	rule := factory.Claim{Text: "no secrets in the diff", OK: true, Evidence: "scan clean", Medium: "policy"}
	stages := []factory.Stage{chat("write"), lint, vet, chat("proof")}
	it := rerunItem(stages, map[string]bool{"proof": true}, map[string][]factory.Claim{"lint": {linted, rule}, "vet": {ok}})
	if err := StepControl(it, "vet", true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if it.Proof != nil || it.Policy != nil {
		t.Fatalf("rerun before proof kept rows: proof=%v policy=%v", it.Proof, it.Policy)
	}
}

// A worker's ordinary run reaches the job as reports that carry each step's
// result and, beside it, the rows its own sheet moved. Those rows are the
// results' rows again, so the rebuild still accounts for every one, and a
// rerun of a step after proof is not refused for the way a worker reports.
func TestRerunAfterAWorkersReportedRunRestoresTheSheet(t *testing.T) {
	tidy := factory.Stage{Name: "tidy", Kind: factory.StagePost, Ask: "tidy", Until: factory.UntilDone}
	held := factory.Claim{Text: "no secrets in the diff", OK: false, Evidence: "key found", Medium: "policy"}
	over := factory.Claim{Text: "No secrets in the diff.", OK: true, Evidence: "tidy says clean", Medium: "policy"}
	neat := factory.Claim{Text: "the page is neat", OK: true, Evidence: "screenshot", Medium: "screenshot"}
	stages := []factory.Stage{chat("write"), chat("proof"), tidy, approveStep()}
	it := rerunItem(stages, map[string]bool{"write": true, "proof": true, "tidy": true, factory.ApproveName: true}, nil)
	it.Proof, it.Policy = nil, nil
	now := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	for _, step := range []struct {
		name   string
		claims []factory.Claim
		moved  []factory.Claim
	}{
		{"write", nil, nil},
		{"proof", []factory.Claim{ok, held}, []factory.Claim{ok, held}},
		{"tidy", []factory.Claim{neat, over}, []factory.Claim{neat, over}},
	} {
		res := factory.StageResult{Done: true, Claims: step.claims}
		report := factory.RunReport{Stages: []factory.StageReport{{Name: step.name, State: factory.PhaseDone, Round: 1, Result: &res}}, Claims: step.moved}
		if err := ApplyReport(it, report, now); err != nil {
			t.Fatal(err)
		}
	}
	if got := sheetRow(t, it.Policy, held.Text); got != over {
		t.Fatalf("setup: tidy did not overwrite proof's row: %+v", got)
	}
	if err := StepControl(it, "tidy", true, now); err != nil {
		t.Fatal(err)
	}
	if got := sheetTexts(it.Proof); len(got) != 1 || got[0] != ok.Text {
		t.Fatalf("proof sheet after rerun of tidy = %v", got)
	}
	if got := sheetRow(t, it.Policy, held.Text); got != held {
		t.Fatalf("rerun of tidy kept %+v, want proof's %+v", got, held)
	}
}

// Worker pipeline reports currently can rewrite a configured kind while the
// server keeps the recorded phase kind. That does not turn earlier work into
// a claim-free gate for a partial rerun.
func TestRerunAfterProofDoesNotTrustAConfiguredGateOverRecordedWork(t *testing.T) {
	bad := factory.Claim{Text: "no secrets in the diff", OK: false, Evidence: "proof found a key", Medium: "policy"}
	good := factory.Claim{Text: "No secrets in the diff.", OK: true, Evidence: "tidy round one passed", Medium: "policy"}
	tidy := factory.Stage{Name: "tidy", Kind: factory.StagePost, Ask: "tidy", Until: factory.UntilDone, On: true}
	it := rerunItem([]factory.Stage{chat("write"), chat("proof"), approveStep(), tidy}, nil, map[string][]factory.Claim{"proof": {ok, bad}, "tidy": {good}})
	it.Stream.Phases[3].Round = 2
	it.Stream.Phases[3].Result = &factory.StageResult{Done: true, Output: "round two done"}
	changed := factory.CopyStages(it.Stages)
	changed[3].Kind = factory.StageGate
	report := factory.RunReport{Pipeline: &factory.PipelineChange{Stages: changed, Phases: it.Stream.Phases}}
	if err := ApplyReport(it, report, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := StepControl(it, factory.ApproveName, true, time.Now()); err != nil {
		return
	}
	if got := sheetRow(t, it.Policy, bad.Text); got.OK {
		t.Fatalf("configured gate hid recorded work and left its stale pass: %+v", got)
	}
}
