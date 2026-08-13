package main

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/provider/pool"
	"github.com/Agent-Field/aforge-v2/internal/revision"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The straggler, wired.
//
// internal/exec knows how to notice one — it counts its own spend on every turn
// and now has a number to compare it against — and internal/revision already
// owns the judgement about a leaf that did not get there. This is the two-line
// join between them, and it lives here because the runner is the only party that
// holds all four things the join needs: the profile directory the threshold is
// derived from, the durable journal the evidence is written to, the planning
// client the judge is asked through, and the retry loop whose next attempt is
// where a hand-back actually lands.
//
// What the wiring deliberately does not do is invent a second escalation path.
// The judge asked here is JudgeRetryWorker, the same one the failed-leaf path
// asks, given the same menu with the same worker excluded; a hand-back lands in
// the same attempt loop that a failure lands in, and the choice this judge makes
// is carried into that loop rather than asked again. The whole change is when
// the question is put: while the siblings are still waiting at the barrier this
// leaf is holding, rather than after its budget is gone and they have all been
// waiting for it the entire time.

// stragglerHandoff carries a judged worker from the leaf's goroutine back to
// the retry loop.
//
// It is guarded rather than a bare string because the executor runs on a
// goroutine the node watchdog is entitled to abandon: a wedged leaf that answers
// its overrun question ten minutes after the runner gave up on it must write
// into a value nobody is reading, never into one somebody is. take clears as it
// reads, so a choice is honoured exactly once.
type stragglerHandoff struct {
	mutex  sync.Mutex
	worker string
}

func (h *stragglerHandoff) set(worker string) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.worker = worker
}

func (h *stragglerHandoff) take() string {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	worker := h.worker
	h.worker = ""
	return worker
}

// stragglerJudge is everything needed to answer one leaf's overrun question.
type stragglerJudge struct {
	// ctx is the job's, not the leaf's. A leaf being asked about is a leaf whose
	// own context is about to be spent or cancelled either way, and a judgement
	// torn down halfway would leave the question asked, paid for and unanswered.
	ctx      context.Context
	settings config.Config
	graph    *store.Store
	client   *pool.Client
	node     store.Node
	// worker is who is running the leaf now — the name excluded from the menu,
	// because a worker must never be handed back its own straggler.
	worker string
	// menu is the specialists this leaf could be moved to. Empty means there is
	// nowhere else for it to go, in which case no call is made at all.
	menu string
	// brief is the assignment, so the judge decides about work rather than about
	// a token count.
	brief string
	// model reports the model actually running the leaf, read at the moment the
	// question is put rather than captured when the watch was built: the runner
	// reassigns it when a leaf escalates.
	model func() string
	// chose is called with the worker the judge named, so the retry loop can
	// honour a judgement that has already been made and paid for instead of
	// asking the same question a second time.
	chose func(string)
}

// stragglerWatch derives one leaf's threshold from its worker's own record and
// hands back a watch, or nil when there is not enough record to derive one.
//
// Nil is the ordinary answer on a fresh machine, on a newly registered
// specialist, and for every reflex micro-leaf, and nil means the executor never
// looks: no threshold, no question, no call, and a loop that behaves exactly as
// it did before any of this existed.
func stragglerWatch(settings config.Config, model string, judge stragglerJudge) *exec.OverrunWatch {
	measured, err := profile.Load(settings.ProfileDir, model, profileSubharness(judge.worker))
	if err != nil {
		return nil
	}
	straggler, ok := measured.Straggler()
	if !ok {
		return nil
	}
	return &exec.OverrunWatch{
		Threshold: straggler.Threshold,
		Anchor:    straggler.Anchor,
		Multiple:  straggler.Multiple,
		Samples:   straggler.Samples,
		Judge:     judge.decide,
	}
}

// decide journals what was seen and returns what was judged.
//
// The journal write comes first and happens whatever the verdict turns out to
// be, including when there is no judge to ask. That ordering is the point: the
// incident this was built for cost 31% of a run and left nothing behind, so a
// threshold that fired and was overruled must be as visible afterwards as one
// that fired and was upheld — otherwise the record only ever shows the cases
// where the mechanism looked right.
func (j stragglerJudge) decide(evidence exec.OverrunEvidence) exec.OverrunVerdict {
	chosen := j.ask(evidence)
	// Written out in words rather than as the verdict's own spelling, because
	// "carry on" is the empty string in the executor's vocabulary and an empty
	// string in a journal is indistinguishable from a field nobody filled in.
	if chosen == "" {
		j.journal(evidence, "continue", "")
		return exec.OverrunContinue
	}
	j.journal(evidence, "hand-back", chosen)
	if j.chose != nil {
		j.chose(chosen)
	}
	return exec.OverrunHandBack
}

// ask puts the question to the existing retry judge, or answers it itself when
// there is nobody to move the work to.
//
// Everything here fails toward continuing. No menu means no call and no change;
// a judge that cannot be reached, cannot be parsed, or names a worker this build
// does not have all come back as the empty string, which this reads as "the
// worker it is with should finish it". A straggler that nobody has a better home
// for is still a straggler, and it still runs — the finding is worth journaling
// and it is not, on its own, worth interrupting anything for.
func (j stragglerJudge) ask(evidence exec.OverrunEvidence) string {
	if j.client == nil || j.menu == "" {
		return ""
	}
	model := ""
	if j.model != nil {
		model = j.model()
	}
	// The judge reads a task and an outcome because that is what it has always
	// read. Both are assembled from what is true right now: the assignment as
	// given, and the work in hand described as unfinished, which it is.
	task := exec.Task{Brief: j.brief, Subharness: j.worker}
	outcome := &exec.Outcome{
		Text: evidence.Partial,
		// The same grading the leaf would get if this ended here, so the judge
		// is not shown a leaf in a state the rest of the system never produces.
		Verdict: provider.VerdictBudgetStop,
		Turns:   evidence.Turns,
	}
	ctx := j.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return revision.JudgeRetryWorker(ctx, j.settings, j.client, j.node,
		task, outcome, stragglerFinding(evidence), j.menu, model)
}

// stragglerFinding is how the overrun is described to the judge: the comparison,
// in one sentence, with both of its sides.
//
// It says the leaf is still running, because it is — this judgement is being
// made mid-flight and a judge told the work had failed would be reading a claim
// nobody made. What it must convey is the only thing that distinguishes this
// from an ordinary slow leaf: the cost is not large in the abstract, it is large
// against this worker's own record of doing this kind of work.
func stragglerFinding(evidence exec.OverrunEvidence) error {
	return fmt.Errorf(
		"still running: %d tokens over %d turns, against a measured median of %d for this worker over %d runs — past the %d its own record supports",
		evidence.Spent, evidence.Turns, evidence.Anchor, evidence.Samples, evidence.Threshold)
}

// journal writes the comparison to the durable record. A failure to write is
// logged and never propagated: this is diagnosis, and a leaf must not be
// disturbed because a note about it could not be filed.
func (j stragglerJudge) journal(evidence exec.OverrunEvidence, verdict, chosen string) {
	if j.graph == nil || j.node.ID == "" {
		return
	}
	if err := j.graph.RecordOverrunEvidence(store.OverrunEvidence{
		NodeID:    j.node.ID,
		Spent:     evidence.Spent,
		Turns:     evidence.Turns,
		Anchor:    evidence.Anchor,
		Threshold: evidence.Threshold,
		Multiple:  evidence.Multiple,
		Samples:   evidence.Samples,
		Worker:    j.worker,
		Verdict:   verdict,
		Chosen:    chosen,
	}); err != nil {
		log.Printf("note: could not journal the overrun evidence for %s: %v", j.node.ID, err)
	}
}
