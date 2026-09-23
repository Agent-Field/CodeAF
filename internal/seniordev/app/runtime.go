//go:build !windows

// This file adapts the model backend, tool registry, durable session store and
// bus into the single `turn` the solo run drives.
package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/seniordev/baked"
	"github.com/Agent-Field/codeaf/internal/seniordev/bus"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/id"
	"github.com/Agent-Field/codeaf/internal/seniordev/modelsdev"
	"github.com/Agent-Field/codeaf/internal/seniordev/netpolicy"
	"github.com/Agent-Field/codeaf/internal/seniordev/question"
	"github.com/Agent-Field/codeaf/internal/seniordev/router/adaptive"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/compaction"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/overflow"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/sessioncore"
	"github.com/Agent-Field/codeaf/internal/seniordev/storage"
	"github.com/Agent-Field/codeaf/internal/seniordev/tool"
)

type turn struct {
	SessionID       string
	ParentSessionID string
	MessageID       string
	SessionTitle    string
	Agent           string
	AgentMarkdown   string
	// AgentPromptVerbatim marks AgentMarkdown as a configured prompt string
	// rather than a baked agent document. A configured `agent.prompt` reaches
	// the model verbatim; only baked documents carry YAML frontmatter worth
	// stripping.
	AgentPromptVerbatim bool
	Workspace           string
	ProviderID          string
	ModelID             string
	Variant             string
	MaxSteps            *float64
	RawModelCall        bool
	Prompt              string
	SystemInstructions  []string
	LoadInstructions    func(context.Context) []string
	Tools               []steploop.ToolDefinition
	Execute             func(context.Context, steploop.ToolCall) (steploop.ToolResult, error)
	BetweenStepReminder func() string
	AfterAssistant      func(context.Context, string)
	CompactionDecisions compaction.DecisionSink
	ModelRequests       modelRequestSink
	Store               steploop.Store
	PromptPersisted     bool
	PromptMessageID     string
	ManageScratch       bool
}

// turnPart is one thing the model produced in a turn: a stretch of text, a
// compaction summary, or a tool call with the arguments it was given and how
// it ended.
type turnPart struct {
	Type    string
	Text    string
	Tool    string
	ArgsKey string
	Status  string
	CostUSD *float64
}

type turnResult struct {
	// FinishReason is the final assistant's unified engine finish, not text
	// inference or the finish of an earlier completed step within the turn.
	FinishReason string
	SessionID    string
	Text         string
	Parts        []turnPart
	CostUSD      float64
}

type backend interface {
	Run(context.Context, turn) (turnResult, error)
}

type runtimeAdapter struct {
	backend   backend
	registry  *tool.Registry
	config    *seniorDevConfig
	workspace string
	durable   *durableSessions
	initErr   error
	now       func() time.Time
	ids       atomic.Uint64
	mu        sync.Mutex
	costUSD   float64
	bus       *bus.Bus
	question  *question.Service
	events    *eventWriter

	unsubscribeQuestionAutoReject func()
	unsubscribeEvents             func()
}

// configureTurn is the single provenance seam for every model turn. The
// event records what the engine will actually execute after baked metadata and
// project overrides have been resolved; it deliberately hashes prompts rather
// than copying potentially sensitive project instructions into telemetry.
func (runtime *runtimeAdapter) configureTurn(value turn) (turn, error) {
	configured, err := runtime.config.configureTurn(value)
	if err != nil {
		return configured, err
	}
	runtime.emitTurnProvenance(configured)
	return configured, nil
}

func (runtime *runtimeAdapter) emitTurnProvenance(configured turn) {
	if runtime.events != nil {
		digest := sha256.Sum256([]byte(configured.AgentMarkdown))
		data := map[string]any{
			"agent": configured.Agent, "session_id": configured.SessionID,
			"provider_id": configured.ProviderID, "model_id": configured.ModelID,
			"prompt_sha256":   fmt.Sprintf("%x", digest[:]),
			"prompt_verbatim": configured.AgentPromptVerbatim,
		}
		if configured.MaxSteps != nil {
			data["max_steps"] = *configured.MaxSteps
		}
		if configured.Variant != "" {
			data["reasoning_effort"] = configured.Variant
		}
		data["compaction"] = runtime.compactionProvenance(configured)
		runtime.events.stage("agent-runtime", "configured", data)
	}
}

// compactionProvenance records the compaction budget the turn will run under:
// the policy in force and the configured block, and when the model's limits
// are known, the capacity, the high and low watermarks and the verbatim-tail
// budget, so the budget a run used can be read back from the event stream.
func (runtime *runtimeAdapter) compactionProvenance(configured turn) map[string]any {
	cfg, err := runtime.config.overflowConfig()
	if concrete, ok := runtime.backend.(*modelAPIBackend); ok && concrete != nil && err == nil {
		cfg = concrete.withPinnedCapacity(cfg, configured.SessionID)
	}
	// Config accepts only the window policy or an empty value, so the policy
	// in force is always the window.
	record := map[string]any{"policy": overflow.PolicyWindow}
	if err != nil {
		record["error"] = err.Error()
		return record
	}
	if cfg.Compaction != nil {
		if cfg.Compaction.CapacityTokens != nil {
			record["configured_capacity_tokens"] = *cfg.Compaction.CapacityTokens
		}
		if cfg.Compaction.PreserveRecentTokens != nil {
			record["configured_preserve_recent_tokens"] = *cfg.Compaction.PreserveRecentTokens
		}
		if cfg.Compaction.PreserveRecentFraction != nil {
			record["configured_preserve_recent_fraction"] = *cfg.Compaction.PreserveRecentFraction
		}
	}
	concrete, ok := runtime.backend.(*modelAPIBackend)
	if !ok || concrete == nil {
		return record
	}
	_, model, err := (seniorDevModels{backend: concrete, agent: configured.Agent}).projection(
		configured.ProviderID, configured.ModelID,
	)
	if err != nil {
		record["error"] = err.Error()
		return record
	}
	marks := overflow.Watermarks(overflow.UsableInput{Cfg: cfg, Model: model})
	if pinned, ok := concrete.pinnedCapacityFor(configured.SessionID); ok {
		record["pinned_capacity_tokens"] = pinned
	}
	record["model_context_tokens"] = model.Limit.Context
	record["capacity_tokens"] = marks.Capacity
	record["high_tokens"] = marks.High
	record["low_tokens"] = marks.Low
	record["tail_budget_tokens"] = compaction.TailBudget(cfg, marks)
	return record
}

func newConfiguredRuntime(workspace string, client backend, cfg *seniorDevConfig) *runtimeAdapter {
	runtime := &runtimeAdapter{
		backend: client, config: cfg, workspace: workspace, now: time.Now,
	}
	runtime.durable, runtime.initErr = openDurableSessions(context.Background(), workspace)
	if runtime.durable != nil && runtime.durable.bus != nil {
		runtime.bus = runtime.durable.bus
	} else {
		// Keep the runtime usable enough to report its initialization failure,
		// while preserving the one-bus invariant for services constructed below.
		runtime.bus = bus.New(bus.Context{Directory: workspace, Workspace: workspace})
	}
	options := cfg.registryOptions()
	// The registry identifies its client as "cli" unless SENIOR_DEV_CLIENT names
	// something else.
	if clientIdentity, ok := os.LookupEnv("SENIOR_DEV_CLIENT"); ok {
		options.ClientIdentity = clientIdentity
	} else {
		options.ClientIdentity = "cli"
	}
	runtime.question = question.NewService(runtime.bus, nil)
	options.Question = runtime.question
	runtime.registry = tool.NewWithOptions(workspace, options)
	// Headless senior-dev has nothing attached that could answer question.asked,
	// so an unanswered question would hang the run for the rest of its wall
	// clock. Auto-reject through the service's own reject path so the model
	// receives the rejection ("The user dismissed this question") and the run
	// keeps moving. The registry converts the third consecutive rejection into
	// its documented terminal success result.
	runtime.unsubscribeQuestionAutoReject = runtime.bus.SubscribeCallback(
		question.Event.Asked, func(payload bus.Payload) {
			if request, ok := payload.Properties.(question.Request); ok {
				runtime.question.Reject(request.ID)
			}
		})
	return runtime
}

func (runtime *runtimeAdapter) nextID(prefix string) string {
	switch prefix {
	case "session":
		value, err := id.Descending("session")
		if err == nil {
			return value
		}
	case "message":
		return steploop.NewAscendingID("msg")
	case "part":
		return steploop.NewAscendingID("prt")
	}
	return fmt.Sprintf("%s_%016x", prefix, runtime.ids.Add(1))
}

func (runtime *runtimeAdapter) addCost(cost float64) {
	runtime.mu.Lock()
	runtime.costUSD += cost
	runtime.mu.Unlock()
}

func (runtime *runtimeAdapter) cost() float64 {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.costUSD
}

func (runtime *runtimeAdapter) runTurn(ctx context.Context, request turn) (turnResult, error) {
	if runtime.initErr != nil {
		return turnResult{}, runtime.initErr
	}
	if runtime.backend == nil {
		return turnResult{}, errors.New("senior-dev runtime: backend is required")
	}
	if request.Variant == "" {
		if concrete, ok := runtime.backend.(*modelAPIBackend); ok {
			request.Variant = concrete.variant
		}
	}
	request.ProviderID, request.ModelID = normalizeModelRef(request.ProviderID, request.ModelID)
	if request.SessionID == "" {
		info, err := runtime.createSession(ctx, sessioncore.CreateInput{
			ParentID: request.ParentSessionID, Title: request.SessionTitle,
			Agent: request.Agent, Directory: request.Workspace,
			Model: sessionModel(request.ProviderID, request.ModelID, request.Variant),
		})
		if err != nil {
			return turnResult{}, err
		}
		request.SessionID = info.ID
	} else if err := runtime.ensureSession(ctx, request); err != nil {
		return turnResult{SessionID: request.SessionID}, err
	}
	request.Store = runtime.durable
	if request.CompactionDecisions == nil {
		request.CompactionDecisions = newSeniorDevCompactionDecisionSink(runtime.bus)
	}
	if request.ModelRequests == nil {
		request.ModelRequests = newModelRequestSink(runtime.bus)
	}
	messageID, err := persistTurnPrompt(
		ctx, runtime.durable, request.SessionID, request.MessageID, request,
	)
	if err != nil {
		return turnResult{SessionID: request.SessionID}, err
	}
	if err := runtime.durable.TouchSession(ctx, request.SessionID); err != nil {
		return turnResult{SessionID: request.SessionID}, err
	}
	request.PromptPersisted = true
	request.PromptMessageID = messageID
	if request.ManageScratch {
		releaseScratch := tool.AcquireShellScratch(request.SessionID)
		defer releaseScratch()
	}
	return runtime.backend.Run(ctx, request)
}

func persistTurnPrompt(
	ctx context.Context,
	store steploop.Store,
	sessionID string,
	messageID string,
	request turn,
) (string, error) {
	if messageID == "" {
		messageID = steploop.NewAscendingID("msg")
	}
	user := msgmodel.User{
		MessageBase: msgmodel.MessageBase{ID: messageID, SessionID: sessionID},
		Time:        msgmodel.TimeCreated{Created: uint64(time.Now().UnixMilli())},
		Agent:       request.Agent,
		Model: msgmodel.UserModel{
			ProviderID: request.ProviderID, ModelID: request.ModelID,
		},
	}
	if request.Variant != "" {
		user.Model.Variant = &request.Variant
	}
	parts := []msgmodel.Part{msgmodel.TextPart{
		PartBase: msgmodel.PartBase{
			ID: steploop.NewAscendingID("prt"), SessionID: sessionID, MessageID: messageID,
		},
		Text: request.Prompt,
	}}
	if paired, ok := store.(interface {
		UpdateMessageWithParts(context.Context, msgmodel.Info, ...msgmodel.Part) error
	}); ok {
		if err := paired.UpdateMessageWithParts(ctx, user, parts...); err != nil {
			return "", err
		}
		return messageID, nil
	}
	for _, part := range parts {
		if err := store.UpdatePart(ctx, part); err != nil {
			return "", err
		}
	}
	if err := store.UpdateMessage(ctx, user); err != nil {
		return "", err
	}
	return messageID, nil
}

func (runtime *runtimeAdapter) ensureSession(ctx context.Context, request turn) error {
	if runtime.durable == nil {
		return errors.New("senior-dev runtime: durable sessions are unavailable")
	}
	if _, err := runtime.durable.sessions.Get(ctx, request.SessionID); err == nil {
		return nil
	} else {
		var missing *storage.NotFoundError
		if !errors.As(err, &missing) {
			return err
		}
	}
	_, err := runtime.createSession(ctx, sessioncore.CreateInput{
		ID: request.SessionID, ParentID: request.ParentSessionID,
		Title: request.SessionTitle, Agent: request.Agent, Directory: request.Workspace,
		Model: sessionModel(request.ProviderID, request.ModelID, request.Variant),
	})
	return err
}

func (runtime *runtimeAdapter) createSession(
	ctx context.Context, input sessioncore.CreateInput,
) (sessioncore.Info, error) {
	if runtime.initErr != nil {
		return sessioncore.Info{}, runtime.initErr
	}
	if runtime.durable == nil {
		return sessioncore.Info{}, errors.New("senior-dev runtime: durable sessions are unavailable")
	}
	info, err := runtime.durable.CreateSession(ctx, input)
	if err != nil {
		return sessioncore.Info{}, err
	}
	return info, nil
}

func (runtime *runtimeAdapter) ensureRootSession(
	ctx context.Context, sessionID, title, agent string,
) error {
	return runtime.ensureSession(ctx, turn{
		SessionID: sessionID, SessionTitle: title, Agent: agent,
		Workspace: runtime.workspace,
	})
}

func sessionModel(providerID, modelID, variant string) *sessioncore.Model {
	if providerID == "" && modelID == "" && variant == "" {
		return nil
	}
	model := &sessioncore.Model{ID: modelID, ProviderID: providerID}
	if variant != "" {
		model.Variant = &variant
	}
	return model
}

func (runtime *runtimeAdapter) Close() {
	if runtime == nil {
		return
	}
	if runtime.unsubscribeQuestionAutoReject != nil {
		runtime.unsubscribeQuestionAutoReject()
	}
	if runtime.unsubscribeEvents != nil {
		runtime.unsubscribeEvents()
	}
	if runtime.question != nil {
		runtime.question.Close()
	}
	if runtime.durable != nil {
		runtime.durable.Close()
	} else if runtime.bus != nil {
		runtime.bus.Dispose()
	}
}

// policyDisabledTools names the builtin tools the network policy withholds
// from the model entirely: with egress off, the web tools disappear from the
// tool list so the model never sees, plans around, or probes them.
func policyDisabledTools(policy netpolicy.Policy) []string {
	if policy.Restricted() {
		return []string{"webfetch", "websearch"}
	}
	return nil
}

func filterTools(
	definitions []steploop.ToolDefinition, disabled map[string]bool,
) []steploop.ToolDefinition {
	out := make([]steploop.ToolDefinition, 0, len(definitions))
	for _, definition := range definitions {
		if !disabled[definition.Provider.Name] {
			out = append(out, definition)
		}
	}
	return out
}

func (runtime *runtimeAdapter) definitionsFor(
	providerID, modelID, agentName string, disabled map[string]bool,
) []steploop.ToolDefinition {
	if disabled == nil {
		disabled = map[string]bool{}
	}
	// Visibility gating lives here rather than in tool.FilterDefinitions.
	// With the tools absent from the definitions
	// the model never plans around them, so it cannot burn turns retrying
	// policy errors; the execute-time checks remain as defense in depth.
	for _, name := range policyDisabledTools(netpolicy.Current()) {
		disabled[name] = true
	}
	for name := range runtime.config.disabledTools(agentName, runtime.registry.IDs()) {
		disabled[name] = true
	}
	definitions := tool.FilterDefinitions(runtime.registry.Definitions(), tool.FilterInput{
		ProviderID: providerID,
		ModelID:    modelID,
		Flags:      tool.CurrentWebSearchFlags(),
	})
	return filterTools(definitions, disabled)
}

// poolResolver holds the model pools the run was started with and answers
// which one a tier routes on. A tier given no pool of its own routes on the
// high pool, the same degradation the router applies.
type poolResolver struct {
	high     []string
	low      []string
	frontier []string
}

func (resolver poolResolver) values(tier baked.Tier) []string {
	pool := resolver.high
	switch tier {
	case baked.TierLow:
		if len(resolver.low) > 0 {
			pool = resolver.low
		}
	case baked.TierFrontier:
		if len(resolver.frontier) > 0 {
			pool = resolver.frontier
		}
	}
	return append([]string{}, pool...)
}

// modelAPIBackend runs the model turns of one run against the model API codeaf
// serves it: an endpoint that answers in OpenRouter's chat-completions shape,
// opened by a token that opens nothing else.
//
// IT HOLDS NO KEY. senior-dev read a provider key and a base URL out of its
// environment before codeaf carried it; both reads are gone, and so is every
// check that a key was set. The API's address and token arrive through the
// delegate.Host, and fetch is the one door every model request leaves by.
type modelAPIBackend struct {
	api            delegate.ModelAPI
	variant        string
	client         *http.Client
	contextLimit   float64
	outputLimit    float64
	totalTimeoutMS float64
	chunkTimeoutMS float64
	config         *seniorDevConfig
	router         *adaptive.AdaptiveModelRouter
	catalog        modelsdev.Catalog
	// events receives the records the backend emits on its own, after
	// configureTurn: the compaction-capacity pins (compaction_pin.go).
	events *eventWriter
	// pinnedCapacity is the per-session capacity a context-overflow rejection
	// named (compaction_pin.go). A run is one process, so the map is the
	// whole of the state.
	pinMu          sync.Mutex
	pinnedCapacity map[string]float64
}

func executeAdvertisedTool(
	ctx context.Context, request turn, call steploop.ToolCall,
) (steploop.ToolResult, error) {
	available := make([]string, 0, len(request.Tools))
	for _, definition := range request.Tools {
		name := definition.Provider.Name
		available = append(available, name)
		if name == call.Name {
			return request.Execute(ctx, call)
		}
	}
	message := "Model tried to call unavailable tool '" + call.Name + "'. "
	if len(available) == 0 {
		message += "No tools are available."
	} else {
		message += "Available tools: " + strings.Join(available, ", ") + "."
	}
	return steploop.ToolResult{}, errors.New(message)
}

// newModelAPIBackend is the backend of a run whose model API is api.
func newModelAPIBackend(api delegate.ModelAPI, variant string) *modelAPIBackend {
	return &modelAPIBackend{
		api: api, variant: variant,
		// Streaming lifetime belongs to the caller context and the reader's
		// inactivity watchdog. http.Client.Timeout measures total request age,
		// including a healthy response body, so it must remain unset.
		client: &http.Client{},
	}
}

// fetch sends one model request: the model API's token goes on here and
// nowhere else, over the backend's one HTTP client.
//
// THIS IS THE ONE DOOR. The streaming client builds each request and hands it
// here (orclient.Client.Fetcher), so no request can leave without the token,
// and none can carry a credential of anybody else's: whatever a configured
// header said, the Authorization header is the API's, set last.
func (backend *modelAPIBackend) fetch(request *http.Request) (*http.Response, error) {
	backend.api.Authorize(request)
	client := backend.client
	if client == nil {
		client = http.DefaultClient
	}
	return client.Do(request)
}
