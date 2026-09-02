package revision

// The acceptance settlement: the half of the gate that asks whether anything
// CHECKS what the person asked for.
//
// The settlement lane fixed the refusal path — a gate that names a gap and is
// overruled by a rule. The sweep that followed it proved the fix and uncovered
// this: two of five graded runs ended at exit 0, reward 0, because the gate had
// said YES. ofetch passed a deliverable opening "All 56 tests pass" at 41 of 47
// hidden tests; happy-dom passed "All tests pass (31/31)" at 13 of 14. In both
// the gate accepted THE WORKER'S CLAIM ABOUT TESTS THE WORKER WROTE ITSELF,
// which is FAILSAFE clause 2 broken in the one place the whole verdict is
// decided. See docs/design/gate/ACCEPTANCE.md.
//
// Everything here answers one question — is there a check that exercises this
// behaviour — and it answers it from two sources, neither of which is prose: the
// check declarations in the worker's own diff, and the identities the project's
// own test runner printed. The deliverable's sentence about its tests is read by
// nothing in this file.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/provider/pool"
	"github.com/Agent-Field/aforge-v2/internal/router"
	"github.com/Agent-Field/aforge-v2/internal/shaped"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/verify"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// HeldPoints is THE checklist this delivery is judged against: the behaviours
// the request states, from the spec the round carries or from the job's own
// remembered list, each still grounded in what was actually asked for.
//
// It is a function because two readers need the identical answer and had been
// computing it in one place only. The settlement below reads it to decide what
// nothing exercises; the delivery gate reads it to decide which quotes a
// refusal may be built on (subject.go). A checklist that differed between the
// two would be a gate refusing a verdict for citing a behaviour the settlement
// was about to count.
func HeldPoints(evidence Evidence, grounds Grounds) []plan.Point {
	if points := Held(evidence.Accept, grounds); len(points) > 0 {
		return points
	}
	return Held(ChecklistFor(verify.JobKey(grounds.Intent)), grounds)
}

// Held is the checklist the gate may actually hold somebody to: the points whose
// quotation is grounded in what this run promised before it began working.
//
// It is the SAME invariant, read by the SAME code, that decides whether a
// review's finding may buy a repair round — citationGrounded, through the three
// doors grounding.go opens. A point the request does not carry is a requirement
// this system wrote for itself after reading its own prompt, and holding a
// worker to one is the failure the whole grounding rule exists to prevent.
//
// It is applied where the checklist is USED and not only where it was written.
// A list that reached the gate down any other route — a rehydrated plan, a
// spliced repair, a future caller nobody has written yet — is still weighed
// against the person's own words before it can convict anything.
func Held(points []plan.Point, grounds Grounds) []plan.Point {
	if len(points) == 0 || grounds.Empty() {
		return nil
	}
	index := grounds.index()
	held := make([]plan.Point, 0, len(points))
	for _, point := range points {
		if point.Empty() || !citationGrounded(point.Quote, index) {
			continue
		}
		held = append(held, point)
	}
	if len(held) == 0 {
		return nil
	}
	return held
}

// CheckEvidence is every check identity this run can prove exists, and it is
// deliberately assembled from the world rather than from the account of it.
//
// Two sources, in the order of what they cost. The worker's own diff declares
// checks by shape and costs a scan of a string the gate already holds; the
// project's own runner names identities in its output and costs nothing extra,
// because the reading was taken anyway. A DELIVERABLE'S PROSE IS NOT A SOURCE:
// "All 56 tests pass" is a sentence about tests the same worker wrote, and
// weighing it is the exact defect this file exists to close.
//
// Empty means nothing can be concluded about coverage. That is a real answer and
// it is the honest one for a project that declares no verification and a worker
// that derived no diff — a capability that cannot work is ABSENT, not broken.
func CheckEvidence(ctx context.Context, evidence Evidence, job string) []string {
	return checkEvidence(evidence, jobReading(ctx, nil, "", evidence, job))
}

// checkEvidence is the same answer read off a reading the caller already holds.
//
// The split exists so the settlement pays for ONE reading and journals ONE row.
// jobReading is memoised against the job, so calling it twice was free in wall
// time and not free in the record: the second call would write a second row
// saying the same thing, and a journal that repeats itself is one an autopsy
// has to learn to discount.
func checkEvidence(evidence Evidence, reading verify.Reading) []string {
	var checks []string
	if patch := evidence.patchSource(); patch != "" {
		added, _ := verify.PatchChecks(patch)
		checks = append(checks, added...)
	}
	// AND THE CHECKS THE RUN WROTE ARE READ OFF THE TREE WHEN THERE IS NO DIFF
	// TO READ THEM OUT OF. This is the same source as the line above it — the
	// declarations the work itself made — reached by the one route that does
	// not depend on a worker having derived a patch.
	//
	// It is here because the diff route is, today, never taken: Evidence.Patch
	// is filled from outcome.Account.Patch and nothing in this tree sets
	// Outcome.Account any more, so every belt reached this reader with the
	// patch half empty. A real issue measured it — a run wrote 195 lines of
	// pytest covering seven stated behaviours, the project's own suite could
	// not collect, the mapping call went out carrying a roster of nothing, and
	// the gate declared six behaviours unexercised three times over a fix whose
	// tests were green (2026-09-02, deepseek-v4-flash, Human-Agent-Society/reef
	// #145). THE WORK'S OWN CHECKS ARE THE ONE SOURCE THAT DOES NOT NEED THE
	// PROJECT TO COLLECT, and they were the one source that never arrived.
	checks = append(checks, declaredByTheRun(evidence)...)
	roster := reading.After.Reported
	if !reading.AfterTaken {
		// The before roster is the fallback and not a substitute: it names the
		// checks that existed when the work STARTED, so it can say a behaviour
		// was already covered and can never say a new one is. That asymmetry is
		// right — a point already exercised by the repository's own suite is
		// exercised — and it is why this is read at all rather than skipped.
		roster = reading.Before.Reported
	}
	if reading.Taken {
		checks = append(checks, roster...)
	}
	return verify.Subtract(checks, nil)
}

// declaredByTheRun is every check identity the run's own new and changed check
// files declare, read out of the tree the delivery stands in.
//
// The record is the artifact registry SETTLED AGAINST THE FILESYSTEM by the
// time any of this is asked (Evidence.completeAgainstTheWorld), so a path here
// is a file that exists, and verify.OwnChecks is the same rule the mapping's
// own grounding door uses to decide which of them are checks — one reading of
// "this is a test file", not two.
//
// The identities come back as the runner-independent bare names
// verify.DeclaredChecks reads, which is what the diff route already produced,
// so nothing downstream learns a second spelling. A name that the project's own
// roster ALSO reported arrives twice and is deduplicated by the Subtract above.
//
// It is bounded twice, by the same two numbers the mapping's body reader
// spends: a repository of generated fixtures may not turn a cheap scan into an
// unbounded one, and a file this could not read declares nothing — which leaves
// the point uncovered, the safe direction.
func declaredByTheRun(evidence Evidence) []string {
	root := strings.TrimSpace(evidence.Workspace)
	if root == "" || len(evidence.Artifacts) == 0 {
		return nil
	}
	budget := mappingBodyBudget
	var checks []string
	for _, path := range verify.OwnChecks(root, evidence.Artifacts) {
		if budget <= 0 {
			break
		}
		whole := filepath.Join(root, filepath.FromSlash(path))
		info, err := os.Stat(whole)
		if err != nil || info.IsDir() || info.Size() > mappingBodyBytes {
			continue
		}
		body, err := os.ReadFile(whole)
		if err != nil {
			continue
		}
		budget -= len(body)
		checks = append(checks, verify.DeclaredChecks(string(body))...)
	}
	return checks
}

// jobReading is THE PHOTOGRAPH THE JOB TOOK, not the one this round's worker
// happened to take.
//
// It is the same law verify.BaselineFor states for the regression comparison,
// read from the coverage side. A job is worked by more than one worker: ofetch
// s7 opened with `bare`, which photographs, and every repair round after it ran
// under `linear`, which does not — so rounds two, three and four arrived at the
// gate with an empty reading and the coverage question could not be asked of
// them at all, while the roster the first round measured was sitting in the
// job's own memory. A READING IS A FACT ABOUT A TREE AND A JOB, AND EVERY ROUND
// OF THAT JOB MAY READ IT.
//
// The round's own reading wins whenever it has one: it is the later measurement,
// and a round that ran the suite has measured the tree the gate is judging. The
// baseline is what stands in when it has none, and it can only ever say a
// behaviour was ALREADY covered — never that a new one is — which is the same
// asymmetry the before-roster fallback below is written for.
// AND WHATEVER IT DOES, IT SAYS SO IN THE RECORD. Every reading the gate takes,
// inherits or refuses is journaled here, because the gate is the ONLY reader on
// the generalist's path and a run whose leaves never photograph was leaving no
// row at all. igel s9 and ink s9 put every node on the generalist, reached their
// gates, and finished with ZERO verification events in the store — not a reading,
// not a refusal, nothing — while ofetch s9 on the identical binary journaled four,
// because one of its nodes happened to run under `bare`. From outside, a run that
// read nothing and a run whose reader is silent are the same run. FAILSAFE.md
// clause 4.
func jobReading(ctx context.Context, graph *store.Store, nodeID string,
	evidence Evidence, job string,
) verify.Reading {
	// The worker's own reading stands and is already journaled where it was
	// taken; a second row for it would say the same thing twice.
	if evidence.Verification.Taken {
		return evidence.Verification
	}
	if strings.TrimSpace(evidence.Workspace) == "" || strings.TrimSpace(job) == "" {
		journalGateReading(graph, nodeID, verify.Reading{Unread: "the gate holds no workspace " +
			"to read, so no reading of the delivered tree could be taken"}, verify.Result{}, false)
		return evidence.Verification
	}
	if held, ok := verify.BaselineFor(evidence.Workspace, job); ok {
		if held.Taken {
			journalGateReading(graph, nodeID, held, held.Before, true)
			return held
		}
		if held.Retakeable() {
			// A scoped reading cut at its ceiling measured this project's pace
			// and nothing else. The gate reads again over what that pace
			// affords rather than inheriting a silence about a size this
			// program chose. See verify.Reading.Retakeable.
			if deadline, timed := ctx.Deadline(); timed {
				retaken := verify.Photograph(ctx, evidence.Workspace, time.Until(deadline),
					gateFocus(evidence), held.Pace())
				verify.RememberBaseline(evidence.Workspace, job, retaken)
				journalGateReading(graph, nodeID, retaken, retaken.Before, false)
				return retaken
			}
		}
		// The job already found out it could not read this project, and why.
		// Paying for that answer twice is what the baseline memory exists to
		// stop; the reason it holds is carried up so the verdict can say it.
		if strings.TrimSpace(evidence.Verification.Unread) == "" {
			journalGateReading(graph, nodeID, held, held.Before, true)
			return held
		}
		journalGateReading(graph, nodeID, evidence.Verification, evidence.Verification.Before, true)
		return evidence.Verification
	}
	// NOBODY HAS LOOKED AT ALL, AND THE READING IS THE GATE'S TO HOLD. Not every
	// worker photographs: textual s7 went through the planner's fallback, ran
	// every node under the generalist, and reached its gates with no reading in
	// the store — no roster, no reason, nothing. A verdict reached there is a
	// verdict reached on the deliverable's own prose, which is the defect this
	// whole mechanism is named after.
	//
	// It is taken on the wall this gate has left, through the same arithmetic
	// the worker is held to, and it is REMEMBERED AGAINST THE JOB — so it costs
	// one reading per job rather than one per round, exactly like the worker's.
	deadline, timed := ctx.Deadline()
	if !timed {
		// A budget is a share of a wall, and there is no wall here to take a
		// share of. That is a refusal like any other and it is written down
		// like one: it costs nothing and it is the difference between a gate
		// that could not look and a gate nobody asked to.
		journalGateReading(graph, nodeID, verify.Reading{Unread: "the gate's own work had no " +
			"deadline, so there was no wall to size a reading against"}, verify.Result{}, false)
		return evidence.Verification
	}
	taken := verify.Photograph(ctx, evidence.Workspace, time.Until(deadline),
		gateFocus(evidence), verify.Pace{})
	verify.RememberBaseline(evidence.Workspace, job, taken)
	journalGateReading(graph, nodeID, taken, taken.Before, false)
	return taken
}

// journalGateReading writes one row saying what the gate's reading of the
// delivered tree was, or why there was not one.
//
// It is internal/exec/bare's own journal, moved to the reader that has the
// store: same event, same fields, and a `when` that says which reader took it,
// so an autopsy can tell the worker's photograph of the tree it arrived in from
// the gate's photograph of the tree it is judging.
//
// It is a MEASUREMENT AND NEVER A GATE. A nil store, a node with no id, a store
// that refuses the row — none of them change a verdict, and none of them are
// worth failing a delivery over.
func journalGateReading(
	graph *store.Store, nodeID string, reading verify.Reading, result verify.Result, inherited bool,
) {
	if graph == nil || strings.TrimSpace(nodeID) == "" {
		return
	}
	strategy := result.Strategy
	if strategy.Empty() {
		strategy = reading.Strategy
	}
	sample := result.Reported
	if len(sample) > store.VerificationSample {
		sample = sample[:store.VerificationSample]
	}
	_ = graph.RecordVerification(nodeID, store.VerificationReading{
		When:        "on the tree the gate is judging",
		Read:        reading.Taken,
		Why:         reading.Unread,
		Command:     strategy.Command,
		Declared:    strategy.Declared,
		Runner:      strategy.Runner,
		Format:      string(strategy.Read),
		Source:      strategy.Source,
		Scope:       strategy.Scope,
		Package:     strategy.Workdir,
		ReadAsPlain: result.ReadAsPlain,
		Exit:        result.Exit,
		TimedOut:    result.TimedOut,
		Named:       len(result.Reported),
		Red:         len(result.Failing),
		Sample:      sample,
		Partial:     reading.Partial,
		Elapsed:     reading.CutAfter,
		Uncollected: result.Uncollected,
		Trouble:     result.Error,
		Inherited:   inherited,
	})
}

// gateFocus is what the gate knows this job is about: the files the request
// named and the files the work left behind. It is the same question
// internal/exec/bare answers from the task, asked by the reader that has the
// record instead of the brief.
func gateFocus(evidence Evidence) verify.Focus {
	focus := verify.Focus(append([]string{}, evidence.Named...))
	return append(focus, evidence.Artifacts...)
}

// ── the checklist and the finding are the JOB'S ──────────────────────────────
//
// Both used to live on one node's spec, which is one round of one job, and that
// is where ofetch s7 lost them. Its first round mapped fifty-four points, found
// eighteen unexercised, named them and bought a repair — and rounds two, three
// and four hold ZERO mapping rows. The continuation nodes are planned afresh, so
// their specs carry no checklist; the gates that judged them raised prose gaps
// about the deliverable's wording; and the run ended at 41 of 47 with the same
// four defaults untested that round one had named out loud.
//
// THE CHECKLIST IS A READING OF THE REQUEST, AND EVERY ROUND OF A JOB HAS THE
// SAME REQUEST. So it is remembered against the job — the identical key
// verify.BaselineFor remembers a tree's photograph against, and for the identical
// reason — and a round whose own spec carries none inherits it.
//
// And so is what the job is still short of. A behaviour measured once as
// exercised by nothing does not stop being unexercised because the next round's
// worker took no reading; it stops when a measurement says a check now covers
// it. Carrying the set is what makes each round's brief say what REMAINS rather
// than restating the whole list or, as s7 did, saying nothing at all.

// rememberedJobs bounds how many jobs this holds a checklist for at once. It is
// verify's rememberedTrees for the same reason and at the same size: a checklist
// is a few dozen short strings, sixteen concurrent jobs is more than any surface
// in this program opens, and past it the oldest is dropped — which costs a round
// its inherited checklist rather than giving it a wrong one.
const rememberedJobs = 16

type jobAcceptance struct {
	points []plan.Point
	// open is what the last measurement said nothing exercises, and stated is
	// how many behaviours were weighed to find it. Both are zero until a
	// mapping has actually been taken.
	open []string
	// weak is the other half of that measurement: the behaviours a check names
	// and no assertion weighs, already rendered with the observables nothing
	// asserted. It stands across rounds exactly as open does, and for the same
	// reason — a round that took no reading does not close a finding an earlier
	// reading proved.
	weak    []string
	stated  int
	settled bool
}

var checklists = struct {
	mutex sync.Mutex
	held  map[string]jobAcceptance
	order []string
}{held: map[string]jobAcceptance{}}

// RememberChecklist records the behaviours this job is judged against, for every
// round of it that follows.
func RememberChecklist(job string, points []plan.Point) {
	if strings.TrimSpace(job) == "" || len(points) == 0 {
		return
	}
	checklists.mutex.Lock()
	defer checklists.mutex.Unlock()
	held := admitJob(job)
	held.points = append([]plan.Point{}, points...)
	checklists.held[job] = held
}

// RememberChecklistForRequest is the same memory, keyed from the request itself,
// for the seam that has the checklist BEFORE any gate does.
//
// It exists because the memory was only ever written by the gate, and a job
// whose first node never reaches one leaves it empty. ofetch s10 is that shape
// exactly: the planner read 47 points onto `task-2`'s spec and journaled them,
// `task-2` was handed over without a delivery gate, and the continuation
// `task-2-x1` — planned afresh, so carrying no `Accept` — reached the only gate
// of the run with no checklist at all. Its event holds `pass: true` and nothing
// else: no mapping, no finding, no `unmeasured`. The coverage question was not
// answered wrongly; it was never asked, and the run left at 42 of 47.
//
// THE CHECKLIST IS A READING OF THE REQUEST, so the moment it is read is the
// moment it can be remembered, and every round of the job — gate or no gate —
// inherits it from there.
func RememberChecklistForRequest(request string, points []plan.Point) {
	RememberChecklist(verify.JobKey(request), points)
}

// ChecklistFor is what an earlier round of this job settled it would be judged
// against, or nothing.
func ChecklistFor(job string) []plan.Point {
	if strings.TrimSpace(job) == "" {
		return nil
	}
	checklists.mutex.Lock()
	defer checklists.mutex.Unlock()
	return checklists.held[job].points
}

// RememberUnexercised records what the LAST MEASUREMENT of this job found
// nothing exercising — including the empty answer, which is the news that a
// round closed the gap and is exactly what must not be lost.
func RememberUnexercised(job string, open, weak []string, stated int) {
	if strings.TrimSpace(job) == "" {
		return
	}
	checklists.mutex.Lock()
	defer checklists.mutex.Unlock()
	held := admitJob(job)
	held.open, held.weak = append([]string{}, open...), append([]string{}, weak...)
	held.stated, held.settled = stated, true
	checklists.held[job] = held
}

// UnexercisedFor is the finding this job is still carrying: what a measurement
// found nothing exercising, and how many behaviours were weighed to find it.
func UnexercisedFor(job string) (open, weak []string, stated int) {
	if strings.TrimSpace(job) == "" {
		return nil, nil, 0
	}
	checklists.mutex.Lock()
	defer checklists.mutex.Unlock()
	held := checklists.held[job]
	return held.open, held.weak, held.stated
}

// ForgetChecklists drops everything remembered. Its only callers are tests,
// which share a process and would otherwise inherit one another's jobs.
func ForgetChecklists() {
	checklists.mutex.Lock()
	defer checklists.mutex.Unlock()
	checklists.held = map[string]jobAcceptance{}
	checklists.order = nil
}

// admitJob makes room for a job and returns what is already held for it. The
// caller holds the lock.
func admitJob(job string) jobAcceptance {
	held, known := checklists.held[job]
	if known {
		return held
	}
	checklists.order = append(checklists.order, job)
	for len(checklists.order) > rememberedJobs {
		delete(checklists.held, checklists.order[0])
		checklists.order = checklists.order[1:]
	}
	return jobAcceptance{}
}

// mapPrompt asks one question and takes no position on the answer.
//
// It is written to make the NEGATIVE cheap to say. A model asked to match things
// up will match everything up, so the instruction that carries the weight is the
// one that names the failure mode: a check whose name is about a neighbouring
// behaviour is not a check for this one, and the whole finding this call feeds
// is the difference between "counts hook errors as failures" and "does not retry
// when a hook throws".
const mapPrompt = `You are given a list of behaviours a request asked for, and a list of the checks
that exist in a project. For each behaviour, say which check exercises it.

A check exercises a behaviour when running that check would FAIL if the behaviour
were absent or wrong. A check whose name is about something adjacent does not
count: "counts hook errors as failures" does not exercise "hook failures are not
retried", and "keyed by origin" does not exercise "two origins are tracked
independently". If you are not sure a check would catch the behaviour breaking,
say there is none.

Answer with one bare JSON object and nothing else — no code fence around it and
no sentence before or after it. Give one entry per behaviour, in the order they
were given, with the check's exact name or an empty string when no check
exercises it:
{"mapped": [{"point": "<the behaviour, copied>", "check": "<the check's name, or empty>"}]}`

var mapSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "mapped": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "point": {"type": "string"},
          "check": {"type": "string"}
        },
        "required": ["point", "check"],
        "additionalProperties": false
      }
    }
  },
  "required": ["mapped"],
  "additionalProperties": false
}`)

// MapChecks asks which check exercises which behaviour, and returns the mapping
// in the order the points were given.
//
// A call that cannot be made, cannot be read, or comes back short answers with
// NOTHING MAPPED rather than with everything mapped. That is the fail-safe
// direction for this particular question and it is the opposite of the gate's
// own: an unanswerable gate must not hold a finished deliverable hostage, but an
// unanswerable coverage question that resolved to "all covered" would silently
// restore exactly the behaviour this mechanism replaces. Unmapped buys a repair
// round; it never ships a wrong answer as a right one.
func MapChecks(ctx context.Context, settings config.Config, client *pool.Client,
	node store.Node, points []plan.Point, checks []string, workerModel string,
) []store.ExercisedPoint {
	mapping := make([]store.ExercisedPoint, 0, len(points))
	for _, point := range points {
		mapping = append(mapping, store.ExercisedPoint{Point: point.Behaviour})
	}
	if client == nil || len(points) == 0 || len(checks) == 0 {
		return mapping
	}
	var body strings.Builder
	body.WriteString("The behaviours the request asked for:\n")
	for index, point := range points {
		fmt.Fprintf(&body, "%d. %s\n", index+1, point.Behaviour)
	}
	body.WriteString("\nThe checks that exist:\n")
	for _, check := range checks {
		body.WriteString(check + "\n")
	}
	mapCtx := settings.Context(router.WithAvoidModel(ctx, workerModel), "gate")
	mapCtx = provider.WithCall(mapCtx, provider.ClassPlanAudit)
	// The same tag the gate's own verdict carries, because this is the gate
	// answering half of its own question and a reader of the model-call log
	// wants the two beside each other.
	mapCtx = provider.WithCallTag(mapCtx, "gate")
	mapCtx = pool.WithSpendNode(mapCtx, node.ID)
	// And on the log, so this half of the gate's question sits beside the other.
	mapCtx = provider.WithCallNode(mapCtx, node.ID)
	var answer struct {
		Mapped []store.ExercisedPoint `json:"mapped"`
	}
	// The seam owns the wire: the schema travels only where a router carries
	// it, a cut answer is continued and a prose one re-asked once, and what
	// comes back is either the object or a typed refusal — the mapping never
	// reads the model's text itself.
	response, err := askVerdict(mapCtx, client, []ai.Message{
		{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: mapPrompt}}},
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: body.String()}}},
	}, mapSchema, &answer)
	if err != nil || response == nil {
		if shaped.Unreadable(err) {
			provider.Report(mapCtx, provider.VerdictFormatFailure)
		} else {
			provider.Report(mapCtx, provider.VerdictProviderFailure)
		}
		return mapping
	}
	provider.Report(mapCtx, provider.VerdictVerifiedSuccess)
	// Merged by position, and only where the answer named a check that actually
	// exists. A model that invents a check name has answered about a project it
	// imagined, and admitting it would let a hallucinated test satisfy a real
	// behaviour.
	known := make(map[string]string, len(checks))
	for _, check := range checks {
		known[strings.ToLower(strings.TrimSpace(check))] = check
	}
	for index, row := range answer.Mapped {
		if index >= len(mapping) {
			break
		}
		if named, ok := known[strings.ToLower(strings.TrimSpace(row.Check))]; ok {
			mapping[index].Check = named
		}
	}
	return mapping
}

// Unexercised is the acceptance finding: the behaviours the request stated that
// nothing in the project's own verification touches.
//
// SOURCED, NOT MECHANICAL, for the reason Regressions is. There is no citation
// to weigh here — the person asked for the behaviour, and whether a check exists
// for it is a measurement of the repository rather than a reading of the words.
// Grounding it would refuse it every time, which is the shape of the two runs
// that shipped a deliverable claiming every test passed while a whole family of
// stated behaviours was exercised by nothing at all.
//
// THE FINDING IS GROUPED BY THE LINE OF THE REQUEST ITS POINTS WERE READ FROM,
// and that grouping is the bound on its size. A checklist is derived per stated
// behaviour, so one sentence listing four defaults becomes four points — which
// is right for the mapping, because four defaults are four things a check either
// exercises or does not, and wrong for the finding, because a repair round aimed
// at four halves of one sentence is four rounds aimed at one sentence. The
// request's own lines are the grouping the person themselves wrote, so the
// number of things this finding can ask for is bounded by the number of things
// the person said, and by nothing this program chose. Past that the list is
// named eight and counted, which is regressionsNamed, stated once in this
// package and read here rather than restated. See PERF.md, "The acceptance
// finding's size".
//
// ok is false when every point is exercised, when there were no points, or when
// nothing could be measured — and the caller then judges exactly as it did
// before this existed.
func Unexercised(points []plan.Point, mapping []store.ExercisedPoint, grounds Grounds) (judgment Judgment, ok bool) {
	found := coverage{
		missing: unexercisedPoints(mapping), weak: unassertedPoints(mapping),
		missingLines: groupUnexercised(points, mapping, grounds),
		weakLines:    groupUnasserted(points, mapping, grounds),
	}
	if len(found.missing) == 0 && len(found.weak) == 0 {
		return Judgment{}, false
	}
	judgment = unexercisedFinding(found, len(points))
	judgment.Exercises = mapping
	return judgment, true
}

// unexercisedPoints is the finding one entry per acceptance point: every
// behaviour of the checklist that no check exercises, in the order the points
// were stated. See coverage for why this and not the grouping is the finding.
func unexercisedPoints(mapping []store.ExercisedPoint) []string {
	points := make([]string, 0, len(mapping))
	for _, row := range mapping {
		if behaviour := strings.TrimSpace(row.Point); behaviour != "" && strings.TrimSpace(row.Check) == "" {
			points = append(points, behaviour)
		}
	}
	return points
}

// unassertedPoints is the other half the same way: every behaviour a check
// names and no assertion weighs, each carrying the observables nothing
// asserted, which is the one fact that closes it.
func unassertedPoints(mapping []store.ExercisedPoint) []string {
	points := make([]string, 0, len(mapping))
	for _, row := range mapping {
		behaviour := strings.TrimSpace(row.Point)
		if behaviour == "" || strings.TrimSpace(row.Check) == "" || len(row.Unasserted) == 0 {
			continue
		}
		points = append(points, behaviour+observablesMark+strings.Join(namedObservables(row.Unasserted), ", "))
	}
	return points
}

// observablesMark separates a weakly-exercised behaviour from the observables
// nothing asserted, and it is stated once because two readers spell it: the
// finding writes it, and citedBehaviours reads it back off an entry that has
// been through the journal.
const observablesMark = " — observables never asserted: "

// citedBehaviours is what a finding may CITE: the behaviours themselves, with
// the observables this program appended taken back off.
//
// A citation is the person's own words. The observables are the person's words
// too, but the sentence around them is this program's — and a finding whose
// citation is a sentence it wrote itself is the exact shape the grounding rule
// exists to refuse, even where (as here) the finding is Sourced and no citation
// is weighed for it.
func citedBehaviours(entries []string) []string {
	cited := make([]string, 0, len(entries))
	for _, entry := range entries {
		if before, _, found := strings.Cut(entry, observablesMark); found {
			entry = before
		}
		if entry = strings.TrimSpace(entry); entry != "" {
			cited = append(cited, entry)
		}
	}
	return cited
}

// coverage is the acceptance finding's two halves, each read twice.
//
// missing and weak are the finding ITSELF, one entry per acceptance point.
// missingLines and weakLines are only how the brief SAYS it: the same
// behaviours folded into the lines of the request they were read from, which is
// the grouping a worker can act on one sentence at a time.
//
// THE TWO USED TO BE ONE, AND THE ONE WAS THE GROUPING. A one-line brief
// collapsed six unexercised behaviours into a single entry, and the person was
// told "1 behaviour the request states has no check" over six of them
// (2026-09-01, deepseek-v4-flash). The count a person reads, the names a round
// is bought for and the names that go SPENT are all the point count, because a
// point is the thing a check is matched to and a request line is not. The
// grouping keeps the one job it was built for and loses the three it had
// silently taken on.
type coverage struct {
	missing, weak           []string
	missingLines, weakLines []string
}

// coverageOf is a finding being rebuilt from lists that have already been
// through the journal, where the request's own lines are no longer in hand. The
// points are then all there is, and they say it themselves.
func coverageOf(missing, weak []string) coverage {
	return coverage{missing: missing, weak: weak, missingLines: missing, weakLines: weak}
}

// unexercisedFinding is the finding itself, built from the behaviours and from
// nothing else.
//
// It is a function rather than four lines inside Unexercised because the finding
// has to be REBUILDABLE. When the gate is already failing the coverage gap joins
// the verdict as text, and when the judge's own citation is then refused the
// measured half has to stand back up on its own — same words, same citations,
// same bound. Two places that each wrote the sentence would be two sentences.
func unexercisedFinding(found coverage, stated int) Judgment {
	missing, weak := found.missing, found.weak
	named := found.missingLines
	if len(named) > regressionsNamed {
		named = named[:regressionsNamed]
	}
	namedWeak := found.weakLines
	if len(namedWeak) > regressionsNamed {
		namedWeak = namedWeak[:regressionsNamed]
	}
	var gap strings.Builder
	if len(named) > 0 {
		// THE COUNT LEADS, AND IT IS THE POINT COUNT. This is the gap's first
		// line, which is the line the closing narration quotes back to a person
		// on a run that is short of nothing else — so a sentence that opened
		// with "The request asks for behaviours" handed them a reservation with
		// no size on it at all.
		verb := "have no check that exercises them"
		if len(missing) == 1 {
			verb = "has no check that exercises it"
		}
		fmt.Fprintf(&gap, "%s the request states %s. "+
			"Nothing in this project's own verification would fail if each of these were "+
			"absent or wrong, so nothing that has been run says whether the work does them:\n",
			countedBehaviours(len(missing)), verb)
		for _, point := range named {
			gap.WriteString("no check exercises: " + point + "\n")
		}
		if len(found.missingLines) > len(named) {
			fmt.Fprintf(&gap, "And %d more.\n", len(found.missingLines)-len(named))
		}
	}
	// AND THE SECOND HALF NAMES WHAT TO ASSERT ON. A round told only that a
	// behaviour is uncovered writes another check that visits it; a round told
	// which identifier nothing weighed has been given the one fact that closes
	// the finding.
	if len(namedWeak) > 0 {
		// The verb agrees with the count, because this sentence is the gate's
		// own line on a run where nothing else is wrong and a person reads it
		// once.
		verb := "are"
		if len(weak) == 1 {
			verb = "is"
		}
		fmt.Fprintf(&gap, "%s the request states %s asserted by no check. "+
			"A check names each of these and runs it, and then asserts nothing about the "+
			"identifiers the request spelled — so it would pass whether the behaviour is "+
			"right or wrong:\n", countedBehaviours(len(weak)), verb)
		for _, point := range namedWeak {
			gap.WriteString("asserted by no check: " + point + "\n")
		}
		if len(found.weakLines) > len(namedWeak) {
			fmt.Fprintf(&gap, "And %d more.\n", len(found.weakLines)-len(namedWeak))
		}
	}
	gap.WriteString("Write the check for each, and make it pass.")
	// AND THE LAST LINE IS THE SCORE. A repair round is aimed at what REMAINS,
	// and a brief that restates the whole list every round tells the worker
	// nothing about whether the last round moved anything. ofetch s7 named
	// eighteen behaviours in round one and then said nothing at all in rounds
	// two, three and four, so the run's own record of its progress against its
	// own checklist was a single sentence at minute nine.
	if stated > 0 {
		fmt.Fprintf(&gap, " %d of the %s this request states still have no check that asserts them.",
			len(missing)+len(weak), countedBehaviours(stated))
	}
	// The citations are the SENTENCE's, so they travel with the gap the person
	// reads rather than beside it: one citation per thing the brief asks a
	// worker to go and check. The finding's own lists below are the points.
	cited := citedBehaviours(append(append([]string{}, named...), namedWeak...))
	return Judgment{
		Pass: false, Gaps: strings.TrimSpace(gap.String()), Quote: joinCitations(cited),
		Citations: cited, Sourced: true, Checked: true, Unexercised: missing,
		Unasserted: weak, Stated: stated,
	}
}

// countedBehaviours spells the denominator of that score once, so a job with one
// stated behaviour never reads "1 of the 1 behaviours".
func countedBehaviours(stated int) string {
	if stated == 1 {
		return "1 behaviour"
	}
	return fmt.Sprintf("%d behaviours", stated)
}

// measuredHalf is this judgement with everything a judge wrote taken off it: the
// coverage finding, alone, as though it had been raised on its own.
//
// It is what a refused citation leaves standing. The admission rules weigh where
// a review got its WORDS, and a coverage gap has none to weigh — nobody has to
// ask for the behaviours they stated to be checked — so refusing the judge's
// span settles nothing about it. igel s6 ended with three behaviours nothing
// exercised and a refusal of a sentence about a test file, and the second took
// the first down with it.
//
// The mapping rides along because it is the evidence the finding is a conclusion
// of, and the grounds because every door downstream weighs against the same ask.
func (j Judgment) measuredHalf() Judgment {
	rebuilt := unexercisedFinding(coverageOf(j.Unexercised, j.Unasserted), j.Stated)
	rebuilt.Exercises, rebuilt.Grounds, rebuilt.Unmeasured = j.Exercises, j.Grounds, j.Unmeasured
	return rebuilt
}

// groupUnexercised collects the behaviours nothing exercises and folds the ones
// that came from a single line of the request into one entry.
//
// The mapping is positional — MapChecks answers in the order the points were
// given — so a row's point is the point at the same index. Where the two lists
// have drifted apart the row's own copy of the behaviour is used, which is what
// it carries the text for.
func groupUnexercised(points []plan.Point, mapping []store.ExercisedPoint, grounds Grounds) []string {
	lines := requestLines(grounds)
	var order []int
	grouped := map[int][]string{}
	loose := 0
	for index, row := range mapping {
		behaviour := strings.TrimSpace(row.Point)
		if behaviour == "" || strings.TrimSpace(row.Check) != "" {
			continue
		}
		// A point with no locatable line is its own group. It is keyed by a
		// descending counter so it can never collide with a line number, and
		// so two unlocatable points stay two findings rather than merging into
		// a sentence neither of them says.
		line := -1
		if index < len(points) {
			line = requestLine(points[index].Quote, lines)
		}
		if line < 0 {
			loose--
			line = loose
		}
		if _, seen := grouped[line]; !seen {
			order = append(order, line)
		}
		grouped[line] = append(grouped[line], behaviour)
	}
	entries := make([]string, 0, len(order))
	for _, line := range order {
		entries = append(entries, strings.Join(grouped[line], "; "))
	}
	return entries
}

// groupUnasserted is the same grouping for the other half of the measurement:
// the behaviours a check NAMES and no assertion WEIGHS, folded by the line of
// the request they were read from and each carrying the observables nothing
// asserted.
//
// The observables of the points in one group are unioned and named once, because
// the group is one sentence to a reader and one thing to fix. They are bounded
// by observablesNamed for the same reason the finding itself is bounded: a line
// is read to learn what SHAPE the gap has, and a list past four names says it no
// better.
func groupUnasserted(points []plan.Point, mapping []store.ExercisedPoint, grounds Grounds) []string {
	lines := requestLines(grounds)
	var order []int
	grouped := map[int][]string{}
	observables := map[int][]string{}
	seen := map[int]map[string]bool{}
	loose := 0
	for index, row := range mapping {
		behaviour := strings.TrimSpace(row.Point)
		if behaviour == "" || strings.TrimSpace(row.Check) == "" || len(row.Unasserted) == 0 {
			continue
		}
		line := -1
		if index < len(points) {
			line = requestLine(points[index].Quote, lines)
		}
		if line < 0 {
			loose--
			line = loose
		}
		if _, held := grouped[line]; !held {
			order = append(order, line)
			seen[line] = map[string]bool{}
		}
		grouped[line] = append(grouped[line], behaviour)
		for _, observable := range row.Unasserted {
			if observable = strings.TrimSpace(observable); observable != "" && !seen[line][observable] {
				seen[line][observable] = true
				observables[line] = append(observables[line], observable)
			}
		}
	}
	entries := make([]string, 0, len(order))
	for _, line := range order {
		named := namedObservables(observables[line])
		entry := strings.Join(grouped[line], "; ")
		if len(named) > 0 {
			entry += observablesMark + strings.Join(named, ", ")
		}
		entries = append(entries, entry)
	}
	return entries
}

// namedObservables is what one entry says out loud about what nothing asserted:
// the identifiers, trimmed, deduplicated in the order they were read, and cut to
// observablesNamed. A list past four names says the shape of the gap no better.
func namedObservables(observables []string) []string {
	named := make([]string, 0, len(observables))
	seen := make(map[string]bool, len(observables))
	for _, observable := range observables {
		if observable = strings.TrimSpace(observable); observable == "" || seen[observable] {
			continue
		}
		seen[observable] = true
		named = append(named, observable)
		if len(named) == observablesNamed {
			break
		}
	}
	return named
}

// requestLines is the person's own request, one whitespace-normalised entry per
// line that says anything. It is the same normalisation the grounding rule
// applies, so a quotation that grounds against the request can be located in it.
func requestLines(grounds Grounds) []string {
	var lines []string
	for _, text := range grounds.texts() {
		for _, line := range strings.Split(text, "\n") {
			if key := citationKey(line); key != "" {
				lines = append(lines, key)
			}
		}
	}
	return lines
}

// requestLine finds the line a quotation was read from, or -1.
//
// A quotation is a sequence of spans (see quotationGrounded), and the FIRST span
// that says anything is the one that locates it: a quotation that elides across
// a line break belongs to the line it starts on, which is the line a person
// reading the finding would look at.
func requestLine(quote string, lines []string) int {
	for _, segment := range elision.Split(quote, -1) {
		key := citationKey(segment)
		if key == "" {
			continue
		}
		for index, line := range lines {
			if strings.Contains(line, key) {
				return index
			}
		}
		return -1
	}
	return -1
}

// WeakenedChecks is the third mechanism: a check that STOPPED EXISTING between
// the two photographs.
//
// The verification photograph SETTLEMENT §4 built sees a check that turned red.
// It cannot see one that was deleted, renamed or skipped — and taking out the
// test that was failing is the cheapest way there is to make a suite green, so
// the one thing a coverage rule must not do is leave that door open while
// closing every other one.
//
// Two sources, either of which convicts. A check declaration on a REMOVED line
// of the worker's own diff is direct evidence and needs no run at all; a name
// the runner reported before the work and did not report after it is the same
// fact measured. Both are already subtractions that cancel a move — see
// verify.PatchChecks and verify.Reading.Vanished — so a check that changed file
// or was merely re-indented is not here.
//
// Sourced, for the same reason as its two siblings: nobody has to ask for their
// tests to keep existing.
func WeakenedChecks(removed, vanished []string) (judgment Judgment, ok bool) {
	gone := verify.Subtract(append(append([]string{}, removed...), vanished...), nil)
	if len(gone) == 0 {
		return Judgment{}, false
	}
	named := gone
	if len(named) > regressionsNamed {
		named = named[:regressionsNamed]
	}
	gap := "This work removed checks that existed before it: " + joinCitations(named) + "."
	if len(gone) > len(named) {
		gap += fmt.Sprintf(" And %d more.", len(gone)-len(named))
	}
	gap += " A check that was there and is not was deleted, renamed or skipped; " +
		"whatever it was holding is now held by nothing."
	return Judgment{
		Pass: false, Gaps: gap, Quote: joinCitations(named),
		Citations: named, Sourced: true, Checked: true, Finding: FindingRemovedCheck,
	}, true
}

// Measured says a reading of the world exists to answer the coverage question
// with — that somebody looked, whatever they found.
//
// It is the distinction the s5 sweep turned on, and the one the first version of
// this file collapsed. A READING THAT WAS TAKEN AND NAMED NOTHING IS STILL A
// TAKEN READING: the project was asked how it checks itself, it answered, and
// nothing it printed exercises anything the request asked for. That is a
// finding. Only a project that declares no verification at all, run by a worker
// that derived no diff, leaves the question unanswerable — and that is
// Unmeasured, which is a different sentence about a different fact.
func Measured(evidence Evidence) bool {
	return evidence.Verification.Taken || strings.TrimSpace(evidence.patchSource()) != ""
}

// settleAcceptance is the whole of this mechanism as the gate reaches it, and it
// reaches it on EVERY verdict.
//
// It used to be asked only of a deliverable the model judge had just called
// whole, on the reasoning that a failing gate buys its repair round anyway. Ten
// delivery gates across the s5 sweep failed, so the coverage question was never
// asked once and no run in the sweep holds a mapping at all. The reasoning was
// wrong twice over: a repair round is aimed at the gap the gate NAMED, so a
// round bought for a missing branch name closes the branch name and leaves every
// unexercised behaviour exactly where it was; and a mechanism that only runs on
// the happy path is a mechanism nothing exercises, which is the failure this
// file is named after.
//
// It returns the verdict unchanged when there is nothing to settle: no
// checklist, or every point exercised.
// settleUnmeasured says on the verdict whether the world behind this delivery
// was read, and it is asked of EVERY verdict.
//
// NOBODY LOOKED IS NOT NOTHING WRONG, and it is not a finding either. A project
// that declares no verification and a worker that derived no diff leave the
// question unanswerable, and a gate that failed every such delivery would fail
// every piece of prose this program writes. So the verdict SAYS SO, and a reader
// of the record can tell an unchecked delivery from a checked one — carried on
// the verdict rather than logged, because a fail-safe that does not reach the
// person watching is decoration (FAILSAFE.md clause 3).
//
// TWO SILENCES, AND ONLY ONE OF THEM IS NOBODY'S FAULT. A project that declares
// no verification cannot be read and nothing follows from it. A project that
// declares one this run could not read has left the question UNANSWERED, and a
// delivery that passes over that is a delivery nothing checked — ink s7 exited 0
// at 13 of 25 that way. Only the second sets Unreadable, which is what turns a
// pass into a partial at the door.
func settleUnmeasured(verdict Judgment, evidence Evidence, reading verify.Reading) Judgment {
	if Measured(evidence) || reading.Taken {
		return verdict
	}
	verdict.Unreadable = reading.Declared() || evidence.Verification.Declared()
	verdict.Unmeasured = "this project declares no verification this run could read, so no " +
		"check could be matched to what the request asked for"
	if verdict.Unreadable {
		verdict.Unmeasured = "nothing in this project's verification could be read"
		if why := strings.TrimSpace(firstOf(reading.Unread, evidence.Verification.Unread)); why != "" {
			verdict.Unmeasured += ": " + why
		}
	}
	// AND A RUN WHOSE OWN CHECKS RAN AND SETTLED IS NOT A RUN NOTHING CHECKED.
	//
	// Unreadable is the fact that turns a pass into a partial, and it is the
	// right fact about a project whose suite nobody could read. It is not the
	// whole fact about a delivery whose own checks ran and every one of them
	// came back settled: something was checked, it is simply not the thing the
	// coverage question asks about. A person told "partial" over work whose
	// tests are green has been told something false, and told nothing at all is
	// worse — so the receipt says WHICH of the two happened, in one sentence,
	// on the record and in what they read.
	//
	// The fallback is the worker's STRUCTURED account of what it ran, settled
	// against the two lists the world produced, and never the deliverable's
	// prose about its own tests — that sentence is weighed by nothing in this
	// file and this does not become the exception.
	if verdict.Unreadable && checkedByItsOwnTests(evidence) {
		verdict.Unreadable = false
		verdict.Receipt = CheckedNotMeasured
	}
	return verdict
}

// CheckedNotMeasured is the receipt a delivery earns where the coverage
// question had no measurement to answer from and the work's own checks ran and
// settled. It is stated once because three readers spell it: the settlement
// sets it, the journal keeps it, and the closing narration says it.
const CheckedNotMeasured = "checked by tests, coverage not measured"

// checkedByItsOwnTests says the work ran checks of its own and every one of
// them settled, with nothing the world measured standing against them.
//
// Three conditions and all three are facts rather than claims. The account is
// the worker's structured record of what it ran and what each run found, which
// is the same reader plan's remainder judge already spends (exec.Account.
// Verified); OwnFailing is the checks this run wrote that are red, read off the
// two photographs; Regressed is what this run turned red that was green before
// it. A run with an empty account answers no, because "nothing was checked" and
// "everything passed" are the two answers a silence used to collapse into.
func checkedByItsOwnTests(evidence Evidence) bool {
	return evidence.Account.Verified() &&
		len(evidence.OwnFailing) == 0 && len(evidence.Regressed) == 0
}

// The three sentences the settlement gives instead of a coverage finding, each
// stated once because the journal keeps them and a person reads them.
//
// They are Unmeasured-shaped on purpose: the field already means "the gate
// answered and this half of its question had no evidence to answer from", and
// all three of these are that fact with a different reason attached. A separate
// boolean per reason would be three fields nothing reads together.
const (
	// NoBehaviourAsked is a request that asks for things to be DONE rather than
	// for something to BE a certain way. Nothing a repository could run would
	// fail if a command that has already been run were not run.
	NoBehaviourAsked = "the request states what the run is to do rather than what the " +
		"finished work is to be, so there is no behaviour a check could exercise"
	// NoCodeChanged is a run that left the tree as it found it. A check
	// exercises something that exists; a run that produced nothing to exercise
	// has nothing for one to be missing from.
	NoCodeChanged = "the work changed no code, so there is no check to ask for"
	// CoverageUnread is the reading that was not taken, was cut, or could not
	// collect, on a run whose own change declares no checks either. AN EMPTY OR
	// UNREADABLE ROSTER IS NOT EVIDENCE THAT NO CHECK EXISTS.
	CoverageUnread = "the project's checks could not be read and the work's own diff names " +
		"none, so no behaviour could be matched to a check"
)

// sayUnmeasured records why the coverage half of this verdict was not settled,
// and never overwrites a sentence that is already there.
//
// settleUnmeasured's reading is about the WORLD — whether anything could be
// read at all — and it is both the older and the more particular fact. Where
// both hold, a verdict should say the one that is about this run's own tree.
func sayUnmeasured(verdict Judgment, why string) Judgment {
	if strings.TrimSpace(verdict.Unmeasured) == "" {
		verdict.Unmeasured = why
	}
	return verdict
}

// unaskable is the verdict of a delivery THE COVERAGE QUESTION COULD NOT BE PUT
// OF AT ALL, and it takes Unreadable back off.
//
// Unreadable says the project declared a way of checking itself and this run
// could not read it, and it is what turns a pass into a partial — the floor
// that stops a delivery nothing checked from leaving as done (FAILSAFE clause
// 5). It is settled above this, before anything has classified what the request
// even asked for, and that ordering was wrong for exactly two runs: one whose
// request states no behaviour of any finished thing, and one that changed no
// code. NEITHER OF THEM HAS A COVERAGE QUESTION TO ANSWER, so a suite that
// timed out somewhere else in the repository says nothing whatever about them,
// and holding such a delivery partial over it charges an errand for a silence
// that could not have acquitted it either.
//
// The read-only errand is the case that measured it: "run this command and
// report the final line, change no files", over a project whose whole-suite
// reading was cut at its ceiling, failed Whole() and left as `partial` — and
// there was never a check that could have exercised anything it asked for.
//
// The third silence is NOT this one. Where the request states behaviours over a
// change that was made, an unreadable suite leaves a real question unanswered
// and Unreadable stands, which is where ink s7 exited 0 at 13 of 25.
func unaskable(verdict Judgment, why string) Judgment {
	verdict.Unreadable = false
	verdict.Unmeasured = why
	return verdict
}

// codeChanged says this run left something behind that a check could exercise,
// AND IT ANSWERS YES WHEREVER IT CANNOT TELL.
//
// A run that changed nothing has nothing for a check to be missing from, and
// asking which repository test exercises it is a question with no answer: the
// errand that measured this bought two coverage-mapping calls carrying ~127k
// prompt tokens each, on a run whose whole instruction was to change no files.
//
// But AN EMPTY RECORD IS ONLY A FACT WHERE SOMEBODY WAS RECORDING, which is the
// distinction Evidence.Observed exists for and the one this rule would
// otherwise collapse. A caller holding no outcome, a judgement reached off a
// rehydrated row, a test that builds a bare Evidence — none of them are runs
// that demonstrably changed nothing, and skipping the coverage question over
// them would switch the whole mechanism off by omission. So the unobserved run
// is mapped exactly as it was before this rule existed.
//
// Where somebody was recording, the two sources are the world rather than an
// account of it: the change's own text where a diff was derived, and the run's
// record settled against the filesystem otherwise. verify's own split of a
// record into checks and sources decides whether a path counts, so "which of
// these files is part of the project" is read one way here and in the mapping's
// grounding door rather than two — and a path outside the workspace is in
// neither list, which is what keeps a run's own sidecars from reading as a
// change to the project.
func codeChanged(evidence Evidence) bool {
	if !evidence.Observed || strings.TrimSpace(evidence.patchSource()) != "" {
		return true
	}
	root := strings.TrimSpace(evidence.Workspace)
	if root == "" {
		return len(evidence.Artifacts) > 0
	}
	return len(verify.ChangedSources(root, evidence.Artifacts)) > 0 ||
		len(verify.OwnChecks(root, evidence.Artifacts)) > 0
}

// rosterSpeaks says the reading this settlement holds is one the coverage
// question can be asked of — that somebody looked and the looking finished.
//
// It is asked only where the check list came back EMPTY, and there it is the
// whole difference between two answers wearing one silence. A project with a
// runner and no tests answers "there is no check for this", and that is a
// measurement and a finding. A suite that could not collect, was killed at its
// ceiling, or printed nothing a reader recognised answers NOTHING, and reading
// that as "there is no check for this" is the gate's own doctrine turned on
// itself — silence in the records is evidence, never proof.
func rosterSpeaks(reading verify.Reading) bool {
	if !reading.Taken || reading.Partial {
		return false
	}
	result := reading.Before
	if reading.AfterTaken {
		result = reading.After
	}
	return !result.TimedOut && !result.Uncollected && strings.TrimSpace(result.Error) == ""
}

func settleAcceptance(ctx context.Context, settings config.Config, client *pool.Client,
	graph *store.Store, node store.Node, evidence Evidence, grounds Grounds,
	workerModel string, verdict Judgment,
) Judgment {
	// THE CHECKLIST IS THE JOB'S. A continuation is planned afresh and its spec
	// carries none, so a round that read only its own spec asked the coverage
	// question once and never again — ofetch s7 mapped fifty-four points in
	// round one and none in the three rounds that followed it.
	job := verify.JobKey(grounds.Intent)
	// THE WORLD IS READ ON EVERY GATE, AND THE CHECKLIST HAS NO SAY IN IT.
	//
	// This used to sit below the checklist, and returning early when a job
	// stated none took the reading with it — so a node with no checklist was
	// never asked whether the project could be read at all, `Unreadable` could
	// not be reached on that path, and the delivery gate settled Whole() over a
	// verification nothing had looked at. Exit 0, on a run where the one
	// question that could have said otherwise was never put.
	//
	// The two are different questions and only one of them is the checklist's.
	// A checklist governs whether COVERAGE can be settled — which behaviours are
	// exercised by nothing. Whether the WORLD was read is a fact about the
	// project and the run, true or false whether or not anybody wrote a
	// checklist, and it is what decides whether a pass is whole.
	reading := jobReading(ctx, graph, node.ID, evidence, job)
	verdict = settleUnmeasured(verdict, evidence, reading)
	points := HeldPoints(evidence, grounds)
	if len(points) == 0 {
		return verdict
	}
	RememberChecklist(job, points)
	// A GATE CHECKS THE REQUEST THE PERSON MADE, NOT THE REQUEST THE GATE
	// IMAGINES. The checklist is the person's whole ask and is remembered whole;
	// what is MAPPED is the half a check could exist for. An action of the run —
	// a command it runs, a report it makes, a step it takes — is exercised by no
	// repository test that could ever be written, and asking which one does is a
	// question with no answer that nonetheless buys a repair round to go and
	// find it. See plan.Behaviours.
	behaviours := plan.Behaviours(points)
	if len(behaviours) == 0 {
		return unaskable(verdict, NoBehaviourAsked)
	}
	// AND A CHECK EXERCISES SOMETHING THAT EXISTS. A run that left the tree as
	// it found it produced nothing for a check to be missing from, so there is
	// no coverage question to ask and no round to buy for the answer. The
	// delivery is still judged for substance by everything above this.
	if !codeChanged(evidence) {
		return unaskable(verdict, NoCodeChanged)
	}
	if !Measured(evidence) && !reading.Taken {
		// AND A FINDING ALREADY MEASURED STANDS UNTIL A MEASUREMENT CLOSES IT.
		// A behaviour an earlier round proved nothing exercises does not become
		// exercised because this round's worker took no reading. Dropping it
		// here is precisely how ofetch s7's eighteen named behaviours turned
		// into three rounds of prose about the deliverable's wording.
		if open, weak, stated := UnexercisedFor(job); len(open) > 0 || len(weak) > 0 {
			standing := unexercisedFinding(coverageOf(open, weak), stated)
			standing.Unmeasured, standing.Grounds = verdict.Unmeasured, grounds
			standing.Unreadable = verdict.Unreadable
			if verdict.Pass {
				return standing
			}
			verdict.Gaps = strings.TrimSpace(verdict.Gaps) + "\n\n" + standing.Gaps
			verdict.Unexercised, verdict.Stated = standing.Unexercised, standing.Stated
			verdict.Unasserted = standing.Unasserted
		}
		return verdict
	}
	// The mapping is asked for even when the roster is empty. MapChecks spends
	// no model call on an empty list — it answers "nothing mapped", which is
	// its own fail-safe direction — and the empty answer is the true one: a
	// reading that named no checks has no check that exercises anything. That
	// branch is the whole of FACT 2 in the s5 autopsy, where an empty roster
	// short-circuited to a note and fifty-two stated behaviours went unasked.
	//
	// AND IT IS ASKED ON EVERY ROUND. The mapping is one model call against a
	// reading the job has already paid for, and it is the only thing that can
	// SHRINK the set: a round that wrote the missing checks grows the roster
	// with names that map, and the next mapping is what notices. A round that
	// skipped the question left the set exactly where it was and called that
	// progress.
	checks := checkEvidence(evidence, reading)
	// AN EMPTY OR UNREADABLE ROSTER IS NOT EVIDENCE THAT NO CHECK EXISTS.
	//
	// This is the gate's own doctrine turned on itself — silence in the records
	// is evidence, never proof. A reading that was TAKEN and named nothing is a
	// measurement and maps: the project was asked how it checks itself, it
	// answered, and nothing it printed exercises anything. A reading that was
	// never taken, was cut at its ceiling, or could not collect answers NOTHING,
	// and mapping six stated behaviours against that silence declares them
	// unexercised over a fix whose own tests are green — three times, on a real
	// issue, while the deliverable in the same gate named the ten tests that
	// exercise them (2026-09-02, Human-Agent-Society/reef #145). The mapping
	// call went out carrying 1,598 tokens of roster; the same call on a project
	// that could be read carries ~127k.
	//
	// There is no measurement here, so the verdict says so and buys nothing —
	// and where the work's own checks ran and settled it says THAT too, because
	// a person owed an answer about green work must not be handed either a
	// partial or a silence. See CheckedNotMeasured.
	if len(checks) == 0 && !rosterSpeaks(reading) {
		verdict = sayUnmeasured(verdict, CoverageUnread)
		if strings.TrimSpace(verdict.Receipt) == "" && checkedByItsOwnTests(evidence) {
			verdict.Receipt = CheckedNotMeasured
		}
		return verdict
	}
	// AND WHAT COMES BACK GOES THROUGH THE SAME DOOR A CITATION GOES THROUGH.
	// The mapping is one model call, and a pairing it makes on vocabulary alone
	// is a behaviour declared covered by a check that is merely about the same
	// subject. See GroundMapping: a check may satisfy a point only where the
	// names the point spells distinctively are names the check spells too.
	//
	// AND THEN THROUGH THE SECOND DOOR, which asks a different question of the
	// pairings the first one let through: does the check ASSERT the behaviour,
	// or does it only visit it. See WeighAssertions — a check that calls
	// `write(expand=True)` and asserts the widget has some lines names every
	// symbol the behaviour names and weighs none of them.
	mapping := WeighAssertions(evidence.Workspace, job, evidence.Artifacts, behaviours,
		GroundMapping(evidence.Workspace, evidence.Artifacts, behaviours,
			MapChecks(ctx, settings, client, node, behaviours, checks, workerModel)))
	verdict.Exercises = mapping
	finding, unexercised := Unexercised(behaviours, mapping, grounds)
	// Measured either way. An empty set is the news that this round closed the
	// gap, and it is exactly the answer that must not be lost. The score it is
	// remembered with counts BEHAVIOURS: "2 of 7" over a checklist whose other
	// five entries are things the run did is a fraction of a number nobody
	// stated.
	RememberUnexercised(job, finding.Unexercised, finding.Unasserted, len(behaviours))
	if !unexercised {
		return verdict
	}
	if verdict.Pass {
		finding.Grounds = grounds
		return finding
	}
	// The gate was already failing. The coverage gap joins the gap that was
	// named rather than replacing it, so the repair brief carries both — and it
	// joins as TEXT only. Its citations are not merged in and Sourced is not
	// set: a sourced finding is admitted with no citation weighed, and letting
	// the judge's own prose ride into a round on the back of that exemption is
	// exactly the laundering the admission rules exist to prevent.
	verdict.Gaps = strings.TrimSpace(verdict.Gaps) + "\n\n" + finding.Gaps
	// And the finding travels as a LIST beside the paragraph, which is the whole
	// of what "journaled, said, and able to buy its own round" needs. Merged into
	// prose it was none of the three: nothing recorded it as a finding, the
	// stream's line is the first line of the gap and never reached it, and the
	// round it rode on could be — and was — refused out from under it. See
	// Judgment.Unexercised and measuredHalf.
	verdict.Unexercised, verdict.Stated = finding.Unexercised, finding.Stated
	verdict.Unasserted = finding.Unasserted
	return verdict
}

// firstOf is the first of these sentences that says anything. Two readings can
// each hold a reason — the round's own and the job's — and the reason a person
// is owed is whichever one exists.
func firstOf(sentences ...string) string {
	for _, sentence := range sentences {
		if trimmed := strings.TrimSpace(sentence); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
