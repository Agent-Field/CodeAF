package session

// THE GRADING LOOP, AND THE TWO ENDS OF IT THAT NEVER MET.
//
// A task node settles with a rich outcome: a check has read the work and said
// whether it holds, the work was sent back some number of times before it did,
// and the whole thing has a model, a bill and a duration attached to it
// (task_run.go, task_audit.go). Meanwhile the divider hands out parts and
// decides how much thinking each one is done with by reading ONE WORD the
// dividing worker wrote — `mechanical` or `careful` — which is a guess made
// before anybody opened the material (task_divide.go).
//
// Both ends existed. Nothing joined them. `aforge models` — the panel that
// exists precisely to ground a choice like this one — printed "nothing measured
// yet. Ratings appear once calls have been graded" on a machine that had settled
// weeks of tasks, because the only thing that had ever written into the ledger
// was internal/router, and the chat engine does not route through it. So on the
// self-build run of 2026-08-31 all four test-writing lanes were graded
// `mechanical` and given the cheap tier, produced nothing usable, and the store
// that could have said "this kind of work does not hold on that model here"
// stayed empty (issue #147).
//
// ── WHAT THIS FILE DOES, IN ONE BREATH ──
//
// Every settled node writes one graded record into the SAME store `aforge
// models` reads, and the divider reads that store before it assigns a tier.
//
// ── THE GRADE IS THE CHECK'S OWN ANSWER, AND NOTHING ELSE ──
//
// NO MODEL CALL IS MADE HERE. The check at the end of a task has already read
// the work, already cost what it cost, and already said one of three things:
// the work holds, the work is missing these gaps, or nobody could say. That
// answer IS the grade — it is written on the node the moment it is given
// ([TaskNode.checkSaid]) and read off the node when the node settles. A second
// judgement pass would be spending somebody's money to re-derive a fact the
// harness already owns, and it would put a token-generating path on a road that
// nothing asked for.
//
// A node that never reached a check is honestly ungraded. It still writes its
// record — the diary is the diary — but an outcome nobody judged moves no
// rating, which is the ledger's own law about unverified successes and provider
// failures restated for this road (internal/provider's verdict.go).
//
// ── THE KIND IS THE DIVIDER'S OWN WORDS, NOT AN ENUM ──
//
// What is being rated is not "tasks"; it is a KIND of work. The kind is the
// name whoever handed the work out gave it — "the eleven adapters", "tests for
// the rail" — lowercased and cut to its words, and nothing else. There is no
// taxonomy, no list of domains, and no rule about what any word means, because
// the next piece of work is always some kind of work nobody enumerated (the
// same argument prompts/shape.md is written on). "Mechanical" stops being a
// prompt's guess and becomes a fact somebody measured.
//
// Two kinds are THE SAME KIND-SHAPED WORK when their words overlap by half or
// more ([taskKindsMeet]). Names are short, so half is two words out of three:
// "tests for the rail" and "tests for the composer" are one population and "the
// eleven adapters" is not in it. Set overlap and not a stemmer, a stopword list
// or a similarity model — a list of function words is a rule about English, and
// this has to hold for work in any domain, in any vocabulary, named by any model.
//
// WHAT THAT RULE CANNOT DO is stated here so nobody has to discover it: two
// names that differ by one word are one family WHICHEVER word it is, so "write
// the adapters" pools with "write the tests" exactly as the pair above pools.
// No rule over word sets separates those two cases without a grammar. The two
// ways of being wrong are deliberately not the same size — a family drawn too
// wide costs one part of one division on a dearer model, and a family drawn too
// tight costs a lift that never happens — so the loose reading is the one taken.
//
// (internal/craft holds a tokenizer with a stopword list and a stemmer. It is
// NOT this: it ranks documents for BM25 retrieval, where dropping grammar keeps
// a short request from scoring on its articles. Here the words on both sides
// come from the same kind of short label written by the same kind of writer, so
// the grammar cancels and the list would only be something to maintain.)
//
// ── WHERE IT IS WRITTEN, AND WHY THAT IS THE SAME PLACE ──
//
// internal/router's Ledger is the ratings store, and `aforge models` reads it
// out of the person's profile directory. A second store would be a second
// answer to "how good is this model at this", which is the drift the
// one-source-of-truth law exists to stop — so a settled node observes into that
// ledger under [provider.ClassTaskNode] with its kind as the shape, exactly the
// way a routed leaf observes under exec.leaf with its own, and the diary row
// beside it is the same router.Event every routed attempt writes.

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/router"
)

const (
	// taskKindWords bounds a kind. A title is already capped at
	// [TaskNameWords]; this is the guard for the other doors a node can be
	// named through — a person's own `/task` sentence, a shaper that ran long —
	// so that one over-long name cannot become a class nothing else ever meets.
	taskKindWords = 6

	// taskKindOverlap is when two kinds are the same kind-shaped work: half the
	// words of the smaller one, or more.
	//
	// HALF, because a name is three words and two of three in common is a
	// genuine family — "tests for the rail" beside "tests for the composer" —
	// while one word in common is a shared verb and nothing else. A threshold
	// that pooled on one word would have every "write" in the project rating
	// one model, which is the exec.leaf mistake arm B paid for.
	taskKindOverlap = 0.5

	// taskGradeEvidence is how many checked settles of one kind-shaped work it
	// takes before the store may move a tier.
	//
	// TWO, and it is deliberately not the ledger's own [router.MinGraded] of
	// eight. That gate guards a decision this one is not: it decides whether a
	// learned rating may REORDER A WHOLE PANEL against a cold-start prior, where
	// being wrong reroutes every leaf of every task (arm B's collapse). This
	// decides whether ONE PART of one division is done on the careful tier
	// instead of the cheap one, where being wrong costs the difference between
	// two models on one piece of work and nothing else — and where the install
	// that has configured no tiers pays literally nothing, because the ladder
	// floors on the model the task is already on.
	//
	// Two is also the fewest that can tell "keeps getting rejected" from "went
	// wrong once", which is the sentence the road is built on.
	taskGradeEvidence = 2
)

// taskGrades is the ratings store as this engine writes to and reads from it:
// one profile directory, and nothing held open.
//
// NOTHING IS HELD OPEN ON PURPOSE. A node settles every few minutes at the
// fastest, and the ledger's flush is already a locked read-merge-write built for
// several aforge processes doing exactly this to the same small file — so taking
// it, folding one observation in and giving it back is one locked write per
// settle, with no lifetime to manage, no handle to close on a crash, and no
// window in which this session's evidence exists only in memory. A long-lived
// handle would buy a debounce that has nothing to batch.
//
// A NIL taskGrades IS THE ABSENCE LAW, not a broken one: a session with no
// profile directory — a headless run, a task node, every scripted graph in the
// tests — grades nothing and reads nothing, and every method here tolerates it.
type taskGrades struct {
	dir string
}

// newTaskGrades answers nil where there is no profile to write into.
func newTaskGrades(dir string) *taskGrades {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	return &taskGrades{dir: strings.TrimSpace(dir)}
}

// taskGradeRecord is one settled node as the store holds it.
//
// It carries what a later reader needs in order to argue with the rating rather
// than only read it: WHICH MODEL did WHAT KIND of work, how it ended in the
// settle's own plain word, how many times the work was sent back before it got
// there, what it cost and how long it took. The Verdict is the check's answer
// translated into the ledger's own taxonomy, and it is the only field that moves
// a rating; everything else is the diary.
type taskGradeRecord struct {
	Model   string
	Kind    string
	Outcome string
	Verdict provider.Verdict

	Retries  int
	Cost     float64
	Duration time.Duration

	Session string
	Node    uint64
}

// keep writes one settled node into the store.
//
// The ledger's Observe is the half that may move a rating and it filters itself:
// a verdict that is not evidence folds nothing in, which is why a stopped node
// and a node nobody could check are passed through here unchanged rather than
// dropped at the door. The diary row is written either way, because "this ran
// and nobody could say" is exactly the row an autopsy goes looking for.
//
// A STORE THAT WILL NOT OPEN IS NOT A REASON TO FAIL A LANDING. Everything here
// is best-effort by construction: the work is already done, the person is already
// being told about it, and an unwritable profile directory must cost a lesson
// rather than a task.
func (g *taskGrades) keep(record taskGradeRecord) {
	if g == nil || strings.TrimSpace(record.Model) == "" || record.Kind == "" {
		return
	}
	class := router.Shaped(provider.ClassTaskNode, record.Kind)
	if ledger, err := router.LoadLedger(g.dir); err == nil {
		// The prior is zero — the middle of the scale — because nobody has made
		// a claim about this model at this kind of work. internal/router's
		// coldStart says the same thing for a panel member with no role hint.
		ledger.Observe(record.Model, class, 0, record.Verdict)
		_ = ledger.Close()
	}
	events, err := router.OpenEvents(g.dir)
	if err != nil {
		return
	}
	events.Append(router.Event{
		Call: record.callID(), Run: record.Session,
		Class: string(provider.ClassTaskNode), Shape: record.Kind,
		Model: record.Model, Verdict: record.Verdict, Final: true,
		Outcome: record.Outcome, Retries: record.Retries,
		Cost: record.Cost, LatencyMS: record.Duration.Milliseconds(),
	})
	_ = events.Close()
}

// callID ties every row about one node together. A node can settle twice — it
// lands needing a look and a person resolves it later — and the diary is
// append-only, so the second row must correct the first rather than read as a
// second piece of work.
func (r taskGradeRecord) callID() string {
	return "task:" + r.Session + ":" + strconv.FormatUint(r.Node, 10)
}

// taskGradeReading is what the store says about one model at one kind-shaped
// piece of work, pooled over every kind it meets.
type taskGradeReading struct {
	// Count is how many checked settles stand behind Rating.
	Count int
	// Rating is the ledger's own ability in logits, count-weighted across the
	// kinds that were pooled. Below zero is a model that has been turned down
	// more often than it has held.
	Rating float64
}

// taskGradeReader is the store read ONCE and asked about many kinds.
//
// It exists because a division is one decision. Eight parts asking the file
// eight questions would be eight reads of it, and — worse — eight readings taken
// at eight instants, so a division whose last part was decided against a store a
// concurrent window had moved underneath it would be a family nobody could
// account for afterwards. One snapshot, one model, and every part of the
// division weighed against the same one.
type taskGradeReader struct {
	entries []router.Entry
}

// reader takes that snapshot: this model's task-node rows and nothing else.
func (g *taskGrades) reader(model string) taskGradeReader {
	if g == nil || strings.TrimSpace(model) == "" {
		return taskGradeReader{}
	}
	ledger, err := router.LoadLedger(g.dir)
	if err != nil {
		return taskGradeReader{}
	}
	// The ledger keys on what the provider actually served, so a floating alias
	// is read through the same map the router reads it through.
	model = ledger.Resolve(model)
	prefix := string(provider.ClassTaskNode) + "/"
	var kept []router.Entry
	for _, entry := range ledger.Entries() {
		if entry.Model != model || entry.Count <= 0 {
			continue
		}
		if !strings.HasPrefix(string(entry.Class), prefix) {
			// Everything the router itself learned about this model is in here
			// too, and none of it is about a whole settled piece of work.
			continue
		}
		kept = append(kept, entry)
	}
	return taskGradeReader{entries: kept}
}

// reading pools every record of this kind-shaped work.
//
// The pooling is count-weighted rather than a plain mean, so a kind with eleven
// settles behind it is not outvoted by one with a single settle — which is the
// same reasoning the ledger's own step size is built on.
func (r taskGradeReader) reading(kind string) taskGradeReading {
	if kind == "" {
		return taskGradeReading{}
	}
	prefix := string(provider.ClassTaskNode) + "/"
	var pooled taskGradeReading
	var weighted float64
	for _, entry := range r.entries {
		if !taskKindsMeet(strings.TrimPrefix(string(entry.Class), prefix), kind) {
			continue
		}
		pooled.Count += entry.Count
		weighted += entry.Rating * float64(entry.Count)
	}
	if pooled.Count > 0 {
		pooled.Rating = weighted / float64(pooled.Count)
	}
	return pooled
}

// saysCareful is the whole of what the divider asks the store: on the evidence
// here, does this model keep getting turned down on this kind of work?
//
// TWO CONDITIONS, AND BOTH ARE ABOUT EVIDENCE. There has to be enough of it
// ([taskGradeEvidence]), and it has to point the wrong way — a rating below the
// middle of the scale is the ledger's own statement that the checks went against
// this model more often than they went for it. Neither condition mentions a
// domain, a model id or a word in a title, which is the house law: what counts
// as mechanical is a learned fact and never a hardcoded one.
func (r taskGradeReader) saysCareful(kind string) bool {
	reading := r.reading(kind)
	return reading.Count >= taskGradeEvidence && reading.Rating < 0
}

// reading and saysCareful on the store itself are the one-question form, for a
// caller with a single kind to ask about. They take a snapshot of their own,
// which is the same read a division takes once.
func (g *taskGrades) reading(model, kind string) taskGradeReading {
	return g.reader(model).reading(kind)
}

func (g *taskGrades) saysCareful(model, kind string) bool {
	return g.reader(model).saysCareful(kind)
}

// ── the kind, and when two of them are one ──────────────────────────────────

// taskKindOf is the divider's own name for a piece of work, in the spelling the
// store keys on: lower case, cut at anything that is not a letter or a digit,
// and capped.
//
// THE WORDS KEEP THEIR ORDER, so the class reads as the name somebody wrote —
// `task.node/tests for the rail` on `aforge models` — for the reason
// internal/router states about its own keys: a ledger nobody can read is a
// ledger nobody checks. The set comparison that pools two kinds happens at read
// time and does not need the key sorted.
func taskKindOf(title string) string {
	fields := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(title)), func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return false
		}
		return true
	})
	seen := make(map[string]bool, len(fields))
	words := make([]string, 0, taskKindWords)
	for _, field := range fields {
		if seen[field] {
			continue
		}
		seen[field] = true
		words = append(words, field)
		if len(words) == taskKindWords {
			break
		}
	}
	return strings.Join(words, " ")
}

// taskKindsMeet reports whether two kinds are the same kind-shaped work: their
// words overlap by [taskKindOverlap] of the smaller one or more.
//
// The smaller side is the denominator on purpose. A three-word name that is
// wholly inside a six-word one is the same work described at two lengths, and
// measuring against the longer side would call that a stranger.
func taskKindsMeet(one, other string) bool {
	first, second := taskKindWordSet(one), taskKindWordSet(other)
	if len(first) == 0 || len(second) == 0 {
		return false
	}
	if len(first) > len(second) {
		first, second = second, first
	}
	shared := 0
	for word := range first {
		if second[word] {
			shared++
		}
	}
	return float64(shared) >= math.Ceil(taskKindOverlap*float64(len(first)))
}

func taskKindWordSet(kind string) map[string]bool {
	words := strings.Fields(kind)
	set := make(map[string]bool, len(words))
	for _, word := range words {
		set[word] = true
	}
	return set
}

// ── what the settle tells the store ─────────────────────────────────────────

// taskGradeOutcome is the settle's own plain word for how a node ended, and it
// is the vocabulary a person already reads on a row: work that landed, work the
// check would not accept, work that did not finish, work somebody stopped, and
// work waiting on somebody to look at it. No machinery vocabulary reaches this
// string — it goes into a file a person may open, and `needs your look` is the
// surface's own words for that state rather than a second spelling of them
// (CLAUDE.md's vocabulary law).
//
// THE ENDING IS READ AND NOT ONLY THE STATE, because a node fails for two
// completely different reasons and one of them is not about the work at all. A
// check that named gaps is `not accepted`; a worktree that could not be made, a
// worker that would not start, a connection that dropped is `did not finish` —
// and a record that called the second one `not accepted` would be reading a
// judgement into a fault nobody judged.
func taskGradeOutcome(state TaskState, ending TaskEnding, stopped bool) string {
	switch {
	case stopped:
		return "stopped"
	case state == TaskDone:
		return "landed"
	case state == TaskUnverified:
		return "needs your look"
	case state == TaskFailed && ending == TaskEndingRefused:
		return "not accepted"
	case state == TaskFailed:
		return "did not finish"
	default:
		return string(state)
	}
}

// ── the settle seam ─────────────────────────────────────────────────────────

// grade writes one settled node into the ratings store.
//
// IT IS ONE SEAM AND NOT SIX. Every road a node can end on — the gate's verdict,
// a threshold, a stop, an error, a person's own answer to work nobody could
// check — passes through [TaskGraph.complete] or [TaskGraph.resettle], which is
// exactly why the record is written from there and not from the endings
// themselves. Six call sites would be five chances to add a seventh ending and
// forget, which is the argument the division's own journal line is written on.
//
// ONLY ORDINARY WORK IS GRADED. A harness being designed, a subharness being
// run and an adaptive run's rows are settled nodes too, and none of them is a
// worker in a copy of the repository whose deliverable somebody checked — so
// rating a model on one would be pooling two populations, which is the whole
// reason the ledger keys on a class at all.
func (g *TaskGraph) grade(node *TaskNode) {
	if g == nil || g.grades == nil || node == nil {
		return
	}
	// Both of these take a lock of their own, so they are asked BEFORE the
	// graph's is taken: the session's id is read under the agent's lock, and
	// this package's one lock order is the agent's outside the graph's.
	session := "unfiled"
	if g.home != nil {
		session = g.home.sessionID()
	}
	cost := node.spend()

	g.mu.Lock()
	record := taskGradeRecord{
		Model:    node.runModelLocked(),
		Kind:     taskKindOf(node.spec.title),
		Outcome:  taskGradeOutcome(node.state, node.ending, node.stopped),
		Verdict:  node.checked,
		Retries:  node.repairs,
		Cost:     cost,
		Duration: node.elapsed,
		Session:  session,
		Node:     node.id,
	}
	ordinary := node.kind == "" && node.state != TaskQueued && node.state != TaskRunning
	g.mu.Unlock()

	if !ordinary {
		return
	}
	g.grades.keep(record)
}

// checkSaid writes down what the check answered about this node's work, and how
// many rounds it took to get there. THE FIRST ANSWER DOES NOT WIN HERE, unlike
// [TaskNode.end]: a node that lands needing a look and is judged again later has
// genuinely been answered twice, and the later answer is the one that stands.
func (n *TaskNode) checkSaid(verdict provider.Verdict, repairs int) {
	if n == nil {
		return
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	n.checked = verdict
	if repairs > n.repairs {
		// The rounds ACCUMULATE over a node's whole life rather than being
		// replaced: a re-audit that finds nothing new has not undone the two
		// rounds the first run spent, and the record is about the work.
		n.repairs = repairs
	}
}

// checkAnswer is what the check said, from outside the lock. "" is a node no
// check ever read, and it grades nothing.
func (n *TaskNode) checkAnswer() provider.Verdict {
	if n == nil {
		return ""
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.checked
}

// auditGrade is the gate's answer in the ledger's taxonomy, and it is the ONLY
// translation between the two anywhere in this package.
//
// A NON-ANSWER GRADES NOTHING, and that is the whole of what makes the loop
// honest. "Nobody could say" is not a finding about the work and never a finding
// about the model — an auditor that died on the wire would otherwise teach the
// store that the model it was judging is weak, which is the provider-failure
// mistake internal/provider's verdict.go names outright.
func auditGrade(verdict auditVerdict) provider.Verdict {
	switch {
	case !verdict.answered:
		return ""
	case verdict.verified:
		return provider.VerdictVerifiedSuccess
	default:
		// The work parsed, ran, and was wrong in a way somebody could point at,
		// which is exactly what a semantic failure is: the reply the model gave
		// against the job it was handed did not hold.
		return provider.VerdictSemanticFailure
	}
}
