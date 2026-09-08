// Package reflex is the per-turn tier's client: the small structured calls a
// conversation makes AROUND an exchange rather than in it.
//
// Three calls live here. [Route] runs before the turn — it reads the message
// just typed and the index of what is already remembered, and answers which
// remembered lines this turn actually needs. [Extract] runs after it — it reads
// the exchange and answers whether anything in it is worth keeping. [Decide]
// settles what to do with something worth keeping when the store already holds
// something near it.
//
// They share one shape and one law:
//
//   - THE MODEL IS A REFLEX, NOT A THINKER. Every call goes out with a small
//     prompt, a ~200 token ceiling, temperature 0 and the thinking pass turned
//     OFF, on [roles.RoleReflex] — its own tier ([roles.TierReflex]) precisely
//     because a call made twice per turn is a different economy from one made
//     once a session. Nothing here asks the model to reason; it sorts, it
//     names, and it answers in a few words of JSON. The disable is not an
//     optimisation but the thing that makes the ceiling honest: a reasoning
//     model left to deliberate spends the whole 200 doing it and answers
//     nothing at all.
//
//   - REFLEX NEVER BREAKS A TURN. A model that answers with prose, with a code
//     fence, or with an enum this package has never heard of costs ONE repair
//     retry. A model that spends its ceiling and answers nothing gets ONE larger
//     retry, then the session's low-tier fallback. After that every failure is a
//     zero result and [ErrReflexFailed]. Every caller treats that as a no-op:
//     the turn happens exactly as it would have if this package did not exist.
//     That is why the failure is a typed error rather than a returned
//     half-answer — there is no such thing as a partly-routed turn.
//
// The package is the client only. It makes no decision about WHEN a call
// happens, holds no store, and is wired into no loop; the chat integration and
// the memory store are separate slices that build against the types below.
package reflex

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Completer is the one-method slice of the provider client this package needs.
// It is spelled the same way internal/plan ([plan.Completer]) and
// internal/session ([session.Completer]) spell it — one method, the same
// signature — so the client a session already holds satisfies it without an
// adapter, and a test satisfies it with a struct that returns a string.
//
// It is declared HERE rather than imported from either of them because both of
// those packages would drag their whole world in: internal/reflex is called
// from a turn loop and must not be a reason internal/session cannot compile.
type Completer interface {
	CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error)
}

// ErrReflexFailed is every way a reflex call can fail to produce something
// usable: the provider refused, the model went quiet, or two attempts in a row
// were not a JSON object this package could read.
//
// It is ONE error on purpose. A caller has exactly one thing to do with any of
// those — nothing — and a taxonomy would invite a turn loop to handle a
// provider timeout differently from a bad enum, which is the beginning of a
// reflex that can break a turn.
var ErrReflexFailed = errors.New("reflex: no usable answer")

const (
	// AnswerTokens is the ordinary reflex answer ceiling. It is exported so the
	// accounting layer can recognize the exact paid ceiling in its provider
	// test doubles without repeating this package's economic constant.
	AnswerTokens = 200

	// answerTokens is the ceiling on every reflex reply. The largest legitimate
	// answer here is an extraction with a state delta in it — a handful of short
	// lines — and a model that wants more than this is not answering the
	// question it was asked.
	//
	// It is only honest because the reflex asks for the thinking pass to be
	// turned OFF (see [call]). A reasoning model handed this ceiling with its
	// thinking left on spends all two hundred deliberating and returns an empty
	// answer, every call, every turn — which is what
	// nex-agi/nex-n2-mini did on the reflex tier: repeated 640-in/200-out calls
	// billed in full for nothing.
	answerTokens = AnswerTokens

	// thinkingAnswerTokens is the ceiling when the disable was refused —
	// [provider.ReasoningMandatory], a fact this process learned from a 400 and
	// not from any published field.
	//
	// A model that MUST think still has to be asked, so the only remaining move
	// is to leave room for the thinking pass in front of the answer. Ten times
	// the answer ceiling is that room: a thinking pass on a question this small
	// runs to a few hundred tokens, and the multiple still stops a model that
	// has decided to write an essay well before it costs what a turn costs.
	// ThinkingAnswerTokens is the recovery ceiling when reasoning cannot be
	// removed. It is derived from the ordinary answer budget so the ratio has
	// one source.
	ThinkingAnswerTokens = AnswerTokens * 10
	thinkingAnswerTokens = ThinkingAnswerTokens

	// messageLimit is how much of one side of an exchange the model is shown,
	// in runes. A router that reads four paragraphs to decide which memory a
	// turn needs is not a reflex, and the decision lives in the opening of a
	// message far more often than in its tail.
	messageLimit = 2000

	// indexLimit is how many remembered lines the router is shown. Two hundred
	// short titles is already a large prompt for a near-free model, and an index
	// past that is a store that owes the router a ranking rather than a dump.
	indexLimit = 200

	// neighborLimit is how many near-duplicates [Decide] is shown. Three is
	// what the store's own search returns as "close"; a fourth adds length to
	// the prompt and nothing to the answer.
	neighborLimit = 3
)

// The enums. They are declared once and rendered INTO the prompts (prompts.go),
// so the words the model is told to use and the words this package accepts back
// cannot drift — a validator that rejects a word its own prompt asked for is a
// call that fails every time and looks like a bad model.
var (
	memoryTypes  = []string{"fact", "preference", "decision", "correction", "project_state"}
	memoryScopes = []string{"user", "project", "env"}
	decideOps    = []string{"add", "update", "supersede", "skip"}
	commandNames = []string{"remember", "forget"}
)

// Stub is one line of the memory index as the router sees it: enough to decide
// whether the line matters to this turn, and never the line's own text.
type Stub struct{ ID, Title, Type, Scope string }

// Cmd is an explicit instruction the person gave about memory itself. Name is
// one of "remember" or "forget".
type Cmd struct{ Name, Arg string }

// RouteResult is the pre-turn answer: which remembered ids belong in this turn,
// and the memory command the person typed, if they typed one.
type RouteResult struct {
	// Inject holds ids FROM THE INDEX IT WAS GIVEN. An id the model invented is
	// dropped rather than passed on — the store would not find it, and a
	// missing memory reported as an injected one is a lie the caller cannot
	// check.
	Inject []string
	// Cmd is nil on almost every turn.
	Cmd *Cmd
}

// StateDelta is what an exchange did to the shape of the work: where it is
// going, what has landed, what is in flight, what is next, what is still open,
// and what it points at.
type StateDelta struct {
	Goal     string
	Done     []string
	Inflight []string
	Next     []string
	Open     []string
	Refs     []string
}

// ExtractResult is the post-turn answer.
//
// Mem is the whole gate: 0 means the exchange held nothing worth carrying into
// another session, which is the answer for most exchanges, and every other
// field is then meaningless and unread.
type ExtractResult struct {
	Mem   int
	Type  string
	Scope string
	Title string
	Text  string
	Tags  []string
	// State is present only when the exchange MOVED the work. A turn that
	// answered a question changed nothing about where the work stands.
	State *StateDelta
	// Used names the injected memory ids that actually BORE ON THE ANSWER, out
	// of the ones this turn was shown. It is asked of a model that is already
	// reading the exchange, so it costs no extra call and about ten output
	// tokens.
	//
	// It exists because counting a retrieval as a use credits a memory for
	// being INJECTED rather than for helping — RoMeRL (arXiv 2608.02508) names
	// that the "memory-reward trap", and fixing the credit assignment is what
	// shrank its memory pool 84.4% for +2.9pp accuracy. An id the model
	// invented is dropped, exactly as [RouteResult.Inject] drops one.
	//
	// It is empty on every turn nothing was injected, and empty is also the
	// honest answer when lines were shown and none of them mattered — the
	// caller reads that difference against what it injected.
	Used []string
}

// Neighbor is one thing the store already holds that sits near a candidate.
type Neighbor struct{ ID, Title, Text string }

// DecideResult is what to do with a candidate given what is already there.
type DecideResult struct {
	Op       string
	TargetID string
	Title    string
	Text     string
	Tags     []string
}

// Model resolves which model the reflex calls run on: the [roles] ladder for
// [roles.RoleReflex] — its pin, then its tier, then the model the session is
// already talking to.
//
// It is here rather than in the three call sites because the ladder is a
// property of the ROLE, and a caller that resolved it itself would be a second
// place that decides what "reflex" means.
func Model(src roles.Source, sessionDefault string) (string, error) {
	return roles.Resolve(src, roles.RoleReflex, sessionDefault)
}

// FallbackModel resolves the configured low tier without borrowing a role pin.
// A reflex model that cannot answer is a failure of that tier's model, so the
// fallback is the next economy itself rather than whichever unrelated low-tier
// role happens to have a private override.
func FallbackModel(src roles.Source, sessionDefault string) string {
	if configured, ok := roles.TierModel(src, roles.TierLow); ok {
		model, _ := roles.SplitEffort(configured)
		if model = strings.TrimSpace(model); model != "" {
			return model
		}
	}
	return strings.TrimSpace(sessionDefault)
}

// Session holds the reflex failover facts that belong to one conversation.
// Provider quirks are process-wide because they describe a model; abandoning a
// model is session-local because another session may have changed its crew or
// may deliberately want to try it again.
type Session struct {
	mu       sync.Mutex
	unusable map[string]bool
	notified map[string]bool
}

// Bind returns a [Completer] that sends every request to one model.
//
// The three calls take a plain Completer because that is the contract the chat
// and store slices build against, and a model has to reach the request
// somehow: a caller resolves once with [Model], binds once with this, and hands
// the result to as many calls as it likes. A session's own client is left
// untouched, which matters — it is the same client the conversation runs on.
func Bind(c Completer, model string) Completer {
	if strings.TrimSpace(model) == "" {
		return c
	}
	return bound{inner: c, model: model, session: &Session{}}
}

// Bind returns a completer that starts on model and, once that model has twice
// spent its ceiling without answering, uses fallback for the rest of this
// session. notice is called only on the transition and never while a lock is
// held.
func (s *Session) Bind(c Completer, model, fallback string, notice func(string)) Completer {
	model = strings.TrimSpace(model)
	if model == "" {
		return c
	}
	if s == nil {
		s = &Session{}
	}
	return bound{inner: c, model: model, fallback: strings.TrimSpace(fallback), session: s, notice: notice}
}

type bound struct {
	inner    Completer
	model    string
	fallback string
	session  *Session
	notice   func(string)
}

func (b bound) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	// A fresh slice, never an append onto the caller's: options is the caller's
	// array and appending into its spare capacity would write the model pin into
	// a slice somebody else is still holding.
	pinned := make([]ai.Option, 0, len(options)+1)
	pinned = append(pinned, options...)
	pinned = append(pinned, ai.WithModel(b.activeModel()))
	return b.inner.CompleteWithMessages(ctx, messages, pinned...)
}

func (b bound) activeModel() string {
	if b.session == nil {
		return b.model
	}
	b.session.mu.Lock()
	defer b.session.mu.Unlock()
	if b.session.unusable[normalizeModel(b.model)] && !sameModel(b.model, b.fallback) {
		return b.fallback
	}
	return b.model
}

// abandon moves this session off the primary model and reports whether there is
// a distinct fallback to ask. The calm line is emitted once, after the state is
// visible to every concurrent reflex call.
func (b bound) abandon(model string) bool {
	if b.session == nil || sameModel(model, b.fallback) || !sameModel(model, b.model) {
		return false
	}
	key := normalizeModel(b.model)
	b.session.mu.Lock()
	if b.session.unusable == nil {
		b.session.unusable = make(map[string]bool)
	}
	if b.session.notified == nil {
		b.session.notified = make(map[string]bool)
	}
	b.session.unusable[key] = true
	say := !b.session.notified[key]
	b.session.notified[key] = true
	b.session.mu.Unlock()
	if say && b.notice != nil {
		b.notice("the reflex model answers nothing at its budget; using " + b.fallback + " for this session")
	}
	return true
}

func normalizeModel(model string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(model), "~"))
}

func sameModel(left, right string) bool {
	right = normalizeModel(right)
	return right != "" && normalizeModel(left) == right
}

// Route answers which remembered lines this turn needs, before the turn runs.
//
// An empty list is the ordinary answer and is not a failure. A message with
// nothing in it is answered without a call at all: there is nothing to route
// against, and a reflex that bills for that would bill for every stray return.
func Route(ctx context.Context, c Completer, userMsg string, index []Stub) (RouteResult, error) {
	if strings.TrimSpace(userMsg) == "" {
		return RouteResult{}, nil
	}
	// Recall is on the first answer's critical path, so the whole operation,
	// including repair and model fallback, shares the interactive silence cap.
	// A deadline here lets the caller proceed without memory when a provider
	// stalls instead of inheriting the transport's multi-minute safety bound.
	ctx, cancel := context.WithTimeout(ctx, lane.VisiblePatience)
	defer cancel()
	known := make(map[string]bool, len(index))
	for _, stub := range index {
		if id := strings.TrimSpace(stub.ID); id != "" {
			known[id] = true
		}
	}
	// RECALL IS ON THE ANSWER'S CRITICAL PATH. Its output stays private, but
	// somebody is waiting for it, so the existing chooser values latency and
	// can recover within the shared cap instead of pricing it as background work.
	var result RouteResult
	err := ask(provider.WithRole(ctx, lane.RoleRecall), c, routePrompt, routeInput(userMsg, index), func(reply string) error {
		var wire struct {
			Inject []string `json:"inject"`
			Cmd    *struct {
				Name string `json:"name"`
				Arg  string `json:"arg"`
			} `json:"cmd"`
		}
		if err := decode(reply, &wire); err != nil {
			return err
		}
		next := RouteResult{}
		for _, id := range wire.Inject {
			id = strings.TrimSpace(id)
			if id != "" && known[id] {
				next.Inject = append(next.Inject, id)
			}
		}
		if wire.Cmd != nil {
			name := strings.ToLower(strings.TrimSpace(wire.Cmd.Name))
			if !allowed(commandNames, name) {
				return fmt.Errorf("reflex: %q is not a memory command", wire.Cmd.Name)
			}
			next.Cmd = &Cmd{Name: name, Arg: strings.TrimSpace(wire.Cmd.Arg)}
		}
		result = next
		return nil
	})
	if err != nil {
		return RouteResult{}, err
	}
	return result, nil
}

// Extract answers whether an exchange held anything worth remembering — and,
// when the turn was shown remembered lines, which of them actually bore on the
// answer.
//
// injected is what the router put in front of the model for this exchange, and
// it is shown to the extractor for that second question alone. An empty list
// is the ordinary case and the question is then not asked at all: a heading
// with nothing under it reads to a small model like a list it failed to
// receive.
//
// Mem 0 is returned as it arrived, with NO further validation of the memory
// fields: the model has said there is nothing here, they are whatever it left
// in them, and refusing that answer over a stray type would turn "nothing to
// remember" — the common case — into a retry on every turn. Used is read
// whatever mem says, because whether a memory helped and whether the exchange
// held something new are different questions about the same turn.
func Extract(ctx context.Context, c Completer, userMsg, assistantMsg string, injected []Stub) (ExtractResult, error) {
	if strings.TrimSpace(userMsg) == "" && strings.TrimSpace(assistantMsg) == "" {
		return ExtractResult{}, nil
	}
	shown := make(map[string]bool, len(injected))
	for _, stub := range injected {
		if id := strings.TrimSpace(stub.ID); id != "" {
			shown[id] = true
		}
	}
	// THIS ONE IS THE MEMORY'S AND NOT THE TURN'S. What it asks is whether the
	// exchange held something worth keeping, and what it costs and what it is
	// worth are the memory reflex's ([lane.RoleMemory]) rather than a side
	// errand's — the turn has already happened either way.
	var result ExtractResult
	err := ask(provider.WithRole(ctx, lane.RoleMemory), c, extractPrompt, extractInput(userMsg, assistantMsg, injected), func(reply string) error {
		var wire struct {
			Mem   int      `json:"mem"`
			Type  string   `json:"type"`
			Scope string   `json:"scope"`
			Title string   `json:"title"`
			Text  string   `json:"text"`
			Tags  []string `json:"tags"`
			Used  []string `json:"used"`
			State *struct {
				Goal     string   `json:"goal"`
				Done     []string `json:"done"`
				Inflight []string `json:"inflight"`
				Next     []string `json:"next"`
				Open     []string `json:"open"`
				Refs     []string `json:"refs"`
			} `json:"state"`
		}
		if err := decode(reply, &wire); err != nil {
			return err
		}
		if wire.Mem != 0 && wire.Mem != 1 {
			return fmt.Errorf("reflex: mem is %d, which is neither 0 nor 1", wire.Mem)
		}
		next := ExtractResult{Mem: wire.Mem}
		if wire.Mem == 1 {
			next.Type = strings.ToLower(strings.TrimSpace(wire.Type))
			next.Scope = strings.ToLower(strings.TrimSpace(wire.Scope))
			if !allowed(memoryTypes, next.Type) {
				return fmt.Errorf("reflex: %q is not a memory type", wire.Type)
			}
			if !allowed(memoryScopes, next.Scope) {
				return fmt.Errorf("reflex: %q is not a memory scope", wire.Scope)
			}
			next.Title = strings.TrimSpace(wire.Title)
			next.Text = strings.TrimSpace(wire.Text)
			next.Tags = cleaned(wire.Tags)
		}
		for _, id := range wire.Used {
			id = strings.TrimSpace(id)
			if id != "" && shown[id] {
				next.Used = append(next.Used, id)
			}
		}
		if wire.State != nil {
			next.State = &StateDelta{
				Goal:     strings.TrimSpace(wire.State.Goal),
				Done:     cleaned(wire.State.Done),
				Inflight: cleaned(wire.State.Inflight),
				Next:     cleaned(wire.State.Next),
				Open:     cleaned(wire.State.Open),
				Refs:     cleaned(wire.State.Refs),
			}
		}
		result = next
		return nil
	})
	if err != nil {
		return ExtractResult{}, err
	}
	return result, nil
}

// Decide settles a candidate against what the store already holds near it: skip
// it, refine one of them, replace one of them, or add it.
//
// A missing TargetID on an update or a supersede is NOT refused here — the
// enum is what this package validates, and the store is the only thing that
// knows whether an id is real. A caller that gets one treats it as a skip.
func Decide(ctx context.Context, c Completer, candidate ExtractResult, neighbors []Neighbor) (DecideResult, error) {
	// The memory's again, for [Extract]'s reason: this call exists only because
	// something was already judged worth keeping, and what to do with it is a
	// question about the store rather than about the turn.
	var result DecideResult
	err := ask(provider.WithRole(ctx, lane.RoleMemory), c, decidePrompt, decideInput(candidate, neighbors), func(reply string) error {
		var wire struct {
			Op       string   `json:"op"`
			TargetID string   `json:"target_id"`
			Title    string   `json:"title"`
			Text     string   `json:"text"`
			Tags     []string `json:"tags"`
		}
		if err := decode(reply, &wire); err != nil {
			return err
		}
		op := strings.ToLower(strings.TrimSpace(wire.Op))
		if !allowed(decideOps, op) {
			return fmt.Errorf("reflex: %q is not one of %s", wire.Op, strings.Join(decideOps, ", "))
		}
		result = DecideResult{
			Op:       op,
			TargetID: strings.TrimSpace(wire.TargetID),
			Title:    strings.TrimSpace(wire.Title),
			Text:     strings.TrimSpace(wire.Text),
			Tags:     cleaned(wire.Tags),
		}
		return nil
	})
	if err != nil {
		return DecideResult{}, err
	}
	return result, nil
}

// ── the call ────────────────────────────────────────────────────────────────

// ask makes one reflex call and, if the answer was not readable, exactly one
// repair call. The answer-budget recovery lives inside [call], where the
// response still carries its finish reason and usage.
//
// ONE retry, never a loop. The repair asks for the same thing in four words
// with the model's own bad answer in front of it, which is what fixes a fenced
// or prefaced reply; a model that misses twice is a model that is going to miss
// again, and a third attempt on a per-turn call is a cost multiplier on a
// feature whose whole claim is that it is nearly free.
func ask(ctx context.Context, c Completer, system, user string, read func(reply string) error) error {
	// Reflex bypasses the session's ordinary auxiliary adapter. It must still
	// share the same tier patience across retries rather than wait on HTTP caps.
	ctx, cancel := context.WithTimeout(ctx, roles.PatienceFor(roles.RoleReflex))
	defer cancel()
	messages := []ai.Message{message("system", system), message("user", user)}
	reply, err := call(ctx, c, messages)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrReflexFailed, err)
	}
	if read(reply) == nil {
		return nil
	}
	if strings.TrimSpace(reply) == "" {
		// NOTHING CAME BACK that the JSON repair can use. A length-capped empty
		// answer already took its one budget recovery inside [call]; every other
		// blank has no evidence that repeating the question can help.
		return fmt.Errorf("%w: the model answered nothing", ErrReflexFailed)
	}
	repair := []ai.Message{
		messages[0], messages[1],
		message("assistant", reply),
		message("user", repairInstruction),
	}
	second, err := call(ctx, c, repair)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrReflexFailed, err)
	}
	if err := read(second); err != nil {
		return fmt.Errorf("%w: %w", ErrReflexFailed, err)
	}
	return nil
}

// call is one request, shaped the way every reflex request is shaped.
//
// WithoutStream for the reason the title and the consolidator use it: nobody
// asked for this call, and streaming it would type a fragment of JSON into a
// room where a person is reading an answer.
//
// [provider.EffortOff] because THE MODEL IS A REFLEX, NOT A THINKER — the law
// at the top of this file, finally said to the provider instead of only to the
// prompt. It sends {"reasoning": {"enabled": false}}, so the model spends its
// whole ceiling on the answer rather than deliberating first. It is REQUIRED
// rather than a best-effort economy: a cold catalog must not erase the field
// that makes this small ceiling usable.
//
// Some endpoints accept that field and ignore it. Their signature is an empty
// answer, finish reason "length", and the whole ceiling charged as output. That
// buys ONE retry with [thinkingAnswerTokens]. An answer records the quirk for
// later calls and later processes; a second empty answer abandons this session's
// reflex model and asks the configured low tier once. There is no loop.
func call(ctx context.Context, c Completer, messages []ai.Message) (string, error) {
	// Every reflex request in the process passes through here, so this is the
	// one place its rows in the model-call log get their word.
	ctx = provider.WithCallTag(ctx, "reflex")
	ctx = provider.WithRequiredReasoningEffort(provider.WithoutStream(ctx), provider.EffortOff)
	budget := answerBudget(c)
	response, err := complete(ctx, c, messages, budget)
	if err != nil {
		return "", err
	}
	if response == nil {
		return "", errors.New("the model answered nothing")
	}
	if !emptyAtCeiling(response, budget) {
		return response.Text(), nil
	}

	model := boundModel(c)
	if budget < thinkingAnswerTokens {
		response, err = complete(ctx, c, messages, thinkingAnswerTokens)
		if err != nil {
			return "", err
		}
		if response != nil && strings.TrimSpace(response.Text()) != "" {
			if model != "" {
				provider.NoteReasoningDisableIgnored(model)
			}
			return response.Text(), nil
		}
	}

	pinned, ok := c.(bound)
	if !ok || !pinned.abandon(model) {
		if response == nil {
			return "", errors.New("the model answered nothing")
		}
		return response.Text(), nil
	}
	response, err = complete(ctx, c, messages, answerBudget(c))
	if err != nil {
		return "", err
	}
	if response == nil {
		return "", errors.New("the fallback model answered nothing")
	}
	return response.Text(), nil
}

func complete(ctx context.Context, c Completer, messages []ai.Message, budget int) (*ai.Response, error) {
	return c.CompleteWithMessages(ctx, messages, ai.WithMaxTokens(budget))
}

// emptyAtCeiling is deliberately narrower than "blank". A refusal or a cut
// stream also has no text, but only a length finish at the requested ceiling
// says that more answer room can change the result.
func emptyAtCeiling(response *ai.Response, budget int) bool {
	return provider.EmptyAtCeiling(response, budget)
}

// EmptyAtCeiling exposes the failure signature to the accounting wrapper that
// journals each paid reflex call. Detection and recovery must read the same
// definition or the bill can call a request empty that the caller did not.
func EmptyAtCeiling(response *ai.Response, budget *int) bool {
	return budget != nil && emptyAtCeiling(response, *budget)
}

func boundModel(c Completer) string {
	if pinned, ok := c.(bound); ok {
		return pinned.activeModel()
	}
	return ""
}

// answerBudget is the ceiling for one call, and it has two values because one
// of them is a lie on a model that cannot be told to stop thinking.
//
// The model is known only to [Bind] — the three calls take a plain Completer,
// which is what lets a test satisfy this package with a struct — so the pin is
// where the question is asked. An unbound completer, and any model whose
// endpoint has never refused the disable, gets [answerTokens] exactly as
// before; only a model this process has been told 400s the disable is given
// [thinkingAnswerTokens], and that fact is learned, never guessed.
func answerBudget(c Completer) int {
	if pinned, ok := c.(bound); ok && provider.ReasoningUnavoidable(pinned.activeModel()) {
		return thinkingAnswerTokens
	}
	return answerTokens
}

// decode reads the model's reply as one JSON object, through the shared
// provider decoder — which already tolerates a ```json fence and prose either
// side of it by taking the first balanced {...} it can parse, and refuses to
// take one found INSIDE an object that never closes, because a fragment of a
// truncated answer is not the answer. Spelling that walk again here would be a
// second answer to "what counts as JSON in a reply", and the two would drift.
func decode(reply string, into any) error {
	return provider.DecodeJSONObject(reply, into)
}

func message(role, text string) ai.Message {
	return ai.Message{Role: role, Content: []ai.ContentPart{{Type: "text", Text: text}}}
}

func allowed(list []string, value string) bool {
	for _, word := range list {
		if word == value {
			return true
		}
	}
	return false
}

// cleaned drops the blanks a model leaves in a list it had nothing to put in.
// An empty list stays nil, because nil is what "nothing" renders as everywhere
// in this tree.
func cleaned(values []string) []string {
	var kept []string
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			kept = append(kept, trimmed)
		}
	}
	return kept
}
