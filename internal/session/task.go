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
// What ships here is the degenerate graph — one node, no edges — and every
// piece of it is the graph's own. depends_on exists on the wire and is honoured
// by the executor; the model just never has a sibling to name yet.
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
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

const taskDescription = "Hand ONE self-contained piece of work to a task that runs on its own, outside this conversation, in its own copy of the repository. Use it when the work would flood the conversation — a long build-and-fix loop, a mechanical sweep across many files, a rewrite whose only interesting moment is the result — or when it simply wants a clean context of its own. Do NOT use it for a quick read, a question you can answer here, or anything that needs the back-and-forth of this conversation: a task cannot ask you anything once it starts. THE BRIEF IS THE TASK'S WHOLE WORLD. It never sees this conversation, so write it as if for a colleague joining today: the goal, the files and symbols involved, the conventions and constraints you have learned here, what has already been tried, and how to check the work. The person is shown the title and summary with a short countdown to redirect or wave it off; silence starts it. You get the id back immediately and the task's report arrives here when it lands, so keep working — never wait for it."

// taskSchemaJSON is the wire schema. depends_on is on it from the first day
// even though a one-node graph can never fill it: the field is the edge, the
// executor already honours it (task_run.go), and a schema that grew the concept
// later would be a second shape for the same idea.
const taskSchemaJSON = `{"type":"object","properties":{` +
	`"title":{"type":"string","description":"One line naming the work, as a person would say it: \"Fix the nil-map crash in the reconciler\""},` +
	`"summary":{"type":"string","description":"Two or three lines the person reads to decide whether to redirect it: what will be done, and to what"},` +
	`"brief":{"type":"string","description":"The task's WHOLE context, self-contained: the goal, the files and symbols, the conventions and constraints, what has been tried, and anything from this conversation the work needs. The task never sees this conversation"},` +
	`"acceptance":{"type":"string","description":"The observable done-condition: the command that must pass, the behaviour that must hold, the output that must appear"},` +
	`"depends_on":{"type":"array","items":{"type":"number"},"description":"Ids of tasks that must finish before this one starts. Its brief is given their reports when it begins"}` +
	`},"required":["title","summary","brief","acceptance"],"additionalProperties":false}`

// taskArguments is the wire form.
type taskArguments struct {
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	Brief      string   `json:"brief"`
	Acceptance string   `json:"acceptance"`
	DependsOn  []uint64 `json:"depends_on"`
}

// taskSpec is one node's settled instruction: what the person was shown, and
// what the node will be given. It is built once, amended at most once (by a
// redirect), and read without a lock by the goroutine that runs the node.
type taskSpec struct {
	title      string
	summary    string
	brief      string
	acceptance string
	dependsOn  []uint64
}

// taskTools is the belt's task family — one tool, and only in a conversation.
//
// A NODE DOES NOT PROPOSE. Not because nesting is forbidden but because there
// is nobody in a node's world to show a proposal to and no countdown that means
// anything there: decomposition, when it lands, is EDGES ADDED TO THIS GRAPH by
// the conversation that owns it, not a second proposal machine running inside a
// worktree. The exclusion is one line here rather than a rule the node has to
// be told about in its prompt (tools.go builds the node's belt from the same
// function this one is on).
func (a *Agent) taskTools() []bare.Tool {
	if a.config.InTask {
		return nil
	}
	return []bare.Tool{{
		Name:        "propose_task",
		Description: taskDescription,
		Schema:      json.RawMessage(taskSchemaJSON),
		Execute:     a.proposeTask,
	}}
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

	graph := a.graph()
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
		spec.brief = strings.TrimRight(spec.brief, "\n") +
			"\n\nThe person redirecting this task says: " + redirect
	}

	state := graph.admit(id, spec)
	if state == TaskQueued {
		return fmt.Sprintf("task %d queued: %s\nIt starts when the work it waits on has finished and a slot is free. Keep working — its report arrives here.", id, spec.title), false, nil
	}
	return fmt.Sprintf("task %d started: %s\nIt works from the brief alone, in its own copy of the repository. Keep working — do not wait for it; its report arrives here when it lands.", id, spec.title), false, nil
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
