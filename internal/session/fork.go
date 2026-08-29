package session

// ── THE THIRD WEIGHT OF PARALLELISM ─────────────────────────────────────────
//
// This session already has two ways to do several things at once, and both are
// heavy. A TASK (task.go) is a piece of work handed away: it gets a brief
// somebody has to write, a copy of the repository, a room, a check and a
// landing. A DIVIDED task (task_divide.go) is that again, several times over,
// under one parent. Both are right for work that outlives the answer, and both
// cost a brief — and a brief is the expensive part, because a brief is one mind
// trying to write down what another mind would need to know.
//
// FORK IS THE WEIGHT UNDER THEM. Mid-work, the running mind copies itself: two
// to four HANDS, each starting with the caller's ENTIRE TRANSCRIPT, told one
// line each about what makes it different from its siblings. Nobody writes a
// brief, because THE CONTEXT IS THE BRIEF. On every provider that caches a
// prompt prefix, the transcript the hands re-send is billed at the cache-read
// rate, which is the whole economic argument: a hand costs its own output and
// almost nothing for everything it knows.
//
// It is fork-join with copy-on-write memory, and the biology is the same shape:
// mitosis, plus the lateral inhibition this repository has already measured —
// telling each cell what its neighbours own is what stops them all growing into
// the same place.
//
// ── WHAT MAKES IT SAFE, AND IT IS NOT THE MODEL'S JUDGEMENT ──
//
// The hands run CONCURRENTLY IN ONE WORKING DIRECTORY. There is no worktree, no
// branch and no merge — that machinery is what makes a task heavy, and paying
// for it here would leave fork weighing what a task weighs. What replaces it is
// DECLARED WRITE SCOPE, enforced deterministically:
//
//   - Every hand declares the paths it may write, and [writeGuard] — the
//     pre-action citizen the adaptive run already uses (orchestrate.go) —
//     refuses `edit` and `write` outside them. Disjoint scopes make a conflict
//     impossible by construction rather than unlikely by instruction.
//   - OVERLAPPING SCOPES ARE REFUSED AT THE CALL, before a single hand starts.
//     Two hands sharing a path is the one shape the guard cannot save, so it
//     never gets to exist.
//   - `bash` is the hole every path gate has, and this one is closed rather than
//     admitted: a hand's bash is the auditor's read-only bash with its own
//     allowlist ([forkCommands]), so a hand can orient itself and cannot change
//     anything the guard did not see.
//   - The hand's belt is COMPOSED, not filtered ([forkBelt]). A verb reaches a
//     hand only because this file names it, so the media hands — which write to
//     model-chosen paths the guard never reads, and which in `generate_video`'s
//     case write minutes later on a detached goroutine — are simply absent.
//
// ── AND WHAT IT DOES NOT DO ──
//
// No hand may fork: the verb is absent from a hand's belt, not present and
// refusing, so the depth is one and a model in a hand never plans around a road
// it does not have. And nothing is built or tested inside a hand — three hands
// editing one tree means every build reads a half-written repository, so a green
// one proves nothing and a red one is a sibling's unfinished work that this hand
// would then "fix". The caller builds, tests and stitches, which is the same law
// divide_work keeps: the parent stays the integrator.
//
// ── A HAND IS A STREAM, NOT A BARRIER ───────────────────────────────────────
//
// This verb used to JOIN. `fork` ran its hands on goroutines and waited on all
// of them, so the caller's tool call blocked until the SLOWEST hand came home.
// Here is what that cost, measured, on one task of a Rust benchmark run:
//
//	00:53:52  the worker forks three scope-guarded hands
//	00:55:19  hand 1 is finished — eleven calls, ninety seconds
//	01:11:28  hand 2 hits its round cap mid-fix
//	01:33:05  hand 3 hits its round cap; the fork call finally returns
//	          the worker rebuilds, runs the check, and gains 29 passes
//
// Forty minutes for work that was ready after two. The worker did NOTHING for
// thirty-nine of them — it could not build, could not measure, could not even
// look, because it was inside a tool call — while hand 1's finished slice sat in
// the working copy unbuilt and unmeasured. THE BARRIER WAS THE DEFECT, not the
// round cap and not the hands.
//
// So a hand is now a STREAM, which is already this session's law for every other
// piece of work it starts (tools_jobs.go, jobs.go, jobfooter.go): every command
// is a stream, completions arrive on their own, and the model never polls.
// Concretely, and reusing that seam rather than a second one beside it:
//
//   - `fork` RETURNS AS SOON AS THE HANDS ARE OUT, naming them. The caller's
//     next thought is its own, not a wait.
//   - EACH HAND IS A JOB (jobs.go's [jobKindHand]): one id space, one log file,
//     one row, one `jobs kill`, one death at Close.
//   - EACH HAND'S REPORT ARRIVES ON THE STEERING LANE the moment that hand
//     finishes — the same lane a background job's exit rides in on, landing in
//     the caller's conversation as its own message at the next step boundary, in
//     the order the hands COME HOME rather than the order they were declared.
//   - EVERY HAND STILL OUT RIDES AT THE FOOT OF EVERY TOOL RESULT the caller
//     reads (jobfooter.go), with its age and its last line, so the caller can
//     never lose track of one and never has to ask.
//
// The caller keeps working. It may build and measure hand 1's slice while hands
// 2 and 3 are still out, which is the whole of the forty minutes back.
//
// Nothing about the SAFETY changed: the scopes are still declared, still
// normalized at the door, still refused when they overlap, still enforced by the
// guard, and a hand's bash still reads and never writes.
//
// ── WHAT HAPPENS TO A HAND STILL OUT WHEN THE WORK ENDS ──
//
// THE LANDING WAITS. A hand is outstanding work this agent handed to itself, so
// it is counted where a sub-task's unread report is counted
// ([Agent.childrenOutstanding]) and a task node's runner parks on it exactly as
// it parks on a part it divided out (task_run.go's [runTaskChild] tail loop):
// the node does not land until every hand has reported and the model has read
// what came back. That is the same answer this session already gives for every
// other piece of work it handed out and has not heard about, and the alternative
// — landing on top of a hand — would throw away the very writes the fork was for.
//
// In a CONVERSATION the same thing happens through the same lane with no waiting
// at all: a report that lands after the turn has ended WAKES the session, which
// is what a background job's exit already does ([Agent.enqueueSteering]).
//
// The person's INTERRUPT is the one thing that ends a hand where it stands, and
// that is a difference from a background job on purpose: a job is a command
// somebody asked to be left running, and a hand is this mind finishing an answer
// nobody is waiting for any more ([jobRegistry.stopHands]). So is Close, which
// stops every job this session started.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	// forkFanLimit is how many hands one call may open.
	//
	// Four, and the ceiling is about the SCOPE rather than about the machine.
	// Every hand needs a slice of the tree nobody else may write, and a caller
	// that can honestly name five disjoint slices of what it is doing right now
	// is a caller that has found a task rather than a fork — that is what
	// propose_task with `wide` is for, and it has the worktrees to survive it.
	// Two is the floor for the same reason it is divide_work's: one hand is the
	// caller doing the work itself, more slowly.
	forkFanLimit = 4

	// forkHandFloor is the other end of the same fact, spelled so the schema and
	// the parser cannot disagree about it.
	forkHandFloor = 2

	// forkRounds is one hand's whole budget, counted in FINISHED TOOL ROUNDS —
	// the same unit the checkpoint meter counts a turn's price in
	// (checkpoint.go), and counted at the same seam.
	//
	// Fifteen, derived from the price that is already in this codebase rather
	// than picked. The checkpoint's price is 10 rounds: the point at which a
	// turn has cost enough that somebody reads it and asks whether it should
	// have been handed over. A hand is a SHORT ERRAND carved out of a turn that
	// has already run — it spends no rounds orienting, because it opens with
	// everything the caller had read — so its natural budget is that price plus
	// the half that lets it finish what it found rather than stop mid-edit.
	// Four hands at fifteen is sixty rounds of work under one round of the
	// person's waiting, which is the trade this verb exists to make.
	forkRounds = 15

	// forkSayLimit bounds what ONE hand's report may weigh in the caller's
	// transcript. The caller stitches from these, so they are the deliverable
	// and not a status line — a task's three-line report ([taskReportLines])
	// would throw away the findings the fork was for. Four hands at this bound
	// is 16KB, which is a large tool result and a small fraction of the
	// transcript they all inherited.
	forkSayLimit = 4000
)

// forkCommands is what a hand's bash may run: ORIENTATION, and nothing else.
//
// IT IS THE AUDITOR'S OWN READING SET, AND THE TWO ARE ONE LIST BECAUSE THEY
// ANSWER ONE QUESTION: which commands PRINT and cannot change the thing being
// looked at (task_checks.go's [auditReadCommands]). The auditor gets checks on
// top of these, read off the work it is judging; a hand gets nothing on top, and
// the reason is the one stated at the head of this file. The hands share one
// working directory, so a build or a test inside a hand reads a tree its
// siblings are still writing: the green ones prove nothing, and the red ones are
// somebody else's half-finished work that this hand would then stop and repair —
// the exact redo the sibling-scope sentence exists to prevent. Builds and tests
// are the caller's, after the join.
var forkCommands = auditReadCommands

// forkShell is the voice a hand's refused command is answered in, and the last
// line of every one of those refusals says where to go instead — the auditor's
// hint made this file's ([auditReaderHint] states the argument for having one).
var forkShell = shellLeash{
	who:     "a hand",
	forWhat: "orientation",
	hint: "For looking around, use read, grep, find and ls — that is what they are for. " +
		"Builds and tests belong to whoever forked you, never to you.",
}

// ── the verb ────────────────────────────────────────────────────────────────

// forkDescription teaches the PRINCIPLE and names no shape of work, because the
// judgement it asks for is not about code: it is about whether what you are
// doing right now has slices in it that could proceed side by side on what you
// have already read. An example would narrow that to whatever the example was.
//
// The two constants are interpolated for the reason every number on this belt
// is (CLAUDE.md's one-source-of-truth law): a schema saying four while the
// parser allowed five is how a model ends up reasoning from a figure nothing
// enforces.
var forkDescription = "Copy yourself into " + strconv.Itoa(forkHandFloor) + "–" + strconv.Itoa(forkFanLimit) +
	" hands working side by side inside this turn. Each opens with EVERYTHING you have read and said — you write " +
	"no brief, your context IS the brief — and is told only the one line that makes it different from its " +
	"siblings. Reach for it the moment you can name slices of what you are ALREADY doing that could proceed on " +
	"what you already know; work you would have to explain from scratch is a task instead. Each hand may write " +
	"only the paths it declares, so the slices must be genuinely separate. A hand reads, edits and writes; it " +
	"cannot build, test or fork again, and gets " + strconv.Itoa(forkRounds) + " tool rounds. RETURNS AT ONCE, " +
	"naming them; each report arrives on its own as that hand finishes, and hands still out ride at the foot of " +
	"every result you read. Never wait or poll — keep working, and build and test each slice as its report lands."

// forkSchemaJSON is the wire schema. It is a literal for the reason every other
// schema in this package is one — the bytes go on the wire and a test can pin
// them — and its numbers come from the same constants the parser reads.
var forkSchemaJSON = `{"type":"object","properties":{` +
	`"parts":{"type":"array","minItems":` + strconv.Itoa(forkHandFloor) + `,"maxItems":` + strconv.Itoa(forkFanLimit) +
	`,"description":"The hands, in order.","items":{"type":"object","properties":{` +
	`"role":{"type":"string","description":"ONE line saying what THIS hand does, written as the DIFFERENCE from its siblings. It has read everything you have read, so never restate the work or the context — say only what is this hand's."},` +
	`"scope":{"type":"array","minItems":1,"items":{"type":"string"},"description":"The files and directories this hand may write, workspace-relative. A directory covers everything under it. No two hands may share a path."},` +
	`"grade":{"type":"string","enum":["` + gradeMechanical + `","` + gradeCareful + `"],"description":"Leave out for ordinary work. Set to ` + gradeCareful + ` for a hand whose work could look finished and be quietly wrong, which lifts it to the careful tier."}` +
	`},"required":["role","scope"],"additionalProperties":false}},` +
	`"note":{"type":"string","description":"Anything EVERY hand must know that is not already in what you have read. Usually empty: they were there."}` +
	`},"required":["parts"],"additionalProperties":false}`

// forkTools is the verb, absent where it cannot honestly be offered.
//
// A hand does not get it, which is the whole of the depth-one law: a model that
// has the verb plans around having it, so a hand that could not actually use it
// must not be told it has one (CLAUDE.md's absence law). Nothing else gates it —
// a chat turn and a task worker both fork, because both are minds mid-work with
// a context worth copying.
func (a *Agent) forkTools() []bare.Tool {
	if a.config.inHand {
		return nil
	}
	return []bare.Tool{{
		Name:        "fork",
		Description: forkDescription,
		Schema:      json.RawMessage(forkSchemaJSON),
		Execute:     a.forkHands,
	}}
}

// forkPart is one hand as the model asked for it.
type forkPart struct {
	Role  string   `json:"role"`
	Scope []string `json:"scope"`
	Grade string   `json:"grade,omitempty"`
}

// careful reports whether this hand rides the careful tier. It reads exactly as
// [dividePart.careful] does, because a grade means one thing on this belt.
func (p forkPart) careful() bool {
	return strings.EqualFold(strings.TrimSpace(p.Grade), gradeCareful)
}

type forkArguments struct {
	Parts []forkPart `json:"parts"`
	Note  string     `json:"note,omitempty"`
}

// ── the call ────────────────────────────────────────────────────────────────

// forkHands is the verb's whole life: read the parts, refuse what cannot be
// made safe, copy the transcript, PUT THE HANDS OUT, and hand back their names.
//
// IT IS ASYNCHRONOUS, and that is the point (see this file's head). What the
// caller gets back is a roster, not a result; the results arrive one at a time
// on the steering lane as the hands come home. The caller is still the
// integrator — it is the only thing that can build, test and stitch — but it is
// an integrator that works while its hands do, instead of one parked inside a
// tool call for as long as its slowest hand takes.
//
// The ctx here is the TOOL CALL'S and is deliberately used for nothing but the
// parse: a hand that ran on it would die the instant this call returned, which
// is one line after it starts. A hand's own context comes from its job
// ([jobRegistry.startHand]) and its life is the session's.
func (a *Agent) forkHands(_ context.Context, args json.RawMessage) (string, bool, error) {
	// THE WORKSPACE IS PASSED IN BECAUSE THE SCOPE IS READ AGAINST IT. A scope is
	// a slice of this directory, and the door cannot say whether a path is inside
	// it without knowing which directory it is (see [normalizeScopePath]).
	parsed, problem := parseForkArguments(a.config.Workspace, args)
	if problem != "" {
		return problem, true, nil
	}

	// THE TRANSCRIPT IS READ OFF THE CALLER ONCE, here, rather than by each hand:
	// a snapshot taken per hand would be the same read three times over, and —
	// worse — three reads at three different instants of a transcript another
	// hand's work could already be showing up in.
	seed, system := a.forkSeed()
	if len(seed) == 0 {
		return "fork found nothing to copy: there is no conversation behind this call yet.", true, nil
	}

	// THE PERSON HEARS ONCE, AT THE MOMENT THE HANDS GO OUT. It is the one line
	// that says why several things are about to happen at once, and it is said
	// here rather than per hand because a person is being told about a decision,
	// not given a status board.
	a.mu.Lock()
	hub := a.hub
	a.mu.Unlock()
	if hub != nil {
		hub.send(Event{Kind: EventNotice, Text: forkNote(len(parsed.Parts))})
	}

	out := make([]handOut, 0, len(parsed.Parts))
	for index := range parsed.Parts {
		listed, handCtx, err := a.startHand(index, parsed.Parts[index])
		if err != nil {
			// A hand that could not even be registered is reported in its own
			// block right here, because it will never send one of its own.
			out = append(out, handOut{index: index, failed: err.Error()})
			continue
		}
		out = append(out, handOut{index: index, id: listed.id})
		go func(index int, listed *job, handCtx context.Context) {
			// A panic in one hand is not the caller's turn. The recovery is the
			// package's own (internal/guard), and because the delivery below is
			// registered AFTER it — so it runs BEFORE it, while the goroutine is
			// still panicking — a hand that died still sends a report saying so
			// rather than silently going missing and holding the landing open
			// for ever.
			defer guard.Recover("fork hand")
			result := forkResult{outcome: fmt.Sprintf(forkFailedFmt, "it did not come back")}
			defer func() { a.handIsHome(listed, index, parsed, result) }()
			result = a.runHand(handCtx, index, parsed, seed, system, listed)
		}(index, listed, handCtx)
	}

	return forkOut(parsed, out), false, nil
}

// handOut is one hand as the caller is told about it: which part it is, and the
// job id the footer, `jobs list` and `jobs kill` all address it by.
type handOut struct {
	index  int
	id     int
	failed string
}

// startHand registers one hand with the job registry and hands back the job and
// the context its turn runs under.
//
// AN AGENT WITH NO REGISTRY IS ANSWERED RATHER THAN CRASHED ON. Nothing in this
// package builds one without a registry ([newAgent], standing_run.go), but a
// hand-made Config is a real case and a verb that panicked on a field nobody
// sets would be a verb whose behaviour depended on it. The hand is a job here in
// every sense that matters — its context, its log, its row, its kill — so there
// is nothing to fall back to, and saying so is the honest answer.
func (a *Agent) startHand(index int, part forkPart) (*job, context.Context, error) {
	label := fmt.Sprintf("hand %d", index+1)
	if a.jobs == nil {
		return nil, nil, errors.New("this session keeps no background work, so a hand cannot be put out")
	}
	return a.jobs.startHand(label, strings.TrimSpace(part.Role))
}

// handIsHome is the delivery, and THE ORDER OF THESE FOUR IS THE WHOLE OF ITS
// CORRECTNESS.
//
//  1. The job SETTLES, which takes the hand off the running footer and ends its
//     row. Doing it first is what stops the caller reading "still running" beside
//     a report that is already in front of it.
//  2. The report goes on the STEERING LANE — the same lane a background job's
//     exit rides, so it lands in the caller's conversation at the next step
//     boundary and WAKES an idle session exactly as a job's exit does. A kill
//     this session ASKED for says nothing, for [jobRegistry.finish]'s reason:
//     the caller already knows, and it is the one case where no report is owed.
//  3. Only then is the hand counted home ([jobRegistry.handHome]), because that
//     count is what a node's landing waits on and lowering it before the report
//     was queued would let the node land on top of it.
//  4. And whoever is PARKED on it is released ([Agent.postTaskNews]), which is
//     the same release a divided part's report makes.
func (a *Agent) handIsHome(listed *job, index int, parsed forkArguments, result forkResult) {
	if listed == nil {
		return
	}
	requested := a.jobs.settled(listed, 0)
	if !requested {
		a.enqueueSteering(handReport(index, parsed, result))
	}
	a.jobs.handHome()
	a.postTaskNews()
}

// forkSeed is the copy each hand opens with: the caller's transcript up to this
// call, and the system message that stands in front of it.
//
// THE ASSISTANT MESSAGE CARRYING THIS VERY CALL LOSES ITS TOOL CALLS AND KEEPS
// ITS WORDS. It has to lose them: a request whose last assistant message asks
// for a tool nothing has answered is a shape providers refuse, and the answer
// does not exist yet — this function is running inside it. It keeps the words
// because those are the caller's own sentence about what it is about to do,
// which is the most recent thing a hand could be told and the one thing it
// would otherwise have to be told twice.
func (a *Agent) forkSeed() ([]ai.Message, string) {
	seed := a.snapshot()
	if len(seed) == 0 {
		return nil, ""
	}
	if last := len(seed) - 1; len(seed[last].ToolCalls) > 0 {
		stripped := seed[last]
		stripped.ToolCalls = nil
		if strings.TrimSpace(messageContentText(stripped)) == "" {
			seed = seed[:last]
		} else {
			seed[last] = stripped
		}
	}
	if len(seed) == 0 {
		return nil, ""
	}
	// The system message is handed over as text as well as riding at seed[0], so
	// the hand's own [Agent.refreshSystemLocked] rewrites message[0] to exactly
	// what is already there. Without it the hand's first request would carry a
	// freshly rendered prompt in front of a transcript built against the
	// caller's, and the shared prefix — the whole economic argument for this
	// verb — would break on its first byte.
	return seed, messageContentText(seed[0])
}

// ── the arguments, and the two refusals that happen before anybody starts ────

// parseForkArguments reads the call and answers a plain-English problem, or "".
//
// Every refusal here is a RESULT the model reads and acts on rather than a Go
// error, exactly as the auditor's refused command is (task_audit.go): the caller
// re-declares its parts and carries on, where an error would cost it the turn
// over a scope it could have redrawn in one sentence.
func parseForkArguments(workspace string, args json.RawMessage) (forkArguments, string) {
	var parsed forkArguments
	if err := json.Unmarshal(args, &parsed); err != nil {
		return parsed, "Invalid arguments: " + err.Error()
	}
	if len(parsed.Parts) < forkHandFloor {
		return parsed, fmt.Sprintf("not forked: %d hands is not a fork — a fork is %d to %d hands with separate work. "+
			"One hand is you, doing it yourself and paying for a copy of your context to do it.",
			len(parsed.Parts), forkHandFloor, forkFanLimit)
	}
	if len(parsed.Parts) > forkFanLimit {
		return parsed, fmt.Sprintf("not forked: %d hands is over the limit of %d. If the work really has that many "+
			"separate slices it is wide enough to be a task, which gets each part its own copy of the repository.",
			len(parsed.Parts), forkFanLimit)
	}
	for index, part := range parsed.Parts {
		if strings.TrimSpace(part.Role) == "" {
			return parsed, fmt.Sprintf("Invalid arguments: hand %d has no role, and a hand that is not told what "+
				"makes it different from its siblings will do what they are doing.", index+1)
		}
		// THE SCOPE IS NORMALIZED BEFORE IT IS JUDGED, and everything below reads
		// the normalized form: the overlap check, the charge the hand is handed,
		// and — through [Config.writeScope] — the guard that enforces it. A scope
		// that cannot be normalized never reaches a hand at all
		// ([normalizeWriteScope] carries the whole argument).
		clean, problem := normalizeWriteScope(workspace, part.Scope)
		if problem != "" {
			return parsed, fmt.Sprintf("not forked: hand %d's scope cannot be used — %s", index+1, problem)
		}
		if len(clean) == 0 {
			return parsed, fmt.Sprintf("Invalid arguments: hand %d declares no write scope, and a hand with no "+
				"scope may write nothing at all.", index+1)
		}
		parsed.Parts[index].Scope = clean
	}
	// THE OVERLAP CHECK IS THE OTHER HALF OF THE SAFETY ARGUMENT and it runs
	// here, AFTER normalization and before anything is spawned: two hands that
	// spelled one path two ways — "src/parser.rs" and "/workspace/src/parser.rs"
	// — are two hands claiming one path, and a collision test run over the raw
	// declarations would have called them disjoint. The guard can keep a hand
	// inside its own
	// scope, and no guard can keep two hands out of each other's when the scopes
	// were drawn on top of one another. The reading is [orchestrate.Covers],
	// which is the SAME reading the guard makes and the adaptive run's scheduler
	// makes — a path contains itself and everything under it — because a
	// collision test that read paths differently from the guard would refuse
	// pairs the guard would have allowed and allow pairs it would not.
	for left := range parsed.Parts {
		for right := left + 1; right < len(parsed.Parts); right++ {
			if path, ok := forkScopesCollide(parsed.Parts[left].Scope, parsed.Parts[right].Scope); ok {
				return parsed, fmt.Sprintf("not forked: hands %d and %d both claim %s, and two hands writing one "+
					"path in one working copy is the one thing this cannot make safe. Redraw the scopes so no path "+
					"is claimed twice — a directory claims everything under it — or give that part to one hand and "+
					"let the other work around it.", left+1, right+1, path)
			}
		}
	}
	return parsed, ""
}

// ── ONE READING OF A DECLARED WRITE SCOPE ───────────────────────────────────

// normalizeScopePath is THE ONE READING OF A DECLARED WRITE SCOPE, and both
// ends of the scope machinery go through it: the DOOR that accepts a model's
// declaration ([parseForkArguments]) and the GUARD that enforces it
// (orchestrate.go's [writeGuard] through [scopeAsGuarded]).
//
// IT EXISTS BECAUSE THE TWO ENDS ONCE READ ONE STRING DIFFERENTLY, and a whole
// division was voided at the seam. A task worker forked three hands two minutes
// into its life and declared their scopes as ABSOLUTE paths —
// "/workspace/rust-java-lsp/src/parser.rs" — which the door only trimmed and
// checked for overlap, because the schema SAID "workspace-relative" and nothing
// made it so. The guard then turned each attempted write into a
// workspace-relative path ("src/parser.rs") before asking [orchestrate.Covers],
// and a relative path is never covered by an absolute one: every write in every
// hand was refused, the model reported that the tool was refusing files that
// were plainly in its scope, and it never divided again. Nothing had gone wrong
// except that two readings of one string disagreed.
//
// So the scope is converted to the form the guard reads — workspace-relative,
// slash-separated, cleaned — at the door, and the guard converts again with this
// same function for the scopes that never came through a door (an adaptive run's
// node scopes are drawn by a planner, orchestrate.go). NORMALIZING TWICE IS
// FREE; NORMALIZING IN ONE PLACE ONLY WAS THE DEFECT.
//
// The error is a FRAGMENT rather than a sentence: it names what is wrong with
// this one path, and the caller writes the sentence its own reader can act on.
func normalizeScopePath(workspace, raw string) (string, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "", errors.New("it is empty")
	}
	local := filepath.FromSlash(clean)
	if filepath.IsAbs(local) {
		// AN ABSOLUTE PATH UNDER THE WORKSPACE IS ACCEPTED AND CONVERTED, never
		// refused. It names exactly the same file, it is the form a model reaches
		// for after a turn spent reading absolute paths, and a refusal would spend
		// a round teaching it a spelling. What is refused is an absolute path that
		// is NOT under the workspace, because that is a different claim entirely.
		root := strings.TrimSpace(workspace)
		if root == "" {
			return "", errors.New("it is an absolute path and there is no workspace to read it against")
		}
		relative, err := filepath.Rel(root, local)
		if err != nil {
			return "", errors.New("it is outside the workspace")
		}
		local = relative
	}
	slash := path.Clean(filepath.ToSlash(local))
	switch {
	case slash == ".." || strings.HasPrefix(slash, "../"):
		return "", errors.New("it is outside the workspace")
	case slash == "." || slash == "/":
		// THE WHOLE WORKSPACE IS NOT A SLICE OF IT. "." reaches every path and so
		// collides with every sibling, and [orchestrate.Covers] reads it as
		// covering NOTHING — so a scope of "." would be a declaration that
		// silently permitted no write at all, which is the exact shape of the
		// failure this function exists to end.
		return "", errors.New("it is the whole workspace rather than a slice of it")
	}
	return slash, nil
}

// normalizeWriteScope reads a WHOLE declared scope and answers either the paths
// in the guard's own form or the one sentence the model is given back.
//
// THE REFUSAL NAMES THE OFFENDING SCOPE AND THE EXPECTED FORM, because a model
// that is told only "no" re-sends the same declaration. It is a RESULT and not
// an error for the reason every refusal in this file is one: the caller redraws
// its scopes and carries on.
//
// A BLANK ENTRY IS DROPPED RATHER THAN REFUSED — a stray "" in a list of real
// paths is a formatting slip, not a claim — and a scope that is nothing BUT
// blanks comes back empty, which its caller answers in its own words.
func normalizeWriteScope(workspace string, scope []string) ([]string, string) {
	out := make([]string, 0, len(scope))
	for _, raw := range scope {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		clean, err := normalizeScopePath(workspace, raw)
		if err != nil {
			return nil, fmt.Sprintf("%q %s. Write scopes are paths INSIDE the working copy, relative to its "+
				"root — like src/parser.rs or docs — and the working copy is %s, so a path under it may also be "+
				"given in full. Redraw the scope and call again.",
				strings.TrimSpace(raw), err.Error(), workspaceShown(workspace))
		}
		out = append(out, clean)
	}
	return out, ""
}

// workspaceShown is the working copy as the refusal names it. An agent with no
// workspace configured is a real case in tests and in a door that never set one,
// and a sentence ending in "the working copy is ." would be worse than one that
// says there is not one.
func workspaceShown(workspace string) string {
	if workspace = strings.TrimSpace(workspace); workspace == "" {
		return "not set for this agent"
	}
	return workspace
}

// scopeAsGuarded is the scope as the ENFORCEMENT must read it: every declaration
// put through [normalizeScopePath], and the ones that cannot be normalized
// DROPPED rather than passed through.
//
// Dropping is the safe direction and it is the only one available here. A scope
// entry that names something outside the working copy cannot be satisfied by any
// path inside it, so keeping it would admit nothing; passing it through raw is
// what let an absolute declaration sit in a scope silently matching no write at
// all. The door refuses these with a sentence the model can act on — this is for
// the scopes that reach the guard without passing a door.
func scopeAsGuarded(workspace string, scope []string) []string {
	out := make([]string, 0, len(scope))
	for _, raw := range scope {
		if clean, err := normalizeScopePath(workspace, raw); err == nil {
			out = append(out, clean)
		}
	}
	return out
}

// forkScopesCollide reports the first path two scopes share, if any.
func forkScopesCollide(left, right []string) (string, bool) {
	for _, one := range left {
		if orchestrate.Covers(right, one) {
			return one, true
		}
	}
	for _, two := range right {
		if orchestrate.Covers(left, two) {
			return two, true
		}
	}
	return "", false
}

// ── one hand ────────────────────────────────────────────────────────────────

// forkOutcome is the word a hand's block ends on. They are the plain words for
// what happened and carry no machinery (CLAUDE.md's vocabulary law), because
// they are read by the model that has to decide what to do about them and they
// reach a person through whatever it then says.
const (
	forkDone      = "done"
	forkOutOf     = "out of rounds"
	forkStopped   = "stopped"
	forkFailedFmt = "failed: %s"
)

// forkResult is what one hand leaves behind.
type forkResult struct {
	// say is the hand's last word, whole, clipped at [forkSayLimit].
	say string
	// wrote is the files it actually changed, workspace-relative, in the order
	// it changed them.
	wrote []string
	// outcome is one of the words above.
	outcome string
}

// runHand builds one hand, gives it its charge, and stays until it is finished
// or out of budget.
//
// THE HAND RUNS ON ITS JOB'S OWN CONTEXT, one cancel deeper. It used to run on
// the TOOL CALL's, which was the nursery law and was free while the tool call
// lasted as long as the hands did; now that the call returns in the time it
// takes to spawn them, that context is dead one line later and a hand on it
// would be stopped before its first request. So the hand's life is its job's —
// the session's — and the three things that end it are its round budget, the
// person's interrupt ([jobRegistry.stopHands]) and Close.
//
// It also WRITES ITS PROGRESS INTO THE JOB'S LOG as it goes. That log is what
// makes a running hand legible from outside: `jobs output` reads it, the running
// footer quotes its last line at the foot of every result the caller reads
// (jobfooter.go), and a hand the person interrupts leaves it behind on disk as
// the only account of what it had been doing.
func (a *Agent) runHand(ctx context.Context, index int, parsed forkArguments, seed []ai.Message, system string, listed *job) forkResult {
	part := parsed.Parts[index]

	// The leash is minted before the context so the context's own cancel can be
	// handed to it: the budget is spent from inside the hand's turn, at the step
	// boundary, and what it does about it is end the turn.
	leash := &handLeash{limit: forkRounds}
	handCtx, stop := context.WithCancel(ctx)
	defer stop()
	leash.arm(stop)

	hand, err := a.newHandAgent(part, seed, system, leash)
	if err != nil {
		return forkResult{outcome: fmt.Sprintf(forkFailedFmt, err.Error())}
	}
	defer func() {
		// The money first and the close second, for the reason the node's fold
		// keeps that order: this is the last moment anybody can ask a child agent
		// what it spent (task_run.go's [Agent.foldTaskUsage]).
		a.foldHandUsage(hand)
		_ = hand.Close()
	}()

	events, err := hand.Submit(handCtx, forkCharge(index, parsed))
	if err != nil {
		return forkResult{outcome: fmt.Sprintf(forkFailedFmt, err.Error())}
	}

	var (
		wrote   []string
		seen    = map[string]bool{}
		failure error
		said    strings.Builder
	)
	for event := range events {
		switch event.Kind {
		case EventTextDelta:
			// THE HAND'S OWN WORDS, LINE BY LINE, INTO THE LOG. A hand narrates
			// what it is about to do before it does it, and that sentence — "now
			// let me fix the file range end column" — is the most useful thing
			// the footer can quote about a hand that has been out for twelve
			// minutes. Whole lines only: half a sentence in a footer is worse
			// than none.
			said.WriteString(event.Text)
			flushHandLines(listed, &said)
		case EventToolBegin:
			// And what it is DOING, in the same words the person's own row uses.
			writeHandLine(listed, event.Hint)
		case EventToolEnd:
			// The same reading the task runner takes of the same events
			// ([changedPath]): the path is in the CALL's arguments, because that
			// is the record of what was asked for.
			if path, saved := changedPath(event, a.config.Workspace); saved && !seen[path] {
				seen[path] = true
				wrote = append(wrote, path)
			}
		case EventError:
			failure = event.Err
		}
	}

	result := forkResult{
		say:   clip(strings.TrimSpace(lastSaid(hand)), forkSayLimit),
		wrote: wrote,
		// THE ORDER OF THESE THREE IS THE TRUTH. A spent leash cancels the hand,
		// so the cancellation it causes must be read as the budget rather than as
		// an interrupt; and the person's own interrupt kills every hand at once,
		// so it is read before a failure that is only the shape that cancel took.
		outcome: forkOutcome(ctx, leash, failure),
	}
	// AND THE REPORT ITSELF GOES INTO THE LOG, whether or not anybody is left to
	// read it on the lane. A hand the person interrupted, or one still out when
	// the session closed, sends no report — the log is then the only place its
	// account of itself exists, and it is a file on disk that outlives all of it.
	writeHandLine(listed, handReport(index, parsed, result))
	return result
}

// writeHandLine puts one line in a hand's log, and does nothing at all when
// there is no job behind the hand (see [Agent.startHand]).
func writeHandLine(listed *job, line string) {
	if listed == nil || listed.sink == nil {
		return
	}
	if line = strings.TrimRight(line, "\n"); strings.TrimSpace(line) == "" {
		return
	}
	_, _ = listed.sink.Write([]byte(line + "\n"))
}

// flushHandLines drains every COMPLETE line out of a hand's streaming reply into
// its log and leaves the unfinished tail in the builder.
func flushHandLines(listed *job, said *strings.Builder) {
	text := said.String()
	cut := strings.LastIndex(text, "\n")
	if cut < 0 {
		return
	}
	for _, line := range strings.Split(text[:cut], "\n") {
		writeHandLine(listed, line)
	}
	said.Reset()
	said.WriteString(text[cut+1:])
}

// forkOutcome reads the three ways a hand can stop being one.
func forkOutcome(ctx context.Context, leash *handLeash, failure error) string {
	switch {
	case leash.spent():
		return forkOutOf
	case ctx.Err() != nil:
		return forkStopped
	case failure != nil:
		return fmt.Sprintf(forkFailedFmt, failure.Error())
	}
	return forkDone
}

// newHandAgent mints one hand: the caller's own model, the caller's transcript,
// a belt that can only write where the hand said it would, and one system-
// authored message it has not seen yet.
//
// IT IS THE SEAM [Agent.newAuditAgent] OPENED and this is its second citizen:
// build the agent through [newAgent] like everything else, then REPLACE the belt
// and the transcript before anything runs. Threading either through Config would
// put "what a hand may touch" inside the function that decides what a
// conversation may touch, and every hand added to the session later would join
// this belt unless somebody remembered a rule written somewhere else.
//
// The seed and the system text are the CALLER'S, read once at the call and
// handed to every hand, which then takes its own index over them (below).
func (a *Agent) newHandAgent(part forkPart, seed []ai.Message, system string, leash *handLeash) (*Agent, error) {
	a.mu.Lock()
	parent := a.config
	model := a.model
	// AND THE CALLER'S CACHE LINEAGE, WHICH IS THE VERB'S WHOLE ECONOMY. The key
	// is a routing hint — "which replica should serve this?" — and a prefix cache
	// can only hit when one replica sees the same bytes twice (internal/provider's
	// hints.go).
	//
	// A fan-out run's leaves are deliberately split apart from each other's keys
	// there, and the argument that splits them is exactly the argument that keeps
	// hands together. Six leaves are six DIFFERENT transcripts growing on one
	// replica, so all they share is a few thousand bytes of head and all they do
	// is evict each other. Hands are the opposite shape: they are the SAME
	// transcript, byte for byte, from message zero to the last thing the caller
	// said, and what differs is one closing message and a short errand's worth of
	// tail. Splitting them would write that whole shared prefix cold once per
	// hand, which is the one cost this verb exists not to pay.
	key := a.cacheKey
	// The provider client itself, past this session's own request wrapper, for
	// the reason a node takes it that way (task_run.go): routing, the fallback
	// chain and the nearest-model rescue are facts about the CONNECTION, and
	// there is one connection.
	client := unwrapCompleter(a.client)
	a.mu.Unlock()

	// THE HANDS ARE THE CALLER'S OWN, so they run on the caller's model. They are
	// not interns being given the cheap tier: each one is this mind continuing to
	// work, and a hand answering worse than the caller would have is a fork that
	// cost the answer. A part graded careful lifts through the roles ladder that
	// already answers this question for a divided task ([Agent.carefulModel]),
	// which on an install that has configured no tiers is the same model again
	// and costs a word on the wire.
	if part.careful() {
		model = a.carefulModel(model)
	}

	hand, err := newAgent(Config{
		// THE SAME DIRECTORY, WHICH IS THE POINT. A worktree per hand is what
		// makes a task a task; what stands in for it here is the scope below.
		//
		// WHICH IS EXACTLY WHY THE DROPPINGS MAY NOT FOLLOW IT. A hand shares the
		// caller's workspace and has no folder of its own, so a stubbed tool
		// result of its own filed itself into that workspace — the person's
		// repository, for every fork the conversation itself runs (landing.go).
		// The caller's answer is the family's answer at any depth: a hand of a
		// worker of a node inherits what that worker was handed.
		droppings:     parent.droppingsPlace(),
		Workspace:     parent.Workspace,
		Model:         model,
		APIKey:        parent.APIKey,
		BaseURL:       parent.BaseURL,
		ContextWindow: parent.ContextWindow,
		// A hand opens on a transcript the caller has already been compacting,
		// and it appends a short errand to it, so it compacts on the caller's own
		// terms rather than on a rule of its own.
		CompactEnabled: parent.CompactEnabled,
		// Handed over as text as well as ridden in at seed[0]: see [Agent.forkSeed].
		System: system,
		// The floor is still the floor (approval's critical table), but the BELT
		// is what constrains a hand — there is nothing on it that writes outside
		// the scope. AskConsent is off and InTask is on for a node's own reason:
		// there is nobody inside a tool call to ask, and a prompt here would be a
		// turn that hangs.
		ApprovalPolicy: &approval.Policy{Default: approval.ActionAllow},
		AskConsent:     false,
		InTask:         true,
		// THE SCOPE, AND THE VERB THAT IS ABSENT BECAUSE OF IT. The first arms
		// [writeGuard] — already a citizen of every agent's control plane, so
		// this is a slice and not a mechanism (orchestrate.go). The second is what
		// keeps the fork one deep.
		writeScope: part.Scope,
		inHand:     true,
		// AND WHOSE WORK THIS HAND IS DOING. A hand shares its caller's working
		// directory, so the question treehold.go asks about every write — is
		// somebody else working in this tree — has to get the same answer for a
		// hand as for the worker that forked it. Without these two a hand of a
		// node's worker is indistinguishable from the conversation, and it would
		// be refused writes into the very tree its own node is holding.
		//
		// They are the caller's, unchanged, at any depth: a hand of a hand of a
		// worker is still that node writing. Nothing else on a hand's belt reads
		// them — the belt is a fixed allowlist with no task verb on it
		// ([forkBelt]) — so this carries identity and no new power.
		tasker: parent.tasker,
		taskID: parent.taskID,
		// And the budget, as a citizen of the same plane (hooks.go).
		handLeash:      leash,
		SupportsImages: parent.SupportsImages,
		// Without this the rescue for a model that cannot hold a tool works
		// exactly zero levels deep here, and a careful hand lifted onto a tier
		// whose model has no tool grammar would spend its whole errand finding
		// out (task_run.go states the same argument for a node).
		SupportsParameter: parent.SupportsParameter,
		ReasoningProfile:  parent.ReasoningProfile,
		RolesSource:       parent.RolesSource,
		// A hand reads documents on the rung the person chose, like the
		// conversation does: the same mind, reading the same file, must not drop
		// to a different engine because it is inside a fork.
		DocumentEngine: parent.DocumentEngine,
	}, client)
	if err != nil {
		return nil, err
	}

	belt := forkBelt(hand.beltTools(), parent.Workspace)
	definitions, err := toolDefinitions(belt)
	if err != nil {
		_ = hand.Close()
		return nil, err
	}
	hand.armMu.Lock()
	hand.tools, hand.definitions = belt, definitions
	hand.armMu.Unlock()
	// EVERY HAND GETS ITS OWN INDEX OVER THE SAME CONTENT, and this copy is the
	// "copy" in copy-on-write. What is shared is what the messages POINT AT —
	// their text, their images — which nothing rewrites and which is where all
	// the weight is; what is not shared is the slice, because two things do write
	// into it. Each hand appends its own charge and its own work at the tail, and
	// [Agent.refreshSystemLocked] rewrites message[0] at the start of every turn:
	// hands sharing one backing array would have had two agents writing one
	// element, which is a data race in the one place nothing would ever look.
	messages := make([]ai.Message, len(seed))
	copy(messages, seed)
	hand.mu.Lock()
	hand.messages = messages
	// The lineage is stamped on the agent AND on the wrapper every one of its
	// requests passes through, because that wrapper is where the key reaches the
	// wire (agent.go's sessionCompleter) and it was built with the fresh one this
	// constructor minted.
	hand.cacheKey = key
	if wrapper, ok := hand.client.(sessionCompleter); ok {
		wrapper.cacheKey = key
		hand.client = wrapper
	}
	hand.mu.Unlock()
	return hand, nil
}

// forkBelt is a hand's hands: the readers, the two writers the scope guard
// binds, and a bash that orients and changes nothing.
//
// IT IS AN ALLOWLIST AND NOT A SUBTRACTION, which is [auditBelt]'s law and is
// worth more here than it is there: the `switch` has no default, so a verb added
// to the session's belt next month reaches a hand only when somebody adds its
// name to this list and says why. A subtraction would have quietly handed hands
// `generate_video`, whose write happens minutes later on a goroutine that no
// pre-action citizen has ever seen.
//
// The tools come from the hand's OWN belt rather than from bare, so `read` keeps
// the senses this session gave it — a hand that inherited a transcript full of
// screenshots and could not open the next one would be a strange copy of the
// mind that made it. bash is the exception and is rebuilt from bare: the
// session's bash can start a BACKGROUND job, and a job outliving the turn that
// started it is the one thing the nursery law forbids.
func forkBelt(belt []bare.Tool, dir string) []bare.Tool {
	var out []bare.Tool
	for _, tool := range belt {
		switch tool.Name {
		case "read", "grep", "find", "ls", "read_document", "manual", "edit", "write":
			out = append(out, tool)
		}
	}
	for _, tool := range bare.AllTools(dir) {
		if tool.Name == "bash" {
			tool.Description = "Look at what the repository already says about itself: " +
				strings.Join(forkCommands, ", ") + ". Every other command is refused, including anything that " +
				"edits, installs, fetches, or chains a second command onto one of these. THERE IS NO BUILD AND NO " +
				"TEST HERE — the other hands are writing this same working copy right now, so neither would mean " +
				"anything; both belong to whoever forked you. " + forkShell.hint + " " + tool.Description
			out = append(out, readingOnlyBash(tool, plainDoor(forkCommands), forkShell))
		}
	}
	return out
}

// foldHandUsage charges a hand's spend to whoever opened it, tagged so a bench
// can tell a fork's cost from the turn's own.
//
// It is [Agent.foldTaskUsage]'s shape with one difference, and the difference is
// the tag: the usage line already carries a role field for exactly this — "a
// line is journaled with the role that made the call so a bad answer can be
// traced to the model that gave it" (loop.go) — and until this there was no way
// to read a turn's journal and say which of its tokens the hands spent.
func (a *Agent) foldHandUsage(hand *Agent) {
	used := hand.Usage()
	if used.Input == 0 && used.Output == 0 && used.CostUSD == 0 {
		return
	}
	cost := used.CostUSD
	// The HAND's model and the hand's OWN call count: a hand that ran twelve
	// rounds is twelve requests, and folding it in as one call on the
	// conversation's model would put a number in the books that never happened.
	a.addAuxiliaryUsageAs(&ai.Response{Usage: &ai.Usage{
		PromptTokens:             used.Input,
		CompletionTokens:         used.Output,
		CacheReadInputTokens:     used.CacheRead,
		CacheCreationInputTokens: used.CacheWrite,
		Cost:                     &cost,
	}}, hand.Model(), used.Calls, auxRoleHand)
}

// ── the round budget ────────────────────────────────────────────────────────

// handLeash is a hand's round budget as a `post-feedback` citizen (hooks.go).
//
// THE SEAM IS THE POINT. post-feedback runs EXACTLY ONCE PER FINISHED TOOL
// ROUND — after a batch's results are in the transcript and before the next
// request is assembled — which is the same boundary [checkpointMeter.round]
// counts a turn's price at. So "fifteen rounds" means here what it means there,
// and it means it because both are counted at one seam rather than because two
// files agree about a word.
//
// What it does when the budget is spent is CANCEL THE HAND'S TURN, not refuse
// its next call. The plane's law is that only pre-action may stop something, and
// a citizen that refused every call from here would leave the model spending
// requests to be told no; a cancelled turn ends where it stands, with everything
// the hand has already written still on disk and everything it has already said
// still readable ([lastSaid]).
type handLeash struct {
	limit int

	mu     sync.Mutex
	rounds int
	stop   context.CancelFunc
}

// Keep the leash tied to the lifecycle seam at compile time. The hook registry
// deliberately accepts citizens through several interfaces, so a signature
// change would otherwise turn this budget into a silent no-op at runtime.
var _ postFeedbackHook = (*handLeash)(nil)

func (*handLeash) Name() string { return "hand-rounds" }

// arm hands the leash the cancel that ends the hand's turn.
func (l *handLeash) arm(stop context.CancelFunc) {
	l.mu.Lock()
	l.stop = stop
	l.mu.Unlock()
}

func (l *handLeash) PostFeedback(context.Context, *episode, *eventHub, []ai.ToolCall, []toolResult, bool) {
	l.mu.Lock()
	l.rounds++
	spent, stop := l.rounds >= l.limit, l.stop
	l.mu.Unlock()
	if spent && stop != nil {
		stop()
	}
}

// spent reports whether this hand ran out of rounds rather than out of work. It
// is read after the turn has drained, which is why it is a state and not the
// return of the call that noticed.
func (l *handLeash) spent() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rounds >= l.limit
}

// ── what a hand is told, and what comes back ────────────────────────────────

// forkCharge is the ONE message a hand is handed that its caller never saw, and
// everything in it is something the transcript above it cannot say.
//
// It says WHICH hand this is, what its part is, what it may write — and what its
// SIBLINGS own, in one sentence, because that is the half the transcript cannot
// carry: every hand read the same words and would draw the same next step out of
// them. Naming the neighbours is lateral inhibition, and it is the measured half
// of this design rather than the pretty half.
func forkCharge(index int, parsed forkArguments) string {
	part := parsed.Parts[index]
	var out strings.Builder
	fmt.Fprintf(&out, "You are hand %d of %d. Everything above is yours — you have already read it, so nothing is "+
		"repeated here.\n\n", index+1, len(parsed.Parts))
	fmt.Fprintf(&out, "YOUR PART: %s\n", strings.TrimSpace(part.Role))
	fmt.Fprintf(&out, "YOU MAY WRITE: %s — and nowhere else. Anything you try to edit or write outside that is "+
		"refused.\n", strings.Join(part.Scope, ", "))

	if len(parsed.Parts) > 1 {
		out.WriteString("\nTHE OTHER HANDS ARE WORKING RIGHT NOW, in this same working copy:\n")
		for other, sibling := range parsed.Parts {
			if other == index {
				continue
			}
			fmt.Fprintf(&out, "  hand %d — %s (%s)\n", other+1, strings.TrimSpace(sibling.Role),
				strings.Join(sibling.Scope, ", "))
		}
		out.WriteString("DO NOT REDO THEIR WORK and do not wait for it: what they own is theirs, and a file of " +
			"theirs that looks half-written is half-written because they are in it.\n")
	}

	if note := strings.TrimSpace(parsed.Note); note != "" {
		fmt.Fprintf(&out, "\nFOR EVERY HAND: %s\n", note)
	}

	fmt.Fprintf(&out, "\nYou have %d tool rounds, and NOBODY IS WAITING FOR THE OTHERS BEFORE READING YOU: your "+
		"report goes to whoever forked you the moment you finish, on its own, while your siblings are still "+
		"working. So finish YOUR part and stop — do not stretch the errand to fill the budget, and do not wait "+
		"on anything.\n\nDo not build and do not run tests: the tree is being written by all of you at once, so "+
		"neither would mean anything, and the one who forked you does both as your report lands. When you are "+
		"done, say what you did, what you found, and anything the others' work depends on. That last message is "+
		"the only thing that reaches whoever is stitching this together.\n\nIF YOU RUN OUT OF ROUNDS your report "+
		"says so and quotes your last words back, so whoever reads it knows a change may be half made. That "+
		"makes it worth SAYING WHAT YOU ARE ABOUT TO DO before you do it, in one line, every time.", forkRounds)
	return out.String()
}

// forkOutLead opens the call's own answer and opens nothing else. It is the
// marker a test and a reader both recognise the roster by.
const forkOutLead = "hands are out"

// forkOut is what the CALL answers with: a roster, not a result.
//
// It says three things and each one is a thing the caller would otherwise get
// wrong. WHO IS OUT, with the job id every other verb addresses them by, so a
// caller that wants to end one can. THAT THE REPORTS COME ON THEIR OWN, in the
// same sentence the jobs tool uses for the same fact, because a model that is
// not told this spends its next call asking. And THAT IT SHOULD KEEP WORKING,
// because the whole forty minutes this change is about were spent by a mind that
// believed it had nothing to do until its hands were back.
func forkOut(parsed forkArguments, out []handOut) string {
	started := 0
	for _, one := range out {
		if one.failed == "" {
			started++
		}
	}
	if started == 0 {
		// NOTHING IS OUT, so nothing may say it is. The roster's opening line is
		// what the caller reasons from for the rest of the turn, and one that
		// claimed hands nobody has would leave it waiting for reports that are
		// never coming.
		var refusal strings.Builder
		refusal.WriteString("not forked: no hand could be started.\n")
		for _, one := range out {
			fmt.Fprintf(&refusal, "\n  hand %d — %s: %s\n",
				one.index+1, strings.TrimSpace(parsed.Parts[one.index].Role), one.failed)
		}
		refusal.WriteString("\nThe work is still yours and nothing has been touched. Do it in your own hands.")
		return refusal.String()
	}

	var report strings.Builder
	fmt.Fprintf(&report, "%s %s.\n", forkCountWord(started), forkOutLead)
	for _, one := range out {
		part := parsed.Parts[one.index]
		if one.failed != "" {
			fmt.Fprintf(&report, "\n  hand %d — %s · DID NOT START: %s\n",
				one.index+1, strings.TrimSpace(part.Role), one.failed)
			continue
		}
		fmt.Fprintf(&report, "\n  hand %d — %s · job %d · writes %s\n",
			one.index+1, strings.TrimSpace(part.Role), one.id, strings.Join(part.Scope, ", "))
	}
	report.WriteString("\nEach hand's report arrives here ON ITS OWN the moment it finishes, in the order they " +
		"come home — no call from you, so never sleep, poll or ask `jobs output` to wait for one. Every hand " +
		"still out rides at the foot of every result you read, with how long it has been out and what it last " +
		"did.\n\nKEEP WORKING. A slice is ready when ITS report lands, not when they are all back: build it, " +
		"test it, measure it then. Nothing has been built, run or reviewed — that is yours, hand by hand.")
	return report.String()
}

// handReportLead opens EVERY hand's report, whatever became of the hand. It is
// one marker so the caller can recognise one of these at a glance in a
// conversation that also carries jobs, watches and landings.
const handReportLead = "hand "

// handReport is ONE hand's report, as it arrives in the caller's conversation.
//
// IT LEADS WITH THE OUTCOME WHEN THE OUTCOME IS BAD, and that is not formatting.
// A hand that ran out of rounds stopped MID-ERRAND: it was told to do a thing,
// it was three quarters through doing it, and its budget ended between one edit
// and the next. A report that opened with its name and its writes and mentioned
// the budget four lines down is a report a caller reads as done — which is
// exactly what a caller did, and it built on a half-made change.
//
// So an unfinished hand's report opens on the words "out of rounds", names the
// files it wrote, and QUOTES ITS LAST STATED INTENT VERBATIM, because that
// sentence is the only description in existence of the change that may be half
// made. And it borrows the vocabulary a landing already has for work nothing
// looked at ([unverifiedEdits]) rather than inventing a state.
func handReport(index int, parsed forkArguments, result forkResult) string {
	part := parsed.Parts[index]
	role := strings.TrimSpace(part.Role)
	outcome := result.outcome
	if outcome == "" {
		// A report with no outcome is a hand whose goroutine died before it
		// could write one. Saying so is the honest answer; leaving it blank
		// would read as done.
		outcome = fmt.Sprintf(forkFailedFmt, "it did not come back")
	}

	var report strings.Builder
	if outcome == forkOutOf {
		fmt.Fprintf(&report, "%s · %s%d of %d — %s\n", strings.ToUpper(forkOutOf), handReportLead,
			index+1, len(parsed.Parts), role)
	} else {
		fmt.Fprintf(&report, "%s%d of %d — %s · %s\n", handReportLead, index+1, len(parsed.Parts), role, outcome)
	}
	if len(result.wrote) > 0 {
		fmt.Fprintf(&report, "  wrote %s\n", strings.Join(result.wrote, ", "))
	} else {
		report.WriteString("  wrote nothing\n")
	}
	if say := strings.TrimSpace(result.say); say != "" {
		report.WriteString(indentLines(say, "  "))
		report.WriteString("\n")
	}

	switch outcome {
	case forkOutOf:
		report.WriteString("\n  It stopped mid-errand: its budget ended between one edit and the next. " +
			"What it last said it was about to do, in its own words:\n")
		if intent := strings.TrimSpace(lastLine(result.say)); intent != "" {
			fmt.Fprintf(&report, "    \u201c%s\u201d\n", intent)
		} else {
			report.WriteString("    (it said nothing before its budget ended — its log is the only account)\n")
		}
		if len(result.wrote) > 0 {
			report.WriteString("  Those files are UNVERIFIED: nothing built or ran them, and the change it was " +
				"in the middle of may be half made. Read them before you build on them.\n")
		}
		report.WriteString("  This part is NOT done. Finish it yourself or fork again for it — do not assume it landed.\n")
	case forkStopped:
		report.WriteString("\n  It was stopped before it finished. Anything it had already written is still in " +
			"your working copy and is UNVERIFIED — nothing built or ran it, and it may be half made.\n")
	case forkDone:
		report.WriteString("\n  Its slice is in your working copy and NOTHING HAS BEEN BUILT, RUN OR REVIEWED. " +
			"That is yours, and you can do it now — the other hands are working elsewhere in the tree.\n")
	}
	return strings.TrimRight(report.String(), "\n")
}

// lastLine is the last non-empty line of a block — a hand's last stated intent,
// which is the sentence before the edit its budget cut short.
func lastLine(text string) string {
	lines := strings.Split(text, "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if line := strings.TrimSpace(lines[index]); line != "" {
			return line
		}
	}
	return ""
}

// indentLines puts a prefix on every line of a block so a hand's own words
// cannot be mistaken for the report's.
func indentLines(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		lines[index] = prefix + line
	}
	return strings.Join(lines, "\n")
}

// ── the line the person reads ───────────────────────────────────────────────

// forkNote is the one dim line a person gets while the hands are out: an
// observation, a middle dot, a promise, which is the register every one-liner
// this harness writes over somebody's turn is held to (checkpoint.go's ceiling
// and split lines are its siblings).
//
// It says HANDS rather than anything about forks, copies or scopes, because the
// person is not being asked to understand the mechanism — they are being told
// why several things are about to happen at once.
//
// AND THE PROMISE CHANGED WITH THE MECHANISM. It used to say "back when they are
// done", which was true of a join and is a lie about a stream: the answer does
// not go quiet and wait for all of them any more, it carries on and folds each
// hand in as that hand lands. A dim line that promises the old behaviour would
// have a person reading a working answer as a stuck one.
func forkNote(hands int) string {
	return forkCountWord(hands) + " hands on it · each one folds in as it lands"
}

// forkCountWord is the count as somebody would say it out loud. It is a list
// rather than arithmetic because it only ever has to cover [forkHandFloor] to
// [forkFanLimit], and a general number-speller for three cases would be three
// cases of code nobody reads.
func forkCountWord(hands int) string {
	switch hands {
	case 2:
		return "two"
	case 3:
		return "three"
	case 4:
		return "four"
	}
	return strconv.Itoa(hands)
}
