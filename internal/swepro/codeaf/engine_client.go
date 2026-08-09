package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/attribution"
	"github.com/Agent-Field/swe-pro-go/internal/baked"
	"github.com/Agent-Field/swe-pro-go/internal/engine/calc"
	"github.com/Agent-Field/swe-pro-go/internal/engine/msgmodel"
	"github.com/Agent-Field/swe-pro-go/internal/engine/orclient"
	"github.com/Agent-Field/swe-pro-go/internal/engine/steploop"
	"github.com/Agent-Field/swe-pro-go/internal/router/adaptive"
	"github.com/Agent-Field/swe-pro-go/internal/session/llmcall"
	sessionretry "github.com/Agent-Field/swe-pro-go/internal/session/retry"
	"github.com/Agent-Field/swe-pro-go/internal/tool"
)

type codeafModels struct {
	backend     *openRouterBackend
	sessionID   string
	agent       string
	variant     string
	temperature *float64
}

func (models codeafModels) GetModel(
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
	effort := models.variant
	if effort == "" {
		effort = models.backend.variant
	}
	if effort != "" {
		reasoning := orclient.NewObject()
		reasoning.SetString("effort", effort)
		options.SetObject("reasoning", reasoning)
	}
	temperature := orclient.Temperature(projection)
	topP := orclient.TopP(projection)
	if value, ok := configNumber(models.backend.config.agent(models.agent)["temperature"]); ok {
		temperature = &value
	}
	if models.temperature != nil {
		value := *models.temperature
		temperature = &value
	}
	if value, ok := configNumber(models.backend.config.agent(models.agent)["top_p"]); ok {
		topP = &value
	}
	return llmcall.Model{
		ProviderID: providerID,
		ID:         modelID,
		APIID:      modelID,
		Params: orclient.RequestParams{
			MaxOutputTokens:   &maximum,
			Temperature:       temperature,
			TopP:              topP,
			TopK:              orclient.TopK(projection),
			OpenRouterOptions: options,
			Compatibility:     orclient.CompatibilityCompatible,
		},
	}, nil
}

func (models codeafModels) Resolve(
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

func (models codeafModels) projection(
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

func (models codeafModels) catalogModel(providerID, modelID string) (calc.Model, error) {
	if models.backend.catalog != nil {
		metadata, err := models.backend.catalog.Resolve(providerID, modelID)
		if err == nil {
			return metadata, nil
		}
		if len(models.backend.config.model(providerID, modelID)) == 0 {
			return calc.Model{}, err
		}
		// provider.ts gives config-defined models absent from models.dev zero
		// cost and zero context/output defaults.
		return calc.Model{
			Cost:         &calc.ModelCost{Cache: &calc.CacheCost{}},
			Capabilities: calc.ModelCapabilities{ToolCall: true},
		}, nil
	}
	// A nil catalog is an explicit seam for injected engine tests. Every
	// shipped CLI backend receives a loaded (possibly disabled/empty) catalog.
	return calc.Model{
		Cost:         &calc.ModelCost{Cache: &calc.CacheCost{}},
		Capabilities: calc.ModelCapabilities{ToolCall: true},
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

type codeafClientFactory struct {
	backend          *openRouterBackend
	sessionID        string
	models           codeafModels
	ledger           *turnLedger
	agent            string
	bypassToolFilter bool
}

func (factory codeafClientFactory) Client(
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
		Headers: codeafOpenRouterHeadersWithConfig(
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
	return codeafStreamClient{
		client: client, model: projection, agent: factory.agent,
		bypassToolFilter: factory.bypassToolFilter,
	}, nil
}

type codeafStreamClient struct {
	client           *orclient.Client
	model            orclient.Model
	agent            string
	bypassToolFilter bool
}

func (client codeafStreamClient) DoStream(
	ctx context.Context, params orclient.RequestParams,
) (llmcall.Stream, error) {
	params.Prompt = orclient.Message(params.Prompt, client.model)
	params.Tools = client.visibleTools(params.Tools)
	return client.client.DoStream(ctx, params)
}

func (client codeafStreamClient) visibleTools(tools []orclient.Tool) []orclient.Tool {
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
		AgentName:  client.agent,
		Flags:      tool.CurrentWebSearchFlags(),
	})
	out := make([]orclient.Tool, 0, len(filtered))
	for _, definition := range filtered {
		out = append(out, definition.Provider)
	}
	return out
}

func codeafOpenRouterHeaders(apiKey, sessionID string) []orclient.HeaderPair {
	return codeafOpenRouterHeadersWithConfig(apiKey, sessionID, nil)
}

func codeafOpenRouterHeadersWithConfig(
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

func (ledger *turnLedger) beginDetached() *turnCall {
	return ledger.beginCall(true, true)
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

func (ledger *turnLedger) lastCost() float64 {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if len(ledger.calls) == 0 {
		return 0
	}
	return ledger.calls[len(ledger.calls)-1].CostUSD
}

type codeafLLM struct {
	backend        *openRouterBackend
	models         codeafModels
	service        *llmcall.Service
	ledger         *turnLedger
	sessionID      string
	providerID     string
	modelID        string
	agent          string
	system         func(context.Context) string
	disableRetries bool
}

func newCodeafLLM(
	backend *openRouterBackend,
	sessionID, providerID, modelID, agent, variant string,
	system func(context.Context) string,
	ledger *turnLedger,
	temperature *float64,
	disableRetries bool,
	bypassToolFilter bool,
) *codeafLLM {
	models := codeafModels{
		backend: backend, sessionID: sessionID, agent: agent, variant: variant,
		temperature: temperature,
	}
	factory := codeafClientFactory{
		backend: backend, sessionID: sessionID, models: models, ledger: ledger,
		agent: agent, bypassToolFilter: bypassToolFilter,
	}
	return &codeafLLM{
		backend: backend, models: models, ledger: ledger,
		service: &llmcall.Service{
			Models: models, Clients: factory, Router: backend.router,
			DisableRouting: backend.router == nil,
		},
		sessionID: sessionID, providerID: providerID, modelID: modelID,
		agent: agent, system: system, disableRetries: disableRetries,
	}
}

func (client *codeafLLM) Stream(
	ctx context.Context, params orclient.RequestParams,
) (steploop.PartStream, error) {
	system := ""
	if client.system != nil {
		system = client.system(ctx)
	}
	return client.stream(ctx, params, client.agent, system, false)
}

type codeafSummaryClient struct{ owner *codeafLLM }

func (client codeafSummaryClient) Stream(
	ctx context.Context, params orclient.RequestParams,
) (steploop.PartStream, error) {
	return client.owner.stream(ctx, params, "compaction", "", true)
}

func (client *codeafLLM) stream(
	ctx context.Context,
	params orclient.RequestParams,
	agent string,
	system string,
	summary bool,
) (steploop.PartStream, error) {
	call := client.ledger.begin(summary)
	stream := &codeafRetryStream{
		owner: client, ctx: ctx, params: params,
		agent: agent, system: system, call: call,
	}
	if err := stream.open(); err != nil {
		return nil, err
	}
	return stream, nil
}

func (client *codeafLLM) streamAttempt(
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
	model, err := client.models.GetModel(ctx, providerID, modelID)
	if err != nil {
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
		return nil, err
	}
	return &costPartStream{inner: stream, ledger: client.ledger, call: call}, nil
}

type costPartStream struct {
	inner  llmcall.Stream
	ledger *turnLedger
	call   *turnCall
}

func (stream *costPartStream) Next() (orclient.StreamPart, error) {
	part, err := stream.inner.Next()
	if finish, ok := part.(orclient.FinishPart); ok {
		stream.ledger.addCost(stream.call, finishCost(finish))
	}
	return part, err
}

func (stream *costPartStream) Close() error { return stream.inner.Close() }

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

type codeafRetryStream struct {
	owner           *codeafLLM
	ctx             context.Context
	params          orclient.RequestParams
	agent           string
	system          string
	call            *turnCall
	active          steploop.PartStream
	failures        int
	yieldedToolCall bool
	closed          bool
}

func (stream *codeafRetryStream) open() error {
	for {
		active, err := stream.owner.streamAttempt(
			stream.ctx, stream.params, stream.agent, stream.system, stream.call,
		)
		if err == nil {
			if active == nil {
				active = &steploop.SliceStream{}
			}
			stream.active = active
			return nil
		}
		retry, retryErr := stream.retry(sessionretry.FromError(err))
		if retryErr != nil {
			return retryErr
		}
		if !retry {
			return err
		}
	}
}

func (stream *codeafRetryStream) Next() (orclient.StreamPart, error) {
	for {
		if stream.closed || stream.active == nil {
			return nil, io.EOF
		}
		part, err := stream.active.Next()
		if err != nil && err != io.EOF {
			_ = stream.active.Close()
			if stream.yieldedToolCall {
				return nil, err
			}
			retry, retryErr := stream.retry(sessionretry.FromError(err))
			if retryErr != nil {
				return nil, retryErr
			}
			if retry {
				if err := stream.open(); err != nil {
					return nil, err
				}
				continue
			}
		}
		if err != nil {
			return nil, err
		}

		var classified sessionretry.Err
		switch value := part.(type) {
		case orclient.ErrorPart:
			classified = sessionretry.FromStreamError(value.Error)
		case orclient.AbortPart:
			if stream.ctx.Err() != nil {
				return part, nil
			}
			message := "Aborted"
			if value.HasReason && value.Reason != "" {
				message = value.Reason
			}
			classified = sessionretry.FromError(errors.New(message))
		default:
			if _, ok := part.(orclient.ToolCallPart); ok {
				stream.yieldedToolCall = true
			}
			return part, nil
		}

		_ = stream.active.Close()
		if stream.yieldedToolCall {
			return part, nil
		}
		retry, retryErr := stream.retry(classified)
		if retryErr != nil {
			return nil, retryErr
		}
		if !retry {
			return part, nil
		}
		if err := stream.open(); err != nil {
			return nil, err
		}
	}
}

func (stream *codeafRetryStream) Close() error {
	stream.closed = true
	if stream.active == nil {
		return nil
	}
	return stream.active.Close()
}

func (stream *codeafRetryStream) retry(classified sessionretry.Err) (bool, error) {
	if err := stream.ctx.Err(); err != nil {
		return false, err
	}
	if stream.owner.disableRetries {
		return false, nil
	}
	stream.failures++
	wait, again, err := sessionretry.PolicyStep(
		float64(stream.failures), classified, sessionretry.PolicyOptions{
			Provider: "openrouter",
			Parse: func(input any) sessionretry.Err {
				return input.(sessionretry.Err)
			},
			Set: func(decision sessionretry.Decision) error {
				if stream.owner.backend.logf != nil {
					stream.owner.backend.logf(
						"retry attempt=%g reason=%q delay_ms=%g",
						decision.Attempt, decision.Message, decision.Wait,
					)
				}
				return nil
			},
		},
	)
	if err != nil || !again {
		return false, err
	}
	wait = sessionretry.ClampProviderSuggestedDelay(wait, classified)
	sleep := stream.owner.backend.sleep
	if sleep == nil {
		sleep = sleepContext
	}
	if err := sleep(stream.ctx, time.Duration(wait*float64(time.Millisecond))); err != nil {
		return false, err
	}
	return true, nil
}

func (backend *openRouterBackend) baseURL() string {
	endpoint := strings.TrimRight(backend.endpoint, "/")
	if endpoint == "" {
		return "https://openrouter.ai/api/v1"
	}
	endpoint = strings.TrimSuffix(endpoint, "/chat/completions")
	return strings.TrimRight(endpoint, "/")
}

var _ llmcall.ModelResolver = codeafModels{}
var _ steploop.ModelResolver = codeafModels{}
var _ llmcall.ClientFactory = codeafClientFactory{}
var _ steploop.LLMClient = (*codeafLLM)(nil)
var _ steploop.LLMClient = codeafSummaryClient{}
