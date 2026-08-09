// Package llmcall ports src/session/llm.ts:1-559 (swe-pro 3b25a1a).
// It resolves adaptive-router choices, assembles a single OpenRouter call, and
// leaves retries and multi-turn behavior to retrysched and steploop.
package llmcall

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/Agent-Field/swe-pro-go/internal/engine/msgmodel"
	"github.com/Agent-Field/swe-pro-go/internal/engine/orclient"
	"github.com/Agent-Field/swe-pro-go/internal/engine/retrysched"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/router/adaptive"
	"github.com/Agent-Field/swe-pro-go/internal/router/state"
)

const OutputTokenMax = 32_000

type Agent struct {
	Name string
	Mode string
	Tier adaptive.ModelTier
}

type Model struct {
	ProviderID string
	ID         string
	APIID      string
	Params     orclient.RequestParams
}

type StreamInput struct {
	UserID          string
	SessionID       string
	ParentSessionID string
	Model           Model
	Agent           Agent
	System          []string
	Messages        []msgmodel.ModelMessage
	Tools           []orclient.Tool
	DisabledTools   map[string]bool
	UserTools       map[string]bool
	ToolChoice      *orclient.ToolChoice
}

type ModelResolver interface {
	GetModel(ctx context.Context, providerID, modelID string) (Model, error)
}

type ModelResolverFunc func(context.Context, string, string) (Model, error)

func (f ModelResolverFunc) GetModel(ctx context.Context, providerID, modelID string) (Model, error) {
	return f(ctx, providerID, modelID)
}

type Stream interface {
	Next() (orclient.StreamPart, error)
	Close() error
}

type StreamClient interface {
	DoStream(ctx context.Context, params orclient.RequestParams) (Stream, error)
}

type ClientFactory interface {
	Client(ctx context.Context, model Model, choice *adaptive.RouteChoice, router *adaptive.AdaptiveModelRouter) (StreamClient, error)
}

type ClientFactoryFunc func(context.Context, Model, *adaptive.RouteChoice, *adaptive.AdaptiveModelRouter) (StreamClient, error)

func (f ClientFactoryFunc) Client(ctx context.Context, model Model, choice *adaptive.RouteChoice, router *adaptive.AdaptiveModelRouter) (StreamClient, error) {
	return f(ctx, model, choice, router)
}

type Service struct {
	Models         ModelResolver
	Clients        ClientFactory
	Router         *adaptive.AdaptiveModelRouter
	DisableRouting bool
}

type ResolvedCall struct {
	Model      Model
	Choice     *adaptive.RouteChoice
	Candidates []adaptive.ModelCandidate
	Params     orclient.RequestParams
}

func (s *Service) resolveRouter() *adaptive.AdaptiveModelRouter {
	if s.DisableRouting {
		return nil
	}
	if s.Router != nil {
		return s.Router
	}
	if router, ok := state.GetRouter().(*adaptive.AdaptiveModelRouter); ok {
		return router
	}
	return nil
}

// ResolveAndAssemble is llm.ts's pure-enough model-resolution and parameter
// assembly core. A failed routed-model lookup is registered immediately and
// the incoming model remains usable.
func (s *Service) ResolveAndAssemble(ctx context.Context, input StreamInput) (ResolvedCall, error) {
	model := input.Model
	router := s.resolveRouter()
	tier := input.Agent.Tier
	if tier == "" {
		tier = adaptive.ModelTierHigh
	}
	var candidates []adaptive.ModelCandidate
	var choice *adaptive.RouteChoice
	if router != nil {
		tier = router.EffectiveTier(tier)
		candidates = router.CandidatesForTier(tier)
		if len(candidates) > 0 {
			picked, err := router.Pick(input.Agent.Name, tier)
			if err != nil {
				return ResolvedCall{}, err
			}
			choice = &picked
			if picked.Candidate.ID != fullID(model) {
				providerID, modelID := splitModelID(picked.Candidate.ID)
				routed, resolveErr := s.getModel(ctx, providerID, modelID)
				if resolveErr != nil {
					router.Register(picked, 0, 0, retrysched.ToRouterValue(errors.New("unresolvable model "+picked.Candidate.ID)))
					choice = nil
				} else {
					model = routed
				}
			}
		}
	}

	// The language/client resolution fallback walks the complete pool in its
	// existing order. The actual client is opened by Stream after assembly.
	if s.Models != nil {
		if _, err := s.Models.GetModel(ctx, model.ProviderID, model.ID); err != nil {
			found := false
			for _, candidate := range candidates {
				providerID, modelID := splitModelID(candidate.ID)
				alternate, altErr := s.Models.GetModel(ctx, providerID, modelID)
				if altErr == nil {
					model = alternate
					found = true
					break
				}
			}
			if !found {
				return ResolvedCall{}, err
			}
		}
	}

	params := model.Params
	params.ModelID = model.ID
	params.Prompt = make([]msgmodel.ModelMessage, 0, len(input.System)+len(input.Messages))
	for _, system := range input.System {
		params.Prompt = append(params.Prompt, msgmodel.ModelMessage{Role: "system", Content: system})
	}
	params.Prompt = append(params.Prompt, input.Messages...)
	params.ToolChoice = input.ToolChoice
	params.Tools = resolveTools(input)

	isLiteLLM := strings.Contains(strings.ToLower(model.ProviderID), "litellm") ||
		strings.Contains(strings.ToLower(model.APIID), "litellm")
	if (isLiteLLM || strings.Contains(model.ProviderID, "github-copilot")) &&
		len(params.Tools) == 0 && HasToolCalls(input.Messages) {
		params.Tools = []orclient.Tool{noopTool()}
	}
	sort.SliceStable(params.Tools, func(i, j int) bool {
		return jscompat.LocaleCompare(params.Tools[i].Name, params.Tools[j].Name) < 0
	})
	return ResolvedCall{Model: model, Choice: choice, Candidates: candidates, Params: params}, nil
}

func (s *Service) Stream(ctx context.Context, input StreamInput) (Stream, error) {
	if s.Clients == nil {
		return nil, errors.New("llmcall: ClientFactory is required")
	}
	call, err := s.ResolveAndAssemble(ctx, input)
	if err != nil {
		return nil, err
	}
	client, err := s.Clients.Client(ctx, call.Model, call.Choice, s.resolveRouter())
	if err != nil {
		return nil, err
	}
	return client.DoStream(ctx, call.Params)
}

func (s *Service) getModel(ctx context.Context, providerID, modelID string) (Model, error) {
	if s.Models == nil {
		return Model{}, errors.New("llmcall: ModelResolver is required")
	}
	return s.Models.GetModel(ctx, providerID, modelID)
}

func resolveTools(input StreamInput) []orclient.Tool {
	out := make([]orclient.Tool, 0, len(input.Tools))
	for _, tool := range input.Tools {
		if input.DisabledTools[tool.Name] {
			continue
		}
		if enabled, present := input.UserTools[tool.Name]; present && !enabled {
			continue
		}
		out = append(out, tool)
	}
	return out
}

// HasToolCalls is llm.ts:549-557. Only array content is inspected.
func HasToolCalls(messages []msgmodel.ModelMessage) bool {
	for _, message := range messages {
		parts, ok := message.Content.([]any)
		if !ok {
			switch typed := message.Content.(type) {
			case []msgmodel.ToolCallContent:
				if len(typed) > 0 {
					return true
				}
			case []msgmodel.ToolResultContent:
				if len(typed) > 0 {
					return true
				}
			}
			continue
		}
		for _, part := range parts {
			switch item := part.(type) {
			case msgmodel.ToolCallContent, msgmodel.ToolResultContent:
				return true
			case map[string]any:
				if item["type"] == "tool-call" || item["type"] == "tool-result" {
					return true
				}
			case json.RawMessage:
				var probe struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(item, &probe) == nil && (probe.Type == "tool-call" || probe.Type == "tool-result") {
					return true
				}
			}
		}
	}
	return false
}

// Retryable preserves retry.ts's model-visible classification text.
func Retryable(err retrysched.Err, providerID string) *retrysched.RetryInfo {
	return retrysched.Retryable(err, providerID)
}

func splitModelID(full string) (string, string) {
	index := strings.IndexByte(full, '/')
	if index <= 0 {
		return full, ""
	}
	return full[:index], full[index+1:]
}

func fullID(model Model) string { return model.ProviderID + "/" + model.ID }

func noopTool() orclient.Tool {
	return orclient.Tool{
		Type:        "function",
		Name:        "_noop",
		Description: "Do not call this tool. It exists only for API compatibility and must never be invoked.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"reason":{"type":"string","description":"Unused"}}}`),
	}
}

// OpenRouterClientFactory wires a configured endpoint builder to llmcall.
type OpenRouterClientFactory func(ctx context.Context, model Model) (*orclient.Client, error)

func (f OpenRouterClientFactory) Client(ctx context.Context, model Model, choice *adaptive.RouteChoice, router *adaptive.AdaptiveModelRouter) (StreamClient, error) {
	client, err := f(ctx, model)
	if err != nil {
		return nil, err
	}
	client.Router = router
	client.RouteChoice = choice
	return concreteClient{client}, nil
}

type concreteClient struct{ client *orclient.Client }

func (c concreteClient) DoStream(ctx context.Context, params orclient.RequestParams) (Stream, error) {
	return c.client.DoStream(ctx, params)
}
