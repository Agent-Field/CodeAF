package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// maxSteps bounds the tool calls of one turn.
const maxSteps = 8

// Service is the app agent. It owns threads and a turn loop over svc.Model;
// it has no way to write definition files (rule 02).
type Service struct {
	ctx     context.Context
	all     *svc.Services
	threads *threadStore
}

var _ svc.Agent = (*Service)(nil)

func New(cfg svc.Config, all *svc.Services) *Service {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return &Service{ctx: ctx, all: all, threads: newThreadStore(cfg.Root)}
}

// Send validates the call, then runs the turn in the background and streams
// it on app/<id>/agent/<thread>.
func (s *Service) Send(ctx context.Context, scope svc.Scope, p bridge.AgentSendParams) (bridge.AgentSendResult, error) {
	if strings.TrimSpace(p.Text) == "" {
		return bridge.AgentSendResult{}, bridge.Errorf(bridge.CodeInvalid, "text is required")
	}
	bundle, err := s.all.Store.Definition(ctx, scope.App)
	if err != nil {
		return bridge.AgentSendResult{}, err
	}
	if bundle == nil || bundle.App == nil || bundle.Agent == nil {
		return bridge.AgentSendResult{}, bridge.Errorf(bridge.CodeConflict, "the app has no agent yet")
	}
	id := p.Thread
	if id == "" {
		id = randomID("t")
	}
	if !threadID.MatchString(id) {
		return bridge.AgentSendResult{}, bridge.Errorf(bridge.CodeInvalid, "invalid thread id")
	}
	t := &turn{svc: s, scope: scope, bundle: bundle, params: p, thread: id, id: randomID("u")}
	go t.run()
	return bridge.AgentSendResult{Thread: id, Turn: t.id}, nil
}

func randomID(prefix string) string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// turn is one agent.send: the person's message and everything that follows.
type turn struct {
	svc    *Service
	scope  svc.Scope
	bundle *spec.Bundle
	params bridge.AgentSendParams
	thread string
	id     string
	denied int // permission asks refused this turn
}

func (t *turn) emit(e bridge.AgentEvent) {
	e.Text = safeText(e.Text)
	e.Summary = safeText(e.Summary)
	e.Turn = t.id
	t.svc.all.Hub.Publish(bridge.TopicAgent(t.scope.App, t.thread), e)
}

func (t *turn) fail(err error) {
	msg := err.Error()
	if typed, ok := err.(*bridge.Error); ok {
		msg = typed.Message
	}
	t.emit(bridge.AgentEvent{Kind: bridge.AgentError, Text: msg})
}

func (t *turn) run() {
	lock := t.svc.threads.turnLock(t.scope.App, t.thread)
	lock.Lock()
	defer lock.Unlock()
	th, err := t.svc.threads.load(t.scope.App, t.thread)
	if err != nil {
		t.fail(err)
		return
	}
	th.Messages = append(th.Messages, svc.ModelMessage{Role: "user", Text: t.userText()})
	err = t.loop(th)
	if saveErr := t.svc.threads.save(t.scope.App, th); err == nil {
		err = saveErr
	}
	if err != nil {
		t.fail(err)
		return
	}
	t.emit(bridge.AgentEvent{Kind: bridge.AgentDone, Cost: t.cost(th)})
}

// userText adds what the person is looking at to their words.
func (t *turn) userText() string {
	var sb strings.Builder
	if t.params.Screen != "" {
		sb.WriteString("[screen: " + t.params.Screen + "]\n")
	}
	if t.params.Record != "" {
		sb.WriteString("[record in view: " + t.params.Record + "]\n")
	}
	sb.WriteString(t.params.Text)
	return sb.String()
}

func (t *turn) cost(th *thread) *bridge.Cost {
	return &bridge.Cost{Spent: th.Spent, Cap: t.bundle.App.Budget.Month}
}

func (t *turn) model() string {
	if t.bundle.Agent.Model != "" {
		return t.bundle.Agent.Model
	}
	return t.bundle.App.Agent.Model
}

func (t *turn) overBudget(th *thread) bool {
	cap := t.bundle.App.Budget.Month
	return cap > 0 && th.Spent >= cap
}

func (t *turn) loop(th *thread) error {
	tb := newToolbox(t.svc.all, t.scope, t.bundle)
	system := systemPrompt(t.bundle, tb.allowed)
	for range maxSteps {
		if t.overBudget(th) {
			return bridge.Errorf(bridge.CodeBudget, "the app's monthly budget is used up")
		}
		reply, err := t.svc.all.Model.Complete(t.svc.ctx, svc.ModelCall{
			Model: t.model(), System: system, Messages: th.Messages,
			Schema: actionSchema, App: t.scope.App, Role: "app agent",
		})
		if err != nil {
			return err
		}
		th.Spent += reply.Cost
		a := parseAction(reply.Text)
		accepted, _ := json.Marshal(a)
		th.Messages = append(th.Messages, svc.ModelMessage{Role: "assistant", Text: string(accepted)})
		done, err := t.step(tb, th, a)
		if a.dropped > 0 {
			th.Messages = append(th.Messages, svc.ModelMessage{Role: "user", Text: fmt.Sprintf("Ignored %d additional actions in your reply. Only the first action was accepted. Reply with exactly one action and wait for its result before choosing the next.", a.dropped)})
		}
		if done || err != nil {
			return err
		}
	}
	t.emit(bridge.AgentEvent{Kind: bridge.AgentDelta, Text: "I stopped after too many steps."})
	return nil
}

// step runs one action. It reports whether the turn is over.
func (t *turn) step(tb *toolbox, th *thread, a action) (bool, error) {
	if a.ShapeThis {
		t.emit(bridge.AgentEvent{Kind: bridge.AgentShapeThis, Text: ShapeThisText, Request: t.params.Text})
		th.Messages[len(th.Messages)-1].Text = ShapeThisText
		return true, nil
	}
	if a.Say != "" {
		t.emit(bridge.AgentEvent{Kind: bridge.AgentDelta, Text: a.Say})
	}
	if a.Tool == "" {
		return true, nil
	}
	feedback, err := t.useTool(tb, a)
	if err != nil {
		return true, err
	}
	if t.denied > 1 {
		t.emit(bridge.AgentEvent{Kind: bridge.AgentDelta, Text: cannotLine(a.Tool)})
		return true, nil
	}
	th.Messages = append(th.Messages, svc.ModelMessage{Role: "user", Text: resultText(a.Tool, feedback)})
	return false, nil
}

// resultText is how a tool's result goes back to the model. Actions are JSON
// in text, not native tool calls, so there is no tool_call_id for a "tool"
// message to carry and the API refuses one: the result is sent as a user turn.
func resultText(tool, feedback string) string {
	return "Result of " + tool + ":\n" + feedback
}

// useTool runs one action and tells the person what it did in plain words,
// never the call's JSON.
func (t *turn) useTool(tb *toolbox, a action) (string, error) {
	out, err := tb.call(t.svc.ctx, a.Tool, a.Args)
	t.emit(bridge.AgentEvent{Kind: bridge.AgentTool, Tool: a.Tool, Summary: summarize(tb.bundle, a.Tool, a.Args, out, err)})
	if isDenied(err) {
		t.denied++
		return deniedFeedback(err), nil
	}
	if err != nil {
		return toolFailure(err)
	}
	if out.approval != nil {
		t.emit(bridge.AgentEvent{Kind: bridge.AgentApproval, Approval: out.approval})
	}
	raw, err := json.Marshal(out.result)
	feedback := string(raw)
	if out.note != "" {
		feedback += "\n" + out.note
	}
	return feedback, err
}
