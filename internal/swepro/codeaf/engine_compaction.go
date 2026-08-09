package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode/utf16"

	"github.com/Agent-Field/swe-pro-go/internal/engine/calc"
	"github.com/Agent-Field/swe-pro-go/internal/engine/msgmodel"
	"github.com/Agent-Field/swe-pro-go/internal/engine/orclient"
	"github.com/Agent-Field/swe-pro-go/internal/engine/steploop"
	"github.com/Agent-Field/swe-pro-go/internal/session/compaction"
	"github.com/Agent-Field/swe-pro-go/internal/session/evidenceharvest"
	"github.com/Agent-Field/swe-pro-go/internal/session/overflow"
)

type codeafCompactionModels struct {
	resolver steploop.ModelResolver
}

func (models codeafCompactionModels) GetModel(
	ctx context.Context, providerID, modelID string,
) (compaction.Model, error) {
	resolved, err := models.resolver.Resolve(ctx, msgmodel.User{
		Model: msgmodel.UserModel{ProviderID: providerID, ModelID: modelID},
	})
	if err != nil {
		return compaction.Model{}, err
	}
	return compaction.Model{Message: resolved.Message, Overflow: resolved.Calc}, nil
}

func (codeafCompactionModels) GetProvider(
	context.Context, string,
) (compaction.ProviderInfo, error) {
	return compaction.ProviderInfo{}, nil
}

type codeafSummaryFactory struct {
	store  steploop.Store
	client steploop.LLMClient
}

func (factory codeafSummaryFactory) Create(
	_ context.Context,
	assistant *msgmodel.Assistant,
	_ string,
	model compaction.Model,
) (compaction.SummaryProcessor, error) {
	stepModel := steploop.Model{Message: model.Message, Calc: model.Overflow}
	return &codeafSummaryProcessor{
		processor: steploop.NewProcessor(steploop.ProcessorOptions{
			Store: factory.store, Assistant: *assistant, Model: stepModel,
		}),
		client: factory.client,
		model:  stepModel,
	}, nil
}

type codeafSummaryProcessor struct {
	processor *steploop.Processor
	client    steploop.LLMClient
	model     steploop.Model
}

func (processor *codeafSummaryProcessor) Process(
	ctx context.Context, request compaction.SummaryRequest,
) (steploop.Result, error) {
	params := processor.model.Request
	params.ModelID = processor.model.Message.ID
	params.Prompt = request.Messages
	if params.MaxOutputTokens == nil {
		maximum := calc.MaxOutputTokens(processor.model.Calc)
		params.MaxOutputTokens = &maximum
	}
	stream, err := processor.client.Stream(ctx, params)
	if err != nil {
		stream = &steploop.SliceStream{Failure: err}
	}
	return processor.processor.Process(ctx, stream)
}

func (processor *codeafSummaryProcessor) Message() msgmodel.Assistant {
	return processor.processor.Message()
}

type cappedCompactionController struct {
	inner compaction.Controller
	mu    sync.Mutex
	count int
}

func (controller *cappedCompactionController) HandleSubtask(
	ctx context.Context, input steploop.TaskInput, task msgmodel.SubtaskPart,
) error {
	return controller.inner.HandleSubtask(ctx, input, task)
}

func (controller *cappedCompactionController) ProcessCompaction(
	ctx context.Context, input steploop.TaskInput, task msgmodel.CompactionPart,
) (steploop.Result, error) {
	return controller.inner.ProcessCompaction(ctx, input, task)
}

func (controller *cappedCompactionController) IsOverflow(
	ctx context.Context,
	assistant msgmodel.Assistant,
	model steploop.Model,
	options steploop.OverflowOptions,
) (bool, error) {
	return controller.inner.IsOverflow(ctx, assistant, model, options)
}

func (controller *cappedCompactionController) CreateCompaction(
	ctx context.Context, sessionID string, user msgmodel.User, overflowed bool,
) error {
	controller.mu.Lock()
	controller.count++
	count := controller.count
	controller.mu.Unlock()
	if count > maxLeafCompactions {
		return fmt.Errorf("codeaf: leaf compaction limit %d exceeded", maxLeafCompactions)
	}
	return controller.inner.CreateCompaction(ctx, sessionID, user, overflowed)
}

func (controller *cappedCompactionController) Prune(
	ctx context.Context, sessionID string,
) error {
	return controller.inner.Prune(ctx, sessionID)
}

func newCodeafCompactionController(
	store steploop.Store,
	summaryClient steploop.LLMClient,
	resolver steploop.ModelResolver,
	workspace string,
	backend *openRouterBackend,
	sessionID string,
	lowModels []string,
	ledger *turnLedger,
) *cappedCompactionController {
	service := compaction.NewService(compaction.Dependencies{
		Store: store,
		Config: compaction.ConfigProviderFunc(func(context.Context) (overflow.Config, error) {
			return backend.config.overflowConfig()
		}),
		Agents: compaction.AgentProviderFunc(func(
			context.Context, string,
		) (compaction.Agent, error) {
			return compaction.Agent{Name: "compaction"}, nil
		}),
		Provider: codeafCompactionModels{resolver: resolver},
		Processors: codeafSummaryFactory{
			store: store, client: summaryClient,
		},
		Evidence: codeafEvidenceSelector{
			backend: backend, sessionID: sessionID,
			candidates: append([]string(nil), lowModels...), ledger: ledger,
		},
		Instance: compaction.InstanceContext{Directory: workspace, Worktree: workspace},
		NewID: func(prefix string) string {
			if prefix == "message" {
				prefix = "msg"
			} else if prefix == "part" {
				prefix = "prt"
			}
			return steploop.NewAscendingID(prefix)
		},
	})
	return &cappedCompactionController{inner: compaction.Controller{Compaction: service}}
}

type codeafEvidenceSelector struct {
	backend    *openRouterBackend
	sessionID  string
	candidates []string
	ledger     *turnLedger
}

func (selector codeafEvidenceSelector) SelectEvidence(
	ctx context.Context, blocks []string,
) (*string, error) {
	if selector.backend == nil || len(selector.candidates) == 0 {
		return compaction.FallbackEvidenceSelector{}.SelectEvidence(ctx, blocks)
	}
	model := ""
	for _, candidate := range selector.candidates {
		if strings.IndexByte(candidate, '/') > 0 {
			model = candidate
			break
		}
	}
	if model == "" {
		return compaction.FallbackEvidenceSelector{}.SelectEvidence(ctx, blocks)
	}
	judge := evidenceharvest.EvidenceJudgeFunc(func(prompt string, _ any) evidenceharvest.EvidenceJudgment {
		for attempt := 0; attempt < 2; attempt++ {
			lines, err := selector.generate(ctx, model, prompt)
			if err == nil {
				return evidenceharvest.EvidenceJudgment{Lines: lines, Source: evidenceharvest.SourceLLM}
			}
		}
		return evidenceharvest.EvidenceJudgment{Source: evidenceharvest.SourceFallback}
	})
	selected := evidenceharvest.SelectEvidence(blocks, model, &evidenceharvest.SelectEvidenceOptions{
		Judge: judge,
	})
	return selected.Text, nil
}

func (selector codeafEvidenceSelector) generate(
	ctx context.Context, fullModel, prompt string,
) ([]string, error) {
	providerID, modelID := normalizeModelRef("", fullModel)
	if providerID != "openrouter" {
		return nil, fmt.Errorf("unsupported evidence provider %q", providerID)
	}
	client := &orclient.Client{
		BaseURL: selector.backend.baseURL(),
		Headers: codeafOpenRouterHeadersWithConfig(
			selector.backend.apiKey, selector.sessionID,
			selector.backend.config.headers(providerID, modelID),
		),
		Compatibility:  orclient.CompatibilityCompatible,
		TotalTimeoutMS: selector.backend.totalTimeoutMS,
		ChunkTimeoutMS: selector.backend.chunkTimeoutMS,
	}
	if selector.backend.client != nil {
		client.Fetcher = selector.backend.client.Do
	}
	maximum := float64(4096)
	stream, err := client.DoStream(ctx, orclient.RequestParams{
		ModelID: modelID, MaxOutputTokens: &maximum,
		Prompt:            []msgmodel.ModelMessage{{Role: "user", Content: prompt}},
		OpenRouterOptions: selector.backend.config.options("", providerID, modelID),
	})
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	call := selector.ledger.beginDetached()
	var text strings.Builder
	for {
		part, nextErr := stream.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return nil, nextErr
		}
		switch value := part.(type) {
		case orclient.TextDeltaPart:
			text.WriteString(value.Delta)
		case orclient.ErrorPart:
			return nil, fmt.Errorf("evidence model error: %s", value.Error)
		case orclient.FinishPart:
			selector.ledger.addCost(call, finishCost(value))
		}
	}
	return parseEvidenceLines(text.String())
}

func parseEvidenceLines(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(strings.TrimSpace(trimmed), "```")
	var value struct {
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(trimmed)), &value); err != nil {
		return nil, err
	}
	if value.Lines == nil || len(value.Lines) > 25 {
		return nil, fmt.Errorf("invalid evidence line count")
	}
	for _, line := range value.Lines {
		length := len(utf16.Encode([]rune(line)))
		if length < 4 || length > 400 {
			return nil, fmt.Errorf("invalid evidence line length")
		}
	}
	return value.Lines, nil
}

var _ steploop.TaskController = (*cappedCompactionController)(nil)
