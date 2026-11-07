package main

// ── A PERSON ANSWERS AN AGENTFIELD RUN'S PAUSE ──────────────────────────────
//
// An AgentField reasoner that paused for a person (app.pause / Agent.Pause)
// puts its job in Needs you with the whole body it asks about
// (needs_you.question.kind agentfield_approval). The person answers it with
// the job verbs they already have — Approve, Ask for changes with their
// words, Reject — and THIS FACTORY CARRIES THE ANSWER TO THE CONTROL PLANE
// itself, over its own AgentField connection: the control plane holds the one
// approval state, resumes the paused reasoner, and the run's next reading
// takes the question down (factoryrun.SyncAgentFieldWait). Nothing rides the
// lease to the runner, so an answer reaches the run however many machines are
// between them, and one given in hax or on the control plane's own page
// clears here the same way.
//
// A Reject also stops the job, the way the person's Stop does: the reasoner
// sees "rejected" and its run is cancelled, and the job reads "Stopped by
// you" rather than a failure.
//
// A step's question (needs_you.question.kind step) answered with Approve and
// words is the factory's own answer door, yes with those words.

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/agentfield"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
)

// afStepQuestion is the QKind a step's question carries while the person holds
// it (internal/factory/run's qkindStep).
const afStepQuestion = "step"

// afAnswerWords are the refusals an answer to a run's pause can meet.
const (
	afAnswerNoConnection = "this factory is not connected to AgentField, so it cannot answer the run; answer it on the control plane"
	afAnswerNeedsWords   = "say what to change: Ask for changes sends your words to the run"
	afAnswerGone         = "the run is no longer waiting for this answer"
	afAnswerElsewhere    = "this was already answered somewhere else, and that answer stands"
	afAnswerIsApproval   = "this is the AgentField run's approval: answer it with Approve, Ask for changes or Reject"
)

// localWaiting is the job on this machine's factory and the run pause it
// waits on; ok is false when the factory is not this machine's, or the job
// waits on no run's pause.
func (b *appBackend) localWaiting(id string, job int) (*appFactoryHost, factory.Item, *factory.AgentFieldWait, bool) {
	host, err := b.localHostOf(id)
	if err != nil || host == nil {
		return nil, factory.Item{}, nil, false
	}
	it, err := host.floor.Get(job)
	if err != nil {
		return nil, factory.Item{}, nil, false
	}
	if it.QKind != factory.QKindAgentField {
		return host, it, nil, false
	}
	w := factory.AgentFieldWaiting(it)
	return host, it, w, w != nil
}

// answerAgentField carries the person's decision on a run's pause to the
// control plane. handled is false when the job waits on no such pause, and
// the verb is then the factory's own.
func (b *appBackend) answerAgentField(ctx context.Context, id string, job int, action, decision, words string, expect *api.Expect) (appwire.FactoryJobActed, bool, error) {
	host, it, w, ok := b.localWaiting(id, job)
	if !ok {
		return appwire.FactoryJobActed{}, false, nil
	}
	words = strings.TrimSpace(words)
	if decision == agentfield.DecisionRequestChanges && words == "" {
		return appwire.FactoryJobActed{}, true, appwire.Errorf(appwire.CodeBadParams, "%s", afAnswerNeedsWords)
	}
	client, _, connected := host.agentFieldConn()
	if !connected {
		return appwire.FactoryJobActed{}, true, appwire.Errorf(appwire.CodeConflict, "%s", afAnswerNoConnection)
	}
	err := client.ResolveApproval(ctx, w.Execution, decision, words)
	elsewhere := errors.Is(err, agentfield.ErrAnsweredElsewhere)
	if err != nil && !elsewhere {
		return appwire.FactoryJobActed{}, true, appwire.Errorf(appwire.CodeConflict, "%s", afAnswerError(err))
	}
	if elsewhere {
		// The first answer stands. Its question comes down here at once;
		// the run's next reading says what it was.
		_ = host.floor.Update(job, func(it *factory.Item) error {
			for _, r := range afRuns(it) {
				if r.Waiting != nil && r.Waiting.Execution == w.Execution {
					r.Waiting = nil
				}
			}
			factoryrun.SyncAgentFieldWait(it, time.Now())
			return nil
		})
		return appwire.FactoryJobActed{}, true, appwire.Errorf(appwire.CodeConflict, "%s", afAnswerElsewhere)
	}
	now := time.Now()
	by := factory.NormalizeHandle(it.Person())
	_ = host.floor.Update(job, func(it *factory.Item) error {
		for _, r := range afRuns(it) {
			if r.Waiting == nil || r.Waiting.Execution != w.Execution {
				continue
			}
			r.Decisions = append(r.Decisions, factory.AgentFieldDecision{RequestID: w.RequestID, Execution: w.Execution, Decision: decision, Words: words, By: by, At: now})
			// THE ANSWER IS TAKEN DOWN AT ONCE: the control plane has it, and
			// the run's next reading would only say so a moment later.
			r.Waiting = nil
		}
		factoryrun.SyncAgentFieldWait(it, now)
		return nil
	})
	if decision == agentfield.DecisionRejected {
		// A REJECT ENDS THE JOB AS THE PERSON'S STOP DOES. The control plane
		// has already cancelled the run; the job's own Stop says so here.
		if _, err := b.jobWrite(ctx, id, appwire.JobActStop, job, factoryVerbStop, guardBody{}); err != nil {
			return appwire.FactoryJobActed{}, true, err
		}
	}
	return b.factoryHub().acted(ctx, id, action, job, appFactoryAnswer{ID: job}), true, nil
}

// afRuns are the job's run records: its own run, and each agentfield step's.
func afRuns(it *factory.Item) []*factory.AgentFieldRun {
	var out []*factory.AgentFieldRun
	if it.AgentField != nil {
		out = append(out, it.AgentField)
	}
	for _, r := range it.AgentFieldSteps {
		if r != nil {
			out = append(out, r)
		}
	}
	return out
}

// afAnswerError is the control plane's refusal in a person's words.
func afAnswerError(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "no_approval_request") || strings.Contains(msg, "not found") {
		return afAnswerGone
	}
	return msg
}

// answerStepQuestion answers a step's question with the person's words,
// through the factory's own answer door. handled is false when the job is
// not waiting on a step's question.
func (b *appBackend) answerStepQuestion(ctx context.Context, id string, job int, words string, expect *api.Expect) (appwire.FactoryJobActed, bool, error) {
	_, it, _, _ := b.localWaiting(id, job)
	if it.ID == 0 || it.State != factory.StateNeedsYou || it.QKind != afStepQuestion {
		return appwire.FactoryJobActed{}, false, nil
	}
	yes := true
	a, err := b.factoryHub().write(ctx, id, jobPath(job, "answer"), struct {
		Yes    *bool       `json:"yes"`
		Words  string      `json:"words"`
		Expect *api.Expect `json:"expect,omitempty"`
	}{&yes, strings.TrimSpace(words), expect})
	if err != nil {
		return appwire.FactoryJobActed{}, true, err
	}
	return b.factoryHub().acted(ctx, id, appwire.JobActApprove, job, a), true, nil
}

// FactoryAgentFieldReasoners is factory.agentfield_reasoners: the reasoners
// of every node this factory's control plane knows, for an agentfield step to
// name. Only this machine's factory has the connection to ask.
func (b *appBackend) FactoryAgentFieldReasoners(ctx context.Context, p appwire.FactoryAgentFieldReasonersParams) (appwire.FactoryAgentFieldReasoners, error) {
	host, err := b.localHostOf(p.Factory)
	if err != nil {
		return appwire.FactoryAgentFieldReasoners{}, err
	}
	client, _, ok := host.agentFieldConn()
	lister, can := client.(interface {
		Nodes(context.Context) ([]agentfield.NodeReasoners, error)
	})
	if !ok || !can {
		return appwire.FactoryAgentFieldReasoners{}, appwire.Errorf(appwire.CodeConflict, "this factory is not connected to AgentField, so it has no reasoners to list")
	}
	nodes, err := lister.Nodes(ctx)
	if err != nil {
		return appwire.FactoryAgentFieldReasoners{}, appwire.Errorf(appwire.CodeConflict, "the AgentField control plane did not list its nodes: %s", err.Error())
	}
	out := appwire.FactoryAgentFieldReasoners{Reasoners: []appwire.FactoryAgentFieldReasoner{}, Placeholders: append([]string(nil), factory.AgentFieldPlaceholders...)}
	for _, n := range nodes {
		for _, r := range n.Reasoners {
			out.Reasoners = append(out.Reasoners, appwire.FactoryAgentFieldReasoner{Node: n.ID, Reasoner: r.ID, Target: n.ID + "." + r.ID,
				Description: r.Description, Online: n.HealthStatus == "active", Input: afSchemaFields(r.InputSchema)})
		}
	}
	return out, nil
}

// afSchemaFields is the top-level fields a JSON Schema object names.
func afSchemaFields(schema json.RawMessage) []appwire.FactoryAgentFieldInput {
	var s struct {
		Properties map[string]struct {
			Type any `json:"type"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if json.Unmarshal(schema, &s) != nil || len(s.Properties) == 0 {
		return nil
	}
	names := make([]string, 0, len(s.Properties))
	for k := range s.Properties {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make([]appwire.FactoryAgentFieldInput, 0, len(names))
	for _, k := range names {
		typ, _ := s.Properties[k].Type.(string)
		out = append(out, appwire.FactoryAgentFieldInput{Name: k, Required: slices.Contains(s.Required, k), Type: typ})
	}
	return out
}

// FactoryJobAnswer is factory.job_answer: the person's answer to a step's
// question — the option they chose, their words, or both — through the
// factory's own answer door, which refuses an answer to a question that has
// changed (question_id) or a job that moved on (expect). A run's approval is
// not a step's question and is refused here with the verbs that answer it.
func (b *appBackend) FactoryJobAnswer(ctx context.Context, p appwire.FactoryJobAnswerParams) (appwire.FactoryJobActed, error) {
	if _, it, _, _ := b.localWaiting(p.Factory, p.Job); it.QKind == factory.QKindAgentField {
		return appwire.FactoryJobActed{}, appwire.Errorf(appwire.CodeBadParams, "%s", afAnswerIsApproval)
	}
	answer := p.Option
	switch {
	case answer == "":
		answer = p.Words
	case p.Words != "":
		answer += ": " + p.Words
	}
	yes := true
	return b.jobWrite(ctx, p.Factory, appwire.JobActAnswer, p.Job, "answer", struct {
		Yes        *bool       `json:"yes"`
		Words      string      `json:"words"`
		QuestionID string      `json:"question_id"`
		Expect     *api.Expect `json:"expect,omitempty"`
	}{&yes, answer, p.QuestionID, p.Expect})
}
