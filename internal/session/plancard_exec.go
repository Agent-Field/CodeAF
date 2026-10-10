package session

// The executor behind the `plan` card. A model proposes a [Plan]; a plan that
// stays inside the current chat runs at once, and one that reaches beyond it
// waits for the person's Go, Edit or Cancel. Each step is carried out through
// the doors the rest of the engine already has (the plan verbs, a note to a
// chat, a question put to a place, remember), and each answers with what it
// did and how to take it back, so the results can be drawn as ONE receipt.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/roles"
)

var (
	// errPlanNoSuchTask is what a deleted or foreign task id answers; the
	// executor reads it as "skip this line" rather than "the plan failed".
	errPlanNoSuchTask = errors.New("no such task")
	// ErrPlanTargetGone is the same word for effects that are not task rows: a
	// chat or place that no longer exists.
	ErrPlanTargetGone = errors.New("that target is gone")
	// ErrPlanUnknown names a plan id that is not waiting (never proposed,
	// cancelled, or already run and forgotten by a restart).
	ErrPlanUnknown = errors.New("no such plan is waiting")
	// ErrPlanCancelled answers Go or Edit on a plan the person cancelled.
	ErrPlanCancelled = errors.New("that plan was cancelled")
)

// noSuchPlanTask keeps the sentence [planNoTask] always had while letting
// errors.Is recognise it.
type noSuchPlanTask struct{ id string }

func (e noSuchPlanTask) Error() string        { return fmt.Sprintf("no task %s in this conversation", e.id) }
func (e noSuchPlanTask) Is(target error) bool { return target == errPlanNoSuchTask }

// PlanEffects is every outside thing a step can do. The executor owns the
// order, the skipping and the receipt; this owns the doing, which is why tests
// drive it with a fake and the Agent supplies the real one.
type PlanEffects interface {
	NoteTask(id, text string) error
	PauseTask(id string) error
	ResumeTask(id string) error
	CancelTask(id string) error
	// NoteChat reaches a chat even when it is closed: the note is kept for its
	// next turn rather than refused.
	NoteChat(chat, text string) error
	AskPlace(ctx context.Context, place, text string) (string, error)
	// Remember saves a fact and returns the token that takes exactly it back.
	Remember(text string) (token string, err error)
	Forget(token string) error
}

// PlanStatus is how one step ended.
type PlanStatus string

const (
	PlanStepDone    PlanStatus = "done"
	PlanStepSkipped PlanStatus = "skipped"
	PlanStepFailed  PlanStatus = "failed"
)

// PlanUndo is the data for taking one step back. Kind is the verb that undoes
// it; a step with no honest inverse (a note already read, a stop, an answer
// already given) carries none, because an undo that only pretends is worse.
type PlanUndo struct {
	Kind   string     `json:"kind"` // "resume", "hold", "forget"
	Target PlanTarget `json:"target"`
	Token  string     `json:"token,omitempty"`
}

// PlanStepResult is one line of the receipt.
type PlanStepResult struct {
	Step   PlanCardStep `json:"step"`
	Status PlanStatus   `json:"status"`
	// Line is the person-facing sentence: what happened, or why it was skipped.
	Line   string    `json:"line"`
	Answer string    `json:"answer,omitempty"`
	Undo   *PlanUndo `json:"undo,omitempty"`
}

// PlanReceipt groups consecutive results of one plan into a single receipt.
type PlanReceipt struct {
	PlanID  string           `json:"planId"`
	Results []PlanStepResult `json:"results"`
}

// PlanBook holds the plans one conversation proposed and what became of them.
type PlanBook struct {
	fx   PlanEffects
	emit func(Event)

	mu      sync.Mutex
	seq     int
	waiting map[string]*Plan
	ran     map[string]*PlanReceipt
	dropped map[string]bool
}

// NewPlanBook builds a book over its effects. emit tells the surface a plan
// is waiting; nil means nobody is listening.
func NewPlanBook(fx PlanEffects, emit func(Event)) *PlanBook {
	return &PlanBook{fx: fx, emit: emit, waiting: map[string]*Plan{}, ran: map[string]*PlanReceipt{}, dropped: map[string]bool{}}
}

// ValidatePlan refuses a plan that could not be carried out as written, before
// anything runs, so a malformed plan never half-executes.
func ValidatePlan(p Plan) error {
	if len(p.Steps) == 0 {
		return errors.New("a plan needs at least one step")
	}
	for i, s := range p.Steps {
		n := 0
		for _, v := range []string{s.Target.Chat, s.Target.Task, s.Target.Place} {
			if strings.TrimSpace(v) != "" {
				n++
			}
		}
		fail := func(msg string) error { return fmt.Errorf("step %d (%s): %s", i+1, s.Kind, msg) }
		switch s.Kind {
		case PlanStepHold, PlanStepStart, PlanStepStop:
			if s.Target.Task == "" || n != 1 {
				return fail("needs exactly one task target")
			}
		case PlanStepSteer:
			if n != 1 || s.Target.Place != "" {
				return fail("needs one task or chat target")
			}
			if strings.TrimSpace(s.Text) == "" {
				return fail("needs words")
			}
		case PlanStepAskPlace:
			if s.Target.Place == "" || n != 1 {
				return fail("needs exactly one place target")
			}
			if strings.TrimSpace(s.Text) == "" {
				return fail("needs a question")
			}
		case PlanStepRemember:
			if n != 0 {
				return fail("takes no target")
			}
			if strings.TrimSpace(s.Text) == "" {
				return fail("needs a fact")
			}
		default:
			return fail("unknown action")
		}
	}
	return nil
}

// Propose takes a plan from the model. Inside the chat it runs now and the
// receipt comes back; beyond the chat it waits, tells the surface, and the
// receipt is nil until Go.
func (b *PlanBook) Propose(ctx context.Context, p Plan) (*PlanReceipt, error) {
	if err := ValidatePlan(p); err != nil {
		return nil, err
	}
	b.mu.Lock()
	if strings.TrimSpace(p.ID) == "" {
		b.seq++
		p.ID = fmt.Sprintf("plan-%d", b.seq)
	}
	if _, taken := b.ran[p.ID]; taken || b.waiting[p.ID] != nil || b.dropped[p.ID] {
		b.mu.Unlock()
		return nil, fmt.Errorf("plan %s was already proposed", p.ID)
	}
	if !p.ReachesBeyond {
		b.mu.Unlock()
		return b.execute(ctx, p), nil
	}
	b.waiting[p.ID] = &p
	b.mu.Unlock()
	b.announce(p)
	return nil, nil
}

// Go runs a waiting plan. Asked again after it ran it returns the same
// receipt without touching anything, so a double click or a retry is safe.
func (b *PlanBook) Go(ctx context.Context, id string) (*PlanReceipt, error) {
	b.mu.Lock()
	if r := b.ran[id]; r != nil {
		b.mu.Unlock()
		return r, nil
	}
	if b.dropped[id] {
		b.mu.Unlock()
		return nil, ErrPlanCancelled
	}
	p := b.waiting[id]
	if p == nil {
		b.mu.Unlock()
		return nil, ErrPlanUnknown
	}
	delete(b.waiting, id)
	b.ran[id] = &PlanReceipt{PlanID: id} // claimed: a racing Go reads this and does nothing
	b.mu.Unlock()

	r := b.execute(ctx, *p)
	b.mu.Lock()
	b.ran[id] = r
	b.mu.Unlock()
	return r, nil
}

// Edit replaces a waiting plan's steps and keeps it waiting, redrawing the card.
func (b *PlanBook) Edit(id string, steps []PlanCardStep) error {
	b.mu.Lock()
	p := b.waiting[id]
	if p == nil {
		dropped := b.dropped[id]
		b.mu.Unlock()
		if dropped {
			return ErrPlanCancelled
		}
		return ErrPlanUnknown
	}
	next := *p
	next.Steps = append([]PlanCardStep(nil), steps...)
	if err := ValidatePlan(next); err != nil {
		b.mu.Unlock()
		return err
	}
	b.waiting[id] = &next
	b.mu.Unlock()
	b.announce(next)
	return nil
}

// Cancel drops a waiting plan. It touches no effect at all.
func (b *PlanBook) Cancel(id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.waiting[id] == nil {
		if b.dropped[id] {
			return nil
		}
		return ErrPlanUnknown
	}
	delete(b.waiting, id)
	b.dropped[id] = true
	return nil
}

// Undo takes one step back using the data its receipt line carried.
func (b *PlanBook) Undo(u PlanUndo) error {
	switch u.Kind {
	case "resume":
		return b.fx.ResumeTask(u.Target.Task)
	case "hold":
		return b.fx.PauseTask(u.Target.Task)
	case "forget":
		return b.fx.Forget(u.Token)
	}
	return fmt.Errorf("cannot undo %q", u.Kind)
}

func (b *PlanBook) announce(p Plan) {
	if b.emit != nil {
		b.emit(Event{Kind: EventPlan, ID: 0, Plan: &p})
	}
}

func (b *PlanBook) execute(ctx context.Context, p Plan) *PlanReceipt {
	r := &PlanReceipt{PlanID: p.ID}
	for _, s := range p.Steps {
		r.Results = append(r.Results, b.step(ctx, s))
	}
	return r
}

// step carries out one step. A target that has gone away is a skipped line,
// not a failure: the person deleted it between the proposal and the Go.
func (b *PlanBook) step(ctx context.Context, s PlanCardStep) PlanStepResult {
	res := PlanStepResult{Step: s, Status: PlanStepDone}
	var err error
	switch s.Kind {
	case PlanStepHold:
		if err = b.fx.PauseTask(s.Target.Task); err == nil {
			res.Line, res.Undo = "Held "+s.Target.Task, &PlanUndo{Kind: "resume", Target: s.Target}
		}
	case PlanStepStart:
		if err = b.fx.ResumeTask(s.Target.Task); err == nil {
			res.Line, res.Undo = "Started "+s.Target.Task, &PlanUndo{Kind: "hold", Target: s.Target}
		}
	case PlanStepStop:
		if err = b.fx.CancelTask(s.Target.Task); err == nil {
			res.Line = "Stopped " + s.Target.Task
		}
	case PlanStepSteer:
		if s.Target.Task != "" {
			err = b.fx.NoteTask(s.Target.Task, s.Text)
		} else {
			err = b.fx.NoteChat(s.Target.Chat, s.Text)
		}
		if err == nil {
			res.Line = "Told " + firstNonEmpty(s.Target.Task, s.Target.Chat)
		}
	case PlanStepAskPlace:
		var answer string
		if answer, err = b.fx.AskPlace(ctx, s.Target.Place, s.Text); err == nil {
			res.Line, res.Answer = "Asked "+s.Target.Place, answer
		}
	case PlanStepRemember:
		var token string
		if token, err = b.fx.Remember(s.Text); err == nil {
			res.Line = "Remembered: " + s.Text
			if token != "" {
				res.Undo = &PlanUndo{Kind: "forget", Token: token}
			}
		}
	}
	if err == nil {
		return res
	}
	res.Undo, res.Answer = nil, ""
	if errors.Is(err, errPlanNoSuchTask) || errors.Is(err, ErrPlanTargetGone) {
		res.Status, res.Line = PlanStepSkipped, "Skipped: "+planStepSubject(s)+" is gone"
		return res
	}
	res.Status, res.Line = PlanStepFailed, "Could not do that: "+err.Error()
	return res
}

func planStepSubject(s PlanCardStep) string {
	return firstNonEmpty(s.Target.Task, s.Target.Chat, s.Target.Place, "the target")
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// agentPlanEffects is the real set of effects for one conversation.
type agentPlanEffects struct{ a *Agent }

func (e agentPlanEffects) NoteTask(id, text string) error { return e.a.PlanNoteFromChat(id, text) }
func (e agentPlanEffects) PauseTask(id string) error      { return e.a.PlanPause(id) }
func (e agentPlanEffects) ResumeTask(id string) error     { return e.a.PlanResume(id) }
func (e agentPlanEffects) CancelTask(id string) error     { return e.a.PlanCancel(id) }

// NoteChat reaches this conversation only. Another chat lives in another
// engine instance the agent has no handle on, so the answer says so rather
// than pretending the note was kept.
func (e agentPlanEffects) NoteChat(chat, text string) error {
	e.a.mu.Lock()
	mine := e.a.id
	e.a.mu.Unlock()
	if chat != mine {
		return errors.New("that chat is not reachable from here")
	}
	if !e.a.enqueueNote(userText(text)) {
		return errors.New("the note was empty or the chat is closed")
	}
	return nil
}

func (e agentPlanEffects) AskPlace(ctx context.Context, place, text string) (string, error) {
	answer, err := e.a.AskPlaces(ctx, placegraph.ModelRequest{
		Role:   roles.RolePlaceSuggest,
		System: "Answer as the place " + place + " would, in two sentences or fewer.",
		User:   text,
	})
	return answer.Text, err
}

// Remember writes the fact to the chat's place when it has one, the only
// kind of save that has an exact undo token; any other scope saves but offers
// no undo.
func (e agentPlanEffects) Remember(text string) (string, error) {
	if _, token, handled, err := e.a.rememberPlaceUndo(text, ""); handled {
		return token, err
	}
	_, err := e.a.RememberScoped(text, "")
	return "", err
}

func (e agentPlanEffects) Forget(token string) error {
	door := e.a.config.PlaceGraph
	if door == nil || door.Path == "" {
		return ErrPlanTargetGone
	}
	graph, err := placegraph.Open(placegraph.Options{Path: door.Path})
	if err != nil {
		return err
	}
	_, err = graph.UndoRemember(token)
	return err
}

// PlanCards is this conversation's book of plan cards, built on first use.
// Its announcements travel the question lane, the standing one a surface
// already holds for the whole life of the session.
func (a *Agent) PlanCards() *PlanBook {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.planBook == nil {
		a.planBook = NewPlanBook(agentPlanEffects{a}, a.emitPlanCard)
	}
	return a.planBook
}

func (a *Agent) emitPlanCard(event Event) {
	a.mu.Lock()
	watchers := append([]*eventStream(nil), a.questionWatchers...)
	a.mu.Unlock()
	for _, w := range watchers {
		w.send(event)
	}
}

const planCardDescription = "Propose a short sequence of actions as a card for the person. Steps: hold/start/stop a task, steer a task or chat with words, ask-place a question of a place, remember a fact. Set reachesBeyond true when any step acts outside this chat; the person then chooses Go, Edit or Cancel before anything runs. Otherwise the steps run at once. Each step carries a target (exactly one of chat, task, place), never a name written in text."

const planCardSchemaJSON = `{"type":"object","properties":{"reachesBeyond":{"type":"boolean"},"steps":{"type":"array","minItems":1,"items":{"type":"object","properties":{"kind":{"type":"string","enum":["hold","steer","start","stop","ask-place","remember"]},"target":{"type":"object","properties":{"chat":{"type":"string"},"task":{"type":"string"},"place":{"type":"string"}}},"text":{"type":"string"}},"required":["kind"]}}},"required":["steps"]}`

// planCardTools is the `plan` verb, absent unless a surface that draws the
// card asked for it ([Config.PlanCards]).
func (a *Agent) planCardTools() []bare.Tool {
	if !a.config.PlanCards {
		return nil
	}
	return []bare.Tool{{
		Name:        "plan",
		Description: planCardDescription,
		Schema:      json.RawMessage(planCardSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var p Plan
			if err := decodeToolArguments(args, &p); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			p.ID = "" // the book numbers plans; a model-chosen id could collide
			receipt, err := a.PlanCards().Propose(ctx, p)
			if err != nil {
				return "Could not plan that: " + err.Error(), true, nil
			}
			if receipt == nil {
				return "The plan is waiting for the person to choose Go, Edit or Cancel. Nothing has run.", false, nil
			}
			lines := make([]string, len(receipt.Results))
			for i, r := range receipt.Results {
				lines[i] = r.Line
			}
			return strings.Join(lines, "\n"), false, nil
		},
	}}
}
