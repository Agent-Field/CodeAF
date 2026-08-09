// Effect-bound compaction orchestration ports src/session/compaction.ts:364-888.
// Concrete config/provider/plugin/session/processor services are represented by
// narrow interfaces; the state transitions and model-visible strings stay here.
package compaction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/engine/msgmodel"
	"github.com/Agent-Field/swe-pro-go/internal/engine/steploop"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/session/ledgers"
	"github.com/Agent-Field/swe-pro-go/internal/session/overflow"
)

type ConfigProvider interface {
	GetConfig(ctx context.Context) (overflow.Config, error)
}

type ConfigProviderFunc func(ctx context.Context) (overflow.Config, error)

func (f ConfigProviderFunc) GetConfig(ctx context.Context) (overflow.Config, error) {
	return f(ctx)
}

type ModelRef struct {
	ProviderID string
	ModelID    string
}

type Agent struct {
	Name  string
	Model *ModelRef
}

type AgentProvider interface {
	GetAgent(ctx context.Context, name string) (Agent, error)
}

type AgentProviderFunc func(ctx context.Context, name string) (Agent, error)

func (f AgentProviderFunc) GetAgent(ctx context.Context, name string) (Agent, error) {
	return f(ctx, name)
}

type ProviderInfo struct {
	Source  string
	Options any
}

type ModelProvider interface {
	GetModel(ctx context.Context, providerID, modelID string) (Model, error)
	GetProvider(ctx context.Context, providerID string) (ProviderInfo, error)
}

type CompactingResult struct {
	Context []string
	Prompt  *string
}

type AutoContinueInput struct {
	SessionID string
	Agent     string
	Model     Model
	Provider  ProviderInfo
	Message   msgmodel.User
	Overflow  bool
}

type Plugin interface {
	Compacting(ctx context.Context, sessionID string) (CompactingResult, error)
	TransformMessages(ctx context.Context, messages []msgmodel.WithParts) error
	AutoContinue(ctx context.Context, input AutoContinueInput) (bool, error)
}

type SummaryRequest struct {
	User      msgmodel.User
	Agent     Agent
	SessionID string
	Messages  []msgmodel.ModelMessage
	Model     Model
}

type SummaryProcessor interface {
	Process(ctx context.Context, request SummaryRequest) (steploop.Result, error)
	Message() msgmodel.Assistant
}

type ProcessorFactory interface {
	Create(
		ctx context.Context,
		assistant *msgmodel.Assistant,
		sessionID string,
		model Model,
	) (SummaryProcessor, error)
}

type ProcessorFactoryFunc func(
	ctx context.Context,
	assistant *msgmodel.Assistant,
	sessionID string,
	model Model,
) (SummaryProcessor, error)

func (f ProcessorFactoryFunc) Create(
	ctx context.Context,
	assistant *msgmodel.Assistant,
	sessionID string,
	model Model,
) (SummaryProcessor, error) {
	return f(ctx, assistant, sessionID, model)
}

type EvidenceSelector interface {
	SelectEvidence(ctx context.Context, blocks []string) (*string, error)
}

type EvidenceSelectorFunc func(ctx context.Context, blocks []string) (*string, error)

func (f EvidenceSelectorFunc) SelectEvidence(ctx context.Context, blocks []string) (*string, error) {
	return f(ctx, blocks)
}

type InstanceContext struct {
	Directory string
	Worktree  string
}

type EventSink interface {
	CompactionStarted(sessionID string, timestamp uint64, reason string)
	CompactionEnded(sessionID string, timestamp uint64, text string, include *string)
	PublishCompacted(ctx context.Context, sessionID string) error
}

type Dependencies struct {
	Store      steploop.Store
	Config     ConfigProvider
	Agents     AgentProvider
	Provider   ModelProvider
	Plugin     Plugin
	Processors ProcessorFactory
	Evidence   EvidenceSelector
	Events     EventSink
	Instance   InstanceContext

	NewID func(prefix string) string
	Now   func() uint64
	Env   func(key string) (string, bool)
}

type Service struct {
	deps Dependencies
}

type OverflowOptions struct {
	Agent *string
	Drift *float64
}

func NewService(deps Dependencies) *Service {
	if deps.NewID == nil {
		deps.NewID = defaultID
	}
	if deps.Now == nil {
		deps.Now = func() uint64 { return uint64(time.Now().UnixMilli()) }
	}
	if deps.Env == nil {
		deps.Env = lookupEnv
	}
	return &Service{deps: deps}
}

func (s *Service) EvidenceCompactionEnabled() bool {
	value, ok := s.deps.Env("CODEAF_COMPACT_EVIDENCE")
	return !ok || value != "0"
}

func EvidenceCompactionEnabled(env ...map[string]string) bool {
	if len(env) > 0 {
		return env[0]["CODEAF_COMPACT_EVIDENCE"] != "0"
	}
	value, ok := lookupEnv("CODEAF_COMPACT_EVIDENCE")
	return !ok || value != "0"
}

func (s *Service) IsOverflow(
	ctx context.Context, tokens msgmodel.Tokens, model Model, options ...OverflowOptions,
) (bool, error) {
	cfg, err := s.config(ctx)
	if err != nil {
		return false, err
	}
	var check OverflowOptions
	if len(options) > 0 {
		check = options[0]
	}
	return overflow.IsOverflow(overflow.OverflowInput{
		Cfg: cfg, Tokens: overflowTokens(tokens), Model: model.Overflow,
		Agent: check.Agent, Drift: check.Drift,
	}), nil
}

func (s *Service) ShouldScanDrift(
	ctx context.Context, tokens msgmodel.Tokens, model Model,
) (bool, error) {
	cfg, err := s.config(ctx)
	if err != nil {
		return false, err
	}
	return overflow.ShouldScanDrift(overflow.ScanDriftInput{
		Cfg: cfg, Tokens: overflowTokens(tokens), Model: model.Overflow,
	}), nil
}

func (s *Service) Estimate(
	messages []msgmodel.WithParts, model Model,
) (float64, error) {
	modelMessages, err := msgmodel.ToModelMessages(messages, model.Message, nil)
	if err != nil {
		return 0, err
	}
	raw, err := jscompat.Stringify(modelMessages)
	if err != nil {
		return 0, err
	}
	return estimateTokens(string(raw)), nil
}

func (s *Service) Select(
	ctx context.Context,
	messages []msgmodel.WithParts,
	model Model,
) (Selection, error) {
	cfg, err := s.config(ctx)
	if err != nil {
		return Selection{}, err
	}
	return selectMessages(messages, cfg, model, s.Estimate)
}

func (s *Service) Prune(ctx context.Context, sessionID string) error {
	if s.deps.Store == nil {
		return errors.New("compaction: nil store")
	}
	cfg, err := s.config(ctx)
	if err != nil {
		return err
	}
	if cfg.Compaction != nil && cfg.Compaction.Prune != nil && !*cfg.Compaction.Prune {
		return nil
	}
	messages, err := s.deps.Store.Messages(ctx, sessionID)
	if errors.Is(err, msgmodel.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	total := float64(0)
	pruned := float64(0)
	toPrune := []msgmodel.ToolPart{}
	turnCount := 0
	stop := false
	for messageIndex := len(messages) - 1; messageIndex >= 0 && !stop; messageIndex-- {
		message := messages[messageIndex]
		if _, ok := message.Info.(msgmodel.User); ok {
			turnCount++
		}
		if turnCount < 2 {
			continue
		}
		if assistant, ok := message.Info.(msgmodel.Assistant); ok && boolPointer(assistant.Summary) {
			break
		}
		for partIndex := len(message.Parts) - 1; partIndex >= 0; partIndex-- {
			part, ok := message.Parts[partIndex].(msgmodel.ToolPart)
			if !ok {
				continue
			}
			completed, ok := part.State.(msgmodel.ToolStateCompleted)
			if !ok || protectedTool(part.Tool) {
				continue
			}
			if completed.Time.Compacted != nil && *completed.Time.Compacted != 0 {
				stop = true
				break
			}
			estimate := estimateTokens(completed.Output)
			total += estimate
			if total <= PruneProtect {
				continue
			}
			pruned += estimate
			toPrune = append(toPrune, part)
		}
	}
	if pruned > PruneMinimum {
		for _, part := range toPrune {
			completed, ok := part.State.(msgmodel.ToolStateCompleted)
			if !ok {
				continue
			}
			now := s.deps.Now()
			completed.Time.Compacted = &now
			part.State = completed
			if err := s.deps.Store.UpdatePart(ctx, part); err != nil {
				return err
			}
		}
	}
	return nil
}

type ProcessInput struct {
	ParentID  string
	Messages  []msgmodel.WithParts
	SessionID string
	Auto      bool
	Overflow  *bool
}

func (s *Service) Process(ctx context.Context, input ProcessInput) (steploop.Result, error) {
	if s.deps.Store == nil || s.deps.Agents == nil ||
		s.deps.Provider == nil || s.deps.Processors == nil {
		return steploop.ResultStop, errors.New("compaction: incomplete dependencies")
	}
	var parent *msgmodel.WithParts
	for i := len(input.Messages) - 1; i >= 0; i-- {
		if input.Messages[i].Info.MessageID() == input.ParentID {
			value := input.Messages[i]
			parent = &value
			break
		}
	}
	if parent == nil {
		return steploop.ResultStop, fmt.Errorf(
			"Compaction parent must be a user message: %s", input.ParentID,
		)
	}
	userMessage, ok := parent.Info.(msgmodel.User)
	if !ok {
		return steploop.ResultStop, fmt.Errorf(
			"Compaction parent must be a user message: %s", input.ParentID,
		)
	}
	compactionPart := findCompaction(parent.Parts)
	overflowed := input.Overflow != nil && *input.Overflow
	historyChoice := selectOverflowHistory(input.Messages, input.ParentID, overflowed)
	messages := historyChoice.Messages
	replay := historyChoice.Replay

	agent, err := s.deps.Agents.GetAgent(ctx, "compaction")
	if err != nil {
		return steploop.ResultStop, err
	}
	ref := ModelRef{
		ProviderID: userMessage.Model.ProviderID, ModelID: userMessage.Model.ModelID,
	}
	if agent.Model != nil {
		ref = *agent.Model
	}
	model, err := s.deps.Provider.GetModel(ctx, ref.ProviderID, ref.ModelID)
	if err != nil {
		return steploop.ResultStop, err
	}
	cfg, err := s.config(ctx)
	if err != nil {
		return steploop.ResultStop, err
	}
	history := messages
	if compactionPart != nil && len(messages) > 0 &&
		messages[len(messages)-1].Info.MessageID() == input.ParentID {
		history = messages[:len(messages)-1]
	}
	prior := completedCompactions(history)
	hidden := map[int]bool{}
	for _, item := range prior {
		hidden[item.UserIndex] = true
		hidden[item.AssistantIndex] = true
	}
	var previousSummary *string
	if len(prior) > 0 {
		previousSummary = prior[len(prior)-1].Summary
	}
	visible := make([]msgmodel.WithParts, 0, len(history)-len(hidden))
	for i, message := range history {
		if !hidden[i] {
			visible = append(visible, message)
		}
	}
	selected, err := selectMessages(visible, cfg, model, s.Estimate)
	if err != nil {
		return steploop.ResultStop, err
	}
	compacting := CompactingResult{Context: []string{}}
	if s.deps.Plugin != nil {
		compacting, err = s.deps.Plugin.Compacting(ctx, input.SessionID)
		if err != nil {
			return steploop.ResultStop, err
		}
	}
	nextPrompt := BuildPrompt(previousSummary, compacting.Context)
	if compacting.Prompt != nil {
		nextPrompt = *compacting.Prompt
	}
	cloned, err := cloneMessages(selected.Head)
	if err != nil {
		return steploop.ResultStop, err
	}
	if s.deps.Plugin != nil {
		if err := s.deps.Plugin.TransformMessages(ctx, cloned); err != nil {
			return steploop.ResultStop, err
		}
	}
	strip := true
	maxChars := float64(ToolOutputMaxChars)
	modelMessages, err := msgmodel.ToModelMessages(
		cloned, model.Message,
		&msgmodel.ToModelOptions{StripMedia: &strip, ToolOutputMaxChars: &maxChars},
	)
	if err != nil {
		return steploop.ResultStop, err
	}
	durablePin := BuildDurableBlockerPin(ledgers.LoadOpenBlockers(s.deps.Instance.Directory))
	auditorPin := buildAuditorPin(userMessage.Agent, s.deps.Instance.Directory)
	pinnedPrompt := assemblePinnedPrompt(nextPrompt, durablePin, auditorPin)
	modelMessages = append(modelMessages, msgmodel.ModelMessage{
		Role: "user",
		Content: []msgmodel.TextContent{
			{Type: "text", Text: pinnedPrompt},
		},
	})
	assistant := msgmodel.Assistant{
		MessageBase: msgmodel.MessageBase{
			ID: s.deps.NewID("message"), SessionID: input.SessionID,
		},
		Time:       msgmodel.AssistantTime{Created: s.deps.Now()},
		ParentID:   input.ParentID,
		ModelID:    model.Message.ID,
		ProviderID: model.Message.ProviderID,
		Mode:       "compaction",
		Agent:      "compaction",
		Path: msgmodel.AssistantPath{
			Cwd: s.deps.Instance.Directory, Root: s.deps.Instance.Worktree,
		},
		Summary: boolAddress(true),
		Cost:    0,
		Tokens: msgmodel.Tokens{
			Cache: msgmodel.TokenCache{},
		},
		Variant: userMessage.Model.Variant,
	}
	if err := s.deps.Store.UpdateMessage(ctx, assistant); err != nil {
		return steploop.ResultStop, err
	}
	processor, err := s.deps.Processors.Create(ctx, &assistant, input.SessionID, model)
	if err != nil {
		return steploop.ResultStop, err
	}
	result, err := processor.Process(ctx, SummaryRequest{
		User: userMessage, Agent: agent, SessionID: input.SessionID,
		Messages: modelMessages, Model: model,
	})
	if err != nil {
		return steploop.ResultStop, err
	}
	if result == steploop.ResultCompact {
		message := processor.Message()
		errorMessage := "Session too large to compact - context exceeds model limit even after stripping media"
		if replay != nil {
			errorMessage = "Conversation history too large to compact - exceeds model context limit"
		}
		converted := msgmodel.NewContextOverflowError(msgmodel.ContextOverflowErrorData{
			Message: errorMessage,
		})
		message.Error = &converted
		finish := "error"
		message.Finish = &finish
		if err := s.deps.Store.UpdateMessage(ctx, message); err != nil {
			return steploop.ResultStop, err
		}
		return steploop.ResultStop, nil
	}

	if s.EvidenceCompactionEnabled() && s.deps.Evidence != nil {
		evidence, evidenceErr := s.deps.Evidence.SelectEvidence(
			ctx, EvidenceBlocksFromMessages(selected.Head),
		)
		if evidenceErr == nil && evidence != nil && *evidence != "" {
			if err := s.deps.Store.UpdatePart(ctx, msgmodel.TextPart{
				PartBase: msgmodel.PartBase{
					ID: s.deps.NewID("part"), MessageID: assistant.ID,
					SessionID: input.SessionID,
				},
				Text: *evidence, Synthetic: boolAddress(true),
			}); err != nil {
				return steploop.ResultStop, err
			}
		}
	}

	if compactionPart != nil && selected.TailStartID != nil &&
		(compactionPart.TailStartID == nil || *compactionPart.TailStartID != *selected.TailStartID) {
		part := *compactionPart
		part.TailStartID = selected.TailStartID
		if err := s.deps.Store.UpdatePart(ctx, part); err != nil {
			return steploop.ResultStop, err
		}
	}

	if result == steploop.ResultContinue && input.Auto {
		if replay != nil {
			if err := s.persistReplay(ctx, input.SessionID, *replay); err != nil {
				return steploop.ResultStop, err
			}
		} else {
			enabled := true
			if s.deps.Plugin != nil {
				info, err := s.deps.Provider.GetProvider(ctx, userMessage.Model.ProviderID)
				if err != nil {
					return steploop.ResultStop, err
				}
				originalModel, err := s.deps.Provider.GetModel(
					ctx, userMessage.Model.ProviderID, userMessage.Model.ModelID,
				)
				if err != nil {
					return steploop.ResultStop, err
				}
				enabled, err = s.deps.Plugin.AutoContinue(ctx, AutoContinueInput{
					SessionID: input.SessionID, Agent: userMessage.Agent,
					Model: originalModel, Provider: info,
					Message: userMessage, Overflow: overflowed,
				})
				if err != nil {
					return steploop.ResultStop, err
				}
			}
			if enabled {
				if err := s.persistAutoContinue(
					ctx, input.SessionID, userMessage, overflowed,
				); err != nil {
					return steploop.ResultStop, err
				}
			}
		}
	}

	processorMessage := processor.Message()
	if processorMessage.Error != nil {
		return steploop.ResultStop, nil
	}
	if result == steploop.ResultContinue {
		var summary *string
		fresh, err := s.deps.Store.Messages(ctx, input.SessionID)
		if err != nil {
			return steploop.ResultStop, err
		}
		for _, item := range fresh {
			if item.Info.MessageID() == assistant.ID {
				summary = summaryText(item)
				break
			}
		}
		if s.deps.Events != nil {
			text := ""
			if summary != nil {
				text = *summary
			}
			s.deps.Events.CompactionEnded(
				input.SessionID, s.deps.Now(), text, selected.TailStartID,
			)
			if err := s.deps.Events.PublishCompacted(ctx, input.SessionID); err != nil {
				return steploop.ResultStop, err
			}
		}
	}
	return result, nil
}

type CreateInput struct {
	SessionID string
	Agent     string
	Model     ModelRef
	Auto      bool
	Overflow  *bool
}

func (s *Service) Create(ctx context.Context, input CreateInput) error {
	if s.deps.Store == nil {
		return errors.New("compaction: nil store")
	}
	message := msgmodel.User{
		MessageBase: msgmodel.MessageBase{
			ID: s.deps.NewID("message"), SessionID: input.SessionID,
		},
		Time:  msgmodel.TimeCreated{Created: s.deps.Now()},
		Agent: input.Agent,
		Model: msgmodel.UserModel{
			ProviderID: input.Model.ProviderID, ModelID: input.Model.ModelID,
		},
	}
	if err := s.deps.Store.UpdateMessage(ctx, message); err != nil {
		return err
	}
	part := msgmodel.CompactionPart{
		PartBase: msgmodel.PartBase{
			ID: s.deps.NewID("part"), MessageID: message.ID,
			SessionID: input.SessionID,
		},
		Auto: input.Auto, Overflow: input.Overflow,
	}
	if err := s.deps.Store.UpdatePart(ctx, part); err != nil {
		return err
	}
	if s.deps.Events != nil {
		reason := "manual"
		if input.Auto {
			reason = "auto"
		}
		s.deps.Events.CompactionStarted(input.SessionID, s.deps.Now(), reason)
	}
	return nil
}

func (s *Service) persistReplay(
	ctx context.Context, sessionID string, replay Replay,
) error {
	message := msgmodel.User{
		MessageBase: msgmodel.MessageBase{
			ID: s.deps.NewID("message"), SessionID: sessionID,
		},
		Time:   msgmodel.TimeCreated{Created: s.deps.Now()},
		Format: replay.Info.Format,
		Agent:  replay.Info.Agent,
		Model:  replay.Info.Model,
		System: replay.Info.System,
		Tools:  replay.Info.Tools,
	}
	if err := s.deps.Store.UpdateMessage(ctx, message); err != nil {
		return err
	}
	for _, part := range buildReplayParts(
		replay, sessionID, message.ID, s.deps.NewID,
	) {
		if err := s.deps.Store.UpdatePart(ctx, part); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) persistAutoContinue(
	ctx context.Context,
	sessionID string,
	user msgmodel.User,
	overflowed bool,
) error {
	message := msgmodel.User{
		MessageBase: msgmodel.MessageBase{
			ID: s.deps.NewID("message"), SessionID: sessionID,
		},
		Time:  msgmodel.TimeCreated{Created: s.deps.Now()},
		Agent: user.Agent, Model: user.Model,
	}
	if err := s.deps.Store.UpdateMessage(ctx, message); err != nil {
		return err
	}
	partID := s.deps.NewID("part")
	start := s.deps.Now()
	end := s.deps.Now()
	return s.deps.Store.UpdatePart(ctx, msgmodel.TextPart{
		PartBase: msgmodel.PartBase{
			ID: partID, MessageID: message.ID, SessionID: sessionID,
		},
		Text:      autoContinueText(overflowed),
		Metadata:  msgmodel.RawObject(`{"compaction_continue":true}`),
		Synthetic: boolAddress(true),
		Time:      &msgmodel.TimeStartEnd{Start: start, End: &end},
	})
}

func (s *Service) config(ctx context.Context) (overflow.Config, error) {
	if s.deps.Config == nil {
		return overflow.Config{}, errors.New("compaction: nil config provider")
	}
	return s.deps.Config.GetConfig(ctx)
}

func cloneMessages(input []msgmodel.WithParts) ([]msgmodel.WithParts, error) {
	raw, err := jscompat.Stringify(input)
	if err != nil {
		return nil, err
	}
	var out []msgmodel.WithParts
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func findCompaction(parts msgmodel.Parts) *msgmodel.CompactionPart {
	for _, raw := range parts {
		if part, ok := raw.(msgmodel.CompactionPart); ok {
			return &part
		}
	}
	return nil
}

func overflowTokens(tokens msgmodel.Tokens) overflow.Tokens {
	var total *float64
	if tokens.Total != nil {
		value := float64(*tokens.Total)
		total = &value
	}
	return overflow.Tokens{
		Total: total, Input: float64(tokens.Input), Output: float64(tokens.Output),
		Reasoning: float64(tokens.Reasoning),
		Cache: overflow.TokenCache{
			Read: float64(tokens.Cache.Read), Write: float64(tokens.Cache.Write),
		},
	}
}

func protectedTool(name string) bool {
	for _, protected := range PruneProtectedTools {
		if name == protected {
			return true
		}
	}
	return false
}

func boolAddress(value bool) *bool { return &value }

var serviceID atomic.Uint64

func defaultID(prefix string) string {
	return fmt.Sprintf("%s_%016x", prefix, serviceID.Add(1))
}

func lookupEnv(key string) (string, bool) {
	return os.LookupEnv(key)
}
