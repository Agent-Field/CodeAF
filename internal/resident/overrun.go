// Just-in-time re-decomposition: a leaf that ran out of budget mid-work is
// the planner's clearest evidence that one node held more than one agent's
// worth. Instead of shipping the partial or failing the job, the remainder is
// re-planned — informed by what the partial actually produced — and spliced
// in as deeper structure that consumes the partial and feeds everyone who
// was waiting. Specialize by addition: the journal keeps the exhausted
// attempt, the graph grows the finish.
package resident

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"

	executor "github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
)

// SpecUnchangedNotice is what a spec says about its own criterion when it is
// carried onto a remainder or onto a replacement attempt.
//
// It is one sentence and it is the whole of the re-target contract in words:
// the bar did not move because the attempt did. Without it, a planner handed a
// criterion reads it as material — something to summarise, improve, or expand
// on — and a criterion that grows every round is a criterion that cannot be met.
const SpecUnchangedNotice = "The criterion this work is judged against has not changed and is given below " +
	"unaltered. Plan against it; do not restate it, extend it, or replace it."

// overrunCriterionLimit bounds the criterion inside a replan goal. The goal
// already carries the assignment, the partial and the gap; the criterion is the
// smallest of the four and must stay that way.
const overrunCriterionLimit = 1200

// OverrunPlanFunc plans the remaining work of an exhausted leaf into a
// subtree, with prefix as the id namespace for the new nodes.
type OverrunPlanFunc func(ctx context.Context, goal, prefix string) (store.Subtree, error)

// overrunMarker tags re-expansion namespaces. The counter advances for every
// repair while replacing the old suffix, so journal ids stay unique without
// growing a stack of -x1 markers.
const overrunMarker = store.SplitNamespace

// The caps that keep re-decomposition a repair rather than a lifestyle live in
// grow.go now, with every other path that can grow a running job: MaxOverrunRounds
// and maxJobNodes were this file's alone while this file was the only mechanism
// that could add work, and they stopped being that the day the revision sentinel
// started adding nodes of its own.

// OverrunGoal phrases the replan brief. The partial result is in the goal on
// purpose — "based on the current result" is the whole point: the planner
// sees what was actually produced and plans only what remains.
//
// The phrasing is deliberately neutral about how much is left. The first
// version asserted the work "could not be completed", and handed that premise
// to a planner that cannot answer "nothing" — so when the partial was in fact
// complete, the planner obliged the premise by inventing verification of it,
// and each round of invented verification became the next round's premise.
// Naming what a reviewer found missing, when a reviewer ran, keeps the replan
// aimed at the actual gap instead of at whatever sounds like more work.
// records, when there are any, are the files the finished work left behind that
// this remainder must READ. They are rendered apart from the artifact list and
// said to be readable, because "reuse rather than recreate" is an instruction
// about not repeating work and this is an instruction about where the facts come
// from — a leaf handed the second under the first's heading reads a path as a
// thing it already has rather than as a thing it has to open.
func OverrunGoal(node store.Node, partial string, artifacts []string, gap, state string, records ...string) string {
	return overrunGoal(node, partial, artifacts, gap, state, "", OpenFindings{}, records...)
}

// overrunGoal is OverrunGoal with the exhausted attempt's own turns as well.
//
// The transcript is a separate parameter rather than another variadic because
// it is the longest block by far and it goes LAST, under Bank's own header:
// the summary blocks above are the attempt's account of itself, and this is the
// attempt itself, so a reader that ran out of attention before reaching it has
// already been told what it needed. See Bank.Continuation, which composes the
// same four blocks in the same order for the in-place retry.
func overrunGoal(node store.Node, partial string, artifacts []string, gap, state, transcript string, findings OpenFindings, records ...string) string {
	var goal strings.Builder
	goal.WriteString("Finish work a previous agent started. It stopped when its resources ran out, so parts of the assignment may already be complete. Plan only what the assignment still needs — work that is already done must not be redone, and do not add verification, re-verification, or review of existing results unless the assignment itself asks for it.\n\nThe original assignment:\n")
	goal.WriteString(node.Brief)
	// The criterion travels with the remainder rather than being re-derived
	// from the brief. A replan that re-derives it writes a new one, and a new
	// one written from a partial result is a criterion aimed at the work that
	// happened rather than at the work that was asked for — which is how a
	// round's invented verification became the next round's premise.
	if criterion := DecodeSpec(node.Spec).Done; !criterion.Empty() {
		goal.WriteString("\n\n")
		goal.WriteString(SpecUnchangedNotice)
		goal.WriteString("\n")
		goal.WriteString(plan.Spec{Done: criterion}.Render(overrunCriterionLimit))
	}
	if strings.TrimSpace(partial) != "" {
		// The header is Bank's, not this function's. Three paths now hand
		// unfinished work on — a re-decomposed leaf, an in-place retry, a requeue
		// at launch — and one wording of "here is what the last agent got to"
		// serves all three. See bank.go.
		goal.WriteString("\n\n" + ContinuationPartialHeader + "\n")
		goal.WriteString(partial)
	}
	if strings.TrimSpace(state) != "" {
		goal.WriteString("\n\n" + ContinuationStateHeader + "\n")
		goal.WriteString(state)
	}
	if strings.TrimSpace(gap) != "" {
		goal.WriteString("\n\nA reviewer compared that result against the assignment and named what is missing. Plan the work that closes these gaps and nothing else:\n")
		goal.WriteString(gap)
		// What comes back from this round is what the person reads, and it is
		// the last thing they read: a repair on a top-level job continues as a
		// top-level job and is announced as its deliverable. Without this
		// sentence the round answers the reviewer instead of the person — one
		// measured cell delivered "no bug to find" as its opening line, on work
		// whose fix had already been applied and verified.
		goal.WriteString("\n\nWhat this work hands back is the finished assignment as the person will read it — the whole answer, standing on its own. It is not a reply to the review, not a note on what was missing, and not an account of what was repaired.")
	}
	if len(artifacts) > 0 {
		goal.WriteString("\n\n" + ContinuationFilesHeader + "\n")
		goal.WriteString(strings.Join(artifacts, "\n"))
	}
	if len(records) > 0 {
		goal.WriteString("\n\nThe record of what the earlier work actually did, as readable files on disk. " +
			"Open them: any statement this assignment makes about what was wrong, what was changed, " +
			"or why, has to come from what is in them. Where they do not settle something, the honest " +
			"answer is that the record does not name it — never an inference from the fact that the " +
			"work succeeded, and never an example of what the answer might have been:\n")
		goal.WriteString(strings.Join(records, "\n"))
	}
	// The measured shortfall, verbatim from the record, so the planner sizes the
	// remainder against what the job is actually short of rather than against
	// the prose of a review. See OpenFindings.
	if section := findings.Words(); section != "" {
		goal.WriteString("\n\n" + section)
	}
	// Last, and longest — the attempt itself rather than its account of itself.
	// It carries Bank's own header so that all three ways unfinished work is
	// handed on say it in one wording. See Growth.Transcript.
	if block := strings.TrimSpace(transcript); block != "" {
		goal.WriteString("\n\n" + ContinuationTranscriptHeader + "\n")
		goal.WriteString(block)
	}
	return goal.String()
}

// RemainderDigest is a piece of remaining work reduced to something two rounds
// can be compared by.
//
// Case and runs of whitespace are dropped because they are the two ways one
// sentence is written twice without being a different sentence; nothing else is.
// This is an EQUALITY test and not a similarity one on purpose: a remainder that
// came back reworded is a different claim about what is left, and it is the
// standstill rule beside this one — which reads the tree rather than the text —
// that catches a loop dressed in fresh words. Empty in, empty out, and an empty
// digest never matches anything, so a caller with no reviewer finding is never
// refused on this ground.
func RemainderDigest(gap string) string {
	if strings.TrimSpace(gap) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.ToLower(strings.Join(strings.Fields(gap), " "))))
	return hex.EncodeToString(sum[:8])
}

// ReplanOverrun splices a repair subtree for a leaf whose partial result is
// about to land. The subtree's entry nodes consume the exhausted node's
// digest, its sink feeds every consumer that was waiting on the exhausted
// node and has not started, and the whole thing lives under the same job so
// workspaces, folding, and narration all treat it as the job's own work.
// The gap, when non-empty, is what a reviewer found missing from the partial —
// it aims the replan at the actual remainder.
// Returns the spliced node count and the repair sink's id. DailyBudgetUSD zero
// is unlimited; at the rail the durable question is posted and no splice lands.
func ReplanOverrun(ctx context.Context, graph *store.Store, node store.Node, partial, gap string, artifacts []string, dailyBudgetUSD float64, planRemainder OverrunPlanFunc) (int, string, error) {
	return ReplanOverrunOn(ctx, graph, node, partial, gap, artifacts, dailyBudgetUSD, "", planRemainder)
}

// ReplanOverrunOn is the same splice with the remaining work handed to a named
// worker.
//
// The name comes from the judgement that decided there was a remainder at all —
// the same call, one question wider — and it rides the subtree's provenance,
// which is where every other whole-subtree choice already rides. Empty is the
// default worker and is what every caller passed before this existed, so the
// splice is unchanged for a build with nothing to choose between.
//
// The choice is not inherited from the exhausted node verbatim — a worker that
// ran out of resources on a piece of work has said nothing about who should
// finish it — but it is never repeated either. An exhaustion is evidence the
// sitting was bigger than the envelope, and re-running the same envelope is
// paying to learn the same lesson twice. So the continuation escalates one
// rung up the generalist ladder (bare → linear) and keeps the generalist at
// its ceiling (linear), while a specialist the judge recognised is never
// downgraded and the frozen engine is left to its own continuation
// semantics. See escalateContinuation. The baseline for a worker nobody
// chose remains the baseline — which is what "degradation, never failure"
// means at a splice.
func ReplanOverrunOn(ctx context.Context, graph *store.Store, node store.Node, partial, gap string, artifacts []string, dailyBudgetUSD float64, worker string, planRemainder OverrunPlanFunc) (int, string, error) {
	return ReplanOverrunAs(ctx, graph, node, partial, gap, artifacts, dailyBudgetUSD, worker, Growth{Reason: GrowOverrun}, planRemainder)
}

// ReplanOverrunAs is the same splice with the growth named for what asked.
//
// The delivery gate grows a job for a reason this file never had — a reviewer
// found the result wrong, not the budget short — and it used to inherit this
// path's governors by borrowing its whole function, which left the journal
// unable to say afterwards which of the two had spent the round. The reason
// travels now; everything else is identical.
func ReplanOverrunAs(ctx context.Context, graph *store.Store, node store.Node, partial, gap string, artifacts []string, dailyBudgetUSD float64, worker string, growth Growth, planRemainder OverrunPlanFunc) (int, string, error) {
	spliced, sink, _, err := replanOverrun(ctx, graph, node, partial, gap, artifacts, dailyBudgetUSD, "", worker, growth, planRemainder)
	return spliced, sink, err
}

// escalateContinuation decides the subharness a continuation node runs on,
// from the envelope the dead leaf ran on (dead) and the worker the caller
// judged the remainder belongs to (judged). It is the continuation half of
// the routing decision: the initial plan's sizing pass chose the dead leaf's
// envelope; here, the exhaustion of that envelope is the evidence the next
// decision is made from.
//
// An exhaustion is evidence the sitting was bigger than the envelope, and
// re-running the same envelope is paying to learn the same lesson twice. So
// the continuation escalates one rung up the generalist ladder — bare to
// linear — instead of repeating the envelope that just ran out. linear is the
// generalist ceiling: it is the largest single-agent envelope, so an
// exhaustion there has no higher generalist rung to climb to, and repeating it
// is correct rather than a reflex. A specialist the judge recognised is never
// downgraded by the escalation — the clock exhausting a generalist says
// nothing about whether the work was the judge's to route to a specialist.
//
// The frozen engine has its own continuation semantics — it resumes from
// its own checkpoints rather than splicing a fresh node — so exhausting it is
// not evidence the envelope was too small: the caller's choice stands and the
// ladder does not touch it. A dead leaf nobody sized promised no envelope, so
// its continuation keeps whatever the caller judged (or the baseline when
// nothing was), preserving the splice's "degradation, never failure" default.
//
// EVERY RUNG IS GATED ON THE WORKER BEING INSTALLED HERE. A judgment is a
// claim about the work; whether this install has the worker to act on it is a
// separate fact, and the profile's roster (internal/config's workers.go) is
// what answers it. A name that reaches no installed worker would run on the
// generalist anyway — the registry degrades rather than fails — but it would
// be WRITTEN onto the continuation node and into the ledger, so a profile that
// holds no coding pipeline would keep filing generalist leaves under the
// pipeline's name. The gate is the same one the provenance rung has always
// had, said once at the top so that every road out of this function obeys it.
func escalateContinuation(dead, judged, provenance string) string {
	dead = strings.TrimSpace(dead)
	judged = strings.TrimSpace(judged)
	// A judged worker this install does not have is THE GENERALIST, NAMED —
	// not silence. Somebody did answer the question, and blanking their answer
	// would make it indistinguishable from the verdict nobody made, which every
	// reader downstream fills in from somewhere else. The generalist is left
	// alone because it is never a registration, and an unanswered question is
	// left alone because it is not an answer.
	if executor.SubharnessChosen(judged) && !executor.GeneralistSubharness(judged) &&
		!executor.KnownSubharness(judged) {
		judged = executor.LinearSubharness
	}
	// The frozen engine first: its exhaustion is its own business, and the
	// caller's choice — judged or empty — is returned unchanged.
	if strings.EqualFold(dead, executor.SWESubharness) {
		return judged
	}
	// A specialist the judge recognised is the top of the ladder; escalation
	// only ever climbs, never downgrades it. It reaches here only when this
	// install actually has that worker, by the gate above.
	if strings.EqualFold(judged, executor.SWESubharness) {
		return executor.SWESubharness
	}
	// bare is the smallest envelope. Escalate it to the generalist rather than
	// repeat it: the bare sitting just proved the work was bigger than bare,
	// and a second bare leaf would pay to learn the same lesson twice.
	if strings.EqualFold(dead, executor.BareSubharness) {
		return executor.LinearSubharness
	}
	// linear is ordinarily the generalist ceiling — the largest single-agent
	// envelope. There is one rung above it, and only when the job's own
	// provenance names it: the compiler judged the ask's shape at admission
	// ("this is specialist work") and two exhausted single-agent envelopes are
	// the size evidence that shape judgment was waiting for. A linear
	// exhaustion on such a job climbs to the provenance's specialist; on any
	// other job the generalist is kept, named, because there is no higher
	// rung to climb to.
	if executor.GeneralistSubharness(dead) {
		if specialist := strings.TrimSpace(provenance); specialist != "" &&
			!executor.GeneralistSubharness(specialist) &&
			!strings.EqualFold(specialist, executor.BareSubharness) &&
			executor.KnownSubharness(specialist) {
			return specialist
		}
		return executor.LinearSubharness
	}
	// A dead leaf nobody sized promised no envelope, so the caller's choice
	// stands unchanged — the continuation inherits whatever was judged, or the
	// baseline when nothing was.
	return judged
}

// replanOverrun reports capped=true when a governor refused the splice: the
// repair is abandoned for good, unlike the rail's zero-splice pause, which is
// waiting for consent. Deferred resumption needs the difference — a capped
// repair must resolve rather than wait forever.
func replanOverrun(ctx context.Context, graph *store.Store, node store.Node, partial, gap string, artifacts []string, dailyBudgetUSD float64, prefix, worker string, growth Growth, planRemainder OverrunPlanFunc) (int, string, bool, error) {
	// The continuation escalates one rung up the ladder from the envelope that
	// just exhausted, instead of repeating it. This is decided here — at the
	// splice, where the continuation node's subharness is journaled onto the
	// subtree's provenance — so it flows to the deferred record at the rail and
	// re-applies idempotently on resume. The dead leaf's envelope is on its node
	// record; the caller's judgement rides the worker parameter.
	if !growth.KeepEnvelope {
		worker = escalateContinuation(node.Subharness, worker, node.Provenance.Subharness)
	}
	var err error
	if prefix == "" {
		prefix, err = nextOverrunPrefix(graph, node.ID)
		if err != nil {
			return 0, "", false, fmt.Errorf("replan overrun %s: %w", node.ID, err)
		}
	}
	// The round is the prefix's own arithmetic rather than the journal's count:
	// nextOverrunPrefix numbers rounds past the highest round that actually
	// spliced, so a refusal never consumes one, and a resumed repair carries the
	// round it was deferred at. The governor is told the answer rather than
	// asked to derive a second one that could disagree.
	lineage, round := OverrunLineage(prefix)
	request := GrowRequest{
		JobRoot: jobRootID(graph, node), Node: node, Lineage: lineage,
		Reason: growth.reason(), Round: round, DailyBudgetUSD: dailyBudgetUSD,
		Ungated: growth.Ungated, Grounded: growth.Grounded,
		// The two facts the governor weighs this round against, both taken from
		// outside the work being weighed. The artifact list is the workspace's
		// own before-and-after reading of the tree, not the leaf's account of
		// itself; the gap is what a reviewer named as still missing.
		//
		// The LIST travels rather than its length. What counts as having
		// produced something is the governor's question and not this path's —
		// a round that wrote fourteen debug files beside a change it never
		// touched produced fourteen of nothing, and a count cannot say so. See
		// MeasureRound.
		Measured: true, Artifacts: artifacts, Remainder: RemainderDigest(gap),
		// And the finding this round is being bought to close, read off the
		// judgement that convened it. It is read HERE, at the one seam every
		// growing job passes through, for the reason the lineage bank and the
		// open findings above it are: a property of the round cannot be a
		// property of whichever caller somebody remembered to wire. A caller
		// with no finding on its context is a round nobody bought for one, and
		// it keeps exactly the governors it had.
		Finding: FindingFrom(ctx),
	}
	// The world is read once for this round, here, and the same reading is what
	// both the way-in decision and the exact recheck below are made from.
	request = request.weighed(graph, request.JobRoot, lineage)
	verdict, err := growJob(ctx, graph, growth.Ask, request)
	if err != nil {
		return 0, "", false, fmt.Errorf("replan overrun %s: check daily rail: %w", node.ID, err)
	}
	if !verdict.Allow {
		if verdict.Cause == CauseRail {
			// A repair held at the rail carries what it needs to be replanned
			// later, and the record roster is deliberately not among it: the
			// files it names are the run's own sidecars, which the workspace may
			// have swept by the time consent arrives, and a path journaled today
			// and dead tomorrow is worse than a plan that knows it has no record.
			// The remainder planned on resumption is told exactly that, and its
			// methods say so rather than improvising. See Growth.Records.
			deferred := store.DeferredOverrun{NodeID: node.ID, Partial: partial, Gap: gap,
				Artifacts: artifacts, Prefix: prefix, Subharness: worker, State: growth.State}
			if err := graph.DeferOverrun(deferred); err != nil {
				return 0, "", false, fmt.Errorf("replan overrun %s: defer at daily rail: %w", node.ID, err)
			}
			return 0, "", false, nil
		}
		return 0, "", true, nil
	}
	anchor := PlanAnchor{NodeID: request.JobRoot, SessionID: node.Provenance.SessionID}
	planCtx := withPlanAnchor(ctx, anchor)
	planCtx = withPlanRecords(planCtx, growth.Records)
	// The caller's own phrasing when it has one; see Growth.Goal for why an
	// exhaustion's words are not a template.
	// WHAT THE JOB HAS ALREADY DONE IS READ HERE, AT THE SPLICE, AND NOT PASSED
	// IN BY WHOEVER ASKED FOR IT.
	//
	// This is the one seam every growing job passes through — the overrun round,
	// the gate's gap round, the cooperative split and the deferred resumption
	// all reach the graph through this function — and until now the seed rode in
	// on the caller's Growth. So it worked on the one caller that had been wired
	// for it and on none of the others: the textual run of 2026-08-29 grew three
	// times on `reason: gap`, spliced fourteen fresh ids, and every one of them
	// opened by exploring a repository the lineage had been editing for minutes.
	// A property of the work cannot be a property of the caller, or it is a
	// property of whichever caller somebody remembered.
	recorded, resumed := LineageBank(graph, lineage, "")
	findings := ReadOpenFindings(graph, lineage)
	goal := strings.TrimSpace(growth.Goal)
	if goal == "" {
		goal = overrunGoal(node, partial, artifacts, gap, growth.State, recorded, findings, growth.Records...)
	}
	subtree, err := planRemainder(planCtx, goal, prefix)
	if err != nil {
		return 0, "", false, fmt.Errorf("replan overrun %s: %w", node.ID, err)
	}
	if len(subtree.Nodes) == 0 {
		return 0, "", false, nil
	}
	// The ceiling is read twice on purpose. On the way in it can only ask
	// whether there is room for anything at all, because until the plan exists
	// nobody knows how many nodes it holds; here it asks the exact question. The
	// second look is a recheck — the free caps again, never the paid one, which
	// was already asked and answered on the way in.
	exact := request
	exact.Adding = len(subtree.Nodes)
	exact.Rechecking = true
	exact.DailyBudgetUSD = 0
	if recheck, err := growJob(ctx, graph, nil, exact); err == nil && !recheck.Allow {
		return 0, "", true, nil
	}
	subtree = attachNeeds(subtree, repairSources(graph, node))

	sink := ""
	for _, spec := range subtree.Nodes {
		if spec.Parent == "" {
			sink = spec.ID
			break
		}
	}
	if sink == "" {
		return 0, "", false, fmt.Errorf("replan overrun %s: subtree has no sink", node.ID)
	}

	// The repair joins the exhausted node's own job when there is one; an
	// exhausted top-level job continues as a new top-level job instead, so
	// its finished result is announced like any other deliverable. That second
	// arm is a delivery law and not a convenience — announceNode says nothing
	// at all for a completion whose parent is not the root, and absorbable()
	// reads the same column — so a repair parented into the settled job would
	// finish correctly and be delivered to nobody.
	//
	// What the repair must NOT lose by standing beside its job is the job
	// itself: the finished siblings whose results it exists to assemble, and
	// the directory they wrote into. The first travels as consumer edges
	// (repairSources, above); the second travels as the id namespace, which is
	// what jobIDOf reads. Losing both is what made one measured repair say
	// "the workspace is empty ... the trace log holds no profile content",
	// refuse to fabricate, and hand back two of twelve finished answers.
	parent := node.Parent
	if parent == "" {
		parent = store.RootID
	}
	provenance := store.Provenance{
		Origin:      store.OriginSelf,
		SessionID:   node.Provenance.SessionID,
		Intent:      node.Provenance.Intent,
		Attachments: append([]string(nil), node.Provenance.Attachments...),
		// The worker the remainder was judged to belong to, journaled at the
		// splice exactly as the compiler's own choice is — once, durably, on the
		// subtree, so every leaf under it is claimed by what it was promised.
		Subharness: strings.TrimSpace(worker),
	}
	if err := graph.Splice(parent, subtree, provenance); err != nil {
		return 0, "", false, fmt.Errorf("replan overrun %s: %w", node.ID, err)
	}
	admitGrowth(graph, request, verdict, len(subtree.Nodes))
	// THE RESUMPTION IS JOURNALED, on the node that is actually resuming. A
	// continuation is a different node id from the leaf it continues, so the
	// claim-time resume row (which reads node.Attempt) can never fire for it —
	// and until this line the store's only account of a continuation was a new
	// node with a long brief, indistinguishable from a cold start. The row is
	// written against the sink because the sink is the node that carries the
	// whole remainder; the stream reads it and says so (`↻ … resumed`).
	if resumed > 0 {
		if err := graph.RecordLeafResumed(sink, store.LeafResumed{
			Turns: resumed, Files: artifacts,
		}); err != nil {
			log.Printf("note: could not journal that %s resumed %s's lineage: %v", sink, lineage, err)
		}
	}

	// Consumers that were waiting on the exhausted node now also wait for
	// the finished remainder. Only consumers that have not started are
	// rewired — one that already ran built its transcript from the partial,
	// and rewriting history is not on offer.
	edges, err := graph.ActiveEdges()
	if err == nil {
		for _, edge := range edges {
			if edge.From != node.ID || edge.Kind == store.Suggests {
				continue
			}
			if strings.HasPrefix(edge.To, prefix) {
				continue
			}
			_ = graph.AddEdge(sink, edge.To, store.FeedsInto)
		}
	}
	return len(subtree.Nodes), sink, false, nil
}

// OverrunLineage names the lineage a node belongs to and how deep into it the
// node already is: the id it was split from and its round number, or its own id
// and zero when it has never been split.
//
// It is exported for the same reason SplitContinuation is: the "-x" arithmetic
// is the id law and it lives here. A caller that wants to read a whole job's
// history — every round of it, under one namespace — asks for the base rather
// than parsing the suffix itself.
func OverrunLineage(nodeID string) (string, int) {
	if base, round, ok := splitOverrunID(nodeID); ok {
		return base, round
	}
	return nodeID, 0
}

// postGovernorNotice is the calm receipt a refused splice leaves on the work's
// own record: a governor stopping work quietly reads as work finishing, and the
// difference is what a reader opening that part needs to know.
//
// It is a record and not a conversation line (13.18) because what it describes
// is how the machinery divided the work — rounds spent, ceilings reached — and
// the sentence the person is owed is the delivery that follows it, which says
// what they got and carries the same "handing over what's done" in its own
// words.
func postGovernorNotice(graph *store.Store, node store.Node, cause, body string) {
	_, _ = thread.Record(graph, store.Message{
		Role:   store.RoleSystem,
		NodeID: node.ID,
		Body:   body,
		// The progress payload is what carries this out of the record and into
		// the headless stream, where the reader watching a run needs it most: a
		// governor stopping work is invisible there otherwise, and "still
		// waiting" over a job that has quietly stopped growing is the line that
		// gets a run killed by hand. See narrateOne in cmd/aforge/do.go.
		Progress: &store.MessageProgress{Phase: governorPhrase(cause), Latest: body},
	})
}

// governorPhrase is the two or three words the stream shows above the governor's
// own sentence. It is per cause so two different refusals on one node are two
// different lines rather than one repeated phase the stream deduplicates away —
// and it is written in the register of the product, which never names its own
// machinery to a person.
func governorPhrase(cause string) string {
	switch cause {
	case CauseStandstill:
		return "nothing is changing"
	case CauseFixedPoint:
		return "the same work again"
	case CauseRounds:
		return "no more rounds"
	case CauseCeiling:
		return "no more room"
	case CauseCovered:
		return "already covered"
	}
	return "handing over"
}

// ResumeDeferredOverruns admits journaled repairs after the rail is raised.
// The splice precedes the resolved event; after a crash, an existing prefix is
// enough evidence to resolve without planning or admitting a duplicate.
func ResumeDeferredOverruns(ctx context.Context, graph *store.Store, dailyBudgetUSD float64, planRemainder OverrunPlanFunc) (int, error) {
	pending, err := graph.PendingOverruns(100)
	if err != nil {
		return 0, err
	}
	resumed := 0
	for _, deferred := range pending {
		if err := ctx.Err(); err != nil {
			return resumed, err
		}
		node, ok, err := graph.Node(deferred.NodeID)
		if err != nil {
			return resumed, err
		}
		if !ok {
			if err := graph.ResolveOverrun(deferred); err != nil {
				return resumed, err
			}
			continue
		}
		exists, err := overrunPrefixExists(graph, deferred.Prefix)
		if err != nil {
			return resumed, err
		}
		if exists {
			if err := graph.ResolveOverrun(deferred); err != nil {
				return resumed, err
			}
			continue
		}
		spliced, _, capped, err := replanOverrun(ctx, graph, node, deferred.Partial, deferred.Gap, deferred.Artifacts,
			dailyBudgetUSD, deferred.Prefix, deferred.Subharness, Growth{Reason: GrowOverrun, State: deferred.State}, planRemainder)
		if err != nil {
			return resumed, err
		}
		// A capped repair is abandoned for good: resolve it so it stops
		// occupying the queue. A rail pause keeps waiting for consent.
		if capped {
			if err := graph.ResolveOverrun(deferred); err != nil {
				return resumed, err
			}
			continue
		}
		if spliced == 0 {
			return resumed, nil
		}
		if err := graph.ResolveOverrun(deferred); err != nil {
			return resumed, err
		}
		resumed += spliced
		// "splitting the remaining work -- 1 pieces queued" is the machinery
		// counting its own pieces. The person raised the rail and the work
		// carries on; what they hear next is the result, not the arithmetic.
		_, _ = thread.Record(graph, store.Message{
			Role:   store.RoleSystem,
			NodeID: node.ID,
			Body:   OverrunContinuationMessage(spliced),
		})
	}
	return resumed, nil
}

// OverrunContinuationMessage is the calm user receipt shared by immediate and
// rail-deferred splitting.
func OverrunContinuationMessage(pieces int) string {
	return fmt.Sprintf("splitting the remaining work -- %d pieces queued", pieces)
}

// SplitContinued reports that a node's summary is not its last word. A leaf that
// ran out of budget mid-thought completes with whatever it had said so far, and
// that sentence is a paragraph cut in half — "the file conflicted, let me clean
// up and run the test properly" — stamped with the receipt that says the rest was
// re-planned elsewhere. Read as a result it is a lie of tense: it narrates as
// present something the graph finished a minute later.
func SplitContinued(summary string) bool {
	return strings.Contains(summary, overrunSplitPrefix)
}

// SplitContinuation follows a stamped node into its own split namespace and
// returns the pieces that carry on from it, oldest round first. It is a read of
// ids and statuses and nothing else: the namespace is an id-index range, the
// sink of each round is that round's prefix exactly (SubtreeFromPlan gives the
// plan root the prefix itself), and a node that is itself a piece only ever
// continues into rounds above its own.
//
// It lives here because the id law lives here. Anyone who re-derived the "-x"
// arithmetic at the reading end would own a second copy of it, and the day the
// counter changes shape the second copy quietly starts answering with the stale
// half of the job — which is the failure it exists to end.
func SplitContinuation(graph *store.Store, node store.Node) ([]store.Node, bool) {
	if graph == nil || !SplitContinued(node.Summary) {
		return nil, false
	}
	base, after := node.ID, 0
	if marked, round, ok := splitOverrunID(node.ID); ok {
		base, after = marked, round
	}
	ids, err := graph.NodeIDsWithPrefix(base + overrunMarker)
	if err != nil {
		return nil, false
	}
	rounds := make(map[int]bool, len(ids))
	highest := 0
	for _, id := range ids {
		candidateBase, round, ok := splitOverrunID(id)
		if !ok || candidateBase != base || round <= after {
			continue
		}
		rounds[round] = true
		if round > highest {
			highest = round
		}
	}
	pieces := make([]store.Node, 0, len(rounds))
	for round := after + 1; round <= highest; round++ {
		if !rounds[round] {
			continue
		}
		piece, found, err := graph.Node(fmt.Sprintf("%s%s%d", base, overrunMarker, round))
		if err != nil || !found {
			continue
		}
		pieces = append(pieces, piece)
	}
	return pieces, len(pieces) > 0
}

func overrunPrefixExists(graph *store.Store, prefix string) (bool, error) {
	return graph.NodeIDExistsWithPrefix(prefix)
}

func nextOverrunPrefix(graph *store.Store, nodeID string) (string, error) {
	base := nodeID
	if marked, _, ok := splitOverrunID(nodeID); ok {
		base = marked
	}
	// Only ids inside this node's own split namespace can carry its rounds, and
	// that namespace is an id-index range rather than a reason to read the graph.
	candidates, err := graph.NodeIDsWithPrefix(base + overrunMarker)
	if err != nil {
		return "", err
	}
	maxRound := 0
	for _, candidate := range candidates {
		candidateBase, round, ok := splitOverrunID(candidate)
		if ok && candidateBase == base && round > maxRound {
			maxRound = round
		}
	}
	return fmt.Sprintf("%s%s%d", base, overrunMarker, maxRound+1), nil
}

func splitOverrunID(id string) (string, int, bool) {
	for offset := 0; offset < len(id); {
		index := strings.Index(id[offset:], overrunMarker)
		if index < 0 {
			return "", 0, false
		}
		index += offset
		start := index + len(overrunMarker)
		end := start
		for end < len(id) && id[end] >= '0' && id[end] <= '9' {
			end++
		}
		if end > start && (end == len(id) || id[end] == '-') {
			round, err := strconv.Atoi(id[start:end])
			if err == nil && round > 0 {
				return id[:index], round, true
			}
		}
		offset = start
	}
	return "", 0, false
}

func jobRootID(graph *store.Store, node store.Node) string {
	root := node
	for root.Parent != "" && root.Parent != store.RootID {
		parent, ok, err := graph.Node(root.Parent)
		if err != nil || !ok {
			return node.ID
		}
		root = parent
	}
	return root.ID
}

// repairSourceLimit bounds the fan-in of one repair. A job that finished forty
// parts is a job whose repair reads a digest of forty parts, and the entry node
// has a context window like every other leaf; twenty-four is the same order as
// the widest split this system plans and well inside what one brief can hold.
const repairSourceLimit = 24

// repairSources names the finished work a repair must be able to see.
//
// It is the exhausted or rejected node itself — which is what this always
// passed — and, when that node is a job whose parts already landed, those
// parts' own results. The distinction is the whole of §13.3's second half. A
// gate fires on a job ROOT (shouldGate admits nothing else), so the node handed
// to a repair is routinely a sink whose twelve children hold the work and whose
// own summary is a joined or reconciled view of it. A repair wired only to the
// sink is wired to one node's account of twelve; a repair the gate rejected the
// account of is then wired to nothing it can trust, and the honest ones say so
// and refuse to invent the rest.
//
// Only settled children with something to say are named. A part that failed,
// was cancelled, or is still running has no result to consume, and naming it
// would either fail the splice's dependency check or hand the repair an empty
// digest that reads as "there was nothing there".
func repairSources(graph *store.Store, node store.Node) []string {
	sources := []string{node.ID}
	if graph == nil {
		return sources
	}
	nodes, err := graph.SubtreeNodes(node.ID)
	if err != nil {
		return sources
	}
	children := make([]store.Node, 0, len(nodes))
	for _, candidate := range nodes {
		if candidate.ID == node.ID || candidate.Parent != node.ID {
			continue
		}
		if candidate.Status != store.Done || strings.TrimSpace(candidate.Summary) == "" {
			continue
		}
		children = append(children, candidate)
	}
	sort.SliceStable(children, func(i, j int) bool {
		if children[i].CreatedSeq != children[j].CreatedSeq {
			return children[i].CreatedSeq < children[j].CreatedSeq
		}
		return children[i].CreatedOrder < children[j].CreatedOrder
	})
	if len(children) > repairSourceLimit {
		children = children[:repairSourceLimit]
	}
	for _, child := range children {
		sources = append(sources, child.ID)
	}
	return sources
}

// attachNeeds points every entry node of a subtree at prior work, the same
// wiring continuity uses: an entry is a node with no in-subtree dependency,
// and each named source arrives as an ordinary dependency digest.
func attachNeeds(subtree store.Subtree, sources []string) store.Subtree {
	inSubtree := make(map[string]bool, len(subtree.Nodes))
	for _, spec := range subtree.Nodes {
		inSubtree[spec.ID] = true
	}
	for index, spec := range subtree.Nodes {
		entry := true
		for _, need := range spec.Needs {
			if inSubtree[need.NodeID] {
				entry = false
				break
			}
		}
		if !entry {
			continue
		}
		subtree.Nodes[index].Needs = entryNeeds(spec.Needs, sources)
	}
	return subtree
}

// entryNeeds is that wiring for one entry node, and it is the one
// implementation of it. The revision sentinel splices its own repairs a node at
// a time rather than as a subtree (see ApplyRevisionGoverned), and while it
// spelled the wiring for itself it spelled none of it: a replacement for a
// failed leaf was admitted with whatever inputs a model had named by integer and
// no edge at all to the work it was replacing. Both paths ask the same question
// now — what prior work must this node be able to see — and get the same answer
// from here.
//
// Anything already needed is left as it is: a source the planner named for
// itself is not named twice, and the caller's order is preserved so the digest
// reads in the order the work happened.
func entryNeeds(needs []store.Need, sources []string) []store.Need {
	existing := make(map[string]bool, len(needs)+len(sources))
	for _, need := range needs {
		existing[need.NodeID] = true
	}
	for _, source := range sources {
		if existing[source] {
			continue
		}
		existing[source] = true
		needs = append(needs, store.Need{NodeID: source, Kind: store.FeedsInto})
	}
	return needs
}
