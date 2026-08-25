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
// it does not have. No hand outlives the caller's turn: they run on the turn's
// own context, so an interrupt kills the nursery. And nothing is built or
// tested inside a hand — three hands editing one tree means every build reads a
// half-written repository, so a green one proves nothing and a red one is a
// sibling's unfinished work that this hand would then "fix". The caller builds,
// tests and stitches after the join, which is the same law divide_work keeps:
// the parent stays the integrator.

import (
	"context"
	"encoding/json"
	"fmt"
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
// It is the auditor's allowlist ([auditCommands]) with the three `go` verbs
// taken off, and the reason is the one stated at the head of this file. The
// hands share one working directory, so a build or a test inside a hand reads a
// tree its siblings are still writing: the green ones prove nothing, and the red
// ones are somebody else's half-finished work that this hand would then stop and
// repair — the exact redo the sibling-scope sentence exists to prevent. Builds
// and tests are the caller's, after the join.
//
// Everything left reads: what the repository already says about itself, and the
// four orientation commands the auditor's own allowlist gained after a refused
// `pwd` cost a verdict.
var forkCommands = []string{
	"git diff",
	"git log",
	"git status",
	"git show",
	"pwd",
	"wc",
	"head",
	"cat",
}

// forkShell is the voice a hand's refused command is answered in, and the last
// line of every one of those refusals says where to go instead — the auditor's
// hint made this file's ([auditReaderHint] states the argument for having one).
var forkShell = shellLeash{
	who:     "a hand",
	forWhat: "orientation",
	hint: "For looking around, use read, grep, find and ls — that is what they are for. " +
		"Builds and tests are the caller's after you are all back, never yours.",
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
	"cannot build, test or fork again, and gets " + strconv.Itoa(forkRounds) + " tool rounds. Returns when every " +
	"hand is back with what it did; you then build, test and make one thing of it."

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
// made safe, copy the transcript, run the hands together, and hand back one
// block each.
//
// IT IS SYNCHRONOUS, and that is the join. The caller's next thought is written
// with every hand's report already in front of it, which is what makes the
// caller the integrator — the same law divide_work keeps for the same reason.
func (a *Agent) forkHands(ctx context.Context, args json.RawMessage) (string, bool, error) {
	parsed, problem := parseForkArguments(args)
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

	// THE PERSON HEARS ONCE, BEFORE THE WAIT AND NOT AFTER IT. The hands are
	// inside this turn and draw no rows of their own; what a person is owed is
	// the one line that explains why the answer has gone quiet.
	a.mu.Lock()
	hub := a.hub
	a.mu.Unlock()
	if hub != nil {
		hub.send(Event{Kind: EventNotice, Text: forkNote(len(parsed.Parts))})
	}

	results := make([]forkResult, len(parsed.Parts))
	var wait sync.WaitGroup
	for index := range parsed.Parts {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			// A panic in one hand is not the caller's turn. The recovery is the
			// package's own (internal/guard), and the block this hand leaves
			// behind says it failed rather than silently going missing.
			defer guard.Recover("fork hand")
			results[index] = a.runHand(ctx, index, parsed, seed, system)
		}(index)
	}
	wait.Wait()

	return forkReport(parsed.Parts, results), false, nil
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
func parseForkArguments(args json.RawMessage) (forkArguments, string) {
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
		clean := make([]string, 0, len(part.Scope))
		for _, path := range part.Scope {
			if path = strings.TrimSpace(path); path != "" {
				clean = append(clean, path)
			}
		}
		if len(clean) == 0 {
			return parsed, fmt.Sprintf("Invalid arguments: hand %d declares no write scope, and a hand with no "+
				"scope may write nothing at all.", index+1)
		}
		parsed.Parts[index].Scope = clean
	}
	// THE OVERLAP CHECK IS THE OTHER HALF OF THE SAFETY ARGUMENT and it runs
	// here, before anything is spawned: the guard can keep a hand inside its own
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
// THE HAND RUNS ON THE CALLER'S OWN CONTEXT, one cancel deeper. That is the
// nursery law and it costs nothing to keep: an interrupt on the person's turn
// reaches the tool's context (loop.go's toolCtx), which reaches here, which
// reaches the hand's turn — so no hand outlives the turn that opened it, and
// nothing has to remember to kill anything.
func (a *Agent) runHand(ctx context.Context, index int, parsed forkArguments, seed []ai.Message, system string) forkResult {
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
	)
	for event := range events {
		switch event.Kind {
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

	return forkResult{
		say:   clip(strings.TrimSpace(lastSaid(hand)), forkSayLimit),
		wrote: wrote,
		// THE ORDER OF THESE THREE IS THE TRUTH. A spent leash cancels the hand,
		// so the cancellation it causes must be read as the budget rather than as
		// an interrupt; and the person's own interrupt kills every hand at once,
		// so it is read before a failure that is only the shape that cancel took.
		outcome: forkOutcome(ctx, leash, failure),
	}
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
		// And the budget, as a citizen of the same plane (hooks.go).
		handLeash:      leash,
		SupportsImages: parent.SupportsImages,
		// Without this the rescue for a model that cannot hold a tool works
		// exactly zero levels deep here, and a careful hand lifted onto a tier
		// whose model has no tool grammar would spend its whole errand finding
		// out (task_run.go states the same argument for a node).
		SupportsParameter: parent.SupportsParameter,
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
				"anything; both happen after you are all back. " + forkShell.hint + " " + tool.Description
			out = append(out, readingOnlyBash(tool, forkCommands, forkShell))
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

func (*handLeash) Name() string { return "hand-rounds" }

// arm hands the leash the cancel that ends the hand's turn.
func (l *handLeash) arm(stop context.CancelFunc) {
	l.mu.Lock()
	l.stop = stop
	l.mu.Unlock()
}

func (l *handLeash) PostFeedback(context.Context, *episode, *eventHub, []ai.ToolCall, []toolResult) {
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

	fmt.Fprintf(&out, "\nYou have %d tool rounds. Do not build and do not run tests — the tree is being written by "+
		"all of you at once, so neither would mean anything; that happens after you are all back. When you are "+
		"done, say what you did, what you found, and anything the others' work depends on. That last message is "+
		"the only thing that reaches whoever is stitching this together.", forkRounds)
	return out.String()
}

// forkReport is the join, as the caller reads it: one block per hand, in the
// order the parts were declared rather than the order they finished, because
// what came back is a narrative and the caller wrote the numbering.
func forkReport(parts []forkPart, results []forkResult) string {
	var out strings.Builder
	fmt.Fprintf(&out, "%s hands, all back:\n", forkCountWord(len(parts)))
	for index, part := range parts {
		result := results[index]
		fmt.Fprintf(&out, "\nhand %d — %s\n", index+1, strings.TrimSpace(part.Role))
		if len(result.wrote) > 0 {
			fmt.Fprintf(&out, "  wrote %s\n", strings.Join(result.wrote, ", "))
		}
		outcome := result.outcome
		if outcome == "" {
			// A block with no outcome is a hand whose goroutine died before it
			// could write one. Saying so is the honest answer; leaving it blank
			// would read as done.
			outcome = fmt.Sprintf(forkFailedFmt, "it did not come back")
		}
		fmt.Fprintf(&out, "  %s\n", outcome)
		if say := strings.TrimSpace(result.say); say != "" {
			out.WriteString(indentLines(say, "  "))
			out.WriteString("\n")
		}
	}
	out.WriteString("\nTheir work is in your working copy now. NOTHING HAS BEEN BUILT OR TESTED and nothing has " +
		"been reviewed: that is yours, and so is making one thing out of what came back. A hand that came back " +
		"out of rounds left its part unfinished — finish it or fork again for it, and do not assume it landed.")
	return out.String()
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
// why the answer has gone quiet and when it comes back.
func forkNote(hands int) string {
	return forkCountWord(hands) + " hands on it · back when they are done"
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
