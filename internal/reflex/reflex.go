// Package reflex is the per-turn tier's client: the one small structured call a
// conversation makes AFTER an exchange rather than in it.
//
// [Notice] reads the exchange that just ended and answers two things in one
// call: what the exchange did to the shape of the work (the state card's delta,
// internal/session's card.go), and — when the conversation is allowed to keep
// things on its own — whether the person said something durable worth keeping
// for every future conversation.
//
// IT IS THE ONLY CALL. There used to be three: a router before every turn that
// chose which remembered lines a message needed, this one, and a decider that
// settled each new line against its neighbours. Local activation now assembles
// each turn's working set without a model call. This call sees admitted lines
// and identifies actual use, so it can also say "that is already
// kept" or "this replaces line 3" itself.
//
// It keeps the shape and the law the three shared:
//
//   - THE MODEL IS A REFLEX, NOT A THINKER, AND THE PROMPT IS WHERE THAT IS
//     SAID. The call goes out with a small prompt on [roles.RoleReflex]. Nothing
//     here asks the model to reason; it names, and it answers in a few words of
//     JSON. How deeply somebody else's model thinks is not this package's to
//     decide, so no ceiling and no reasoning switch travel on the wire.
//
//   - REFLEX NEVER BREAKS A TURN. A model that answers with prose, with a code
//     fence, or with a word this package has never heard of costs ONE repair
//     retry. A model that answers nothing gets the session's low-tier fallback.
//     A transient request failure also gets one bounded fallback attempt.
//     Remaining failures return [ErrReflexFailed]; the owner may retry the
//     original exchange in the background without interrupting its turn.
//
// The package is the client only. It makes no decision about WHEN a call
// happens and holds no store.
package reflex

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
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
// The error remains recognizable without losing its wrapped provider cause.
// The owner can recover a transient failure off the turn's path, while malformed
// output and permanent refusals do not trigger another background pass.
var ErrReflexFailed = errors.New("reflex: no usable answer")

// messageLimit is how much of one side of an exchange the model is shown, in
// runes. The durable thing a person says lives in the opening of a message far
// more often than in its tail.
const messageLimit = 2000

// keptLimit is how many already-kept lines the model is shown. It is the
// conversation's whole memory block in the ordinary case; past it the oldest
// are shown, which is the same order the block itself carries.
const keptLimit = 80

// memoryScopes is the one list of scope words, declared once and rendered INTO
// the prompt (prompts.go) so the words the model is told to use and the words
// this package accepts back cannot drift.
var memoryScopes = []string{"user", "project", "env"}

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

// NoticeResult is the post-turn answer.
type NoticeResult struct {
	// Keep is the one line worth keeping for every future conversation, in the
	// person's own terms, or "" — which is the answer for most exchanges, and
	// always the answer when the call was not asked about memory.
	Keep string
	// Durability classifies capture; ephemeral candidates never reach storage.
	Durability string
	// Used names the 1-based kept lines actually applied in the answer.
	Used []int
	// Contradicted names kept lines explicitly corrected by the person.
	Contradicted []int
	// Scope is where Keep holds: `user` (everywhere), `project` or `env` (this
	// machine).
	Scope string
	// Replaces is the 1-based number of the kept line Keep corrects or
	// updates, or 0 when it replaces nothing. A number outside the list the
	// call was shown is dropped rather than passed on.
	Replaces int
	// State is present only when the exchange MOVED the work.
	State *StateDelta
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

// Bind returns a completer that starts on model and, once that model has
// answered nothing at all, uses fallback for the rest of this session. notice is called only on the transition and never while a lock is
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
		b.notice("the reflex model answers nothing; using " + b.fallback + " for this session")
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

// Notice reads one exchange and answers what it did to the work and, when
// askMemory is set, whether the person said something worth keeping. kept is
// the lines this conversation already keeps, in the order its memory block
// carries them; the model sees them numbered from 1.
//
// A Keep with no usable scope is answered as `project`, the default every other
// door keeps by, rather than refused: a model that got the line right and the
// scope word wrong has still noticed the line.
func Notice(ctx context.Context, c Completer, userMsg, assistantMsg string, kept []string, askMemory bool) (NoticeResult, error) {
	if strings.TrimSpace(userMsg) == "" && strings.TrimSpace(assistantMsg) == "" {
		return NoticeResult{}, nil
	}
	if len(kept) > keptLimit {
		kept = kept[:keptLimit]
	}
	system := statePrompt
	if askMemory {
		system = noticePrompt
	}
	var result NoticeResult
	err := ask(provider.WithRole(ctx, lane.RoleMemory), c, system, noticeInput(userMsg, assistantMsg, kept, askMemory), func(reply string) error {
		var wire struct {
			Keep         string `json:"keep"`
			Durability   string `json:"durability"`
			Used         []int  `json:"used"`
			Contradicted []int  `json:"contradicted"`
			Scope        string `json:"scope"`
			Replaces     int    `json:"replaces"`
			State        *struct {
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
		next := NoticeResult{Durability: strings.ToLower(strings.TrimSpace(wire.Durability))}
		next.Used = validUsedMemoryIndices(wire.Used, len(kept))
		next.Contradicted = validUsedMemoryIndices(wire.Contradicted, len(kept))
		if askMemory {
			next.Keep = strings.TrimSpace(wire.Keep)
			if next.Durability == "ephemeral" {
				next.Keep = ""
			}
		}
		if next.Keep != "" {
			next.Scope = strings.ToLower(strings.TrimSpace(wire.Scope))
			if !allowed(memoryScopes, next.Scope) {
				next.Scope = "project"
			}
			if wire.Replaces >= 1 && wire.Replaces <= len(kept) {
				next.Replaces = wire.Replaces
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
		return NoticeResult{}, err
	}
	return result, nil
}

// validUsedMemoryIndices bounds attribution to the exact numbered input and
// removes repeats, so a malformed answer cannot rehearse an unrelated chunk.
func validUsedMemoryIndices(indices []int, count int) []int {
	seen := map[int]bool{}
	var used []int
	for _, index := range indices {
		if index >= 1 && index <= count && !seen[index] {
			used = append(used, index)
			seen[index] = true
		}
	}
	return used
}

// ask allows one JSON repair per pass. A transport failure can separately use
// the configured fallback inside [call], but all requests and the repair share
// the same patience. The owner may retry a transiently failed pass off the turn's
// path; malformed replies alone never start another pass.
//
// The repair asks for the same thing with the unreadable answer in front of it.
// A second malformed reply is returned to the owner rather than entering a loop.
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
// room where a person is reading an answer. It is the only thing this function
// puts on the request that the caller did not.
//
// IT USED TO SEND TWO MORE THINGS, and both are gone. A REQUIRED
// [provider.EffortOff] — {"reasoning": {"enabled": false}} — said the law at the
// top of this file to the provider instead of only to the prompt; and a 200
// token ceiling, which was only honest BECAUSE of the disable, plus a ten-times
// larger retry for the endpoints that accept the disable and ignore it. Each
// rung of that was this harness deciding how somebody else's model should
// answer, on evidence from the models it happened to be pointed at. The prompt
// still asks for a few words of JSON, which is where a reflex's shape has
// always actually come from. An explicit effort already carried by the caller
// or configured model still reaches this call; the reflex adds none.
//
// WHAT SURVIVES IS THE PART THAT WAS NEVER ABOUT GENERATION: an answer this
// package cannot use must not break a turn. A model that says nothing at all
// abandons this session's reflex model and asks the configured low tier once.
// A transient request failure also tries that fallback, without declaring the
// primary unusable for the rest of the session. Both attempts share one bound.
func call(ctx context.Context, c Completer, messages []ai.Message) (string, error) {
	// Every reflex request in the process passes through here, so this is the
	// one place its rows in the model-call log get their word.
	ctx = provider.WithCallTag(ctx, "reflex")
	ctx = provider.WithoutStream(ctx)
	response, err := completeWithRecovery(ctx, c, messages)
	if err != nil {
		return "", err
	}
	if response == nil {
		return "", errors.New("the model answered nothing")
	}
	if !AnsweredNothing(response) {
		return response.Text(), nil
	}

	pinned, ok := c.(bound)
	if !ok || !pinned.abandon(boundModel(c)) {
		return response.Text(), nil
	}
	response, err = c.CompleteWithMessages(ctx, messages)
	if err != nil {
		return "", err
	}
	if response == nil {
		return "", errors.New("the fallback model answered nothing")
	}
	return response.Text(), nil
}

// completeWithRecovery reserves time for a transient failure's fallback.
// It returns a response without decoding or applying session failover policy.
func completeWithRecovery(ctx context.Context, c Completer, messages []ai.Message) (*ai.Response, error) {
	// Reserve half of the remaining patience for recovery. Only a bound client
	// knows where to send that recovery; unbound callers keep their deadline.
	attemptCtx := ctx
	cancel := func() {}
	pinned, canRecover := c.(bound)
	if canRecover && pinned.fallback != "" {
		if deadline, ok := ctx.Deadline(); ok {
			attemptCtx, cancel = context.WithTimeout(ctx, time.Until(deadline)/2)
		}
	}
	response, err := c.CompleteWithMessages(attemptCtx, messages)
	cancel()
	if err != nil {
		// A cancelled owner never starts another paid request. A transient
		// failure tries the small-work model once, with the RESERVED parent
		// deadline rather than the primary attempt's expired child context.
		kind, _ := provider.RouteFailureOf(err)
		if ctx.Err() != nil || !canRecover || pinned.fallback == "" || kind != provider.RouteTransient {
			return nil, err
		}
		fallback := pinned
		fallback.model = pinned.fallback
		fallback.session = nil
		response, err = fallback.CompleteWithMessages(ctx, messages)
		if err != nil {
			return nil, err
		}
		if response == nil || AnsweredNothing(response) {
			return nil, errors.New("the fallback model answered nothing")
		}
		return response, nil
	}
	return response, nil
}

// AnsweredNothing is the one failure this package can do anything about: a call
// that came back with no visible text at all.
//
// IT USED TO BE "EMPTY AT THE CEILING WE SENT", and that reading died with the
// ceiling. What is left is the honest half of it — there is nothing to decode,
// so there is nothing to use — and it is exported because the accounting
// wrapper that journals each paid reflex call must call a request empty on
// exactly the same terms this package does, or the bill and the recovery
// disagree about what happened (internal/session's memory.go).
func AnsweredNothing(response *ai.Response) bool {
	return response == nil || strings.TrimSpace(response.Text()) == ""
}

func boundModel(c Completer) string {
	if pinned, ok := c.(bound); ok {
		return pinned.activeModel()
	}
	return ""
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
