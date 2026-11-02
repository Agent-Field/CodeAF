package session

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// ── ERRANDS: ONE SHORT, DETACHED, METERED COMPLETION WITH THE CALLER'S PROMPT ──
//
// An errand is a model call a surface asks of an agent that is not a turn: no
// tools, no journal entry, no message in the conversation, nothing the person
// sees unless the caller shows it. It rides the agent's own metered door — its
// admission, its provider and keys, its one-model promise, its job budget, its
// receipts and its lifetime — so a caller's prompt costs what the agent's own
// errands cost and stops when the agent stops.
//
// THE ENGINE NEVER KNOWS WHAT AN ERRAND IS FOR. The purpose is a ledger label
// and the key of the one-in-flight slot, and nothing here branches on it. A
// reply draft ([Agent.SuggestReply]) is an errand with a fixed prompt of its
// own, built on this same core.

// The version of the errand ask this build answers, and its limits. They are
// the one source of the numbers the app wire advertises (internal/appwire).
const (
	ErrandVersion = 1
	// ErrandMaxInputBytes bounds the system text and every message's text
	// together, in UTF-8 bytes.
	ErrandMaxInputBytes = 48 << 10
	ErrandMaxMessages   = 32
	// ErrandMaxSchemaBytes bounds a caller's JSON schema.
	ErrandMaxSchemaBytes = 8 << 10
	ErrandMaxCacheKey    = 128
	ErrandDefaultTokens  = 400
	ErrandMaxTokens      = 4096
	ErrandMinDeadline    = time.Second
	// ErrandDefaultDeadline is the whole errand's wall, admission to answer.
	ErrandDefaultDeadline = 10 * time.Second
	ErrandMaxDeadline     = 60 * time.Second
	// errandCacheEntries bounds the per-agent answer cache, across purposes.
	errandCacheEntries = 32
)

// The reasons an errand answers with no text. They are machine words a
// caller branches on, never sentences for a person.
const (
	ErrandClosed      = "closed"
	ErrandStopped     = "stopped"
	ErrandOverBudget  = "over_budget"
	ErrandBusy        = "busy"
	ErrandDeadline    = "deadline"
	ErrandCancelled   = "cancelled"
	ErrandModelFailed = "model_failed"
	ErrandInvalidJSON = "invalid_json"
)

// ErrErrandAsk is an ask outside the errand's shape or limits; the message
// says which field. ErrErrandRoute is a role or model this agent may not use
// for an errand. Both are the caller's to fix, never the model's.
var (
	ErrErrandAsk   = errors.New("errand ask")
	ErrErrandRoute = errors.New("errand route")
)

var errandPurposeShape = regexp.MustCompile(`^[a-z][a-z0-9_]{1,31}$`)

// ErrandMessage is one message of an errand's dialogue: "user" or "assistant",
// and its text.
type ErrandMessage struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// ErrandAsk is one errand. It travels as JSON to a remote owner unchanged.
type ErrandAsk struct {
	// Purpose labels the receipt and keys the one-in-flight slot.
	Purpose string `json:"purpose"`
	// Role is any role this build has a tier for; empty is the naming role.
	Role string `json:"role,omitempty"`
	// Model pins the call to one model id, inside the agent's model policy.
	Model string `json:"model,omitempty"`
	// Effort is a thinking level the route may honour; empty sends none.
	Effort   string          `json:"effort,omitempty"`
	System   string          `json:"system,omitempty"`
	Messages []ErrandMessage `json:"messages"`
	// JSON asks for one JSON object; JSONSchema asks for one of that schema.
	JSON       bool            `json:"json,omitempty"`
	JSONSchema json.RawMessage `json:"json_schema,omitempty"`
	MaxTokens  int             `json:"max_tokens,omitempty"`
	Deadline   time.Duration   `json:"deadline,omitempty"`
	// Cache, when set, reuses the answer to an identical ask under the same key.
	Cache string `json:"cache,omitempty"`
}

// ErrandUsage is what the errand's request cost, as the provider reported it.
type ErrandUsage struct {
	In      int     `json:"in"`
	Out     int     `json:"out"`
	CostUSD float64 `json:"cost_usd,omitempty"`
}

// ErrandAnswer is an errand's outcome. Text is empty exactly when Reason is set.
type ErrandAnswer struct {
	Text     string        `json:"text"`
	Model    string        `json:"model,omitempty"`
	Provider string        `json:"provider,omitempty"`
	Role     string        `json:"role,omitempty"`
	Usage    ErrandUsage   `json:"usage"`
	Took     time.Duration `json:"took,omitempty"`
	Cached   bool          `json:"cached,omitempty"`
	Reason   string        `json:"reason,omitempty"`
}

func errandAskError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrErrandAsk, fmt.Sprintf(format, args...))
}

// Normalize checks an ask against the errand's limits and fills its defaults:
// the naming role, the default output bound and deadline, each clamped into
// its range. Sizes and shapes are refused; numbers out of range are clamped.
func (ask ErrandAsk) Normalize() (ErrandAsk, error) {
	if !errandPurposeShape.MatchString(ask.Purpose) {
		return ask, errandAskError("purpose must match %s", errandPurposeShape)
	}
	if err := ask.checkDialogue(); err != nil {
		return ask, err
	}
	if len(ask.JSONSchema) > 0 && !isJSONObject(ask.JSONSchema) {
		return ask, errandAskError("json_schema must be one JSON object")
	}
	if len(ask.JSONSchema) > ErrandMaxSchemaBytes {
		return ask, errandAskError("json_schema is over %d bytes", ErrandMaxSchemaBytes)
	}
	if len(ask.Cache) > ErrandMaxCacheKey {
		return ask, errandAskError("cache key is over %d bytes", ErrandMaxCacheKey)
	}
	rung, ok := effort.Parse(ask.Effort)
	if !ok {
		return ask, errandAskError("effort %q is not a level", ask.Effort)
	}
	ask.Effort = string(rung)
	ask.Role = strings.TrimSpace(ask.Role)
	if ask.Role == "" {
		ask.Role = string(roles.RoleTitle)
	}
	ask.Model = strings.TrimSpace(ask.Model)
	ask.MaxTokens = clampOr(ask.MaxTokens, 1, ErrandMaxTokens, ErrandDefaultTokens)
	ask.Deadline = clampOr(ask.Deadline, ErrandMinDeadline, ErrandMaxDeadline, ErrandDefaultDeadline)
	return ask, nil
}

func (ask ErrandAsk) checkDialogue() error {
	if len(ask.Messages) == 0 || len(ask.Messages) > ErrandMaxMessages {
		return errandAskError("messages must hold 1 to %d messages", ErrandMaxMessages)
	}
	size := len(ask.System)
	texts := []string{ask.System}
	for i, m := range ask.Messages {
		if m.Role != "user" && m.Role != "assistant" {
			return errandAskError("messages[%d].role must be user or assistant", i)
		}
		if strings.TrimSpace(m.Text) == "" {
			return errandAskError("messages[%d].text is empty", i)
		}
		size += len(m.Text)
		texts = append(texts, m.Text)
	}
	if ask.Messages[len(ask.Messages)-1].Role != "user" {
		return errandAskError("the last message must be the user's")
	}
	if size > ErrandMaxInputBytes {
		return errandAskError("system and messages are over %d bytes", ErrandMaxInputBytes)
	}
	for _, text := range texts {
		if !utf8.ValidString(text) {
			return errandAskError("text must be UTF-8")
		}
	}
	return nil
}

// clampOr is value inside [low, high], or fallback when value is unset.
func clampOr[T int | time.Duration](value, low, high, fallback T) T {
	if value <= 0 {
		return fallback
	}
	return min(max(value, low), high)
}

// errandState is the agent's errand bookkeeping: which purposes are in
// flight, and a bounded cache of answers to asks that carried a cache key.
type errandState struct {
	mu     sync.Mutex
	active map[string]bool
	cache  map[[32]byte]ErrandAnswer
	order  [][32]byte
}

// claim takes the purpose's slot, or reports it busy. It never waits.
func (s *errandState) claim(purpose string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active[purpose] {
		return false
	}
	if s.active == nil {
		s.active = map[string]bool{}
	}
	s.active[purpose] = true
	return true
}

func (s *errandState) release(purpose string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.active, purpose)
}

func (s *errandState) cached(key [32]byte) (ErrandAnswer, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	answer, ok := s.cache[key]
	return answer, ok
}

func (s *errandState) remember(key [32]byte, answer ErrandAnswer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil {
		s.cache = map[[32]byte]ErrandAnswer{}
	}
	if _, ok := s.cache[key]; !ok {
		s.order = append(s.order, key)
	}
	s.cache[key] = answer
	if len(s.order) > errandCacheEntries {
		delete(s.cache, s.order[0])
		s.order = s.order[1:]
	}
}

// cacheKey is the whole ask but its deadline: a cached answer is reused only
// for the same prompt, route and shape under the same key.
func (ask ErrandAsk) cacheKey() [32]byte {
	ask.Deadline = 0
	data, _ := json.Marshal(ask)
	return sha256.Sum256(data)
}

// RunErrand runs one errand on this agent's metered door. A bad ask or a
// route outside the agent's model policy is an error; every other way of not
// answering — closed, stopped, over budget, busy, deadline, a failed or
// unusable answer — is an empty text with a Reason.
func (a *Agent) RunErrand(ctx context.Context, ask ErrandAsk) (ErrandAnswer, error) {
	ask, err := ask.Normalize()
	if err != nil {
		return ErrandAnswer{}, err
	}
	if err := a.checkErrandRoute(ask); err != nil {
		return ErrandAnswer{}, err
	}
	return a.runErrand(ctx, ask, nil)
}

// checkErrandRoute is the agent's model policy applied to an errand: a role
// must be one with a tier, and ONE-MODEL MODE WINS over any explicit model.
func (a *Agent) checkErrandRoute(ask ErrandAsk) error {
	if _, ok := roles.TierOf(roles.Role(ask.Role)); !ok {
		return fmt.Errorf("%w: role %q is not one this engine routes", ErrErrandRoute, ask.Role)
	}
	if ask.Model == "" {
		return nil
	}
	a.mu.Lock()
	one, model := a.config.OneModel, a.model
	a.mu.Unlock()
	if one && ask.Model != model {
		return fmt.Errorf("%w: this agent runs in one-model mode on %s, so %s is not allowed", ErrErrandRoute, model, ask.Model)
	}
	return nil
}

// runErrand is the core every errand shares, the reply draft included. check,
// when given, turns a raw answer into the text the caller keeps.
func (a *Agent) runErrand(ctx context.Context, ask ErrandAsk, check func(string) (string, string)) (ErrandAnswer, error) {
	if why := a.errandRefusal(); why != "" {
		return ErrandAnswer{Reason: why}, nil
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return ErrandAnswer{Reason: ErrandClosed}, nil
	}
	// The errand shares the naming errands' lifetime: Close cancels it and
	// settles its receipt before the journal closes. Reserved under the lock.
	a.titleJobs.Add(1)
	life := a.titleCtx
	a.mu.Unlock()
	defer a.titleJobs.Done()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if life != nil {
		stop := context.AfterFunc(life, cancel)
		defer stop()
	}
	key := ask.cacheKey()
	if ask.Cache != "" {
		if answer, ok := a.errands.cached(key); ok {
			answer.Cached, answer.Took = true, 0
			return answer, nil
		}
	}
	if !a.errands.claim(ask.Purpose) {
		return ErrandAnswer{Reason: ErrandBusy}, nil
	}
	defer a.errands.release(ask.Purpose)
	answer := a.callErrand(ctx, ask, check)
	// AN ANSWER THE MODEL GAVE, USABLE OR NOT, IS KEPT: it cost money, and a
	// cached ask must not pay for it again when a surface remounts. A failure
	// or refusal is not an answer and is asked again next time.
	if ask.Cache != "" && (answer.Reason == "" || answer.Reason == ErrandInvalidJSON) {
		a.errands.remember(key, answer)
	}
	return answer, nil
}

// errandRefusal is why no errand may start now, or "". The job budget is
// asked outside the agent's lock, as its watcher asks it.
func (a *Agent) errandRefusal() string {
	a.mu.Lock()
	closed, stopped, railed := a.closed, a.workStopped, a.railBlockLocked() != nil
	a.mu.Unlock()
	switch {
	case closed:
		return ErrandClosed
	// Before stopped: a job out of money stops its work, and the money is
	// the reason a caller can act on.
	case railed || a.jobBudgetBlock() != nil:
		return ErrandOverBudget
	case stopped:
		return ErrandStopped
	}
	return ""
}

func (a *Agent) callErrand(ctx context.Context, ask ErrandAsk, check func(string) (string, string)) ErrandAnswer {
	started := time.Now()
	ctx, cancel := context.WithTimeout(ctx, ask.Deadline)
	defer cancel()
	go a.watchErrand(ctx, cancel)
	pick := errandPick{model: ask.Model, effort: ask.Effort}
	callCtx := withErrandPick(withFirstRungOnly(withDetachedUsage(ctx)), pick)
	// The conversation's own model is the ladder's floor, as it is for a
	// title: a role nobody configured still answers on the model already paid
	// for, rather than not at all.
	response, model, err := a.callRole(callCtx, roles.Role(ask.Role), a.Model(), ask.messages(), ask.options()...)
	// Rejected output still cost money. Book it on the agent's auxiliary
	// ledger and job meter under the caller's purpose, never on a later turn.
	a.addDetachedUsageAs(response, model, 1, ask.Purpose)
	answer := ErrandAnswer{Model: model, Role: ask.Role, Took: time.Since(started)}
	if response != nil {
		answer.Usage = errandUsage(response)
		if response.Model != "" {
			answer.Model = response.Model
		}
	}
	answer.Provider = a.errandProvider(model)
	if answer.Reason = a.errandFailure(ctx, err, response); answer.Reason != "" {
		return answer
	}
	answer.Text, answer.Reason = ask.shape(response.Text(), check)
	return answer
}

// watchErrand ends the errand the moment its agent closes, stops, or its job
// runs out of money, rather than at its deadline.
func (a *Agent) watchErrand(ctx context.Context, cancel context.CancelFunc) {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			a.mu.Lock()
			stopped := a.closed || a.workStopped
			a.mu.Unlock()
			if stopped || a.jobBudgetBlock() != nil {
				cancel()
				return
			}
		}
	}
}

// errandFailure names why a call gave nothing to keep, or "".
func (a *Agent) errandFailure(ctx context.Context, err error, response *ai.Response) string {
	if err == nil && response != nil && ctx.Err() == nil {
		return ""
	}
	switch why := a.errandRefusal(); {
	case why != "":
		return why
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return ErrandDeadline
	case ctx.Err() != nil:
		return ErrandCancelled
	}
	return ErrandModelFailed
}

// shape is the raw answer as the caller asked for it.
func (ask ErrandAsk) shape(raw string, check func(string) (string, string)) (string, string) {
	if check != nil {
		return check(raw)
	}
	if !ask.JSON && len(ask.JSONSchema) == 0 {
		return strings.TrimSpace(raw), ""
	}
	if text := oneJSONObject(raw); text != "" {
		return text, ""
	}
	return "", ErrandInvalidJSON
}

func (ask ErrandAsk) messages() []ai.Message {
	messages := make([]ai.Message, 0, len(ask.Messages)+1)
	if ask.System != "" {
		messages = append(messages, textMessage("system", ask.System))
	}
	for _, m := range ask.Messages {
		messages = append(messages, textMessage(m.Role, m.Text))
	}
	return messages
}

// options are the request's output bound and shape. A schema is sent unstrict
// because a caller's schema need not meet a strict mode's rules, and the
// provider drops the format for a route that refuses it; the answer is still
// checked to be one object either way.
func (ask ErrandAsk) options() []ai.Option {
	options := []ai.Option{ai.WithMaxTokens(ask.MaxTokens)}
	switch {
	case len(ask.JSONSchema) > 0:
		schema := ask.JSONSchema
		options = append(options, func(r *ai.Request) error {
			r.ResponseFormat = &ai.ResponseFormat{Type: "json_schema", JSONSchema: &ai.JSONSchema{Name: "answer", Schema: schema}}
			return nil
		})
	case ask.JSON:
		options = append(options, ai.WithJSONMode())
	}
	return options
}

func errandUsage(response *ai.Response) ErrandUsage {
	if response.Usage == nil {
		return ErrandUsage{}
	}
	usage := ErrandUsage{In: response.Usage.PromptTokens, Out: response.Usage.CompletionTokens}
	if response.Usage.Cost != nil {
		usage.CostUSD = *response.Usage.Cost
	}
	return usage
}

// errandProvider is the service a model is reached through, or "" when this
// agent's client is not a managed pool (a test's, a bare endpoint's).
func (a *Agent) errandProvider(model string) string {
	if model == "" {
		return ""
	}
	_, _, _, service, err := a.completerWithService(model)
	if err != nil {
		return ""
	}
	return service.Source.ID
}

// oneJSONObject is raw as one JSON object, unwrapped from a single code fence
// when a model added one, or "" when it is anything else.
func oneJSONObject(raw string) string {
	text := strings.TrimSpace(raw)
	if body, ok := strings.CutPrefix(text, "```"); ok {
		if fenced, ok := strings.CutSuffix(body, "```"); ok {
			text = strings.TrimSpace(strings.TrimPrefix(fenced, "json"))
		}
	}
	if !isJSONObject([]byte(text)) {
		return ""
	}
	return text
}

// isJSONObject reports data as exactly one JSON object and nothing after it.
func isJSONObject(data []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var object map[string]json.RawMessage
	if decoder.Decode(&object) != nil || object == nil {
		return false
	}
	_, err := decoder.Token()
	return errors.Is(err, io.EOF)
}

// errandPick is a caller's route for one errand: a model that replaces the
// role's ladder, and an effort for the rung that answers.
type errandPick struct{ model, effort string }

type errandPickKey struct{}

func withErrandPick(ctx context.Context, pick errandPick) context.Context {
	if pick == (errandPick{}) {
		return ctx
	}
	return context.WithValue(ctx, errandPickKey{}, pick)
}

// routedErrandLadder is the role's ladder as the caller routed it. An explicit model
// is the whole ladder: an errand that named its model is never answered by
// another rung's.
func routedErrandLadder(ctx context.Context, rungs []roles.Call, err error) ([]roles.Call, error) {
	pick, ok := ctx.Value(errandPickKey{}).(errandPick)
	switch {
	case !ok:
		return rungs, err
	case pick.model != "":
		return []roles.Call{{Model: pick.model, Effort: pick.effort}}, nil
	case err == nil && pick.effort != "":
		rungs = append([]roles.Call(nil), rungs...)
		rungs[0].Effort = pick.effort
	}
	return rungs, err
}

// ErrandRoute is where an errand of one role would go now: the first rung of
// that role's ladder on this agent, and the level that rung carries.
type ErrandRoute struct {
	Role   string `json:"role"`
	Model  string `json:"model"`
	Effort string `json:"effort,omitempty"`
}

// ErrandRoutes lists every role an errand may name and the model it resolves
// to on this agent right now, from the same live settings and one-model
// promise a call would read, and under one-model mode that one model. A role
// with no model anywhere is left out.
func (a *Agent) ErrandRoutes() (routes []ErrandRoute, oneModel string) {
	reader := a.roleModels()
	if reader.one {
		oneModel = reader.model
	}
	return reader.routes(reader.model), oneModel
}

// ErrandRoutesFrom is [Agent.ErrandRoutes] for a settings source and floor
// model with no agent behind them.
func ErrandRoutesFrom(source roles.Source, floor string) []ErrandRoute {
	return roleReader{source: source}.routes(floor)
}

// ConfigRoutes is [Agent.ErrandRoutes] for a config no agent has opened yet:
// what each role would resolve to on a session built from cfg, the one-model
// promise included. A door can check what it is about to hand over.
func ConfigRoutes(cfg Config) []ErrandRoute {
	return readerFor(cfg, cfg.Model).routes(cfg.Model)
}

// routes lists every registered role this reader can name a model for.
func (r roleReader) routes(floor string) []ErrandRoute {
	var routes []ErrandRoute
	for _, role := range roles.Registered() {
		call, err := r.ResolveCall(role, floor)
		if err != nil {
			continue
		}
		routes = append(routes, ErrandRoute{Role: string(role), Model: call.Model, Effort: call.Effort})
	}
	return routes
}
