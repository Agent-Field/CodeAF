//go:build !windows

package app

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/seniordev/attribution"
	"github.com/Agent-Field/codeaf/internal/seniordev/baked"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/calc"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/orclient"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/router/adaptive"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/compaction"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/llmcall"
	"github.com/Agent-Field/codeaf/internal/seniordev/tool"
)

type seniorDevModels struct {
	backend   *openRouterBackend
	sessionID string
	agent     string
	variant   string
}

func (models seniorDevModels) GetModel(
	_ context.Context, providerID, modelID string,
) (llmcall.Model, error) {
	providerID, modelID = normalizeModelRef(providerID, modelID)
	projection, _, err := models.projection(providerID, modelID)
	if err != nil {
		return llmcall.Model{}, err
	}
	maximum := orclient.MaxOutputTokens(projection)
	options := orclient.Options(orclient.OptionsInput{
		Model: projection, SessionID: models.sessionID,
	})
	options = orclient.MergeOptions(options, models.backend.config.options(models.agent, providerID, modelID))
	routing, err := models.backend.config.providerRouting(models.agent, providerID, modelID)
	if err != nil {
		return llmcall.Model{}, err
	}
	if object := routing.Object(); object != nil {
		// The typed block is authoritative over any ad-hoc `options.provider`.
		options.SetObject("provider", object)
	}
	effort := models.variant
	if effort == "" {
		effort = models.backend.variant
	}
	if effort != "" {
		reasoning := orclient.NewObject()
		reasoning.SetString("effort", effort)
		options.SetObject("reasoning", reasoning)
	}
	params := orclient.RequestParams{
		MaxOutputTokens:   &maximum,
		OpenRouterOptions: options,
		Compatibility:     orclient.CompatibilityCompatible,
	}
	return llmcall.Model{
		ProviderID: providerID,
		ID:         modelID,
		APIID:      modelID,
		Params:     params,
	}, nil
}

func (models seniorDevModels) Resolve(
	ctx context.Context, user msgmodel.User,
) (steploop.Model, error) {
	resolved, err := models.GetModel(ctx, user.Model.ProviderID, user.Model.ModelID)
	if err != nil {
		return steploop.Model{}, err
	}
	projection, metadata, err := models.projection(resolved.ProviderID, resolved.ID)
	if err != nil {
		return steploop.Model{}, err
	}
	return steploop.Model{
		Message: msgmodel.Model{
			ProviderID: resolved.ProviderID,
			ID:         resolved.ID,
			API: msgmodel.ModelAPI{
				Npm: projection.API.Npm, ID: projection.API.ID,
			},
		},
		Calc:    metadata,
		Request: resolved.Params,
	}, nil
}

func (models seniorDevModels) projection(
	providerID, modelID string,
) (orclient.Model, calc.Model, error) {
	metadata, err := models.catalogModel(providerID, modelID)
	if err != nil {
		return orclient.Model{}, calc.Model{}, err
	}
	configured := models.backend.config.model(providerID, modelID)
	limits := objectValue(configured["limit"])
	if value, ok := configNumber(limits["context"]); ok {
		metadata.Limit.Context = value
	}
	if value, ok := configNumber(limits["output"]); ok {
		metadata.Limit.Output = value
	}
	if value, ok := configNumber(limits["input"]); ok {
		metadata.Limit.Input = &value
	}
	if models.backend.contextLimit != 0 {
		metadata.Limit.Context = models.backend.contextLimit
	}
	if models.backend.outputLimit != 0 {
		metadata.Limit.Output = models.backend.outputLimit
	}
	if metadata.Cost == nil {
		metadata.Cost = &calc.ModelCost{Cache: &calc.CacheCost{}}
	}
	if metadata.Cost.Cache == nil {
		metadata.Cost.Cache = &calc.CacheCost{}
	}
	cost := objectValue(configured["cost"])
	if value, ok := configNumber(cost["input"]); ok {
		metadata.Cost.Input = value
	}
	if value, ok := configNumber(cost["output"]); ok {
		metadata.Cost.Output = value
	}
	if value, ok := configNumber(cost["cache_read"]); ok {
		metadata.Cost.Cache.Read = value
	}
	if value, ok := configNumber(cost["cache_write"]); ok {
		metadata.Cost.Cache.Write = value
	}
	projection := orclient.Model{
		ProviderID: providerID,
		ID:         modelID,
		API: orclient.ModelAPI{
			Npm: "@openrouter/ai-sdk-provider", ID: modelID,
		},
		Capabilities: orclient.ModelCapabilities{
			Temperature: metadata.Capabilities.Temperature,
			Reasoning:   metadata.Capabilities.Reasoning,
			Attachment:  metadata.Capabilities.Attachment,
			ToolCall:    metadata.Capabilities.ToolCall,
			Input:       metadata.Capabilities.Input,
			Output:      metadata.Capabilities.Output,
		},
		Limit: orclient.ModelLimit{
			Context: metadata.Limit.Context,
			Input:   metadata.Limit.Input,
			Output:  metadata.Limit.Output,
		},
	}
	return projection, metadata, nil
}

func (models seniorDevModels) catalogModel(providerID, modelID string) (calc.Model, error) {
	if models.backend.catalog != nil {
		metadata, err := models.backend.catalog.Resolve(providerID, modelID)
		if err == nil {
			return metadata, nil
		}
		if len(models.backend.config.model(providerID, modelID)) == 0 {
			return calc.Model{}, err
		}
		// A config-defined model absent from models.dev gets zero cost and zero
		// context/output limits unless the config block supplies them.
		return calc.Model{
			Cost: &calc.ModelCost{Cache: &calc.CacheCost{}},
			// Unknown capabilities are permissive.
			Capabilities: calc.ModelCapabilities{ToolCall: true, Temperature: true},
		}, nil
	}
	// A nil catalog is an explicit seam for injected engine tests. Every
	// shipped CLI backend receives a loaded (possibly disabled/empty) catalog.
	return calc.Model{
		Cost: &calc.ModelCost{Cache: &calc.CacheCost{}},
		// Unknown capabilities are permissive.
		Capabilities: calc.ModelCapabilities{ToolCall: true, Temperature: true},
	}, nil
}

func normalizeModelRef(providerID, modelID string) (string, string) {
	if providerID == "" {
		if before, after, ok := strings.Cut(modelID, "/"); ok && before == "openrouter" {
			providerID, modelID = before, after
		}
	}
	if providerID == "" {
		providerID = "openrouter"
	}
	if providerID == "openrouter" {
		modelID = strings.TrimPrefix(modelID, "openrouter/")
	}
	return providerID, modelID
}

type seniorDevClientFactory struct {
	backend          *openRouterBackend
	sessionID        string
	models           seniorDevModels
	ledger           *turnLedger
	agent            string
	bypassToolFilter bool
}

func (factory seniorDevClientFactory) Client(
	_ context.Context,
	model llmcall.Model,
	choice *adaptive.RouteChoice,
	router *adaptive.AdaptiveModelRouter,
) (llmcall.StreamClient, error) {
	projection, _, err := factory.models.projection(model.ProviderID, model.ID)
	if err != nil {
		return nil, err
	}
	factory.ledger.setModel(model.ProviderID + "/" + model.ID)
	client := &orclient.Client{
		BaseURL: factory.backend.baseURL(),
		Headers: seniorDevOpenRouterHeadersWithConfig(
			factory.backend.apiKey, factory.sessionID,
			factory.backend.config.headers(model.ProviderID, model.ID),
		),
		Compatibility:  orclient.CompatibilityCompatible,
		Router:         router,
		RouteChoice:    choice,
		TotalTimeoutMS: factory.backend.totalTimeoutMS,
		ChunkTimeoutMS: factory.backend.chunkTimeoutMS,
	}
	if factory.backend.client != nil {
		client.Fetcher = factory.backend.client.Do
	}
	return seniorDevStreamClient{
		client: client, model: projection, agent: factory.agent,
		bypassToolFilter: factory.bypassToolFilter,
		backend:          factory.backend, sessionID: factory.sessionID,
	}, nil
}

type seniorDevStreamClient struct {
	backend          *openRouterBackend
	sessionID        string
	client           *orclient.Client
	model            orclient.Model
	agent            string
	bypassToolFilter bool
}

func (client seniorDevStreamClient) DoStream(
	ctx context.Context, params orclient.RequestParams,
) (llmcall.Stream, error) {
	params.Prompt = orclient.Message(params.Prompt, client.model)
	params.Tools = client.visibleTools(params.Tools)
	stream, err := client.client.DoStream(ctx, params)
	if err != nil {
		// A context-length rejection is how a smaller-than-advertised
		// endpoint announces itself; under the window policy it pins the
		// session's capacity (compaction_pin.go). The error itself is
		// unchanged: the step loop still turns it into a compaction.
		client.backend.pinCapacityOnOverflow(
			client.sessionID, client.agent, client.model.ProviderID, client.model.ID, err,
		)
		return nil, err
	}
	return stream, nil
}

func (client seniorDevStreamClient) visibleTools(tools []orclient.Tool) []orclient.Tool {
	if client.bypassToolFilter {
		return tools
	}
	definitions := make([]steploop.ToolDefinition, 0, len(tools))
	for _, provider := range tools {
		definitions = append(definitions, steploop.ToolDefinition{Provider: provider})
	}
	filtered := tool.FilterDefinitions(definitions, tool.FilterInput{
		ProviderID: client.model.ProviderID,
		ModelID:    client.model.ID,
		Flags:      tool.CurrentWebSearchFlags(),
	})
	out := make([]orclient.Tool, 0, len(filtered))
	for _, definition := range filtered {
		out = append(out, definition.Provider)
	}
	return out
}

func seniorDevOpenRouterHeadersWithConfig(
	apiKey, sessionID string, configured []orclient.HeaderPair,
) []orclient.HeaderPair {
	provider := []orclient.HeaderPair{{Name: "Authorization", Value: "Bearer " + apiKey}}
	for _, pair := range attribution.OpenRouterHeaderPairs() {
		provider = append(provider, orclient.HeaderPair{Name: pair[0], Value: pair[1]})
	}
	provider = append(provider, configured...)
	return orclient.BuildHeaders(orclient.HeaderInputs{
		Provider:                provider,
		ProviderUserAgentSuffix: "ai-sdk/openrouter/2.8.1",
		Call: []orclient.HeaderPair{
			{Name: "x-session-affinity", Value: sessionID},
		},
		UtilsUserAgentSuffix:   "ai-sdk/provider-utils/4.0.23",
		RuntimeUserAgentSuffix: "runtime/" + runtime.Version(),
	})
}

type turnCall struct {
	Summary  bool
	Detached bool
	CostUSD  float64
	ModelID  string
}

type turnLedger struct {
	mu    sync.Mutex
	calls []*turnCall
}

func (ledger *turnLedger) begin(summary bool) *turnCall {
	return ledger.beginCall(summary, false)
}

func (ledger *turnLedger) beginCall(summary, detached bool) *turnCall {
	call := &turnCall{Summary: summary, Detached: detached}
	ledger.mu.Lock()
	ledger.calls = append(ledger.calls, call)
	ledger.mu.Unlock()
	return call
}

func (ledger *turnLedger) addCost(call *turnCall, cost float64) {
	ledger.mu.Lock()
	call.CostUSD += cost
	ledger.mu.Unlock()
}

func (ledger *turnLedger) setModel(modelID string) {
	ledger.mu.Lock()
	if len(ledger.calls) > 0 {
		ledger.calls[len(ledger.calls)-1].ModelID = modelID
	}
	ledger.mu.Unlock()
}

func (ledger *turnLedger) snapshot() []turnCall {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	out := make([]turnCall, 0, len(ledger.calls))
	for _, call := range ledger.calls {
		out = append(out, *call)
	}
	return out
}

type seniorDevLLM struct {
	backend       *openRouterBackend
	models        seniorDevModels
	service       *llmcall.Service
	ledger        *turnLedger
	sessionID     string
	providerID    string
	modelID       string
	agent         string
	system        func(context.Context) string
	modelRequests modelRequestSink
}

func newSeniorDevLLM(
	backend *openRouterBackend,
	sessionID, providerID, modelID, agent, variant string,
	system func(context.Context) string,
	ledger *turnLedger,
	bypassToolFilter bool,
) *seniorDevLLM {
	models := seniorDevModels{
		backend: backend, sessionID: sessionID, agent: agent, variant: variant,
	}
	factory := seniorDevClientFactory{
		backend: backend, sessionID: sessionID, models: models, ledger: ledger,
		agent: agent, bypassToolFilter: bypassToolFilter,
	}
	return &seniorDevLLM{
		backend: backend, models: models, ledger: ledger,
		service: &llmcall.Service{
			Models: models, Clients: factory, Router: backend.router,
			DisableRouting: backend.router == nil,
		},
		sessionID: sessionID, providerID: providerID, modelID: modelID,
		agent: agent, system: system,
	}
}

func (client *seniorDevLLM) Stream(
	ctx context.Context, params orclient.RequestParams,
) (steploop.PartStream, error) {
	system := ""
	if client.system != nil {
		system = client.system(ctx)
	}
	return client.stream(ctx, params, client.agent, system, false)
}

type seniorDevSummaryClient struct{ owner *seniorDevLLM }

func (client seniorDevSummaryClient) Stream(
	ctx context.Context, params orclient.RequestParams,
) (steploop.PartStream, error) {
	return client.owner.stream(
		ctx, params, "compaction", compaction.SummarySystemPrompt, true,
	)
}

func (client *seniorDevLLM) stream(
	ctx context.Context,
	params orclient.RequestParams,
	agent string,
	system string,
	summary bool,
) (steploop.PartStream, error) {
	call := client.ledger.begin(summary)
	return client.streamAttempt(ctx, params, agent, system, call)
}

func (client *seniorDevLLM) streamAttempt(
	ctx context.Context,
	params orclient.RequestParams,
	agent string,
	system string,
	call *turnCall,
) (steploop.PartStream, error) {
	providerID, modelID := normalizeModelRef(client.providerID, params.ModelID)
	if modelID == "" {
		modelID = client.modelID
	}
	observation := beginModelRequest(ctx, client.modelRequests, client.sessionID, agent, providerID, modelID)
	model, err := client.models.GetModel(ctx, providerID, modelID)
	if err != nil {
		observation.finish("resolve", err)
		return nil, err
	}
	systems := []string{}
	if system != "" {
		systems = append(systems, system)
	}
	stream, err := client.service.Stream(ctx, llmcall.StreamInput{
		SessionID: client.sessionID,
		Model:     model,
		Agent: llmcall.Agent{
			Name: agent, Mode: agent,
			Tier: adaptive.ModelTier(baked.TierFor(agent)),
		},
		System: systems, Messages: params.Prompt,
		Tools: params.Tools, ToolChoice: params.ToolChoice,
	})
	if err != nil {
		observation.finish("begin", err)
		return nil, err
	}
	return &costPartStream{inner: stream, ledger: client.ledger, call: call, observation: observation}, nil
}

type costPartStream struct {
	inner       llmcall.Stream
	ledger      *turnLedger
	call        *turnCall
	observation *modelRequestObservation
}

func (stream *costPartStream) Next() (orclient.StreamPart, error) {
	part, err := stream.inner.Next()
	stream.observation.observe(part, err)
	if finish, ok := part.(orclient.FinishPart); ok {
		stream.ledger.addCost(stream.call, finishCost(finish))
	}
	return part, err
}

func (stream *costPartStream) Close() error {
	err := stream.inner.Close()
	stream.observation.finish("close", err)
	return err
}

func finishCost(finish orclient.FinishPart) float64 {
	raw, ok := finish.Metadata.Usage.Get("cost")
	if !ok {
		return 0
	}
	var cost float64
	if json.Unmarshal(raw, &cost) != nil {
		return 0
	}
	return cost
}

func (backend *openRouterBackend) baseURL() string {
	endpoint := strings.TrimRight(backend.endpoint, "/")
	if endpoint == "" {
		return "https://openrouter.ai/api/v1"
	}
	endpoint = strings.TrimSuffix(endpoint, "/chat/completions")
	return strings.TrimRight(endpoint, "/")
}

var _ llmcall.ModelResolver = seniorDevModels{}
var _ steploop.ModelResolver = seniorDevModels{}
var _ llmcall.ClientFactory = seniorDevClientFactory{}
var _ steploop.LLMClient = (*seniorDevLLM)(nil)
var _ steploop.LLMClient = seniorDevSummaryClient{}
