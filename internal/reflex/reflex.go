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
//     prompt, a ~200 token ceiling and temperature 0, on [roles.RoleReflex] —
//     its own tier ([roles.TierReflex]) precisely because a call made twice per
//     turn is a different economy from one made once a session. Nothing here
//     asks the model to reason; it sorts, it names, and it answers in a few
//     words of JSON.
//
//   - REFLEX NEVER BREAKS A TURN. A model that answers with prose, with a code
//     fence, with an enum this package has never heard of, or with nothing at
//     all, costs ONE repair retry and then a zero result and [ErrReflexFailed].
//     Every caller treats that as a no-op: the turn happens exactly as it would
//     have if this package did not exist. That is why the failure is a typed
//     error rather than a returned half-answer — there is no such thing as a
//     partly-routed turn.
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
	// answerTokens is the ceiling on every reflex reply. The largest legitimate
	// answer here is an extraction with a state delta in it — a handful of short
	// lines — and a model that wants more than this is not answering the
	// question it was asked.
	answerTokens = 200

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
	return bound{inner: c, model: model}
}

type bound struct {
	inner Completer
	model string
}

func (b bound) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	// A fresh slice, never an append onto the caller's: options is the caller's
	// array and appending into its spare capacity would write the model pin into
	// a slice somebody else is still holding.
	pinned := make([]ai.Option, 0, len(options)+1)
	pinned = append(pinned, options...)
	pinned = append(pinned, ai.WithModel(b.model))
	return b.inner.CompleteWithMessages(ctx, messages, pinned...)
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
	known := make(map[string]bool, len(index))
	for _, stub := range index {
		if id := strings.TrimSpace(stub.ID); id != "" {
			known[id] = true
		}
	}
	var result RouteResult
	err := ask(ctx, c, routePrompt, routeInput(userMsg, index), func(reply string) error {
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

// Extract answers whether an exchange held anything worth remembering.
//
// Mem 0 is returned as it arrived, with NO further validation: the model has
// said there is nothing here, the other fields are whatever it left in them,
// and refusing that answer over a stray type would turn "nothing to remember"
// — the common case — into a retry on every turn.
func Extract(ctx context.Context, c Completer, userMsg, assistantMsg string) (ExtractResult, error) {
	if strings.TrimSpace(userMsg) == "" && strings.TrimSpace(assistantMsg) == "" {
		return ExtractResult{}, nil
	}
	var result ExtractResult
	err := ask(ctx, c, extractPrompt, extractInput(userMsg, assistantMsg), func(reply string) error {
		var wire struct {
			Mem   int      `json:"mem"`
			Type  string   `json:"type"`
			Scope string   `json:"scope"`
			Title string   `json:"title"`
			Text  string   `json:"text"`
			Tags  []string `json:"tags"`
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
	var result DecideResult
	err := ask(ctx, c, decidePrompt, decideInput(candidate, neighbors), func(reply string) error {
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
// more.
//
// ONE retry, never a loop. The repair asks for the same thing in four words
// with the model's own bad answer in front of it, which is what fixes a fenced
// or prefaced reply; a model that misses twice is a model that is going to miss
// again, and a third attempt on a per-turn call is a cost multiplier on a
// feature whose whole claim is that it is nearly free.
func ask(ctx context.Context, c Completer, system, user string, read func(reply string) error) error {
	messages := []ai.Message{message("system", system), message("user", user)}
	reply, err := call(ctx, c, messages)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrReflexFailed, err)
	}
	if read(reply) == nil {
		return nil
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
func call(ctx context.Context, c Completer, messages []ai.Message) (string, error) {
	response, err := c.CompleteWithMessages(provider.WithoutStream(ctx), messages,
		ai.WithMaxTokens(answerTokens), ai.WithTemperature(0))
	if err != nil {
		return "", err
	}
	if response == nil {
		return "", errors.New("the model answered nothing")
	}
	return response.Text(), nil
}

// decode reads the model's reply as one JSON object, through the shared
// provider decoder — which already tolerates a ```json fence and prose either
// side of it by taking the first BALANCED {...} it can parse. Spelling that
// walk again here would be a second answer to "what counts as JSON in a reply",
// and the two would drift.
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
