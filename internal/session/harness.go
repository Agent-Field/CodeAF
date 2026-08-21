package session

// THE HARNESS QUESTION: the moment between "somebody said something" and "the
// model was sent it".
//
// A sub-harness has no slash command (docs/SUBHARNESS.md). If running one
// required typing its name, the registry would be a menu, and a menu is a thing
// people forget they have — the harness that exists to do research properly
// would sit unused beside a turn doing research badly. So the turn itself is the
// trigger: what a person said is matched against the registry, and a strong
// match raises ONE line asking whether they meant it.
//
// Three laws hold this to something a person can live with.
//
//   - IT IS A QUESTION, NEVER A ROUTING. Nothing runs because a matcher said so.
//     A harness runs because somebody said yes to a card that named it, and the
//     answer that costs nothing — no — leaves an ordinary turn behind, already
//     recorded, already about to be sent. Detection cannot lose a turn.
//   - IT IS ASKED ONCE PER TURN, HERE, before the first request. Not per step,
//     not per tool call: a question that could arrive mid-turn would be a
//     question about a sentence the model has already half-answered.
//   - IT IS SILENT WHEN NOBODY IS WATCHING. No registry, no runner, or no
//     surface that answers questions (Config.AskConsent) and this file does
//     nothing at all — not one extra branch a person can observe, and not one
//     turn that behaves differently than it did before any of this existed.
//     That is the same law consent.go keeps for the same reason: a headless run
//     must never block on a question nobody will ever be shown.
//
// The scoring is [subharness.Score] and it is deliberately dull: a table lookup
// over the designer's own cue list, pure, deterministic, no model call. That
// package says why at length.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ResolveHarness answers one EventHarnessOffer: true runs the harness, false is
// the ordinary turn. An id nobody is waiting on — an offer whose turn was
// interrupted, a second click — is dropped rather than reported, exactly as a
// late consent answer is.
//
// The model is WHICH MODEL THE ANSWER WAS GIVEN ABOUT, and EMPTY IS THE ONE THE
// OFFER CARRIED — which is every surface that draws the card as it arrived and
// hands the answer straight back. A surface that shows the model on the card
// (tui3's harness.go) returns what it showed, so what runs is what the person
// read; a word this session cannot resolve falls back to the offer's own model
// rather than starting a run on a model nobody has. It is resolved through the
// same matcher the offer's own word went through, so the two cannot disagree
// about what "opus" means.
func (a *Agent) ResolveHarness(id uint64, run bool, model string) {
	a.mu.Lock()
	ask, waiting := a.harnessAsks[id]
	if waiting {
		delete(a.harnessAsks, id)
	}
	a.mu.Unlock()
	if !waiting {
		return
	}
	// Buffered to one and read at most once, so this never blocks and never
	// needs the lock held across it.
	ask.answers <- harnessAnswer{run: run, model: model}
}

// harnessAnswer is one answer to one offer: whether to run it, and the model the
// surface was showing when it was answered.
type harnessAnswer struct {
	run   bool
	model string
}

// routeHarness is the whole of detection's place in a turn, called once from
// [Agent.runTurn] before anything is sent anywhere.
//
// It reports (answered, completed). answered=false is the ordinary turn — no
// registry, no match, or a person who said no — and the loop carries on as if
// this function did not exist. answered=true means the turn is OVER: the
// harness ran and its report is in the transcript, or it failed, or the turn
// was interrupted while the question was up.
func (a *Agent) routeHarness(ctx context.Context, hub *eventHub, user userMessage, started time.Time) (bool, bool) {
	// A HARNESS THE PERSON PICKED THEMSELVES IS READ FIRST, and it skips both
	// halves of the question below: they chose the name off a list and then typed
	// the request, so there is nothing to detect and nothing to ask
	// ([Agent.RunHarnessRequest]). Everything AFTER the question is the same code
	// either way — that is the point of the split.
	if picked, ok := a.takeHarnessPick(); ok {
		return a.runHarnessRoute(ctx, hub, picked, started)
	}
	// BUILDING ONE IS NOT READ HERE, and it used to be: a cue ahead of this line
	// caught "make a harness for triaging flaky tests" so that a matcher would not
	// offer to RUN the harness that already triages flaky tests. Commissioning one
	// is now a tool the model reaches for (tools_harness.go), which leaves this
	// function what it always was — one question about one sentence — and leaves
	// the harness that already does the work free to be offered for it.
	match, ok := a.harnessMatch(user)
	if !ok {
		return false, false
	}
	answer, err := a.askHarness(ctx, hub, match)
	if err != nil {
		// The turn died under the question — an interrupt, a closed agent. The
		// turn ends the way every interrupted turn ends, and nothing ran.
		hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(Usage{}, started, a.Model())})
		return true, false
	}
	if !answer.run {
		return false, false
	}
	// The model the SURFACE answered with, folded back onto the route, so that
	// what runs below cannot disagree with what the card showed.
	match.Model = a.answeredHarnessModel(match, answer.model)
	return a.runHarnessRoute(ctx, hub, match, started)
}

// runHarnessRoute is everything a harness run does AFTER the decision to run it
// — announced, executed, recorded, and the turn sealed — and it reports on
// [Agent.routeHarness]'s own terms.
//
// It is its own function because there are two doors onto it and only one of
// them asks a question: a matched turn somebody said yes to, and a harness
// somebody picked out of a list themselves. A second copy of this for the second
// door would be a run that landed in the transcript one way here and another way
// there, which is the drift the whole file is written against.
func (a *Agent) runHarnessRoute(ctx context.Context, hub *eventHub, route harnessRoute, started time.Time) (bool, bool) {
	entry := route.Entry
	model := route.Model
	// THE RUN IS PUT ON THE REGISTER BEFORE IT IS ANNOUNCED, and the id it gets
	// is the id the event carries: a run somebody can see on screen is a run
	// somebody may want to stop, and [Agent.Cancel] can only reach one it can
	// name (cancel.go). The register is emptied by the defer, so the id names
	// this run for exactly as long as it is running.
	runCtx, runID, ended := a.beginHarnessRun(ctx)
	defer ended()
	hub.send(Event{Kind: EventHarnessRun, ID: runID, Text: entry.Name, Hint: entry.Description, Model: model})
	// EACH STEP IS SAID AS IT LANDS. Between the announcement above and the
	// report below is the whole of the run — minutes of it — and until this
	// existed a person watching had the harness's name and then silence. The
	// send is synchronous with the walk, which is exactly what makes the row on
	// screen the step that is actually finishing.
	report, spent, err := a.config.RunHarness(runCtx, entry.Name, route.Turn.Text, model, func(step subharness.Trail) {
		hub.send(Event{Kind: EventHarnessStep, ID: runID, Text: entry.Name, Step: &step})
	})
	// CHARGED BEFORE IT IS READ, and on both roads out of here: a run that made
	// twenty calls and then failed to be reported is a run somebody paid for
	// twenty calls of.
	a.foldHarnessUsage(spent, model)
	if err != nil {
		hub.send(Event{Kind: EventError, Err: err, Usage: a.sealTurn(Usage{Turns: 1}, started, a.Model())})
		return true, false
	}
	// The report is the turn's answer, so it is recorded as one. A harness whose
	// run said nothing records nothing rather than an empty assistant message:
	// the run happened, the events said so, and a blank turn in the transcript
	// is a thing later requests would carry forever.
	if report = strings.TrimSpace(report); report != "" {
		a.record(ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: report}}})
		hub.send(Event{Kind: EventTextDelta, Text: report})
	}
	// THE SEAL IS ZERO-TOKEN AND THAT IS NOT AN OVERSIGHT: what the run spent
	// went through the auxiliary door above ([Agent.foldHarnessUsage]), which is
	// the same shape the image turn keeps (image.go). Counting it here as well
	// would bill the person twice for one run.
	hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(Usage{Turns: 1}, started, a.Model())})
	// And the name, on the same terms the ordinary turn takes it (title.go): a
	// session whose first turn was a harness run is still a session with a
	// subject.
	a.maybeTitle(ctx, hub)
	return true, true
}

// foldHarnessUsage charges what a run spent to the session that asked for it.
//
// IT GOES THROUGH THE AUXILIARY DOOR AND THE TURN SEAL STAYS ZERO-TOKEN. The
// figures did not come from this conversation's own transcript — a run has its
// own provider client, its own messages, its own belt and often its own model
// (cmd/aforge's chatv3_harness.go) — so folding them into the turn would
// attribute a context this session never held to the context it is about to
// send, which is the number compaction is decided on. What a person is actually
// owed is the money and the request count, and that is exactly what
// [Agent.addAuxiliaryUsage] takes: session total, spend rail, /cost and the
// status line, without touching Turns or contextTokens.
//
// It also settles an asymmetry that was plainly wrong: DESIGNING a harness has
// always been billed to the session (harness_build.go), and until this fold
// existed RUNNING one — dozens of calls, minutes of work — was free to every
// cost surface in the program.
//
// THE MODEL IS THE RUN'S OWN, in the order of who knows best: what the provider
// reported the calls answered on, then the model the turn asked the run to
// ride, and finally this conversation's — a run that named nothing is built on
// the model the session launched with. A run whose nodes pinned models of their
// own reports no single model (subharness's usage.go), and falls through here to
// the one the turn asked for rather than to a name half its calls would deny.
//
// A RUN THE PROVIDER SAID NOTHING ABOUT FOLDS NOTHING. Zero tokens and no cost
// is "the provider did not say", not "the run was free", and the auxiliary door
// keeps that same silence for every other errand whose response carried no
// accounting at all.
func (a *Agent) foldHarnessUsage(spent subharness.Usage, model string) {
	if !spent.Reported() {
		return
	}
	if spent.Model != "" {
		model = spent.Model
	}
	if model == "" {
		model = a.Model()
	}
	used := &ai.Usage{
		PromptTokens:             spent.Input,
		CompletionTokens:         spent.Output,
		CacheReadInputTokens:     spent.CacheRead,
		CacheCreationInputTokens: spent.CacheWrite,
	}
	// The cost is attached only when the provider gave one. A pointer to zero
	// and no pointer at all are the same arithmetic here, but they are not the
	// same claim, and this struct is read elsewhere as the provider's own words.
	if spent.CostUSD > 0 {
		used.Cost = &spent.CostUSD
	}
	a.addAuxiliaryUsage(&ai.Response{Usage: used}, model, spent.Calls)
}

// harnessRoute is one turn matched against one registry.
type harnessRoute struct {
	subharness.Match
	// Turn is what was matched, carried through because the runner is handed
	// the person's words rather than the id of a message it cannot read. When
	// the turn named a model, this is the turn WITHOUT that clause: the run is
	// asked to do the work, not to read the sentence that chose its model.
	Turn subharness.Turn
	// Model is what the person named, resolved to an id this install has.
	// Empty is the ordinary turn — nobody said — and the run takes whatever
	// model the surface's own runner is built on.
	Model string
	// ModelNote is why a model that WAS named is not in Model: a word no model
	// here answers to, or one that half the catalog answers to. It rides the
	// card so the offer can say what it could not do, and it is never a refusal
	// — the harness still runs, on the default.
	ModelNote string
}

// harnessMatch is the detection pass: the cheap refusals, then the score.
//
// It is a method only for the config; the deciding is [subharness.Best] and
// nothing here adds to it.
func (a *Agent) harnessMatch(user userMessage) (harnessRoute, bool) {
	// The refusals, cheapest first. Every existing caller of this package fails
	// the first one and pays two nil checks per turn for the whole feature.
	if a.config.RunHarness == nil {
		return harnessRoute{}, false
	}
	registry := a.harnessRegistry()
	if len(registry) == 0 {
		return harnessRoute{}, false
	}
	if !a.config.AskConsent {
		return harnessRoute{}, false
	}
	// ONLY WHAT A PERSON TYPED IS MATCHED. A woken turn (task_run.go, jobs.go)
	// opens with an empty message and reads its note off the steering queue, and
	// a note the SESSION wrote is not somebody asking for a harness — offering
	// one against a task's own completion report would be the harness talking
	// itself into work nobody requested.
	if user.empty() || user.wake || user.authored {
		return harnessRoute{}, false
	}
	text := user.text()
	if strings.TrimSpace(text) == "" {
		return harnessRoute{}, false
	}
	// THE MODEL IS READ BEFORE THE SCORE. "research the pricing tiers with
	// opus" is a sentence about research, and the two words that chose the model
	// are not evidence about which harness was meant — left in, they are two
	// more words the cue list has to score around, and a person who names a
	// model would be quietly making the offer less likely to appear.
	model, note, text := a.harnessTurnModel(text)
	turn := subharness.Turn{Text: text}
	match, ok := subharness.Best(turn, registry)
	if !ok {
		return harnessRoute{}, false
	}
	return harnessRoute{Match: match, Turn: turn, Model: model, ModelNote: note}, true
}

// harnessRegistry is what a turn is matched against: the registry the surface
// handed over, plus whatever this conversation has designed and saved since
// (harness_build.go).
//
// A HARNESS BUILT HERE IS REACHABLE FROM THE NEXT SENTENCE. Config.Harnesses is
// a snapshot taken at launch, so without this a page approved a minute ago would
// be a page the store has and the matcher has never heard of — the person would
// have to restart to reach the thing they just built, which is the one moment
// they are most likely to want it.
//
// A name that appears in both lists is the SAVED one: it is the same harness at
// a later version, and the entry that carries its cues is the newer one.
func (a *Agent) harnessRegistry() []subharness.Entry {
	a.mu.Lock()
	added := a.harnessAdded
	a.mu.Unlock()
	if len(added) == 0 {
		// The ordinary case, and it allocates nothing: no conversation has built
		// a harness until one does.
		return a.config.Harnesses
	}
	out := make([]subharness.Entry, 0, len(a.config.Harnesses)+len(added))
	for _, entry := range a.config.Harnesses {
		if harnessNamed(added, entry.Name) {
			continue
		}
		out = append(out, entry)
	}
	return append(out, added...)
}

func harnessNamed(entries []subharness.Entry, name string) bool {
	for _, entry := range entries {
		if entry.Name == name {
			return true
		}
	}
	return false
}

// ── THE OTHER DOOR: A HARNESS SOMEBODY PICKED OUT OF A LIST ─────────────────
//
// Everything above this line is DETECTION: a sentence is scored, a card is
// raised, and a person answers it. That is the road for somebody who did not
// know the registry had the thing they were describing.
//
// This is the road for somebody who does. The surface has a picker on
// `/harness ` (tui3's harnesspick.go): the harness is chosen from a list, it
// sits in a chip above the message box, and the next thing typed is the
// request. Nothing about that needs matching or asking — the choice IS the
// answer — so this skips exactly those two steps and nothing else. The run is
// registered so it can be stopped, announced with the same event, and its
// report recorded as an assistant turn, because it goes through the same
// [Agent.runHarnessRoute] the offer's yes goes through.

// RunHarnessRequest runs one named harness on one request and streams the turn
// it becomes.
//
// It is a TURN and not a side channel: the request is recorded as the person's
// message, the report as the answer, and the events are the ones every surface
// already draws. So it refuses what Submit refuses — a closed agent, a turn
// already in flight, a session past its spend rail — rather than starting a
// second conversation beside the first.
//
// THE DETECTION REFUSALS DO NOT APPLY. A name that scores nothing, a turn that
// mentions no cue, a description nobody wrote well: none of it is read here.
// The person named the harness, and a matcher's opinion about a choice already
// made would be this surface overruling them.
//
// The model is the same clause the offer carries — a word this install can
// place, or empty for whatever the runner is built on. A word that resolves to
// nothing runs the default rather than refusing, which is the offer's law too.
func (a *Agent) RunHarnessRequest(ctx context.Context, name, text, model string) (<-chan Event, error) {
	name = strings.TrimSpace(name)
	text = strings.TrimSpace(text)
	switch {
	case name == "":
		return nil, errors.New("session: no harness was named")
	case text == "":
		return nil, errors.New("session: empty message")
	}
	// The registry and the model are resolved BEFORE the lock, because both read
	// seams the surface handed over and neither may be called with a.mu held.
	entry, found := harnessEntry(a.harnessRegistry(), name)
	if !found {
		return nil, fmt.Errorf("session: no harness named %q", name)
	}
	if model != "" {
		model, _ = a.harnessModel(model)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil, errors.New("session: agent is closed")
	}
	// A PICKED HARNESS NEVER STEERS. A plain message typed into a running turn
	// lands inside it (agent.go's Submit); a harness run cannot, because it does
	// not use the model this turn is talking to — so the honest answer is that
	// there is nowhere to put it yet.
	if a.running {
		return nil, errors.New("session: a turn is already running")
	}
	if a.config.RunHarness == nil {
		return nil, errors.New("session: harnesses are unavailable here")
	}
	if err := a.railBlockLocked(); err != nil {
		return refusedStream(err), nil
	}
	// The route is left where the turn will find it rather than passed down
	// through startTurnLocked: the turn is started by the ONE function every
	// turn is started by, and a second parameter on that function for a thing
	// one caller in twenty uses would be a signature the other nineteen read
	// past (see [Agent.takeHarnessPick]).
	a.harnessPick = &harnessRoute{
		Match: subharness.Match{Entry: entry},
		Turn:  subharness.Turn{Text: text},
		Model: model,
	}
	return a.startTurnLocked(ctx, userText(text), nil), nil
}

// takeHarnessPick is the turn collecting what [Agent.RunHarnessRequest] left
// for it, at most once: a picked harness is one run of one harness, and a route
// still sitting here at the next turn would be the harness taking a sentence
// nobody pointed at it.
func (a *Agent) takeHarnessPick() (harnessRoute, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	picked := a.harnessPick
	a.harnessPick = nil
	if picked == nil {
		return harnessRoute{}, false
	}
	return *picked, true
}

// harnessEntry finds one entry by name, case-insensitively — a name typed back
// or clicked out of a list is the same harness whatever case it arrived in.
func harnessEntry(entries []subharness.Entry, name string) (subharness.Entry, bool) {
	for _, entry := range entries {
		if strings.EqualFold(entry.Name, name) {
			return entry, true
		}
	}
	return subharness.Entry{}, false
}

// askHarness emits one offer and waits for the answer or for the turn to end.
//
// The wait is on the TURN's context, which is what makes Interrupt work on a
// pending card, and nothing here holds a.mu across it — the lock Interrupt
// needs must never be held by something waiting on a person.
func (a *Agent) askHarness(ctx context.Context, hub *eventHub, match harnessRoute) (harnessAnswer, error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return harnessAnswer{}, errAgentClosed
	}
	a.harnessSeq++
	id := a.harnessSeq
	answers := make(chan harnessAnswer, 1)
	if a.harnessAsks == nil {
		a.harnessAsks = make(map[uint64]harnessAsk, 1)
	}
	a.harnessAsks[id] = harnessAsk{answers: answers}
	a.mu.Unlock()

	hub.send(Event{
		Kind: EventHarnessOffer,
		ID:   id,
		Text: match.Entry.Name,
		// The entry's own sentence, so the card can say what saying yes would
		// get somebody without the surface writing a description of its own.
		Hint: match.Entry.Description,
		// And the model the person named, so the card says what yes would run
		// on — or why the word they used named nothing.
		Model:     match.Model,
		ModelNote: match.ModelNote,
	})

	select {
	case answer := <-answers:
		return answer, nil
	case <-ctx.Done():
		a.forgetHarness(id)
		return harnessAnswer{}, ctx.Err()
	}
}

// forgetHarness drops an offer nobody will answer. Without it an interrupted
// turn would leave its question in the map for the life of the session.
func (a *Agent) forgetHarness(id uint64) {
	a.mu.Lock()
	delete(a.harnessAsks, id)
	a.mu.Unlock()
}

// ── WHICH MODEL A RUN RIDES, WHEN THE TURN SAID SO ──────────────────────────
//
// A harness offer is raised by what somebody said, so the model one runs on is
// chosen the same way: "research the pricing tiers with opus" is one sentence
// carrying two decisions, and the second one is a clause at the end of it. This
// is the same bargain propose_task already keeps (taskmodel.go) and it shares
// that file's matcher outright, so a word that means one model to a task means
// the same model here.
//
// THREE RULES HOLD IT TO SOMETHING THAT CANNOT COST A PERSON A TURN.
//
//   - IT IS READ AT THE END, AND ONLY THERE. A trailing "with|using|via <word>"
//     is how a person hangs an aside off a sentence they already finished. The
//     same words in the middle are ordinary English — "find out what broke with
//     the new parser" chooses no model — and reading them would turn every
//     sentence into a place a model name could hide.
//   - A CLAUSE THAT NAMED NO MODEL IS LEFT WHERE IT WAS. The text is stripped
//     for scoring and for the runner only when the word RESOLVED. A word that
//     matched nothing was, on the evidence, not a model at all, and cutting it
//     off the turn would hand the harness half a sentence on the strength of a
//     guess.
//   - IT IS NEVER A REFUSAL. A model nobody here carries leaves the offer
//     standing and says so on the card, because the person asked for a harness
//     and the model was the smaller half of what they said.
const (
	// harnessModelWords bounds the clause. A model is one word — "opus",
	// "claude-opus-5", "anthropic/claude-opus-5" — and two or three are what a
	// person spells one with ("gpt 5 mini"). Past that it is a sentence.
	harnessModelWords = 3
)

// harnessModelPreps are the three words a model gets named after. They are the
// prepositions that take a means and not a subject: "run it with opus" chooses
// a model and "look into the crash in opus" does not.
var harnessModelPreps = map[string]bool{"with": true, "using": true, "via": true}

// harnessModelStop are the words that mean the clause is prose. Every one of
// them is a function word — an article, a conjunction, a pronoun — so the list
// can never quietly decide that somebody's noun was not a model name: "with the
// tests passing" is refused because it opens with "the", not because anything
// here has an opinion about tests.
var harnessModelStop = map[string]bool{
	"a": true, "an": true, "and": true, "any": true, "are": true, "as": true,
	"at": true, "be": true, "both": true, "but": true, "by": true, "each": true,
	"for": true, "from": true, "her": true, "his": true, "in": true, "into": true,
	"is": true, "it": true, "its": true, "many": true, "me": true, "more": true,
	"most": true, "my": true, "no": true, "not": true, "of": true, "on": true,
	"one": true, "or": true, "our": true, "some": true, "that": true, "the": true,
	"their": true, "them": true, "these": true, "they": true, "this": true,
	"those": true, "to": true, "us": true, "was": true, "were": true, "what": true,
	"which": true, "who": true, "with": true, "you": true, "your": true,
}

// harnessTurnModel reads one turn's trailing model clause and answers with the
// three things the offer needs: the model to run on, the note when a named model
// could not be found, and the text the harness is actually asked to do.
func (a *Agent) harnessTurnModel(text string) (model, note, rest string) {
	before, word, ok := splitHarnessModel(text)
	if !ok {
		return "", "", text
	}
	model, note = a.harnessModel(word)
	if model == "" {
		// Nothing was chosen, so nothing was said: the sentence goes on to the
		// matcher and to the run exactly as it was typed.
		return "", note, text
	}
	return model, "", before
}

// harnessModel resolves one named word against the models this install has.
//
// NOBODY HOLDING A LIST IS NOT A REFUSAL. taskmodel.go states the rule in full:
// a session with no catalog cannot validate an id, so the word travels as
// written and the provider answers for it. Everything else is the shared
// matcher, and only an unambiguous answer wins — a word half the catalog answers
// to has not named a model, it has named a family.
func (a *Agent) harnessModel(word string) (model, note string) {
	available := a.taskModelList()
	if len(available) == 0 {
		return word, ""
	}
	candidates := matchTaskModel(word, available)
	switch {
	case len(candidates) == 1:
		return candidates[0], ""
	case len(candidates) == 0:
		return "", "model " + quoteModel(word) + " not found, running default"
	default:
		return "", "model " + quoteModel(word) + " matches several here, running default"
	}
}

// answeredHarnessModel is which model the answer settled on: the surface's word
// when it resolves, and the offer's own model otherwise.
//
// A surface that hands back what the card showed lands on the same model it
// drew, which is the whole point — what ran is what the person read. A surface
// that hands back nothing, or a word this session cannot place, gets the model
// the offer was raised with, because an unresolvable answer must not become a
// run on a model nobody has.
func (a *Agent) answeredHarnessModel(match harnessRoute, answered string) string {
	if answered = strings.TrimSpace(answered); answered == "" {
		return match.Model
	}
	if model, _ := a.harnessModel(answered); model != "" {
		return model
	}
	return match.Model
}

// splitHarnessModel cuts a trailing "with|using|via <model words>" off a turn.
//
// It answers the text before the clause and the word inside it, and false for
// every sentence that has no such clause — which is nearly all of them. The
// clause has to be the LAST thing in the text, the preposition has to be a whole
// word, and something has to be left in front of it: "with opus" alone is not a
// turn asking for a harness, it is a fragment.
func splitHarnessModel(text string) (rest, word string, ok bool) {
	words, offsets := textWords(text)
	// The LAST preposition is the one that carries the clause, so that "compare
	// the two with opus" reads past nothing and "look into it using the notes
	// with opus" still lands on the tail.
	for at := len(words) - 1; at >= 1; at-- {
		if !harnessModelPreps[strings.ToLower(words[at])] {
			continue
		}
		tail := words[at+1:]
		if !modelWords(tail) {
			// A preposition with prose behind it is prose. Nothing earlier in the
			// sentence can be the clause either — the clause is the END of the
			// text — so this is the whole answer.
			return "", "", false
		}
		before := strings.TrimRight(text[:offsets[at]], " \t\r\n")
		if strings.TrimSpace(before) == "" {
			return "", "", false
		}
		return before, strings.Join(tail, " "), true
	}
	return "", "", false
}

// modelWords reports whether a clause's words could spell one model id: a few of
// them, none of them a function word, every one of them made of the characters
// an id is written with, and the FIRST of them carrying a letter.
//
// The first word is where a model's name is — "gpt 5 mini" is three words and
// one id — and holding it to a letter is what keeps a quantity out: "with 3
// sources" is a sentence about sources, and nothing that opens with a number is
// a model somebody named.
func modelWords(words []string) bool {
	if len(words) == 0 || len(words) > harnessModelWords {
		return false
	}
	for _, word := range words {
		if harnessModelStop[strings.ToLower(word)] || !modelWordShape(word) {
			return false
		}
	}
	return hasLetter(words[0])
}

// modelWordShape holds one word to the characters model ids are spelled with:
// letters, digits, and the separators vendors use. A word carrying a comma, a
// quote or a question mark is punctuation from the sentence around it, and a
// sentence is not an id.
func modelWordShape(word string) bool {
	for _, r := range word {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '.', r == '_', r == '/', r == ':', r == '~':
		default:
			return false
		}
	}
	return word != ""
}

func hasLetter(word string) bool {
	for _, r := range word {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// textWords cuts text into words with the byte offset each one starts at. The
// offsets are what let the clause be cut off the ORIGINAL text: rebuilding the
// sentence from its words would hand the runner a turn with its own line breaks
// and spacing quietly rewritten.
func textWords(text string) ([]string, []int) {
	var words []string
	var offsets []int
	start := -1
	for at, r := range text {
		if unicode.IsSpace(r) {
			if start >= 0 {
				words = append(words, text[start:at])
				offsets = append(offsets, start)
				start = -1
			}
			continue
		}
		if start < 0 {
			start = at
		}
	}
	if start >= 0 {
		words = append(words, text[start:])
		offsets = append(offsets, start)
	}
	return words, offsets
}
