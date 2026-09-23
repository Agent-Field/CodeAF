//go:build !windows

// The compaction service runs one compaction boundary end to end. Concrete
// config/provider/plugin/session/processor services are represented by narrow
// interfaces; the state transitions and model-visible strings live here.
package compaction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/jsonutil"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/overflow"
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

// ContextSizer measures the complete model-visible request represented by a
// projected message list. The senior-dev adapter includes its system prompt and
// tool schemas; the default service implementation measures messages alone.
type ContextSizer interface {
	EstimateContext(ctx context.Context, messages []msgmodel.WithParts, model Model) (float64, error)
}

type ContextSizerFunc func(
	ctx context.Context, messages []msgmodel.WithParts, model Model,
) (float64, error)

func (f ContextSizerFunc) EstimateContext(
	ctx context.Context, messages []msgmodel.WithParts, model Model,
) (float64, error) {
	return f(ctx, messages, model)
}

// CompactionDecision is the one record a boundary leaves behind: what the
// summarizer was handed (transcript and prompt sizes), what the provider
// reported back (prompt and output tokens), how the summary was judged, and
// how much verbatim tail was kept.
type CompactionDecision struct {
	SessionID string  `json:"sessionID"`
	Status    string  `json:"status"`
	Before    float64 `json:"beforeTokens"`
	After     float64 `json:"afterTokens"`
	Capacity  float64 `json:"capacityTokens"`
	Low       float64 `json:"lowTokens"`
	High      float64 `json:"highTokens"`
	// DroppedTail: the watermark rebuild removed the verbatim tail.
	// StubbedSummary: it also had to replace the summary with the capacity
	// stub because the summary block alone did not fit.
	DroppedTail    bool `json:"droppedTail"`
	StubbedSummary bool `json:"stubbedSummary,omitempty"`
	// SummaryStatus is valid, normalized, fallback, no-head (nothing to
	// summarize, no call made), overflow (the summary request itself exceeded
	// the model), summary-error (the call failed), summary-stopped, or
	// capacity-fallback (the watermark rebuild replaced the record). Every
	// status except valid and normalized installs the deterministic record.
	SummaryStatus string `json:"summaryStatus"`
	SummaryClass  string `json:"summaryClass,omitempty"`
	SummaryError  string `json:"summaryError,omitempty"`

	TranscriptMessages  int     `json:"transcriptMessages"`
	TranscriptChars     int     `json:"transcriptChars"`
	TranscriptCutChars  float64 `json:"transcriptCutChars,omitempty"`
	PromptChars         int     `json:"promptChars"`
	SummaryPromptTokens uint64  `json:"summaryPromptTokens"`
	SummaryOutputTokens uint64  `json:"summaryOutputTokens"`
	SummaryWallMs       int64   `json:"summaryWallMs"`

	TailBudget             float64 `json:"tailBudget"`
	TailMessages           int     `json:"tailMessages"`
	TailTokens             float64 `json:"tailTokens"`
	TailTruncatedOutputs   int     `json:"tailTruncatedOutputs"`
	TailTruncatedReasoning int     `json:"tailTruncatedReasoning,omitempty"`
	// PreviousSummaryCarried is set when a deterministic record carried the
	// previous boundary's summary forward verbatim.
	PreviousSummaryCarried bool `json:"previousSummaryCarried,omitempty"`
}

type DecisionSink interface {
	CompactionDecision(decision CompactionDecision)
}

type DecisionSinkFunc func(decision CompactionDecision)

func (f DecisionSinkFunc) CompactionDecision(decision CompactionDecision) { f(decision) }

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
	Sizer      ContextSizer
	Decisions  DecisionSink
	Events     EventSink
	Instance   InstanceContext
	// ChangedFiles reports the workspace's changed files as preformatted
	// lines, computed by code (a diffstat against the starting tree plus the
	// status). It is pinned beside every summary as a record the model cannot
	// misremember; nil disables the pin.
	ChangedFiles func(ctx context.Context) []string

	NewID func(prefix string) string
	Now   func() uint64
}

type Service struct {
	deps Dependencies
}

var ErrContextCapacityExhausted = errors.New("compaction: context capacity exhausted")

type ContextCapacityError struct {
	After float64
	High  float64
}

func (err ContextCapacityError) Error() string {
	return fmt.Sprintf(
		"%s: deterministic rebuild is %.0f tokens; high watermark is %.0f",
		ErrContextCapacityExhausted, err.After, err.High,
	)
}

func (ContextCapacityError) Unwrap() error { return ErrContextCapacityExhausted }

func NewService(deps Dependencies) *Service {
	if deps.NewID == nil {
		deps.NewID = defaultID
	}
	if deps.Now == nil {
		deps.Now = func() uint64 { return uint64(time.Now().UnixMilli()) }
	}
	return &Service{deps: deps}
}

func (s *Service) IsOverflow(
	ctx context.Context, tokens msgmodel.Tokens, model Model,
) (bool, error) {
	cfg, err := s.config(ctx)
	if err != nil {
		return false, err
	}
	return overflow.IsOverflow(overflow.OverflowInput{
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
	raw, err := jsonutil.Marshal(modelMessages)
	if err != nil {
		return 0, err
	}
	return estimateTokens(string(raw)), nil
}

func (s *Service) estimateContext(
	ctx context.Context, messages []msgmodel.WithParts, model Model,
) (float64, error) {
	if s.deps.Sizer != nil {
		return s.deps.Sizer.EstimateContext(ctx, messages, model)
	}
	return s.Estimate(messages, model)
}

func (s *Service) projectedContextTokens(
	ctx context.Context, sessionID string, model Model,
) (float64, error) {
	messages, err := s.deps.Store.Messages(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	newest := make([]msgmodel.WithParts, len(messages))
	for index := range messages {
		newest[len(messages)-1-index] = messages[index]
	}
	return s.estimateContext(ctx, msgmodel.FilterCompacted(newest), model)
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
	originalModel := model
	if ref.ProviderID != userMessage.Model.ProviderID || ref.ModelID != userMessage.Model.ModelID {
		originalModel, err = s.deps.Provider.GetModel(
			ctx, userMessage.Model.ProviderID, userMessage.Model.ModelID,
		)
		if err != nil {
			return steploop.ResultStop, err
		}
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
	beforeTokens, err := s.estimateContext(ctx, history, originalModel)
	if err != nil {
		return steploop.ResultStop, fmt.Errorf("compaction: measure input context: %w", err)
	}
	beforeTokens = math.Max(beforeTokens, observedContextTokens(history))
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
	// The tail first: the newest messages are kept verbatim (older tool
	// outputs truncated in the store so the tail always fits), and only what
	// precedes them is summarized.
	// The tail budget follows the same watermarks the trigger used: it is a
	// fraction of the high watermark, and it is recorded on the decision
	// beside the tail actually kept.
	budget := tailBudget(cfg, overflow.Watermarks(
		overflow.UsableInput{Cfg: cfg, Model: originalModel.Overflow},
	))
	selected, err := selectTail(visible, budget, model, s.Estimate, TailToolOutputMaxChars)
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
	pinnedPrompt := nextPrompt
	// The head is flattened into ONE user text block (SerializeTranscript):
	// data to read, with no open turn for the model to continue.
	transcript, transcriptCut := CapTranscript(
		SerializeTranscript(cloned, ToolOutputMaxChars), SummaryTranscriptMaxChars,
	)
	summaryPrompt := "<conversation>\n" + transcript + "\n</conversation>\n\n" + pinnedPrompt
	authoritativeTask, taskSource := s.authoritativeTask(messages)
	changedFiles := s.changedFiles(ctx)

	decision := CompactionDecision{
		SessionID:            input.SessionID,
		TranscriptMessages:   len(selected.Head),
		TranscriptChars:      charCount(transcript),
		TranscriptCutChars:   transcriptCut,
		PromptChars:          charCount(summaryPrompt),
		TailBudget:           budget,
		TailMessages:         selected.Messages,
		TailTokens:           selected.Tokens,
		TailTruncatedOutputs: selected.TruncatedOutputs, TailTruncatedReasoning: selected.TruncatedReasoning,
	}

	// ONE summary call. Whatever comes back, this boundary completes and the
	// tail is kept: a valid record is installed as generated, a summary-shaped
	// one is normalized, and anything else -- rejected, errored, overflowed,
	// or nothing to summarize -- gets the deterministic record, which carries
	// the previous summary forward verbatim. There are no retries and no
	// second attempt message, so there is nothing for FilterCompacted to
	// pick wrongly and no path on which the boundary destroys progress.
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
	var (
		accepted msgmodel.WithParts
		cause    error
	)
	decision.SummaryStatus = "valid"
	if strings.TrimSpace(transcript) == "" {
		decision.SummaryStatus = "no-head"
	} else {
		processor, err := s.deps.Processors.Create(ctx, &assistant, input.SessionID, model)
		if err != nil {
			return steploop.ResultStop, err
		}
		started := time.Now()
		attemptResult, err := processor.Process(ctx, SummaryRequest{
			User: userMessage, Agent: agent, SessionID: input.SessionID,
			Messages: []msgmodel.ModelMessage{msgmodel.UserText(summaryPrompt)},
			Model:    model,
		})
		if err != nil {
			return steploop.ResultStop, err
		}
		decision.SummaryWallMs = time.Since(started).Milliseconds()
		processorMessage := processor.Message()
		decision.SummaryPromptTokens = processorMessage.Tokens.Input +
			processorMessage.Tokens.Cache.Read + processorMessage.Tokens.Cache.Write
		decision.SummaryOutputTokens = processorMessage.Tokens.Output
		candidate, err := s.summaryMessage(ctx, input.SessionID, assistant.ID)
		if err != nil {
			return steploop.ResultStop, err
		}
		switch {
		case attemptResult == steploop.ResultCompact:
			decision.SummaryStatus = "overflow"
			cause = errors.New("the summary request itself exceeded the model context")
		case processorMessage.Error != nil:
			decision.SummaryStatus = "summary-error"
			cause = errors.New(assistantErrorText(processorMessage.Error))
		case attemptResult != steploop.ResultContinue:
			decision.SummaryStatus = "summary-stopped"
			cause = fmt.Errorf("summary call ended with %s", attemptResult)
		default:
			validationErr := ValidateSummary(candidate)
			if validationErr == nil {
				accepted = candidate
			} else if normalized, ok := NormalizeOffFormatSummary(candidate); ok {
				decision.SummaryStatus = "normalized"
				decision.SummaryError = validationErr.Error()
				accepted, err = s.installSummaryText(ctx, input.SessionID, candidate, normalized)
				if err != nil {
					return steploop.ResultStop, err
				}
			} else {
				decision.SummaryStatus = "fallback"
				decision.SummaryClass = ClassifySummaryFailure(candidate)
				cause = validationErr
			}
		}
	}
	if accepted.Info == nil {
		// The deterministic record. The message becomes the boundary: no
		// error, a finish reason, and a record that validates.
		if cause != nil {
			decision.SummaryError = cause.Error()
		}
		candidate, err := s.summaryMessage(ctx, input.SessionID, assistant.ID)
		if err != nil {
			return steploop.ResultStop, err
		}
		info, _ := candidate.Info.(msgmodel.Assistant)
		info.Error = nil
		finish := "stop"
		info.Finish = &finish
		if err := s.deps.Store.UpdateMessage(ctx, info); err != nil {
			return steploop.ResultStop, err
		}
		candidate.Info = info
		decision.PreviousSummaryCarried = previousSummary != nil && *previousSummary != ""
		accepted, err = s.installSummaryText(ctx, input.SessionID, candidate,
			fallbackRecord(previousSummary, authoritativeTask, cause, len(changedFiles) > 0))
		if err != nil {
			return steploop.ResultStop, err
		}
	}
	result := steploop.ResultContinue
	if authoritativeTask != "" {
		if err := s.deps.Store.UpdatePart(ctx, msgmodel.TextPart{
			PartBase: msgmodel.PartBase{
				ID: s.deps.NewID("part"), MessageID: accepted.Info.MessageID(),
				SessionID: input.SessionID,
			},
			Text:      BuildAuthoritativeTaskPin(authoritativeTask, taskSource),
			Synthetic: boolAddress(true),
			Metadata:  msgmodel.RawObject(`{"compaction_role":"authoritative_task"}`),
		}); err != nil {
			return steploop.ResultStop, err
		}
	}

	if pin := BuildChangedFilesPin(changedFiles); pin != "" {
		if err := s.deps.Store.UpdatePart(ctx, msgmodel.TextPart{
			PartBase: msgmodel.PartBase{
				ID: s.deps.NewID("part"), MessageID: accepted.Info.MessageID(),
				SessionID: input.SessionID,
			},
			Text:      pin,
			Synthetic: boolAddress(true),
			Metadata:  msgmodel.RawObject(`{"compaction_role":"changed_files"}`),
		}); err != nil {
			return steploop.ResultStop, err
		}
	}

	if s.deps.Evidence != nil {
		evidence, evidenceErr := s.deps.Evidence.SelectEvidence(
			ctx, EvidenceBlocksFromMessages(selected.Head),
		)
		if evidenceErr == nil && evidence != nil && *evidence != "" {
			if err := s.deps.Store.UpdatePart(ctx, msgmodel.TextPart{
				PartBase: msgmodel.PartBase{
					ID: s.deps.NewID("part"), MessageID: accepted.Info.MessageID(),
					SessionID: input.SessionID,
				},
				Text: *evidence, Synthetic: boolAddress(true),
				Metadata: msgmodel.RawObject(`{"compaction_role":"evidence"}`),
			}); err != nil {
				return steploop.ResultStop, err
			}
		}
	}

	// The kept tail was measured at its truncated size; make the store agree
	// before the tail is projected, so what the model sees costs what was
	// counted.
	for _, part := range selected.Truncated {
		if err := s.deps.Store.UpdatePart(ctx, part); err != nil {
			return steploop.ResultStop, err
		}
	}
	if compactionPart != nil && selected.StartID != nil &&
		(compactionPart.TailStartID == nil || *compactionPart.TailStartID != *selected.StartID) {
		part := *compactionPart
		part.TailStartID = selected.StartID
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

	capacity, capacityErr := s.enforceWatermarks(
		ctx, input.SessionID, beforeTokens, originalModel, cfg,
		compactionPart, taskSource,
	)
	decision.Status = capacity.Status
	decision.Before, decision.After = capacity.Before, capacity.After
	decision.Capacity, decision.Low, decision.High = capacity.Capacity, capacity.Low, capacity.High
	decision.DroppedTail = capacity.DroppedTail
	decision.StubbedSummary = capacity.StubbedSummary
	if capacity.StubbedSummary {
		decision.SummaryStatus = "capacity-fallback"
	}
	if s.deps.Decisions != nil {
		s.deps.Decisions.CompactionDecision(decision)
	}
	if capacityErr != nil {
		return steploop.ResultStop, capacityErr
	}

	if result == steploop.ResultContinue {
		var summary *string
		fresh, err := s.deps.Store.Messages(ctx, input.SessionID)
		if err != nil {
			return steploop.ResultStop, err
		}
		for _, item := range fresh {
			if item.Info.MessageID() == accepted.Info.MessageID() {
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
				input.SessionID, s.deps.Now(), text, selected.StartID,
			)
			if err := s.deps.Events.PublishCompacted(ctx, input.SessionID); err != nil {
				return steploop.ResultStop, err
			}
		}
	}
	return result, nil
}

func (s *Service) summaryMessage(
	ctx context.Context, sessionID, messageID string,
) (msgmodel.WithParts, error) {
	messages, err := s.deps.Store.Messages(ctx, sessionID)
	if err != nil {
		return msgmodel.WithParts{}, err
	}
	for _, message := range messages {
		if message.Info.MessageID() == messageID {
			return message, nil
		}
	}
	return msgmodel.WithParts{}, fmt.Errorf("compaction summary message not found: %s", messageID)
}

const (
	compactionStatusTarget    = "target"
	compactionStatusDegraded  = "degraded"
	compactionStatusRebuilt   = "rebuilt"
	compactionStatusExhausted = "context_capacity_exhausted"
	compactionStatusUnbounded = "unbounded"
)

func minimumContinuationHeadroom(marks overflow.CompactionWatermarks) float64 {
	gap := math.Max(0, marks.High-marks.Low)
	wanted := math.Max(2_048, math.Floor(marks.High*0.10))
	return math.Max(1, math.Min(gap, wanted))
}

func (s *Service) enforceWatermarks(
	ctx context.Context,
	sessionID string,
	before float64,
	model Model,
	cfg overflow.Config,
	compactionPart *msgmodel.CompactionPart,
	source string,
) (CompactionDecision, error) {
	marks := overflow.Watermarks(
		overflow.UsableInput{Cfg: cfg, Model: model.Overflow},
	)
	decision := CompactionDecision{
		SessionID: sessionID, Before: before,
		Capacity: marks.Capacity, Low: marks.Low, High: marks.High,
	}
	if marks.High <= 0 || math.IsInf(marks.High, 1) {
		decision.Status = compactionStatusUnbounded
		return decision, nil
	}
	after, err := s.projectedContextTokens(ctx, sessionID, model)
	if err != nil {
		return decision, fmt.Errorf("compaction: measure reconstructed context: %w", err)
	}
	decision.After = after
	headroom := minimumContinuationHeadroom(marks)
	if after <= marks.Low {
		decision.Status = compactionStatusTarget
		return decision, nil
	}
	if after < marks.High && before-after >= headroom && marks.High-after >= headroom {
		decision.Status = compactionStatusDegraded
		return decision, nil
	}

	// Stage one: drop the verbatim tail and keep the summary. The tail is
	// almost always what does not fit (typically one giant newest message),
	// and the summary is the progress record, so it is kept as long as it
	// fits on its own.
	if compactionPart != nil && compactionPart.TailStartID != nil {
		part := *compactionPart
		part.TailStartID = nil
		if err := s.deps.Store.UpdatePart(ctx, part); err != nil {
			return decision, err
		}
		decision.DroppedTail = true
		after, err = s.projectedContextTokens(ctx, sessionID, model)
		if err != nil {
			return decision, fmt.Errorf("compaction: measure tail-dropped rebuild: %w", err)
		}
		decision.After = after
		if after < marks.High && marks.High-after >= headroom {
			decision.Status = compactionStatusRebuilt
			return decision, nil
		}
	}

	// Stage two: the summary block itself does not fit. Replace it with the
	// deterministic capacity stub.
	if err := s.installCapacityFallback(
		ctx, sessionID, compactionPart, source, after, marks,
	); err != nil {
		return decision, err
	}
	decision.DroppedTail = true
	decision.StubbedSummary = true
	after, err = s.projectedContextTokens(ctx, sessionID, model)
	if err != nil {
		return decision, fmt.Errorf("compaction: measure deterministic rebuild: %w", err)
	}
	decision.After = after
	if after < marks.High && marks.High-after >= headroom {
		decision.Status = compactionStatusRebuilt
		return decision, nil
	}
	decision.Status = compactionStatusExhausted
	return decision, ContextCapacityError{After: after, High: marks.High}
}

func compactFallbackSummary(source string, observed float64, marks overflow.CompactionWatermarks) string {
	current := "- Continue the authoritative task pinned verbatim beside this state record."
	files := "- (none)"
	if source == ".senior-dev/spec.md" {
		files = "- .senior-dev/spec.md: authoritative task specification"
	} else if source == "" {
		current = "- Recover the original request from durable session state before editing."
	}
	context := fmt.Sprintf(
		"- Prior projection was %.0f tokens (target %.0f; high watermark %.0f); retained history did not fit.",
		observed, marks.Low, marks.High,
	)
	return strings.Join([]string{
		"## Working State",
		"### Completed", "- No generated completion claim survived the capacity rebuild.",
		"### Current", current,
		"### Verification", context,
		"### Next", "- Inspect the current diff and latest exact failure before editing.",
		"### Files", files,
	}, "\n")
}

func compactionRole(part msgmodel.TextPart) string {
	if len(part.Metadata) == 0 {
		return ""
	}
	var value struct {
		Role string `json:"compaction_role"`
	}
	if json.Unmarshal(part.Metadata, &value) != nil {
		return ""
	}
	return value.Role
}

func (s *Service) installCapacityFallback(
	ctx context.Context,
	sessionID string,
	compactionPart *msgmodel.CompactionPart,
	source string,
	observed float64,
	marks overflow.CompactionWatermarks,
) error {
	if compactionPart != nil {
		part := *compactionPart
		part.TailStartID = nil
		if err := s.deps.Store.UpdatePart(ctx, part); err != nil {
			return err
		}
	}

	messages, err := s.deps.Store.Messages(ctx, sessionID)
	if err != nil {
		return err
	}
	prior := completedCompactions(messages)
	if len(prior) == 0 {
		return errors.New("compaction: completed summary missing during deterministic rebuild")
	}
	message := messages[prior[len(prior)-1].AssistantIndex]
	fallback := compactFallbackSummary(source, observed, marks)
	written := false
	for _, raw := range message.Parts {
		part, ok := raw.(msgmodel.TextPart)
		if ok && !boolPointer(part.Synthetic) && !written {
			part.Text = fallback
			part.Ignored = nil
			if err := s.deps.Store.UpdatePart(ctx, part); err != nil {
				return err
			}
			written = true
			continue
		}
		if ok && compactionRole(part) == "authoritative_task" {
			continue
		}
		switch raw.(type) {
		case msgmodel.StepStartPart, msgmodel.StepFinishPart:
			continue
		}
		ignored := true
		if err := s.deps.Store.UpdatePart(ctx, msgmodel.TextPart{
			PartBase: raw.Base(), Text: "", Ignored: &ignored,
		}); err != nil {
			return err
		}
	}
	if !written {
		return errors.New("compaction: generated summary text missing during deterministic rebuild")
	}
	accepted, err := s.summaryMessage(ctx, sessionID, message.Info.MessageID())
	if err != nil {
		return err
	}
	if err := ValidateSummary(accepted); err != nil {
		return fmt.Errorf("compaction deterministic rebuild validation failed: %w", err)
	}
	return nil
}

func (s *Service) authoritativeTask(messages []msgmodel.WithParts) (string, string) {
	if s.deps.Instance.Directory != "" {
		path := filepath.Join(s.deps.Instance.Directory, ".senior-dev", "spec.md")
		if raw, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(raw)) != "" {
			return string(raw), ".senior-dev/spec.md"
		}
	}
	for _, message := range messages {
		if IsSyntheticUser(message) || hasCompaction(message.Parts) {
			continue
		}
		if _, ok := message.Info.(msgmodel.User); !ok {
			continue
		}
		parts := []string{}
		for _, raw := range message.Parts {
			part, ok := raw.(msgmodel.TextPart)
			if !ok || boolPointer(part.Ignored) || boolPointer(part.Synthetic) ||
				strings.TrimSpace(part.Text) == "" {
				continue
			}
			parts = append(parts, part.Text)
		}
		if task := strings.TrimSpace(strings.Join(parts, "\n\n")); task != "" {
			return task, "original user request"
		}
	}
	return "", ""
}

func BuildAuthoritativeTaskPin(task, source string) string {
	return strings.Join([]string{
		"# AUTHORITATIVE TASK (verbatim — durable, not generated)",
		"Source: " + source,
		"The text below is the task. It overrides any conflicting claim in the generated summary.",
		"<authoritative-task>",
		task,
		"</authoritative-task>",
	}, "\n")
}

// fallbackRecord is the deterministic state record installed when no generated
// summary is available. It never claims progress it cannot know, and it never
// loses progress either: the previous boundary's summary is carried forward
// verbatim (quoted as data), the task is pinned beside it by the caller, the
// changed files are computed by code, and the verbatim tail follows.
func fallbackRecord(previous *string, task string, cause error, filesPinned bool) string {
	completed := "- (none: no state record could be generated at this boundary)"
	if previous != nil && strings.TrimSpace(*previous) != "" {
		completed = "- No summary was generated at this boundary; the previous state record is carried forward verbatim:\n" +
			quoteAsData(HeadTailTruncate(strings.TrimSpace(*previous), 8_000))
	}
	current := "- Continue the authoritative task from the current repository state; the most recent messages are retained verbatim after this record."
	if strings.TrimSpace(task) == "" {
		current = "- Recover the original request from durable session state before editing; the most recent messages are retained verbatim after this record."
	}
	verification := "- No verification claim was retained at this boundary."
	if cause != nil {
		verification += " Cause: " + cause.Error()
	}
	files := "- (none recorded)"
	if filesPinned {
		files = "- See the CHANGED FILES record pinned beside this state."
	}
	return strings.Join([]string{
		"## Working State",
		"### Completed", completed,
		"### Current", current,
		"### Verification", verification,
		"### Next", "- Inspect the current diff and the retained recent messages before editing.",
		"### Files", files,
	}, "\n")
}

// quoteAsData prefixes every line with "> " and escapes leading heading
// markers, so quoted text can never collide with the record's own contract
// headings.
func quoteAsData(text string) string {
	quoted := make([]string, 0, strings.Count(text, "\n")+1)
	for _, line := range strings.Split(text, "\n") {
		if trimmed := strings.TrimLeft(line, " \t"); strings.HasPrefix(trimmed, "#") {
			line = strings.Replace(line, "#", `\#`, 1)
		}
		quoted = append(quoted, "> "+line)
	}
	return strings.Join(quoted, "\n")
}

// BuildChangedFilesPin renders the code-computed changed-files record that is
// pinned beside every summary: a generated summary can forget a file, a
// diffstat cannot.
func BuildChangedFilesPin(lines []string) string {
	lines = nonEmptyStrings(lines)
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(append([]string{
		"# CHANGED FILES (computed by senior-dev at this compaction, not generated)",
	}, lines...), "\n")
}

func (s *Service) changedFiles(ctx context.Context) []string {
	if s.deps.ChangedFiles == nil {
		return nil
	}
	return s.deps.ChangedFiles(ctx)
}

// assistantErrorText renders a stored assistant error for the decision event,
// bounded so a provider's HTML error page cannot flood the record.
func assistantErrorText(err *msgmodel.AssistantError) string {
	if err == nil {
		return ""
	}
	text := err.Name
	if len(err.Data) > 0 {
		text += ": " + string(err.Data)
	}
	return HeadTailTruncate(text, 600)
}

func (s *Service) installSummaryText(
	ctx context.Context,
	sessionID string,
	message msgmodel.WithParts,
	text string,
) (msgmodel.WithParts, error) {
	written := false
	for _, raw := range message.Parts {
		if part, ok := raw.(msgmodel.TextPart); ok && !written &&
			!boolPointer(part.Synthetic) {
			part.Text = text
			part.Ignored = nil
			part.Synthetic = nil
			if err := s.deps.Store.UpdatePart(ctx, part); err != nil {
				return msgmodel.WithParts{}, err
			}
			written = true
			continue
		}
		switch raw.(type) {
		case msgmodel.StepStartPart, msgmodel.StepFinishPart:
			continue
		}
		ignored := true
		if err := s.deps.Store.UpdatePart(ctx, msgmodel.TextPart{
			PartBase: raw.Base(), Text: "", Ignored: &ignored,
		}); err != nil {
			return msgmodel.WithParts{}, err
		}
	}
	if !written {
		if err := s.deps.Store.UpdatePart(ctx, msgmodel.TextPart{
			PartBase: msgmodel.PartBase{
				ID: s.deps.NewID("part"), MessageID: message.Info.MessageID(),
				SessionID: sessionID,
			},
			Text: text,
		}); err != nil {
			return msgmodel.WithParts{}, err
		}
	}
	accepted, err := s.summaryMessage(ctx, sessionID, message.Info.MessageID())
	if err != nil {
		return msgmodel.WithParts{}, err
	}
	if err := ValidateSummary(accepted); err != nil {
		return msgmodel.WithParts{}, fmt.Errorf("compaction installed summary validation failed: %w", err)
	}
	return accepted, nil
}

func nonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
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
	raw, err := jsonutil.Marshal(input)
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

func observedContextTokens(messages []msgmodel.WithParts) float64 {
	observed := float64(0)
	for _, message := range messages {
		assistant, ok := message.Info.(msgmodel.Assistant)
		if !ok {
			continue
		}
		tokens := float64(assistant.Tokens.Input + assistant.Tokens.Output +
			assistant.Tokens.Cache.Read + assistant.Tokens.Cache.Write)
		if assistant.Tokens.Total != nil && *assistant.Tokens.Total != 0 {
			tokens = float64(*assistant.Tokens.Total)
		}
		observed = math.Max(observed, tokens)
	}
	return observed
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
