package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Agent-Field/swe-pro-go/internal/engine/msgmodel"
	"github.com/Agent-Field/swe-pro-go/internal/engine/orclient"
	"github.com/Agent-Field/swe-pro-go/internal/engine/steploop"
	"github.com/Agent-Field/swe-pro-go/internal/session/agentjson"
	"github.com/Agent-Field/swe-pro-go/internal/session/merger"
	"github.com/Agent-Field/swe-pro-go/internal/session/mergerecovery"
	"github.com/Agent-Field/swe-pro-go/internal/session/scheduler"
)

// runtimeMergerDispatcher adapts merger.ts's structured agent call to the
// same agent-json machinery used by the observer, reviewer, and auditor.
type runtimeMergerDispatcher struct {
	runtime *runtimeAdapter
}

func (dispatcher runtimeMergerDispatcher) Dispatch(
	ctx context.Context,
	request merger.DispatchRequest,
) (merger.DispatchResult, error) {
	if dispatcher.runtime == nil {
		return merger.DispatchResult{}, errors.New("merger dispatcher: runtime is required")
	}
	maxRetries := request.MaxRetries
	timeoutMS := int64(request.TimeoutMS)
	label := request.Label
	fallback := request.Fallback
	tools := []agentjson.ToolSetting{
		{Name: "edit", Enabled: request.Tools.Edit},
		{Name: "apply_patch", Enabled: request.Tools.ApplyPatch},
	}
	result, err := agentjson.DispatchJSON(ctx, agentjson.Input[merger.MergerDecision]{
		Agent: request.Agent, ParentSessionID: request.ParentSessionID,
		Workspace: request.Workspace, TaskPrompt: request.TaskPrompt,
		OutputPath: request.OutputPath, Schema: mergerAgentJSONSchema{},
		Fallback: &fallback, MaxRetries: &maxRetries, TimeoutMS: &timeoutMS,
		Label: &label, Tools: tools,
	}, dispatcher.runtime.agentJSON())
	return merger.DispatchResult{
		Data: result.Data, FirstTry: result.FirstTry, UsedFallback: result.UsedFallback,
	}, err
}

type mergerAgentJSONSchema struct{}

func (mergerAgentJSONSchema) SafeParse(
	raw json.RawMessage,
) agentjson.Validation[merger.MergerDecision] {
	decision, err := merger.ParseDecision(raw)
	if err != nil {
		return agentjson.Validation[merger.MergerDecision]{
			Issues: []agentjson.Issue{{Message: err.Error()}},
		}
	}
	return agentjson.Validation[merger.MergerDecision]{Data: decision}
}

type runtimeLanguageModel struct {
	ProviderID string
	ModelID    string
}

func resolveRuntimeLanguage(model any) (runtimeLanguageModel, error) {
	switch value := model.(type) {
	case scheduler.ProviderModel:
		modelID := value.ID
		if modelID == "" {
			modelID = value.ModelID
		}
		if value.ProviderID != "" && modelID != "" {
			return runtimeLanguageModel{ProviderID: value.ProviderID, ModelID: modelID}, nil
		}
	case *scheduler.ProviderModel:
		if value != nil {
			return resolveRuntimeLanguage(*value)
		}
	case runtimeLanguageModel:
		if value.ProviderID != "" && value.ModelID != "" {
			return value, nil
		}
	}
	return runtimeLanguageModel{}, fmt.Errorf("codeaf provider: model has no provider/model identity: %T", model)
}

var errRecoveryTurnComplete = errors.New("merge recovery turn complete")

// runtimeMergeRecoveryClient is merge-recovery.ts's raw streamText call. The
// ported recovery package owns the prompt, tools, verdict, and counters; this
// adapter supplies the selected model and executes its multi-step tool loop.
type runtimeMergeRecoveryClient struct {
	runtime *runtimeAdapter
}

func (client runtimeMergeRecoveryClient) Run(request mergerecovery.Request) error {
	if client.runtime == nil {
		return errors.New("merge recovery client: runtime is required")
	}
	model, err := resolveRuntimeLanguage(request.Model)
	if err != nil {
		return err
	}
	definitions := make([]steploop.ToolDefinition, 0, len(request.Tools))
	byName := make(map[string]mergerecovery.ToolDefinition, len(request.Tools))
	reported := false
	for _, recoveryTool := range request.Tools {
		schema, marshalErr := json.Marshal(recoveryTool.InputSchema)
		if marshalErr != nil {
			return fmt.Errorf("merge recovery tool %s schema: %w", recoveryTool.Name, marshalErr)
		}
		definitions = append(definitions, steploop.ToolDefinition{Provider: orclient.Tool{
			Type: "function", Name: recoveryTool.Name,
			Description: recoveryTool.Description, InputSchema: schema,
		}})
		byName[recoveryTool.Name] = recoveryTool
	}
	maxSteps := request.MaxSteps
	steps := 0
	result, runErr := client.runtime.runTurn(context.Background(), turn{
		SessionTitle: "merge-recovery", Agent: "merge-recovery",
		Workspace: client.runtime.workspace, ProviderID: model.ProviderID,
		ModelID: model.ModelID, Prompt: request.Prompt, MaxSteps: &maxSteps,
		Temperature: &request.Temperature, DisableRetries: request.MaxRetries == 0,
		RawModelCall: true, Tools: definitions,
		Execute: func(_ context.Context, call steploop.ToolCall) (steploop.ToolResult, error) {
			definition, ok := byName[call.Name]
			if !ok {
				return steploop.ToolResult{}, fmt.Errorf("unknown merge recovery tool %q", call.Name)
			}
			var input any = map[string]any{}
			if len(call.Input) > 0 {
				if err := json.Unmarshal(call.Input, &input); err != nil {
					return steploop.ToolResult{}, err
				}
			}
			output, err := definition.Execute(input)
			if call.Name == "report" && err == nil {
				reported = true
			}
			return steploop.ToolResult{
				Title: call.Name, Metadata: msgmodel.RawObject(`{}`), Output: output,
			}, err
		},
		AfterTurn: func(context.Context, scheduler.LeafTurnObservation) error {
			steps++
			if reported || float64(steps) >= request.MaxSteps {
				return errRecoveryTurnComplete
			}
			return nil
		},
	})
	client.runtime.addCost(result.CostUSD)
	if errors.Is(runErr, errRecoveryTurnComplete) {
		return nil
	}
	return runErr
}

var _ merger.Dispatcher = runtimeMergerDispatcher{}
var _ mergerecovery.Client = runtimeMergeRecoveryClient{}
