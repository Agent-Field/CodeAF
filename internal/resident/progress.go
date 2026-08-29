// What a round actually moved, weighed against what the job is about.
//
// THE DEFECT THIS ANSWERS. ink s10 was given a 5400-second wall and spent all
// of it. One lineage — task-2 → x1 → x2 → x3 — exhausted eight times, resumed
// three, and every round of it was admitted by the growth governor because
// every round had "produced" something: `debug-grid.ts`, `debug-grid2.ts`,
// `debug-grid3.tsx`, `debug-grid10.ts`, `debug-grid11.ts`, `debug-yoga.ts`,
// `debug-yoga2.ts`, `debug-test2.tsx`, `debug-test3.tsx`, `debug-grid-pos.tsx`.
// A model that is stuck writes scratch files, and a scratch file is a file, so
// the standstill rule — which was right, and which is the only rule in the
// governor that asks whether anything was ACHIEVED — saw motion on every round
// and never once spoke. What stopped the run was the round cap, four rounds and
// ninety minutes later, with the wall gone, no gate cut, and `settled: false`.
//
// So the reading of "did this round move something" is narrowed to the work:
//
//   - EVERY CHECK FILE COUNTS. A check the run wrote is the one thing a job can
//     produce that is progress on its own terms whatever else happened, and it
//     is what the coverage finding exists to buy. verify.OwnChecks decides what
//     a check is, by the runner's own naming convention and by nothing else.
//   - A CHANGED SOURCE COUNTS WHEN THE JOB IS ABOUT IT. The job's focus is what
//     its request names, resolved against the workspace by verify.Locate — the
//     identical reading internal/exec takes to decide how much of a project to
//     verify — plus the adjacency verify.Adjacent's first rank defines: a file
//     named after something in the focus, or sitting in a directory the focus
//     is in. `debug-grid.ts` at a repository root beside `package.json` is
//     neither, and `src/grid.ts` is both.
//   - A JOB THAT NAMED NOTHING IS ABOUT EVERYTHING. An empty focus is verify's
//     own "reading of the whole project", and the fail-safe direction here is
//     the same: with nothing to narrow by, every changed source counts and the
//     governor keeps exactly the bounds it had.
//
// NO FILENAME PATTERN APPEARS ANYWHERE IN THIS FILE. `debug-` is a spelling one
// model happened to choose, and a rule written against it would be one spelling
// behind forever (FAILSAFE clause 1). What is structural is the relationship
// between what the round wrote and what the request is about, and that
// relationship is one verify already computes for its own reasons.
package resident

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// scratchNamed bounds how many of a fruitless round's own files the journal
// keeps. The count is the finding; the names are so a person can recognise it,
// and a dozen of them is already more than anybody reads.
const scratchNamed = 12

// RoundChange is one round's work as the world holds it, split by whether the
// job is about it.
type RoundChange struct {
	// Relevant is what the round changed that the job is about: check files,
	// and sources inside the focus.
	Relevant []string
	// Scratch is the rest — files the round wrote that are outside everything
	// the request named. They are journaled rather than dropped: "this round
	// wrote fourteen files and moved none of them" is a recognisable failure
	// and the names are what make it recognisable.
	Scratch []string
	// Measured says somebody could look. It is false where there is no
	// workspace to read the round against, which reads as "nobody counted" and
	// leaves every governor exactly as it was.
	Measured bool
}

// Moved reports that the round changed something the job is about.
func (c RoundChange) Moved() bool { return len(c.Relevant) > 0 }

// MeasureRound reads what one round left behind against what its job is about.
//
// artifacts is the workspace's own before-and-after reading of the tree — never
// a worker's account of itself (FAILSAFE clause 2) — and workspace is the
// directory that reading was taken in. An empty workspace is a caller that
// cannot say where the work happened, and it answers "nobody looked".
func MeasureRound(graph *store.Store, jobRoot, workspace string, artifacts []string) RoundChange {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return RoundChange{}
	}
	if len(artifacts) == 0 {
		// The workspace looked and the tree gained nothing. That is a reading,
		// and it is the one the standstill rule exists to weigh.
		return RoundChange{Measured: true}
	}
	change := RoundChange{Measured: true}
	// Checks first and unconditionally. A check this run wrote is in scope by
	// verify.Adjacent's own leading rule — "A CHECK THE JOB ITSELF NAMED IS
	// ALWAYS IN SCOPE" — and for the same reason: it is the one thing a scope
	// decided before the work could never have known about.
	change.Relevant = append(change.Relevant, verify.OwnChecks(workspace, artifacts)...)
	sources := verify.ChangedSources(workspace, artifacts)
	focus := jobFocus(graph, jobRoot, workspace)
	if len(focus) == 0 {
		// The job named nothing this workspace holds, so nothing narrows and
		// every source is the job's. This is verify's own reading of an empty
		// focus and it is the fail-safe direction: a governor that refused a
		// round here would be refusing it for the request's silence.
		change.Relevant = append(change.Relevant, sources...)
		sort.Strings(change.Relevant)
		return change
	}
	near := newFocusShape(focus)
	for _, source := range sources {
		if near.holds(source) {
			change.Relevant = append(change.Relevant, source)
			continue
		}
		change.Scratch = append(change.Scratch, source)
	}
	sort.Strings(change.Relevant)
	sort.Strings(change.Scratch)
	return change
}

// jobFocus is what the job is about, as paths this workspace holds.
//
// It is read from the JOB ROOT and never from the round being weighed. A
// continuation's brief carries the partial, the artifact list and the whole
// transcript of the attempt before it, so a focus derived from it would name
// every scratch file the last round wrote and then rule that writing more of
// them was progress — the focus would grow to fit whatever the model did, which
// is the property a bound may never have. The request is what every round of a
// job shares, which is exactly why internal/exec derives its reading's scope
// from the same place.
func jobFocus(graph *store.Store, jobRoot, workspace string) verify.Focus {
	root, ok, err := graph.Node(jobRoot)
	if err != nil || !ok {
		return nil
	}
	// The verbatim intent as well as the brief: the store stamps the person's
	// own words on every node of every splice, and a job root that was itself
	// spliced by a repair carries a brief written by a planner over the top of
	// them.
	said := strings.Join([]string{root.Provenance.Intent, root.Title, root.Brief}, "\n")
	if spec := DecodeSpec(root.Spec); !spec.Empty() {
		said += "\n" + spec.Render(jobFocusLimit)
	}
	named := verify.NamedSubjects(said)
	if len(named) == 0 {
		return nil
	}
	located := verify.Locate(workspace, verify.Focus(named))
	held := make(verify.Focus, 0, len(located))
	for _, entry := range located {
		// Only what RESOLVED. Locate keeps a name it could not place, on the
		// grounds that it costs nothing downstream; here it would cost
		// everything, because an unresolved name matched against a path by
		// spelling is the substring adjacency FAILSAFE clause 1 and
		// verify.Adjacent both refuse.
		if strings.ContainsAny(entry, "/\\") {
			held = append(held, filepath.ToSlash(filepath.Clean(entry)))
		}
	}
	return held
}

// jobFocusLimit bounds how much of the job's own criterion the focus is read
// from. The criterion is the smallest of the three texts here and the names in
// it are the ones a request states rather than a planner's restatement of them;
// past this it is prose, and prose contributes nothing verify.Locate can place.
const jobFocusLimit = 1200

// focusShape is the focus as the three questions a changed file is asked of it.
//
// The questions are verify.Adjacent's first rank, asked the other way round.
// That rank exists to find the CHECKS a change is in; this asks whether a file
// the round wrote is in the change — the file itself, a file beside it, or a
// file the request named by stem. Whole names on both sides, never fragments:
// `Log` matches `_log.py` and never `dialog.py`, which is the rule that file
// states and the reason it states it.
type focusShape struct {
	paths map[string]bool
	dirs  map[string]bool
	stems map[string]bool
}

func newFocusShape(focus verify.Focus) focusShape {
	shape := focusShape{
		paths: make(map[string]bool, len(focus)),
		dirs:  make(map[string]bool, len(focus)),
		stems: make(map[string]bool, len(focus)),
	}
	for _, entry := range focus {
		clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(entry)))
		if clean == "" || clean == "." {
			continue
		}
		shape.paths[clean] = true
		shape.dirs[pathDirectory(clean)] = true
		if stem := pathStem(clean); stem != "" {
			shape.stems[stem] = true
		}
	}
	return shape
}

func (s focusShape) holds(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
	if clean == "" {
		return false
	}
	return s.paths[clean] || s.dirs[pathDirectory(clean)] || s.stems[pathStem(clean)]
}

func pathDirectory(path string) string {
	dir := filepath.ToSlash(filepath.Dir(path))
	if dir == "." || dir == "/" {
		return ""
	}
	return dir
}

// pathStem is a file's name with its extension off, lower-cased. Case is
// dropped because `RichLog.ts` and `richlog.ts` are one name written twice, and
// nothing else is — this is an equality test on whole names, which is what
// keeps it from becoming the substring adjacency verify.Adjacent was rewritten
// to stop being.
func pathStem(path string) string {
	name := filepath.Base(path)
	if extension := filepath.Ext(name); extension != "" {
		name = strings.TrimSuffix(name, extension)
	}
	return strings.ToLower(strings.TrimSpace(name))
}

// Shortfall is the job's own account of what it is still short of, as counts a
// later round can be weighed against.
//
// MOVING A FILE IS NOT THE ONLY WAY TO MOVE THE WORK. A round that closed a
// regression, brought a stated behaviour under a check, or answered a standing
// review finding has made progress even where the focus gained nothing — so the
// evidence the governor reads is the union of the two, and a rule that read
// only the tree would refuse the round after the one that finally started
// working.
//
// It is counts and a digest rather than the findings themselves because every
// comparison made of it is a FALL: fewer unexercised behaviours, fewer red
// checks, a finding that stood and now does not. A shortfall REWORDED is not a
// shortfall closed, which is the distinction the remainder digest already draws
// one rule along.
type Shortfall struct {
	Unexercised int
	Red         int
	// Lost is how many public names the newest symbol-level reading says the
	// finished tree no longer spells. It is the check roster's sibling and it
	// belongs here for the same reason: a round that put back eight deleted
	// public attributes moved the work whatever the check-level row said, and
	// on igel s11 the check-level row said the tree had got BETTER on the run
	// that deleted them. store.SurfaceReading is the reading; nothing here
	// re-derives it.
	Lost int
	// Standing is the review finding that is still open, as a digest. Empty is
	// a job with nothing standing against it.
	Standing string
}

// ReadShortfall reads it off one lineage's own journal, through the same
// account of the record every brief is composed from.
func ReadShortfall(graph *store.Store, lineage string) Shortfall {
	findings := ReadOpenFindings(graph, lineage)
	shortfall := Shortfall{
		Unexercised: len(findings.Unexercised),
		Red:         len(findings.Failing),
		Lost:        lostPublicNames(graph, lineage),
	}
	if findings.Unclosed || strings.TrimSpace(findings.Gap) != "" {
		shortfall.Standing = RemainderDigest(findings.Gap)
	}
	return shortfall
}

// lostPublicNames is the newest symbol-level reading in a lineage, as a count.
//
// The newest and not the union, exactly as failingChecks reads the check
// roster: a name that was missing three rounds ago and is back is history, not
// a finding, and handing it to a rule as outstanding is how a round gets spent
// on something already done.
func lostPublicNames(graph *store.Store, lineage string) int {
	nodes, err := graph.LineageNodes(lineage)
	if err != nil {
		return 0
	}
	for index := len(nodes) - 1; index >= 0; index-- {
		readings, err := graph.SurfacesFor(nodes[index].ID)
		if err != nil || len(readings) == 0 {
			continue
		}
		return readings[len(readings)-1].Lost
	}
	return 0
}

// closerThan reports that this shortfall is smaller than the one before it.
func (s Shortfall) closerThan(previous Shortfall) bool {
	switch {
	case previous.Unexercised > 0 && s.Unexercised < previous.Unexercised:
		return true
	case previous.Red > 0 && s.Red < previous.Red:
		return true
	case previous.Lost > 0 && s.Lost < previous.Lost:
		return true
	case previous.Standing != "" && s.Standing == "":
		return true
	}
	return false
}

// ── where the work happened ──────────────────────────────────────────────────
//
// The directory a job's rounds run in is A PROPERTY OF THE JOB and not of
// whoever asks to grow it. Four paths grow a running job — an overrun replan,
// the delivery gate's gap round, the revision sentinel, a cooperative split —
// and they are reached through signatures owned by four different waves, two of
// them in packages this one may not reach into. A fact threaded through the
// callers is a fact that works on whichever caller somebody remembered, which
// is the defect the splice's own seed comment already records having shipped
// once.
//
// So it is a seam, on the same terms as the satisfaction gate above it: written
// once by whoever builds the workspace, read by everything that weighs a round,
// and unset it simply answers "nobody could look".

var jobWorkspaces = newBoundedWorkspaces()

// RememberJobWorkspace records where a job's work happens. Calling it twice
// with the same answer is free; calling it with a different one replaces the
// answer, because a job that moved is working in the new place.
func RememberJobWorkspace(graph *store.Store, node store.Node, workspace string) {
	workspace = strings.TrimSpace(workspace)
	if graph == nil || workspace == "" {
		return
	}
	jobWorkspaces.remember(jobRootID(graph, node), workspace)
}

// JobWorkspace is where a job's rounds run, or empty when nothing said.
func JobWorkspace(jobRoot string) string { return jobWorkspaces.recall(jobRoot) }

// rememberedWorkspaces bounds how many jobs this holds a directory for at once.
// It is internal/revision's rememberedJobs and verify's rememberedTrees for the
// same reason and at the same size: past it the oldest is dropped, which costs
// a round its measurement rather than giving it a wrong one.
const rememberedWorkspaces = 16

type boundedWorkspaces struct {
	mu    sync.Mutex
	dirs  map[string]string
	order []string
}

func newBoundedWorkspaces() *boundedWorkspaces {
	return &boundedWorkspaces{dirs: make(map[string]string, rememberedWorkspaces)}
}

func (b *boundedWorkspaces) remember(jobRoot, workspace string) {
	if strings.TrimSpace(jobRoot) == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, held := b.dirs[jobRoot]; !held {
		b.order = append(b.order, jobRoot)
		for len(b.order) > rememberedWorkspaces {
			delete(b.dirs, b.order[0])
			b.order = b.order[1:]
		}
	}
	b.dirs[jobRoot] = workspace
}

func (b *boundedWorkspaces) recall(jobRoot string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dirs[strings.TrimSpace(jobRoot)]
}

// forgetJobWorkspaces is the tests' reset. Nothing in the program calls it: a
// process holds one resident and its jobs are all real.
func forgetJobWorkspaces() { jobWorkspaces = newBoundedWorkspaces() }
