package session

// The task tool: how one piece of work leaves the conversation.
//
// A session is a place to think with somebody, and some work does not want to
// be thought about out loud. A long build-and-fix loop, a mechanical sweep over
// forty files, a rewrite whose only interesting moment is the end — run in the
// conversation, each of those buries the discussion under its own output and
// spends the context window on text nobody will read twice. propose_task is the
// model's way of saying "this part is self-contained; let it go and work
// somewhere else", and the machinery under it (task_run.go) is a GRAPH: the
// proposal is a node, the countdown is how the node is admitted, the worktree is
// how it is isolated, and its report is what its dependents will read.
//
// depends_on exists on the wire and is honoured by the executor, so a
// conversation that grooms two pieces of work can say which waits for which.
//
// ── AND A TASK MAY HAND PART OF ITS OWN WORK OUT ──
//
// This tool is on a NODE'S belt too, and the node's proposals join the
// conversation's own graph under the node that made them (session.go's
// Config.tasker). That is the fan-out law: a step with two or three genuinely
// INDEPENDENT parts is faster as three nodes in three worktrees than as one
// model doing them in order, and the coordination — reading the reports,
// folding them into one deliverable — stays with the node that split the work.
// Work that is sequential, or that shares heavy context, is not split at all:
// the parts would each pay for a worktree, an audit and a wait to save nothing.
//
// TWO BOUNDS, AND THEY ARE DIFFERENT KINDS OF THING. The DEPTH cap is absence:
// a node standing on taskDepthLimit is handed no propose_task at all, because a
// capability that cannot work is left off the belt rather than made to refuse
// (tools.go). The FAN cap is a REFUSAL the model reads and acts on
// ([TaskGraph.claimChild]) — it has already been given its slots, and the answer
// to a fourth part is to do it in its own hands.
//
// ── THE PROPOSAL IS THE CONSENT, AND IT HAS A CLOCK ──
//
// The call BLOCKS, exactly as a consent question does (consent.go), and it is
// resolved by one of four things: the person approves, the person redirects
// (approval plus a correction appended to the brief), the person declines (the
// model reads the refusal as grooming feedback and keeps working), or THE CLOCK
// APPROVES. Silence is a yes, because the countdown is a window to redirect
// work the model has already groomed rather than a gate the work waits behind:
// a person who is reading, or away, or on another screen must not be the reason
// nothing happened. A surface that draws no answer box for this event is a
// surface where every task starts after five seconds, which is the right
// behavior for a surface that has not been taught the question yet.
//
// ── WHY A HEADLESS RUN NEVER WAITS ──
//
// With nobody subscribed to the events there is no one to answer, so the
// deadline approves whatever the setting says — including the 0 that means
// "wait for an answer" in a watched session. The alternative is a --once run
// that hangs forever on a question with no reader, which is the same fault
// consent.go refuses for the same reason.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// taskDescription is what the model reads before it calls. THE FAN CAP IS
// INTERPOLATED for taskSchemaJSON's reason: a number a model reasons with must
// be the number the code enforces, and the two drift the moment they are typed
// twice.
var taskDescription = "Hand ONE self-contained piece of work to a task that runs on its own, outside this conversation, in its own copy of the repository. Use it when the work would flood the conversation — a long build-and-fix loop, a mechanical sweep across many files, a rewrite whose only interesting moment is the result — or when it simply wants a clean context of its own. Do NOT use it for a quick read, a question you can answer here, or anything that needs the back-and-forth of this conversation: a task cannot ask you anything once it starts. THE BRIEF IS THE TASK'S WHOLE WORLD. It never sees this conversation, so write it as if for a colleague joining today: the goal, the files and symbols involved, the conventions and constraints you have learned here, what has already been tried, and how to check the work. The person is shown the title and summary with a short countdown to redirect or wave it off; silence starts it. You get the id back immediately and the task's report arrives here when it lands, so keep working — never wait for it. A TASK MAY CALL THIS TOO, for parts of its own work that are genuinely independent of each other: up to " + strconv.Itoa(taskFanLimit) + " of them, one level deep, each registered under the task that asked for it. Split a step only when its parts do not need each other — sequential parts, and parts that share heavy context, are faster done in your own hands."

// taskSchemaJSON is the wire schema. depends_on is on it from the first day
// even though a one-node graph can never fill it: the field is the edge, the
// executor already honours it (task_run.go), and a schema that grew the concept
// later would be a second shape for the same idea.
//
// THE TWO THRESHOLD DEFAULTS ARE INTERPOLATED, NEVER TYPED TWICE. They were
// typed twice once, and they drifted: this schema told the model the step
// default was 40 while the executor applied 200 (task_run.go's taskMaxSteps).
// A number in a tool description is not documentation — it is what the model
// reasons with, so a model that wanted room for a long sweep was raising a
// figure that was already five times higher than it believed, and one that
// wanted to keep a small job tight was setting 40 thinking it changed nothing.
// It is a var rather than a const for exactly this reason; the cost is one
// package-level string built at init, and what it buys is a default that
// cannot be wrong.
var taskSchemaJSON = `{"type":"object","properties":{` +
	`"title":{"type":"string","description":"One line naming the work, as a person would say it: \"Fix the nil-map crash in the reconciler\""},` +
	`"summary":{"type":"string","description":"Two or three lines the person reads to decide whether to redirect it: what will be done, and to what"},` +
	`"brief":{"type":"string","description":"The task's WHOLE context, self-contained: the goal, the files and symbols, the conventions and constraints, what has been tried, and anything from this conversation the work needs. The task never sees this conversation"},` +
	`"acceptance":{"type":"string","description":"The observable done-condition: the command that must pass, the behaviour that must hold, the output that must appear"},` +
	`"depends_on":{"type":"array","items":{"type":"number"},"description":"Ids of tasks that must finish before this one starts. Its brief is given their reports when it begins"},` +
	`"model":{"type":"string","description":"Optional. The model this work runs on, as a catalog id (\"anthropic/claude-opus-5\") or the part of one that names it (\"opus-5\"). Set it ONLY when the person asked for a particular model or class of model for this work; leave it out and the task runs on the configured one. A name that fits more than one model is shown to the person to settle"},` +
	`"max_steps":{"type":"number","description":"Optional. How many tool calls this work is worth before it is stopped as stuck (default ` + strconv.Itoa(taskMaxSteps) + `). Raise it for a sweep across many files; lower it for something small that should not wander"},` +
	`"no_progress":{"type":"number","description":"Optional. How many tool calls in a row may change no file before it is stopped as stuck (default ` + strconv.Itoa(taskNoProgress) + `). Raise it when the work genuinely needs a lot of reading before its first edit"}` +
	`},"required":["title","summary","brief","acceptance"],"additionalProperties":false}`

// taskArguments is the wire form.
type taskArguments struct {
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	Brief      string   `json:"brief"`
	Acceptance string   `json:"acceptance"`
	DependsOn  []uint64 `json:"depends_on"`
	Model      string   `json:"model"`
	MaxSteps   int      `json:"max_steps"`
	NoProgress int      `json:"no_progress"`
}

// taskSpec is one node's settled instruction: what the person was shown, and
// what the node will be given.
//
// IT IS BUILT ONCE AND AMENDED AT MOST ONCE — by a redirect, BEFORE admission
// (see proposeTask below). After [TaskGraph.admit] takes it, brief and
// acceptance are frozen for the node's whole life: that is the goal contract,
// and the law and the reason for it are written out on [TaskNode].
type taskSpec struct {
	title      string
	summary    string
	brief      string
	acceptance string
	dependsOn  []uint64
	// modelWord is the `model` argument as the model wrote it — a word, not an
	// id — and it lives only until [Agent.resolveTaskModel] has answered for it
	// (taskmodel.go). model is that answer: the id this node will actually run
	// on, settled before admission and frozen with the rest of the spec.
	//
	// modelOptions is the shortlist a word that fits more than one model raises.
	// It is on the proposal the person is shown and is empty by the time the node
	// is admitted: [settleTaskModel] closes it with their answer, or with the
	// closest match when the clock does.
	modelWord    string
	model        string
	modelOptions []string
	// maxSteps and noProgress are the node's own thresholds, 0 when the model
	// did not name one and the defaults apply (task_run.go).
	maxSteps   int
	noProgress int
	// design is set on the one node this session admits that is NOT a piece of
	// work handed to a child in a worktree: a sub-harness being written
	// (harness_task.go). It is nil on every ordinary task, and where it is set
	// [Agent.runTaskNode] hands the node to the designer's body instead of the
	// worker's — same graph, same room, same stop, a different middle.
	//
	// IT IS NOT IN THE CHECKPOINT, deliberately. A design node restored from
	// disk was RUNNING when the session ended, and a resume turns a running node
	// into a failed one before the graph ever holds it (task_store.go's
	// interrupt) — so there is nothing to re-enter, and a spec that claimed
	// otherwise would be a node the frontier tried to design again with the
	// registry already untouched.
	design *harnessDesignSpec
	// parent, depth and owner are THE FAMILY this proposal was made in, and they
	// are the whole of what nesting adds to the spec: 0, 0 and nil for the work
	// a conversation grooms, and the proposing node's id, its depth plus one and
	// its own agent for a sub-task. [TaskNode] carries the same three and says
	// what each is for.
	parent uint64
	depth  int
	owner  *Agent
}

// taskTools is the belt's task family — one tool, in the conversation and in
// every node that is not standing on the floor of the tree.
//
// A NODE'S PROPOSAL IS NOT A SECOND MACHINE. It reserves an id from the
// conversation's own sequence, admits into the conversation's own graph, and
// runs under the same cap and the same checkpoint; what makes it a sub-task is
// one field, the parent it is registered under. There is nobody in a worktree to
// show a proposal to, so the countdown simply expires and the work starts —
// which is what an unwatched proposal already did before nesting existed
// ([Agent.askTask]).
//
// AT THE FLOOR THE TOOL IS ABSENT, NOT REFUSING. A node at taskDepthLimit has
// nothing left worth handing out, so it is not given the verb — the law every
// conditional family on this belt is built on (tools.go).
func (a *Agent) taskTools() []bare.Tool {
	if !a.mayProposeTask() {
		return nil
	}
	return []bare.Tool{{
		Name:        "propose_task",
		Description: taskDescription,
		Schema:      json.RawMessage(taskSchemaJSON),
		Execute:     a.proposeTask,
	}}
}

// mayProposeTask says whether propose_task belongs on this agent's belt: always
// in a conversation, and in a node only when it was handed the conversation's
// graph to admit into and is not standing on the floor of the tree.
//
// The graph is what the orchestrate run's workers and the auditor are NOT handed
// (task_run.go's newTaskAgent), which is how they end up without the verb
// without anybody writing a second rule about them.
func (a *Agent) mayProposeTask() bool {
	return !a.config.InTask || a.config.mayFanOut()
}

// mayFanOut is the same question asked of a CONFIG, before there is an agent to
// ask: [renderSystem] decides whether to tell this worker how to split its work
// at construction, and the belt and the prompt must not disagree about whether
// it can.
func (c Config) mayFanOut() bool {
	return c.InTask && c.tasker != nil && c.taskDepth < taskDepthLimit
}

// proposeTask is the tool's whole life: validate, ask, and admit.
//
// Everything it can answer badly is an ordinary tool result rather than a Go
// error, the way every other tool on this belt answers: a brief the model
// forgot to write is a call it can make again, and an error would end the turn
// over a missing field.
func (a *Agent) proposeTask(ctx context.Context, args json.RawMessage) (string, bool, error) {
	spec, problem := parseTaskArguments(args)
	if problem != "" {
		return problem, true, nil
	}
	// WHICH HANDS THE WORK LEAVES ON, settled before anybody is asked anything
	// (taskmodel.go). A word that names no model this install has is a refusal
	// the model can act on — it names the nearest ids — and one that names
	// several is not refused at all: the shortlist rides on the proposal, and the
	// person settles it in the same breath as the work.
	choice := a.resolveTaskModel(spec.modelWord)
	if choice.problem != "" {
		return choice.problem, true, nil
	}
	spec.model, spec.modelOptions = choice.model, choice.options

	// WHOSE WORK THIS IS. In a conversation the three are zero and this is a
	// root; in a node they are the node, its depth and its own agent, and they
	// are what make the proposal a sub-task rather than a second root
	// (task_run.go's [TaskNode]).
	spec.parent, spec.depth, spec.owner = a.config.taskID, a.config.taskDepth+1, a
	graph := a.graph()
	// THE SLOT IS TAKEN BEFORE THE QUESTION and handed back by everything that
	// is not an admission, so a batch of proposals cannot walk through the fan
	// cap together ([TaskGraph.claimChild]).
	if refusal := graph.claimChild(spec.parent); refusal != "" {
		return refusal, true, nil
	}
	admitted := false
	defer func() {
		if !admitted {
			graph.releaseChild(spec.parent)
		}
	}()

	id := graph.reserve()
	answer, err := a.askTask(ctx, id, spec)
	if err != nil {
		// The turn ended under the question. Nothing was admitted, so there is
		// no node to cancel and nothing to clean up — the proposal simply never
		// became one.
		return "the task was never answered: the turn ended first", true, nil
	}
	if !answer.Approved {
		// A DECLINE IS A RESULT, NOT AN ERROR. The model asked a reasonable
		// question and got a plain no; handing it a failure would put a red row
		// in the transcript for a conversation working exactly as intended.
		if reason := strings.TrimSpace(answer.Redirect); reason != "" {
			return "the person declined this task: " + reason, false, nil
		}
		return "the person declined this task", false, nil
	}
	if redirect := strings.TrimSpace(answer.Redirect); redirect != "" {
		// APPENDED, never merged into the brief's prose. The person's words
		// arrive last and in their own voice, so the node reads them as the
		// correction they are rather than as one more paragraph the model wrote.
		//
		// AND IT HAPPENS HERE, BEFORE admit — this line is the last moment in
		// the node's life at which its brief may change. Everything after
		// admission reads a frozen spec, including the auditor that decides
		// whether the work is done ([TaskNode]'s goal contract): a target that
		// can move while the work runs is a target the work can always be made
		// to hit. A correction that arrives later is a new proposal, which is
		// the person exercising the same authority a second time.
		spec.brief = strings.TrimRight(spec.brief, "\n") +
			"\n\nThe person redirecting this task says: " + redirect
	}
	// THE SHORTLIST IS CLOSED HERE, in the same breath the brief is: a node is
	// admitted with one model and never a set of them. An answer that named one
	// of the options takes it; an answer that named nothing — including the
	// clock's silence — takes the closest match, which is the one the proposal
	// showed (taskmodel.go).
	if len(spec.modelOptions) > 0 {
		spec.model, spec.modelOptions = settleTaskModel(spec.modelOptions, answer.Model), nil
	}

	state := graph.admit(id, spec)
	admitted = true
	// THE MODEL IS NAMED BACK ONLY WHEN IT WAS ASKED FOR. A word resolves to an
	// id and a shortlist is settled by somebody else, so the one thing the model
	// cannot know after this call is what its own argument came to; a task that
	// named no model has nothing to be told, and a receipt reciting the default
	// every time would be a line nobody reads.
	on := ""
	if spec.modelWord != "" && spec.model != "" {
		on = " on " + spec.model
	}
	if state == TaskQueued {
		return fmt.Sprintf("task %d queued%s: %s\nIt starts when the work it waits on has finished and a slot is free. Keep working — its report arrives here.", id, on, spec.title), false, nil
	}
	return fmt.Sprintf("task %d started%s: %s\nIt works from the brief alone, in its own copy of the repository. Keep working — do not wait for it; its report arrives here when it lands.", id, on, spec.title), false, nil
}

// parseTaskArguments reads one call and says, in plain words, what is missing.
//
// Every field is required because every field is load-bearing: the title and
// summary are what the person decides on, the brief is the node's whole world,
// and the acceptance is what the node is finished against. A task groomed
// without one of them is not a task that was groomed.
func parseTaskArguments(args json.RawMessage) (taskSpec, string) {
	var parsed taskArguments
	if err := json.Unmarshal(args, &parsed); err != nil {
		return taskSpec{}, "Invalid arguments: " + err.Error()
	}
	spec := taskSpec{
		title:      strings.TrimSpace(parsed.Title),
		summary:    strings.TrimSpace(parsed.Summary),
		brief:      strings.TrimSpace(parsed.Brief),
		acceptance: strings.TrimSpace(parsed.Acceptance),
		dependsOn:  parsed.DependsOn,
		modelWord:  strings.TrimSpace(parsed.Model),
		maxSteps:   parsed.MaxSteps,
		noProgress: parsed.NoProgress,
	}
	// A NEGATIVE THRESHOLD IS A MISTAKE WORTH SAYING OUT LOUD, where an absent
	// one is not: omitting the field means "use the default" and is the ordinary
	// case, but a model that asked for -1 steps meant something it did not say,
	// and silently running that node for forty would be the harness inventing an
	// answer to a question the model got wrong.
	for _, threshold := range []struct {
		value int
		field string
	}{
		{spec.maxSteps, "max_steps"},
		{spec.noProgress, "no_progress"},
	} {
		if threshold.value < 0 {
			return spec, "Invalid arguments: " + threshold.field + " cannot be negative"
		}
	}
	for _, missing := range []struct {
		value string
		field string
	}{
		{spec.title, "title"},
		{spec.summary, "summary"},
		{spec.brief, "brief"},
		{spec.acceptance, "acceptance"},
	} {
		if missing.value == "" {
			return spec, "Invalid arguments: " + missing.field + " is required"
		}
	}
	return spec, ""
}

// ── the proposal's admission ────────────────────────────────────────────────

// ResolveTask answers one EventTaskProposal. A surface hands back the id the
// event carried and what the person said about it.
//
// An id nobody is waiting on — a proposal the clock already approved, a second
// click, a turn that was interrupted — is IGNORED rather than reported, exactly
// as [Agent.ResolveConsent] ignores a late answer. The answer is simply late,
// and the surface has already seen the node start or the turn end.
func (a *Agent) ResolveTask(id uint64, answer TaskAnswer) {
	a.mu.Lock()
	answers, waiting := a.taskAnswers[id]
	if waiting {
		delete(a.taskAnswers, id)
	}
	a.mu.Unlock()
	if !waiting {
		return
	}
	// Buffered to one and read at most once, so this never blocks and never
	// needs the lock held across it.
	answers <- answer
}

// askTask emits one proposal and waits for the person, the clock, or the end of
// the turn.
//
// THE CLOCK IS THE DIFFERENCE from consent's ask, and where it applies is the
// whole law:
//
//   - WATCHED, countdown > 0: the deadline is real and approves on expiry.
//   - WATCHED, countdown 0: no clock at all. Somebody is there, and they said
//     they want to answer every time; the Deadline field goes out zero so the
//     surface draws no bar.
//   - UNWATCHED: the deadline approves whatever the countdown says, zero
//     included. There is no one to wait for, and a headless run blocked on a
//     question nobody can see is a hang, not a safeguard.
func (a *Agent) askTask(ctx context.Context, id uint64, spec taskSpec) (TaskAnswer, error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return TaskAnswer{}, errAgentClosed
	}
	answers := make(chan TaskAnswer, 1)
	if a.taskAnswers == nil {
		a.taskAnswers = make(map[uint64]chan TaskAnswer, 1)
	}
	a.taskAnswers[id] = answers
	// The turn's hub, read under the same lock that registers the wait: a tool
	// runs inside a turn, and the turn's fan-out is where its question is seen.
	hub := a.hub
	watched := a.config.AskConsent && hub != nil
	countdown := time.Duration(a.config.TaskAutoApproveSeconds) * time.Second
	if countdown < 0 {
		countdown = 0
	}
	a.mu.Unlock()

	clock := watched && countdown > 0 || !watched
	var deadline time.Time
	if clock {
		deadline = time.Now().Add(countdown)
	}

	if hub != nil {
		hub.send(Event{
			Kind: EventTaskProposal,
			Tool: "propose_task",
			Task: &TaskNotice{
				ID:         id,
				Title:      spec.title,
				Summary:    spec.summary,
				Brief:      spec.brief,
				Acceptance: spec.acceptance,
				DependsOn:  spec.dependsOn,
				Deadline:   deadline,
				// What it will run on, and — when one word fit more than one model
				// — what it could run on instead. A surface draws the first as a
				// fact and offers the second as a choice; both are settled by the
				// answer this select is waiting for.
				Model:        firstTaskModel(spec.modelOptions, spec.model),
				ModelOptions: append([]string(nil), spec.modelOptions...),
			},
		})
	}

	var expiry <-chan time.Time
	if clock {
		timer := time.NewTimer(countdown)
		defer timer.Stop()
		expiry = timer.C
	}

	select {
	case answer := <-answers:
		return answer, nil
	case <-expiry:
		a.forgetTask(id)
		// Silence is a yes, and it is a yes with no redirect: the person said
		// nothing, so nothing is appended to the brief.
		return TaskAnswer{Approved: true}, nil
	case <-ctx.Done():
		a.forgetTask(id)
		return TaskAnswer{}, ctx.Err()
	}
}

// forgetTask drops a proposal nobody will answer, so a late resolve does not
// deliver into a channel with no reader and the map does not grow for the life
// of the session.
func (a *Agent) forgetTask(id uint64) {
	a.mu.Lock()
	delete(a.taskAnswers, id)
	a.mu.Unlock()
}

// PendingTasks lists the proposals still waiting for an answer, oldest id
// first. It is [Agent.PendingConsent] for the other question: a surface
// redrawing itself mid-turn — a resize, a reattach — needs to know a proposal
// is outstanding without having kept the event.
func (a *Agent) PendingTasks() []uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	ids := make([]uint64, 0, len(a.taskAnswers))
	for id := range a.taskAnswers {
		ids = append(ids, id)
	}
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j] < ids[j-1]; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
	return ids
}
