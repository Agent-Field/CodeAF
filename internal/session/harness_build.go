package session

// BUILDING ONE, from the same sentence one is offered.
//
// harness.go is the question "did you mean the harness that already does this?".
// This file is the other half: "make me a harness that does this" — the turn
// that has no harness to match against yet, because the thing it is asking for
// does not exist.
//
// Until this file, that sentence went to the model, which answered it the way it
// answers anything: with prose about harnesses. The designer — the meta-guide in
// internal/subharness/prompts, the review pass that patches its draft, the
// validator that refuses a page the runner could not run — lived in a
// development rig (cmd/harness-design) and could not be reached from a
// conversation at all. The registry was a thing a person could run and could not
// fill.
//
// FOUR LAWS HOLD THE CHAT SIDE OF IT.
//
//   - THE INTENT IS THE MODEL'S JUDGEMENT, AND IT IS SPELLED AS A TOOL. This
//     path used to be entered by an anchored cue — "make|build|create|design a
//     (sub)harness for|to|that X" and nothing else — which was cheap and could
//     only ever read the sentences somebody happened to phrase that way. It is
//     now [Agent.buildHarnessTool] on the belt (tools_harness.go), so the
//     deciding is done once, by the thing that has the whole conversation in
//     front of it, and the goal it passes is a brief it wrote rather than the
//     tail of a sentence. Nothing below this line changed with it: the same job,
//     the same card, the same registry.
//   - THE TURN DOES NOT WAIT, AND THE DESIGN IS A TASK. A design is two model
//     calls against a twenty-five-thousand-token guide, and it is followed by a
//     QUESTION nobody may be at the keyboard for. Held in the turn loop that
//     would be a conversation frozen for a minute on work the person can watch
//     happen; so the turn ends the moment the design starts. WHERE it happens
//     is the work graph: harness_task.go admits a node whose body is this file,
//     so the design has a row, a room, a journal, an id and a stop, and the card
//     still arrives on the standing lane ([Agent.HarnessDesigns]) whenever it is
//     ready.
//   - NOTHING IS SAVED WITHOUT AN ANSWER. What comes back is a PAGE, drawn as
//     the card every surface shares (subharness.CardLines), and the registry is
//     untouched until somebody says yes. A design nobody answered is a design
//     that changed nothing.
//   - IT IS SILENT WHERE THE OFFER IS SILENT. No runner, no store, or no surface
//     that answers questions and this path does not exist — the tool is left off
//     the belt entirely ([Agent.canDesignHarness]), so the model does not have
//     the verb rather than having it and being refused.
//
// WHAT THE DESIGNER IS TOLD is not written here. The brief is the shared
// document (internal/subharness/prompts/designer.md) with THIS build's machinery
// rendered into it — the caps, both ladders, and the tool belt a harness may
// actually reach on this surface — so a guide that has drifted from the package
// it describes fails before the first request instead of in a bad design.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/aforge-v2/internal/subharness/prompts"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	// harnessDesignWindow bounds THE WRITING OF ONE PAGE — the design turn, the
	// retries under it, and the review pass — and nothing after that. It is
	// generous because a long guide read by a reasoning model is slow, and it is
	// bounded at all because a model call that never answers would otherwise hold
	// the node open forever.
	//
	// IT DOES NOT COVER THE CARD, and it used to, which was a bug with a person on
	// the other end of it: a design that wrote its page in ten minutes and then
	// waited for somebody to come back from lunch was collected at thirty minutes
	// and reported as having run out of time with nothing saved, while the page sat
	// finished in its own room. A card is a question on somebody's screen, and the
	// only things that may end one are their answer, their ✕, and the process
	// closing (harness_task.go's designHarnessNode).
	//
	// AND IT IS SPENT ONCE PER PAGE AND NOT ONCE PER JOB. A design is a
	// conversation — the person says what to change and the page is written again
	// — so every rewrite gets this window whole (harness_task.go's designRun.round
	// makes the argument). One clock over the whole job would expire in the middle
	// of somebody's third round, which is to say it would punish the designs that
	// were being taken seriously.
	harnessDesignWindow = 30 * time.Minute

	// harnessDesignRetries is how many times a refused design is handed its own
	// error back. It is the rig's number, and the ladder under it is the rig's
	// too: salvage costs nothing, a repair turn costs one small call, and only
	// then is a whole attempt spent re-reading the guide.
	harnessDesignRetries = 2

	// The two completion budgets, from the rig that measured them. The design
	// turn writes a whole page; the review turn writes findings and a PATCH and
	// never re-emits the page, but on a reasoning model its thinking comes out of
	// the same budget — a critic that cannot afford its own answer is the most
	// expensive kind of nothing, because the design is already paid for.
	//
	// THE DESIGN BUDGET IS THE CRITIC'S LESSON, LEARNED TWICE. 8000 was measured
	// against a model that answers straight, and the page it has to write is only
	// about fifteen hundred tokens. But the guide's own PART TWO orders the pair
	// table walked "PAIR BY PAIR, IN WRITING, BEFORE YOU DRAW A SINGLE EDGE" —
	// that is a request for long thinking, and on a reasoning model the thinking
	// is metered from this same number. Ceiling reached, a reasoning model comes
	// back EMPTY: the content is null, the whole completion went into the
	// thinking, and the attempt is spent for nothing. The rig has named that
	// failure for a while (cmd/harness-design's openrouter.go) and it was
	// measured again here — deepseek-v4-flash, rendered against this belt,
	// answering with nothing twice in three attempts.
	//
	// HEADROOM IS NOT THE WHOLE ANSWER, and the number should not be raised
	// again without measuring. A model determined to deliberate will use whatever
	// it is given, and the room bought here is also latency: a design turn is
	// bounded by the session's own providerTimeout, so a budget large enough to
	// fund an unbounded deliberation buys a transport timeout instead of a page.
	// The other half of the fix is in the guide, which now says plainly that the
	// thinking and the answer come out of one budget and the derivation belongs
	// IN the reply.
	harnessDesignTokens = 16000
	harnessReviewTokens = 10000

	// harnessToolAbout bounds one tool's line in the belt the guide is shown. The
	// wire descriptions are paragraphs — they are written for a model deciding
	// whether to CALL the tool — and what a designer needs is a name and a
	// sentence.
	harnessToolAbout = 160
)

// harnessDesigningWord is the Hint on EventHarnessDesign: one word, because the
// event's own text is the goal and a surface draws it as a note.
const harnessDesigningWord = "designing"

// ── who writes it ───────────────────────────────────────────────────────────

// designerModel is [Agent.harnessDesignModel] with the lock taken, for the
// caller that needs the answer BEFORE the design starts (tools_harness.go's
// build_harness, whose note names the model). The two are one function
// deliberately: a second ladder written out here is a second answer to "who
// designs this", and the whole point of asking early is that the note and the
// design agree.
func (a *Agent) designerModel(named string) string {
	return a.designerCall(named).model
}

func (a *Agent) designerCall(named string) roleRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.harnessDesignCall(named)
}

// harnessDesignModel is what the design AND ITS REVIEW think with — one model
// for both, because they are two halves of writing one page.
//
// The turn's word wins when it named one this install has, exactly as it does
// for a run ([orchestrateRoleModel] makes the whole argument). With nothing
// named it is RoleDesigner's, which sits high: this pair writes a page that is
// SAVED and picked off a menu by everybody afterwards, so a bad one is a wrong
// answer with a name on it rather than a wrong answer once. The ladder's floor
// is the session's own model, so an install with no tiers set designs as it
// always did. It is called with a.mu held.
func (a *Agent) harnessDesignModel(named string) string {
	return a.harnessDesignCall(named).model
}

func (a *Agent) harnessDesignCall(named string) roleRequest {
	if named = strings.TrimSpace(named); named != "" {
		return roleRequest{model: named}
	}
	if call, err := roles.ResolveCall(roles.Source(a.config.RolesSource), roles.RoleDesigner, a.model); err == nil {
		return newRoleRequest(call)
	}
	return roleRequest{model: a.model}
}

// roleRequest is the provider-facing half of a resolved role call. Invalid or
// empty effort remains absent, preserving the old request context exactly.
type roleRequest struct {
	model  string
	effort provider.Effort
}

func newRoleRequest(call roles.Call) roleRequest {
	effort, ok := provider.ParseEffort(call.Effort)
	if !ok {
		effort = provider.EffortNone
	}
	return roleRequest{model: call.Model, effort: effort}
}

func (r roleRequest) context(ctx context.Context) context.Context {
	if r.effort == provider.EffortNone {
		return ctx
	}
	return provider.WithConfiguredReasoningEffort(ctx, r.effort)
}

// emitHarness puts one design event in front of whoever is watching.
//
// IT IS THE STANDING LANE AND NOT THE TURN'S HUB, which is where this fan-out
// differs from [Agent.emitTaskUpdate]. A task update is about work the turn
// handed off while the turn is still going, so it belongs on both. EVERY event
// here is about work that outlives its turn by construction — the turn ends the
// moment the design starts — so the hub would carry at most the first of them,
// and a surface reading both lanes would draw that one twice.
func (a *Agent) emitHarness(event Event) {
	a.mu.Lock()
	watchers := make([]*eventStream, len(a.harnessWatchers))
	copy(watchers, a.harnessWatchers)
	a.mu.Unlock()

	for _, watcher := range watchers {
		watcher.send(event)
	}
}

// HarnessDesigns is the standing subscription to what this session's harness
// designer is doing: the design starting, the card asking whether to keep what
// it wrote, and the notes that say a design failed, was declined or was saved.
//
// It exists for [Agent.TaskUpdates]'s reason. A design outlives the turn that
// asked for it — that is the point of it — so its most important event has no
// Submit channel to arrive on. A surface that draws harness cards subscribes
// once at startup; a surface that does not never calls this and pays nothing.
//
// The channel is never closed by a turn ending, and a surface holds it for the
// life of the session.
func (a *Agent) HarnessDesigns() <-chan Event {
	lane, _ := a.WatchHarnessDesigns()
	return lane
}

// WatchHarnessDesigns is [Agent.HarnessDesigns] with a way to stop, for
// [Agent.WatchTaskUpdates]' reason and on its terms: same subscription, stop
// takes the watcher off the list and ends its pump, never nil, and calling it
// twice is calling it once.
func (a *Agent) WatchHarnessDesigns() (<-chan Event, func()) {
	stream := newEventStream()
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		stream.close()
		return stream.out, func() {}
	}
	a.harnessWatchers = append(a.harnessWatchers, stream)
	// A CARD ALREADY STANDING IS REPLAYED TO THE NEWCOMER. A design card is
	// emitted once, at the moment the page lands, and a subscriber that attached
	// after that moment would otherwise wait forever on a question that is
	// already up — which is the ordinary case after a restart, where recovery
	// raises a carried-over card at construction, before any surface has
	// subscribed. Only design cards live in this map with an event on them; the
	// run-this-harness offers keyed beside them carry none and replay nothing.
	for _, ask := range a.harnessAsks {
		if ask.card.Harness != nil {
			stream.send(ask.card)
		}
	}
	// AND SO IS A SUBHARNESS PROPOSAL, for exactly the same reason and in the
	// commonest case of all: the card holds a turn for up to a quarter of an
	// hour (tools_subharness.go), which is long enough for the person to put the
	// conversation behind home and come back to it — and coming back is a fresh
	// subscription. The offers are replayed in the order they were raised, so a
	// surface handed two of them draws them in the order the person was asked.
	for _, card := range a.standingSubharnessCardsLocked() {
		stream.send(card)
	}
	a.mu.Unlock()
	var once sync.Once
	return stream.out, func() {
		once.Do(func() {
			a.mu.Lock()
			a.harnessWatchers = dropWatcher(a.harnessWatchers, stream)
			a.mu.Unlock()
			stream.leave()
		})
	}
}

// ── the card ────────────────────────────────────────────────────────────────

// harnessWord is what ends one wait on a design card, and EXACTLY ONE of its
// halves is filled.
//
// A card used to have two answers, which is why the wait used to be a
// [harnessAnswer] and nothing else. It has three now: save it, drop it, and the
// one a person reaches for most — say what is wrong with it. The third is not a
// yes and it is emphatically not a no, and a wait that flattened it into either
// would either save a page nobody approved or throw away a design somebody was
// in the middle of working on.
// harnessAsk is one standing question on [Agent.harnessAsks]: the channel its
// answer arrives on, and — for a DESIGN card only — the card event itself, kept
// so a watcher that subscribes while the question stands can be handed it
// ([Agent.WatchHarnessDesigns]). A run-this-harness offer and a route judgement
// share the map and carry no card: their questions live in a turn, and a turn
// has no late subscribers.
type harnessAsk struct {
	answers chan harnessAnswer
	card    Event
}

type harnessWord struct {
	// answer is the person's, through [Agent.ResolveHarness], and it is the only
	// half that decides anything: run saves the page, and its absence drops it.
	answer harnessAnswer
	// change is the person's own words, verbatim, when this wait ended in a
	// request for the page to be rewritten instead (harness_task.go's
	// revise_design). Nothing has landed when this is filled — the registry is
	// untouched and the page in hand is what the next round rewrites.
	change string
}

// askHarnessDesign raises the preview card and waits for whatever answers it.
//
// It is [Agent.askHarness] with two differences. The wait is on the DESIGN
// NODE's own context rather than a turn's, because there is no turn; the id was
// minted when the job started, so the question a surface answers is the job a
// person watched begin. And it watches a SECOND lane beside the answers — the
// one the design's own thread asks for a rewrite on ([TaskNode.openRevisions]) —
// because the room this card is raised into is a place where a person can also
// just say what they want different.
//
// THE CONTEXT IT IS GIVEN CARRIES NO CLOCK, and the caller is written to keep it
// that way (harness_task.go's designHarnessNode). A deadline here is a deadline
// on a person reading a card, and when it fired it took the answer channel with
// it: the card stayed drawn in the feed, its `enter` did nothing, and the page —
// written, valid, minutes of model time — was gone. The wait ends when they
// answer, when they ask for a change, when they stop the node, or when the
// session closes.
//
// WHICHEVER ARRIVES FIRST WINS, AND THE OTHER FINDS NOTHING. Both doors take the
// question out of [Agent.harnessAsks] before they act on it — ResolveHarness
// deletes it under the lock, and the revision branch below calls
// [Agent.forgetHarness] at the instant it takes the words — so a person pressing
// save while their thread is calling revise_design gets exactly one of the two,
// and the loser is dropped the way every late answer on this lane is dropped.
func (a *Agent) askHarnessDesign(ctx context.Context, node *TaskNode, page subharness.Harness, model string, changes <-chan string) (harnessWord, error) {
	id := node.id
	answers := make(chan harnessAnswer, 1)
	// The page travels on the event and this is its only copy: nothing else
	// holds it, and whether it is ever written down is what the answer decides.
	carried := page
	card := Event{
		Kind:    EventHarnessDesignDone,
		ID:      id,
		Text:    page.Id.Name,
		Hint:    page.Id.Desc,
		Model:   model,
		Harness: &carried,
		// Which node to go and watch, for the one subscriber that never saw the
		// design begin — a surface that attached after this card went out reads
		// the task off the replayed event ([Agent.WatchHarnessDesigns]).
		Task: &TaskNotice{ID: id},
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return harnessWord{}, errAgentClosed
	}
	if a.harnessAsks == nil {
		a.harnessAsks = make(map[uint64]harnessAsk, 1)
	}
	// THE CARD IS KEPT BESIDE THE ANSWER CHANNEL for as long as the question
	// stands, so a watcher that subscribes late — above all the surface of a
	// session that RESTORED this design from its checkpoint, which subscribes
	// moments after recovery raised the card — is handed the standing question
	// instead of a silence ([Agent.WatchHarnessDesigns]).
	a.harnessAsks[id] = harnessAsk{answers: answers, card: card}
	a.mu.Unlock()

	a.emitHarness(card)

	select {
	case answer := <-answers:
		return harnessWord{answer: answer}, nil
	case change := <-changes:
		// THE CARD COMES DOWN IN THE SAME BREATH THE CHANGE IS TAKEN. The page it
		// is about is about to stop existing, so a card left standing would be a
		// save key over a draft that has been replaced — and the answer it took
		// would write the wrong page into the registry. The question is forgotten
		// first and the withdrawal is announced second, in that order, so that a
		// surface acting on the event can never race an answer back in through a
		// door that is already shut.
		a.forgetHarness(id)
		a.emitHarness(Event{Kind: EventHarnessDesignRevising, ID: id, Text: change, Model: model})
		return harnessWord{change: change}, nil
	case <-ctx.Done():
		a.forgetHarness(id)
		return harnessWord{}, ctx.Err()
	}
}

// saveHarness writes an approved page and makes it reachable from the next
// sentence.
//
// THE VERSION IS THE STORE'S TO MINT. The page arrives with whatever the
// designer wrote in it — usually nothing, because the guide says to omit it —
// and it is zeroed here so that a name nobody has used lands as v1 and a name
// that exists lands as the next version. A design that asked to be v1 over a
// registry that already holds v3 would be refused for saying so, which is a good
// design lost to a field the designer was told not to fill.
func (a *Agent) saveHarness(page subharness.Harness, cues []string) (subharness.Harness, error) {
	store := a.config.HarnessStore
	if store == nil {
		return subharness.Harness{}, errors.New("there is no registry to save into")
	}
	page.Id.Version = 0
	saved, err := store.Save(page)
	if err != nil {
		return subharness.Harness{}, err
	}
	a.registerHarness(subharness.Entry{
		Name:        saved.Id.Name,
		Description: saved.Id.Desc,
		// THE CUES ARE WHY THIS IS NOT A RE-READ OF THE STORE. A page has nowhere
		// to put them (internal/subharness's Entry says so), so the designer's own
		// trigger vocabulary exists exactly once — in the envelope that carried the
		// page — and dropping it here would leave a harness reachable only by its
		// own name.
		Cues:     cues,
		Revision: saved.Id.Version,
	})
	return saved, nil
}

// registerHarness adds one saved harness to what detection reads, replacing an
// earlier entry of the same name: that is the same harness at a later version,
// and two entries would score the same sentence twice.
func (a *Agent) registerHarness(entry subharness.Entry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for at, have := range a.harnessAdded {
		if have.Name == entry.Name {
			a.harnessAdded[at] = entry
			return
		}
	}
	a.harnessAdded = append(a.harnessAdded, entry)
}

// ── the design ──────────────────────────────────────────────────────────────

// harnessDesign is the design turn's envelope: the page, plus the three things a
// page has nowhere to put.
//
// CUES AND JUSTIFICATION LIVE OUTSIDE THE PAGE because subharness.Harness has no
// field for either and Decode refuses unknown fields — a designer that wrote its
// trigger vocabulary into the page would produce a page that cannot be read
// back. DERIVATION is outside for the same reason and is checked rather than
// kept: subharness.CheckDerivation holds the table against the edges, so a
// designer that called two jobs independent and then drew one into the other is
// refused by its own homework.
type harnessDesign struct {
	Cues          []string                `json:"cues"`
	Justification string                  `json:"justification"`
	Derivation    []subharness.Derivation `json:"derivation,omitempty"`
	Harness       json.RawMessage         `json:"harness"`
}

// harnessFinding is one thing the critic found and which of its passes found it.
type harnessFinding struct {
	Pass string `json:"pass"`
	Text string `json:"text"`
}

// harnessRevision is the review turn's envelope: what the critic found, the
// PATCH it wrote, and its own count of what the two versions cost.
//
// There is no `harness` field and that is the point. The ops ARE the change, so
// the page is never re-emitted and a transcription slip cannot damage text
// nobody reviewed. `cues` and `justification` are optional for the same reason:
// omitted means the draft's, unchanged.
type harnessRevision struct {
	Findings []harnessFinding `json:"findings"`
	Ops      []subharness.Op  `json:"ops"`
	Calls    struct {
		Draft   int `json:"draft"`
		Revised int `json:"revised"`
	} `json:"calls"`
	Cues          []string `json:"cues,omitempty"`
	Justification string   `json:"justification,omitempty"`
}

// harnessAccepted is a page that passed the whole law, with the two things a
// page has nowhere to keep: the cue vocabulary that reaches it, and the
// designer's own account of why it is shaped this way.
//
// IT IS CARRIED RATHER THAN UNPACKED AND DROPPED, and that is what changed when
// a design stopped being over at the first card. The person may ask for the page
// to be CHANGED, and a rewrite is handed the envelope the designer last wrote as
// the thing it is rewriting ([Agent.revisePage]) — so a justification thrown away
// here would be a designer shown a page it has to reconstruct its own reasoning
// about before it can change one step of it.
type harnessAccepted struct {
	page          subharness.Harness
	cues          []string
	justification string
}

// designPage is the first draft, in the shape this file has always answered
// with. It is [Agent.draftPage] unpacked, and it exists because the two things a
// caller wants out of a design are the page and the cues.
func (a *Agent) designPage(ctx context.Context, goal, model string, seat designSeat) (subharness.Harness, []string, error) {
	accepted, err := a.draftPage(ctx, goal, model, seat)
	return accepted.page, accepted.cues, err
}

// draftPage writes a harness for a goal, from nothing.
func (a *Agent) draftPage(ctx context.Context, goal, model string, seat designSeat) (harnessAccepted, error) {
	designer, reviewer, err := a.harnessBriefs()
	if err != nil {
		return harnessAccepted{}, err
	}
	return a.writeHarness(ctx, harnessOpening(designer, goal), reviewer, goal, model, seat)
}

// harnessOpening is the two messages every design starts from: the guide, and
// the goal. It is a function because a REWRITE starts from the same two and then
// says what happened next ([Agent.revisePage]) — a rewrite whose history began at
// the change would be a designer asked to alter a page it was never given the
// brief for.
func harnessOpening(designer, goal string) []ai.Message {
	return []ai.Message{
		textMessage("system", designer),
		textMessage("user", "THE GOAL:\n\n"+goal+"\n\nDesign the sub-harness for it."),
	}
}

// revisePage writes the harness AGAIN, with one change in it, and holds the
// result to exactly the law the draft passed.
//
// THE HISTORY IS THE STORY OF WHAT ACTUALLY HAPPENED, which is why it is built
// out of the same opening: the guide, the goal, the envelope the designer wrote,
// and then the person asking for something different. A designer reading that is
// in the position it is really in — it wrote a page, somebody read it, and they
// want one thing about it changed — rather than being handed a page out of
// nowhere and told to edit it.
//
// IT IS NOT A PATCH AND THE ASK SAYS SO. The review pass patches, because it is
// a critic working over a draft nobody has read; a person's change is a change to
// the harness, and what comes back is the whole envelope again, held to
// [Agent.acceptHarness] and the retry ladder like any other design. That is the
// whole reason this and [Agent.draftPage] end in the same function.
func (a *Agent) revisePage(ctx context.Context, goal, model string, standing harnessAccepted, change string, seat designSeat) (harnessAccepted, error) {
	designer, reviewer, err := a.harnessBriefs()
	if err != nil {
		return harnessAccepted{}, err
	}
	written, err := harnessEnvelope(standing)
	if err != nil {
		return harnessAccepted{}, err
	}
	history := append(harnessOpening(designer, goal),
		textMessage("assistant", written),
		textMessage("user", harnessRevisionAsk(change)))
	return a.writeHarness(ctx, history, reviewer, goal, model, seat)
}

// harnessEnvelope is a page that already passed, written back out in the shape
// the designer answers in — so that the assistant turn in a rewrite's history is
// a reply the designer could have sent, rather than a paraphrase of one.
func harnessEnvelope(accepted harnessAccepted) (string, error) {
	page, err := subharness.Encode(accepted.page)
	if err != nil {
		return "", err
	}
	written, err := json.Marshal(harnessDesign{
		Cues:          accepted.cues,
		Justification: accepted.justification,
		Harness:       json.RawMessage(page),
	})
	if err != nil {
		return "", err
	}
	return string(written), nil
}

// harnessRevisionAsk is the person's change, put to the designer.
//
// THEIR WORDS GO ACROSS VERBATIM AND ARE NOT SUMMARISED. What somebody says
// about a page they have just read is the most specific thing anybody will ever
// say about this design, and a thread that rewrote it into its own vocabulary
// first would be handing the designer its own reading in place of the request.
//
// AND THE STANDING LAW IS RESTATED, because it is the one thing about this turn
// that differs from the review pass: what comes back REPLACES the page above it,
// so it has to be the whole envelope and not a patch.
func harnessRevisionAsk(change string) string {
	return "The person read that harness and asked for this change, in their own words:\n\n" + change +
		"\n\nRewrite the harness so that it does that. Change nothing they did not ask about — the rest of the page is one they have already read and accepted. " +
		"Reply with the WHOLE ENVELOPE again — one JSON object with the keys cues, justification, derivation, harness — because what you send back REPLACES the page above rather than patching it, and it is held to exactly the same law."
}

// writeHarness is the two stages every design goes through, whether it is the
// first draft or the fourth rewrite: a design held to the law, then one review
// pass that may improve it.
//
// THE HISTORY IS KEPT ACROSS ATTEMPTS. A designer shown its own refused page AND
// the validator's exact sentence is being asked to repair what it wrote; one
// shown only the error is being asked to guess again.
//
// DRAFT AND REWRITE SHARE THIS FUNCTION AND NOT A COPY OF IT. The gauntlet is
// the bargain the whole file makes — the strict envelope, the validator, the
// derivation table, this surface's own lint, three attempts, one review — and a
// second path for rewrites would be a second answer to "what is a legal page",
// drifting from this one the first time either was touched. What differs between
// the two is the history they open with, and that is all that differs.
func (a *Agent) writeHarness(ctx context.Context, history []ai.Message, reviewer, goal, model string, seat designSeat) (harnessAccepted, error) {
	var (
		draft harnessDesign
		page  subharness.Harness
		err   error
	)
	for tries := 0; ; tries++ {
		var raw string
		draft, page, raw, err = a.designHarnessOnce(ctx, history, model, goal, tries+1, seat)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return harnessAccepted{}, ctx.Err()
		}
		// THE STORY OF A DESIGN THAT TOOK FOUR ATTEMPTS IS THE THREE REFUSALS, and
		// this is where they are written down — one sentence each, in the words the
		// law refused it with ([harnessRefusedNote]). It is said for the LAST attempt
		// too, before this function gives up: a journal that stopped narrating one
		// refusal short would end on a design that simply vanished.
		seat.noted(harnessRefusedNote(tries+1, err))
		if tries >= harnessDesignRetries {
			return harnessAccepted{}, fmt.Errorf("no valid design in %d attempts: %w", tries+1, err)
		}
		reason := "malformed draft"
		if strings.Contains(err.Error(), "ran out of completion budget") || strings.Contains(err.Error(), "stopped in the middle") {
			reason = "truncated draft"
		}
		a.emitHarness(Event{Kind: EventHarnessProgress, ID: seat.id, Goal: goal, Phase: "designing", Attempt: tries + 1, Attempts: harnessDesignRetries + 1, Hint: "retrying · " + reason})
		// THE REFUSED PAGE GOES BACK WITH THE REFUSAL, but only when there IS
		// one. A model that spent its whole budget thinking answered with
		// nothing, and an empty assistant turn is a message with no content in
		// it — noise at best, and refused outright by some endpoints.
		if strings.TrimSpace(raw) != "" {
			history = append(history, textMessage("assistant", raw))
		}
		history = append(history,
			textMessage("user", "That harness was REFUSED:\n\n"+err.Error()+
				"\n\nFix exactly that and reply with the whole envelope again — one JSON object, no prose."))
	}
	// THE DRAFT IS NEWS THE MOMENT IT PASSES, and before the review rather than
	// after it, because those are two things that happened and a person watching
	// went there to watch them happen. What the review does to it is its own
	// milestone one stage later ([harnessReviewNote]).
	seat.noted(harnessDraftNote(page, draft))

	// ONE REVIEW PASS, AND IT IS NOT A GATE. A critic that cannot produce a legal
	// patch loses its turn and the draft goes forward: the page in hand already
	// passed the whole law, and refusing it because the improvement failed would
	// throw away a good design over an optional second opinion.
	if reviewed, ok := a.reviewHarnessOnce(ctx, goal, draft, page, reviewer, model, seat); ok {
		return reviewed, nil
	}
	return harnessAccepted{page: page, cues: draft.Cues, justification: draft.Justification}, nil
}

// designHarnessOnce asks for one design and answers with it decoded, the raw
// text it came in (for the retry history), and the error the model is going to
// be shown.
//
// The goal, the attempt and the seat are carried for the WATCHERS and for
// nothing else: the goal and the attempt name this call on the live design block
// in the chat, and the seat is the room the designer thinks into and the journal
// its caller writes this attempt's outcome to ([designSeat]).
func (a *Agent) designHarnessOnce(ctx context.Context, history []ai.Message, model, goal string, attempt int, seat designSeat) (harnessDesign, subharness.Harness, string, error) {
	data, raw, err := a.harnessJSON(ctx, history, model, harnessDesignTokens, harnessProgressCall{seat: seat, goal: goal, phase: "designing", attempt: attempt, attempts: harnessDesignRetries + 1})
	if err != nil {
		return harnessDesign{}, subharness.Harness{}, raw, err
	}
	var envelope harnessDesign
	if err := harnessStrict(data, &envelope); err != nil {
		return harnessDesign{}, subharness.Harness{}, raw, fmt.Errorf(
			"your reply is not the envelope: %w. Reply with ONE JSON object with the keys cues, justification, derivation, harness", err)
	}
	page, err := a.acceptHarness(envelope)
	return envelope, page, raw, err
}

// reviewHarnessOnce is the second pass: one critique, one patch, applied to the
// draft this session already parsed and held to exactly the law the draft
// passed. It reports whether the review produced something better than what it
// was given.
//
// The critic is shown the draft AS JSON rather than as the card, because it is
// patching a page and the node ids its ops name are on that page.
func (a *Agent) reviewHarnessOnce(ctx context.Context, goal string, draft harnessDesign, page subharness.Harness, reviewer, model string, seat designSeat) (harnessAccepted, bool) {
	encoded, err := subharness.Encode(page)
	if err != nil {
		return harnessAccepted{}, false
	}
	history := []ai.Message{
		textMessage("system", reviewer),
		textMessage("user", strings.Join([]string{
			"THE GOAL:\n\n" + goal,
			"THE DRAFT'S CUES:\n\n" + strings.Join(draft.Cues, " · "),
			"THE DRAFT'S JUSTIFICATION:\n\n" + draft.Justification,
			"THE DRAFT HARNESS:\n\n" + string(encoded),
			"Review it and reply with your findings and the ops that answer them.",
		}, "\n\n")),
	}
	// THE REVIEW IS WATCHED EXACTLY AS THE DESIGN IS. It is the same seat, so the
	// critic thinks into the same room and what it made of the draft lands in the
	// same journal, so a person who saw the draft written sees the second pass too.
	//
	// A REVIEW THAT CANNOT BE READ IS NOT NEWS, and every early return below says
	// so with an empty milestone: the draft goes forward unchanged, which is what
	// the draft's own milestone already told the person, and a note explaining
	// that a critic's JSON did not parse would be the machinery talking about
	// itself. The event still goes, because the model call finished
	// ([designSeat.noted] says why that matters).
	data, _, err := a.harnessJSON(ctx, history, model, harnessReviewTokens, harnessProgressCall{seat: seat, goal: goal, phase: "reviewing", attempt: 1, attempts: 1})
	if err != nil {
		return harnessAccepted{}, false
	}
	var envelope harnessRevision
	if err := harnessStrict(data, &envelope); err != nil {
		seat.noted("")
		return harnessAccepted{}, false
	}
	if len(envelope.Ops) == 0 {
		// "The draft is right" is a real review outcome, and it is cheaper than a
		// change nobody needed. The cues may still have been rewritten.
		seat.noted(harnessReviewNote(envelope))
		return harnessAccepted{page: page, cues: harnessCues(draft, envelope), justification: draft.Justification}, true
	}
	revised, err := subharness.Apply(page, envelope.Ops)
	if err != nil {
		seat.noted("")
		return harnessAccepted{}, false
	}
	// The patched page passes the SAME gauntlet the draft did — a review is not a
	// way around the law — and the derivation is the draft's, because the critic
	// does not restate the table.
	//
	// PRUNED TO THE PAGE IT NOW DESCRIBES, though, and that is not a loosening —
	// subharness.PairsWithin makes the whole argument.
	patched := draft
	patched.Derivation = subharness.PairsWithin(revised, draft.Derivation)
	patched.Cues = harnessCues(draft, envelope)
	if strings.TrimSpace(envelope.Justification) != "" {
		patched.Justification = envelope.Justification
	}
	if err := a.checkHarness(revised, patched); err != nil {
		seat.noted("")
		return harnessAccepted{}, false
	}
	// The patch held, so it is what the page IS now, and only now is it worth
	// telling anybody the review changed something.
	seat.noted(harnessReviewNote(envelope))
	return harnessAccepted{page: revised, cues: patched.Cues, justification: patched.Justification}, true
}

// harnessCues is the cue list after a review: the critic's when it wrote one,
// and the draft's otherwise — an omitted field means "unchanged", never "none".
func harnessCues(draft harnessDesign, revised harnessRevision) []string {
	if len(revised.Cues) > 0 {
		return revised.Cues
	}
	return draft.Cues
}

// ── holding a model to JSON ─────────────────────────────────────────────────

// harnessJSON is one model turn whose reply has to be JSON, with the ONE repair
// turn this pipeline allows before a whole attempt is spent.
//
// The order is cheapest-first. subharness.Salvage costs nothing and fixes the
// code fence, the prose around the object, the typographic quotes and the
// trailing comma; for what it cannot fix, a REPAIR turn is still an order of
// magnitude cheaper than re-reading the guide.
//
// EXCEPT FOR ONE THING, and it is the one this pipeline kept losing designs to:
// a reply that never finished. A repair turn cannot put back text a model never
// emitted, so a cut-off reply skips it and is answered with the truth instead
// (see below).
func (a *Agent) harnessJSON(ctx context.Context, history []ai.Message, model string, maxTokens int, progress harnessProgressCall) ([]byte, string, error) {
	raw, cut, err := a.harnessComplete(ctx, history, model, maxTokens, progress)
	if err != nil {
		return nil, "", err
	}
	salvaged, salvageErr := subharness.SalvageDetail(raw)
	if salvageErr == nil {
		return salvaged.JSON, raw, nil
	}

	// A REPLY THAT RAN OUT OF BUDGET IS NOT A DELIMITER PROBLEM, and this is the
	// one place the two are told apart. The salvage ladder's complaint about a
	// truncated object reads exactly like its complaint about a stray brace, and
	// the repair turn below is written to believe it: told "this did not parse",
	// a repairer handed half an object will close the braces and hand back a
	// page with nodes that were never written — a design made up by the pass that
	// was meant to be transcribing.
	//
	// BUT A PAGE THAT STOPPED MID-SENTENCE WAS STILL BEING WRITTEN, and the
	// cheapest true answer to that is to let its own author keep writing it
	// ([Agent.harnessContinue]). Only when that fails too does the attempt go to
	// the retry loop with the truth on it, where the model is asked for a
	// SMALLER page instead of a shorter one — a demand that used to be the FIRST
	// answer, which meant the ceiling systematically punished exactly the
	// complex designs a person asks for on purpose.
	if cut {
		if data, whole, ok := a.harnessContinue(ctx, history, raw, model, maxTokens, progress); ok {
			return data, whole, nil
		}
		return nil, raw, errors.New(harnessRanOut(raw))
	}

	// THE REPAIR TURN CARRIES NO GUIDE. It is not a second attempt at the design
	// — it is a transcription job, and handing it the guide that produced the
	// first reply would invite it to reconsider the architecture while it is
	// meant to be fixing a delimiter.
	repair := []ai.Message{
		textMessage("system", "You repair malformed JSON and do nothing else. You never change content, never add a field, never drop one, and never explain. Your whole reply is one JSON value."),
		textMessage("user", "This was meant to be one JSON object:\n\n"+raw+
			"\n\nIt did not parse. "+salvageErr.Error()+
			"\n\nReply with ONLY the corrected JSON — the same content, nothing added, nothing dropped, no prose, no code fence. "+
			"JSON delimiters and syntax are ASCII: every key and string value is wrapped in \" (U+0022). Prose inside a string value stays exactly as it is."),
	}
	second, cut, err := a.harnessComplete(ctx, repair, model, maxTokens, progress)
	if err != nil {
		return nil, raw, err
	}
	// A REPAIR TURN HAS NOTHING TO SAY TO A PERSON. It is transport plumbing —
	// it changed no content, and what it was fixing was a delimiter — so the
	// milestone is empty and only the event goes: the call finished, and the room's
	// catch-up has to be told that ([designSeat.noted]). What it could not fix
	// surfaces one level up as the refused attempt it becomes.
	progress.seat.noted("")
	if cut {
		return nil, second, errors.New(harnessRanOut(second))
	}
	salvaged, err = subharness.SalvageDetail(second)
	if err != nil {
		return nil, second, fmt.Errorf("neither the reply nor its repair parsed: %w", err)
	}
	return salvaged.JSON, second, nil
}

// harnessContinue gives a cut-off reply the one thing a repair turn cannot: the
// rest of itself, written by the model that was writing it. The partial goes
// back as the assistant's own turn and the model is asked for the remaining
// characters only, so the whole completion budget funds the tail of the page
// instead of a second beginning.
//
// IT IS ONE TURN AND IT PROVES ITSELF OR IT IS DISCARDED. Models asked to
// continue sometimes start the object over from the top, so the stitched text
// is only trusted if it salvages — and the fresh reply is tried alone too,
// because a model that restarted and FINISHED has also answered the question.
// Anything else falls back to the retry loop's truth ([harnessRanOut]), exactly
// as if this turn had never run.
//
// A REPLY THAT NEVER ARRIVED CANNOT BE CONTINUED. The empty case is the
// reasoning model that spent its whole budget thinking — there is no partial to
// hand back, and "continue" from nothing is just the same request again.
func (a *Agent) harnessContinue(ctx context.Context, history []ai.Message, raw, model string, maxTokens int, progress harnessProgressCall) ([]byte, string, bool) {
	if strings.TrimSpace(raw) == "" {
		return nil, "", false
	}
	asked := append(append([]ai.Message{}, history...),
		textMessage("assistant", raw),
		textMessage("user", "Your reply was CUT OFF by the transport mid-object — it was not rejected. "+
			"Continue it from the exact character it stopped at: reply with ONLY the remaining characters of that same JSON object, "+
			"no repetition of what you already wrote, no prose, no code fence, until the object is closed."))
	rest, cut, err := a.harnessComplete(ctx, asked, model, maxTokens, progress)
	// The call finished and had nothing to say to a person; the room's catch-up
	// still has to be told the step is over ([designSeat.noted]).
	progress.seat.noted("")
	if err != nil || cut {
		return nil, "", false
	}
	for _, whole := range []string{raw + rest, rest} {
		if salvaged, salvageErr := subharness.SalvageDetail(whole); salvageErr == nil {
			return salvaged.JSON, whole, true
		}
	}
	return nil, "", false
}

// harnessRanOut is what a design that hit the completion ceiling is told, and it
// is handed to the model verbatim through the retry loop. There are TWO ways to
// hit that ceiling and they want opposite answers, so they are told apart here.
//
// A REASONING MODEL THAT RAN OUT OF ROOM ANSWERS WITH NOTHING AT ALL. The
// content comes back null, the whole completion having gone into the thinking,
// and the reply reads exactly like a refusal unless it is named — which is the
// diagnosis the development rig has carried for a while (cmd/harness-design's
// openrouter.go) and this surface did not, so a person watching a chat saw three
// attempts fail on "the reply is not JSON" about a reply that was never written.
// There is nothing for the model to do about that one: it did not write too
// much, it thought too long, and the answer is the budget above.
//
// A reply that arrived and STOPPED is the other one, and that one the model can
// act on: it wrote a page too big for the room it had.
func harnessRanOut(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "your reply came back empty with the completion budget spent: the whole of it went into thinking and none into the answer. " +
			"Answer more directly — reach the JSON object sooner and reason inside the justification rather than before it."
	}
	return "your reply stopped in the middle: it ran out of completion budget before the JSON object was closed. " +
		"This is not a punctuation problem and re-sending the same page will hit the same wall. " +
		"Design a SMALLER harness — fewer nodes, shorter briefs, a justification of a few tight sentences — and close the object."
}

// harnessComplete is one call on the session's own client. It reports the
// text and whether the answer was CUT OFF at the token ceiling.
//
// The stream is OBSERVED, never rendered into the CONVERSATION
// ([provider.WithStreamObserver]): the person's chat would otherwise type a page
// of JSON into itself. What the observer feeds the chat is the live design card
// — reasoning lines, step counts, and the stall clock — so the wait is never a
// blank spinner. No tools either: the designer's only job is to answer.
//
// THE DESIGN'S OWN ROOM IS THE OTHER READER, AND IT WANTS THE SAME THING for the
// same reason. The content deltas are the page's JSON and they go nowhere but the
// progress card; what crosses into the room is the REASONING, unthrottled, which
// is the half of the stream a person can read ([designSeat.says]). Both readers
// therefore get one live row that stays a row over prose that is worth following,
// and the page arrives as the card it is meant to be read as.
//
// The truncation is read through internal/store's own classifier rather than by
// comparing finish_reason strings here: the vocabulary an endpoint uses for
// "you hit the ceiling" is already known in one place, and a second reading of
// it would be a second answer to the same question.
type harnessProgressCall struct {
	// seat is who is watching this call: the node's id, its room, its journal.
	// The id is not repeated beside it — one design has one number, and a second
	// copy of it is a number that can disagree with itself.
	seat              designSeat
	goal, phase       string
	attempt, attempts int
}

// harnessProgress holds the private stream long enough to turn a flood of
// deltas into a calm card update. The model's JSON remains private; only a
// name, a step count, byte count, and the recent reasoning cross the lane.
type harnessProgress struct {
	mu               sync.Mutex
	a                *Agent
	call             harnessProgressCall
	content, thought string
	last, sent       time.Time
}

func (p *harnessProgress) add(kind provider.StreamEventKind, delta string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.last = time.Now()
	if kind == provider.StreamReasoning {
		p.thought += delta
	} else if kind == provider.StreamDelta {
		p.content += delta
	}
	p.emit(false)
}

func (p *harnessProgress) emit(stalled bool) {
	now := time.Now()
	if !stalled && !p.sent.IsZero() && now.Sub(p.sent) < 300*time.Millisecond {
		return
	}
	p.sent = now
	tail := p.thought
	if len(tail) > 500 {
		tail = tail[len(tail)-500:]
	}
	hint := harnessPartialHint(p.content)
	if p.call.phase == "reviewing" && hint == "thinking" {
		hint = "checking the draft"
	}
	p.a.emitHarness(Event{Kind: EventHarnessProgress, ID: p.call.seat.id, Goal: p.call.goal, Phase: p.call.phase,
		Attempt: p.call.attempt, Attempts: p.call.attempts, ThoughtTail: tail,
		Hint: hint, Bytes: len(p.content), Stalled: stalled})
}

func harnessPartialHint(raw string) string {
	if at := strings.Index(raw, `"name"`); at >= 0 {
		rest := raw[at+len(`"name"`):]
		if colon := strings.IndexByte(rest, ':'); colon >= 0 {
			rest = strings.TrimSpace(rest[colon+1:])
			if strings.HasPrefix(rest, `"`) {
				if end := strings.Index(rest[1:], `"`); end >= 0 {
					return "naming it: " + rest[1:1+end]
				}
			}
		}
	}
	if n := strings.Count(raw, `"kind"`); n > 0 {
		return fmt.Sprintf("%d steps so far", n)
	}
	if len(raw) > 0 {
		return fmt.Sprintf("receiving · %.1f KB", float64(len(raw))/1024)
	}
	return "thinking"
}

func (a *Agent) harnessComplete(ctx context.Context, messages []ai.Message, model string, maxTokens int, call harnessProgressCall) (string, bool, error) {
	progress := &harnessProgress{a: a, call: call, last: time.Now()}
	streamCtx := provider.WithStreamObserver(ctx, func(event provider.StreamEvent) {
		if event.Kind == provider.StreamDelta || event.Kind == provider.StreamReasoning {
			progress.add(event.Kind, event.Delta)
			// AND THE SAME CHUNK IS OFFERED TO THE DESIGN'S ROOM, which takes the
			// thinking and leaves the JSON: unthrottled prose is the one thing the
			// throttled card above cannot be, and a page of braces is the one thing
			// a room somebody walked into to watch must not become
			// ([designSeat.says]).
			call.seat.says(event.Kind, event.Delta)
		}
	})
	done := make(chan struct{})
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				progress.mu.Lock()
				stalled := time.Since(progress.last) >= 10*time.Second
				if stalled {
					progress.emit(true)
				}
				progress.mu.Unlock()
			}
		}
	}()
	// What this call is FOR, in the vocabulary the router and the phase clock
	// share: a craft pass on a harness page, watched through the progress above
	// rather than through its token stream (internal/lane's roles.go).
	response, err := a.client.CompleteWithMessages(
		provider.WithRole(streamCtx, lane.RoleDesign),
		messages,
		ai.WithModel(model),
		ai.WithMaxTokens(maxTokens))
	close(done)
	if err != nil {
		call.seat.broke(err)
		return "", false, err
	}
	if response == nil {
		empty := errors.New("the designer answered with nothing")
		call.seat.broke(empty)
		return "", false, empty
	}
	// The design is spent on the person's account like every other auxiliary
	// call (title.go, guardian.go): it is not a turn, and it is not free.
	a.addAuxiliaryUsage(response, model, 1)
	// THE REPLY IS NOT THE ROOM'S HISTORY, and this is where it stopped being one.
	// It used to be journaled here verbatim, which meant a design reopened tomorrow
	// replayed the JSON envelope the designer had written — the same wall of braces
	// the live lane no longer shows ([designSeat.says]). A design that took four
	// attempts is still explained, and better: the STAGES say what they reached
	// (designPage and reviewHarnessOnce, through [designSeat.noted]), and each of
	// those milestones carries the event this call's end owes the room.
	return response.Text(), harnessCutOff(response), nil
}

// harnessCutOff reports whether a completion ended because it ran out of room.
// The call is never streamed, so an endpoint that says nothing at all is being
// terse rather than dropping — which is exactly the distinction
// [store.ClassifyEnd] draws.
func harnessCutOff(response *ai.Response) bool {
	return store.ClassifyEnd(provider.FinishReason(response), false) == store.EndLength
}

// harnessStrict reads an envelope the way a page is read from disk: unknown
// fields refused. A key nobody asked for is a model answering a different
// question, and accepting it would be accepting the answer to that one.
func harnessStrict(data []byte, into any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(into)
}

// ── the law a page is held to ───────────────────────────────────────────────

// acceptHarness is the gauntlet a design passes: the page decoded by the package
// that owns the format, then checked.
func (a *Agent) acceptHarness(envelope harnessDesign) (subharness.Harness, error) {
	if len(envelope.Harness) == 0 {
		return subharness.Harness{}, errors.New("the envelope has no harness in it")
	}
	page, err := subharness.Decode(envelope.Harness)
	if err != nil {
		return subharness.Harness{}, err
	}
	return page, a.checkHarness(page, envelope)
}

// checkHarness is Validate, the derivation table, and the lint this surface has
// that neither of them does.
func (a *Agent) checkHarness(page subharness.Harness, envelope harnessDesign) error {
	if err := subharness.Validate(page); err != nil {
		return err
	}
	if len(envelope.Derivation) > 0 {
		if err := subharness.CheckDerivation(page, envelope.Derivation); err != nil {
			return err
		}
	}
	return a.lintHarness(page, envelope)
}

// lintHarness is what Validate cannot know, and every line of it is a page that
// would fail in the middle of a run rather than at the moment it was written.
//
//   - THE WHITELIST IS FREE STRINGS to the package. A page may whitelist a tool
//     nothing on this surface has, and the failure would surface as a dead
//     tool.call three steps in.
//   - A NESTED HARNESS THAT IS NOT REGISTERED is the same defect one level up:
//     subharness.call names a harness by name, and a name the store does not hold
//     is a step that cannot run.
//   - A VERIFY WITH NO CHECK is a rung claimed by an empty box.
//   - CUES ARE NOT PART OF THE PAGE (see [harnessDesign]), so nothing in the
//     package can refuse a harness that could never be reached: fewer than two
//     and the entry answers to its own name and nothing else.
func (a *Agent) lintHarness(page subharness.Harness, envelope harnessDesign) error {
	belt := a.harnessToolNames()
	var problems []string
	for _, tool := range page.Whitelist {
		if !belt[tool] {
			problems = append(problems, fmt.Sprintf("the whitelist names %q, which does not exist here (the tools are %s)",
				tool, strings.Join(sortedNames(belt), ", ")))
		}
	}
	for _, node := range page.Program.Nodes {
		switch node.Kind {
		case subharness.KindVerify:
			if node.Fields.Get("check") == "" {
				problems = append(problems, fmt.Sprintf(
					"node %q is a verify with no `check`: a rung is a promise and this one says nothing about what is being checked", node.Id))
			}
		case subharness.KindSubharnessCall:
			name := node.Fields.Get("name")
			if a.config.HarnessStore == nil {
				continue
			}
			if _, err := a.config.HarnessStore.Head(name); err != nil {
				problems = append(problems, fmt.Sprintf(
					"node %q calls the harness %q, which is not registered here — build that one first, or do the work in this page", node.Id, name))
			}
		}
	}
	if strings.TrimSpace(page.Id.Desc) == "" {
		problems = append(problems, "id.desc is empty, so the card would have nothing to say and detection would have nothing to match")
	}
	if len(envelope.Cues) < 2 {
		problems = append(problems, "fewer than two cues: the entry would be unreachable by anything but its own name")
	}
	if len(problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(problems, "; "))
}

func sortedNames(set map[string]bool) []string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	return names
}

// ── the brief ───────────────────────────────────────────────────────────────

// harnessBriefs builds what the two turns are told: the meta-guide with this
// build's machinery rendered into it, and the same guide wearing the critic's
// addendum.
//
// THE REVIEWER SEES THE WHOLE GUIDE, because a critic that cannot see the law it
// is judging against would be reviewing its recollection of it — and the
// checklists in the addendum are the guide's own steps turned into questions.
//
// A guide whose placeholders have drifted from this package's numbers is an
// ERROR here, before the first request, rather than a stray brace in a prompt.
func (a *Agent) harnessBriefs() (designer, reviewer string, err error) {
	guide, err := prompts.Render(prompts.Designer, a.harnessMachinery())
	if err != nil {
		return "", "", err
	}
	return guide, guide + "\n" + prompts.Reviewer, nil
}

// harnessMachinery is every value the guide leaves a hole for: the caps, both
// ladders and the kind catalog are [subharness.Machinery]'s — the ONE map both
// doors read, so the next placeholder the guide renames cannot break only the
// standalone tool — and the belt is the one a harness may actually reach HERE.
//
// The belt is [HarnessBelt] — the wire tools plus whichever media verbs this
// machine has models for — and it is the SAME CALL the run resolves its nodes
// against (cmd/aforge's chatv3_harness.go). harness_belt.go states what is
// excluded and why, and why the three lists that used to answer this
// independently are now one.
func (a *Agent) harnessMachinery() map[string]string {
	tools := HarnessBelt(a.config.Workspace, a.harnessSeams())
	belt := make([]subharness.BeltEntry, 0, len(tools))
	for _, tool := range tools {
		belt = append(belt, subharness.BeltEntry{Name: tool.Name, About: clip(firstLine(tool.Description), harnessToolAbout)})
	}
	return subharness.Machinery(belt)
}

// harnessToolNames is the belt as a set, for the lint. It is the SAME belt the
// guide above was written from, which is the property that matters: a lint that
// refused a name the designer had just been offered was the failure mode
// harness_belt.go exists to close.
func (a *Agent) harnessToolNames() map[string]bool {
	return HarnessBeltNames(a.config.Workspace, a.harnessSeams())
}
