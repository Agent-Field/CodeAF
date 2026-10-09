package run

// The inbox: every question a step asks goes to the manager first.
//
// THE MANAGER CONVERSATION IS THE ONE INBOX (the owner's decision of
// 2026-10-09). A step's conversation that calls `ask` does not reach the
// person: its question comes here ([Job.Ask], the stage door's Ask), and
//
//   - it is kept on the item ([factory.Item.Asking]) and said into the
//     manager conversation, `plan asks: which storage shape?`;
//   - the manager is given ONE turn on it ([Options.Inbox]) to answer from the
//     issue, the recipe and the stages. An answer goes back to the step at
//     once, and the manager conversation says `manager answered plan: …`;
//   - anything else (the manager sends it on, its turn fails or takes longer
//     than the shaping wait, there is no manager) sends the question to the
//     person: the manager conversation says `plan asks you: … · why`, the item
//     needs you with the question on it, and THE STEP WAITS. The person answers
//     in the manager conversation (any words: the question takes words) or with
//     the floor's keys ([Runner.Answer]), at any later time, and the step goes
//     on with the answer.
//
// AN ANSWER OUTLIVES THE ROUND THAT ASKED. A pause cuts the round and a
// restart ends the process, but the question the person holds stays on the
// item; an answer given then is kept on the item's notes, which every round's
// brief opens with ([floorLoop.keepAnswer]), so the step's next round reads it.
//
// One question at a time per item: a second waits for the first to be
// answered, so the manager is never given two turns at once.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// THE INBOX'S LINES, said on the item's log and into the manager
// conversation (the manual quotes them in internal/manual/chat/factory.md),
// and the words the step reads back.
const (
	sayStepAsks        = "%s asks: %s"             // plan asks: which storage shape?
	sayStepAsksYou     = "%s asks you: %s"         // plan asks you: which storage shape? · it is a taste call
	sayManagerAnswered = "manager answered %s: %s" // manager answered plan: sqlite, the recipe says so
	sayKept            = "noted for %s: %s"        // noted for plan: sqlite (an answer kept for the next round)

	answerFromManager = "the manager answered: %s"
	answerFromYou     = "the person answered: %s"
	noteAnswered      = "%s asked: %s · %s answered: %s" // plan asked: … · the manager answered: …

	// qkindStep is the QKind of a step's question the person holds. It takes
	// words, so any line typed into the manager conversation answers it.
	qkindStep = "step"
)

var errEmptyQuestion = errors.New("the question is empty")

// inbox puts one question of the step stage (phase i) to the item's manager,
// then to the person when the manager sends it on, and answers the answer in
// words for the step. It waits until there is one, or ctx (the round's) ends.
func (lp *floorLoop) inbox(ctx context.Context, c *loopCtl, i int, stage string, q factory.Asked) (string, error) {
	q.Stage = stage
	q.Question = oneLine(q.Question)
	if q.Question == "" {
		return "", errEmptyQuestion
	}
	q.Pick = oneLine(q.Pick)
	var opts []string
	for _, o := range q.Options {
		if o = oneLine(o); o != "" {
			opts = append(opts, o)
		}
	}
	q.Options = opts
	q.With, q.Why = factory.AskedManager, ""

	c.inboxMu.Lock()
	defer c.inboxMu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	now := lp.r.now()
	q.At = now
	asks := fmt.Sprintf(sayStepAsks, stage, q.Question)
	held := q
	if err := lp.write(c, func(it *factory.Item) error {
		it.Asking = &held
		loopSay(it, now, "ask", asks)
		return nil
	}); err != nil {
		return "", err
	}
	lp.tell(c.id, asks)

	reply := lp.managerAnswers(ctx, c, q)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if answer := oneLine(reply.Answer); answer != "" {
		line := fmt.Sprintf(sayManagerAnswered, stage, answer)
		if err := lp.write(c, func(it *factory.Item) error {
			it.Asking = nil
			loopSay(it, lp.r.now(), "said", line)
			return nil
		}); err != nil {
			return "", err
		}
		lp.addNote(c.id, fmt.Sprintf(noteAnswered, stage, q.Question, "the manager", answer))
		lp.tell(c.id, line)
		return fmt.Sprintf(answerFromManager, answer), nil
	}

	// THE MANAGER SENT IT ON: the person holds it, and the step waits.
	why := oneLine(reply.Why)
	if err := lp.write(c, func(it *factory.Item) error {
		if it.Asking != nil {
			it.Asking.With, it.Asking.Why = factory.AskedYou, why
		}
		return nil
	}); err != nil {
		return "", err
	}
	question := asks
	if len(q.Options) > 0 {
		question += saySep + strings.Join(q.Options, " / ")
	}
	said := fmt.Sprintf(sayStepAsksYou, stage, q.Question)
	if len(q.Options) > 0 {
		said += saySep + strings.Join(q.Options, " / ")
	}
	if why != "" {
		said += saySep + why
	}
	a, ok := lp.askIn(ctx, c, i, qkindStep, qkindStep, question, factory.PhaseWaiting, "", said)
	if !ok {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return "", errLoopStopped
	}
	word := answeredWord(a)
	_ = lp.write(c, func(it *factory.Item) error {
		it.Asking = nil
		return nil
	})
	lp.addNote(c.id, fmt.Sprintf(noteAnswered, stage, q.Question, "the person", word))
	return fmt.Sprintf(answerFromYou, word), nil
}

// managerAnswers is the manager's one turn on a step's question, under the
// round's ctx and the shaping wait. A turn that fails or does not answer in
// time is no answer, which sends the question to the person.
func (lp *floorLoop) managerAnswers(ctx context.Context, c *loopCtl, q factory.Asked) InboxReply {
	fn := lp.r.opts.Inbox
	if fn == nil {
		return InboxReply{}
	}
	it, err := lp.r.opts.Store.Get(c.id)
	if err != nil {
		return InboxReply{}
	}
	tctx, cancel := context.WithTimeout(ctx, lp.shapeWait())
	defer cancel()
	type answer struct {
		reply InboxReply
		err   error
	}
	got := make(chan answer, 1)
	guard.Go("factory/inbox-turn", func() {
		reply, err := fn(tctx, it, q)
		got <- answer{reply: reply, err: err}
	})
	select {
	case a := <-got:
		if a.err != nil {
			return InboxReply{}
		}
		return a.reply
	case <-tctx.Done():
		return InboxReply{}
	}
}

// letGoAsk takes a step's question off the item's control when the round
// that asked it ended (a pause). An answer [Runner.Answer] already took is on
// its way to the answers channel, and is kept for the step's next round.
func (lp *floorLoop) letGoAsk(c *loopCtl) {
	lp.mu.Lock()
	if c.pending != "" {
		c.pending = ""
		lp.mu.Unlock()
		return
	}
	lp.mu.Unlock()
	a := <-c.answers
	_ = lp.keepAnswer(c, c.id, a)
}

// keepAnswer is the person's answer to a step's question that no round is
// waiting on any more: it goes on the item's notes, which the step's next
// round reads, and the question comes off the item. c is the item's control,
// nil when no run holds it (after a restart).
func (lp *floorLoop) keepAnswer(c *loopCtl, id int, a loopAnswer) error {
	word := answeredWord(a)
	now := lp.r.now()
	err := lp.r.opts.Store.Update(id, func(it *factory.Item) error {
		q := it.Asking
		if q == nil {
			return fmt.Errorf("%s is not waiting on you", it.Ref())
		}
		note := fmt.Sprintf(noteAnswered, q.Stage, q.Question, "the person", word)
		it.Notes = append(it.Notes, note)
		it.Asking = nil
		if it.QKind == qkindStep {
			// THE ITEM WAITS ON NOTHING NOW, with or without a run holding
			// it (after a restart, none does): it is running again, paused
			// where a pause cut it, never `needs you` with no question.
			it.Question, it.QKind = "", ""
			if it.State == factory.StateNeedsYou {
				it.State = factory.StateRunning
			}
		}
		// THE STEP'S OWN CHAT HEARS THE ANSWER when it carries on in the
		// same conversation (a pause's resume reads no fresh brief): kept on
		// the step, which waits no more.
		if s := it.Stream; s != nil {
			if i := askingPhase(s, q.Stage); i >= 0 {
				ph := &s.Phases[i]
				if ph.State == factory.PhaseWaiting {
					ph.State = factory.PhaseRunning
				}
				ph.Carry = append(ph.Carry, note)
			}
		}
		loopSay(it, now, "said", fmt.Sprintf(sayKept, q.Stage, word))
		return nil
	})
	if err != nil {
		return err
	}
	lp.tell(id, fmt.Sprintf(sayAnswered, word))
	return nil
}

// askingPhase is the phase of the step named stage that asked: the current
// one when it bears the name, else the first of that name not finished; -1
// for none.
func askingPhase(s *factory.Stream, stage string) int {
	if s.Cur >= 0 && s.Cur < len(s.Phases) && s.Phases[s.Cur].Name == stage {
		return s.Cur
	}
	for i, ph := range s.Phases {
		if ph.Name == stage && ph.State != factory.PhaseDone && ph.State != factory.PhaseFailed {
			return i
		}
	}
	return -1
}

// dropStaleAsk takes a step's question off an item whose round is starting
// again: the round that asked it is gone, and the step asks again if it
// still needs to.
func dropStaleAsk(it *factory.Item) {
	if it.Asking == nil {
		return
	}
	it.Asking = nil
	if it.QKind == qkindStep {
		it.Question, it.QKind = "", ""
	}
}
