package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
)

// ── THE MANAGER IS THE INBOX ────────────────────────────────────────────────
//
// Every question a step of a run asks goes to the item's manager first
// (internal/factory/run's inbox.go). The runner gives the manager ONE turn of
// its own conversation with the question ([shapeTurns.Inbox], wired as
// Options.Inbox), by the same road a shaping turn takes: through the window's
// conversation when this process holds it, else opened for the turn. On that
// turn `factory_answer` answers through [inboxDoor], which keeps the reply for
// the runner: an answer goes back to the step, a reason sends the question to
// the person. A turn in which the manager calls neither sends it to the
// person too, because a question nobody answered is the person's.

// The runner's ask on an inbox turn, said once so the manual and the tests
// quote the same words.
const (
	inboxAsk      = "%s asks: %s"
	inboxOptions  = " · its answers: %s"
	inboxPick     = " · it would take: %s"
	inboxAskTail  = " · Reply with factory_answer: answer it yourself when the issue, the recipe, the stages or what the person said here settle it; ask_person with one line of why when it turns on taste, scope, risk, credentials or access, money, or anything they do not settle."
	inboxAnswered = "answered · %s goes on with it"
	inboxSentOn   = "sent to the person · %s waits for their answer"
	inboxOnlyOnce = "you already replied to %s's question on this turn"
	inboxNoStages = "this turn is for %s's question: the stages are not changed on it"
)

// inboxAskFor is the inbox turn's ask for one question.
func inboxAskFor(q factory.Asked) string {
	ask := fmt.Sprintf(inboxAsk, q.Stage, oneLineWords(q.Question))
	if len(q.Options) > 0 {
		ask += fmt.Sprintf(inboxOptions, strings.Join(q.Options, " / "))
	}
	if pick := oneLineWords(q.Pick); pick != "" {
		ask += fmt.Sprintf(inboxPick, pick)
	}
	return ask + inboxAskTail
}

// oneLineWords is s with its whitespace folded to single spaces.
func oneLineWords(s string) string { return strings.Join(strings.Fields(s), " ") }

// Inbox is the runner's Options.Inbox: the manager's one turn on a step's
// question.
func (s *shapeTurns) Inbox(ctx context.Context, it factory.Item, q factory.Asked) (factoryrun.InboxReply, error) {
	door := &inboxDoor{stage: q.Stage}
	if _, err := s.through(ctx, it, inboxAskFor(q), door); err != nil {
		return factoryrun.InboxReply{}, err
	}
	return door.reply(), nil
}

// inboxDoor is `factory_answer` (and `factory_run`, refused) on an inbox
// turn: it keeps the manager's first reply and refuses a second.
type inboxDoor struct {
	stage string

	mu      sync.Mutex
	got     factoryrun.InboxReply
	replied bool
}

// EditRun refuses: this turn answers a question, it does not shape the run.
func (d *inboxDoor) EditRun(context.Context, string, factory.RunEdit) (factory.Item, []string, error) {
	return factory.Item{}, nil, fmt.Errorf(inboxNoStages, d.stage)
}

// AnswerStep keeps the manager's answer.
func (d *inboxDoor) AnswerStep(_ context.Context, _ string, answer string) (string, error) {
	if err := d.keep(factoryrun.InboxReply{Answer: answer}); err != nil {
		return "", err
	}
	return fmt.Sprintf(inboxAnswered, d.stage), nil
}

// SendOn keeps the manager's reason for sending the question to the person.
func (d *inboxDoor) SendOn(_ context.Context, _ string, why string) (string, error) {
	if err := d.keep(factoryrun.InboxReply{Why: why}); err != nil {
		return "", err
	}
	return fmt.Sprintf(inboxSentOn, d.stage), nil
}

func (d *inboxDoor) keep(r factoryrun.InboxReply) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.replied {
		return fmt.Errorf(inboxOnlyOnce, d.stage)
	}
	d.got, d.replied = r, true
	return nil
}

// reply is what the manager replied, the zero reply (to the person) for none.
func (d *inboxDoor) reply() factoryrun.InboxReply {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.got
}
