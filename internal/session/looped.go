package session

// Loop detection: the nudge for a turn going in circles.
//
// A model that is stuck does not stop — it repeats. The same grep with the same
// pattern, four times. The same build, failing the same way, six times. Nothing
// in the loop (loop.go) notices, because every individual step is legal: a tool
// was called, a result came back, the model asked again. The turn ends when the
// model runs out of ideas or when the person interrupts, and the second is what
// actually happens.
//
// ── WHAT IS WATCHED ──
//
// Four signals, per turn, over a sliding window of the last dozen calls:
//
//   - THE SAME CALL THREE TIMES IN A ROW. Consecutive, because a call repeated
//     with other work between the repeats is usually a person's transcript being
//     re-read, not a loop. Identity is the tool name and its arguments — a read
//     of two different files is two different calls.
//   - THE SAME ERROR THREE TIMES IN THE TURN. Total rather than consecutive, and
//     across tools rather than per tool, because this is the shape a real loop
//     takes: the model varies the call, the failure does not move.
//   - SILENT TOOL BATCHES IN A ROW. A reasoning model can keep re-deriving a
//     plan that vanishes at every step boundary while every individual call
//     remains distinct. The notes arrive after six batches and after twelve, the
//     second stronger than the first, and progress resets the ladder.
//     SILENCE IS HYGIENE AND NOT STUCKNESS, so its notes are the only ones that
//     cannot end a turn through the hand-off below. THE SECOND RUNG IS WHERE THE
//     ADVICE STOPS BEING ADVICE: from there the loop withholds a submission that
//     carries only tool calls until a note lands, and ends the turn on its own
//     honest line if the model will not write one (processrule.go).
//   - ROUNDS THAT READ NOTHING NEW. Distinct command strings can ask the same
//     question with slightly different words, so the ledger rather than the
//     signature decides whether the answers added anything. Five consecutive
//     rounds whose every result has no fresh line name that fact directly.
//   - AND, FASTER THAN EITHER: THE SAME CALL REFUSED FOR ITS ARGUMENTS TWICE.
//
// ── THE ARGUMENT-REFUSAL RULE, AND WHY IT IS TWO AND NOT THREE ──
//
// An argument refusal is the one failure that arrives with its own cure. A tool
// that answers "Invalid arguments: limit takes a whole number: send
// {"limit":10}, not 10.0" has already told the model the corrected call, so a
// SECOND identical call proves the model did not read it — no further evidence
// is going to arrive, and every repeat after that is spend. So an identical call
// whose result is an argument refusal is named at TWO, and the note carries the
// tool's own repair sentence rather than the harness's advice to think again:
// there is nothing to rethink, there is a call to correct.
//
// The measurement that set this: a model sent `tasks {"limit":10.0}` eleven
// times in one turn, and the counts that reached the person were nine and
// eleven — the ordinary ladder, arriving eight calls after the answer was known.
// Everything that is NOT an argument refusal keeps the old counts, because
// nothing about those is known in advance.
//
// AND NOTHING THE HARNESS ITSELF ANSWERED IS WATCHED AT ALL. A hand that was
// withdrawn (withdrawn.go) and a door that refused the call never reached the
// world: the failure was written on this side of the wall, and a repetition of
// it is the harness's doing, not the model's. Those results are skipped before
// any rule sees them — the measured cost of not doing so is three [stuck]
// notes scolding a worker for retrying a tool the harness had just taken away.
//
// ── WHAT A NUDGE IS ──
//
// A note in the transcript, in the lane a person's steering rides (agent.go's
// enqueueSteering): plain user-role text, drained at the next step boundary,
// journaled like anything else the model was told. It is deliberately NOT a
// provider error and not a tool failure — the model is not being punished, it is
// being told what it has been doing, which is the one fact it cannot see.
//
// ONE NUDGE PER SIGNATURE, AND THEN HYSTERESIS. A loop that continues
// immediately after being named does not earn a second identical note; a second,
// different loop in the same turn does. The whole watch resets per turn, because
// a turn is where a person's attention resets too.
//
// ── THE HYSTERESIS LAW ──
//
// Forward progress is accepted IMMEDIATELY; backward movement needs REPEATED
// evidence (PMCoder's phase hysteresis, https://arxiv.org/abs/2608.06811 — the
// mechanism that stops a planner thrashing between phases on one noisy signal).
// Here the two directions are:
//
//   - FORWARD is a successful call this turn has not already been nudged about.
//     One of those kills the backward case outright: every streak resets, and a
//     model that was three repetitions deep starts again from nothing. No
//     confirmation, no decay, no half-credit — the evidence that the turn is
//     working is the turn working.
//   - BACKWARD is a signature that was already named repeating AGAIN. It takes
//     [loopHysteresis] more of them to advance the ladder, not one. The
//     asymmetry is the whole mechanism: the harness is quick to believe the turn
//     is fine and slow to escalate against it, because escalating costs the
//     person's attention and being wrong about progress costs nothing.
//
// ── AND THEN THE HAND-OFF, WHICH SILENCE HAS NO PART IN ──
//
// Past two nudges the notes have stopped working, and a third one is the harness
// talking to itself. The third signal therefore ends the turn through the same
// checkpoint hand-off that governs any other overlong turn. When that road is
// unavailable — inside a task, without a consent surface, or when no brief can
// be carried — the turn still ends and says plainly that its remains were left.
//
// FOUR RULES CAN SPEND THAT COUNT, AND SILENCE IS NOT ONE OF THEM: identity, the
// repeated error, the argument refusal, and the round that read nothing new.
// Each of those is a claim that the turn is not moving. Silence is a claim about
// the RECORD — that reasoning is being lost between steps — and a turn can be
// entirely silent while committing, pushing and landing real work.
//
// The measured case: a worker's last six calls before it was stopped were
// `commit-tree`, `write-tree`, a second commit, a ref update, a log and a
// cleanup — all distinct, all succeeding, with three visible notes written in
// the minute before. Two early silence notes plus one late one added up to a
// hand-off, and the row it left said the turn "went in circles" when the turn
// had been working the whole time. So a silent note keeps its rung and its
// wording and books nothing, and no rung of it promises a hand-off it cannot
// make.
//
// AND MATERIAL PROGRESS GIVES THE COUNT BACK, ONE RUNG AT A TIME. The two
// ledgers this file keeps take different evidence, on purpose:
//
//   - THE PER-SIGNATURE STREAKS take the weak kind — any successful call this
//     turn has not been nudged about — and clear to NOTHING. They are evidence
//     about one repetition, and evidence that the turn is working destroys the
//     backward case outright. That is the hysteresis law above, unchanged.
//   - THE NUDGE COUNT takes only the strong kind: a file written, or a shell
//     command that left the tree different from how it found it — and neither of
//     them from a call this turn has already been nudged about, because a loop
//     that paid its own refund would put the ceiling out of reach. It steps DOWN
//     BY ONE rather than clearing.
//
// Both halves of that are load-bearing. VISIBLE TEXT CANNOT BUY THE COUNT BACK,
// because a model narrating its own loop is still looping — text is the cure for
// the silent ladder and for nothing else. NOR CAN THE WEAK KIND: the first two
// calls of a turn's SECOND loop are by construction calls nobody has been nudged
// about yet, so a turn-wide budget refunded by them is no budget at all.
//
// And it steps down rather than zeroing because the count is not evidence — it
// is the person's attention, already spent, and attention already spent does not
// un-spend. Zeroing would mean a turn that loops, is nudged, does one token of
// real work and loops again can never be handed over however long it runs.
// Stepping down says: forgiven quickly, and still remembered.

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	// loopWindow is how many recent call signatures are remembered. Twelve is
	// three or four batches: long enough to hold a repetition, short enough that
	// a turn's early work cannot haunt its later work.
	loopWindow = 12

	// loopRepeats is how many identical calls, or identical errors, make a loop.
	// Two is a retry — which is often the right thing to do and sometimes works.
	// Three is a habit.
	loopRepeats = 3

	// loopInvalidRepeats is how many identical calls REFUSED FOR THEIR ARGUMENTS
	// make a loop. Two, where an ordinary repetition takes three: the refusal
	// named the corrected call, so the second identical one is not a retry that
	// might work, it is evidence the correction went unread.
	loopInvalidRepeats = 2

	// silentStreakLimit is how many consecutive tool-using steps may carry no
	// visible assistant text before the model is asked to externalize its plan.
	// Six leaves room for a short inspect-decide sequence; beyond that, silence
	// is more likely lost cross-step reasoning than useful brevity.
	silentStreakLimit = 6

	// noNewInformationLimit is how many consecutive finished tool rounds may
	// bring back no fresh line before the transcript itself is named as the place
	// the answer already lives. Five catches a rephrased search before the silent
	// ladder's first rung without treating a short verification sequence as spin.
	noNewInformationLimit = 5

	// loopNudgeCeiling is how many notes a turn gets before the work is handed
	// off instead. Two, because a third note would be the third time advice failed
	// to change anything.
	//
	// IT COUNTS THE RULES THAT CLAIM THE TURN IS NOT MOVING and no others, which
	// is why [silentRungs] is its own number rather than this one reused: the
	// two ladders answer different questions and only one of them may stop work.
	loopNudgeCeiling = 2

	// silentRungs is how many notes ONE silent stretch earns: two, at six batches
	// and at twelve. A stretch broken by progress and begun again starts at the
	// first rung.
	//
	// IT WAS THREE, AND THE THIRD RUNG IS GONE BECAUSE NOTHING CAN REACH IT ANY
	// MORE. The second rung is where the write-your-notes rule stops being advice
	// (processrule.go's [silentEnforceRung]): from there the loop answers a
	// tool-calls-only submission with the rule's demand instead of running it,
	// and a submission the harness itself refused is not one this watch counts —
	// so the streak freezes at twelve and a rung at twenty-four is a rung no run
	// can climb to. Its words were also the opposite of what now happens: it said
	// "Nothing is being stopped — keep working", and by then something is.
	silentRungs = 2

	// loopHysteresis is how many MORE repetitions of an already-named signature
	// count as evidence before the ladder advances again.
	//
	// Two, not one. One would mean a nudged model gets a second nudge on its very
	// next step — before the note it was just handed has even reached a request —
	// and a ladder that climbs faster than its own advice can be read is a ladder
	// measuring latency, not stuckness. Two says: the model saw the note, kept
	// going, and did the same thing twice anyway.
	loopHysteresis = 2
)

// nudge is one detected loop, ready to be said out loud.
type nudge struct {
	// call is the repeating call itself, kept whole so the event and the retained
	// explicit recovery helper can name the same row the turn already drew.
	call ai.ToolCall
	// tool is the call's name, and count how many times it repeated (or how many
	// times the error came back).
	tool  string
	count int
	// failing distinguishes a repeated CALL from a repeated ERROR. They read
	// differently to the model, so they are worded differently.
	failing bool
	// silent distinguishes the batch-streak rule from the two identity rules.
	// It gets its own note and event hint because the remedy is to write the plan
	// down, not merely to choose a different call.
	silent bool
	// silentRung is which rung of the silent ladder fired, 1-based. Its wording
	// grows stronger independently of the shared turn-wide nudge count.
	silentRung int
	// stale distinguishes the ledger rule from identity and silence. Its count
	// is rounds rather than calls because a parallel batch is one attempt.
	stale bool
	// invalid says the repeat was a call the TOOL refused over its arguments,
	// which is the third rule and the one that fires at two. It changes both
	// sentences — the model is told what to send instead, and the person is told
	// the same argument went out twice — because a call nobody can execute is
	// not the same news as work that keeps failing out in the world.
	invalid bool
	// nth is which STOPPING nudge of this turn it is, 1-based — and zero for a
	// note that cannot end a turn, which today is every silent one. It is what
	// the escalation law reads, so a zero here is the whole of "this note is
	// hygiene, and the work goes on".
	nth int
	// fact is the structural sentence the turn's [workClock] can say about
	// itself — when the work last changed, and how much the results since
	// brought back (novelty.go). It is empty when there is nothing to say.
	//
	// ON AN ARGUMENT REFUSAL IT IS THE TOOL'S OWN REPAIR SENTENCE instead, and
	// the clock's note is not taken: `limit takes a whole number: send
	// {"limit":10}, not 10.0` is the whole of what that model needs, and a
	// paragraph about when the work last changed would be the harness talking
	// over the top of the answer.
	fact string
}

// loopWatch is one turn's memory of what it has been doing.
//
// It carries a mutex although only the turn goroutine drives it today: it is
// reachable from the Agent's turn, the batch results arrive from goroutines the
// batch owns, and a watcher whose safety depended on nobody ever calling observe
// from a second place is a watcher that breaks silently the first time somebody
// does.
type loopWatch struct {
	mu sync.Mutex
	// recent is the sliding window of call signatures, oldest first.
	recent []string
	// errors counts each distinct error text seen this turn.
	errors map[string]int
	// named is every signature already nudged for, so nothing is said twice in a
	// row for the same reason.
	named map[string]bool
	// streak is the backward evidence: how many times each ALREADY-NAMED
	// signature has repeated since its last nudge. It is what
	// [loopHysteresis] is counted against, and forward progress empties it.
	streak map[string]int
	// silentStreak counts consecutive tool-using batches with no visible text;
	// silentCalls is the number of calls those batches contained, for the note,
	// and silentRung is the next rung not yet spoken in this streak.
	silentStreak int
	silentCalls  int
	silentRung   int
	// noNewStreak counts consecutive batches in which every observed call
	// brought back zero fresh lines. noNewNudged makes the rule one-shot for this
	// streak; either fresh information or a successful write rearms it.
	noNewStreak int
	noNewNudged bool
	// nudges is how many STOPPING nudges this turn has produced — the count the
	// hand-off ceiling is read against. Silent notes never touch it, and a batch
	// of forward progress gives one of them back.
	nudges int
	// dir is the directory whose worktree answers "did anything actually change"
	// for a batch of shell commands, and "" for a watch with no workspace behind
	// it. dirt is the last fingerprint read from it, and dirtRead says one has
	// been read at all: THE FIRST READING IS A BASELINE AND NEVER PROGRESS,
	// because a tree that was already dirty when the turn opened is not work this
	// batch did.
	dir      string
	dirt     string
	dirtRead bool
	// clock and ledger are the turn's account of ITSELF rather than of its
	// repetitions: when the work last changed, and how much of what has come
	// back since was new (novelty.go). The ledger also supplies the structural
	// no-new-information rule above, while the clock remains description only;
	// a note that says "you
	// have repeated this three times" is much more useful beside "and the work
	// has not changed since step 12".
	//
	// IT IS THE RUNNER'S LEDGER AND NOT A SECOND ONE. The no-progress counter
	// out at the task boundary (task_run.go's [addedSomething]) and this note
	// are two readings of the same run, and the day they are kept by two
	// mechanisms is the day a node is told the work has been moving while the
	// counter that kills it says otherwise. Everything here goes through
	// [progressLedger] — the line memory, the questions, and the change that
	// arms the reading after it — so the two sides count the same way.
	clock  workClock
	ledger *progressLedger
}

func newLoopWatch() *loopWatch {
	return &loopWatch{
		errors: make(map[string]int, 4),
		named:  make(map[string]bool, 2),
		streak: make(map[string]int, 2),
		ledger: newProgressLedger(),
	}
}

// observe folds one finished batch into the watch and reports a nudge if this
// batch is the one that tipped a rule over.
//
// AT MOST ONE NUDGE PER BATCH, even when several rules fire: two notes about the
// same moment is the harness being noisy about its own cleverness. The identity
// rules are more specific than silence, so they are read first.
//
// FORWARD EVIDENCE IS READ FIRST, before any rule is tested, so a batch that
// both progressed and repeated cannot escalate. That ordering is the hysteresis
// law's "immediately": a model that got something done this step is not a model
// the harness interrupts this step, even if it also re-ran the thing it was
// nudged about.
func (w *loopWatch) observe(calls []ai.ToolCall, results []toolResult, visibleText bool) (nudge, bool) {
	if w == nil || len(calls) == 0 {
		return nudge{}, false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	// The third signal is terminal. loop.go spends it at this same boundary, but
	// keeping the cap here makes the watch's own contract true even for a caller
	// that inspects it directly: there is no fourth nudge after a hand-off signal.
	if w.nudges > loopNudgeCeiling {
		return nudge{}, false
	}
	worldCalls := 0
	for index := range calls {
		if index < len(results) && results[index].harness {
			continue
		}
		worldCalls++
	}
	if worldCalls == 0 {
		return nudge{}, false
	}

	// BOTH KINDS OF FORWARD EVIDENCE ARE READ BEFORE ANY RULE IS TESTED, so a
	// batch that both progressed and repeated cannot escalate. The weak kind
	// empties the per-signature streaks; the strong kind also breaks the silent
	// ladder and gives a spent note back.
	if w.sawProgress(calls, results) {
		clear(w.streak)
	}
	material := w.materialProgress(calls, results)
	if visibleText || material {
		w.silentStreak = 0
		w.silentCalls = 0
		w.silentRung = 0
	} else {
		w.silentStreak++
		w.silentCalls += worldCalls
	}

	var found nudge
	ok := false
	observed := 0
	batchFresh := material
	for index, call := range calls {
		// ── A FAILURE THE HARNESS WROTE IS NOT THE MODEL REPEATING ITSELF ──
		//
		// Measured in SWE-Marathon s4 (withdrawn.go): the harness took `bash` off
		// a worker's belt mid-run, answered every call to it "Unknown tool: bash",
		// and this watch then injected three [stuck] notes telling the worker it
		// had "repeated bash 4 times and it has failed the same way each time" —
		// which was true, and which the harness had caused. A withdrawn hand, a
		// refused door and every other answer written on this side of the wall
		// are skipped ENTIRELY: not entered in the window, not counted as an
		// error, so neither rule can fire on them and neither can a later, real
		// repetition be blamed on the run they interrupted.
		if index < len(results) && results[index].harness {
			continue
		}
		observed++
		batchFresh = w.count(call, results, index) || batchFresh
		signature := callSignature(call)
		w.recent = append(w.recent, signature)
		if len(w.recent) > loopWindow {
			w.recent = w.recent[len(w.recent)-loopWindow:]
		}
		run := w.trailingRun(signature)

		// ── THE ARGUMENT REFUSAL, WHICH ARRIVES WITH ITS OWN CURE ──
		//
		// The tool wrote the corrected call into its own refusal (toolargs.go),
		// so a second identical call is the END of the evidence rather than the
		// start of it, and the repair rides out as the nudge's fact.
		//
		// IT OWNS THE SIGNATURE WHERE IT APPLIES. The general call rule below is
		// the same signature counted to a different number, so testing both
		// would ask [loopWatch.speakAbout] about one repetition twice — which
		// climbs the hysteresis ladder at double speed and names one loop twice
		// in two vocabularies.
		repair, refused := "", false
		if index < len(results) && results[index].isError {
			repair, refused = argumentRepair(results[index].text)
		}
		switch {
		case refused:
			if run >= loopInvalidRepeats && !ok && w.speakAbout(signature) {
				found, ok = nudge{
					call: call, tool: call.Function.Name, count: run,
					failing: true, invalid: true, fact: repair,
				}, true
				// AND THE ERROR RULE IS TOLD IT HAS BEEN SAID. Left unmarked, the
				// same refusal would tip the general rule one call later and the
				// turn would be nudged twice about one loop. Marking it books the
				// hysteresis exactly as a nudge of its own would have.
				w.named[errorSignature(results[index].text)] = true
			}
		case run >= loopRepeats && !ok && w.speakAbout(signature):
			found, ok = nudge{call: call, tool: call.Function.Name, count: run}, true
		}

		if index >= len(results) || !results[index].isError {
			continue
		}
		failure := errorSignature(results[index].text)
		w.errors[failure]++
		if count := w.errors[failure]; count >= loopRepeats && !ok && w.speakAbout(failure) {
			found, ok = nudge{call: call, tool: call.Function.Name, count: count, failing: true}, true
		}
	}
	if observed > 0 {
		if batchFresh {
			w.noNewStreak = 0
			w.noNewNudged = false
		} else {
			w.noNewStreak++
		}
	}
	if w.noNewStreak >= noNewInformationLimit && !w.noNewNudged {
		// Booking the rung even when a more specific identity rule won this batch
		// preserves AT MOST ONE NUDGE PER BATCH without letting the two rules take
		// turns describing the same stalled stretch.
		w.noNewNudged = true
		if !ok {
			last := calls[len(calls)-1]
			found, ok = nudge{
				call:  last,
				tool:  last.Function.Name,
				count: w.noNewStreak,
				stale: true,
			}, true
		}
	}
	if w.silentRung < silentRungs && w.silentStreak >= silentThreshold(w.silentRung) {
		// An identity nudge from this same batch already told the model the turn
		// is stuck. Booking silence with it avoids two rules taking turns to say
		// the same moment is bad, while distinct-call silence gets its own words.
		w.silentRung++
		if !ok {
			last := calls[len(calls)-1]
			found, ok = nudge{
				call:       last,
				tool:       last.Function.Name,
				count:      w.silentCalls,
				silent:     true,
				silentRung: w.silentRung,
			}, true
		}
	}
	// THE COUNT COMES BACK BEFORE IT IS SPENT. A batch that got something done
	// and also tipped a rule over cannot escalate on it: the step down and the
	// step up cancel, and the note goes out as an aside.
	if material && w.nudges > 0 {
		w.nudges--
	}
	if !ok {
		return nudge{}, false
	}
	// AND A SILENT NOTE BOOKS NOTHING. It keeps its rung, it says its piece, and
	// the hand-off ceiling never hears about it: nth stays zero, which is what
	// [Agent.nudgeIfLooping] reads as "this one cannot end the turn".
	if !found.silent {
		w.nudges++
		found.nth = w.nudges
	}
	// The clock's note is the fallback and never an override: an argument
	// refusal already put the sentence that matters here.
	if found.fact == "" {
		found.fact = w.clock.note()
	}
	return found, true
}

// silentThreshold derives every rung from the first: rung zero is six batches,
// then each rung doubles. Its caller bounds the rung by [silentRungs], so one
// silent stretch cannot earn a fourth note.
func silentThreshold(rung int) int {
	return silentStreakLimit << rung
}

// silentLadderRung is how many rungs of the silent ladder this turn's CURRENT
// stretch of silence has spoken, 1-based and zero for a turn that has written
// something.
//
// It is the one thing this watch says about itself to anybody outside it, and
// the predicate the write-your-notes rule is enforced on (processrule.go). It is
// read under the watch's own lock, and a nil watch — an episode assembled by a
// test that has no detector — answers zero, which is "there is nothing to
// enforce" and not "enforce everything".
func (w *loopWatch) silentLadderRung() int {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.silentRung
}

// materialProgress reports the STRONG kind of forward evidence: this batch put
// something in the world. It is what breaks a silent streak and what gives a
// spent note back, and it has two halves.
//
// THE CHEAP HALF is a successful call through a belt hand whose effect on the
// disk is a known path ([mutatingTools]). [loopWatch.sawProgress] stays
// deliberately broader for signature hysteresis, but using it here would let a
// run of distinct successful greps reset the ladder forever — precisely the
// silent loop these rules exist to catch.
//
// THE OTHER HALF IS THE TREE. A commit, a push, a landing phase is pure bash, so
// a worker doing the last and most valuable part of its job is structurally
// silent AND structurally reading nothing new — which is how six distinct,
// successful git calls came to be written up as a turn going in circles. The one
// signal nobody can argue with is whether the tree looks different from a step
// ago, and it is THE RUNNER'S OWN reading ([worktreeDirt], task_run.go): one
// fingerprint, one exclusion of the harness's own droppings, so the leash out at
// the task boundary and this watch cannot disagree about what moving means.
//
// IT IS READ ONCE PER BATCH AND ONLY WHEN THE ANSWER COULD CHANGE ONE — never
// per call. A batch that already wrote a file has its answer, and a batch with no
// unnamed successful shell command in it has no verb that could have moved the
// tree unseen. A git call per model step is thirty milliseconds against a model
// round trip; a git call per tool call is a process per grep.
//
// AND A CALL THIS TURN HAS ALREADY BEEN NUDGED ABOUT COUNTS FOR NOTHING, however
// well it went — [loopWatch.sawProgress]'s rule, and it has to hold here too. A
// model writing the same file with the same content seven times is looping, and
// a refund the loop paid itself would put the hand-off ceiling out of reach.
func (w *loopWatch) materialProgress(calls []ai.ToolCall, results []toolResult) bool {
	shell := false
	for index, call := range calls {
		if index >= len(results) || results[index].isError || results[index].harness {
			continue
		}
		if w.named[callSignature(call)] {
			continue
		}
		if mutatingTools[call.Function.Name] {
			return true
		}
		if call.Function.Name == "bash" {
			shell = true
		}
	}
	return shell && w.treeMoved()
}

// treeMoved reports whether the worktree under this turn's workspace looks
// different from the last time this watch looked at it, and records the new
// fingerprint either way.
//
// A watch with no directory behind it, and a directory that is not a
// repository, both answer no forever — stable, so they never move any counter.
// THE FIRST READING IS A BASELINE. With nothing to compare against, a tree that
// was already dirty when the turn opened would read as work this batch did, and
// the whole point of the reading is that it is the one claim nobody can argue
// with.
//
// It keeps its OWN fingerprint rather than sharing the runner's cell: reading a
// transition consumes it, and two readers sharing one cell would each see half
// the movement (task_run.go's [worktreeMoved] is that cell's only owner).
func (w *loopWatch) treeMoved() bool {
	if w.dir == "" {
		return false
	}
	dirt := worktreeDirt(w.dir)
	if !w.dirtRead {
		w.dirtRead = true
		w.dirt = dirt
		return false
	}
	if dirt == w.dirt {
		return false
	}
	w.dirt = dirt
	return true
}

// count folds one call and its result into the turn's clock: a step taken, what
// the result brought back, and whether the work itself moved.
//
// THE WORK MOVING IS A SUCCESSFUL CALL TO A HAND THAT SAVES A FILE
// ([savingTools], task_run.go) — the same rule the landing stages a node's
// deliverable by ([stageTaskWork]), so the two cannot disagree about what the
// work is. A failed edit changed nothing; a command that dirtied the directory
// wrote droppings the landing would not take either.
//
// AND A HAND THAT SAVED SOMETHING IS INFORMATION BY CONSTRUCTION, whatever its
// confirmation said. `wrote 12 lines` is boilerplate the second time, so its
// bytes are weighed and then the clock is reset around them: what is being
// counted since is what came back AFTER the world last changed — and the ledger
// is told the same thing, so the reading that follows a write is read the way
// the runner's counter reads it (novelty.go's [progressLedger]).
//
// THE WATCH READS INFORMATIVENESS STRUCTURALLY and never semantically. Fresh
// lines reset the no-new-information streak; zero fresh lines advance it. What
// it needs from the ledger is the BOOKS — the same lines remembered, the same
// questions counted, the same change spent — so the [stuck] note's fact and the
// counter out at the task boundary are one account of one run.
func (w *loopWatch) count(call ai.ToolCall, results []toolResult, index int) bool {
	w.clock.step()
	failed := index >= len(results) || results[index].isError
	freshResult := false
	if index < len(results) {
		_, fresh, lines := w.ledger.read(call.Function.Name, call.Function.Arguments,
			stripJobFooter(results[index].text))
		w.clock.read(fresh, lines)
		freshResult = fresh > 0
	}
	if savingTools[call.Function.Name] && !failed {
		w.clock.wrote()
		w.ledger.wrote()
		return true
	}
	return freshResult
}

// speakAbout reports whether a signature that has just tipped a rule over is
// worth saying something about, and books the consequence of the answer.
//
// The first time, always: the loop has a name nobody has said yet. After that it
// is the hysteresis law — the signature has to come back [loopHysteresis] more
// times before the ladder advances, and the streak resets on the escalation so
// the next one costs the same evidence again rather than firing on every step
// from here on.
func (w *loopWatch) speakAbout(signature string) bool {
	if !w.named[signature] {
		w.named[signature] = true
		return true
	}
	w.streak[signature]++
	if w.streak[signature] < loopHysteresis {
		return false
	}
	w.streak[signature] = 0
	return true
}

// sawProgress reports whether this batch contains forward movement: a call that
// SUCCEEDED and that this turn has not already been nudged about.
//
// A named signature repeating is not progress however well it went — the model
// re-running a successful grep for the fourth time is the loop, not the way out
// of it — and a failed call is not progress by definition. Everything else is:
// the turn did something new and it worked.
func (w *loopWatch) sawProgress(calls []ai.ToolCall, results []toolResult) bool {
	for index, call := range calls {
		if index >= len(results) || results[index].isError {
			continue
		}
		if w.named[callSignature(call)] {
			continue
		}
		return true
	}
	return false
}

// trailingRun is how many entries at the END of the window are this signature.
// Only the tail counts: the rule is "three times in a row", and a signature seen
// three times with other calls between them is a model working, not looping.
func (w *loopWatch) trailingRun(signature string) int {
	run := 0
	for index := len(w.recent) - 1; index >= 0; index-- {
		if w.recent[index] != signature {
			break
		}
		run++
	}
	return run
}

// callSignature identifies a call by what it DOES: the tool and its arguments,
// hashed so the window costs bytes rather than kilobytes. The hash is fnv — this
// is a cache key for a nudge, not a claim about anything, and a collision costs
// one note that names the wrong tool.
func callSignature(call ai.ToolCall) string {
	digest := fnv.New64a()
	_, _ = digest.Write([]byte(call.Function.Name))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(strings.TrimSpace(call.Function.Arguments)))
	return fmt.Sprintf("call:%x", digest.Sum64())
}

// errorSignature identifies a failure by its text. Whitespace is folded because
// the same failure re-run is the same failure however a shell wrapped its lines.
//
// THE JOB FOOTER COMES OFF FIRST, for the reason task_run.go's progress counter
// strips it (jobfooter.go): every result carries the elapsed time of every
// outstanding job, so two identical failures a minute apart hash differently and
// a detector built to notice a repeat would notice nothing at all whenever a
// background job happened to be running.
func errorSignature(text string) string {
	digest := fnv.New64a()
	_, _ = digest.Write([]byte(strings.Join(strings.Fields(stripJobFooter(text)), " ")))
	return fmt.Sprintf("error:%x", digest.Sum64())
}

// nudgeNote is what the model reads. It states the fact, then asks the two
// questions a stuck model has stopped asking itself.
func nudgeNote(n nudge) string {
	// AN ARGUMENT REFUSAL IS ANSWERED WITH THE CORRECTION AND NOTHING ELSE. The
	// two questions below — which assumption is wrong, what is a different way —
	// are the right questions about work that keeps failing in the world and the
	// wrong ones about a call the tool has already spelled out for you.
	if n.invalid {
		note := fmt.Sprintf("[stuck] You have sent the same %s call %d times and it was refused the same way each time.", n.tool, n.count)
		if n.fact != "" {
			note += " " + n.fact
		}
		return note + " Send the corrected call, or say what you needed and stop — sending this one again cannot work."
	}
	if n.silent {
		note := fmt.Sprintf("[silent] You have made %d tool calls without writing anything down. "+
			"Before your next tool call, write a short visible note: what you've learned so far, "+
			"what you're checking next, and why. Your reasoning between steps is not saved — "+
			"if it isn't in your visible reply, it's gone.", n.count)
		// AND THE SECOND RUNG SAYS WHAT IS ABOUT TO HAPPEN, because from here it
		// is true: the loop holds the next tool-calls-only submission rather than
		// running it (processrule.go). This sentence used to promise exactly that
		// with nothing behind it, thirteen times in one measured conversation.
		if n.silentRung >= silentEnforceRung {
			note += " This is the second and last note about it: from here your tool calls are held. The next reply that carries only tool calls will not be run."
		}
		return note
	}
	if n.stale {
		return fmt.Sprintf("[stuck] The last %d rounds read nothing new; what you are looking for is already in the transcript. "+
			"Use what is already there to take a different action, or say what remains blocked and stop.", n.count)
	}
	what := "the same " + n.tool + " call"
	outcome := "with the same result"
	if n.failing {
		what = n.tool
		outcome = "and it has failed the same way each time"
	}
	note := fmt.Sprintf("[stuck] You have repeated %s %d times %s.", what, n.count, outcome)
	// AND THEN THE FACT ABOUT THE WORK, when there is one. The repetition is
	// what the model did; this is what the work did, and it is the half a model
	// in a measure→measure loop cannot see — every run of its script answered,
	// every answer looked slightly different, and the thing being measured had
	// not moved since it started (novelty.go's [workClock]).
	if n.fact != "" {
		note += " " + n.fact
	}
	return note + " Rethink your approach: " +
		"which assumption is wrong, and what is a different way to get this done? " +
		"If there is no different way, say so and stop rather than trying again."
}

// loopRule is the compact event hint naming the signal to the person. The
// retained explicit recovery helper also uses it as its question's rule.
func loopRule(n nudge) string {
	if n.invalid {
		return fmt.Sprintf("stuck: %s was sent the same wrong argument %d times", n.tool, n.count)
	}
	if n.silent {
		return fmt.Sprintf("silent: %d tool calls without visible assistant text", n.count)
	}
	if n.stale {
		return fmt.Sprintf("stuck: the last %d rounds read nothing new", n.count)
	}
	if n.failing {
		return fmt.Sprintf("stuck: %s has failed the same way %d times", n.tool, n.count)
	}
	return fmt.Sprintf("stuck: the same %s call %d times", n.tool, n.count)
}

// ── the turn's side ─────────────────────────────────────────────────────────

// nudgeIfLooping folds one finished batch into the turn's watch and says
// something if it tipped a rule over. It is the loop detector's `post-feedback`
// hook (hooks.go), so it runs at the step boundary, after the batch's results
// are in the transcript and before the next request is assembled — the one
// moment a note can ride into the next request the way a person's steering does.
//
// The first two stopping nudges are asides, and every silent note is one
// forever. Past the ceiling the episode is marked for the main loop to end
// through checkpointing, because only that caller owns the turn usage and the
// person's original request needed by the hand-off.
func (a *Agent) nudgeIfLooping(ctx context.Context, hub *eventHub, ep *episode, calls []ai.ToolCall, results []toolResult, visibleText bool) {
	// WAITING ON HANDED-OUT PARTS IS NEITHER WORKING NOR SPINNING. A parent with
	// pieces outstanding has no new information because those pieces are still
	// making it elsewhere; task_run.go parks it at the end of this turn and folds
	// every report into the next one. Counting its last looks here would end the
	// turn for the right reason but spend warnings on a state that resets when the
	// reports arrive, contradicting the task park's shared no-progress law.
	if a.childrenOutstanding() {
		return
	}
	looping, ok := ep.watch.observe(calls, results, visibleText)
	if !ok {
		return
	}
	// The event fires for every nudge, escalated or not: what a surface draws is
	// "this turn is going in circles", which is true either way.
	hub.send(Event{
		Kind:  EventNudge,
		Tool:  looping.tool,
		Count: looping.count,
		Hint:  loopRule(looping),
	})

	// AND THE PROCESS RULE THIS NOTE BELONGS TO COUNTS IT (processrule.go). The
	// advisory rungs above are one rule's first step, and how often that step has
	// had to be taken in this CONVERSATION is the number the enforced rung is
	// measured against — thirty-two of them in the run that ordered the
	// enforcement, and no turn-shaped counter could ever have said so.
	a.countProcessRuleAdvice(looping)

	// Past the ceiling no fourth message is useful. The hook cannot end a turn,
	// so it leaves the decision on the episode for loop.go to spend immediately.
	// A SILENT NOTE CARRIES nth 0 AND NEVER REACHES THIS. What it is about is the
	// record, not the work, and taking a turn away from a worker that is landing
	// commits because it landed them quietly is the defect this guard caused.
	if looping.nth > loopNudgeCeiling {
		ep.loopHandoff = true
		return
	}
	// The AMBIENT lane (agent.go): a nudge belongs to the turn it is about and
	// nobody is waiting to be told about it, so it never starts one.
	a.enqueueAmbientNote(nudgeNote(looping))
}

// loopLeftUndoneNote is the sentence a handed-over turn leaves behind, and the
// one this package rather than a worker wrote — task_run.go's [endingOfClaim]
// reads it back to say a node "went in circles". ONLY THE RULES THAT CLAIM THE
// TURN IS NOT MOVING can reach it: a silent note books no nudge, so no amount of
// quiet work can put these words on a row.
const loopLeftUndoneNote = "this turn is going in circles · stopping here with anything remaining left undone"

// handOverLoopingTurn spends the terminal signal at the one point that owns all
// of checkpointing's inputs. A capable conversation uses the ordinary ceiling
// road; every other shape still ends, records an honest line, and leaves disk
// exactly as the turn left it.
func (a *Agent) handOverLoopingTurn(ctx context.Context, hub *eventHub, user userMessage, meter *checkpointMeter, turn *Usage, started time.Time, model string) bool {
	if a.checkpoints(ctx, user) {
		rounds := meter.rounds + 1
		read := a.readMark(ctx)
		a.journalMarkRead(read, checkpointMarks, rounds, checkpointDecisionContinue)
		if a.checkpointCeiling(ctx, hub, turn, started, model, rounds, meter.raced, read) {
			return true
		}
	}
	hub.send(Event{Kind: EventNotice, Text: loopLeftUndoneNote})
	a.record(textMessage("assistant", loopLeftUndoneNote))
	hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(*turn, started, model)})
	a.maybeTitle(ctx, hub)
	return true
}

// promptMode reports whether this session's blanket answer is "ask me".
//
// Approval tests use this policy reading independently of the loop ceiling. It
// reads the DEFAULT rather than one tool's answer because it asks whether this
// is a session where somebody is expected to answer questions.
func (a *Agent) promptMode() bool {
	policy := a.approvalGate()
	if policy == nil {
		return false
	}
	return policy.Default == approval.ActionPrompt || policy.Default == ""
}
