package run

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE PROOF SHEET VOUCHES ONLY FOR WHAT IT SAW. These tests pin the engine's
// answer to a step edit that would put new work where the sheet cannot have
// seen it: once proof has started the stages are frozen, and before it starts
// nothing but a person's own gate may stand after it.

func proofStages() []factory.Stage {
	return []factory.Stage{
		{Name: "plan", Kind: factory.StageChat, Ask: "plan it", Until: "done", On: true},
		{Name: "write", Kind: factory.StageChat, Ask: "write it", Until: "done", On: true},
		{Name: "proof", Kind: factory.StageChat, Ask: "the sheet", Until: factory.UntilProven, On: true},
		{Name: "tidy", Kind: factory.StageChat, Ask: "tidy it", Until: "done", On: false},
	}
}

func phase(name string, state factory.PhaseState) factory.Phase {
	return factory.Phase{Name: name, Kind: factory.StageChat, State: state}
}

// proofItem is an item whose plan and write are done and whose proof is in the given state.
func proofItem(state factory.Item, proof factory.PhaseState, round int) factory.Item {
	state.ID = 1
	state.Kind = factory.KindIssue
	state.Stages = proofStages()
	p := phase("proof", proof)
	p.Round = round
	phases := []factory.Phase{phase("plan", factory.PhaseDone), phase("write", factory.PhaseDone), p}
	if proof == factory.PhasePending {
		phases[1].State = factory.PhaseRunning
	}
	state.Stream = &factory.Stream{Cur: 2, Phases: phases}
	return state
}

func landed() factory.Item {
	return factory.Item{State: factory.StateLanded, Proof: []factory.Claim{{Text: "tests pass", OK: true, Evidence: "exit 0"}}}
}

func afterProof() factory.RunEdit {
	return factory.RunEdit{By: factory.ByYou, Add: []factory.Added{{
		Stage: factory.Stage{Name: "tidy2", Kind: factory.StageChat, Ask: "tidy it", Until: "done", On: true},
		After: "proof",
	}}}
}

// D1: a landed job with its proof done must not take a stage after proof.
func TestEditAfterProofOnLandedJobIsRefusedUnchanged(t *testing.T) {
	it := proofItem(landed(), factory.PhaseDone, 0)
	before := it
	before.Stages = factory.CopyStages(it.Stages)
	streamBefore := *it.Stream
	streamBefore.Phases = append([]factory.Phase(nil), it.Stream.Phases...)
	before.Stream = &streamBefore

	lines, err := EditStepsItem(&it, afterProof(), factory.DefaultRecipe())
	if err == nil {
		t.Fatalf("add-after-proof on a landed job was accepted: lines=%v phases=%+v", lines, it.Stream.Phases)
	}
	var refusal factory.EditRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("refusal is not an EditRefusal: %T %v", err, err)
	}
	if !reflect.DeepEqual(it, before) {
		t.Fatalf("a refused edit changed the item:\nnow  %+v\nwas  %+v", it, before)
	}
	if err := ApprovalRefused(&it, false); err != nil {
		t.Fatalf("the landed job's approval changed: %v", err)
	}
}

// Once proof has started in any way, no edit of any part is taken.
func TestEditsAreRefusedOnceProofHasStarted(t *testing.T) {
	edits := map[string]factory.RunEdit{
		"add after proof": afterProof(),
		"add before proof": {By: factory.ByYou, Add: []factory.Added{{
			Stage: factory.Stage{Name: "lint", Kind: factory.StageChat, Ask: "lint it", Until: "done", On: true}, After: "plan"}}},
		"ask":      {By: factory.ByYou, Ask: map[string]string{"tidy": "tidy a lot"}},
		"thinking": {By: factory.ByYou, Thinking: map[string]string{"tidy": "strong"}},
		"on":       {By: factory.ByYou, On: []string{"tidy"}},
		"skip":     {By: factory.ByYou, Skip: []string{"write"}},
	}
	states := map[string]struct {
		state factory.PhaseState
		round int
		it    factory.Item
	}{
		"running":       {factory.PhaseRunning, 0, factory.Item{State: factory.StateRunning}},
		"waiting":       {factory.PhaseWaiting, 0, factory.Item{State: factory.StateNeedsYou}},
		"done":          {factory.PhaseDone, 0, landed()},
		"failed":        {factory.PhaseFailed, 0, factory.Item{State: factory.StateNeedsYou}},
		"pending round": {factory.PhasePending, 1, factory.Item{State: factory.StateRunning}},
	}
	for sn, s := range states {
		for en, e := range edits {
			t.Run(sn+"/"+en, func(t *testing.T) {
				it := proofItem(s.it, s.state, s.round)
				was := it
				was.Stages = factory.CopyStages(it.Stages)
				lines, err := EditStepsItem(&it, e, factory.DefaultRecipe())
				if err == nil {
					t.Fatalf("accepted: lines=%v", lines)
				}
				if !reflect.DeepEqual(it.Stages, was.Stages) || len(it.Adapted) != 0 || len(it.Stream.Phases) != 3 {
					t.Fatalf("a refused edit changed the item: %+v", it)
				}
			})
		}
	}
}

// Before proof starts, work after proof is refused however it is introduced.
func TestUnverifiedWorkAfterProofIsRefusedBeforeProofStarts(t *testing.T) {
	edits := map[string]factory.RunEdit{
		"add after proof": afterProof(),
		"add last":        {By: factory.ByYou, Add: []factory.Added{{Stage: factory.Stage{Name: "tidy2", Kind: factory.StageChat, Ask: "tidy it", Until: "done", On: true}, After: "tidy"}}},
		"enable":          {By: factory.ByYou, On: []string{"tidy"}},
		"ask":             {By: factory.ByYou, Ask: map[string]string{"tidy": "do more"}},
		"thinking":        {By: factory.ByYou, Thinking: map[string]string{"tidy": "strong"}},
	}
	for name, e := range edits {
		t.Run(name, func(t *testing.T) {
			it := proofItem(factory.Item{State: factory.StateRunning}, factory.PhasePending, 0)
			if _, err := EditStepsItem(&it, e, factory.DefaultRecipe()); err == nil {
				t.Fatalf("accepted; stages=%+v", it.Stages)
			}
			if len(it.Adapted) != 0 || it.Stages[3].On || it.Stages[3].Ask != "tidy it" {
				t.Fatalf("a refused edit changed the item: %+v", it.Stages)
			}
		})
	}
}

// The defaults, and anything before proof, still work while proof is pending.
func TestEditsBeforeProofStillWork(t *testing.T) {
	place := func(after string) factory.RunEdit {
		return factory.RunEdit{By: factory.ByYou, Add: []factory.Added{{
			Stage: factory.Stage{Name: "lint", Kind: factory.StageChat, Ask: "lint it", Until: "done", On: true}, After: after}}}
	}
	for name, e := range map[string]factory.RunEdit{
		"default place": place(""),
		"after write":   place("write"),
		"ask a future":  {By: factory.ByYou, Ask: map[string]string{"proof": "show more"}},
	} {
		t.Run(name, func(t *testing.T) {
			it := proofItem(factory.Item{State: factory.StateRunning}, factory.PhasePending, 0)
			it.Stream.Phases[1].State = factory.PhaseRunning
			lines, err := EditStepsItem(&it, e, factory.DefaultRecipe())
			if err != nil || len(lines) == 0 {
				t.Fatalf("refused: %v %v", lines, err)
			}
		})
	}
}

// A person's gate after proof, added before proof starts, stays: it is a hold,
// not work, and the proof sheet is not vouching for anything past it.
func TestApproveStepAfterProofIsKeptBeforeProofStarts(t *testing.T) {
	it := proofItem(factory.Item{State: factory.StateRunning}, factory.PhasePending, 0)
	e := factory.RunEdit{By: factory.ByYou, Add: []factory.Added{{
		Stage: factory.Stage{Name: factory.ApproveName, Kind: factory.StageGate}, After: "proof"}}}
	lines, err := EditStepsItem(&it, e, factory.DefaultRecipe())
	if err != nil || len(lines) == 0 {
		t.Fatalf("the gate was refused: %v %v", lines, err)
	}
	last := it.Stream.Phases[len(it.Stream.Phases)-1]
	if last.Name != factory.ApproveName || last.State != factory.PhasePending || !strings.Contains(strings.Join(lines, "\n"), "added "+factory.ApproveName) {
		t.Fatalf("the gate is not the last pending step: %+v %v", it.Stream.Phases, lines)
	}
}

// renamedProofItem is proofItem with its proof stage renamed to a word that is
// proof only because it runs until proven.
func renamedProofItem(state factory.Item, proof factory.PhaseState) factory.Item {
	it := proofItem(state, proof, 0)
	it.Stages[2].Name = "evidence"
	it.Stream.Phases[2].Name = "evidence"
	return it
}

func TestEditAfterRenamedProvenStageIsRefused(t *testing.T) {
	it := renamedProofItem(landed(), factory.PhaseDone)
	edit := afterProof()
	edit.Add[0].After = "evidence"
	if _, err := EditStepsItem(&it, edit, factory.DefaultRecipe()); err == nil {
		t.Fatal("a completed until-proven stage accepted new work after its proof")
	}
}

// A renamed proof that has not started still keeps work from standing after it.
func TestRenamedPendingProofRefusesWorkAfterIt(t *testing.T) {
	edits := map[string]factory.RunEdit{
		"add":      {By: factory.ByYou, Add: []factory.Added{{Stage: factory.Stage{Name: "tidy2", Kind: factory.StageChat, Ask: "x", Until: "done", On: true}, After: "evidence"}}},
		"enable":   {By: factory.ByYou, On: []string{"tidy"}},
		"ask":      {By: factory.ByYou, Ask: map[string]string{"tidy": "other"}},
		"thinking": {By: factory.ByYou, Thinking: map[string]string{"tidy": "strong"}},
	}
	for name, e := range edits {
		t.Run(name, func(t *testing.T) {
			it := renamedProofItem(factory.Item{State: factory.StateRunning}, factory.PhasePending)
			if _, err := EditStepsItem(&it, e, factory.DefaultRecipe()); err == nil {
				t.Fatalf("accepted; stages=%+v", it.Stages)
			}
			if len(it.Adapted) != 0 || it.Stages[3].On || it.Stages[3].Ask != "tidy it" {
				t.Fatalf("a refused edit changed the item: %+v", it.Stages)
			}
		})
	}
	t.Run("gate is kept", func(t *testing.T) {
		it := renamedProofItem(factory.Item{State: factory.StateRunning}, factory.PhasePending)
		e := factory.RunEdit{By: factory.ByYou, Add: []factory.Added{{Stage: factory.Stage{Name: factory.ApproveName, Kind: factory.StageGate}, After: "evidence"}}}
		if _, err := EditStepsItem(&it, e, factory.DefaultRecipe()); err != nil {
			t.Fatalf("the gate was refused: %v", err)
		}
	})
}

// Any started proof freezes every edit, however the stage is spelled.
func TestStartedRenamedProofFreezesEdits(t *testing.T) {
	for _, state := range []factory.PhaseState{factory.PhaseRunning, factory.PhaseDone} {
		it := renamedProofItem(factory.Item{State: factory.StateRunning}, state)
		e := factory.RunEdit{By: factory.ByYou, On: []string{"tidy"}}
		_, err := EditStepsItem(&it, e, factory.DefaultRecipe())
		if err == nil || !strings.Contains(err.Error(), factory.ProofFrozenRefusal) {
			t.Fatalf("%s: err=%v", state, err)
		}
		if it.Stages[3].On {
			t.Fatalf("%s: refused edit changed the item", state)
		}
	}
	// Round > 0 alone also counts as started.
	it := renamedProofItem(factory.Item{State: factory.StateRunning}, factory.PhasePending)
	it.Stream.Phases[2].Round = 1
	if _, err := EditStepsItem(&it, factory.RunEdit{By: factory.ByYou, On: []string{"tidy"}}, factory.DefaultRecipe()); err == nil {
		t.Fatal("a proof on a later round accepted an edit")
	}
}

// A send-back's prove* phase, absent from the configured stages, freezes too.
func TestStartedSendBackProveFreezesEdits(t *testing.T) {
	it := proofItem(factory.Item{State: factory.StateRunning}, factory.PhasePending, 0)
	it.Stream.Phases = append(it.Stream.Phases, phase("prove again", factory.PhaseRunning))
	if _, err := EditStepsItem(&it, factory.RunEdit{By: factory.ByYou, On: []string{"tidy"}}, factory.DefaultRecipe()); err == nil {
		t.Fatal("a started prove phase accepted an edit")
	}
}

// A historical phase whose name matches a configured until-proven stage freezes.
func TestHistoricalPhaseOfConfiguredProofFreezes(t *testing.T) {
	it := proofItem(factory.Item{State: factory.StateRunning}, factory.PhasePending, 0)
	it.Stages[1].Name = "Review"
	it.Stages[1].Until = factory.UntilProven
	it.Stream.Phases[1].Name = "review"
	it.Stream.Phases[1].State = factory.PhaseDone
	if _, err := EditStepsItem(&it, factory.RunEdit{By: factory.ByYou, On: []string{"tidy"}}, factory.DefaultRecipe()); err == nil {
		t.Fatal("a started configured proof stage accepted an edit")
	}
}

// Several proof stages: work after the EARLIEST is refused, and a started
// check does not freeze, because checks precede legitimate work.
func TestMultipleProofStagesAndChecks(t *testing.T) {
	it := proofItem(factory.Item{State: factory.StateRunning}, factory.PhasePending, 0)
	it.Stages = []factory.Stage{
		{Name: "write", Kind: factory.StageChat, Ask: "w", Until: "done", On: true},
		{Name: "evidence", Kind: factory.StageChat, Ask: "e", Until: factory.UntilProven, On: true},
		{Name: "polish", Kind: factory.StageChat, Ask: "p", Until: "done", On: false},
		{Name: "proof", Kind: factory.StageChat, Ask: "s", Until: factory.UntilProven, On: true},
	}
	it.Stream = &factory.Stream{Phases: []factory.Phase{phase("write", factory.PhaseRunning), phase("evidence", factory.PhasePending), phase("proof", factory.PhasePending)}}
	if _, err := EditStepsItem(&it, factory.RunEdit{By: factory.ByYou, On: []string{"polish"}}, factory.DefaultRecipe()); err == nil {
		t.Fatal("work between two proofs was switched on")
	}

	chk := proofItem(factory.Item{State: factory.StateRunning}, factory.PhasePending, 0)
	chk.Stages[0] = factory.Stage{Name: "check", Kind: factory.StageCheck, Ask: "go test", On: true}
	chk.Stream.Phases[0] = factory.Phase{Name: "check", Kind: factory.StageCheck, State: factory.PhaseDone}
	if _, err := EditStepsItem(&chk, factory.RunEdit{By: factory.ByYou, Add: []factory.Added{{Stage: factory.Stage{Name: "lint", Kind: factory.StageChat, Ask: "l", Until: "done", On: true}, After: "write"}}}, factory.DefaultRecipe()); err != nil {
		t.Fatalf("a finished check froze the stages: %v", err)
	}
}

func TestLandedEditsWithoutStartedProofAreRefusedUnchanged(t *testing.T) {
	for _, variant := range []string{"proof missing", "proof off", "proof not selected"} {
		t.Run(variant, func(t *testing.T) {
			it := proofItem(landed(), factory.PhasePending, 0)
			it.Stream.Phases = it.Stream.Phases[:2]
			it.Stream.Cur = 1
			it.Stream.Phases[1].State = factory.PhaseDone
			switch variant {
			case "proof missing":
				it.Stages = it.Stages[:2]
			case "proof off":
				it.Stages[2].On = false
			case "proof not selected":
				it.Stages[2].When = "pr"
			}
			before := it
			lines, err := EditStepsItem(&it, factory.RunEdit{By: factory.ByYou, Add: []factory.Added{{Stage: factory.Stage{Name: "tidy", Kind: factory.StageChat, Ask: "tidy it", On: true}}}}, factory.DefaultRecipe())
			if err == nil || err.Error() != "the job has landed, so the steps can no longer be edited" {
				t.Fatalf("expected truthful landed refusal, got %v (lines=%v)", err, lines)
			}
			if !reflect.DeepEqual(before, it) {
				t.Fatal("landed refusal changed item")
			}
		})
	}
}

func TestSemanticProofCannotBeSkippedBeforeItStarts(t *testing.T) {
	for _, st := range []factory.Stage{{Name: "evidence", Kind: factory.StageChat, Until: factory.UntilProven, On: true}, {Name: "prove_again", Kind: factory.StageChat, On: true}, {Name: "Proof", Kind: factory.StageChat, On: true}} {
		t.Run(st.Name, func(t *testing.T) {
			it := factory.Item{State: factory.StateNew, Stages: []factory.Stage{{Name: "write", Kind: factory.StageChat, On: true}, st}}
			before := it
			_, err := EditStepsItem(&it, factory.RunEdit{By: factory.ByYou, Skip: []string{st.Name}}, factory.DefaultRecipe())
			if err == nil {
				t.Fatal("semantic proof skip accepted")
			}
			if !reflect.DeepEqual(before, it) {
				t.Fatal("refused proof skip changed item")
			}
		})
	}
}
