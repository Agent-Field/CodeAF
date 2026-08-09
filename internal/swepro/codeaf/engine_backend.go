package codeaf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/attribution"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/baked"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/project"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/leafoutcome"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/scheduler"
	systemprompt "github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/system"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/testmemo"
)

type turnToolExecutor struct{ request turn }

func (backend *openRouterBackend) Run(
	ctx context.Context, request turn,
) (turnResult, error) {
	return backend.runEngine(ctx, request)
}

func (executor turnToolExecutor) Execute(
	ctx context.Context, call steploop.ToolCall,
) (steploop.ToolResult, error) {
	return executeAdvertisedTool(ctx, executor.request, call)
}

func (backend *openRouterBackend) runEngine(
	ctx context.Context, request turn,
) (turnResult, error) {
	if backend.apiKey == "" {
		return turnResult{}, errors.New("OPENROUTER_API_KEY is not set in the environment")
	}
	sessionID := request.SessionID
	if sessionID == "" {
		sessionID = steploop.NewAscendingID("ses")
	}
	providerID, modelID := normalizeModelRef(request.ProviderID, request.ModelID)
	system := func(callCtx context.Context) string {
		// prompt.ts:1805-1811 resolves root instructions on every iteration.
		instructions := request.SystemInstructions
		if request.LoadInstructions != nil {
			instructions = request.LoadInstructions(callCtx)
		}
		return composeTurnSystem(callCtx, request, providerID, modelID, instructions)
	}

	store := request.Store
	if store == nil {
		store = newTurnStore()
	}
	variant := request.Variant
	if variant == "" {
		variant = backend.variant
	}
	startMessageID := request.PromptMessageID
	if !request.PromptPersisted {
		var seedErr error
		startMessageID, seedErr = seedTurn(
			ctx, store, sessionID, request, providerID, modelID, variant,
		)
		if seedErr != nil {
			return turnResult{SessionID: sessionID}, seedErr
		}
	}
	if startMessageID == "" {
		return turnResult{SessionID: sessionID}, errors.New("codeaf engine: prompt message is required")
	}
	ledger := &turnLedger{}
	models := codeafModels{
		backend: backend, sessionID: sessionID, agent: request.Agent, variant: variant,
		temperature: request.Temperature,
	}
	client := newCodeafLLM(
		backend, sessionID, providerID, modelID, request.Agent, variant, system, ledger,
		request.Temperature, request.DisableRetries, request.RawModelCall,
	)
	tasks := newCodeafCompactionController(
		store, codeafSummaryClient{owner: client}, models, request.Workspace,
		backend, sessionID, request.LowModels, ledger,
	)
	loop := steploop.Loop{
		Store: store, Client: client, Models: models,
		Executor: turnToolExecutor{request: request}, Tasks: tasks,
		Scheduler: request.Scheduler, ExitGuard: request.ExitGuard,
	}
	assistant, runErr := loop.Run(ctx, steploop.RunOptions{
		SessionID: sessionID, ParentID: request.ParentSessionID,
		Workspace: request.Workspace, Worktree: request.Workspace,
		MaxSteps:        request.MaxSteps,
		Tools:           request.Tools,
		InjectReminders: turnReminderInjector(store, sessionID, request.BetweenStepReminder),
		AfterAssistant:  request.AfterAssistant,
		AfterTurn: func(hookCtx context.Context, assistant msgmodel.Assistant, parts msgmodel.Parts) error {
			if request.AfterTurn == nil {
				return nil
			}
			observation := scheduler.LeafTurnObservation{CostUSD: ledger.lastCost()}
			if assistant.Finish != nil {
				observation.Finish = *assistant.Finish
			}
			for _, raw := range parts {
				part, ok := raw.(msgmodel.ToolPart)
				if !ok {
					continue
				}
				args := "{}"
				status := "pending"
				if part.State != nil {
					args = string(part.State.ToolInput().Value())
					status = part.State.ToolStatus()
				}
				observation.Parts = append(observation.Parts, scheduler.LeafPart{
					Type: "tool", Tool: part.Tool, ArgsKey: args, Status: status,
				})
			}
			return request.AfterTurn(hookCtx, observation)
		},
	})
	messages, messagesErr := store.Messages(ctx, sessionID)
	result := projectTurnResult(
		sessionID, messagesSince(messages, startMessageID), ledger.snapshot(),
	)
	if runErr != nil {
		return result, runErr
	}
	if messagesErr != nil {
		return result, messagesErr
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return result, ctxErr
	}
	if assistant.Error != nil {
		return result, turnAssistantError(assistant.Error)
	}
	return result, nil
}

func composeTurnSystem(
	ctx context.Context,
	request turn,
	providerID string,
	modelID string,
	instructions []string,
) string {
	if request.RawModelCall {
		return ""
	}
	model := systemprompt.Model{ProviderID: providerID, API: systemprompt.API{ID: modelID}}
	parts := []string{}
	// llm.ts:186 — `input.agent.prompt ? [input.agent.prompt] : provider(model)`.
	// Either/or, not additive: an agent prompt fully replaces the model-family
	// base prompt. Appending the family prompt on top would send Codex/GPT/
	// Claude agents a second large base prompt TS never sends, changing role
	// behavior and breaking the cached prompt prefix.
	agentPrompt := request.AgentMarkdown
	if !request.AgentPromptVerbatim {
		agentPrompt = baked.PromptContent(agentPrompt)
	}
	if strings.TrimSpace(agentPrompt) != "" {
		parts = append(parts, agentPrompt)
	} else {
		parts = append(parts, systemprompt.Provider(model)...)
	}
	service := systemprompt.New(turnSystemContext(ctx, request.Workspace))
	parts = append(parts, service.Environment(model)...)
	parts = append(parts, instructions...)
	parts = append(parts, request.Reminder)
	if instruction := attribution.CommitPromptInstruction(); instruction != "" {
		parts = append(parts, instruction)
	}
	return strings.Join(nonEmpty(parts...), "\n")
}

func turnSystemContext(ctx context.Context, workspace string) systemprompt.Context {
	directory, worktree, vcs := workspace, workspace, ""
	if instance, ok := project.FromContext(ctx); ok {
		if instance.Directory != "" {
			directory = instance.Directory
		}
		if instance.Worktree != "" {
			worktree = instance.Worktree
		}
		if instance.Project.VCS != nil {
			vcs = *instance.Project.VCS
		}
	}
	if vcs == "" && directory != "" {
		command := exec.CommandContext(ctx, "git", "-C", directory, "rev-parse", "--is-inside-work-tree")
		if output, err := command.Output(); err == nil && strings.TrimSpace(string(output)) == "true" {
			vcs = "git"
		}
	}
	return systemprompt.Context{
		Directory: directory, Worktree: worktree,
		Project: systemprompt.Project{VCS: vcs},
	}
}

func nonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}

func seedTurn(
	ctx context.Context,
	store steploop.Store,
	sessionID string,
	request turn,
	providerID string,
	modelID string,
	variant string,
) (string, error) {
	now := uint64(time.Now().UnixMilli())
	messageID := request.MessageID
	if messageID == "" {
		messageID = steploop.NewAscendingID("msg")
	}
	user := msgmodel.User{
		MessageBase: msgmodel.MessageBase{ID: messageID, SessionID: sessionID},
		Time:        msgmodel.TimeCreated{Created: now},
		Agent:       request.Agent,
		Model: msgmodel.UserModel{
			ProviderID: providerID, ModelID: modelID,
		},
	}
	if variant != "" {
		user.Model.Variant = &variant
	}
	if err := store.UpdateMessage(ctx, user); err != nil {
		return "", err
	}
	if err := store.UpdatePart(ctx, msgmodel.TextPart{
		PartBase: msgmodel.PartBase{
			ID: steploop.NewAscendingID("prt"), SessionID: sessionID, MessageID: messageID,
		},
		Text: request.Prompt,
	}); err != nil {
		return "", err
	}
	return messageID, nil
}

func messagesSince(messages []msgmodel.WithParts, messageID string) []msgmodel.WithParts {
	for index, message := range messages {
		if message.Info.MessageID() == messageID {
			return messages[index:]
		}
	}
	return messages
}

func turnReminderInjector(
	store steploop.Store,
	sessionID string,
	next func() string,
) func(context.Context, []msgmodel.WithParts, msgmodel.User) ([]msgmodel.WithParts, error) {
	if next == nil {
		return nil
	}
	return func(
		ctx context.Context, messages []msgmodel.WithParts, user msgmodel.User,
	) ([]msgmodel.WithParts, error) {
		text := next()
		if text == "" {
			return messages, nil
		}
		messageID := steploop.NewAscendingID("msg")
		reminder := msgmodel.User{
			MessageBase: msgmodel.MessageBase{ID: messageID, SessionID: sessionID},
			Time:        msgmodel.TimeCreated{Created: uint64(time.Now().UnixMilli())},
			Agent:       user.Agent, Model: user.Model,
		}
		part := msgmodel.TextPart{
			PartBase: msgmodel.PartBase{
				ID: steploop.NewAscendingID("prt"), SessionID: sessionID, MessageID: messageID,
			},
			Text: text, Synthetic: boolPointer(true),
		}
		if err := store.UpdateMessage(ctx, reminder); err != nil {
			return nil, err
		}
		if err := store.UpdatePart(ctx, part); err != nil {
			return nil, err
		}
		out := append([]msgmodel.WithParts(nil), messages...)
		return append(out, msgmodel.WithParts{Info: reminder, Parts: msgmodel.Parts{part}}), nil
	}
}

func boolPointer(value bool) *bool { return &value }

func projectTurnResult(
	sessionID string, messages []msgmodel.WithParts, calls []turnCall,
) turnResult {
	result := turnResult{SessionID: sessionID, CallCosts: make([]float64, 0, len(calls))}
	if encoded, err := json.Marshal(messages); err == nil {
		var transcript []any
		if json.Unmarshal(encoded, &transcript) == nil {
			result.TestPassed = testmemo.LatestTestCommandPassed(transcript)
		}
	}
	for _, call := range calls {
		result.CostUSD += call.CostUSD
		result.CallCosts = append(result.CallCosts, call.CostUSD)
	}

	lastSummary := -1
	summaryText := ""
	callIndex := 0
	messageCalls := make([]turnCall, 0, len(calls))
	for _, call := range calls {
		if !call.Detached {
			messageCalls = append(messageCalls, call)
		}
	}
	assistantCalls := make(map[int]turnCall)
	for index, message := range messages {
		assistant, ok := message.Info.(msgmodel.Assistant)
		if !ok {
			continue
		}
		call := turnCall{}
		if callIndex < len(messageCalls) {
			call = messageCalls[callIndex]
		}
		assistantCalls[index] = call
		callIndex++
		if assistant.Summary != nil && *assistant.Summary &&
			assistant.Finish != nil && *assistant.Finish != "" && assistant.Error == nil {
			lastSummary = index
			summaryText = messageText(message)
		}
	}
	if lastSummary >= 0 {
		result.Parts = append(result.Parts, scheduler.LeafPart{
			Type: "compaction", Text: summaryText,
		})
	}

	hiddenCost := 0.0
	pendingActionCost := 0.0
	for index, message := range messages {
		assistant, ok := message.Info.(msgmodel.Assistant)
		if !ok {
			continue
		}
		call := assistantCalls[index]
		if index <= lastSummary || (assistant.Summary != nil && *assistant.Summary) {
			hiddenCost += call.CostUSD
			pendingActionCost += call.CostUSD
			continue
		}
		pendingActionCost += call.CostUSD
		statuses := make([]string, 0)
		charged := false
		for _, raw := range message.Parts {
			switch part := raw.(type) {
			case msgmodel.TextPart:
				if part.Text != "" {
					result.Parts = append(result.Parts, scheduler.LeafPart{
						Type: "text", Text: part.Text,
					})
					result.Text = part.Text
				}
			case msgmodel.ToolPart:
				status := "pending"
				args := "{}"
				if part.State != nil {
					status = part.State.ToolStatus()
					args = string(part.State.ToolInput().Value())
				}
				leafPart := scheduler.LeafPart{
					Type: "tool", Tool: part.Tool, ArgsKey: args, Status: status,
				}
				if !charged && pendingActionCost != 0 {
					cost := pendingActionCost
					leafPart.CostUSD = &cost
					pendingActionCost = 0
					charged = true
				}
				result.Parts = append(result.Parts, leafPart)
				statuses = append(statuses, status)
			}
		}
		result.Messages = append(result.Messages, engineLeafObservation(call.CostUSD, statuses))
	}
	carryLeafObservationCost(result.Messages, hiddenCost)
	return result
}

func messageText(message msgmodel.WithParts) string {
	var lines []string
	for _, raw := range message.Parts {
		if part, ok := raw.(msgmodel.TextPart); ok && strings.TrimSpace(part.Text) != "" {
			lines = append(lines, part.Text)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func engineLeafObservation(
	costUSD float64, toolStatuses []string,
) *leafoutcome.SessionMessage {
	role := "assistant"
	cost := jscompat.JSNumber(costUSD)
	parts := make([]*leafoutcome.SessionMessagePart, 0, len(toolStatuses))
	for _, value := range toolStatuses {
		partType, status := "tool", value
		parts = append(parts, &leafoutcome.SessionMessagePart{
			Type: &partType, State: &leafoutcome.SessionMessagePartState{Status: &status},
		})
	}
	return &leafoutcome.SessionMessage{
		Info: &leafoutcome.SessionMessageInfo{Role: &role, Cost: &cost}, Parts: parts,
	}
}

func turnAssistantError(value *msgmodel.AssistantError) error {
	if value == nil {
		return nil
	}
	var data struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(value.Data, &data) == nil && data.Message != "" {
		return errors.New(data.Message)
	}
	if value.Name != "" {
		return errors.New(value.Name)
	}
	return fmt.Errorf("codeaf: model turn failed")
}

var _ steploop.ToolExecutor = turnToolExecutor{}
