package head

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// Coalescing is the answer to the oldest complaint about this surface: you type
// a question, see the head take it, notice the word you got wrong, type the
// correction — and get two replies, the first one answering the question you
// had already withdrawn. It happened by construction. The head answered rows one
// at a time in journal order, and the prompt window for a turn hard-stops at the
// row being answered, so the correction was invisible to the turn it corrected.
//
// The fix is the semantics the worker steering mailbox has always had: drain
// what the person has said into one turn and answer it once. Two rules bound it.
// Freshness, because a sentence typed an hour later is a new conversation rather
// than a second half; and a row ceiling, because somebody typing continuously
// must still be answered.
//
// It does not fold distinct asks away. Five things said in one breath still
// produce five pieces of work — the router's own fan-out reads them out of the
// folded words — but they produce one reply, which is what a person who said
// five things in one breath is waiting for.
const (
	// foldQuiet is how long a turn stays open to the next thing the person
	// says. The adjacency window measures the same kind of freshness against a
	// job's heartbeat and is four minutes wide for it; a person correcting
	// themselves is far quicker than that — they are still looking at what they
	// typed — and a minute is generous for noticing and retyping while staying
	// nowhere near the span in which a thread has moved on.
	foldQuiet = time.Minute
	// foldLimit is how many of the person's rows one turn may carry. Without a
	// ceiling, somebody typing steadily could refold the turn ahead of every
	// answer forever; with it, the fifth thing they say is simply the next turn.
	foldLimit = 4
)

// errRefold is not a failure. It is a turn withdrawing itself: the person said
// something else while the routing call was out, nothing has been said back to
// them yet, and the turn is asked again carrying both halves.
var errRefold = errors.New("serve head: turn refolded")

// foldedTurn is one turn's worth of the person's words: the row that opened it,
// the rows folded into it, and the single message the whole turn is answered
// from. The message keeps the opening row's seq so the thread window the router
// reads still stops before the first of these rows — otherwise every folded body
// would appear twice in one prompt, once as history and once as the ask.
type foldedTurn struct {
	message store.Message
	// last is the newest row folded in. It is what the cursor may advance to and
	// what a reply stamps as the span it answers.
	last   int64
	rows   int
	latest time.Time
}

func newFoldedTurn(opening store.Message) *foldedTurn {
	return &foldedTurn{message: opening, last: opening.Seq, rows: 1, latest: opening.Time}
}

// absorbs is the whole freshness rule, asked against the last row folded in
// rather than the first: somebody typing three corrections in a row is one
// thought, and measuring from the opening row would cut it off mid-way.
func (fold *foldedTurn) absorbs(message store.Message) bool {
	if fold == nil || fold.rows >= foldLimit || message.Seq <= fold.last {
		return false
	}
	if fold.latest.IsZero() || message.Time.IsZero() {
		return true
	}
	return message.Time.Sub(fold.latest) <= foldQuiet
}

// absorb folds one row in. The bodies become consecutive lines in the order they
// were typed, because that is what they are: the model must read the correction
// as the person's own next sentence, not as a note about their last one.
func (fold *foldedTurn) absorb(message store.Message) {
	body := strings.TrimSpace(message.Body)
	if body != "" {
		fold.message.Body = strings.TrimRight(fold.message.Body, "\n") + "\n" + body
	}
	if len(message.Attachments) > 0 {
		// A copy, because the opening row's slice belongs to the store's read.
		attachments := make([]string, 0, len(fold.message.Attachments)+len(message.Attachments))
		attachments = append(attachments, fold.message.Attachments...)
		fold.message.Attachments = append(attachments, message.Attachments...)
	}
	// A model asked for mid-turn is asked for the answer that has not happened
	// yet, so the newest request wins.
	if model := strings.TrimSpace(message.Model); model != "" {
		fold.message.Model = model
	}
	fold.last = message.Seq
	fold.latest = message.Time
	fold.rows++
}

// answerable is the head's own mail: the person speaking to the conversation
// rather than to a worker. A user message anchored to a node is mid-flight
// steering the executor consumes between turns.
func answerable(message store.Message) bool {
	return message.Role == store.RoleUser && strings.TrimSpace(message.NodeID) == ""
}

// foldable narrows that to the rows a turn may open or carry. An answer to a
// numbered question is a selection against a durable question rather than a
// sentence, and folding it into its neighbour would answer one question with the
// other's words.
func foldable(message store.Message) bool {
	return answerable(message) && message.QuestionSeq == 0
}

// foldStep is what one row after the opening does to the turn. The third answer
// is the one that matters: a turn may only ever consume a contiguous run, because
// the cursor advances to the newest row it folded and every row it passed is
// marked handled. Anything the turn cannot answer has to stop it rather than be
// stepped over, or that row goes silent.
type foldStep int

const (
	foldSkip foldStep = iota
	foldTake
	foldStop
)

func (fold *foldedTurn) step(message store.Message) foldStep {
	// Nobody typed it, or they typed it at a worker: neither is this turn's to
	// answer, and neither is anything the poll would have answered either.
	if !answerable(message) {
		return foldSkip
	}
	// Another window's turn. It is the person's too, and it is owed its own
	// answer in its own thread.
	if message.SessionID != fold.message.SessionID {
		return foldStop
	}
	if message.QuestionSeq != 0 || !fold.absorbs(message) {
		return foldStop
	}
	return foldTake
}

// foldAhead reads one turn out of the page the poll already has. Lines nobody
// typed do not close a turn — a job narrating between two of the person's
// sentences does not make them two conversations.
func foldAhead(page []store.Message) *foldedTurn {
	fold := newFoldedTurn(page[0])
	if !foldable(page[0]) {
		return fold
	}
	for _, message := range page[1:] {
		switch fold.step(message) {
		case foldSkip:
			continue
		case foldTake:
			fold.absorb(message)
		default:
			return fold
		}
	}
	return fold
}

// watchForFold is the head noticing, mid-turn, that the person has said
// something else.
//
// The poll loop only looks at the journal between turns, so without this the
// first turn runs blind to the correction and the correction is answered
// afterwards — the exact two-replies-for-one-thought failure folding exists to
// end. It watches only around the routing call, and that placement is the whole
// safety argument: everything that acts on the graph has either already run and
// spoken, or has not started. A turn withdrawn here has done nothing to undo.
func (h *Head) watchForFold(ctx context.Context) (context.Context, func()) {
	if h == nil || h.store == nil {
		return ctx, func() {}
	}
	h.turnMu.Lock()
	fold := h.turnFold
	h.turnMu.Unlock()
	if fold == nil || !foldable(fold.message) {
		return ctx, func() {}
	}
	watched, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-watched.Done():
				return
			case <-ticker.C:
			}
			if h.absorbArrivals() {
				cancel()
				return
			}
		}
	}()
	return watched, func() {
		cancel()
		<-done
	}
}

// absorbArrivals folds whatever the person has said since this turn was made
// into the turn itself, and reports whether anything landed. The row ceiling is
// checked here as well as in absorbs, so a turn that has already refolded its
// limit stops reading the journal rather than stopping at the fold.
func (h *Head) absorbArrivals() bool {
	h.turnMu.Lock()
	fold := h.turnFold
	h.turnMu.Unlock()
	if fold == nil || fold.rows >= foldLimit {
		return false
	}
	// Every session, not this one, for the same reason foldAhead reads the whole
	// page: the cursor the poll advances is one journal-wide watermark, and a
	// turn that jumped another window's row would take that row's answer with it.
	messages, err := h.store.Messages("", fold.last, messagePageSize)
	if err != nil || len(messages) == 0 {
		return false
	}
	h.turnMu.Lock()
	defer h.turnMu.Unlock()
	if h.turnFold != fold {
		return false
	}
	absorbed := false
	for _, message := range messages {
		step := fold.step(message)
		if step == foldStop {
			break
		}
		if step == foldTake {
			fold.absorb(message)
			absorbed = true
		}
	}
	if absorbed {
		h.turnRefold = true
	}
	return absorbed
}

// refolding reports whether the turn in flight has been overtaken by the
// person's own next sentence.
func (h *Head) refolding() bool {
	if h == nil {
		return false
	}
	h.turnMu.Lock()
	defer h.turnMu.Unlock()
	return h.turnRefold
}

// answering is the span whatever the head says next covers: the newest row the
// turn in flight was given. Every line the head posts carries it, because a
// surface counting the turns it is still owed cannot otherwise tell one reply
// that settled three of them from one reply that settled one.
func (h *Head) answering() int64 {
	if h == nil {
		return 0
	}
	h.turnMu.Lock()
	defer h.turnMu.Unlock()
	return h.turnAnswers
}
