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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/aforge-v2/internal/subharness/prompts"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	// harnessDesignWindow bounds one whole design job: both model calls AND the
	// wait for an answer to the card. It is long because the second half is a
	// person — a card raised while somebody is at lunch is still worth answering
	// when they come back — and it is bounded at all because a goroutine parked
	// on a question nobody will ever answer is a goroutine parked forever. The
	// design node takes it off its own deadline (harness_task.go), which is an
	// hour and is the wrong shape of bound for a question.
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

// ── who writes it ───────────────────────────────────────────────────────────

// designerModel is [Agent.harnessDesignModel] with the lock taken, for the
// caller that needs the answer BEFORE the design starts (tools_harness.go's
// build_harness, whose note names the model). The two are one function
// deliberately: a second ladder written out here is a second answer to "who
// designs this", and the whole point of asking early is that the note and the
// design agree.
func (a *Agent) designerModel(named string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.harnessDesignModel(named)
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
	if named = strings.TrimSpace(named); named != "" {
		return named
	}
	if model, err := roles.Resolve(roles.Source(a.config.RolesSource), roles.RoleDesigner, a.model); err == nil {
		return model
	}
	return a.model
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
func (a *Agent) designPage(ctx context.Context, goal, model string, designID ...uint64) (subharness.Harness, []string, error) {
	id := uint64(0)
	if len(designID) > 0 {
		id = designID[0]
	}
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
		draft, page, raw, err = a.designHarnessOnce(ctx, history, model, goal, tries+1, id)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return subharness.Harness{}, nil, ctx.Err()
		}
		if tries >= harnessDesignRetries {
			return subharness.Harness{}, nil, fmt.Errorf("no valid design in %d attempts: %w", tries+1, err)
		}
		reason := "malformed draft"
		if strings.Contains(err.Error(), "ran out of completion budget") || strings.Contains(err.Error(), "stopped in the middle") {
			reason = "truncated draft"
		}
		a.emitHarness(Event{Kind: EventHarnessProgress, ID: id, Goal: goal, Phase: "designing", Attempt: tries + 1, Attempts: harnessDesignRetries + 1, Hint: "retrying · " + reason})
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

	// ONE REVIEW PASS, AND IT IS NOT A GATE. A critic that cannot produce a legal
	// patch loses its turn and the draft goes forward: the page in hand already
	// passed the whole law, and refusing it because the improvement failed would
	// throw away a good design over an optional second opinion.
	if revised, cues, ok := a.reviewHarnessOnce(ctx, goal, draft, page, reviewer, model, id); ok {
		return revised, cues, nil
	}
	return page, draft.Cues, nil
}

// designHarnessOnce asks for one design and answers with it decoded, the raw
// text it came in (for the retry history), and the error the model is going to
// be shown.
func (a *Agent) designHarnessOnce(ctx context.Context, history []ai.Message, model string, live ...any) (harnessDesign, subharness.Harness, string, error) {
	goal, attempt := "", 1
	if len(live) > 0 {
		goal, _ = live[0].(string)
	}
	if len(live) > 1 {
		attempt, _ = live[1].(int)
	}
	id := uint64(0)
	if len(live) > 2 {
		id, _ = live[2].(uint64)
	}
	data, raw, err := a.harnessJSON(ctx, history, model, harnessDesignTokens, harnessProgressCall{id: id, goal: goal, phase: "designing", attempt: attempt, attempts: harnessDesignRetries + 1})
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
func (a *Agent) reviewHarnessOnce(ctx context.Context, goal string, draft harnessDesign, page subharness.Harness, reviewer, model string, designID ...uint64) (subharness.Harness, []string, bool) {
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
	id := uint64(0)
	if len(designID) > 0 {
		id = designID[0]
	}
	data, _, err := a.harnessJSON(ctx, history, model, harnessReviewTokens, harnessProgressCall{id: id, goal: goal, phase: "reviewing", attempt: 1, attempts: 1})
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
// trailing comma; for what it cannot fix, a REPAIR turn is still an order of
// magnitude cheaper than re-reading the guide.
//
// EXCEPT FOR ONE THING, and it is the one this pipeline kept losing designs to:
// a reply that never finished. A repair turn cannot put back text a model never
// emitted, so a cut-off reply skips it and is answered with the truth instead
// (see below).
func (a *Agent) harnessJSON(ctx context.Context, history []ai.Message, model string, maxTokens int, progress harnessProgressCall) ([]byte, string, error) {
	raw, cut, err := a.harnessComplete(ctx, history, model, maxTokens, harnessDesignTemp, progress)
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
	// was meant to be transcribing. So a cut-off reply skips the repair entirely
	// and goes to the retry loop with the truth on it, where the model can write
	// a SMALLER page instead of a shorter one.
	if cut {
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
	second, cut, err := a.harnessComplete(ctx, repair, model, maxTokens, 0, progress)
	if err != nil {
		return nil, raw, err
	}
	if cut {
		return nil, second, errors.New(harnessRanOut(second))
	}
	salvaged, err = subharness.SalvageDetail(second)
	if err != nil {
		return nil, second, fmt.Errorf("neither the reply nor its repair parsed: %w", err)
	}
	return salvaged.JSON, second, nil
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
// The stream is OBSERVED, never rendered ([provider.WithStreamObserver]): the
// person's chat would otherwise type a page of JSON into itself. What the
// observer feeds is the live design card — reasoning lines, step counts, and
// the stall clock — so the wait is never a blank spinner. No tools either:
// the designer's only job is to answer.
//
// The truncation is read through internal/store's own classifier rather than by
// comparing finish_reason strings here: the vocabulary an endpoint uses for
// "you hit the ceiling" is already known in one place, and a second reading of
// it would be a second answer to the same question.
type harnessProgressCall struct {
	id                uint64
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
	p.a.emitHarness(Event{Kind: EventHarnessProgress, ID: p.call.id, Goal: p.call.goal, Phase: p.call.phase,
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

func (a *Agent) harnessComplete(ctx context.Context, messages []ai.Message, model string, maxTokens int, temperature float64, call harnessProgressCall) (string, bool, error) {
	progress := &harnessProgress{a: a, call: call, last: time.Now()}
	streamCtx := provider.WithStreamObserver(ctx, func(event provider.StreamEvent) {
		if event.Kind == provider.StreamDelta || event.Kind == provider.StreamReasoning {
			progress.add(event.Kind, event.Delta)
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
	response, err := a.client.CompleteWithMessages(
		streamCtx,
		messages,
		ai.WithModel(model),
		ai.WithMaxTokens(maxTokens),
		ai.WithTemperature(temperature))
	close(done)
	if err != nil {
		return "", false, err
	}
	if response == nil {
		return "", false, errors.New("the designer answered with nothing")
	}
	// The design is spent on the person's account like every other auxiliary
	// call (title.go, guardian.go): it is not a turn, and it is not free.
	a.addAuxiliaryUsage(response)
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
