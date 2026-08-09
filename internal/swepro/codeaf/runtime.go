// This file adapts the Effect services consumed by swe-pro/src/cli/cmd/run.ts:404-2899.
package codeaf

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/baked"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/bus"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/id"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/modelsdev"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/permission"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/project"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/question"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/router/adaptive"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/agentjson"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/leafoutcome"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/scheduler"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/sessioncore"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/storage"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/tool"
)

type turn struct {
	SessionID       string
	ParentSessionID string
	MessageID       string
	SessionTitle    string
	Agent           string
	AgentMarkdown   string
	// AgentPromptVerbatim marks AgentMarkdown as a configured prompt string
	// rather than a baked agent document. TS passes a configured
	// `agent.prompt` straight through (llm.ts:186); only baked documents
	// carry YAML frontmatter worth stripping.
	AgentPromptVerbatim bool
	Workspace           string
	ProviderID          string
	ModelID             string
	Variant             string
	MaxSteps            *float64
	Temperature         *float64
	DisableRetries      bool
	RawModelCall        bool
	LowModels           []string
	Prompt              string
	Reminder            string
	SystemInstructions  []string
	LoadInstructions    func(context.Context) []string
	Tools               []steploop.ToolDefinition
	Execute             func(context.Context, steploop.ToolCall) (steploop.ToolResult, error)
	BetweenStepReminder func() string
	AfterAssistant      func(context.Context, string)
	AfterTurn           func(context.Context, scheduler.LeafTurnObservation) error
	Store               steploop.Store
	Scheduler           steploop.Scheduler
	ExitGuard           steploop.ExitGuard
	PlanDB              *project.PlanDB
	PromptPersisted     bool
	PromptMessageID     string
	ManageScratch       bool
}

type turnResult struct {
	SessionID  string
	Text       string
	Parts      []scheduler.LeafPart
	Messages   []*leafoutcome.SessionMessage
	CostUSD    float64
	CallCosts  []float64
	TestPassed *bool
}

type backend interface {
	Run(context.Context, turn) (turnResult, error)
}

type runtimeAdapter struct {
	backend   backend
	registry  *tool.Registry
	config    *codeafConfig
	pool      poolResolver
	workspace string
	durable   *durableSessions
	initErr   error
	now       func() time.Time
	ids       atomic.Uint64
	mu        sync.Mutex
	sessionMu sync.Mutex
	costUSD   float64
	audits    []auditTurnObservation
	reminders *sessioncore.Service
	bus       *bus.Bus
	question  *question.Service

	unsubscribeQuestionAutoReject func()
	unsubscribeEvents             func()
}

type auditTurnObservation struct {
	executed []string
	evidence []string
}

func newRuntime(workspace string, client backend) *runtimeAdapter {
	return newConfiguredRuntime(workspace, client, nil)
}

func newConfiguredRuntime(workspace string, client backend, cfg *codeafConfig) *runtimeAdapter {
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
	// swe-pro defaults Flag.CODEAF_CLIENT to "cli". This binary is the Go CLI;
	// retain an explicit environment override for parity with the TS gate.
	if clientIdentity, ok := os.LookupEnv("CODEAF_CLIENT"); ok {
		options.ClientIdentity = clientIdentity
	} else {
		options.ClientIdentity = "cli"
	}
	options.TaskSpawner = runtime
	options.TaskSessionID = func() string { return runtime.nextID("session") }
	runtime.question = question.NewService(runtime.bus, nil)
	options.Question = runtime.question
	runtime.registry = tool.NewWithOptions(workspace, options)
	// Headless codeaf has nothing attached that could answer question.asked:
	// the pinned TS blocks such a run forever (src/question/index.ts:155-179
	// awaits a Deferred nothing resolves; src/cli/cmd/run.ts never subscribes),
	// an unbounded wall-clock hang. Deliberate divergence (BUGS-KEPT.md
	// "question tool"): auto-reject through the service's own reject path so
	// the model receives the exact TS rejection ("The user dismissed this
	// question") and the run keeps moving. The registry converts the third
	// consecutive rejection into its documented terminal success result.
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

func (runtime *runtimeAdapter) agentJSON() agentjson.Dependencies {
	return agentjson.Dependencies{
		Resolver: runtime.pool,
		Client:   runtime,
		NewID:    runtime.nextID,
	}
}

func (runtime *runtimeAdapter) Run(ctx context.Context, request agentjson.Request) error {
	ctx = rebindWorkspace(ctx, request.Workspace)
	disabled := map[string]bool{}
	for _, setting := range request.Tools {
		disabled[setting.Name] = !setting.Enabled
	}
	configured, err := runtime.config.configureTurn(turn{
		SessionID: request.SessionID, ParentSessionID: request.ParentSessionID,
		MessageID: request.MessageID, SessionTitle: request.Agent,
		Agent: request.Agent, AgentMarkdown: request.AgentMarkdown,
		Workspace: request.Workspace, ProviderID: request.Model.ProviderID,
		ModelID: request.Model.ModelID, Prompt: request.TaskPrompt,
	})
	if err != nil {
		return err
	}
	configured.LowModels = runtime.pool.values("low")
	configured.Reminder = request.Reminder
	configured.SystemInstructions = runtime.registry.SystemInstructions(ctx)
	configured.LoadInstructions = runtime.registry.SystemInstructions
	configured.Tools = runtime.definitionsFor(configured.ProviderID, configured.ModelID, request.Agent, disabled)
	configured.Execute = runtime.registry.Execute
	configured.BetweenStepReminder = request.BetweenStepReminder
	configured.AfterAssistant = runtime.registry.ClearInstructionClaims
	result, err := runtime.runTurn(ctx, configured)
	runtime.observeTurn(request.Agent, result)
	runtime.addCost(result.CostUSD)
	return err
}

func (runtime *runtimeAdapter) ResolvePromptParts(
	_ context.Context, template string,
) ([]any, error) {
	return []any{map[string]any{"type": "text", "text": template}}, nil
}

func (runtime *runtimeAdapter) Prompt(ctx context.Context, input any) (any, error) {
	fields := reflectValue(input)
	agent := stringField(fields, "Agent")
	workspace := stringField(fields, "Workspace")
	ctx = rebindWorkspace(ctx, workspace)
	providerID, modelID := nestedModel(fields)
	prompt := textFromParts(fieldInterface(fields, "Parts"))
	markdown, _ := baked.GetBakedAgent(agent)
	disabled := disabledFromToolSettings(fieldInterface(fields, "Tools"))
	configured, err := runtime.config.configureTurn(turn{
		SessionID:       stringField(fields, "SessionID"),
		ParentSessionID: stringField(fields, "ParentSessionID"),
		MessageID:       stringField(fields, "MessageID"),
		SessionTitle:    agent,
		Agent:           agent, AgentMarkdown: markdown, Workspace: workspace,
		ProviderID: providerID, ModelID: modelID, Prompt: prompt,
	})
	if err != nil {
		return nil, err
	}
	configured.LowModels = runtime.pool.values("low")
	configured.SystemInstructions = runtime.registry.SystemInstructions(ctx)
	configured.LoadInstructions = runtime.registry.SystemInstructions
	configured.Tools = runtime.definitionsFor(configured.ProviderID, configured.ModelID, agent, disabled)
	configured.Execute = runtime.registry.Execute
	configured.AfterAssistant = runtime.registry.ClearInstructionClaims
	result, err := runtime.runTurn(ctx, configured)
	runtime.observeTurn(agent, result)
	runtime.addCost(result.CostUSD)
	return result, err
}

func (runtime *runtimeAdapter) Cancel(context.Context, string) error { return nil }

func (runtime *runtimeAdapter) Create(
	ctx context.Context, parentID string, agent string,
) (string, error) {
	info, err := runtime.createSession(ctx, sessioncore.CreateInput{
		ParentID: parentID, Agent: agent, Title: agent, Directory: runtime.workspace,
	})
	return info.ID, err
}

func (runtime *runtimeAdapter) SpawnTask(
	ctx context.Context, request tool.TaskSpawnRequest,
) (tool.TaskSpawnResult, error) {
	agent := request.SubagentType
	if agent == "planner" {
		if _, err := os.Stat(filepath.Join(runtime.workspace, ".codeaf", "plan", "architecture.md")); err == nil {
			agent = "planner-translate"
		}
	}
	markdown, ok := baked.GetBakedAgent(agent)
	if !ok {
		markdown, ok = runtime.config.agent(agent)["prompt"].(string)
	}
	if !ok {
		return tool.TaskSpawnResult{}, fmt.Errorf("unknown agent type: %s is not a valid agent type", agent)
	}
	models := runtime.pool.CandidatesForTier(request.Tier)
	if len(models) == 0 {
		return tool.TaskSpawnResult{}, fmt.Errorf("task: no model in %s tier", request.Tier)
	}
	model := agentjsonModel(models[0])
	workspace := project.Directory(ctx, runtime.workspace)
	if instance, ok := project.FromContext(ctx); ok {
		instance.Directory = workspace
		if instance.PlanDB != nil && request.AssignedPlanDBTask != "" {
			instance.PlanDB.TaskID = request.AssignedPlanDBTask
		}
		ctx = project.WithContext(ctx, instance)
	}
	configured, err := runtime.config.configureTurn(turn{
		SessionID: request.SessionID, ParentSessionID: request.ParentSessionID,
		SessionTitle: request.Description + " (@" + agent + " subagent)",
		Agent:        agent, AgentMarkdown: markdown, Workspace: workspace,
		ProviderID: model.ProviderID, ModelID: model.ModelID, Prompt: request.Prompt,
	})
	if err != nil {
		return tool.TaskSpawnResult{}, err
	}
	configured.Reminder = request.PlanReminder
	configured.ManageScratch = true
	configured.LowModels = runtime.pool.values("low")
	configured.SystemInstructions = runtime.registry.SystemInstructions(ctx)
	configured.LoadInstructions = runtime.registry.SystemInstructions
	configured.Tools = runtime.definitionsFor(configured.ProviderID, configured.ModelID, agent, nil)
	configured.Execute = runtime.registry.Execute
	configured.AfterAssistant = runtime.registry.ClearInstructionClaims
	result, err := runtime.runTurn(ctx, configured)
	runtime.observeTurn(agent, result)
	runtime.addCost(result.CostUSD)
	if err != nil {
		return tool.TaskSpawnResult{}, err
	}
	return tool.TaskSpawnResult{SessionID: result.SessionID, ResultText: result.Text}, nil
}

func (runtime *runtimeAdapter) RunLeaf(
	ctx context.Context, input scheduler.LeafRunRequest,
) (scheduler.LeafRunResult, error) {
	markdown, _ := baked.GetBakedAgent(input.Agent.Name)
	input.ProviderID, input.ModelID = normalizeModelRef(input.ProviderID, input.ModelID)
	sessionID, sessionErr := runtime.leafSession(ctx, input)
	if sessionErr != nil {
		return scheduler.LeafRunResult{}, sessionErr
	}
	configured, err := runtime.config.configureTurn(turn{
		SessionID: sessionID, ParentSessionID: input.ParentSessionID,
		SessionTitle: input.SessionTitle,
		Agent:        input.Agent.Name, AgentMarkdown: markdown,
		Workspace: input.Worktree, ProviderID: input.ProviderID,
		ModelID: input.ModelID, Prompt: input.Prompt,
	})
	if err != nil {
		return scheduler.LeafRunResult{}, err
	}
	configured.ManageScratch = true
	configured.LowModels = runtime.pool.values("low")
	configured.Reminder = input.SystemReminder
	configured.SystemInstructions = runtime.registry.SystemInstructions(ctx)
	configured.LoadInstructions = runtime.registry.SystemInstructions
	configured.Tools = runtime.definitionsFor(configured.ProviderID, configured.ModelID, input.Agent.Name, nil)
	configured.Execute = runtime.registry.Execute
	configured.AfterAssistant = runtime.registry.ClearInstructionClaims
	configured.AfterTurn = input.AfterTurn
	result, err := runtime.runTurn(ctx, configured)
	runtime.observeTurn(input.Agent.Name, result)
	runtime.addCost(result.CostUSD)
	if result.SessionID == "" {
		result.SessionID = sessionID
	}
	parts := result.Parts
	if len(parts) == 0 && result.Text != "" {
		parts = []scheduler.LeafPart{{Type: "text", Text: result.Text}}
	}
	if len(parts) == 1 && parts[0].Type == "compaction" && result.Text == "" {
		// A bare compaction boundary is not successful output — let the
		// scheduler's empty-dispatch handling engage.
		parts = nil
	}
	return scheduler.LeafRunResult{
		SessionID: result.SessionID, Parts: parts,
		Messages: result.Messages, CostUSD: result.CostUSD,
		CallCosts: result.CallCosts, TestPassed: result.TestPassed,
	}, err
}

func (runtime *runtimeAdapter) runTurn(ctx context.Context, request turn) (turnResult, error) {
	if runtime.initErr != nil {
		return turnResult{}, runtime.initErr
	}
	if runtime.backend == nil {
		return turnResult{}, errors.New("codeaf runtime: backend is required")
	}
	if request.Variant == "" {
		if concrete, ok := runtime.backend.(*openRouterBackend); ok {
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
	messageID, err := persistTurnPromptWithReminders(
		ctx, runtime.durable, request.SessionID, request.MessageID, request,
		runtime.reminders,
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
	return persistTurnPromptWithReminders(ctx, store, sessionID, messageID, request, nil)
}

func persistTurnPromptWithReminders(
	ctx context.Context,
	store steploop.Store,
	sessionID string,
	messageID string,
	request turn,
	reminders *sessioncore.Service,
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
	if request.PlanDB != nil && request.PlanDB.ProjectID != "" && request.PlanDB.RootTaskID != "" {
		parts = append(parts, rootPlanDBReminder(sessionID, messageID, *request.PlanDB))
	}
	if reminders != nil {
		// TS prompt.ts:1306-1324 drains only while creating a new user
		// message, appending observer reminders after the PlanDB reminder.
		synthetic := true
		for _, text := range reminders.DrainReminders(sessionID) {
			parts = append(parts, msgmodel.TextPart{
				PartBase: msgmodel.PartBase{
					ID: steploop.NewAscendingID("prt"), SessionID: sessionID,
					MessageID: messageID,
				},
				Text: text, Synthetic: &synthetic,
			})
		}
	}
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
		return errors.New("codeaf runtime: durable sessions are unavailable")
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
		return sessioncore.Info{}, errors.New("codeaf runtime: durable sessions are unavailable")
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

func (runtime *runtimeAdapter) leafSession(
	ctx context.Context, input scheduler.LeafRunRequest,
) (string, error) {
	if input.TaskID == "" {
		info, err := runtime.createSession(ctx, sessioncore.CreateInput{
			ParentID: input.ParentSessionID, Title: input.SessionTitle,
			Agent: input.Agent.Name, Directory: input.Worktree,
			Model: sessionModel(input.ProviderID, input.ModelID, ""),
		})
		return info.ID, err
	}
	runtime.sessionMu.Lock()
	defer runtime.sessionMu.Unlock()
	key := leafSessionKey(input.ParentSessionID, input.TaskID, input.Attempt)
	var sessionID string
	if err := runtime.durable.store.ReadInto(key, &sessionID); err == nil && sessionID != "" {
		if _, getErr := runtime.durable.sessions.Get(ctx, sessionID); getErr == nil {
			return sessionID, nil
		}
		if err := runtime.durable.store.Remove(key); err != nil {
			return "", err
		}
	}
	info, err := runtime.createSession(ctx, sessioncore.CreateInput{
		ParentID: input.ParentSessionID, Title: input.SessionTitle,
		Agent: input.Agent.Name, Directory: input.Worktree,
		Model: sessionModel(input.ProviderID, input.ModelID, ""),
	})
	if err != nil {
		return "", err
	}
	claimed, err := runtime.durable.store.CreateExclusive(key, info.ID)
	if err != nil {
		return "", err
	}
	if claimed {
		return info.ID, nil
	}
	if err := runtime.durable.store.ReadInto(key, &sessionID); err != nil || sessionID == "" {
		if err == nil {
			err = errors.New("empty winning session ID")
		}
		return "", fmt.Errorf("codeaf: leaf session claim: %w", err)
	}
	if err := runtime.durable.RemoveSession(ctx, info.ID); err != nil {
		return "", err
	}
	return sessionID, nil
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

func leafSessionKey(parentSessionID, taskID string, attempt int) []string {
	digest := sha256.Sum256([]byte(taskID))
	return []string{
		"session_run", parentSessionID,
		fmt.Sprintf("%x-%s", digest[:], strconv.Itoa(attempt)),
	}
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

func (runtime *runtimeAdapter) observeTurn(agent string, result turnResult) {
	observation := auditTurnObservation{}
	for _, part := range result.Parts {
		switch part.Type {
		case "text", "compaction":
			if strings.TrimSpace(part.Text) != "" {
				observation.evidence = append(observation.evidence, part.Text)
			}
		case "tool":
			if part.Tool != "bash" {
				continue
			}
			var input struct {
				Command string `json:"command"`
			}
			if json.Unmarshal([]byte(part.ArgsKey), &input) == nil && input.Command != "" {
				observation.executed = append(observation.executed, input.Command)
				observation.evidence = append(
					observation.evidence,
					"executed `"+input.Command+"` status="+part.Status,
				)
			}
		}
	}
	if strings.TrimSpace(result.Text) != "" {
		seen := false
		for _, block := range observation.evidence {
			if block == result.Text {
				seen = true
				break
			}
		}
		if !seen {
			observation.evidence = append(observation.evidence, result.Text)
		}
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if agent == "auditor" || agent == "auditor-light" {
		runtime.audits = append(runtime.audits, observation)
	}
}

func (runtime *runtimeAdapter) latestToolEventMS(
	ctx context.Context, sessionID string,
) (*float64, error) {
	if runtime == nil || runtime.durable == nil || runtime.durable.db == nil {
		return nil, nil
	}
	rows, err := runtime.durable.db.QueryContext(ctx,
		`WITH RECURSIVE session_tree(id) AS (
			SELECT id FROM session WHERE id = ?
			UNION
			SELECT child.id FROM session child
			JOIN session_tree parent ON child.parent_id = parent.id
		)
		SELECT time_created, time_updated, data FROM part
		WHERE session_id IN (SELECT id FROM session_tree)`,
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var latest *float64
	for rows.Next() {
		var created, updated float64
		var data string
		if err := rows.Scan(&created, &updated, &data); err != nil {
			return nil, err
		}
		var projection struct {
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(data), &projection) != nil || projection.Type != msgmodel.PartTypeTool {
			continue
		}
		for _, candidate := range []float64{created, updated} {
			if latest == nil || candidate > *latest {
				value := candidate
				latest = &value
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return latest, nil
}

func (runtime *runtimeAdapter) auditObservation(index int) auditTurnObservation {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if index < 0 || index >= len(runtime.audits) {
		return auditTurnObservation{}
	}
	row := runtime.audits[index]
	row.executed = append([]string(nil), row.executed...)
	row.evidence = append([]string(nil), row.evidence...)
	return row
}

func (runtime *runtimeAdapter) auditCount() int {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return len(runtime.audits)
}

func reflectValue(value any) reflect.Value {
	result := reflect.ValueOf(value)
	for result.IsValid() && result.Kind() == reflect.Pointer {
		if result.IsNil() {
			return reflect.Value{}
		}
		result = result.Elem()
	}
	return result
}

func fieldInterface(value reflect.Value, name string) any {
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return nil
	}
	field := value.FieldByName(name)
	if !field.IsValid() || !field.CanInterface() {
		return nil
	}
	return field.Interface()
}

func stringField(value reflect.Value, name string) string {
	field := fieldInterface(value, name)
	text, _ := field.(string)
	return text
}

func nestedModel(value reflect.Value) (string, string) {
	model := reflectValue(fieldInterface(value, "Model"))
	return stringField(model, "ProviderID"), stringField(model, "ModelID")
}

func textFromParts(value any) string {
	parts, ok := value.([]any)
	if !ok {
		encoded, _ := json.Marshal(value)
		return string(encoded)
	}
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		partValue := reflectValue(part)
		if partValue.IsValid() && partValue.Kind() == reflect.Struct {
			if text := stringField(partValue, "Text"); text != "" {
				lines = append(lines, text)
				continue
			}
		}
		if object, ok := part.(map[string]any); ok {
			if text, ok := object["text"].(string); ok {
				lines = append(lines, text)
				continue
			}
		}
		encoded, _ := json.Marshal(part)
		lines = append(lines, string(encoded))
	}
	return strings.Join(lines, "\n\n")
}

func disabledFromToolSettings(value any) map[string]bool {
	out := map[string]bool{}
	reflected := reflectValue(value)
	if !reflected.IsValid() || reflected.Kind() != reflect.Struct {
		return out
	}
	typ := reflected.Type()
	for index := 0; index < reflected.NumField(); index++ {
		field := reflected.Field(index)
		if field.Kind() != reflect.Bool || field.Bool() {
			continue
		}
		name := typ.Field(index).Tag.Get("json")
		name, _, _ = strings.Cut(name, ",")
		if name != "" && name != "-" {
			out[name] = true
		}
	}
	return out
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
	for name := range runtime.config.disabledTools(agentName, runtime.registry.IDs()) {
		disabled[name] = true
	}
	definitions := tool.FilterDefinitions(runtime.registry.Definitions(), tool.FilterInput{
		ProviderID: providerID,
		ModelID:    modelID,
		AgentName:  agentName,
		Flags:      tool.CurrentWebSearchFlags(),
	})
	return filterTools(definitions, disabled)
}

type poolResolver struct {
	high     []string
	low      []string
	frontier []string
	fallback bool
}

func (resolver poolResolver) values(tier string) []string {
	switch tier {
	case "low":
		if len(resolver.low) > 0 {
			return append([]string(nil), resolver.low...)
		}
	case "frontier":
		if len(resolver.frontier) > 0 {
			return append([]string(nil), resolver.frontier...)
		}
	}
	if len(resolver.high) > 0 {
		return append([]string(nil), resolver.high...)
	}
	if resolver.fallback {
		return []string{"openrouter/fake/offline"}
	}
	return nil
}

func (resolver poolResolver) CandidatesForTier(tier baked.Tier) []string {
	return resolver.values(string(tier))
}

type phaseModels struct{ pool poolResolver }

func (models phaseModels) CandidatesForTier(tier string) []string {
	return models.pool.values(tier)
}

type schedulerPools struct{ pool poolResolver }

func (pools schedulerPools) CandidatesForTier(
	tier scheduler.ModelTier,
) []scheduler.ModelCandidate {
	values := pools.pool.values(string(tier))
	out := make([]scheduler.ModelCandidate, 0, len(values))
	for _, value := range values {
		out = append(out, scheduler.ModelCandidate{ID: value})
	}
	return out
}

type schedulerProvider struct{}

type bakedAgentRegistry struct{ config *codeafConfig }

func (registry bakedAgentRegistry) Get(_ context.Context, name string) (*scheduler.AgentInfo, error) {
	markdown, ok := baked.GetBakedAgentMarkdown(name)
	if !ok && len(registry.config.agent(name)) == 0 {
		return nil, fmt.Errorf("unknown baked agent: %s", name)
	}
	rules := registry.config.rulesForAgent(name)
	if registry.config == nil {
		var err error
		rules, err = permission.RulesetFromFrontmatter(markdown)
		if err != nil {
			return nil, err
		}
	}
	projected := make([]scheduler.PermissionRule, len(rules))
	for index, rule := range rules {
		projected[index] = scheduler.PermissionRule{
			Permission: rule.Permission, Pattern: rule.Pattern, Action: rule.Action,
		}
	}
	info := &scheduler.AgentInfo{Name: name, Permission: projected}
	if configured, ok := registry.config.agent(name)["model"].(string); ok {
		providerID, modelID := splitConfiguredModel(configured)
		info.Model = scheduler.ProviderModel{ProviderID: providerID, ID: modelID, ModelID: modelID}
	}
	return info, nil
}

func (schedulerProvider) GetModel(
	_ context.Context, providerID, modelID string,
) (any, error) {
	return scheduler.ProviderModel{ProviderID: providerID, ID: modelID}, nil
}

func (schedulerProvider) GetLanguage(_ context.Context, model any) (any, error) {
	return resolveRuntimeLanguage(model)
}

type openRouterBackend struct {
	apiKey         string
	variant        string
	client         *http.Client
	endpoint       string
	logf           func(string, ...any)
	sleep          func(context.Context, time.Duration) error
	contextLimit   float64
	outputLimit    float64
	totalTimeoutMS float64
	chunkTimeoutMS float64
	config         *codeafConfig
	router         *adaptive.AdaptiveModelRouter
	catalog        modelsdev.Catalog
}

const (
	maxLeafCompactions = 3
)

func carryLeafObservationCost(
	observations []*leafoutcome.SessionMessage, cost float64,
) {
	if cost == 0 || len(observations) == 0 {
		return
	}
	last := observations[len(observations)-1]
	if last == nil || last.Info == nil {
		return
	}
	if last.Info.Cost == nil {
		value := jscompat.JSNumber(cost)
		last.Info.Cost = &value
		return
	}
	value := jscompat.JSNumber(float64(*last.Info.Cost) + cost)
	last.Info.Cost = &value
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

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func projectContext(ctx context.Context, workspace, dbPath, projectID string) context.Context {
	return project.WithContext(ctx, project.InstanceContext{
		Directory: workspace, Worktree: workspace,
		Project: project.Info{ID: project.ID(projectID), Worktree: workspace},
		PlanDB:  &project.PlanDB{DBPath: dbPath, ProjectID: projectID},
	})
}

func rebindWorkspace(ctx context.Context, workspace string) context.Context {
	if workspace == "" {
		return ctx
	}
	instance, ok := project.FromContext(ctx)
	if !ok {
		return project.WithContext(ctx, project.InstanceContext{
			Directory: workspace, Worktree: workspace,
			Project: project.Info{Worktree: workspace},
		})
	}
	instance.Directory = workspace
	if instance.Worktree == "" {
		instance.Worktree = workspace
	}
	return project.WithContext(ctx, instance)
}

func defaultBackend(variant string) backend {
	endpoint := ""
	if base := os.Getenv("OPENROUTER_BASE_URL"); base != "" {
		endpoint = openRouterEndpoint(base)
	}
	return &openRouterBackend{
		apiKey: os.Getenv("OPENROUTER_API_KEY"), variant: variant,
		endpoint: endpoint,
		client:   &http.Client{Timeout: 35 * time.Minute},
		logf: func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, "[codeaf] %s\n", fmt.Sprintf(format, args...))
		},
	}
}

func openRouterEndpoint(base string) string {
	base = strings.TrimRight(base, "/")
	base = strings.TrimSuffix(base, "/api/v1")
	return base + "/api/v1/chat/completions"
}
