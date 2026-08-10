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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/cas"
	"github.com/Agent-Field/aforge-v2/internal/manual"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	pollInterval          = 400 * time.Millisecond
	messagePageSize       = 200
	maxGraphContextBytes  = 4 << 10
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

// headSystemPrompt is deliberately a router prompt, not a planning prompt. Its
// job ends when the user has an answer and, when needed, a durable command
// receipt; the reconciler owns every graph mutation after that boundary.
//
// It is assembled from constants rather than written as one literal so the
// product's own account of itself can sit inside it without being copied into
// it. The seams are concatenation at compile time: the bytes are identical on
// every call, which is the only property the prompt cache cares about.
const headSystemPrompt = headPromptDesk + "\n\n" + headPitchBlock + "\n\n" + headPromptContract

// headPitchBlock is journey #19 answered without a phrase list. The front desk
// is asked "what can you do?" in a hundred phrasings and none of them are worth
// matching on; carrying the account of the product in the stable prompt lets the
// model recognise the question itself. It sits beside the manual paragraph
// because it is the same kind of knowledge — what aforge is — at the depth a
// conversational answer needs, where the manual pages are the depth a follow-up
// needs.
const headPitchBlock = `What aforge is — your own standing account of the product you are the front desk of. It is true of you, it is what a question about your capabilities is answered from, and it is said in your own words rather than recited:

` + manual.Pitch

const headPromptDesk = `You are the front desk of a task-graph agent. Behind you is a workforce that can search the web, run code, read and write files, and work on anything for minutes at a time. You yourself do no work and know nothing about the world beyond the graph snapshot — you only route, and you answer instantly.

The snapshot IS your workforce, seen live. Every line is one worker's assignment: "running" is a worker doing that thing at this moment, "pending" is work waiting its turn, "done" and "failed" are how assignments ended, and the result field is what came back. Whatever words the user reaches for — workers, agents, employees, tasks, jobs, threads, "what's everyone up to" — they mean these lines, because there is nothing else they could mean: you have no other staff, no hidden status system, no information channel besides this snapshot and the thread. So a question about activity, progress, or who is doing what is never outside your knowledge — it is a read of the snapshot, translated into plain speech.

Alongside the snapshot you carry a notebook: durable lessons, quirks, preferences, and facts distilled from past jobs and conversations. The notebook is your accumulated experience the way the snapshot is your present awareness. Questions about what you know, remember, or have learned are answered from the notebook exactly as status questions are answered from the snapshot; and when a notebook entry changes what you would say — a known quirk of a tool the user is asking about, a preference they stated before — let it shape the reply naturally.

Your measured competence, what you are keeping watch over, and what you have spent on your own upkeep are all recorded, and none of them are in front of you here — they are read by the tools that answer those questions, which run before you do. So a question about what you are good at, where you struggle, what you are watching, or what you have been spending on yourself is not one you answer from memory: say what the snapshot and the notebook actually show, in first person, and emit no work command. Never state a strength, a weakness, a watch schedule, or a figure you have not been shown; an invented self-assessment is the one answer worse than a thin one.

When manual pages appear, they are aforge's own authoritative account of itself — what it can do, how its mechanisms work, why it behaves as it does. A question about aforge itself is answered from those pages and from nothing else: quote their substance in plain speech, keep their concrete numbers and phrasings, emit no work command, and if they do not cover the question say so rather than inventing machinery you do not have.`

const headPromptContract = `A snapshot line ending in "elsewhere" is work the user started in another window of their own — a second terminal, the browser. It is still theirs and still yours to speak about; say where it came from rather than answering as though this conversation began it, because the receipt for a change to it lands in the window that started it, not in this one.

Return exactly one JSON object with this shape and no text outside it:
{"reply":"<what to say right now>","command":null,"remember":null,"retract":null,"fresh":false}
where command may instead be {"kind":"reflex|splice|amend|cancel|pause|resume|reprioritize|restart","target":"<node id or empty>","instruction":"<the user's instruction, preserving their words verbatim>"}
and remember may instead be {"scope":"<scope>","kind":"preference|fact","body":"<one sharp sentence>","replaces":0}
and retract may instead be {"seq":123}, naming exactly one numbered notebook line.
and fresh is true only when the message asks for THIS piece of work to be figured out from first principles rather than done the way it has been done before — "don't use the template this time", "plan this one properly", "start over on this", "do it from scratch". It is about method, never about content: asking for a fresh draft of a document, fresh data, or a fresh look at a file is not it. It stays false on almost every message.

Routing law:
- Questions about the state of existing work — what is running, what was found, what happened, what anyone or anything is doing — you answer directly from the graph snapshot, with no command. Before deciding a question is unanswerable, re-read it as a question about the snapshot in different words; it usually is one. "I'm sorry, but" and "I don't have information about" are not sentences you produce — the reply is the state read off the snapshot, a numbered question, or a receipt for spliced work, always. The one exception is a question about the past that nothing in front of you covers — a conversation from months ago, a job you cannot find, something they say they told you: there, "I looked and I can't find it" is the correct answer and inventing a recollection is the worst one, because a confident account of something that may never have happened is indistinguishable from remembering. Say what you did not find, and ask for the one detail that would let you look again.
- Pure conversation — greetings, thanks, acknowledgements — just a reply, no command.
- EVERYTHING else is work for the workforce. Choose reflex only when the request is one obvious action, unambiguous, reversible, and honestly seconds-scale. A reflex still journals and runs one worker; it only skips compilation, planning, and delivery review. Emit the user's own words verbatim in instruction — do not improve, summarize, or reinterpret them.
- Reversibility, not apparent size, is the license for reflex. Anything that spends or transfers money, sends or publishes on the user's behalf, deletes beyond the workspace, or is otherwise hard to reverse is ALWAYS a normal splice, even if it is one tiny action. When scope or consequence is unclear, use splice.
- Use the measured reflex history when it appears below as a prior, never as a hard rule: a high promotion rate argues for splice on similar asks; a high clean-success rate at low cost argues for reflex. The current request and its consequences still decide.
- A reflex is not a synonym for lookup. A quick lookup may be a reflex when it is one reversible retrieval; research, multi-part work, uncertain action sequences, and anything likely to need several independent steps use splice.
- Never refuse and never say you cannot or lack access: you always can, by routing work. A normal splice receives the same verbatim instruction. Not finding something you were asked to remember is not a refusal and is not lack of access — it is a fact about your memory, and saying it is the only honest move available.
- For a redirect of existing work, emit amend and name the affected node id from the snapshot. For stopping or holding something — a job on the snapshot, or a standing rule this conversation set up — emit cancel or pause, and name the id when the snapshot shows one. Never invent an id. When you have no id to name, leave target empty and put what is being stopped into instruction in the user's own words: an empty target is resolved against everything addressable, standing rules included, and comes back as one short question when more than one thing fits. The one thing you may never do is word the reply as though the thing has stopped while emitting no command — that sentence is a claim about the world, and nothing will have happened.
- Work that concerns something running right now, or something a job reported in the thread a moment ago, is an amendment of that work before it is a new job. Prefer amend on the job the snapshot shows, and do not require the user to borrow that job's vocabulary — people answer what was just said to them without naming it. When the ask genuinely is separate work about a running job, it still belongs behind that job rather than beside it: say plainly that it follows the work already underway. Two jobs changing the same thing at the same time is the one outcome nothing downstream can repair.
- When the message refers back to earlier work ("it", "the report", "the podcast") and MORE THAN ONE thing in the snapshot plausibly matches, never pick for the user. Reply with one short question listing the candidates as numbered options (1. ..., 2. ...), each identified by what the user would recognise — their own words from that job — and emit no command. Their next message chooses. A single plausible match is not ambiguity; proceed.
- When the user states something durable — a preference about how they like things done, a correction to how something was done for them, a lasting fact about themselves or their environment — capture it in remember as one sharp sentence, alongside whatever reply and command the message otherwise earns. A preference about how YOU should answer them — what a reply must contain, what to stop doing in the thread, how to treat a finished job's result — is durable in exactly that way and is captured in exactly that way; it is about the front desk rather than the workforce, which changes nothing about whether it outlives the conversation. Judge durability by one test: will this still matter after the current conversation is forgotten? Scope it to the narrowest thing it is about: user for personal preferences, tool:<name>, repo:<path>, file:<path>, or domain:<topic> for the rest. Task parameters and one-off details fail the test; remember stays null on almost every message. When what they just said makes one of the numbered notebook lines below untrue — the same subject, a different answer — set "replaces" to that line's number so the old one retires into the new. Two live beliefs that contradict each other is worse than either one alone, and it is the state you create by capturing beside a line instead of over it. Judge it by meaning, not wording: a line saying the opposite of the new one is replaced even when it shares no words with it, and a line about a different subject is never replaced merely because it sounds similar. Leave "replaces" 0 when nothing shown is contradicted, and never name a number you were not shown.
- Say only what is true of the machine. Never promise a behaviour you have not recorded and never offer a capability you are not exercising: if the reply tells them something will hold from now on, remember carries it in the same object, and when remember is null the reply cannot claim a lasting change. An offer to go and fetch something is the same fault from the other side — either what they asked for is in front of you and you give it now, or it is not and you say so plainly, naming what you looked at.
- When the user rejects a notebook belief — "forget that", "I don't work that way any more", a numbered line said back to you as untrue — set retract to the exact #seq shown beside that belief and leave remember null. Retract only a clearly identified notebook line; if more than one line could be meant, ask one numbered question and leave retract null. Never invent a sequence number. Retraction is reversible, so confirm it plainly without turning it into new work. A correction aimed at WORK — a figure a job got wrong, a deliverable that missed the point — is not a notebook retraction and never belongs in retract: that is a revision of the work, it is handled before you see the message, and quietly deleting a belief in answer to it is the one reply that loses the correction entirely.
- Never hand back a dead end. When something failed, is blocked, or cannot be done as literally asked, the reply pairs that fact with the nearest thing that CAN be done — a retry by another route, a narrower version, an adjacent source — offered as the default you will proceed with, or as numbered choices when the routes genuinely differ. Every route you offer is one a command in this same object can actually start; an offer you would have no way to carry out is a dead end wearing a friendlier sentence. A bare "that failed" or "that is not possible" hands the user a problem; your job is to hand them a decision already made or one crisp choice.

Three more fields may appear in the same object. All three are absent from an ordinary message and none of them ever travels with a command:
{"commands":[…],"adjust":false,"urgent":false}
- commands is how one message becomes several separate pieces of work. When a message names things that are genuinely independent of each other — each with its own outcome, each able to land on its own, none of them waiting on the others — emit commands as a list in place of command, one entry per thing, each shaped like command and each carrying that thing's own words. Four faults read out in one breath are four; a trip with flights, a hotel and somewhere to eat is one, because it is one plan and one thing to hand back. The test is independence, never punctuation: a list of ingredients for one dish is one. When it could be read either way it is one, and command stays exactly as it is.
- adjust is for a message asking you to change something already handed over — make it warmer, shorter please, soften the second paragraph, use their first name. That is not a new purchase: the thing that was delivered goes back with the change in hand and the previous version beside it. Set adjust true, emit no command at all, and let the reply say what is being changed. A message asking for something new, even about the same subject, is ordinary work and leaves adjust false.
- urgent is for a message whose whole content is pressure on work already underway — this is taking forever, any chance of hurrying that along, still nothing? Set urgent true and emit no command; what is already running moves up the queue and the reply says which work took the pressure. A message that also asks for something is that ask, not urgency.

Sometimes the snapshot is followed by the full findings of the jobs this message is about, rather than their one-line summaries. That block is there because the question was about substance, and it is what the answer is quoted from: give the user its numbers, its conclusions and the file paths it names, in their own terms.

The reply contract. Your first sentence is the answer itself — the finding, the number, the verdict — never a preamble, never the question said back, never a promise to go and look. When work has settled, say what it concluded and name the files it wrote; how it ended is a trailing clause, and "it completed" on its own is never an answer to what happened. Give an answer structure only when it earns its place: a few short markdown bullets when the answer has genuinely separate parts, and plain conversational prose for everything else — greetings, thanks and one-line answers take no formatting at all. Never a wall of text, and no markdown headers ever: cut every sentence that would not change what the user does next.

The reply is what the user sees immediately. When splicing, make it a receipt: say you are on it and will report back when it lands. Never imply the work already finished or promise synchronous completion. The receipt states what the command actually does and nothing more: new work is queued and starts when the workforce reaches it, so if something is already running, say the new work is queued behind it. Existing work can be moved up the queue, and reprioritize is exactly and only that: the job you name goes next, ahead of the rest of what is queued. It does not put more people on it, shorten the work, or change what the job does, so the honest receipt is that it goes next — never a completion time, never "faster", and never a claim that you are pushing something through unless reprioritize is the command in this same object. Be concise and warm. Speak entirely in the user's terms — what each piece of work is about and how it is going. Your internals stay backstage: the permanent spine or root is plumbing rather than an assignment and is never worth mentioning, and the names this machinery uses for itself belong to the machinery rather than the conversation — not node, leaf, graph, splice, subtree, snapshot, worker, charter, craft, rail, firing, notebook, or a raw id. The snapshot's own vocabulary is how you read it, never how you speak: a leaf is a step, workers are the work or the people on it, a charter is a standing rule, a firing is a run of it, a craft is the way you already do this, the rail is the daily limit, the notebook is what you have learned. Translate every one of them.`

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
// On startup it resumes at the last non-user message. This intentionally
// replays only a trailing run of user messages: history ending in an agent or
// system message is treated as answered, while a crash after the user wrote but
// before the head replied remains recoverable. It is a deliberately simple
// journal rule; the cursor advances only after each message has been handled.
func (h *Head) Serve(ctx context.Context) error {
	if h == nil || h.client == nil {
		return errors.New("serve head: nil client")
	}
	if h.store == nil {
		return errors.New("serve head: nil store")
	}

	cursor, err := h.initialCursor()
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
			cursor, err = h.poll(ctx, cursor)
			if err != nil {
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

func (h *Head) initialCursor() (int64, error) { return h.store.LastNonUserMessageSeq() }

func (h *Head) poll(ctx context.Context, cursor int64) (int64, error) {
	for {
		if err := ctx.Err(); err != nil {
			return cursor, err
		}
		messages, err := h.store.Messages("", cursor, messagePageSize)
		if err != nil {
			return cursor, fmt.Errorf("serve head: tail messages: %w", err)
		}
		if len(messages) == 0 {
			return cursor, nil
		}
		for index, message := range messages {
			// Rows the turn before this one folded into itself are answered
			// already; the cursor is what says so.
			if message.Seq <= cursor {
				continue
			}
			if answerable(message) {
				answered, err := h.answerTurn(ctx, foldAhead(messages[index:]))
				if err != nil {
					return cursor, err
				}
				if answered > cursor {
					cursor = answered
				}
			}
			if message.Seq > cursor {
				cursor = message.Seq
			}
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
func (h *Head) postInterrupted(sessionID, partial string) error {
	body := interruptedReply
	if partial != "" {
		body = partial + interruptedTail
	}
	return h.postAgentFloor(sessionID, body, 0, "")
}

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
	if handled, err := h.manageService(user); err != nil {
		return fmt.Errorf("serve head: manage service: %w", err)
	} else if handled {
		return nil
	}
	if handled, err := h.manageCharter(user); err != nil {
		return fmt.Errorf("serve head: manage charter: %w", err)
	} else if handled {
		return nil
	}
	if handled, err := h.manageSurgery(user); err != nil {
		return fmt.Errorf("serve head: manage node surgery: %w", err)
	} else if handled {
		return nil
	}
	// Last deterministic readings before the model gets a vote. Recognized
	// durable language becomes a charter draft here; standing runs before
	// redirect so "whenever/every time" phrasing stays a rule even while work
	// it happens to mention is in flight. Nothing that reaches this point can
	// also be noted as a fact or answered as ordinary chat.
	if handled, err := h.manageStanding(user); err != nil {
		return fmt.Errorf("serve head: draft standing charter: %w", err)
	} else if handled {
		return nil
	}
	if handled, err := h.manageRedirect(ctx, user); err != nil {
		return fmt.Errorf("serve head: manage redirection: %w", err)
	} else if handled {
		return nil
	}
	// Redirection owns work in flight; this owns work already delivered. It has
	// to sit above the control loop rather than inside it, because a correction
	// is not a change to the board — the belt's verbs cannot touch settled work
	// at all — and it has to sit above the router, because the router's own law
	// once read "that's wrong" as a notebook retraction.
	if handled, err := h.manageCorrection(user); err != nil {
		return fmt.Errorf("serve head: manage correction: %w", err)
	} else if handled {
		return nil
	}
	// Everything above is a cue vocabulary, and cue vocabularies end. The
	// control loop is where the novel phrasing goes: the model composes typed
	// tools over the graph, every rule lives inside them, and anything it
	// cannot honestly settle falls through to the router below.
	if handled, err := h.manageControl(ctx, user); err != nil {
		return fmt.Errorf("serve head: manage graph control: %w", err)
	} else if handled {
		return nil
	}

	decision, err := h.route(ctx, user)
	if err != nil {
		// The turn withdrew itself; nothing has been said and nothing is owed
		// here. answerTurn asks it again with everything the person has said.
		if errors.Is(err, errRefold) {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return h.postAgent(user.SessionID, providerErrorReply, 0)
	}

	h.remember(decision.Remember)

	if retraction := decision.Retract; retraction != nil {
		fact, found, readErr := h.store.FactBySeq(retraction.Seq)
		if readErr != nil || !found || fact.Status != store.FactActive {
			decision.Reply = fmt.Sprintf("I couldn't find an active notebook belief #%d to forget.", retraction.Seq)
		} else if err := h.store.QuarantineFact(retraction.Seq, user.Seq,
			store.FactOriginUser); err != nil {
			decision.Reply = fmt.Sprintf("I couldn't retract notebook belief #%d.", retraction.Seq)
		} else {
			return h.postSystem(user.SessionID, "· let go — "+firstLine(fact.Body))
		}
	}

	// Two readings the model makes that are not commands, tried before the
	// command block because both of them act on work that is already there.
	// Neither can invent a target, so a reading that finds nothing to act on
	// simply hands the message back to the ordinary path below.
	if decision.Adjust {
		if handled, adjustErr := h.applyAdjustment(user); adjustErr != nil {
			return adjustErr
		} else if handled {
			return nil
		}
	}
	if decision.Urgent {
		if handled, urgentErr := h.applyUrgency(ctx, user); urgentErr != nil {
			return urgentErr
		} else if handled {
			return nil
		}
	}

	if len(decision.Commands) > 1 {
		return h.spliceWorkOrders(user, decision)
	}

	if decision.Command != nil {
		return h.issueRoutedCommand(user, decision)
	}
	return h.postAgentFloor(user.SessionID, decision.Reply, 0, decision.model)
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
func (h *Head) postAgentFloor(sessionID, body string, commandSeq int64, model string) error {
	if strings.TrimSpace(body) == "" {
		return h.postAgent(sessionID, providerErrorReply, commandSeq)
	}
	return h.postAgentModel(sessionID, body, commandSeq, model)
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

func (h *Head) route(ctx context.Context, user store.Message) (routeDecision, error) {
	client, err := h.clientFor(user)
	if err != nil {
		return routeDecision{}, err
	}
	snapshot, err := h.store.ActiveSnapshot()
	if err != nil {
		return routeDecision{}, fmt.Errorf("read active graph: %w", err)
	}
	recent, err := h.recentThread(user.SessionID, user.Seq)
	if err != nil {
		return routeDecision{}, fmt.Errorf("read recent thread: %w", err)
	}

	threadContext := h.renderThread(recent)
	// Depth is bought in its own budget and only for the jobs this message is
	// about. A message about nothing on the graph adds nothing at all, so the
	// ordinary prompt is unchanged to the byte.
	//
	// It is computed before the board rather than after it, and that ordering is
	// the whole of the dedup: the board is the floor and must never starve, so
	// it still gets its own bytes and its line for every job, but it now knows
	// which of those jobs the block below is about to quote in full and drops
	// only the clause it would have said twice.
	deep, opened := h.renderDeep(user.Body, threadContext)
	graphContext := h.renderGraph(snapshot, user.SessionID, threadContext, opened, time.Now())
	if services := renderServices(h.store); services != "" {
		graphContext += "\n" + services
	}
	if deep != "" {
		graphContext += "\n\n" + deep
	}

	// The one user message is assembled stable-first, and the reason is money.
	// Every endpoint we ride caches by prefix: the bytes before the earliest
	// change are billed at a tenth, everything from that byte onward at full
	// price. So the order is a cost decision, not a rhetorical one. Measured
	// self-knowledge moves with completed jobs and the thread now moves in big
	// steps, so both sit at the front where they can be reused message after
	// message. Below them is the volatile floor: the snapshot ticks with every
	// status, the notebook is retrieved against this message's words, the
	// question-cued blocks appear and vanish with the question, and the spend
	// line moves with every cent — each of them, wherever it sits, invalidates
	// everything after it, so they are gathered together at the bottom where
	// there is nothing left to invalidate but the message itself.
	var body strings.Builder
	if h.knowledge != nil {
		if measured := strings.TrimSpace(h.knowledge()); measured != "" {
			body.WriteString("Measured execution history (evidence for routing priors):\n" + measured + "\n\n")
		}
	}
	body.WriteString("Recent thread before this message:\n" + threadContext)
	body.WriteString("\n\nLive graph snapshot:\n" + graphContext)
	body.WriteString("\n\nNotebook (durable memory across jobs and conversations):\n" + renderNotebook(h.store, user.Body, threadContext))
	// The belt normally answers self-questions, but it needs a client and a
	// model willing to call a tool. When it declined or was never reachable,
	// the router still gets the pages rather than the alternative, which is a
	// fluent invention nobody can tell from a remembered fact.
	if selfQuestionCued(user.Body) {
		if pages := manual.Context(user.Body, manualRouteSections); pages != "" {
			body.WriteString("\n\nAforge manual (the authoritative account of aforge itself; quote its substance, invent nothing):\n" + pages)
		}
	}
	// The clock. Every other block in this prompt is time-ordered and none of
	// them said what time it is, so "yesterday", "this week" and "how long has
	// that been sitting there" were words the head could read and never resolve.
	// It rides in the volatile floor beside the spend line because it moves
	// every minute; at minute resolution it still holds still for the length of
	// an exchange, and there is nothing behind it left to invalidate.
	body.WriteString("\n\n" + nowLine(time.Now()))
	// The spend line is the fastest-moving fact in the prompt, so it is the last
	// thing before the message. It is never dropped: the daily-rail approval
	// flow reads the user's "yes" against it, and the system prompt promises it
	// as the ground truth for what today has cost.
	if h.dailyRailSet {
		if rail, railErr := h.store.DailyRailToday(h.dailyBudgetUSD); railErr == nil {
			line := fmt.Sprintf("today's spend: $%.2f of $%.2f daily rail", rail.Spend, rail.Ceiling)
			if rail.Unlimited {
				line = fmt.Sprintf("today's spend: $%.2f; daily rail unlimited", rail.Spend)
			}
			body.WriteString("\n\n" + line)
		}
	}
	body.WriteString("\n\nCurrent user message (verbatim):\n" + user.Body)
	prompt := body.String()
	messages := []ai.Message{
		// No retrieval cue from the message: voice preferences are standing user
		// style, not query-relevant, and cueing them on the current message made
		// the system prompt a different string every turn — the one place in the
		// whole call that could have been identical from message to message.
		textMessage("system", resident.VoicePrompt(h.store, headSystemPrompt)),
		textMessage("user", prompt),
	}
	if h.supportsImages(client) && len(user.Attachments) > 0 {
		parts := messages[1].Content
		for _, path := range user.Attachments {
			if part, ok := imageContentPart(path); ok {
				parts = append(parts, part)
			}
		}
		messages[1].Content = parts
	}
	// No response-format schema here: measured against the shipped default
	// model, schema-constrained calls came back empty two times in three and
	// took 4-6s, while prompt-shaped JSON parsed three of three at under a
	// second. The defensive decoder below covers the difference.
	// The one call in the turn that is long enough for the person to overtake it,
	// and the last moment at which overtaking costs nothing: everything that
	// acts has either already acted and spoken, or has not begun.
	routeContext, stopWatch := h.watchForFold(ctx)
	defer stopWatch()
	raw := ""
	servedModel := ""
	for attempt := 0; attempt < 2; attempt++ {
		response, err := client.CompleteWithMessages(routeContext, messages, ai.WithMaxTokens(600))
		if err != nil {
			if h.refolding() {
				return routeDecision{}, errRefold
			}
			// One transient failure should not surface as "try again" — the
			// user already tried. Retry once; only a repeat offense escapes.
			if attempt == 0 && routeContext.Err() == nil {
				select {
				case <-routeContext.Done():
					return routeDecision{}, routeContext.Err()
				case <-time.After(400 * time.Millisecond):
				}
				continue
			}
			return routeDecision{}, err
		}
		if response == nil {
			return routeDecision{}, errors.New("provider returned a nil response")
		}
		servedModel = strings.TrimSpace(response.Model)
		raw = strings.TrimSpace(response.Text())
		if raw != "" {
			break
		}
	}
	attribution := replyModel(user, client, servedModel)
	var decision routeDecision
	if err := decodeJSONObject(raw, &decision); err != nil {
		if raw == "" {
			return routeDecision{}, errors.New("provider returned an empty response")
		}
		return routeDecision{Reply: raw, model: attribution}, nil
	}
	decision.normalizeFanOut(user.Body)
	if decision.Command != nil && decision.Command.Kind == routeReflexKind {
		decision.Command.Instruction = user.Body
		decision.enforceConsequences()
	}
	if err := decision.validate(); err != nil {
		// A decoded object whose only defect is its command is not prose. The
		// old answer here was to hand the whole raw object back as the reply,
		// which posted the model's own promise — "it won't fire anymore" — with
		// nothing behind it and no way for the turn below to know. The command
		// travels on to the journaling door instead, which owes the person
		// either the act or an honest sentence and may not post the reply
		// without one. A defect anywhere else — no reply at all, a retraction
		// that contradicts itself — really is prose, and falls through.
		if decision.commandFault() != nil && decision.Retract == nil &&
			strings.TrimSpace(decision.Reply) != "" {
			decision.Reply = strings.TrimSpace(decision.Reply)
			decision.model = attribution
			return decision, nil
		}
		if raw == "" {
			return routeDecision{}, errors.New("provider returned an empty response")
		}
		return routeDecision{Reply: raw, model: attribution}, nil
	}
	decision.Reply = strings.TrimSpace(decision.Reply)
	decision.model = attribution
	return decision, nil
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
	return h.postAgentModel(sessionID, body, commandSeq, "")
}

func (h *Head) postSystem(sessionID, body string) error {
	_, err := h.store.PostMessage(store.Message{
		SessionID: sessionID,
		Role:      store.RoleSystem,
		Body:      body,
		Answers:   h.answering(),
	})
	if err != nil {
		return fmt.Errorf("serve head: post system line: %w", err)
	}
	return nil
}

func (h *Head) postAgentModel(sessionID, body string, commandSeq int64, model string) error {
	_, err := h.store.PostMessage(store.Message{
		SessionID:  sessionID,
		Role:       store.RoleAgent,
		Body:       body,
		CommandSeq: commandSeq,
		Model:      strings.TrimSpace(model),
		Answers:    h.answering(),
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

type routeDecision struct {
	Reply    string           `json:"reply"`
	Command  *routeCommand    `json:"command"`
	Commands []*routeCommand  `json:"commands"`
	Remember *routeMemory     `json:"remember"`
	Retract  *routeRetraction `json:"retract"`
	// Adjust and Urgent are readings of the message, not commands: what to do
	// with them is the head's, because both of them act on work that already
	// exists and the model is not shown the ids that work is addressed by.
	Adjust bool `json:"adjust"`
	Urgent bool `json:"urgent"`
	// Fresh is the person asking for this one to be worked out from scratch
	// rather than the way it has been done before. It belongs on this surface
	// for the same reason remember and retract do: it is a reading of what a
	// sentence MEANT, and the reading is made here or it is made by a phrase
	// list somewhere downstream that has to be taught every spelling. "Don't
	// use the template this time", "plan this one properly", "start over on
	// this" are all the same intent and no list will hold them.
	Fresh bool `json:"fresh"`
	model string
}

// fanOutLimit bounds one message's work orders. Past it the message is not a
// handful of asks, it is a list — and a list is one job that enumerates, which
// is what the compiler already does well. Falling back to a single order there
// loses nothing: the user's own words, with every item in them, still travel.
const fanOutLimit = 6

// normalizeFanOut settles what the list actually means before anything is
// journaled. Only new work fans out: steering, cancelling and the rest name one
// target each and a list of them is a different feature entirely. A list that
// survives with one entry is simply that entry, and a list that survives with
// none leaves the ordinary command alone.
func (decision *routeDecision) normalizeFanOut(body string) {
	if len(decision.Commands) == 0 {
		return
	}
	orders := make([]*routeCommand, 0, len(decision.Commands))
	seen := make(map[string]bool, len(decision.Commands))
	for _, order := range decision.Commands {
		if order == nil {
			continue
		}
		instruction := strings.TrimSpace(order.Instruction)
		kind := strings.TrimSpace(order.Kind)
		if instruction == "" || seen[instruction] {
			continue
		}
		if kind != "" && kind != string(store.CommandSplice) && kind != routeReflexKind {
			continue
		}
		seen[instruction] = true
		orders = append(orders, &routeCommand{
			Kind: string(store.CommandSplice), Instruction: instruction,
		})
	}
	if len(orders) > fanOutLimit {
		orders = []*routeCommand{{
			Kind: string(store.CommandSplice), Instruction: strings.TrimSpace(body),
		}}
	}
	switch len(orders) {
	case 0:
		decision.Commands = nil
	case 1:
		decision.Command = orders[0]
		decision.Commands = nil
	default:
		decision.Command = nil
		decision.Commands = orders
	}
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

type routeRetraction struct {
	Seq int64 `json:"seq"`
}

type routeCommand struct {
	Kind        string `json:"kind"`
	Target      string `json:"target"`
	Instruction string `json:"instruction"`
}

func (decision routeDecision) validate() error {
	if strings.TrimSpace(decision.Reply) == "" {
		return errors.New("empty reply")
	}
	if decision.Retract != nil {
		if decision.Retract.Seq <= 0 || decision.Command != nil || decision.Remember != nil {
			return errors.New("invalid or conflicting retraction")
		}
		return nil
	}
	return decision.commandFault()
}

// commandFault is the half of validation that is about the command alone.
//
// It is separated from validate because the two halves have different remedies.
// A decision that is malformed as a whole is not a decision — the model wrote
// prose and the prose is the reply. A decision whose command is malformed is an
// intention stated badly: the model meant to act, said so in the reply, and got
// one field wrong. Collapsing the second case into the first published the
// promise and dropped the act, which is the one failure the thread must never
// contain. So this names the fault and the head's turn decides what to do with
// it — resolve it, ask about it, or say plainly that it did not happen.
func (decision routeDecision) commandFault() error {
	if decision.Command == nil {
		return nil
	}
	_, reflex, ok := commandKind(decision.Command.Kind)
	if !ok {
		return fmt.Errorf("unknown command kind %q", decision.Command.Kind)
	}
	if strings.TrimSpace(decision.Command.Instruction) == "" {
		return errors.New("empty command instruction")
	}
	if reflex && consequenceGated(decision.Command.Instruction) {
		return errors.New("consequential instruction cannot use reflex")
	}
	if decision.Command.Kind != string(store.CommandSplice) && !reflex && strings.TrimSpace(decision.Command.Target) == "" {
		return fmt.Errorf("%s command has no target", decision.Command.Kind)
	}
	if reflex && strings.TrimSpace(decision.Command.Target) != "" {
		return errors.New("reflex command cannot target existing work")
	}
	return nil
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

func (decision *routeDecision) enforceConsequences() {
	if decision.Command != nil && decision.Command.Kind == routeReflexKind &&
		consequenceGated(decision.Command.Instruction) {
		decision.Command.Kind = string(store.CommandSplice)
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

// renderGraph is the board: one line per job, breadth over depth.
//
// It sits behind beltAddressable, the same ownership membrane the belt and the
// deep slice sit behind, and that agreement is the fix for a live failure. Three
// rules over one table meant a charter-fired job passed the router's snapshot,
// passed the deep slice, and failed the control loop's board — so the head
// described the job in one sentence and answered "there is no work of the user's
// with that id" to "stop it" in the next. One membrane, and the membrane is the
// documented one: OriginTrigger work is the user's, because it came from a
// charter they ratified.
//
// thread and opened are what the rest of the prompt has already said. A result
// line the user can read in the thread above, or one the deep slice is about to
// quote in full below, is the same finding stated twice in one prompt — which
// costs budget and reads to the model as corroboration.
// It is a method for one reason: a board row is not always a read of the node it
// names. A node whose remainder was re-planned elsewhere has its current truth in
// the graph rather than in its own summary, and following that takes the store.
func (h *Head) renderGraph(snapshot store.Snapshot, sessionID, thread string, opened map[string]bool, now time.Time) string {
	if len(snapshot.Nodes) == 0 {
		return "(no active nodes)"
	}
	// Live work first, then the freshest history. The snapshot budget is
	// finite and the store returns creation order, so a long-lived graph fed
	// the head months of settled nodes before the job running right now —
	// which is how the head answered "I don't see anything labeled webgpu"
	// about a subtree the rail was rendering 49 minutes into its run. The
	// one thing the snapshot must never truncate away is what is happening.
	nodes := make([]store.Node, 0, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		if beltAddressable(node) {
			nodes = append(nodes, node)
		}
	}
	if len(nodes) == 0 {
		return "(no active nodes)"
	}
	rank := func(node store.Node) int {
		switch {
		case node.Status == store.Running || node.Status == store.Claimed:
			return 0
		case node.Status == store.Pending:
			return 1
		case node.FoldRoot:
			// Packed history: reachable, but last in line for the budget.
			return 3
		default:
			return 2
		}
	}
	// Jobs before their parts, and the reason is that the flat list was a lie of
	// omission. Sixteen leaves and four roots rendered as twenty peers left the
	// model to invent which of them were the workstreams a person would name —
	// while the belt's own board, one row per job with its subtree rolled up,
	// sat behind a trigger the router never fires. So the board leads with the
	// jobs and their counts, and a part says whose part it is.
	byID := make(map[string]store.Node, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		byID[node.ID] = node
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		if ri, rj := boardJobRoot(nodes[i], byID), boardJobRoot(nodes[j], byID); ri != rj {
			return ri
		}
		ri, rj := rank(nodes[i]), rank(nodes[j])
		if ri != rj {
			return ri < rj
		}
		if ri >= 2 {
			return nodes[i].FinishedAt.After(nodes[j].FinishedAt)
		}
		return nodes[i].CreatedSeq > nodes[j].CreatedSeq
	})
	children := make(map[string][]string, len(byID))
	for _, node := range snapshot.Nodes {
		if _, ok := byID[node.Parent]; ok {
			children[node.Parent] = append(children[node.Parent], node.ID)
		}
	}
	var rendered strings.Builder
	for _, node := range nodes {
		brief := firstLine(node.Brief)
		if brief == "" {
			brief = "(no brief)"
		}
		// A settled node's first result line is what "what did you find?"
		// gets answered from; without it the head can only recite statuses.
		// Unless it is already in this prompt, in which case the second copy
		// teaches nothing: the deep slice below is about to quote this job in
		// full, or announceNode already posted the same summary into the thread
		// above and renderThread has rendered it.
		result := firstLine(h.jobResult(node))
		if opened[node.ID] || deepAlreadyInThread(thread, result) {
			result = ""
		}
		line := fmt.Sprintf("- %s | %s | %s", node.ID, node.Status, brief)
		// The board was sorted by time and never labelled by it, so the head was
		// handed an ordering it could not read as one and asked "what did you do
		// yesterday" with no way to tell yesterday from an hour ago. The age is
		// the belt's own age — coarse on purpose, so a row does not rewrite
		// itself between two messages the way a running clock would.
		if age := store.AgeLabel(node.FinishedAt, now); age != "" {
			line += " | finished " + age
		}
		if result != "" {
			line += " | result: " + result
		}
		if boardJobRoot(node, byID) {
			// The rolled-up counts are what turns a status into a workforce: one
			// row that says how many people are on this job right now, how many
			// steps are waiting, and what has already broken. A settled job with
			// nothing open adds no clause at all.
			if counts := boardSubtreeCounts(node, byID, children); counts != "" {
				line += " | " + counts
			}
		} else if owner := boardOwnerLabel(node, byID); owner != "" {
			// A part says whose part it is, by the job's own name — the same name
			// the thread, the cards and the receipts use.
			line += " | part of " + owner
		}
		if crossSession(node, sessionID) {
			line += crossSessionMark
		}
		line += "\n"
		if rendered.Len()+len(line) > maxGraphContextBytes-len(snapshotTruncatedMark) {
			rendered.WriteString(snapshotTruncatedMark)
			break
		}
		rendered.WriteString(line)
	}
	return strings.TrimSpace(rendered.String())
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
