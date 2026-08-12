// Package head turns durable thread messages into immediate conversational
// replies and asynchronous graph commands. It never plans or executes work;
// the thread remains responsive while the rest of Aforge changes the graph.
package head

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/cas"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	pollInterval    = 400 * time.Millisecond
	messagePageSize = 200
	// maxGraphContextBytes rose by a kilobyte when the board learned structure.
	// Two clauses were added to it — what a row is waiting on, and how long live
	// work has been going — and both land on exactly the rows a status question
	// is about. Paying for them out of the old budget would have bought the
	// answer by truncating the history the same prompt answers "what did you do
	// yesterday" from; a kilobyte is the smaller price, and the live-work-first
	// ordering still decides what the ceiling drops.
	maxGraphContextBytes  = 5 << 10
	maxThreadContextBytes = 8 << 10
	// The truncation markers are part of what gets sent, so they are part of
	// what the budget covers. Written after the check, they put the block over
	// its ceiling in exactly the case the ceiling exists for; reserving their
	// bytes up front makes the budget the real bound it claims to be.
	snapshotTruncatedMark = "(snapshot truncated)\n"
	threadTruncatedMark   = "(thread context truncated)\n"
	providerErrorReply    = "hit a provider error answering that — try again"
	commandErrorReply     = "I couldn't queue that change — try again"
	// unclearCommandReply is what the head says when it meant to change
	// something and cannot say what. It replaces a reply that was already
	// worded as if the change had happened, so it has to do that reply's whole
	// job: say plainly that nothing was done, and name what would settle it.
	unclearCommandReply = "I couldn't tell what you wanted changed there — say which one you mean and I'll do it."
	// noSuchTargetReply is the same honesty when the description was clear and
	// matched nothing. There is no third option here: either it acts, or it
	// asks, or it says this.
	noSuchTargetReply = "I don't see any work or standing rule like that."
	// manualRouteSections is the router's grounding read. It is smaller than
	// the belt's because the router carries the snapshot, the notebook and the
	// thread in the same prompt, and the manual must not crowd them out.
	manualRouteSections = 3
	// A stopped turn is still a turn, and the floor under every route holds for
	// it too: a message the user cut off may not end in nothing on screen. The
	// tail marks the words that did arrive as the piece of an answer they are;
	// the standalone line is for a turn stopped before it had any.
	interruptedTail  = "\n\n— interrupted"
	interruptedReply = "— interrupted before I had anything to say"
)

// The router prompt stood here — a second system prompt restating, in its own
// vocabulary, law the belt prompt already carried, above a decision object that
// could emit exactly one command as the terminal act of a turn. Both are gone.
// prompt.go holds the one prompt; loop.go holds the one loop.

// Client is the one provider operation the conversational components need.
// Keeping the boundary this small makes both routing and compiling testable
// without a network.
type Client interface {
	CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error)
}

// Head tails the durable thread and turns each new user message into one fast
// routing call, one reply, and at most one asynchronous command.
type Head struct {
	client         Client
	messageClient  func(store.Message) (Client, error)
	store          *store.Store
	knowledge      func() string
	competence     func() string
	standingWatch  func() string
	dailyBudgetUSD float64
	modalities     interface {
		Supports(string, string, string) bool
	}
	defaultModel string
	dailyRailSet bool
	// foldMu guards fold. The head answers one message at a time, so this is
	// never contended in the running product; it is here because a cache is a
	// piece of shared state and shared state that is only accidentally
	// single-threaded is the kind that stops being so without anyone noticing.
	foldMu sync.Mutex
	fold   *threadFold
	// turnMu guards the in-flight turn's cancellation. The head answers on its
	// own goroutine and the surface that stops a turn runs on another, both
	// inside one process: this is the entire seam between them, and it is a
	// handle rather than a journal row because a message the user has already
	// given up on must not wait on the store to be given up.
	turnMu      sync.Mutex
	turnCancel  context.CancelFunc
	turnPartial string
	turnStopped bool
	// turnFold is the run of the person's words this turn answers, and it keeps
	// growing while the turn runs: the mid-turn watch folds arrivals into it and
	// raises turnRefold, which withdraws the routing call. turnAnswers is the
	// span the turn was actually given, stamped on everything the head says.
	turnFold    *foldedTurn
	turnRefold  bool
	turnAnswers int64
	// workspace is where the artifact door writes. Empty means the process's
	// working directory, which is what a person typing into a terminal means by
	// "put it on disk"; write.go argues the default and the override.
	workspace string
	// actWindow overrides how long one act may take. Zero means actWindow, which
	// is what the running product uses; act.go argues the ceiling and why the
	// override exists at all.
	actWindow time.Duration
	// wrote is every artifact this head has written, so it can open one again to
	// repair it. It shares turnMu because it is small, rarely touched, and the
	// alternative is a second lock guarding one map. write.go records the
	// shortfall: the durable form is a message part, which is Wave 2's.
	wrote map[string]bool
	// namesRooms is the room-naming clerk, off unless a window asks for it. See
	// WithRoomNaming.
	namesRooms bool
	// scribing is the rooms whose naming pass is in flight, and scribeMu guards
	// it. It has a lock of its own rather than sharing turnMu because it is held
	// across a provider call that runs AFTER a turn — the one piece of the
	// head's state that is deliberately not the turn's (scribe.go).
	scribeMu sync.Mutex
	scribing map[string]bool
	// absorbed is the jobs already spoken back to the person, and absorbMu
	// guards it. It is its own lock for the scribe's reason — the claim is taken
	// before a provider call and held across it — and it is keyed by job rather
	// than by row because a settled job can put several rows of its own into a
	// room and every one of them reads as a delivery (absorb.go).
	absorbedMu sync.Mutex
	absorbed   map[string]bool
	// awaitingReceipt is every command a head turn journaled and has not yet
	// heard back about, and receiptMu guards it. It is registered as the command
	// is journaled and claimed when its receipt lands, which makes one map answer
	// both of the wake's questions — did a head turn start this, and has anything
	// been said about it already (wake.go).
	receiptMu       sync.Mutex
	awaitingReceipt map[int64]bool
}

// WithRoomNaming turns on the post-turn clerk that names rooms (scribe.go).
//
// It is a switch rather than always-on because it costs a provider call the
// TURN did not ask for, and only one kind of window is buying anything with it:
// one that draws a list of rooms. A headless errand has no rail, one room, and
// nobody to read a name — so the default is off and the chat window says so out
// loud, which also keeps every measurement of a turn's cost a measurement of
// the turn.
func (h *Head) WithRoomNaming(enabled bool) *Head {
	h.namesRooms = enabled
	return h
}

// WithImageInput lets the routing head receive durable chat attachments as
// OpenAI-style image parts when its current talk model advertises vision.
func (h *Head) WithImageInput(modalities interface {
	Supports(string, string, string) bool
}, defaultModel string) *Head {
	h.modalities = modalities
	h.defaultModel = defaultModel
	return h
}

// New returns a conversational head backed by graphStore.
func New(client Client, graphStore *store.Store) *Head {
	return &Head{client: client, store: graphStore}
}

// WithMessageClient selects a conversational client for one durable user
// message. Messages without an override continue through the Head's ordinary
// client; the callback is the single seam used by heavier chat lanes.
func (h *Head) WithMessageClient(selectClient func(store.Message) (Client, error)) *Head {
	h.messageClient = selectClient
	return h
}

// WithSelfKnowledge supplies measured execution history to the routing call.
// Nil and empty values preserve the original prompt exactly.
func (h *Head) WithSelfKnowledge(knowledge func() string) *Head {
	h.knowledge = knowledge
	return h
}

// WithCompetenceMap registers the derived capability view with the head's
// grounding path. It is read only for competence-shaped questions, and its
// structured data is voiced by the head's existing single routing call.
func (h *Head) WithCompetenceMap(competence func() string) *Head {
	h.competence = competence
	return h
}

// WithStandingWatch registers the same calm status block used by doctor. It
// is read only for presence-shaped questions.
func (h *Head) WithStandingWatch(status func() string) *Head {
	h.standingWatch = status
	return h
}

// WithDailyBudgetUSD lets the head render policy state and consume a pending
// rail question deterministically. Zero is unlimited.
func (h *Head) WithDailyBudgetUSD(amount float64) *Head {
	h.dailyBudgetUSD = amount
	h.dailyRailSet = true
	return h
}

// Serve tails every session until ctx is cancelled.
//
// On startup it resumes each session at that session's last non-user message.
// This intentionally replays only a trailing run of user messages: history
// ending in an agent or system message is treated as answered, while a crash
// after the user wrote but before the head replied remains recoverable. It is a
// deliberately simple journal rule; the cursor advances only after each message
// has been handled. The rule is asked per room because one number cannot state
// it for two — a reply in either would carry it past the other's unanswered
// rows, and those rows would never be read.
func (h *Head) Serve(ctx context.Context) error {
	if h == nil || h.client == nil {
		return errors.New("serve head: nil client")
	}
	if h.store == nil {
		return errors.New("serve head: nil store")
	}

	cursors, err := h.initialCursors()
	if err != nil {
		return fmt.Errorf("serve head: initialize cursor: %w", err)
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Every thread message is journalled, so an unmoved journal watermark is
	// proof that no message arrived — the same quiet path the lens already
	// takes. The watermark is read before the poll and only recorded after it
	// succeeds: anything committed while the poll was reading sits above the
	// recorded mark, so the next tick reads again rather than sleeping through
	// it. The cost of that ordering is one redundant poll after each burst,
	// which is what the head does all day anyway.
	quiet := int64(-1)
	for {
		watermark, watermarkErr := h.store.LatestEventSeq()
		if watermarkErr != nil || watermark != quiet {
			if err := h.poll(ctx, cursors); err != nil {
				return err
			}
			quiet = -1
			if watermarkErr == nil {
				quiet = watermark
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// initialCursors reads every room's resume point in one query. One room is the
// ordinary case and reads exactly as the single watermark did; the rest is what
// keeps a second room from being answered on the first one's word.
func (h *Head) initialCursors() (*sessionCursors, error) {
	answered, err := h.store.SessionMessageCursors()
	if err != nil {
		return nil, err
	}
	return resumeCursors(answered), nil
}

func (h *Head) poll(ctx context.Context, cursors *sessionCursors) error {
	if cursors == nil {
		cursors = newSessionCursors(0)
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		// One read across every room, because the journal is one sequence: the
		// page is split by room only when deciding what is owed.
		messages, err := h.store.Messages("", cursors.scanned, messagePageSize)
		if err != nil {
			return fmt.Errorf("serve head: tail messages: %w", err)
		}
		if len(messages) == 0 {
			return nil
		}
		for index, message := range messages {
			// Walking past a row is what moves the read position, whatever the
			// row turns out to be. It is recorded before anything is decided
			// about the row, because the page can begin below rooms that are
			// long since answered: a page made entirely of their rows would
			// otherwise leave the poll exactly where it started and be read
			// again forever.
			cursors.read(message.Seq)
			// Rows the turn before this one folded into itself are answered
			// already; that room's watermark is what says so.
			if cursors.handled(message) {
				continue
			}
			// Work the person asked for has landed. One short turn says what it
			// means for what they asked, in the head's own voice, beside the
			// card the delivery already drew (absorb.go). It runs inline rather
			// than beside the poll on purpose: this sentence answers the row
			// above it, and a turn racing the person's next message could land
			// under their reply to it.
			if deliveredRow(message) {
				h.absorbDeliveryBounded(ctx, message)
			}
			// A change the head put in hand has settled. One short turn says
			// what the workforce actually made of it — or takes back what the
			// head said, when the change was refused (wake.go). It runs inline
			// beside the delivery wake and for the same reason: this sentence
			// is about the row above it.
			if receiptRow(message) {
				h.wakeReceiptBounded(ctx, message)
			}
			if answerable(message) {
				answered, err := h.answerTurn(ctx, foldAhead(messages[index:]))
				if err != nil {
					return err
				}
				// The turn's own room, and only it: a fold stops at the first
				// row from anywhere else, so every row this answer covers was
				// typed here.
				cursors.mark(message.SessionID, answered)
				// The turn has settled, so the room may now be nameable. This is
				// the post-turn lane and it is behind the reply on purpose: the
				// scribe's call is the head's own business, and nothing the
				// person is waiting for may wait on it (scribe.go).
				h.nameRoomLater(ctx, message.SessionID)
			}
			cursors.mark(message.SessionID, message.Seq)
		}
	}
}

// answerTurn answers one turn — one folded run of the person's words — under a
// context of its own. The head's own context outlives every turn — it is the
// process — so a turn nobody wants any more had no way to end before this: the
// provider call ran to completion and the reply landed in a conversation that
// had moved on.
//
// It returns the newest row the turn actually answered, which is what the cursor
// may advance to. That is deliberately not the newest row folded in: a turn that
// finished before its refold could take effect has absorbed rows it never
// answered, and reporting those as handled is how a message goes silent.
//
// The cursor advances whether the turn finished or was stopped, because a
// stopped turn is handled: re-answering the message the user gave up on is the
// one thing an interrupt may not lead to.
func (h *Head) answerTurn(ctx context.Context, fold *foldedTurn) (int64, error) {
	for {
		turnContext, cancel := context.WithCancel(ctx)
		// Stamped once, from the fold rather than from `user` below, because it
		// names the room this turn is answering for and every event a provider
		// call inside it emits belongs to that room — the same fold field
		// `beginTurn` is about to hand back as `user.SessionID`.
		turnContext = provider.WithStreamSession(turnContext, fold.message.SessionID)
		user, answered := h.beginTurn(cancel, fold)
		err := h.answer(turnContext, user)
		cancel()
		partial, stopped, refolded := h.endTurn()
		// The person corrected themselves while the routing call was out. The
		// turn withdrew before saying anything, so it is asked again carrying
		// both halves of what they said.
		if refolded && errors.Is(err, errRefold) && ctx.Err() == nil {
			continue
		}
		if !stopped {
			return answered, err
		}
		// The interrupt raced the answer and lost. The turn already ended in
		// words, and a second line about it would be the thread talking to
		// itself.
		if err == nil {
			return answered, nil
		}
		// The head itself is going away, and the thread with it. Nothing to say.
		if ctx.Err() != nil {
			return answered, ctx.Err()
		}
		return answered, h.postInterrupted(user.SessionID, partial)
	}
}

// Interrupt stops the turn being answered right now and carries in whatever of
// it the reader had already seen. It reports whether there was a turn to stop,
// so a surface that asked at the wrong moment can tell that nothing happened.
func (h *Head) Interrupt(partial string) bool {
	if h == nil {
		return false
	}
	h.turnMu.Lock()
	cancel := h.turnCancel
	if cancel == nil {
		h.turnMu.Unlock()
		return false
	}
	h.turnPartial = strings.TrimSpace(partial)
	h.turnStopped = true
	h.turnMu.Unlock()
	cancel()
	return true
}

// beginTurn arms the turn and hands back the two things it is answered under:
// the folded message as it stands right now, and the span that message covers.
// Both are snapshots — the fold itself keeps growing under the mid-turn watch.
func (h *Head) beginTurn(cancel context.CancelFunc, fold *foldedTurn) (store.Message, int64) {
	h.turnMu.Lock()
	defer h.turnMu.Unlock()
	h.turnCancel = cancel
	h.turnPartial = ""
	h.turnStopped = false
	h.turnRefold = false
	h.turnFold = fold
	h.turnAnswers = fold.last
	return fold.message, fold.last
}

func (h *Head) endTurn() (string, bool, bool) {
	h.turnMu.Lock()
	defer h.turnMu.Unlock()
	partial, stopped, refolded := h.turnPartial, h.turnStopped, h.turnRefold
	h.turnCancel = nil
	h.turnPartial = ""
	h.turnStopped = false
	h.turnRefold = false
	h.turnFold = nil
	// turnAnswers outlives the turn on purpose. The interrupted line is posted
	// after the turn has ended and is still that turn's answer; anything else
	// the head says next is later than every row already settled by it.
	return partial, stopped, refolded
}

// postInterrupted is postAgentFloor's law applied to the one route that never
// reaches it: the words the user stopped. What they saw on screen is what the
// thread keeps, marked where it stopped, so the transcript reads as the
// conversation it was rather than as a gap.
// The mark is the same fact interruptedTail states in prose, said in a form a
// renderer can act on rather than scan for. Both travel: the body is what every
// surface already draws, the part is what the next one will.
func (h *Head) postInterrupted(sessionID, partial string) error {
	body := interruptedReply
	if partial != "" {
		body = partial + interruptedTail
	}
	return h.postAgentFloor(sessionID, body, 0, "",
		[]store.MessagePart{store.EndedMark(*store.InterruptedEnd())})
}

// answer is the whole of what one turn does now.
//
// Three things stand in front of the loop and nothing else does. All three are
// answers to a durable question this head itself posted — a worker's open
// question, a numbered choice, a daily-rail approval — and none of them is a cue
// recognizer. They are the ANSWER side of the consent gates, which is why they
// keep their place exactly: a "yes" typed against a confirm question is not a
// sentence to be reasoned about, it is the settlement of a gate that is already
// open, and routing it through a model would let the model reword what the
// person consented to. 4.1 expands authority without weakening the gates, and
// this is where that promise is kept.
//
// Everything below them — the ten recognizers and the router they sat on — is
// gone. What was a ladder of terminal readings is one agentic loop with those
// readings demoted to evidence (hints.go) and their machinery demoted to tool
// bodies (toolbelt.go). A message that trips no cue no longer misses the tools;
// there is nothing left for it to miss.
func (h *Head) answer(ctx context.Context, user store.Message) error {
	if handled, err := h.answerAgentQuestion(ctx, user); err != nil {
		return fmt.Errorf("serve head: answer agent question: %w", err)
	} else if handled {
		return nil
	}
	if handled, err := h.answerPendingQuestion(ctx, user); err != nil {
		return fmt.Errorf("serve head: answer selectable question: %w", err)
	} else if handled {
		return nil
	}
	if raised, err := h.raiseRailFromReply(user); err != nil {
		return fmt.Errorf("serve head: raise daily rail: %w", err)
	} else if raised {
		return nil
	}
	err := h.runTurn(ctx, user)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errRefold):
		// The turn withdrew itself; nothing has been said and nothing is owed
		// here. answerTurn asks it again with everything the person has said.
		return err
	case ctx.Err() != nil:
		return ctx.Err()
	default:
		// A turn that could not be assembled at all still owes a sentence. Going
		// quiet is the one outcome no route may produce.
		return h.postAgent(user.SessionID, providerErrorReply, 0)
	}
}

// postAgentFloor is the seam a route may not go quiet through. A reasoning model
// that spends its whole window deliberating returns zero words, and posting that
// empty string is silence wearing a message id — the user watches nothing
// happen, twice, and concludes the whole thing is broken. An honest "try again"
// is the floor under every message that reaches here.
//
// Every route that acts on the graph ends in this call. That is the invariant,
// not a convention: a redirection that journals a command and returns is the
// same silence arriving by a different door.
// parts rides through untouched. A message that has nothing structured to say
// passes nil and is indistinguishable from the message this function posted
// before parts existed.
func (h *Head) postAgentFloor(sessionID, body string, commandSeq int64, model string, parts []store.MessagePart) error {
	if strings.TrimSpace(body) == "" {
		// The floor's own words are the head's, not the model's, and they are
		// complete. Whatever marks the withdrawn turn earned describe a reply
		// that is not being posted, so they do not travel with this one.
		return h.postAgent(sessionID, providerErrorReply, commandSeq)
	}
	return h.postAgentModel(sessionID, body, commandSeq, model, parts)
}

// remember writes one durable belief a decision asked to keep. It is the single
// capture seam into the notebook: the router reaches it with what a message
// stated, and the revision route reaches it with what a redirection taught, so a
// lesson lands the same way whichever door the sentence came through. Kind is
// coerced rather than rejected — a belief filed under the wrong heading is still
// a belief, and dropping it loses the only copy.
func (h *Head) remember(memory *routeMemory) bool {
	if h == nil || h.store == nil || memory == nil {
		return false
	}
	body := strings.TrimSpace(memory.Body)
	if body == "" {
		return false
	}
	scope := strings.TrimSpace(strings.ToLower(memory.Scope))
	if scope == "" {
		scope = "user"
	}
	kind := store.FactKind(strings.ToLower(strings.TrimSpace(memory.Kind)))
	switch kind {
	case store.FactPreference, store.FactQuirk, store.FactLesson, store.FactPlain:
	default:
		kind = store.FactPreference
	}
	recorded, err := h.store.RecordFactFrom(store.FactWriterHead, store.RootID, scope, kind, body)
	if err != nil {
		return false
	}
	supersedeBelief(h.store, memory.Replaces, recorded)
	return true
}

// supersedeBelief retires the line a capture makes untrue. The judgment of
// which line that is belongs to the model; everything checkable is checked
// here, because a mis-aimed supersession is the one memory operation that
// destroys a belief nobody asked to lose: the target must exist, be active,
// and not be the row we just wrote.
func supersedeBelief(graphStore *store.Store, replaces int64, recorded store.Fact) {
	if graphStore == nil || replaces <= 0 || replaces == recorded.Seq || recorded.Seq <= 0 {
		return
	}
	prior, found, err := graphStore.FactBySeq(replaces)
	if err != nil || !found || prior.Status != store.FactActive {
		return
	}
	_ = graphStore.SupersedeFact(replaces, recorded.Seq)
}

func (h *Head) raiseRailFromReply(user store.Message) (bool, error) {
	if h == nil || h.store == nil || h.dailyBudgetUSD <= 0 || !affirmativeRailReply(user.Body) {
		return false, nil
	}
	rail, pending, err := h.store.PendingDailyRailApproval(h.dailyBudgetUSD, user.SessionID)
	if err != nil || !pending {
		return false, err
	}
	amount := h.railRaiseCovering(user, rail)
	if err := h.store.RaiseDailyRail(amount, "head:"+user.SessionID); err != nil {
		return false, err
	}
	updated, err := h.store.DailyRailToday(h.dailyBudgetUSD)
	if err != nil {
		return false, err
	}
	reply := fmt.Sprintf("Daily rail raised by $%.2f to $%.2f -- continuing.", amount, updated.Ceiling)
	return true, h.postAgent(user.SessionID, reply, 0)
}

// railRaiseCovering is consent delivering what the question promised.
//
// The rail question the store posts is worded from journaled spend, and when the
// thing that stopped the work has not been journaled yet — a catalog-priced
// generation, an in-process headless total — it names that cost separately:
// "$19.00 spent of $20.00, and the next step costs $15.00. Say the word and I'll
// continue." The head then recomputed the raise from journaled spend alone, so
// consent bought one budget unit, the ceiling landed under the step the sentence
// had just quoted, and the work stopped again at the same place with the user
// having already said yes. "I'll continue" has to be true; the raise therefore
// clears the item the question named as well as the spend it named.
//
// The pending figure is recovered from the durable question itself because that
// is where the store wrote it — the rail read the head is handed reports only
// what the journal has seen. The clean version of this lives on the store side,
// which owns both the wording and the arithmetic; here the parse fails closed,
// so a reworded question simply returns today's raise rather than a wrong one.
func (h *Head) railRaiseCovering(user store.Message, rail store.DailyRail) float64 {
	amount := rail.RaiseAmount()
	pending := h.pendingRailStep(user)
	if pending <= 0 {
		return amount
	}
	// One cent of headroom above the quoted step, so the very item the user
	// consented to does not re-trip the rail on the boundary.
	shortfall := rail.Spend + pending + 0.01 - (rail.Ceiling + amount)
	if shortfall <= 0 {
		return amount
	}
	return amount + math.Ceil(shortfall*100)/100
}

// railStepPrefix is the store's own wording for the not-yet-journaled item, and
// it is matched rather than reconstructed so that a change to that sentence
// makes this return zero — today's behaviour — instead of a wrong number.
const railStepPrefix = ", and the next step costs $"

// pendingRailStep reads the cost the pending rail question quoted. It looks in
// the same thread window the router reads, which is where the question the user
// is answering necessarily sits.
func (h *Head) pendingRailStep(user store.Message) float64 {
	recent, err := h.recentThread(user.SessionID, user.Seq)
	if err != nil {
		return 0
	}
	for index := len(recent) - 1; index >= 0; index-- {
		message := recent[index]
		if message.Role != store.RoleAgent ||
			!strings.HasPrefix(message.Body, store.DailyRailQuestionPrefix) {
			continue
		}
		start := strings.Index(message.Body, railStepPrefix)
		if start < 0 {
			return 0
		}
		// The figure is followed immediately by the sentence's own full stop, so
		// the first decimal point belongs to the number and a second one ends
		// it. A trailing point is punctuation either way.
		digits := message.Body[start+len(railStepPrefix):]
		end, point := 0, false
		for end < len(digits) {
			character := digits[end]
			if character >= '0' && character <= '9' {
				end++
				continue
			}
			if character == '.' && !point {
				point = true
				end++
				continue
			}
			break
		}
		cost, parseErr := strconv.ParseFloat(strings.TrimSuffix(digits[:end], "."), 64)
		if parseErr != nil || cost <= 0 || math.IsNaN(cost) || math.IsInf(cost, 0) {
			return 0
		}
		return cost
	}
	return 0
}

func affirmativeRailReply(body string) bool {
	normalized := strings.ToLower(strings.TrimSpace(body))
	normalized = strings.Trim(normalized, " .,!?:;\t\n\r")
	switch normalized {
	case "y", "yes", "yes please", "continue", "go ahead", "go on", "proceed", "do it", "sure", "ok", "okay":
		return true
	default:
		return false
	}
}

func (h *Head) clientFor(user store.Message) (Client, error) {
	if h.messageClient == nil || strings.TrimSpace(user.Model) == "" {
		return h.client, nil
	}
	client, err := h.messageClient(user)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("message client selector returned nil")
	}
	return client, nil
}

func replyModel(user store.Message, client Client, resolved string) string {
	// Ordinary talk replies remain byte-for-byte unannotated. Only a durable
	// per-message request earns reply attribution in the thread.
	if strings.TrimSpace(user.Model) == "" {
		return ""
	}
	if resolved = strings.TrimSpace(resolved); resolved != "" {
		return resolved
	}
	if modeled, ok := client.(interface{ Model() string }); ok {
		if model := strings.TrimSpace(modeled.Model()); model != "" {
			return model
		}
	}
	return strings.TrimSpace(user.Model)
}

func (h *Head) supportsImages(client Client) bool {
	if h == nil || h.modalities == nil {
		return false
	}
	model := h.defaultModel
	if current, ok := client.(interface{ Model() string }); ok {
		model = current.Model()
	}
	return h.modalities.Supports(model, "input", "image")
}

func imageContentPart(reference string) (ai.ContentPart, bool) {
	// An attachment is journaled as a durable reference to our own copy. The
	// front desk answers in the moment and has no blob store in hand, so it
	// reads the file the person attached, which is still where they left it.
	path := cas.SourcePath(reference)
	ext := strings.ToLower(filepath.Ext(path))
	mediaType := map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp", ".gif": "image/gif"}[ext]
	if mediaType == "" {
		return ai.ContentPart{}, false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > 10<<20 {
		return ai.ContentPart{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ai.ContentPart{}, false
	}
	if len(data) > 10<<20 {
		return ai.ContentPart{}, false
	}
	return ai.ContentPart{Type: "image_url", ImageURL: &ai.ImageURLData{
		URL: "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data),
	}}, true
}

// Two phrase lists used to stand here — twelve substrings deciding whether the
// measured competence map reached the prompt, fourteen deciding the same for
// standing-watch status. They are gone, and nothing replaced them at this seam.
// "Am I asking too much of you lately?" matched neither, so the head answered a
// question about its own measured performance out of the notebook and invented a
// self-assessment the map beside it contradicted — the exact failure the prompt
// forbids, arriving through the door the gate left open. Both are belt reads
// now: the law lives in the tool description, the model decides when a question
// needs the evidence, and the next phrasing nobody wrote down still lands.

// The thread window moves in big steps rather than sliding. A one-message slide
// meant the oldest rendered message changed on every single turn, and the oldest
// message sits near the front of the router's prompt: everything after it was
// re-billed at full price each time the user said anything. So the window fills
// to threadWindowMax and, on overflowing, drops back to threadWindowKeep in one
// cut. The front then holds still for another ten messages or so, and each of
// those messages appends to a prefix the endpoint already has.
//
// maxThreadContextBytes doubled alongside the window, which leaves the
// allowance per message exactly where the sliding window had it: the block is
// still bounded, and nothing about it grows message over message.
// The window is counted over the conversation, not over the journal. A message
// count was the whole window once, and a running job speaks on a two-minute
// heartbeat: three of them filled twenty rows in about thirteen minutes and the
// user's own words fell out the front of a window they had never left. The
// person asks "did you find anything cheaper?" against a slice containing none
// of the conversation that question is deictic to.
//
// So there are two windows over one read. The person's window holds their turns
// and every thread-level line spoken to them, and nothing a job says can evict
// a single row of it. The ambient window holds the job chatter — narration,
// heartbeats, deliverables — bounded separately and far smaller, because the
// board carries that content already and the thread only needs enough of it for
// position to mean something.
//
// Both cut in big steps for the same reason the single window did: the front of
// this block sits near the front of the router's prompt, and a front that moves
// on every message re-bills everything behind it at full price.
const (
	threadWindowMax   = 20
	threadWindowKeep  = 10
	ambientWindowMax  = 12
	ambientWindowKeep = 6
)

// personRelevant separates the conversation from the reporting around it. A
// user turn is theirs; so is anything said to the thread itself rather than
// filed under a job — a receipt, a question, an answer. A line anchored to a
// node is a worker narrating, which is ambient by construction.
func personRelevant(message store.Message) bool {
	return message.Role == store.RoleUser || strings.TrimSpace(message.NodeID) == ""
}

// threadFold is where the window's fold gets left between reads.
//
// The window is a left fold over an append-only sequence, which is the property
// the big-step cut was built on and it has a second consequence nobody was
// collecting: a fold that has already consumed the first N messages never has
// to consume them again. So the state is kept — the two windows and the bound
// they cover — and the next read starts where the last one stopped.
//
// That matters twice over. Within one turn the same window is asked for between
// two and six times, by the router, the correction reader, the redirect reader
// and the identity reader, all with the same bound; they now fold once and the
// rest read the answer. Across turns, a session that has run all day stops
// re-decoding every message it has ever held to keep the last thirty-two.
//
// One session's fold is kept rather than a table of them: the head answers one
// message at a time and a conversation arrives in runs, so a second thread
// simply folds from zero — which is what every read did before.
type threadFold struct {
	sessionID string
	// folded is the exclusive bound the two windows cover: every message of
	// this session below it has been folded in. It is also the resume point,
	// because the journal only ever appends and a seq below it can never
	// appear later.
	folded  int64
	person  []store.Message
	ambient []store.Message
	window  []store.Message
}

func (h *Head) recentThread(sessionID string, beforeSeq int64) ([]store.Message, error) {
	h.foldMu.Lock()
	defer h.foldMu.Unlock()

	fold := h.fold
	// Resuming is only sound forward. A read of an older bound would have to
	// un-fold messages the window has already cut against, so it starts over.
	if fold == nil || fold.sessionID != sessionID || fold.folded > beforeSeq {
		fold = &threadFold{
			sessionID: sessionID,
			person:    make([]store.Message, 0, threadWindowMax+1),
			ambient:   make([]store.Message, 0, ambientWindowMax+1),
		}
	}
	if fold.folded == beforeSeq && fold.window != nil {
		return copyThread(fold.window), nil
	}

	// The fold is dropped for the duration of the work and put back only when
	// the work finished. Folding appends to the kept windows and a cut rewrites
	// one of them in place, so a read that fails halfway leaves state that no
	// longer matches the bound recorded beside it — and the cheapest correct
	// answer to that is to have no fold rather than a wrong one.
	h.fold = nil
	cursor := fold.folded - 1
	if cursor < 0 {
		cursor = 0
	}
	person, ambient := fold.person, fold.ambient
	for done := false; !done; {
		messages, err := h.store.Messages(sessionID, cursor, messagePageSize)
		if err != nil {
			return nil, err
		}
		if len(messages) == 0 {
			break
		}
		for _, message := range messages {
			cursor = message.Seq
			if message.Seq >= beforeSeq {
				done = true
				break
			}
			// Each cut is a fold over the whole session, so the window is a pure
			// function of how many messages precede this one — the same session
			// read twice renders the same bytes.
			if personRelevant(message) {
				person = append(person, message)
				if len(person) > threadWindowMax {
					person = append(person[:0], person[len(person)-threadWindowKeep:]...)
				}
				continue
			}
			ambient = append(ambient, message)
			if len(ambient) > ambientWindowMax {
				ambient = append(ambient[:0], ambient[len(ambient)-ambientWindowKeep:]...)
			}
		}
	}

	// The kept window is a copy of its own, and so is every window handed out.
	// mergeThreadWindow returns the person slice itself when nothing is ambient,
	// and the next cut rewrites that slice in place — a caller holding it, or a
	// cache holding it, would find its thread had quietly changed shape. Every
	// read used to build its own slices, so a copy is what callers already had.
	fold.person, fold.ambient = person, ambient
	fold.folded = beforeSeq
	fold.window = copyThread(mergeThreadWindow(person, ambient))
	h.fold = fold
	return copyThread(fold.window), nil
}

func copyThread(messages []store.Message) []store.Message {
	return append(make([]store.Message, 0, len(messages)), messages...)
}

// mergeThreadWindow puts the two windows back into journal order. Both are
// already in it, so this is one pass — and every surviving line keeps its true
// position, which is the whole of what adjacency reads.
func mergeThreadWindow(person, ambient []store.Message) []store.Message {
	if len(ambient) == 0 {
		return person
	}
	merged := make([]store.Message, 0, len(person)+len(ambient))
	next := 0
	for _, message := range person {
		for next < len(ambient) && ambient[next].Seq < message.Seq {
			merged = append(merged, ambient[next])
			next++
		}
		merged = append(merged, message)
	}
	return append(merged, ambient[next:]...)
}

func (h *Head) postAgent(sessionID, body string, commandSeq int64) error {
	return h.postAgentModel(sessionID, body, commandSeq, "", nil)
}

// There is no postSystem, and its absence is 13.18 written in the type system.
// The head has exactly three things it may put in a thread — a COMMITMENT, a
// DELIVERY, a QUESTION — and every one of them is somebody speaking: postAgent
// and postAgentModel for the first two, postQuestion and the ask gates for the
// third. A system-voice helper is a door onto the fourth class the law says
// does not exist, and the last caller of the one that used to live here went
// away long before the law was written down.

func (h *Head) postAgentModel(sessionID, body string, commandSeq int64, model string, parts []store.MessagePart) error {
	_, err := thread.Post(h.store, store.Message{
		SessionID:  sessionID,
		Role:       store.RoleAgent,
		Body:       body,
		CommandSeq: commandSeq,
		Model:      strings.TrimSpace(model),
		Answers:    h.answering(),
		Parts:      parts,
	})
	if err != nil {
		return fmt.Errorf("serve head: post reply: %w", err)
	}
	return nil
}

// notebookContextBytes bounds the memory shown to the router: enough for the
// beliefs that matter to this message, never the whole archive.
const notebookContextBytes = 2000

// renderNotebook blends the two free retrieval layers — BM25 relevance to
// this message, then recency — into a bounded, age-annotated view. The age
// on every line is deliberate: a claim's freshness is part of its evidence.
//
// thread is the rendered thread window sitting above this block in the same
// prompt, and a fact whose body is already visible there is suppressed. The head
// manufactures that collision itself: a preference captured mid-turn from the
// user's own sentence comes back one message later as a numbered notebook line
// beside the sentence it was taken from, so the model is shown its own capture
// as independent standing evidence for the thing it captured.
func renderNotebook(graphStore *store.Store, message, thread string) string {
	if graphStore == nil {
		return "(no notebook)"
	}
	now := time.Now()
	seen := make(map[int64]bool)
	total := 0
	var lines []string
	add := func(facts []store.Fact) {
		for _, fact := range facts {
			eligible, err := graphStore.PromptEligible(fact)
			if err != nil || !eligible {
				continue
			}
			if seen[fact.Seq] {
				continue
			}
			seen[fact.Seq] = true
			// The same probe the deep slice dedups with, pointed at a different
			// pair — floor and all, so a two-word belief cannot match the thread
			// by accident and vanish from the memory the model reads.
			if deepAlreadyInThread(thread, fact.Body) {
				continue
			}
			line := fmt.Sprintf("- #%d [%s · %s · %s] %s", fact.Seq, fact.Scope,
				fact.Kind, store.AgeLabel(fact.Time, now), fact.Body)
			// Skip, never stop. One 512-byte belief landing at relevance-rank
			// three used to end the whole pass — silencing the search hits behind
			// it AND the recency layer that runs after it — so the notebook the
			// model read was a function of which long fact happened to match this
			// message's wording. Packing past an oversized line costs nothing and
			// makes the budget the bound it claims to be.
			if total+len(line) > notebookContextBytes {
				continue
			}
			total += len(line)
			lines = append(lines, line)
		}
	}
	if found, err := graphStore.SearchFacts(store.FactQuery{
		Cues: resident.ExtractCues(message), Terms: message, Limit: 8,
	}); err == nil {
		add(found)
	}
	if recent, err := graphStore.RecentFacts(10); err == nil {
		add(recent)
	}
	if len(lines) == 0 {
		return "(nothing learned yet)"
	}
	return strings.Join(lines, "\n")
}

// routeMemory is a durable fact the user just stated, captured into the
// notebook at conversation speed rather than waiting for a job to distill it.
type routeMemory struct {
	Scope string `json:"scope"`
	Kind  string `json:"kind"`
	Body  string `json:"body"`
	// Replaces is the numbered notebook line this capture makes untrue.
	//
	// Without it the head could only ever add. It is shown the beliefs relevant
	// to the message, the person says "we don't do that any more", and the only
	// verb available was remember — so the contradiction landed BESIDE the
	// belief it contradicted, both active, both selectable into every future
	// job. The consolidator's own prompt calls that state worse than either line
	// alone, and the system produced it on purpose because nothing else was
	// spellable. This is not retraction: the person is not throwing a line away,
	// they are telling you the new version of it, and the old one retires as
	// evidence for the new rather than as something refused.
	Replaces int64 `json:"replaces"`
}

const routeReflexKind = "reflex"

func commandKind(kind string) (store.CommandKind, bool, bool) {
	switch kind {
	case routeReflexKind:
		return store.CommandSplice, true, true
	case string(store.CommandSplice):
		return store.CommandSplice, false, true
	case string(store.CommandAmend):
		return store.CommandAmend, false, true
	case string(store.CommandCancel):
		return store.CommandCancel, false, true
	case string(store.CommandPause):
		return store.CommandPause, false, true
	case string(store.CommandResume):
		return store.CommandResume, false, true
	case string(store.CommandReprioritize):
		return store.CommandReprioritize, false, true
	case string(store.CommandRestart):
		return store.CommandRestart, false, true
	default:
		return "", false, false
	}
}

// consequenceGated is a safety membrane, not a triviality classifier. It names
// only irreversible effect families; everything about how small or obvious an
// action is remains a learned model judgment.
func consequenceGated(instruction string) bool {
	if RecognizesServiceIntent(instruction) {
		return true
	}
	lower := strings.ToLower(instruction)
	words := strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	contains := func(candidates ...string) bool {
		for _, word := range words {
			for _, candidate := range candidates {
				if word == candidate {
					return true
				}
			}
		}
		return false
	}
	if contains("buy", "purchase", "pay", "spend", "transfer", "donate", "subscribe", "order", "refund") {
		return true
	}
	if contains("publish", "post", "tweet", "email", "send", "deploy", "release", "push", "merge") {
		return true
	}
	if !contains("delete", "remove", "erase", "wipe", "destroy", "drop") {
		return false
	}
	if strings.Contains(lower, "outside the workspace") ||
		contains("account", "database", "production", "remote", "cloud", "system") {
		return true
	}
	for _, field := range strings.Fields(lower) {
		field = strings.Trim(field, `"'(),;:`)
		if strings.HasPrefix(field, "/") || strings.HasPrefix(field, "~/") {
			return true
		}
	}
	return false
}

// crossSessionMark is #41's whole surface. The board is global and the thread is
// not: with a terminal and a browser both open, one conversation can amend or
// cancel work the other started and neither could say where it came from. The
// answer is not to hide the row — the snapshot IS the workforce, and a board that
// omits a job is the one reality the head must never be handed — but to say
// whose window it came from, in one word, on the line that was already there.
const crossSessionMark = " | elsewhere"

// crossSession reports whether a node was started from a different conversation
// than the one asking. Work with no session at all — the resident's own, a
// charter's firing — is nobody's window and is never marked.
func crossSession(node store.Node, sessionID string) bool {
	origin := strings.TrimSpace(node.Provenance.SessionID)
	return origin != "" && sessionID != "" && origin != sessionID
}

// boardJobRoot is the store's own definition of a job root read off a snapshot:
// parented on the permanent spine, or on a territory that packed it away. A node
// whose parent is not in the snapshot at all is treated as a root, because there
// is nothing to attribute it to and dropping it is never an option.
func boardJobRoot(node store.Node, byID map[string]store.Node) bool {
	parent := strings.TrimSpace(node.Parent)
	if parent == "" || parent == store.RootID {
		return true
	}
	owner, ok := byID[parent]
	return !ok || owner.Group == store.TerritoryGroup
}

// boardSubtreeCounts rolls a job's open subtree into the clause the belt's board
// has always carried. It is the same reading: "the running ones" means jobs with
// somebody working on them, not jobs whose root happens to hold a running status.
func boardSubtreeCounts(root store.Node, byID map[string]store.Node, children map[string][]string) string {
	running, queued, failed := 0, 0, 0
	seen := make(map[string]bool, 8)
	stack := []string{root.ID}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		switch byID[id].Status {
		case store.Running, store.Claimed:
			running++
		case store.Pending:
			queued++
		case store.Failed:
			failed++
		}
		stack = append(stack, children[id]...)
	}
	if running == 0 && queued == 0 && failed == 0 {
		return ""
	}
	counts := fmt.Sprintf("%d running, %d queued", running, queued)
	if failed > 0 {
		counts += fmt.Sprintf(", %d failed", failed)
	}
	return counts
}

// boardOwnerLabel names the job one part belongs to.
func boardOwnerLabel(node store.Node, byID map[string]store.Node) string {
	for depth := 0; depth < adjacencyAncestorDepth; depth++ {
		owner, ok := byID[strings.TrimSpace(node.Parent)]
		if !ok || owner.ID == store.RootID {
			return ""
		}
		if boardJobRoot(owner, byID) {
			return surgeryTargetLabel(owner)
		}
		node = owner
	}
	return ""
}

const (
	// boardWaitsCap is how many upstreams one row names. Past a few this is the
	// dependency list rather than the reason a row is sitting still, and the
	// plan read is where a whole dependency list belongs.
	boardWaitsCap = 3
	// boardWaitLabelBytes keeps one upstream to the width of a name. The label
	// is a job's own title; a title long enough to need cutting is a brief that
	// was never given a title.
	boardWaitLabelBytes = 48
)

// boardWaits derives "what is this row sitting behind" from the edges the
// snapshot already carries. Only unsettled upstreams count: an edge from work
// that has landed is a record of where the input came from, not a reason
// anything is waiting, and saying "waits on" about it would make a moving job
// read as a stuck one.
func boardWaits(snapshot store.Snapshot, byID map[string]store.Node) map[string]string {
	if len(snapshot.Edges) == 0 {
		return nil
	}
	labels := make(map[string][]string)
	named := make(map[string]bool, len(snapshot.Edges))
	for _, edge := range snapshot.Edges {
		source, ok := byID[edge.From]
		if !ok || source.FoldRoot || !classOpen(source.Status) {
			continue
		}
		if _, ok := byID[edge.To]; !ok {
			continue
		}
		// Two kinds of edge between the same pair are one wait, not two.
		pair := edge.To + "\x00" + edge.From
		if named[pair] || len(labels[edge.To]) >= boardWaitsCap {
			continue
		}
		named[pair] = true
		labels[edge.To] = append(labels[edge.To],
			truncateBytes(surgeryTargetLabel(source), boardWaitLabelBytes))
	}
	rendered := make(map[string]string, len(labels))
	for id, names := range labels {
		rendered[id] = strings.Join(names, ", ")
	}
	return rendered
}

// boardRunningFor is how long the longest-running node at or under a row has
// been going. A job root usually carries no start time of its own — its parts
// do the work — so a row that says "running" with no duration was the whole
// reason the head could describe a job forty-nine minutes in as if it had just
// been asked for.
func boardRunningFor(node store.Node, byID map[string]store.Node, children map[string][]string, now time.Time) time.Duration {
	longest := time.Duration(0)
	seen := make(map[string]bool, 8)
	stack := []string{node.ID}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		member := byID[id]
		if member.Status == store.Running || member.Status == store.Claimed {
			if !member.StartedAt.IsZero() {
				if elapsed := now.Sub(member.StartedAt); elapsed > longest {
					longest = elapsed
				}
			}
		}
		stack = append(stack, children[id]...)
	}
	return longest
}

// boardElapsed spells a live duration at the resolution the head's clock uses.
// Nothing this head says turns on a second, and a second-resolution clause
// would rewrite the board between two messages the way the cost in cents once
// rewrote the belt's.
func boardElapsed(elapsed time.Duration) string {
	switch {
	case elapsed < time.Minute:
		return "under a minute"
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm", int(elapsed/time.Minute))
	default:
		return fmt.Sprintf("%dh %02dm", int(elapsed/time.Hour), int(elapsed/time.Minute)%60)
	}
}

// renderThread is the conversation as the model reads it, and every line spoken
// by a job says which job spoke it. The label is the job's own short title —
// what the user sees on the card and in the receipt — because a referent only
// resolves when both parties are using the same name for the same work.
func (h *Head) renderThread(messages []store.Message) string {
	if len(messages) == 0 {
		return "(no earlier messages in this session)"
	}
	names := h.jobNames()
	var rendered strings.Builder
	for _, message := range messages {
		body := truncateBytes(strings.TrimSpace(message.Body), 600)
		body = strings.ReplaceAll(body, "\n", "\n  ")
		line := fmt.Sprintf("%s: %s\n", message.Role, body)
		if label := names.label(message.NodeID); label != "" {
			line = fmt.Sprintf("%s [%s]: %s\n", message.Role, label, body)
		}
		if rendered.Len()+len(line) > maxThreadContextBytes-len(threadTruncatedMark) {
			rendered.WriteString(threadTruncatedMark)
			break
		}
		rendered.WriteString(line)
	}
	return strings.TrimSpace(rendered.String())
}

// nowLineLayout is the one spelling of the clock every conversational prompt
// uses. The weekday is there because people say "Monday" far more often than
// they say a date; the minute is the floor because nothing this head decides
// turns on a second, and a second-resolution clock would rewrite the prompt's
// tail on every single message.
const nowLineLayout = "Mon 2006-01-02 15:04"

// nowLine is the head's clock, in local time because every word the user uses
// for time is local. It is deliberately one line: the time-scoped reads bound
// their own windows, and this is only what those windows are measured from.
func nowLine(now time.Time) string {
	return "now: " + now.Local().Format(nowLineLayout) + " local"
}

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if index := strings.IndexAny(value, "\r\n"); index >= 0 {
		value = value[:index]
	}
	return strings.TrimSpace(value)
}

func truncateBytes(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	cut := limit - len("…")
	for cut > 0 && !utf8.ValidString(value[:cut]) {
		cut--
	}
	return value[:cut] + "…"
}

func textMessage(role, body string) ai.Message {
	return ai.Message{Role: role, Content: []ai.ContentPart{{Type: "text", Text: body}}}
}
