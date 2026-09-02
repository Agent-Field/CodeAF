package exec

// Two readings of the project's own verification, one on either side of the
// work, for EVERY belt rather than for one of them.
//
// This glue used to live beside one belt, where only that belt could reach it.
// That made the measurement a property of one loop instead of a property of a
// run: the leaf belt in linear.go — the belt every node gets — took no reading
// at all, and a graded run on it left a store with no verification event in it.
// The three leaves of ink-grid-box-layout's s9 run journaled not one row.
//
// That silence is FAILSAFE.md's sixth failure exactly. AN ABSENCE IN THE RECORD
// IS NEVER A DIAGNOSIS; it is the four diagnoses nobody can tell apart — a
// project that declares no verification, a wall too short to afford a reading, a
// shell the preamble cannot be trusted in, and a command killed at its ceiling.
// Three of those cost a run nothing and the fourth costs an eighth of its wall,
// and the finished store spelled all four the same way.
//
// Everything here is a MEASUREMENT and never a gate. It never fails the leaf, it
// never changes what a loop does, and every way it can go wrong leaves the
// outcome exactly as it would have been. An error is journaled or dropped; none
// is ever returned.

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// The budget both readings are taken on, the floor under it and the arithmetic
// between them are verify.ReadingBudget. They used to be three constants inside
// one belt's own file, where only that belt could reach them; the delivery gate
// weighs the same photograph and takes one of its own where none exists, and two
// readers of one cap is how a number in this repository drifts. See PERF.md,
// "The verification photograph's budget".

// PhotographBefore is the reading the whole comparison is subtracted from, and
// it is the JOB's reading rather than this leaf's.
//
// A repair round is a new leaf, in a new workspace object, standing in a tree
// its own job has already changed. A leaf that photographed what IT found took
// the broken tree as its baseline, so every check an earlier round turned red
// subtracted to nothing and was never a finding again — textual's s5 run walked
// twenty project checks down to one across four rounds and raised nothing. So
// the baseline is looked up first: where this job already settled what its tree
// looked like — a reading, or the reason there could not be one — this leaf
// inherits that answer and spends nothing on reaching it again.
//
// A first reading of a tree runs BEFORE Workspace.WatchTree deliberately. A test
// runner leaves its own droppings — a .pytest_cache, a target/, a coverage
// file — and a reading taken after the tree was photographed would file every
// one of them as something this leaf produced. Taken first, they are part of the
// world the leaf arrived in, which is what they are. Every belt that calls this
// owes it that ordering.
//
// EVERY OUTCOME IS JOURNALED, including every way of having no reading. That is
// the whole repair of the s6 silence: a leaf spent five minutes and twenty-seven
// seconds on a reading that was killed at its ceiling, and the finished store
// held no row saying so, which read from outside exactly like a project that
// declares no verification at all.
//
// The worker's own wall is passed in rather than read off a belt, because the
// three belts hold it under three different names and a shared measurement must
// not have to know which one it is standing in. A nil history is ordinary — a
// leaf run outside a graph has no journal to write into — and the reading is
// taken and weighed identically with or without one.
//
// moved is the other half of the answer, and it is what the second reading is
// bought with: THE JOB HAS CHANGED FILES SINCE THIS READING WAS TAKEN. It used
// to say "this leaf inherited a reading", which is a fact about a lookup and not
// about a tree — every leaf after the first inherits — so a job whose leaves
// were ordered to change nothing bought a whole second reading at every one of
// them. The measured errand spent four of them, two minutes each, on a tree that
// never moved. See verify.TreeState.
func PhotographBefore(
	ctx context.Context, workspace *Workspace, history *store.Store,
	wall time.Duration, task Task,
) (reading verify.Reading, moved bool) {
	if workspace == nil {
		return verify.Reading{}, false
	}
	job := verify.JobKey(task.Goal)
	// What the job has produced or changed by the time this leaf starts. It is
	// the same record focusOf reads for the same reason: the rounds this leaf
	// continues are the only account of the work that exists yet.
	tree := verify.TreeState(workspace.Root(), changedSoFar(task))
	pace := verify.Pace{}
	if held, ok := verify.BaselineFor(workspace.Root(), job); ok {
		moved = !verify.TreeUnchangedSince(workspace.Root(), job, tree)
		// ONE ANSWER IS NOT INHERITED: a scoped reading killed at its ceiling
		// having named nothing. Every other refusal is a fact about the tree,
		// the project or the wall, and none of those move between rounds. That
		// one is a fact about a SIZE this program chose, and the cut measured
		// the pace that sizes it properly — so this round reads again over what
		// that pace affords rather than declining to look. textual s8 spent its
		// one reading on forty files, was cut naming nothing, and every round
		// after it inherited the silence.
		if !held.Retakeable() {
			journalReading(history, task, held, held.Before, "before the job's first change", true)
			return held, moved
		}
		pace = held.Pace()
	}
	reading = verify.Photograph(ctx, workspace.Root(), wall, focusOf(task), pace)
	verify.RememberBaseline(workspace.Root(), job, tree, reading)
	journalReading(history, task, reading, reading.Before, "before the job's first change", false)
	// A READING JUST TAKEN IS A READING OF THIS TREE, so nothing has moved since
	// it: whatever the job had already changed when this leaf arrived is inside
	// the photograph rather than after it. Only this leaf's own work can move
	// the tree from here, and that is what changed says at the other end.
	return reading, false
}

// leafMovedTheTree says this leaf changed the tree it was standing in, AND IT
// COUNTS A DELETION.
//
// The artifact list cannot: Workspace.Artifacts holds what the tree still has,
// deliberately, because that list is also what the person is shown and a
// deletion is not a file anybody can open. So a leaf whose whole job was to take
// a file out reported an empty list, which read here as a leaf that changed
// nothing — and the second reading, the one that would have caught what the
// removal broke, was never taken. The workspace's own before-and-after record
// knows the difference and is asked for it (Workspace.ArtifactFacts).
func leafMovedTheTree(workspace *Workspace, leaf string) bool {
	// The facts are the artifact list PLUS the deletions — Artifacts is these
	// with ArtifactDeleted taken out — so this is the older `len(Artifacts) > 0`
	// widened by exactly the one thing it could not see.
	return workspace != nil && len(workspace.ArtifactFacts(leaf)) > 0
}

// changedSoFar is the job's own account of what it has produced or changed
// before this leaf has done anything: the artifacts the rounds it continues left
// behind.
//
// It is the world's record and not a worker's claim — the same list the focus is
// widened by, and the same list the delivery gate holds — because the question
// it answers is whether the TREE moved, and only the tree can say.
func changedSoFar(task Task) []string {
	var record []string
	for _, input := range task.Inputs {
		record = append(record, input.Artifacts...)
	}
	return record
}

// focusOf is what this job is about, as paths: the files the person's own request
// names, and the files the work this leaf continues left behind.
//
// It is what decides HOW MUCH of the project a reading covers and WHERE it is
// taken — the checks next to the change rather than the whole repository, and
// the package of a monorepo the change is in rather than the fan-out at its root
// (verify.Focus, verify.Members). Both of those were the difference between a
// reading and no reading at all: textual's whole-repository suite is 793 seconds
// against a budget of 5m30s, and happy-dom's root command dies inside turbo
// having named no check of any package.
//
// It reads the request for NAMES rather than only for paths. Most requests
// spell no path at all: happy-dom's says "Implement `observe()`, `unobserve()`,
// `disconnect()` and `takeRecords()`" and names `IntersectionObserver`, and a
// focus built from paths alone was empty — so no package was chosen, the whole
// reading was taken at the repository root, and it was killed at its ceiling.
// verify.Locate matches those names, whole, against files the workspace holds.
//
// IT IS DERIVED FROM THE REQUEST AND NOT FROM THE DIFF, and that is forced
// rather than chosen. The baseline is the tree BEFORE the job's first change, so
// at the moment it is taken there is no diff to read; the request is the only
// account of what the work is about that exists yet. It is also the right one —
// the request is what every round of the job shares, so the scope it decides is
// the scope every round inherits, which is exactly what the comparison rule
// needs (verify.Reading.comparable). A continuation's inputs are read too,
// because the files an earlier round left behind are the same job's own record
// of where it has been working.
func focusOf(task Task) verify.Focus {
	focus := verify.Focus(verify.NamedSubjects(
		strings.Join([]string{task.Title, task.Goal, task.Brief}, "\n")))
	return append(focus, changedSoFar(task)...)
}

// PhotographAfter takes the second reading and writes what the two readings say
// onto the outcome.
//
// changed is the workspace's own account of whether THIS leaf moved anything.
// moved says the JOB had already moved the tree before this leaf's reading was
// taken (verify.TreeState, by way of PhotographBefore). Either is reason enough
// to take the second reading: a continuation that only rewrote its account still
// hands over a tree an earlier round may have broken, and the whole reason the
// baseline is the job's is so that breakage is still visible here. Neither is
// the case for a leaf that changed nothing in a tree nothing had changed — it
// cannot have regressed anything, and the reading it is holding is a reading of
// the very bytes in front of it, so it stands rather than being taken again for
// an eighth of the wall.
//
// It belongs at whatever single point a belt lands through, and it runs on an
// EXHAUSTED landing exactly as on a chosen one: a leaf ordered to stop still
// changed the tree it was standing in, and a reading nobody took is the silence
// this whole file exists to end.
func PhotographAfter(
	ctx context.Context, workspace *Workspace, history *store.Store,
	wall time.Duration, task Task, reading verify.Reading,
	changed, moved bool, outcome *Outcome,
) {
	if outcome == nil || workspace == nil {
		return
	}
	// A SECOND READING REPLACES THE FIRST; IT DOES NOT ADD TO IT.
	//
	// A leaf can now be photographed twice — once when it offers an answer, so
	// its own finding can be put to it while it is still standing, and once when
	// it actually lands (see selfclose.go). Every field below is THIS reading's
	// answer to a question the tree has already been asked, so a finding the
	// first reading raised and the second does not must stop being a finding.
	// Left standing, a name a leaf deleted and then restored would ride the
	// outcome to a gate that would buy a round to fix something that is already
	// fixed — which is the twenty-first chapter's rule, one seam earlier.
	//
	// Nil here is what it is everywhere else: NO CLAIM, nobody looked. Each
	// reader below leaves it nil when it could not compare, and writes an empty
	// answer only where it actually compared and found nothing.
	outcome.Baseline, outcome.Removed, outcome.Unbound = nil, nil, nil
	outcome.Regressed, outcome.OwnFailing = nil, nil
	// THE SYMBOL-LEVEL HALF IS SETTLED FIRST, AND IT IS SETTLED WHETHER OR NOT A
	// CHECK EVER RAN. It needs no runner, no budget and no declaration — only
	// the two readings of the tree — so it is the one measurement a project with
	// no suite, a wall too short for one, or a suite killed at its ceiling still
	// gets. See verify.Surface.
	surfaceRemoved(workspace, history, task, reading, outcome)
	// And the reading beside it that needs no baseline at all: the names this
	// work READS that nothing in the tree binds. It is taken here, on the same
	// seam and under the same rule — whatever happened to the check-level half —
	// because a project with no suite, a wall too short for one and a suite
	// killed at its ceiling are exactly the runs where nothing else would say a
	// word about it.
	unboundNames(workspace, history, task, outcome)
	// The photograph rides the outcome whether or not a reading was taken,
	// because WHAT WAS MEASURED AND WHAT NOBODY MEASURED ARE DIFFERENT FACTS
	// and only the run that stood there before the work can tell them apart.
	// A reading that could not be taken carries the sentence saying why, so
	// the gate is handed a reason rather than a void.
	outcome.Verification = reading
	if !reading.Taken {
		return
	}
	// The pre-existing reds are owed to the judge whether or not this leaf
	// changed anything: this job photographed the repository before the work
	// started, and the judge two processes away cannot rerun anything.
	if len(reading.Before.Failing) > 0 {
		outcome.Baseline = append(outcome.Baseline, "`"+reading.Before.Entrypoint.Command+
			"` was ALREADY failing at this commit before the run touched the workspace ("+
			describeChecks(reading.Before.Failing)+"). This is the repository's pre-existing "+
			"state, not this change's doing.")
	}
	if !changed && !moved {
		// THE TREE IS THE TREE THAT WAS READ, SO THE READING BEFORE THE WORK IS
		// THE READING OF THE FINISHED TREE. Nothing the job has done has reached
		// a file, so the suite would be run a second time over the identical
		// bytes for the identical roster — and the run is already holding it.
		// The measured errand bought that four times over 4,587 tests, two
		// minutes each, and every one of them was killed at its ceiling.
		//
		// It is journaled as inherited rather than passed over in silence,
		// because an absence in the record is never a diagnosis: a reading
		// nobody needed and a reading nobody took were the same missing row.
		if settled, ok := reading.OnAnUnchangedTree(); ok {
			reading = settled
			outcome.Verification = reading
			journalReading(history, task, reading, reading.Before, "on the finished tree", true)
		}
		return
	}
	// The SAME rung of the ladder the baseline was taken on, pinned rather than
	// re-derived — two readings taken with two different commands subtract to
	// noise, and re-deriving would hand a worker that edited its own test script
	// the power to choose what the after reading measures, which is the tamper
	// the photograph exists to catch — WITH THE RUN'S OWN CHECKS ADDED — the one thing about the
	// after reading that is allowed to differ from the before one. A scope is
	// decided before the work exists, so it cannot contain a test file the work
	// itself wrote; igel's s8 run scoped every reading to the one source file it
	// was told about and never saw the forty checks it had just written. The
	// record of what the run left behind is read for check files by the runner's
	// own convention and they join the selection; it widens and never narrows,
	// so Strategy.covers still reads the pair as comparable and a check that did
	// not exist before cannot be a regression. This lives here, not in one belt,
	// because a mechanism only the optional worker has is one the run does not.
	strategy := reading.Before.Strategy
	switch widened, added := strategy.WithChangedWork(workspace.Root(), outcome.Artifacts); {
	case added:
		strategy = widened
	case strategy.Scope == verify.ScopeWhole && reading.Partial:
		// A WHOLE READING THAT DID NOT FIT DOES NOT FIT TWICE. The first one
		// proved this project's suite is bigger than the wall; running it again
		// on the finished tree spends the same eighth of the wall to be killed
		// at the same ceiling, and the run ends holding no roster of the work it
		// just did. The change always resolves — it is a list of files that
		// exist — so the second reading is aimed at it. The pair stops being
		// comparable, which covers already refuses and Regressed already answers
		// nothing to; what it buys is the roster the coverage settlement spends.
		if narrowed, ok := verify.ChangedWorkStrategy(
			workspace.Root(), reading.Plan, outcome.Artifacts); ok {
			strategy = narrowed
		}
	}
	after, ok := readFinishedTree(ctx, workspace.Root(), strategy, reading, outcome.Artifacts)
	switch {
	case !ok:
		reading.Unread = "the finished tree could not be read: `" +
			strategy.Command + "` could not be started a second time"
	case after.Uncollected:
		// IT RAN AND IT NEVER GOT TO A CHECK. A suite that failed to collect is
		// not a suite that went red, and the two used to arrive here identical:
		// vitest printed no JSON, the shared vocabulary scraped one word out of
		// the error text, and a reading naming a single check was subtracted
		// against a baseline of twenty-eight. The runner's own words are quoted
		// so a person is told what actually broke.
		reading.Unread = "`" + strategy.Command + "` ran on the finished tree and its suite " +
			"failed to collect, so no check of it ran"
		if trouble := strings.TrimSpace(after.Error); trouble != "" {
			reading.Unread += ": " + trouble
		}
	case after.TimedOut && len(after.Reported) == 0:
		// IT RAN. That is a different fact from "nobody could read this tree",
		// and the sentence says which: a command that started, produced no
		// runner output this reader could name a check out of, and was killed at
		// its ceiling. A gate handed "was not read" cannot tell it from a
		// project that declares no verification at all.
		reading.Unread = "`" + after.Strategy.Command + "` ran on the finished tree and was " +
			"killed at its ceiling of " + reading.Budget.Round(time.Second).String() +
			" without naming a single check"
	default:
		// A CUT ROSTER IS STILL A ROSTER, AND THIS SIDE USED TO THROW IT AWAY.
		// The before half has kept what a killed runner had already streamed
		// since ink s7 (verify.photograph); this half discarded it, so ink's
		// after reading has come back `read: false, named: 0` in two whole
		// sweeps while the baseline of the same run, on the same command, named
		// 44 checks. Partial says the subtraction is refused —
		// Reading.comparable already reads it that way — and the roster is kept,
		// because "does a check for this exist" is answerable off a partial
		// roster and is the question the coverage settlement asks.
		reading.After, reading.AfterTaken = after, true
		if after.TimedOut {
			reading.Partial = true
		}
		outcome.Verification = reading
		outcome.Regressed = reading.Regressed()
		outcome.OwnFailing = reading.OwnFailing()
		journalReading(history, task, reading, after, "on the finished tree", false)
		return
	}
	// Not taken, and said so. The before half stands and the outcome keeps it;
	// what is lost is the subtraction, and a run that cannot say a check went
	// red must not be able to say one did not either. The result is journaled as
	// the runner actually left it — its exit status and whatever it printed —
	// rather than as a zero value, which is how a command that ran and exited 1
	// came to be written down as `exit: 0`.
	outcome.Verification = reading
	if after.Strategy.Empty() {
		after.Strategy = strategy
	}
	journalReading(history, task, reading, after, "on the finished tree", false)
}

// readFinishedTree runs the second reading, and RETAKES IT ON THE BASELINE'S OWN
// RUNG before reporting that the tree could not be read.
//
// The after reading is allowed to differ from the before one in exactly two
// ways — widened by the run's own work, or aimed at the diff where the whole
// suite did not fit — and both of those choose a command the baseline never
// proved could run. A selection this program built is the one thing that can be
// wrong here in a way a retake fixes: a runner handed a file it cannot run
// alone, a path with a space in it, a package whose config the narrowed
// invocation dropped. The baseline's rung is the one command this job has
// watched work.
//
// So a derived rung that will not start, or that is killed having named nothing,
// falls back to it — once, and only when it is actually a different command.
// What comes back is whichever attempt said more.
func readFinishedTree(
	ctx context.Context, root string, strategy verify.Strategy,
	reading verify.Reading, record []string,
) (verify.Result, bool) {
	after, ok := verify.RunReading(ctx, root, strategy, reading.Budget)
	if (ok && len(after.Reported) > 0) || reading.Before.Strategy.Command == strategy.Command {
		return after, ok
	}
	retaken, retook := verify.RunReading(ctx, root, reading.Before.Strategy, reading.Budget)
	if !retook || (len(retaken.Reported) == 0 && ok) {
		return after, ok
	}
	return retaken, retook
}

// journalReading writes one reading — or one reading that could not be taken —
// into the run's own record.
//
// A FAIL-SAFE THAT LEAVES NO RECORD CANNOT BE AUTOPSIED (FAILSAFE.md clause 4).
// Every way of having no reading is written down here, because the absence of
// the event used to be the only spelling of four different facts: a project that
// declares no verification, a wall that could not afford a reading, a shell that
// could not run one, and a command killed at its ceiling. They cost a run
// nothing, nothing, nothing and five and a half minutes respectively, and an
// autopsy could not tell which had happened.
//
// It is a measurement of the run and never a gate: a store that refuses the row,
// or a belt running with no store at all, changes nothing about what the leaf
// does.
func journalReading(
	history *store.Store, task Task, reading verify.Reading,
	result verify.Result, when string, inherited bool,
) {
	if history == nil || strings.TrimSpace(task.StoreNodeID) == "" {
		return
	}
	// The rung that was actually reached. A taken reading carries it on its own
	// result; a refused one carries only the rung it got to.
	strategy := result.Strategy
	if strategy.Empty() {
		strategy = reading.Strategy
	}
	taken := reading.Taken
	if when == "on the finished tree" {
		taken = reading.AfterTaken
	}
	sample := result.Reported
	if len(sample) > store.VerificationSample {
		sample = sample[:store.VerificationSample]
	}
	_ = history.RecordVerification(task.StoreNodeID, store.VerificationReading{
		When:     when,
		Read:     taken,
		Why:      reading.Unread,
		Command:  strategy.Command,
		Declared: strategy.Declared,
		Runner:   strategy.Runner,
		Format:   string(strategy.Read),
		Source:   strategy.Source,
		// WHERE and HOW MUCH, beside WHAT and HOW. A roster of forty means one
		// thing for a small project read whole and another for a large one read
		// next to the change, and the row could not say which.
		Scope:   strategy.Scope,
		Package: strategy.Workdir,
		// And what a cut reading learned before it was cut: that its roster
		// stops where the clock did, and how long it took to get that far. The
		// second is the only thing this run ever measures about the PACE of the
		// machine it is on, and a ceiling derived from a wall knows nothing
		// about that until a reading is cut and says so.
		Partial:     reading.Partial && when == "before the job's first change",
		Elapsed:     reading.CutAfter,
		Uncollected: result.Uncollected,
		Trouble:     result.Error,
		ReadAsPlain: result.ReadAsPlain,
		Exit:        result.Exit,
		TimedOut:    result.TimedOut,
		Named:       len(result.Reported),
		Red:         len(result.Failing),
		// And how many of the names this roster dropped were rewritten rather
		// than deleted. It is a fact about the SUBTRACTION, so it exists only
		// on the half that has one to make.
		Replaced:  len(reading.Replaced()),
		Sample:    append([]string{}, sample...),
		Inherited: inherited,
	})
}

// describeChecks names a bounded handful of checks for a reader. It is
// presentation and it is bounded for the same reason every other list a model
// reads is: a suite with two hundred reds says nothing more than a suite with
// eight and a count.
//
// The delivery gate's own naming of failing checks spells the same
// eight-and-a-count rule, and the two are deliberately separate because one
// words a gate's refusal and this one words an outcome sentence; if a third ever
// appears, that is the moment the rule belongs in internal/verify instead of in
// each caller.
func describeChecks(names []string) string {
	if len(names) == 0 {
		return "no individually named tests"
	}
	if len(names) > 8 {
		return strings.Join(names[:8], ", ") + " and " + strconv.Itoa(len(names)-8) + " more"
	}
	return strings.Join(names, ", ")
}

// surfaceRemoved settles the public names this work deleted, and journals what
// it found either way.
//
// The comparison is scoped to the run's OWN RECORD of what it changed, which is
// what keeps it a measurement of the work rather than of the repository: a name
// that vanished from a file nobody touched vanished some other way, and a
// finding about that would be a finding about something this leaf never did.
//
// It is a measurement and never a gate. A baseline nobody took, a record that
// names no source file, a file that cannot be read — each leaves the outcome
// exactly as it arrived, which reads downstream as NO CLAIM and never as nothing
// removed.
func surfaceRemoved(
	workspace *Workspace, history *store.Store, task Task,
	reading verify.Reading, outcome *Outcome,
) {
	if len(reading.Surface) == 0 {
		return
	}
	// The settlement itself is verify.LostNames and is deliberately not spelled
	// here: the delivery gate takes the same one against the JOB's baseline,
	// because the leaf that lost a name and the node that gets judged are
	// routinely not the same node, and two spellings of one settlement would be
	// two answers to what a run deleted.
	removed, compared := verify.LostNames(workspace.Root(), reading.Surface, outcome.Artifacts)
	if compared == 0 {
		return
	}
	if len(removed) > 0 {
		outcome.Removed = removed
	}
	journalSurface(history, task, compared, removed)
}

// journalSurface writes what the symbol-level reading found, INCLUDING when it
// found nothing.
//
// A row saying "sixteen files were compared and no public name was lost" is the
// difference between a run that checked and a run whose reader never ran, and
// those two were the same silence in every store this mechanism was built from
// (FAILSAFE.md clause 4). It is a measurement: a store that refuses the row
// changes nothing about what the leaf does.
func journalSurface(history *store.Store, task Task, compared int, removed []string) {
	if history == nil || strings.TrimSpace(task.StoreNodeID) == "" {
		return
	}
	_ = history.RecordSurface(task.StoreNodeID, store.SurfaceReading{
		Compared: compared,
		Lost:     len(removed),
		Names:    verify.SurfaceNamed(removed),
	})
}

// unboundNames settles the names this work reads that nothing in the tree binds,
// and journals what it found either way.
//
// The reading is scoped to the run's OWN RECORD of what it changed, which is
// what keeps it a measurement of the work rather than of the repository: a
// dangling reference in a file nobody touched was dangling before this run
// started.
//
// It is a measurement and never a gate. No workspace, a record that names no
// source this program reads, a scope holding any construct that binds names
// dynamically — each leaves the outcome exactly as it arrived, which reads
// downstream as NO CLAIM and never as nothing unbound.
//
// The settlement itself is verify.UnboundReferences and is deliberately not
// spelled here: the delivery gate re-takes the same one against the tree it is
// judging, because the leaf that wrote the reference and the node that gets
// judged are routinely not the same node, and two spellings of one settlement
// would be two answers to what a run left dangling.
func unboundNames(workspace *Workspace, history *store.Store, task Task, outcome *Outcome) {
	found := verify.UnboundReferences(workspace.Root(), outcome.Artifacts)
	if len(found) > 0 {
		outcome.Unbound = verify.UnboundWords(found)
	}
	journalUnbound(history, task, found)
}

// journalUnbound writes what the reading found, INCLUDING when it found nothing.
//
// A row saying "the changed sources were read and every name they use is bound"
// is the difference between a run that looked and a run whose reader never ran,
// and those two were the same silence in every store this mechanism was built
// from (FAILSAFE.md clause 4).
func journalUnbound(history *store.Store, task Task, found []verify.UnboundName) {
	if history == nil || strings.TrimSpace(task.StoreNodeID) == "" {
		return
	}
	reading := store.UnboundReading{Found: len(found)}
	for _, one := range verify.UnboundNamed(found) {
		reading.Names = append(reading.Names, store.UnboundSite{
			Name: one.Name, Where: one.Where(), Ground: one.Ground})
	}
	_ = history.RecordUnbound(task.StoreNodeID, reading)
}
