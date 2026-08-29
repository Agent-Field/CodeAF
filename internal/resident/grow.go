// One governor for every way a running job grows.
//
// Growth used to be bounded in as many places as it happened. The overrun path
// held both caps and enforced them inline; the delivery gate inherited them by
// calling that path; and the revision sentinel — the mechanism that adds work
// because a landed result contradicted the plan — spliced straight into the job
// root with no ceiling, no round counter and no rail check at all. Three paths,
// two of which were governed by accident and one not at all, and afterwards
// nothing could tell an overrun replan from a sentinel add without reading
// intents node by node.
//
// So every execution-time add asks the same question of the same helper, and
// the answer is journaled against the job it grew. The order of the checks is
// cheapest first, and it ends with the only one that costs money: is the goal
// already covered? That question is the reason the criterion exists. Rounds,
// nodes and dollars all answer "have we done too much"; none of them answers
// "is there anything left to do", which is why a job could spend three
// legitimate rounds inventing verification of the round before it and be
// refused only by arithmetic, long after the money was gone.
package resident

import (
	"context"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The two caps that keep growth a repair rather than a lifestyle.
//
// Splitting used to be bounded by dollars alone, and one real run showed what
// that bound is worth on a cheap model: a leaf that had already finished was
// replanned 27 rounds deep — each round inventing verification of the round
// before it — and burned $4.48 of a $20 rail in 22 minutes while the job's
// actual work sat pending behind it. Dollars bound the damage, not the loop.
const (
	// MaxOverrunRounds bounds how many times one lineage may grow. Rounds are
	// sequential by construction — each plans the remainder of the last — so a
	// lineage still growing after three fresh budgets is not too big, it is
	// thrashing, and the honest move is to hand over what exists.
	//
	// It keeps its name because the overrun path is where it was learned and
	// every reader of that path knows it by this name; it governs every growth
	// path now.
	MaxOverrunRounds = 3

	// maxJobNodes is the job-lifetime ceiling on dynamic growth, the runtime
	// twin of the planner's NodeBudget: that ceiling is enforced per planning
	// pass, so a job that keeps splicing repairs could sprawl past it without
	// any single pass noticing. 1.5x the default plan budget leaves real room
	// for legitimate repair while refusing the sprawl the round cap alone
	// might miss when many siblings each split within their allowance.
	maxJobNodes = 90
)

// Why a path is asking to grow. The names are the journal's vocabulary and the
// only thing that made an overrun replan and a sentinel add distinguishable
// after the fact.
const (
	GrowOverrun  = "overrun"
	GrowGap      = "gap"
	GrowRevision = "revision"
	GrowRedirect = "redirect"
	GrowJIT      = "jit"
	GrowCoverage = "coverage"

	// GrowCooperative is the round a worker asked for rather than earned by
	// failing. It is its own word in the journal because it is the one growth
	// reason that is evidence of the machinery working: every other reason here
	// is a repair, and a battery asking "how often did a leaf divide before it
	// burned a budget instead of after" is asking for exactly this column.
	GrowCooperative = "cooperative"
)

// Which governor spoke. Cause is the machine-readable half of the refusal the
// person reads; a battery asking "did anything ever refuse growth for a reason
// other than a cap" is asking for exactly this column.
const (
	CauseRounds  = "rounds"
	CauseCeiling = "ceiling"
	CauseRail    = "rail"
	CauseCovered = "goal-already-covered"

	// CauseStandstill is the round that was refused because the round before it
	// changed nothing in the world, and neither did the work before that.
	CauseStandstill = "standstill"

	// CauseFixedPoint is the round that was refused because it was handed the
	// same remaining work as the round before it: a split whose output is its
	// own input.
	CauseFixedPoint = "fixed-point"
)

// The refusals in the words a person reads. They are constants because two of
// them are already load-bearing in tests and in the record: a governor stopping
// work quietly reads as work finishing, and the difference is the whole point
// of saying anything at all.
const (
	RefusedRounds  = "this work has split as many times as splitting helps — handing over what's done"
	RefusedCeiling = "this job has grown as large as jobs are allowed to grow — handing over what's done"
	RefusedCovered = "everything this job is judged on is already covered by work that has landed or is already running — handing over what's done"

	// The two refusals that read evidence rather than a count. They say what
	// was observed and not what rule fired, because the person reading them is
	// owed the fact — nothing has changed, twice over — and not the machinery.
	RefusedStandstill = "carrying on has stopped changing anything — twice over now, nothing was written or altered — so this is handed over as it stands"
	RefusedFixedPoint = "the work left to do came back word for word the same as last time, so another round would ask for exactly what this one already did — handing over what's done"
)

// GrowthGate is the wave's rollback switch. Off, the governor keeps the three
// free checks — rounds, ceiling, rail — and never asks the paid question, which
// is today's behaviour plus the revision fix and the journal.
var GrowthGate = os.Getenv("AFORGE_GROWTH_GATE") != "0"

// Satisfier answers the positive stopping question. It is an interface rather
// than a direct call into the plan package because the graph layer must not
// need a provider client to be tested, and because a build with no client at
// all — every test in this package — must behave exactly as it did before the
// gate existed.
type Satisfier interface {
	Satisfied(ctx context.Context, criterion plan.Done, landed []plan.Landed, inflight []plan.Spec) (plan.Satisfaction, error)
}

// SatisfierFunc adapts a plain function to the seam.
type SatisfierFunc func(ctx context.Context, criterion plan.Done, landed []plan.Landed, inflight []plan.Spec) (plan.Satisfaction, error)

// Satisfied calls the function.
func (f SatisfierFunc) Satisfied(ctx context.Context, criterion plan.Done, landed []plan.Landed, inflight []plan.Spec) (plan.Satisfaction, error) {
	return f(ctx, criterion, landed, inflight)
}

// SatisfierFor binds a planning client, and the window that client reads
// through, to the seam. Zero tokens is unknown and clips the gate's tables
// exactly where they were clipped before any of this existed.
//
// The window is bound once with the client rather than read per call, because
// it decides how much of the landed table each row carries and that table is
// this call's cache prefix: a number that moved between two asks about the same
// job would move the prefix with it.
//
// The call's usage is dropped rather than threaded back: it is one small call
// against a refused round's full replan plus the leaf that round would have
// spawned, and the paths that grow a job mid-run have no accounting slot to
// return it through. What it costs is visible where every other plan call's
// cost is, under the job's own spend node.
func SatisfierFor(client plan.Completer, contextTokens int) Satisfier {
	if client == nil {
		return nil
	}
	return SatisfierFunc(func(ctx context.Context, criterion plan.Done, landed []plan.Landed, inflight []plan.Spec) (plan.Satisfaction, error) {
		verdict, _, err := plan.Satisfied(ctx, client, contextTokens, criterion, landed, inflight)
		return verdict, err
	})
}

// The process-wide default, installed once by whoever owns a planning client.
//
// It is a package seam rather than a parameter because the callers that grow a
// job mid-run are reached through signatures owned by other waves — the daily
// budget and the plan function are already all they carry — and threading a
// client through every one of them to reach a call that is refused before it is
// made would be a wide change for a narrow question. Unset, the gate is off.
var (
	growthSatisfierMu sync.RWMutex
	growthSatisfier   Satisfier
)

// SetGrowthSatisfier installs the process-wide satisfaction gate. Nil removes
// it, which is the rollback.
func SetGrowthSatisfier(ask Satisfier) {
	growthSatisfierMu.Lock()
	defer growthSatisfierMu.Unlock()
	growthSatisfier = ask
}

func growthAsk(explicit Satisfier) Satisfier {
	if explicit != nil {
		return explicit
	}
	growthSatisfierMu.RLock()
	defer growthSatisfierMu.RUnlock()
	return growthSatisfier
}

// Growth is what a caller says about itself: why it is growing the job, and —
// where it has one of its own — the reader that answers whether the job still
// needs anything. Both are optional; the zero value is an overrun asking the
// process-wide gate.
type Growth struct {
	Reason string
	Ask    Satisfier
	// Ungated skips the satisfaction question and keeps the caps. It is for the
	// one caller whose growth is not a machine's second thought: a person who
	// has just said what they want more of is not answerable by "the goal is
	// already covered", because they have just redefined the goal.
	Ungated bool
	// After is the landed node this growth is a reaction to — the result that
	// convened the revision sentinel, exhausted or failed or merely surprising.
	// The zero node is growth with no such result behind it (a person changing
	// the goal), and it wires no evidence.
	//
	// It travels with the reason because it answers the same kind of question:
	// the reason says why the job grew, and this says what it grew from, which
	// is what an added node has to be able to read. The overrun splice has
	// always had it in hand — it is that path's whole subject — and the
	// revision path used to have nowhere to put it, so its additions were
	// admitted with no edge to the work they were replacing.
	After store.Node

	// Transcript is the exhausted attempt's OWN TURNS, read back from the
	// record it wrote as it worked (BankedRun). It is the field that makes a
	// continuation a resumption rather than a restart, for the same reason the
	// in-place retry's bank has it: an attempt stopped mid-turn ANNOUNCED
	// almost nothing, because announcing is what a leaf does when it is
	// finishing, and everything it actually did is nonetheless in the record.
	//
	// The textual run of 2026-08-29 is the measurement: six exhaustions, six
	// continuations, and not one resumption line — every successor opened by
	// exploring the repository its predecessor had already spent minutes in,
	// because State carried what the leaf chose to summarise and nothing
	// carried what it did.
	Transcript string
	// Resumed is how many turns Transcript was read from, journaled against the
	// continuation so the record can tell a claim that picked up work from one
	// that started over. Zero writes no row. See store.EventLeafResumed.
	Resumed int

	// Records are files the finished work left behind that the remainder must
	// READ rather than reuse: the text of a change, a measurement, a transcript.
	//
	// They are separate from the artifact list because the two are separate
	// invitations and collapsing them was measured producing a false statement.
	// An artifact is "this exists, do not make it again". A record is "this is
	// what happened, and it is where your account of it has to come from" — and
	// a repair that was handed only artifact NAMES had no way to learn what the
	// work it is finishing actually did, so the pass that wrote its method
	// offered an illustrative root cause instead and the leaf shipped that
	// example verbatim as the real one.
	//
	// Empty is every caller that has one kind of file and not the other, which
	// renders exactly the bytes this path has always rendered.
	Records []string
	// State is the dead leaf's structured findings — files it touched, checks
	// it ran, and its last tool calls — derived from the leaf's own outcome
	// by LeafState. It travels with Records for the same reason: the remainder
	// needs to know what the finished work actually did, not just what files
	// it left. A continuation that knows what the dead leaf already found
	// resumes from there instead of re-reading everything it already diagnosed.
	// Empty is every caller that has no structured outcome, which renders
	// exactly the bytes this path has always rendered.
	State string

	// Goal overrides the brief the splice is planned from. Empty — every caller
	// that existed before the cooperative path — keeps OverrunGoal, which is
	// the only phrasing the splice has ever used.
	//
	// It exists because that phrasing is a claim and not a template: "it stopped
	// when its resources ran out, so parts of the assignment may already be
	// complete" is the first thing the planner reads, and it is false of a leaf
	// that handed its budget back on purpose. A planner told the work ran out
	// plans a remainder; the cooperative path needs it to plan a division, and
	// those are different questions asked of the same call.
	Goal string

	// KeepEnvelope leaves the worker choice exactly as the caller made it,
	// instead of climbing the generalist ladder from the envelope that just
	// ended. False — every caller that existed before the cooperative path —
	// keeps escalateContinuation, which is what a continuation of an exhausted
	// leaf needs.
	//
	// The ladder's premise is that an exhaustion is evidence the sitting was
	// bigger than the envelope, so repeating the envelope pays to learn the
	// same lesson twice. A cooperative split is the opposite evidence: nothing
	// ran out, the leaf handed its grant back, and each part is smaller than
	// what the leaf was holding. Escalating those would provision every part of
	// a division against a failure that did not happen — and it would overwrite
	// the envelope the division's own sizing pass chose for each part.
	KeepEnvelope bool
}

func (g Growth) reason() string {
	if reason := strings.TrimSpace(g.Reason); reason != "" {
		return reason
	}
	return GrowOverrun
}

// GrowRequest is one path asking to add work to a running job.
type GrowRequest struct {
	// JobRoot is the id namespace the job's nodes are minted under: both the
	// ceiling's corpus and the journal's key.
	JobRoot string
	// Node is where a refusal is recorded — the work whose reader needs to know
	// that a governor, and not the work finishing, is why nothing more happens.
	Node store.Node
	// Lineage is the namespace rounds are counted under: a leaf's own split
	// lineage for a replan, the job root for growth that belongs to the job as
	// a whole. Two siblings that each split once are two lineages with one
	// round each, not one lineage with two.
	Lineage string
	Reason  string
	// Adding is the node count about to be spliced. Zero means the caller does
	// not know yet — it has not planned the growth — and the ceiling is then
	// read as "is there room for anything at all".
	Adding int
	// Round is the round this growth would be, when the caller already knows it
	// from its own id arithmetic. Zero derives it from the journal.
	Round int
	// Criterion overrides what the job is judged against. Empty reads it off
	// the job root's own spec, which is where W1 put it.
	Criterion      plan.Done
	DailyBudgetUSD float64
	// Ungated keeps the caps and skips the paid question — Growth.Ungated,
	// carried to the one helper that acts on it.
	Ungated bool
	// Quiet keeps the refusal out of the work's record while keeping it in the
	// journal. It is for the one caller whose refusal is not a handover: a
	// claim-time expansion that is refused runs the node whole, immediately, on
	// the worker that is already holding it — so the words every other path
	// needs ("handing over what's done") would describe something that is not
	// happening, on a node the reader is about to watch finish.
	Quiet bool
	// Measured says this caller read the world and Produced is its answer.
	//
	// It is a separate field and not a zero test on Produced, because "nobody
	// looked" and "somebody looked and the answer was nothing" are opposite
	// facts that a bare zero spells the same way — and a rule that read them as
	// one would refuse a caller that never measured anything, which is a
	// fail-safe pointing the wrong way. A growth path with no reading of the
	// tree keeps exactly the governors it had.
	Measured bool
	// Produced is how many files the work this growth reacts to left behind in
	// the world. It comes from the workspace's own before-and-after reading of
	// the tree, never from the worker's account of itself, because a worker
	// that produced nothing is exactly the worker whose account cannot be
	// trusted about it. Meaningless unless Measured.
	Produced int
	// Remainder is the work this round is being bought to finish, as the
	// reviewer named it. It is compared against the last round's for equality
	// and for nothing else.
	Remainder string
	// Rechecking marks a second look at a decision already taken this round —
	// the exact ceiling, once a plan exists and its node count is known. The
	// free checks are re-read; the paid question is not, because it was already
	// asked on the way in and its answer has not changed.
	Rechecking bool
}

// GrowVerdict is the governor's answer.
type GrowVerdict struct {
	Allow bool
	Round int
	// Refused is the sentence a person reads, empty when allowed and when the
	// pause is the daily rail — a rail is not a refusal, it is a question
	// already asked in its own words elsewhere.
	Refused string
	Cause   string
}

// growJob is the one gate every execution-time add passes through.
//
// Order is cheapest first, and the model call is last so that on the common
// path — a job with rounds and room to spare, growing for the first time — it
// is never asked at all.
func growJob(ctx context.Context, graph *store.Store, ask Satisfier, req GrowRequest) (GrowVerdict, error) {
	if graph == nil {
		return GrowVerdict{Allow: true}, nil
	}
	jobRoot := strings.TrimSpace(req.JobRoot)
	if jobRoot == "" {
		jobRoot = req.Node.ID
	}
	lineage := strings.TrimSpace(req.Lineage)
	if lineage == "" {
		lineage = jobRoot
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = GrowOverrun
	}

	round := req.Round
	if round <= 0 {
		round = growthRound(graph, jobRoot, lineage)
	}
	refuse := func(cause, words string) (GrowVerdict, error) {
		verdict := GrowVerdict{Round: round, Cause: cause, Refused: words}
		if words != "" && !req.Quiet {
			postGovernorNotice(graph, req.Node, cause, words)
		}
		noteGrowth(graph, jobRoot, store.JobGrowth{
			Reason: reason, Lineage: lineage, Adding: req.Adding,
			Round: round, Allowed: false, Refused: words, Cause: cause,
			Measured: req.Measured, Produced: req.Produced, Remainder: req.Remainder,
		})
		return verdict, nil
	}

	// 0. Evidence, before any of the counters. This is the only check here that
	//    asks whether the last round ACHIEVED anything; every other one asks
	//    whether too much has been spent, and a loop that produces nothing can
	//    run its whole allowance before arithmetic notices. It is free — one
	//    read of the journal this function already writes.
	//
	//    Both readings are of the world rather than of a worker's account of
	//    itself: what the workspace holds that it did not hold before, and the
	//    remaining work a reviewer named. See [previousGrowth].
	if previous, ok := previousGrowth(graph, jobRoot, lineage); ok {
		// A split whose remaining work is its parent's remaining work is a
		// fixed point. The round that just ran was aimed at exactly this text
		// and gave it back unchanged, so the next round would buy the same
		// question a third time. Measured: one lineage was handed a
		// byte-identical remainder three rounds running, at $0.47 a round, and
		// produced nothing on any of them.
		if req.Remainder != "" && req.Remainder == previous.Remainder {
			return refuse(CauseFixedPoint, RefusedFixedPoint)
		}
		// And the same finding from the other side, for a remainder that was
		// reworded rather than repeated: two consecutive bodies of work that
		// left nothing at all in the tree. One is not evidence — a leaf can run
		// out before it writes its first file, and that is exactly the round a
		// repair exists for, so the first one is never refused here. Two is a
		// standstill.
		if req.Measured && previous.Measured && req.Produced == 0 && previous.Produced == 0 {
			return refuse(CauseStandstill, RefusedStandstill)
		}
	}

	// 1. Rounds. A lineage that is still growing after its allowance is
	//    thrashing, whatever the reason it gives for the next round.
	//
	//    It is the BACKSTOP and not the mechanism: a count cannot tell a round
	//    that is finishing the work from a round that is repeating it, which is
	//    why check 0 above reads what happened instead.
	if round > MaxOverrunRounds {
		return refuse(CauseRounds, RefusedRounds)
	}

	// 2. Nodes. The read failing is not a reason to refuse: the ceiling is a
	//    bound on sprawl, and a job that cannot be counted is not evidence of
	//    sprawl. The round cap still holds either way.
	if ids, err := graph.NodeIDsWithPrefix(jobRoot); err == nil {
		wanted := req.Adding
		if wanted <= 0 {
			wanted = 1
		}
		if len(ids)+wanted > maxJobNodes {
			return refuse(CauseCeiling, RefusedCeiling)
		}
	}

	// 3. Dollars. Not a refusal: the rail journals the repair and waits for
	//    consent, and the caller — which owns what waiting means for its own
	//    path — is told which pause this is rather than being handed words.
	if req.DailyBudgetUSD > 0 {
		rail, _, err := graph.PauseDailyRail(req.DailyBudgetUSD, req.Node.Provenance.SessionID)
		if err != nil {
			return GrowVerdict{Round: round}, err
		}
		if rail.Reached {
			return refuse(CauseRail, "")
		}
	}

	// 4. The only question that can say "there is nothing left to do".
	if gate := growthAsk(ask); GrowthGate && gate != nil && !req.Rechecking && !req.Ungated {
		criterion := req.Criterion
		if criterion.Empty() {
			criterion = jobCriterion(graph, jobRoot)
		}
		if !criterion.Empty() {
			landed, inflight := jobCoverage(graph, jobRoot)
			verdict, err := gate.Satisfied(ctx, criterion, landed, inflight)
			switch {
			case err != nil:
				// Fail open. A gate that cannot answer must not truncate work
				// that was genuinely unfinished — the caps below it are what
				// bound the damage, and they already held.
				log.Printf("note: could not ask whether %s still needs work: %v", jobRoot, err)
			case verdict.Complete:
				return refuse(CauseCovered, RefusedCovered)
			}
		}
	}

	return GrowVerdict{Allow: true, Round: round}, nil
}

// admitGrowth journals a round that actually landed. It is called after the
// splice rather than at the verdict, because the round counter is derived from
// these events and an admission that never spliced is a round nobody spent —
// the same rule the overrun prefix arithmetic already follows.
func admitGrowth(graph *store.Store, req GrowRequest, verdict GrowVerdict, spliced int) {
	if graph == nil || spliced <= 0 {
		return
	}
	jobRoot := strings.TrimSpace(req.JobRoot)
	if jobRoot == "" {
		jobRoot = req.Node.ID
	}
	lineage := strings.TrimSpace(req.Lineage)
	if lineage == "" {
		lineage = jobRoot
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = GrowOverrun
	}
	noteGrowth(graph, jobRoot, store.JobGrowth{
		Reason: reason, Lineage: lineage, Adding: spliced,
		Round: verdict.Round, Allowed: true,
		// The evidence travels with the admission because the NEXT round is
		// weighed against it. A round admitted without it leaves the lineage
		// with no record of what the work it grew from actually did, and check 0
		// then has nothing to compare — which reads as "not measured" and lets
		// the round through, the fail-safe direction for a bound that has the
		// round cap under it.
		Measured: req.Measured, Produced: req.Produced, Remainder: req.Remainder,
	})
}

// noteGrowth writes one decision to the journal. Losing it costs the counter
// its memory, so it is noted rather than swallowed — and never fatal, because a
// splice that happened is more true than a record of it.
func noteGrowth(graph *store.Store, jobRoot string, growth store.JobGrowth) {
	if err := graph.RecordJobGrowth(jobRoot, growth); err != nil {
		log.Printf("note: could not journal growth of %s: %v", jobRoot, err)
	}
}

// previousGrowth is the last round this lineage actually spent, and it is what
// check 0 weighs the next one against.
//
// Only ADMITTED rounds count, for the same reason growthRound only counts them:
// a refusal is not a round anybody spent, and a lineage refused once at the rail
// and resumed later has done its work once. A read failure answers "no previous
// round", which admits the growth — the fail-safe direction for a bound that has
// the round cap and the job ceiling underneath it.
func previousGrowth(graph *store.Store, jobRoot, lineage string) (store.JobGrowth, bool) {
	growths, err := graph.JobGrowths(jobRoot)
	if err != nil {
		log.Printf("note: could not read the growth journal for %s: %v", jobRoot, err)
		return store.JobGrowth{}, false
	}
	for i := len(growths) - 1; i >= 0; i-- {
		if growths[i].Allowed && growths[i].Lineage == lineage {
			return growths[i], true
		}
	}
	return store.JobGrowth{}, false
}

// growthRound is the next round number for a lineage, counted from the
// admissions journaled under the job root. Rounds are per lineage and the
// journal is per job: two siblings that each split once do not consume each
// other's allowance, and reading either one back is one query.
func growthRound(graph *store.Store, jobRoot, lineage string) int {
	growths, err := graph.JobGrowths(jobRoot)
	if err != nil {
		log.Printf("note: could not read the growth journal for %s: %v", jobRoot, err)
		return 1
	}
	spent := 0
	for _, growth := range growths {
		if growth.Allowed && growth.Lineage == lineage {
			spent++
		}
	}
	return spent + 1
}

// jobCriterion reads what a job is judged against off its own root.
func jobCriterion(graph *store.Store, jobRoot string) plan.Done {
	root, ok, err := graph.Node(jobRoot)
	if err != nil || !ok {
		return plan.Done{}
	}
	return DecodeSpec(root.Spec).Done
}

// jobCoverage is what the gate is shown: what the job already has in hand, and
// what its running and pending work is committed to producing.
//
// Landed carries results in admission order, which is what makes the prompt
// prefix append-only across a job's life. In flight carries specs, because the
// question is what a piece commits to and not what it is called; a node with no
// spec still says something — its title is a commitment of a weaker kind, and
// omitting it entirely would tell the gate that nothing is coming.
func jobCoverage(graph *store.Store, jobRoot string) ([]plan.Landed, []plan.Spec) {
	nodes, err := graph.SubtreeNodes(jobRoot)
	if err != nil {
		log.Printf("note: could not read %s to weigh its coverage: %v", jobRoot, err)
		return nil, nil
	}
	var landed []plan.Landed
	var inflight []plan.Spec
	for _, node := range nodes {
		if node.ID == jobRoot || node.Folded {
			continue
		}
		title := strings.TrimSpace(node.Title)
		if title == "" {
			title = firstLine(node.Brief)
		}
		switch node.Status {
		case store.Done:
			landed = append(landed, plan.Landed{Title: title, Result: node.Summary})
		case store.Pending, store.Claimed, store.Running:
			spec := DecodeSpec(node.Spec)
			if spec.Empty() {
				spec = plan.Spec{Instruction: title}
			}
			inflight = append(inflight, spec)
		}
	}
	return landed, inflight
}
