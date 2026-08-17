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
//   - THE INTENT IS A TABLE LOOKUP, NEVER A JUDGEMENT. "make|build|create|design
//     a (sub)harness for|to|that X" enters this path and nothing else does — the
//     same bargain detection keeps one file over, for the same reason: a model
//     asked "was that a request to build a harness?" is a model call on every
//     turn, and a wrong yes costs somebody their turn. What the cue does not
//     match behaves exactly as it did before this file existed.
//   - THE TURN DOES NOT WAIT. A design is two model calls against a
//     twenty-five-thousand-token guide, and it is followed by a QUESTION nobody
//     may be at the keyboard for. Held in the turn loop that would be a
//     conversation frozen for a minute on work the person can watch happen; so
//     the turn ends the moment the design starts, and the card arrives on the
//     standing lane ([Agent.HarnessDesigns]) whenever it is ready. That is the
//     same arrangement a task node's landing already uses (task_run.go), for the
//     same reason.
//   - NOTHING IS SAVED WITHOUT AN ANSWER. What comes back is a PAGE, drawn as
//     the card every surface shares (subharness.CardLines), and the registry is
//     untouched until somebody says yes. A design nobody answered is a design
//     that changed nothing.
//   - IT IS SILENT WHERE THE OFFER IS SILENT. No runner, no store, or no surface
//     that answers questions and this path does not exist — one cue match per
//     turn is not paid for either, because the gates are checked first.
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
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/aforge-v2/internal/subharness/prompts"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	// harnessDesignWindow bounds one whole design job: both model calls AND the
	// wait for an answer to the card. It is long because the second half is a
	// person — a card raised while somebody is at lunch is still worth answering
	// when they come back — and it is bounded at all because a goroutine parked
	// on a question nobody will ever answer is a goroutine parked forever.
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
	harnessDesignTokens = 8000
	harnessReviewTokens = 10000

	// harnessDesignTemp is the design turn's temperature. Low, not zero: this is
	// architecture, and the guide is asking for a choice among shapes.
	harnessDesignTemp = 0.3

	// harnessToolAbout bounds one tool's line in the belt the guide is shown. The
	// wire descriptions are paragraphs — they are written for a model deciding
	// whether to CALL the tool — and what a designer needs is a name and a
	// sentence.
	harnessToolAbout = 160
)

// harnessDesigningWord is the Hint on EventHarnessDesign: one word, because the
// event's own text is the goal and a surface draws it as a note.
const harnessDesigningWord = "designing"

// ── the intent ──────────────────────────────────────────────────────────────

// harnessBuildCue is the whole of build detection: a verb, the noun, and the
// joiner that hands over the goal.
//
// IT IS ANCHORED, and that is the difference between a rule and a guess. "make a
// harness for X" at the head of what somebody typed is a request; the same words
// in the middle of a paragraph are usually somebody describing one ("the reason
// we make a harness for this is…"). Anchoring costs the sentences that bury the
// request and buys a matcher that can never take a turn away from a person who
// was talking ABOUT harnesses.
//
// The joiner is required for the same reason: "make a harness" names no goal,
// and a designer handed no goal designs nothing. That turn goes to the model,
// which can ask what for.
var harnessBuildCue = regexp.MustCompile(
	`(?is)^(?:make|build|create|design)\s+(?:me\s+)?(?:a|an|the)?\s*(?:sub[\s-]?)?harness\s+(?:for|to|that)\s+(.+)$`)

// harnessBuildOpeners are the courtesies a request is wrapped in, stripped
// before the cue is read so that "please make a harness for X" is the same
// request as "make a harness for X". They are openers only — each is removed
// from the FRONT and the rest is re-read — so none of them can match anything
// in the middle of a sentence.
var harnessBuildOpeners = []string{
	"please ", "can you ", "could you ", "would you ", "let's ", "lets ",
	"i want you to ", "i'd like you to ", "i would like you to ",
}

// harnessBuildGoal reads one turn's build request and answers with the goal —
// what somebody wants the harness to DO, with the words that asked for it
// removed. false is every other sentence, which is nearly all of them.
func harnessBuildGoal(text string) (string, bool) {
	text = strings.TrimSpace(text)
	// The openers come off one at a time, so "please can you make a harness for
	// X" is read too. The loop terminates because every pass strips a prefix.
	for stripped := true; stripped; {
		stripped = false
		for _, opener := range harnessBuildOpeners {
			if len(text) >= len(opener) && strings.EqualFold(text[:len(opener)], opener) {
				text = strings.TrimSpace(text[len(opener):])
				stripped = true
				break
			}
		}
	}
	found := harnessBuildCue.FindStringSubmatch(text)
	if found == nil {
		return "", false
	}
	goal := strings.TrimSpace(found[1])
	if goal == "" {
		return "", false
	}
	return goal, true
}

// routeHarnessBuild is this file's place in a turn, called from
// [Agent.routeHarness] before detection reads the same sentence.
//
// It reports (answered, completed) on that function's own terms. answered=true
// means THE TURN IS OVER — the design has started and nothing else is going to
// happen on this turn — and completed=true because it is over the ordinary way:
// nothing failed, nobody interrupted, and a follow-up somebody typed while this
// was starting may run.
func (a *Agent) routeHarnessBuild(hub *eventHub, user userMessage, started time.Time) (bool, bool) {
	goal, model, ok := a.harnessBuild(user)
	if !ok {
		return false, false
	}
	// THE MODEL ON THE EVENT IS THE RESOLVED ONE. What comes back from
	// [Agent.harnessBuild] is the turn's own word and it is usually empty — the
	// ladder that answers "then who designs it" runs inside
	// [Agent.startHarnessDesign] a line later — so an event carrying it would
	// name a model only when the person had already typed one, which is the case
	// where nobody needed telling. Resolving here costs a registry read and makes
	// the note and the call name the same model by construction: a named model
	// resolves to itself, so the second resolution is the same answer.
	model = a.designerModel(model)
	a.emitHarness(Event{Kind: EventHarnessDesign, Text: goal, Hint: harnessDesigningWord, Model: model})
	a.startHarnessDesign(goal, model)
	// The turn ends HERE, with no assistant message: the design is the answer and
	// it is not written yet. A blank assistant turn recorded now would be a line
	// every later request carries forever, saying nothing.
	hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(Usage{}, started)})
	return true, true
}

// harnessBuild is the gate and the read: whether this build can design at all,
// and what this turn asked for.
//
// THE GATES ARE THE OFFER'S GATES. A store to write into, a runner to run what
// is written, and somebody watching who can answer the card — a design nobody
// can approve is two model calls spent on a page that will be dropped, and a
// page saved into a registry with no engine under it is a menu item that fails
// when it is picked.
func (a *Agent) harnessBuild(user userMessage) (goal, model string, ok bool) {
	if a.config.RunHarness == nil || a.config.HarnessStore == nil {
		return "", "", false
	}
	if !a.config.AskConsent {
		return "", "", false
	}
	// ONLY WHAT A PERSON TYPED, on harnessMatch's own terms: a woken turn's note
	// is the session talking to itself, and a harness built out of one would be
	// the harness commissioning its own tools.
	if user.empty() || user.wake || user.authored {
		return "", "", false
	}
	goal, ok = harnessBuildGoal(user.text())
	if !ok {
		return "", "", false
	}
	// THE MODEL IS READ OFF THE GOAL, by the clause reader the offer already uses
	// (harness.go): "make a harness for triaging flakes with opus" chose a model
	// and asked for a harness about flakes, and the designer must be handed the
	// second thing without the first. A word this install cannot place leaves the
	// design on the session's own model — never a refusal, exactly as the offer
	// treats it.
	model, _, goal = a.harnessTurnModel(goal)
	if goal = strings.TrimSpace(goal); goal == "" {
		return "", "", false
	}
	return goal, model, true
}

// ── the job ─────────────────────────────────────────────────────────────────

// startHarnessDesign puts one design in flight and returns immediately.
//
// The context is the SESSION's and not the turn's, deliberately: the turn is
// about to end and its context with it, and a design cancelled by the very turn
// that asked for it would never produce anything. It is registered so that
// [Agent.Close] can end it — a design still thinking when the session leaves has
// nowhere to deliver.
func (a *Agent) startHarnessDesign(goal, model string) {
	ctx, cancel := context.WithTimeout(context.Background(), harnessDesignWindow)
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		cancel()
		return
	}
	// ONE ID NAMES THE WHOLE JOB, minted from the offer lane's counter because
	// the card at the end of it is answered through [Agent.ResolveHarness] — the
	// same method, the same map, one question shape.
	a.harnessSeq++
	id := a.harnessSeq
	if a.harnessDesigns == nil {
		a.harnessDesigns = make(map[uint64]context.CancelFunc, 1)
	}
	a.harnessDesigns[id] = cancel
	model = a.harnessDesignModel(model)
	a.mu.Unlock()

	go func() {
		defer a.endHarnessDesign(id)
		a.designHarness(ctx, id, goal, model)
	}()
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
// designerModel is [Agent.harnessDesignModel] with the lock taken, for the one
// caller that needs the answer BEFORE the design goroutine exists
// ([Agent.routeHarnessBuild]'s event). The two are one function deliberately: a
// second ladder written out here is a second answer to "who designs this", and
// the whole point of asking early is that the note and the design agree.
func (a *Agent) designerModel(named string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.harnessDesignModel(named)
}

func (a *Agent) harnessDesignModel(named string) string {
	if named = strings.TrimSpace(named); named != "" {
		return named
	}
	if model, err := roles.Resolve(roles.Source(a.config.RolesSource), roles.RoleDesigner, a.model); err == nil {
		return model
	}
	return a.model
}

// endHarnessDesign forgets one finished job.
func (a *Agent) endHarnessDesign(id uint64) {
	a.mu.Lock()
	cancel := a.harnessDesigns[id]
	delete(a.harnessDesigns, id)
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// cancelHarnessDesigns ends every design in flight. It is called from
// [Agent.Close] with a.mu held.
func (a *Agent) cancelHarnessDesignsLocked() {
	for _, cancel := range a.harnessDesigns {
		cancel()
	}
	a.harnessDesigns = nil
}

// designHarness is the whole job: design, review, ask, save.
//
// EVERY EXIT SAYS SOMETHING. A design that failed, a page that was declined, a
// save that would not write — each one ends in a line on the lane and a note in
// the transcript, because the person asked for a harness and silence is the one
// answer that leaves them wondering whether anything is still happening.
func (a *Agent) designHarness(ctx context.Context, id uint64, goal, model string) {
	page, cues, err := a.designPage(ctx, goal, model)
	if err != nil {
		if ctx.Err() != nil {
			// The session left, or the window ran out. Nobody is there to be told.
			return
		}
		a.noteHarnessDesign("harness design failed: " + err.Error())
		return
	}
	answer, err := a.askHarnessDesign(ctx, id, page, model)
	if err != nil {
		return
	}
	if !answer.run {
		a.noteHarnessDesign(fmt.Sprintf("harness %q was designed and not saved", page.Id.Name))
		return
	}
	saved, err := a.saveHarness(page, cues)
	if err != nil {
		a.noteHarnessDesign(fmt.Sprintf("harness %q could not be saved: %v", page.Id.Name, err))
		return
	}
	a.noteHarnessDesign(fmt.Sprintf("harness %q v%d saved", saved.Id.Name, saved.Id.Version))
}

// noteHarnessDesign says one thing about a design, in the two places it belongs.
//
// TWO LANES, ONE SENTENCE, and they answer to two different readers. The event
// is for the PERSON — a dim line on the surface, now, about work they watched
// start. The ambient note is for the MODEL: the next thing said in this
// conversation happens after a harness was saved, and a session that did not
// know would answer as though it had not been. It is ambient rather than a wake
// (agent.go) because nobody is owed a sentence about it — the card already said
// what happened.
func (a *Agent) noteHarnessDesign(text string) {
	a.emitHarness(Event{Kind: EventNotice, Text: text})
	a.enqueueAmbientNote(text)
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
	stream := newEventStream()
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		stream.close()
		return stream.out
	}
	a.harnessWatchers = append(a.harnessWatchers, stream)
	a.mu.Unlock()
	return stream.out
}

// ── the card ────────────────────────────────────────────────────────────────

// askHarnessDesign raises the preview card and waits for the answer.
//
// It is [Agent.askHarness] with one difference and it is the whole difference of
// this file: the wait is on the DESIGN's context rather than a turn's, because
// there is no turn. The id was minted when the job started, so the question a
// surface answers is the job a person watched begin.
func (a *Agent) askHarnessDesign(ctx context.Context, id uint64, page subharness.Harness, model string) (harnessAnswer, error) {
	answers := make(chan harnessAnswer, 1)
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return harnessAnswer{}, errAgentClosed
	}
	if a.harnessAsks == nil {
		a.harnessAsks = make(map[uint64]chan harnessAnswer, 1)
	}
	a.harnessAsks[id] = answers
	a.mu.Unlock()

	// The page travels by pointer and this is its only copy: nothing else holds
	// it, and whether it is ever written down is what the answer decides.
	carried := page
	a.emitHarness(Event{
		Kind:    EventHarnessDesignDone,
		ID:      id,
		Text:    page.Id.Name,
		Hint:    page.Id.Desc,
		Model:   model,
		Harness: &carried,
	})

	select {
	case answer := <-answers:
		return answer, nil
	case <-ctx.Done():
		a.forgetHarness(id)
		return harnessAnswer{}, ctx.Err()
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

// designPage is the two stages: a design held to the law, then one review pass
// that may improve it.
//
// THE HISTORY IS KEPT ACROSS ATTEMPTS. A designer shown its own refused page AND
// the validator's exact sentence is being asked to repair what it wrote; one
// shown only the error is being asked to guess again.
func (a *Agent) designPage(ctx context.Context, goal, model string) (subharness.Harness, []string, error) {
	designer, reviewer, err := a.harnessBriefs()
	if err != nil {
		return subharness.Harness{}, nil, err
	}
	history := []ai.Message{
		textMessage("system", designer),
		textMessage("user", "THE GOAL:\n\n"+goal+"\n\nDesign the sub-harness for it."),
	}
	var (
		draft harnessDesign
		page  subharness.Harness
	)
	for tries := 0; ; tries++ {
		var raw string
		draft, page, raw, err = a.designHarnessOnce(ctx, history, model)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return subharness.Harness{}, nil, ctx.Err()
		}
		if tries >= harnessDesignRetries {
			return subharness.Harness{}, nil, fmt.Errorf("no valid design in %d attempts: %w", tries+1, err)
		}
		history = append(history,
			textMessage("assistant", raw),
			textMessage("user", "That harness was REFUSED:\n\n"+err.Error()+
				"\n\nFix exactly that and reply with the whole envelope again — one JSON object, no prose."))
	}

	// ONE REVIEW PASS, AND IT IS NOT A GATE. A critic that cannot produce a legal
	// patch loses its turn and the draft goes forward: the page in hand already
	// passed the whole law, and refusing it because the improvement failed would
	// throw away a good design over an optional second opinion.
	if revised, cues, ok := a.reviewHarnessOnce(ctx, goal, draft, page, reviewer, model); ok {
		return revised, cues, nil
	}
	return page, draft.Cues, nil
}

// designHarnessOnce asks for one design and answers with it decoded, the raw
// text it came in (for the retry history), and the error the model is going to
// be shown.
func (a *Agent) designHarnessOnce(ctx context.Context, history []ai.Message, model string) (harnessDesign, subharness.Harness, string, error) {
	data, raw, err := a.harnessJSON(ctx, history, model, harnessDesignTokens)
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
func (a *Agent) reviewHarnessOnce(ctx context.Context, goal string, draft harnessDesign, page subharness.Harness, reviewer, model string) (subharness.Harness, []string, bool) {
	encoded, err := subharness.Encode(page)
	if err != nil {
		return subharness.Harness{}, nil, false
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
	data, _, err := a.harnessJSON(ctx, history, model, harnessReviewTokens)
	if err != nil {
		return subharness.Harness{}, nil, false
	}
	var envelope harnessRevision
	if err := harnessStrict(data, &envelope); err != nil {
		return subharness.Harness{}, nil, false
	}
	if len(envelope.Ops) == 0 {
		// "The draft is right" is a real review outcome, and it is cheaper than a
		// change nobody needed. The cues may still have been rewritten.
		return page, harnessCues(draft, envelope), true
	}
	revised, err := subharness.Apply(page, envelope.Ops)
	if err != nil {
		return subharness.Harness{}, nil, false
	}
	// The patched page passes the SAME gauntlet the draft did — a review is not a
	// way around the law — and the derivation is the draft's, because the critic
	// does not restate the table.
	patched := draft
	patched.Cues = harnessCues(draft, envelope)
	if strings.TrimSpace(envelope.Justification) != "" {
		patched.Justification = envelope.Justification
	}
	if err := a.checkHarness(revised, patched); err != nil {
		return subharness.Harness{}, nil, false
	}
	return revised, patched.Cues, true
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
// trailing comma; what it cannot fix is a reply cut off mid-object. For those a
// REPAIR turn is still an order of magnitude cheaper than re-reading the guide.
func (a *Agent) harnessJSON(ctx context.Context, history []ai.Message, model string, maxTokens int) ([]byte, string, error) {
	raw, err := a.harnessComplete(ctx, history, model, maxTokens, harnessDesignTemp)
	if err != nil {
		return nil, "", err
	}
	salvaged, salvageErr := subharness.SalvageDetail(raw)
	if salvageErr == nil {
		return salvaged.JSON, raw, nil
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
	second, err := a.harnessComplete(ctx, repair, model, maxTokens, 0)
	if err != nil {
		return nil, raw, err
	}
	salvaged, err = subharness.SalvageDetail(second)
	if err != nil {
		return nil, second, fmt.Errorf("neither the reply nor its repair parsed: %w", err)
	}
	return salvaged.JSON, second, nil
}

// harnessComplete is one non-streamed call on the session's own client.
//
// WithoutStream for the compaction summary's reason: this is work beside the
// conversation, and left on the turn's stream it would type a page of JSON into
// the room. No tools either — the designer's only job is to answer.
func (a *Agent) harnessComplete(ctx context.Context, messages []ai.Message, model string, maxTokens int, temperature float64) (string, error) {
	response, err := a.client.CompleteWithMessages(
		provider.WithoutStream(ctx),
		messages,
		ai.WithModel(model),
		ai.WithMaxTokens(maxTokens),
		ai.WithTemperature(temperature))
	if err != nil {
		return "", err
	}
	if response == nil {
		return "", errors.New("the designer answered with nothing")
	}
	// The design is spent on the person's account like every other auxiliary
	// call (title.go, guardian.go): it is not a turn, and it is not free.
	a.addAuxiliaryUsage(response)
	return response.Text(), nil
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
	designer = guide + harnessASCIIRule
	return designer, designer + "\n" + prompts.Reviewer, nil
}

// harnessASCIIRule is the one line of output contract this surface adds to the
// guide. It lives here rather than in the document because the guide is about
// DESIGN and this is a fact about the transport: a page whose delimiters came
// out as typographic quotes is a good design that will not parse.
//
// It draws the line where the salvage ladder draws it — syntax is ASCII, prose
// is the writer's — so a designer is never told to flatten an em-dash out of a
// brief in order to be read.
const harnessASCIIRule = "\n" + `
One rule about the characters, not the design: JSON DELIMITERS AND SYNTAX ARE
ASCII. The quotes around every key and every string value are " (U+0022) — never
“ ” ‘ ’ — and so are the braces, brackets, colons and commas. Prose may use any
character INSIDE a string value: an em-dash in a brief is content and stays.
`

// harnessMachinery is every value the guide leaves a hole for: this package's
// caps, both ladders, and the tool belt a harness may actually reach HERE.
//
// The belt is the wire tools (internal/exec/bare) and not the session's own,
// which is the same set a run resolves its tool.call nodes against
// (cmd/aforge's chatv3_harness.go). It is the model's belt that is excluded, and
// deliberately: notes, jobs, connected accounts and image generation are things
// a conversation reaches for, not things a saved procedure should inherit by
// accident.
func (a *Agent) harnessMachinery() map[string]string {
	tools := bare.AllTools(a.config.Workspace)
	belt := make([]string, 0, len(tools))
	for _, tool := range tools {
		belt = append(belt, fmt.Sprintf("%-6s %s", tool.Name, clip(firstLine(tool.Description), harnessToolAbout)))
	}
	return map[string]string{
		"max_turns":      strconv.Itoa(subharness.MaxTurns),
		"max_rounds":     strconv.Itoa(subharness.MaxRounds),
		"default_rounds": strconv.Itoa(subharness.DefaultRounds),
		"max_width":      strconv.Itoa(subharness.MaxWidth),
		"max_nodes":      strconv.Itoa(subharness.MaxNodes),
		"max_id_bytes":   strconv.Itoa(subharness.MaxIdBytes),
		"max_dyn_cap":    strconv.Itoa(subharness.MaxDynCap),
		"verify_ladder":  strings.Join(subharness.VerifyLadder(), " < "),
		"dyn_ladder":     strings.Join(subharness.DynLadder(), " < "),
		"tools":          strings.Join(belt, "\n"),
	}
}

// harnessToolNames is the belt as a set, for the lint.
func (a *Agent) harnessToolNames() map[string]bool {
	names := map[string]bool{}
	for _, tool := range bare.AllTools(a.config.Workspace) {
		names[tool.Name] = true
	}
	return names
}
