//go:build !windows

package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/seniordev/attribution"
	"github.com/Agent-Field/codeaf/internal/seniordev/baked"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/netpolicy"
	"github.com/Agent-Field/codeaf/internal/seniordev/project"
	systemprompt "github.com/Agent-Field/codeaf/internal/seniordev/session/system"
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
	// The only way composing the system prompt fails is a turn with no agent
	// prompt, which is a property of the request rather than of any one step,
	// so it is rejected once here and the per-step closure below cannot fail.
	if _, err := composeTurnSystem(ctx, request, providerID, modelID, request.SystemInstructions); err != nil {
		return turnResult{SessionID: sessionID}, err
	}
	system := func(callCtx context.Context) string {
		// Root instructions are resolved again on every step.
		instructions := request.SystemInstructions
		if request.LoadInstructions != nil {
			instructions = request.LoadInstructions(callCtx)
		}
		text, _ := composeTurnSystem(callCtx, request, providerID, modelID, instructions)
		return text
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
		return turnResult{SessionID: sessionID}, errors.New("senior-dev engine: prompt message is required")
	}
	ledger := &turnLedger{}
	models := seniorDevModels{
		backend: backend, sessionID: sessionID, agent: request.Agent, variant: variant,
	}
	client := newSeniorDevLLM(
		backend, sessionID, providerID, modelID, request.Agent, variant, system, ledger,
		request.RawModelCall,
	)
	client.modelRequests = request.ModelRequests
	tasks := newSeniorDevCompactionController(
		store, seniorDevSummaryClient{owner: client}, models, request.Workspace,
		backend, system, request.Tools, request.CompactionDecisions, sessionID,
	)
	loop := steploop.Loop{
		Store: store, Client: client, Models: models,
		Executor: turnToolExecutor{request: request}, Tasks: tasks,
	}
	assistant, runErr := loop.Run(ctx, steploop.RunOptions{
		SessionID: sessionID, ParentID: request.ParentSessionID,
		Workspace: request.Workspace, Worktree: request.Workspace,
		MaxSteps:        request.MaxSteps,
		Tools:           request.Tools,
		InjectReminders: turnReminderInjector(store, sessionID, request.BetweenStepReminder),
		AfterAssistant:  request.AfterAssistant,
	})
	messages, messagesErr := store.Messages(ctx, sessionID)
	result := projectTurnResult(
		sessionID, messagesSince(messages, startMessageID), ledger.snapshot(),
	)
	if assistant.Finish != nil {
		result.FinishReason = *assistant.Finish
	}
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
) (string, error) {
	if request.RawModelCall {
		return "", nil
	}
	model := systemprompt.Model{ProviderID: providerID, API: systemprompt.API{ID: modelID}}
	// The agent prompt is the whole role: there is no model-family base prompt
	// behind it, so a turn without one has nothing to say and is refused.
	agentPrompt := request.AgentMarkdown
	if !request.AgentPromptVerbatim {
		agentPrompt = baked.PromptContent(agentPrompt)
	}
	if strings.TrimSpace(agentPrompt) == "" {
		return "", fmt.Errorf("senior-dev engine: agent %q has no system prompt", request.Agent)
	}
	parts := []string{agentPrompt}
	service := systemprompt.New(turnSystemContext(ctx, request.Workspace))
	parts = append(parts, service.Environment(model)...)
	// Restricted runs say so up front, so agents plan around the missing
	// network instead of discovering it one failed command at a time.
	parts = append(parts, netpolicy.Current().EnvironmentNotice())
	parts = append(parts, instructions...)
	if instruction := attribution.CommitPromptInstruction(); instruction != "" {
		parts = append(parts, instruction)
	}
	return strings.Join(nonEmpty(parts...), "\n"), nil
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
	result := turnResult{SessionID: sessionID}
	for _, call := range calls {
		result.CostUSD += call.CostUSD
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
		result.Parts = append(result.Parts, turnPart{
			Type: "compaction", Text: summaryText,
		})
	}

	pendingActionCost := 0.0
	for index, message := range messages {
		assistant, ok := message.Info.(msgmodel.Assistant)
		if !ok {
			continue
		}
		call := assistantCalls[index]
		if index <= lastSummary || (assistant.Summary != nil && *assistant.Summary) {
			pendingActionCost += call.CostUSD
			continue
		}
		pendingActionCost += call.CostUSD
		charged := false
		for _, raw := range message.Parts {
			switch part := raw.(type) {
			case msgmodel.TextPart:
				if part.Text != "" {
					result.Parts = append(result.Parts, turnPart{
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
				toolPart := turnPart{
					Type: "tool", Tool: part.Tool, ArgsKey: args, Status: status,
				}
				if !charged && pendingActionCost != 0 {
					cost := pendingActionCost
					toolPart.CostUSD = &cost
					pendingActionCost = 0
					charged = true
				}
				result.Parts = append(result.Parts, toolPart)
			}
		}
	}
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

type modelTurnError struct {
	kind         string
	message      string
	statusCode   *uint64
	retryable    bool
	responseBody string
}

func (err *modelTurnError) Error() string {
	if err.message != "" {
		return err.message
	}
	if err.kind != "" {
		return err.kind
	}
	return "senior-dev: model turn failed"
}

func turnAssistantError(value *msgmodel.AssistantError) error {
	if value == nil {
		return nil
	}
	failure := &modelTurnError{kind: value.Name}
	if value.Name == msgmodel.ErrNameAPI {
		var data msgmodel.APIError
		if json.Unmarshal(value.Data, &data) == nil {
			failure.message = data.Message
			failure.statusCode = data.StatusCode
			failure.retryable = data.IsRetryable
			if data.ResponseBody != nil {
				failure.responseBody = *data.ResponseBody
			}
		}
	}
	var data struct {
		Message string `json:"message"`
	}
	if failure.message == "" && json.Unmarshal(value.Data, &data) == nil {
		failure.message = data.Message
	}
	return failure
}

var _ steploop.ToolExecutor = turnToolExecutor{}
