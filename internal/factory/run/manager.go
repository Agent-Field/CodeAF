package run

// The manager: the item's own conversation runs its run.
//
// AN ITEM'S CONVERSATION IS THE MANAGER OF ITS RUN (the owner's decision of
// 2026-10-08). It is the conversation `T` opens on the floor ([factory.Item.Talk]),
// and a launch makes it when the item has none ([Options.Manager]), named the
// lead of the item's team so the Teams place shows it above the stages. The
// runner does three things with it, and this file is all three:
//
//   - IT REPORTS. One line per event goes into the conversation as the
//     manager's own message ([Talk.Say]), in the order the events happened. The
//     lines are the table below and nowhere else, in the floor's words.
//   - IT LISTENS. What the person types there is read off the conversation's
//     own file ([Talk.Heard]) after the last thing the runner took
//     ([factory.Item.Heard]): before a run it becomes the item's notes, which
//     open every stage's brief; during a run it is the steer, folded in at each
//     stage's boundary and every round's start, and handed to a round that is
//     running by a small watch every [Options.TalkPoll].
//   - IT TAKES AN ANSWER. While the run waits on a question, a yes or a no
//     typed there, or any words when the question takes words, answers it
//     through the same door the floor's keys use ([Runner.Answer]), and the
//     manager's next line says so (`answered: yes`).
//
// THE ROAD IS THE FILE, NOT A TOOL, and that is deliberate. A door the
// conversation's model would have to call is a door it can forget, misread or
// call on words the person never meant for the run; the file holds exactly
// what the person typed, with the instant they typed it, and the runner reads
// it at moments it chooses. The model in the manager conversation still
// answers the person, and still changes the item through `factory_item`, whose
// card asks the person first.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE MANAGER'S LINES, every sentence the runner says into an item's
// conversation, in ONE table. The manual quotes them
// (internal/manual/chat/factory-stage-conversations.md); a line respelled here
// is respelled there in the same change.
const (
	sayStarted   = "%s started"                                 // plan started
	sayDone      = "%s done"                                    // plan done · 2m · $0.04 · what it said
	sayAsking    = "asking you: %s"                             // asking you: plan is ready · go, or change it?
	sayFailed    = "%s failed%s"                                // test failed 1 of 2, said before the question
	sayFailedAsk = "%s · asking you: %s"                        // test failed 1 of 2 · asking you: …
	sayCapAsk    = "budget of %s reached · asking you"          // budget of $5 reached · asking you
	sayAnswered  = "answered: %s"                               // answered: yes · answered: no · answered: <words>
	sayChanges   = "changes requested: %s"                      // the person's request changes
	sayLanded    = "landed · proof sheet ready · your approval" // every stage done
	sayShipped   = "shipped"                                    // approved, or shipped itself as a habit
	sayStopped   = "stopped"
	sayPaused    = "paused"
	sayResumed   = "resumed"
	saySteer     = "steer: %s"       // what the person typed during a run
	sayBack      = "sent back to %s" // sent back to plan: <the person's words>, from an approve step
	saySep       = " · "
)

// talkPollDefault is how often a running item reads its manager conversation
// for a steer or an answer when [Options.TalkPoll] is zero.
const talkPollDefault = 2 * time.Second

// summaryMost is the most characters a stage's summary carries on its done
// line; the stage's own conversation holds the rest.
const summaryMost = 400

// talkOwedMost is how many lines an item may owe its conversation (one held
// by another process, or mid-turn) before the oldest are let go.
const talkOwedMost = 200

// ErrTalkGone is a [Talk.Say] for a conversation that is not there any more:
// its lines are dropped rather than owed.
var ErrTalkGone = errors.New("the item's conversation is gone")

// Talk is the manager conversation's file, read and written. cmd/codeaf wires
// internal/session's doors ([session.AppendProgress], [session.PersonLines],
// [session.LastSaid]); this package imports none of the engine.
type Talk interface {
	// Say appends one line to the conversation as the manager's own message.
	// An error leaves the line owed: the runner says it again later, in order.
	// [ErrTalkGone] drops it.
	Say(transcript, line string) error
	// Heard is what the person typed into the conversation after the instant
	// after, oldest first.
	Heard(transcript string, after time.Time) ([]Heard, error)
	// LastSaid is the last sentence a conversation's model said, "" for none.
	LastSaid(transcript string) string
}

// Heard is one line the person typed into the manager conversation.
type Heard struct {
	At    time.Time
	Words string
}

// owedLine is a line said to a conversation that could not take it yet.
type owedLine struct {
	transcript string
	line       string
}

// tell says line into the item's manager conversation, after every line the
// item still owes it.
func (lp *floorLoop) tell(id int, line string) {
	talk := lp.r.opts.Talk
	if talk == nil || strings.TrimSpace(line) == "" {
		return
	}
	it, err := lp.r.opts.Store.Get(id)
	if err != nil || strings.TrimSpace(it.Talk) == "" {
		return
	}
	lp.sayMu.Lock()
	q := append(lp.owed[id], owedLine{transcript: it.Talk, line: line})
	if over := len(q) - talkOwedMost; over > 0 {
		q = q[over:]
	}
	lp.owed[id] = q
	lp.sayMu.Unlock()
	lp.flushTalk(id)
}

// flushTalk says what the item owes its conversation, oldest first, until a
// line is refused; a refused line is tried again after a poll.
func (lp *floorLoop) flushTalk(id int) {
	talk := lp.r.opts.Talk
	lp.sayMu.Lock()
	defer lp.sayMu.Unlock()
	for len(lp.owed[id]) > 0 {
		o := lp.owed[id][0]
		err := talk.Say(o.transcript, o.line)
		if errors.Is(err, ErrTalkGone) {
			delete(lp.owed, id)
			return
		}
		if err != nil {
			if !lp.retrying[id] {
				lp.retrying[id] = true
				time.AfterFunc(lp.talkPoll(), func() {
					lp.sayMu.Lock()
					lp.retrying[id] = false
					lp.sayMu.Unlock()
					lp.flushTalk(id)
				})
			}
			return
		}
		lp.owed[id] = lp.owed[id][1:]
	}
	delete(lp.owed, id)
}

func (lp *floorLoop) talkPoll() time.Duration {
	if d := lp.r.opts.TalkPoll; d > 0 {
		return d
	}
	return talkPollDefault
}

// manage makes the item's manager conversation at launch, or names the one it
// has the lead of its team, and answers it. "" is none: no maker, or one that
// failed, whose sentence goes on the item's log.
func (r *Runner) manage(it factory.Item) (string, string) {
	have := strings.TrimSpace(it.Talk)
	if r.opts.Manager == nil {
		return have, ""
	}
	chat, err := r.opts.Manager(context.Background(), it)
	if err != nil {
		return have, "the item's conversation could not be made: " + loopFirstLine(err.Error())
	}
	if chat = strings.TrimSpace(chat); chat == "" {
		return have, ""
	}
	return chat, ""
}

// brief is what the person typed into the manager conversation before this
// run, which the launch puts on the item's notes.
func (r *Runner) brief(transcript string, after time.Time) []Heard {
	if r.opts.Talk == nil || strings.TrimSpace(transcript) == "" {
		return nil
	}
	lines, _ := r.opts.Talk.Heard(transcript, after)
	return lines
}

// hear takes what the person typed into the manager conversation since the
// runner last read it: an answer while a question stands, a steer otherwise.
// AN ANSWER ENDS THE READING, so the lines after it wait for the next one and
// are read against the question (or none) that stands then.
func (lp *floorLoop) hear(c *loopCtl) {
	talk := lp.r.opts.Talk
	if talk == nil || c.reverify {
		return
	}
	c.hearMu.Lock()
	defer c.hearMu.Unlock()
	it, err := lp.r.opts.Store.Get(c.id)
	if err != nil || strings.TrimSpace(it.Talk) == "" {
		return
	}
	lines, err := talk.Heard(it.Talk, it.Heard)
	if err != nil {
		return
	}
	for _, h := range lines {
		if c.ctx.Err() != nil {
			return
		}
		lp.mu.Lock()
		pending := c.pending != ""
		lp.mu.Unlock()
		cur, _ := lp.r.opts.Store.Get(c.id)
		// A STEP'S QUESTION THE PERSON HOLDS is answered here too when the
		// round that asked it is gone (a pause): the answer is kept for the
		// step's next round (inbox.go).
		kept := !pending && cur.Asking != nil && cur.Asking.With == factory.AskedYou
		if pending || kept {
			if yes, words, ok := typedAnswer(h.Words, cur.QKind); ok {
				if lp.heard(c, h.At) != nil {
					return
				}
				_ = lp.r.Answer(c.id, yes, words)
				return
			}
		}
		if lp.heard(c, h.At) != nil {
			return
		}
		// THE WORDS ARE HANDLED ONCE: the person typed them to the manager,
		// whose own turn on them answers and reshapes the stages not yet
		// started with `factory_run` when that is what they meant. The runner
		// adds no second turn on the same words (the owner's run of
		// 2026-10-09 answered `how's the plan coming along?` twice).
		lp.steer(c, h.Words, lp.roundRunning(c))
	}
}

// steer takes words for the item's run: the next round's brief reads them in
// the notes, the item's log and its conversation say `steer: …`, and push
// hands them to the round running now as well.
func (lp *floorLoop) steer(c *loopCtl, words string, push bool) {
	if words = strings.TrimSpace(words); words == "" {
		return
	}
	if push {
		select {
		case c.steer <- words:
		default:
		}
	}
	lp.addNote(c.id, words)
	lp.say(c, "said", fmt.Sprintf(saySteer, words))
	lp.tell(c.id, fmt.Sprintf(saySteer, words))
}

// roundRunning says whether a round of the item is running now.
func (lp *floorLoop) roundRunning(c *loopCtl) bool {
	lp.mu.Lock()
	defer lp.mu.Unlock()
	return c.roundCancel != nil
}

// heard moves the item's mark past a line the runner took.
func (lp *floorLoop) heard(c *loopCtl, at time.Time) error {
	return lp.write(c, func(it *factory.Item) error {
		if at.After(it.Heard) {
			it.Heard = at
		}
		return nil
	})
}

// watchTalk reads the manager conversation every poll while the item holds
// its bench, so a steer reaches a round that is running and an answer reaches
// a question that waits.
func (lp *floorLoop) watchTalk(c *loopCtl, done <-chan struct{}) {
	t := time.NewTicker(lp.talkPoll())
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-c.ctx.Done():
			return
		case <-t.C:
			lp.hear(c)
		}
	}
}

// typedAnswer reads words typed into the manager conversation as an answer to
// a question of kind qkind: a yes or a no, or, for a question that takes words
// (a gate, a plan, a scope), the words themselves. ok is false for words that
// do not answer it, which are a steer instead.
func typedAnswer(words, qkind string) (yes bool, said string, ok bool) {
	words = strings.TrimSpace(words)
	fields := strings.Fields(strings.ToLower(words))
	if len(fields) == 0 {
		return false, "", false
	}
	first := strings.Trim(fields[0], ".,!?:;-")
	rest := strings.TrimSpace(strings.Trim(strings.Join(fields[1:], " "), ".,!?:;- "))
	var isYes, isNo bool
	switch first {
	case "yes", "y", "yeah", "yep", "go", "ok", "okay", "sure", "continue", "approve", "approved", "lgtm":
		isYes = true
	case "no", "n", "nope", "stop":
		isNo = true
	}
	// AN APPROVE STEP TAKES WORDS EITHER WAY: a yes with words goes on with
	// them as the person's note (`yes, keep the old flag`), and a no with words
	// sends the run back with them (`no, use the other file`). WORDS THAT OPEN
	// ON NEITHER ARE NOT AN ANSWER: the person is talking to the manager (`what
	// does the plan change?`), and the run keeps holding.
	if qkind == factory.QKindApprove {
		said := strings.TrimSpace(strings.Trim(strings.Join(strings.Fields(words)[1:], " "), ".,!?:;- "))
		switch {
		case isNo:
			return false, said, true
		case isYes:
			return true, said, true
		}
		return false, "", false
	}
	takesWords := false
	switch qkind {
	case "gate", "plan", "scope", qkindStep:
		takesWords = true
	}
	// A LINE THAT OPENS ON A YES IS A YES (`yes, go`): words after it are
	// not a change, because words on a plan run the plan again. A no with
	// words after it is a change (`no, use the other file`) where words are
	// taken, and a plain no where they are not.
	switch {
	case isYes:
		return true, "", true
	case isNo && rest == "":
		return false, "", true
	case takesWords:
		return false, words, true
	case isNo:
		return false, "", true
	}
	return false, "", false
}

// answeredWord is how an answer is said back: yes, no, or the words.
func answeredWord(a loopAnswer) string {
	switch {
	case a.words != "":
		return a.words
	case a.yes:
		return "yes"
	}
	return "no"
}

// askLine is the manager's line for a question: the failure that raised it,
// when one did, and the question.
func askLine(lead, question string) string {
	if lead = strings.TrimSpace(lead); lead != "" {
		return fmt.Sprintf(sayFailedAsk, lead, question)
	}
	return fmt.Sprintf(sayAsking, question)
}

// failedLead is a stage's failure as the manager says it before a question:
// `test failed 1 of 2`, `write failed`.
func failedLead(name, detail string) string {
	if detail = strings.TrimSpace(detail); detail != "" {
		detail = " " + detail
	}
	return fmt.Sprintf(sayFailed, name, detail)
}

// failCount is a round's shortfall in the fewest words: `1 of 2` for claims
// shown, else what [shortfall] says.
func failCount(res factory.StageResult) string {
	if res.Done && res.Findings == 0 && res.Exit == 0 && len(res.Claims) > 0 {
		shown := 0
		for _, cl := range res.Claims {
			if cl.OK && strings.TrimSpace(cl.Evidence) != "" {
				shown++
			}
		}
		return strconv.Itoa(shown) + " of " + strconv.Itoa(len(res.Claims))
	}
	return shortfall(res)
}

// doneLine is `plan done · 2m · $0.04 · what it said`, each part left out when
// there is nothing to say (the emptiness law).
func doneLine(name string, took time.Duration, spent float64, summary string) string {
	parts := []string{fmt.Sprintf(sayDone, name)}
	if w := tookWord(took); w != "" {
		parts = append(parts, w)
	}
	if spent >= 0.005 {
		parts = append(parts, "$"+strconv.FormatFloat(spent, 'f', 2, 64))
	}
	if summary = oneLine(summary); summary != "" {
		if r := []rune(summary); len(r) > summaryMost {
			summary = strings.TrimSpace(string(r[:summaryMost])) + "…"
		}
		parts = append(parts, summary)
	}
	return strings.Join(parts, saySep)
}

// tookWord is a stage's time as the floor says it: `40s`, `2m`, `1h 5m`.
func tookWord(d time.Duration) string {
	switch {
	case d < time.Second:
		return ""
	case d < time.Minute:
		return strconv.Itoa(int(d/time.Second)) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	}
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	if m == 0 {
		return strconv.Itoa(h) + "h"
	}
	return strconv.Itoa(h) + "h " + strconv.Itoa(m) + "m"
}

// summary is a stage's one paragraph: the notes it left, joined, else the last
// sentence its conversation said.
func (lp *floorLoop) summary(res factory.StageResult) string {
	var notes []string
	for _, n := range res.Notes {
		if n = oneLine(n); n != "" {
			notes = append(notes, n)
		}
	}
	if len(notes) > 0 {
		return strings.Join(notes, " ")
	}
	if talk := lp.r.opts.Talk; talk != nil && strings.TrimSpace(res.Chat) != "" {
		if s := talk.LastSaid(res.Chat); s != "" {
			return s
		}
	}
	if out := loopFirstLine(res.Output); out != "" && !strings.HasPrefix(out, stageNoReport) {
		return out
	}
	return ""
}
