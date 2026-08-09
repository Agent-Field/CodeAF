package steploop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/calc"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/orclient"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	sessionretry "github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/retry"
)

// ProcessorOptions are processor.create's inputs plus the two unbuilt seams.
type ProcessorOptions struct {
	Store       Store
	Assistant   msgmodel.Assistant
	Model       Model
	Tools       []ToolDefinition
	Executor    ToolExecutor
	WaitTimeout time.Duration
}

type toolCallState struct {
	part msgmodel.ToolPart
	done chan struct{}
	once sync.Once
	// dispatched marks that an executor was actually launched for this call.
	// A registration whose tool-call part never arrived (length-truncated or
	// errored stream) has no executor and can never settle itself — cleanup
	// must keep the bounded drain for those (TS settles them the same way).
	dispatched bool
}

// Processor persists one assistant turn and owns its in-flight tool registry.
type Processor struct {
	store     Store
	message   msgmodel.Assistant
	model     Model
	tools     []ToolDefinition
	executor  ToolExecutor
	wait      time.Duration
	sessionID string

	mu             sync.Mutex
	toolCalls      map[string]*toolCallState
	toolOrder      []string
	current        *msgmodel.TextPart
	reasoning      map[string]msgmodel.ReasoningPart
	reasoningOrder []string
	streamAborted  bool
}

func NewProcessor(opts ProcessorOptions) *Processor {
	wait := opts.WaitTimeout
	if wait <= 0 {
		wait = 250 * time.Millisecond
	}
	return &Processor{
		store:     opts.Store,
		message:   opts.Assistant,
		model:     opts.Model,
		tools:     append([]ToolDefinition(nil), opts.Tools...),
		executor:  opts.Executor,
		wait:      wait,
		sessionID: opts.Assistant.SessionID,
		toolCalls: map[string]*toolCallState{},
		reasoning: map[string]msgmodel.ReasoningPart{},
	}
}

// Message returns a copy of the current assistant message.
func (p *Processor) Message() msgmodel.Assistant {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.message
}

// UpdateToolCall is processor.ts:162-176. The callback runs while the
// processor lock is held.
func (p *Processor) UpdateToolCall(ctx context.Context, toolCallID string, update func(msgmodel.ToolPart) msgmodel.ToolPart) (*msgmodel.ToolPart, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	call := p.toolCalls[toolCallID]
	if call == nil {
		return nil, nil
	}
	part := update(call.part)
	if err := p.store.UpdatePart(ctx, part); err != nil {
		return nil, err
	}
	call.part = part
	copy := part
	return &copy, nil
}

// CompleteToolCall is processor.ts:178-202. A non-running/missing call is a
// no-op and is not settled.
func (p *Processor) CompleteToolCall(ctx context.Context, toolCallID string, output ToolResult) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	call := p.toolCalls[toolCallID]
	if call == nil || call.part.State == nil || call.part.State.ToolStatus() != msgmodel.ToolStatusRunning {
		return nil
	}
	// Settle on every exit past the guard: execute() discards these errors,
	// and an unsettled dispatched call would hang cleanup's unbounded wait.
	defer p.settleLocked(toolCallID)
	end := currentNow()
	start, _ := call.part.State.StartTime()
	metadata := output.Metadata
	if !msgmodel.IsRecord(metadata) {
		metadata = msgmodel.RawObject("{}")
	}
	state, err := completedState(call.part.State.ToolInput(), output, metadata, start, end)
	if err != nil {
		return err
	}
	call.part.State = state
	return p.store.UpdatePart(ctx, call.part)
}

// FailToolCall is processor.ts:204-221. A non-running/missing call is a no-op.
func (p *Processor) FailToolCall(ctx context.Context, toolCallID string, failure error) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	call := p.toolCalls[toolCallID]
	if call == nil || call.part.State == nil || call.part.State.ToolStatus() != msgmodel.ToolStatusRunning {
		return false, nil
	}
	// Settle on every exit past the guard — see CompleteToolCall.
	defer p.settleLocked(toolCallID)
	message := "null"
	if failure != nil {
		message = failure.Error()
	}
	end := currentNow()
	start, _ := call.part.State.StartTime()
	state, err := errorState(call.part.State.ToolInput(), message, start, end)
	if err != nil {
		return false, err
	}
	call.part.State = state
	if err := p.store.UpdatePart(ctx, call.part); err != nil {
		return false, err
	}
	return true, nil
}

func (p *Processor) settleLocked(toolCallID string) {
	call := p.toolCalls[toolCallID]
	delete(p.toolCalls, toolCallID) // delete BEFORE resolving Deferred.
	if call != nil {
		call.once.Do(func() { close(call.done) })
	}
}

// Process drains one provider stream, synthesizing start-step/finish-step and
// tool-result/error events around orclient's lower-level parts.
func (p *Processor) Process(ctx context.Context, stream PartStream) (result Result, err error) {
	result = ResultContinue
	p.mu.Lock()
	p.streamAborted = true
	p.mu.Unlock()
	if stream == nil {
		return ResultStop, errors.New("steploop: nil stream")
	}
	defer stream.Close()
	defer func() {
		cleanupErr := p.cleanup(ctx)
		if err == nil && cleanupErr != nil {
			err = cleanupErr
		}
	}()

	if err = p.persistPart(ctx, msgmodel.StepStartPart{
		PartBase: msgmodel.PartBase{ID: nextID("prt"), SessionID: p.sessionID, MessageID: p.message.ID},
	}); err != nil {
		return ResultStop, err
	}

	for {
		var part orclient.StreamPart
		part, err = stream.Next()
		if err == io.EOF {
			p.mu.Lock()
			p.streamAborted = false
			p.mu.Unlock()
			return result, nil
		}
		if err != nil {
			if sessionretry.IsContextOverflow(sessionretry.FromError(err)) {
				return ResultCompact, nil
			}
			p.setAssistantError(err)
			return ResultStop, nil
		}
		switch value := part.(type) {
		case orclient.ReasoningStartPart:
			err = p.reasoningStart(ctx, value)
		case orclient.ReasoningDeltaPart:
			err = p.reasoningDelta(ctx, value)
		case orclient.ReasoningEndPart:
			err = p.reasoningEnd(ctx, value)
		case orclient.TextStartPart:
			err = p.textStart(ctx)
		case orclient.TextDeltaPart:
			err = p.textDelta(ctx, value)
		case orclient.TextEndPart:
			err = p.textEnd(ctx)
		case orclient.ToolInputStartPart:
			err = p.toolInputStart(ctx, value)
		case orclient.ToolCallPart:
			err = p.toolCall(ctx, value)
		case orclient.ErrorPart:
			if sessionretry.IsContextOverflow(sessionretry.FromStreamError(value.Error)) {
				return ResultCompact, nil
			}
			p.setAssistantError(errors.New(errorPartMessage(value.Error)))
			return ResultStop, nil
		case orclient.AbortPart:
			message := "Aborted"
			if value.HasReason && value.Reason != "" {
				message = value.Reason
			}
			p.mu.Lock()
			abort := msgmodel.NewMessageAbortedError(message)
			p.message.Error = &abort
			p.mu.Unlock()
			return ResultStop, nil
		case orclient.FinishPart:
			err = p.finish(ctx, value)
		}
		if err != nil {
			return ResultStop, err
		}
	}
}

func (p *Processor) persistPart(ctx context.Context, part msgmodel.Part) error {
	return p.store.UpdatePart(ctx, part)
}

func (p *Processor) reasoningStart(ctx context.Context, value orclient.ReasoningStartPart) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.reasoning[value.ID]; exists {
		return nil
	}
	part := msgmodel.ReasoningPart{
		PartBase: msgmodel.PartBase{ID: nextID("prt"), SessionID: p.sessionID, MessageID: p.message.ID},
		Text:     "",
		Time:     msgmodel.TimeStartEnd{Start: currentNow()},
	}
	p.reasoning[value.ID] = part
	p.reasoningOrder = append(p.reasoningOrder, value.ID)
	return p.store.UpdatePart(ctx, part)
}

func (p *Processor) reasoningDelta(ctx context.Context, value orclient.ReasoningDeltaPart) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	part, exists := p.reasoning[value.ID]
	if !exists {
		return nil
	}
	part.Text += value.Delta
	p.reasoning[value.ID] = part
	p.publishPartDelta(ctx, part.PartBase, value.Delta)
	return nil
}

func (p *Processor) reasoningEnd(ctx context.Context, value orclient.ReasoningEndPart) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	part, exists := p.reasoning[value.ID]
	if !exists {
		return nil
	}
	end := currentNow()
	part.Time.End = &end
	part.Metadata = reasoningMetadata(value.Details)
	delete(p.reasoning, value.ID)
	return p.store.UpdatePart(ctx, part)
}

func (p *Processor) textStart(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	part := msgmodel.TextPart{
		PartBase: msgmodel.PartBase{ID: nextID("prt"), SessionID: p.sessionID, MessageID: p.message.ID},
		Text:     "",
		Time:     &msgmodel.TimeStartEnd{Start: currentNow()},
	}
	p.current = &part
	return p.store.UpdatePart(ctx, part)
}

func (p *Processor) textDelta(ctx context.Context, value orclient.TextDeltaPart) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.current == nil {
		return nil
	}
	p.current.Text += value.Delta
	p.publishPartDelta(ctx, p.current.PartBase, value.Delta)
	return nil
}

func (p *Processor) publishPartDelta(ctx context.Context, base msgmodel.PartBase, delta string) {
	store, ok := p.store.(interface {
		UpdatePartDelta(context.Context, msgmodel.PartDeltaEvent)
	})
	if !ok {
		return
	}
	store.UpdatePartDelta(ctx, msgmodel.PartDeltaEvent{
		SessionID: base.SessionID,
		MessageID: base.MessageID,
		PartID:    base.ID,
		Field:     "text",
		Delta:     delta,
	})
}

func (p *Processor) textEnd(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.current == nil {
		return nil
	}
	end := currentNow()
	if p.current.Time == nil {
		p.current.Time = &msgmodel.TimeStartEnd{Start: end}
	}
	p.current.Time.End = &end
	err := p.store.UpdatePart(ctx, *p.current)
	p.current = nil
	return err
}

func (p *Processor) toolInputStart(ctx context.Context, value orclient.ToolInputStartPart) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	partID := nextID("prt")
	if previous := p.toolCalls[value.ID]; previous != nil {
		partID = previous.part.ID
	}
	part := msgmodel.ToolPart{
		PartBase: msgmodel.PartBase{ID: partID, SessionID: p.sessionID, MessageID: p.message.ID},
		CallID:   value.ID,
		Tool:     value.ToolName,
		State:    msgmodel.PendingToolState(),
	}
	if err := p.store.UpdatePart(ctx, part); err != nil {
		return err
	}
	if _, exists := p.toolCalls[value.ID]; !exists {
		p.toolOrder = append(p.toolOrder, value.ID)
	}
	p.toolCalls[value.ID] = &toolCallState{part: part, done: make(chan struct{})}
	return nil
}

func (p *Processor) toolCall(ctx context.Context, value orclient.ToolCallPart) error {
	specs := make([]orclient.ToolSpec, 0, len(p.tools)+1)
	for _, tool := range p.tools {
		specs = append(specs, orclient.ToolSpec{Name: tool.Provider.Name, Validate: tool.Validate})
	}
	specs = append(specs, orclient.ToolSpec{Name: orclient.InvalidToolName})
	// llm.ts:308 sorts the full map (including invalid) before parsing calls;
	// activeTools later hides invalid from the provider while retaining it as
	// the fallback execution target (llm.ts:427-452).
	parsed := orclient.ParseToolCall(orclient.RawToolCall{
		ToolCallID: value.ToolCallID,
		ToolName:   value.ToolName,
		Input:      value.Input,
	}, orclient.SortedToolMap(specs...), orclient.CodeafRepairToolCall)

	_, err := p.UpdateToolCall(ctx, value.ToolCallID, func(part msgmodel.ToolPart) msgmodel.ToolPart {
		start := currentNow()
		state, stateErr := runningState(part.State, msgmodel.RawObject(parsed.Input), start)
		if stateErr == nil {
			part.State = state
		}
		part.Tool = parsed.ToolName
		if value.HasProviderMetadata {
			part.Metadata = reasoningMetadata(value.Details)
		}
		return part
	})
	if err != nil {
		return err
	}
	if parsed.Invalid {
		_, err = p.FailToolCall(ctx, value.ToolCallID, parsed.Error)
		return err
	}
	if parsed.ToolName == orclient.InvalidToolName {
		return p.CompleteToolCall(ctx, value.ToolCallID, ToolResult{
			Title: "Invalid Tool",
			Output: "The arguments provided to the tool are invalid: " +
				invalidToolMessage(parsed.Input),
			Metadata: msgmodel.RawObject("{}"),
		})
	}
	if p.executor == nil {
		// The real SDK would have no execute callback and leave the call
		// incomplete; cleanup performs the same forced-abort settlement.
		return nil
	}
	modelID := p.model.Message.API.ID
	if modelID == "" {
		modelID = p.model.Message.ID
	}
	call := ToolCall{
		ID:        parsed.ToolCallID,
		Name:      parsed.ToolName,
		Input:     append(json.RawMessage(nil), parsed.Input...),
		SessionID: p.sessionID,
		MessageID: p.message.ID,
		Agent:     p.message.Agent,
		ModelID:   modelID,
	}
	execute := func() {
		messages, messagesErr := p.store.Messages(ctx, p.sessionID)
		if messagesErr != nil {
			_, _ = p.FailToolCall(ctx, call.ID, messagesErr)
			return
		}
		executeCtx := WithToolMessages(ctx, msgmodel.FilterCompacted(newestFirst(messages)))
		output, executeErr := p.executor.Execute(executeCtx, call)
		if executeErr != nil {
			_, _ = p.FailToolCall(ctx, call.ID, executeErr)
			return
		}
		_ = p.CompleteToolCall(ctx, call.ID, output)
	}
	p.mu.Lock()
	if state := p.toolCalls[call.ID]; state != nil {
		state.dispatched = true
	}
	p.mu.Unlock()
	for _, definition := range p.tools {
		if definition.Provider.Name == call.Name && definition.WaitForResult {
			execute()
			return nil
		}
	}
	go execute()
	return nil
}

func (p *Processor) finish(ctx context.Context, value orclient.FinishPart) error {
	usage := calc.GetUsage(calc.GetUsageInput{
		Model: p.model.Calc,
		Usage: calc.AsLanguageModelUsage(value.Usage),
	})
	finish := value.FinishReason.Unified
	tokens := messageTokens(usage.Tokens)
	part := msgmodel.StepFinishPart{
		PartBase: msgmodel.PartBase{ID: nextID("prt"), SessionID: p.sessionID, MessageID: p.message.ID},
		Reason:   finish,
		Cost:     jscompat.JSNumber(usage.Cost),
		Tokens:   tokens,
	}
	if err := p.store.UpdatePart(ctx, part); err != nil {
		return err
	}
	p.mu.Lock()
	p.message.Finish = &finish
	p.message.Cost = jscompat.JSNumber(float64(p.message.Cost) + usage.Cost)
	p.message.Tokens = tokens
	message := p.message
	p.mu.Unlock()
	return p.store.UpdateMessage(ctx, message)
}

func (p *Processor) setAssistantError(err error) {
	message := ""
	if err != nil {
		message = err.Error()
	}
	p.mu.Lock()
	value := msgmodel.NewUnknownError(message)
	p.message.Error = &value
	p.mu.Unlock()
}

func (p *Processor) cleanup(ctx context.Context) error {
	p.mu.Lock()
	streamAborted := p.streamAborted
	if p.current != nil {
		end := currentNow()
		if p.current.Time == nil {
			p.current.Time = &msgmodel.TimeStartEnd{Start: end}
		}
		p.current.Time.End = &end
		if err := p.store.UpdatePart(ctx, *p.current); err != nil {
			p.mu.Unlock()
			return err
		}
		p.current = nil
	}
	for _, id := range p.reasoningOrder {
		part, exists := p.reasoning[id]
		if !exists {
			continue
		}
		end := currentNow()
		part.Time.End = &end
		if err := p.store.UpdatePart(ctx, part); err != nil {
			p.mu.Unlock()
			return err
		}
		delete(p.reasoning, id)
	}
	p.reasoningOrder = nil
	type pendingSettle struct {
		done       <-chan struct{}
		dispatched bool
	}
	waiting := make([]pendingSettle, 0, len(p.toolCalls))
	for _, id := range p.toolOrder {
		if call := p.toolCalls[id]; call != nil {
			waiting = append(waiting, pendingSettle{done: call.done, dispatched: call.dispatched})
		}
	}
	p.mu.Unlock()

	// The TS AI SDK executes tools inside streamText, so its stream cannot end
	// before a launched execute() resolves — a dispatched call on a normally
	// drained turn waits unbounded, with the 250ms grace applying only once ctx
	// is cancelled. Every abnormal stream end aborts that scoped stream, so all
	// calls use the bounded drain. A registration that never got its tool-call
	// part has no executor to settle it and always uses the bounded drain.
	var group sync.WaitGroup
	for _, wait := range waiting {
		wait := wait
		group.Add(1)
		go func() {
			defer group.Done()
			if wait.dispatched && !streamAborted {
				select {
				case <-wait.done:
					return
				case <-ctx.Done():
				}
			}
			timer := time.NewTimer(p.wait)
			defer timer.Stop()
			select {
			case <-wait.done:
			case <-timer.C:
			}
		}()
	}
	group.Wait()

	p.mu.Lock()
	for _, id := range p.toolOrder {
		call := p.toolCalls[id]
		if call == nil {
			continue
		}
		end := currentNow()
		raw, spreadErr := msgmodel.SpreadAbortedToolState(call.part.State, end)
		if spreadErr != nil {
			p.mu.Unlock()
			return spreadErr
		}
		call.part.State = newRawToolState(raw)
		if updateErr := p.store.UpdatePart(ctx, call.part); updateErr != nil {
			p.mu.Unlock()
			return updateErr
		}
	}
	p.toolCalls = map[string]*toolCallState{}
	p.toolOrder = nil
	completed := currentNow()
	p.message.Time.Completed = &completed
	message := p.message
	p.mu.Unlock()
	return p.store.UpdateMessage(ctx, message)
}

func messageTokens(tokens calc.UsageTokens) msgmodel.Tokens {
	var total *uint64
	if tokens.Total != nil {
		value := safeUint(*tokens.Total)
		total = &value
	}
	return msgmodel.Tokens{
		Total:     total,
		Input:     safeUint(tokens.Input),
		Output:    safeUint(tokens.Output),
		Reasoning: safeUint(tokens.Reasoning),
		Cache: msgmodel.TokenCache{
			Read:  safeUint(tokens.Cache.Read),
			Write: safeUint(tokens.Cache.Write),
		},
	}
}

func safeUint(value float64) uint64 {
	if math.IsNaN(value) || value <= 0 {
		return 0
	}
	if math.IsInf(value, 1) || value >= math.MaxUint64 {
		return math.MaxUint64
	}
	return uint64(value)
}

// rawToolState carries TS object-spread leaks that the schema-clean msgmodel
// variants intentionally cannot represent.
type rawToolState struct {
	raw      json.RawMessage
	status   string
	input    msgmodel.RawObject
	metadata msgmodel.RawObject
	start    uint64
	hasStart bool
}

func newRawToolState(raw json.RawMessage) rawToolState {
	state := rawToolState{raw: append(json.RawMessage(nil), raw...)}
	var probe struct {
		Status   string             `json:"status"`
		Input    msgmodel.RawObject `json:"input"`
		Metadata msgmodel.RawObject `json:"metadata"`
		Time     *struct {
			Start uint64 `json:"start"`
		} `json:"time"`
	}
	_ = json.Unmarshal(raw, &probe)
	state.status = probe.Status
	state.input = probe.Input
	state.metadata = probe.Metadata
	if probe.Time != nil {
		state.start = probe.Time.Start
		state.hasStart = true
	}
	return state
}

func (s rawToolState) ToolStatus() string               { return s.status }
func (s rawToolState) ToolInput() msgmodel.RawObject    { return s.input }
func (s rawToolState) ToolMetadata() msgmodel.RawObject { return s.metadata }
func (s rawToolState) StartTime() (uint64, bool)        { return s.start, s.hasStart }
func (s rawToolState) MarshalJSON() ([]byte, error)     { return append([]byte(nil), s.raw...), nil }

func runningState(previous msgmodel.ToolState, input msgmodel.RawObject, start uint64) (rawToolState, error) {
	status, _ := jscompat.Stringify(msgmodel.ToolStatusRunning)
	timeRaw, err := jscompat.Stringify(msgmodel.ToolTimeStart{Start: start})
	if err != nil {
		return rawToolState{}, err
	}
	raw, err := msgmodel.SpreadToolState(previous,
		msgmodel.RawField{Key: "status", Value: status},
		msgmodel.RawField{Key: "input", Value: json.RawMessage(input)},
		msgmodel.RawField{Key: "time", Value: timeRaw},
	)
	if err != nil {
		return rawToolState{}, err
	}
	return newRawToolState(raw), nil
}

func completedState(input msgmodel.RawObject, output ToolResult, metadata msgmodel.RawObject, start, end uint64) (rawToolState, error) {
	status, _ := jscompat.Stringify(msgmodel.ToolStatusCompleted)
	outputRaw, _ := jscompat.Stringify(output.Output)
	titleRaw, _ := jscompat.Stringify(output.Title)
	timeRaw, err := jscompat.Stringify(msgmodel.ToolTimeCompleted{Start: start, End: end})
	if err != nil {
		return rawToolState{}, err
	}
	fields := []msgmodel.RawField{
		{Key: "status", Value: status},
		{Key: "input", Value: json.RawMessage(input)},
		{Key: "output", Value: outputRaw},
		{Key: "metadata", Value: json.RawMessage(metadata)},
		{Key: "title", Value: titleRaw},
		{Key: "time", Value: timeRaw},
	}
	if output.Attachments != nil {
		attachments, marshalErr := jscompat.Stringify(*output.Attachments)
		if marshalErr != nil {
			return rawToolState{}, marshalErr
		}
		fields = append(fields, msgmodel.RawField{Key: "attachments", Value: attachments})
	}
	return newRawToolState(msgmodel.SpreadObject(nil, fields...)), nil
}

func errorState(input msgmodel.RawObject, message string, start, end uint64) (rawToolState, error) {
	status, _ := jscompat.Stringify(msgmodel.ToolStatusError)
	messageRaw, _ := jscompat.Stringify(message)
	timeRaw, err := jscompat.Stringify(msgmodel.ToolTimeSpan{Start: start, End: end})
	if err != nil {
		return rawToolState{}, err
	}
	return newRawToolState(msgmodel.SpreadObject(nil,
		msgmodel.RawField{Key: "status", Value: status},
		msgmodel.RawField{Key: "input", Value: json.RawMessage(input)},
		msgmodel.RawField{Key: "error", Value: messageRaw},
		msgmodel.RawField{Key: "time", Value: timeRaw},
	)), nil
}

func reasoningMetadata(details orclient.ReasoningDetailsView) msgmodel.RawObject {
	type envelope struct {
		ReasoningDetails orclient.ReasoningDetailsView `json:"reasoning_details"`
	}
	type metadata struct {
		Openrouter envelope `json:"openrouter"`
	}
	raw, _ := jscompat.Stringify(metadata{Openrouter: envelope{ReasoningDetails: details}})
	return msgmodel.RawObject(raw)
}

func errorPartMessage(raw json.RawMessage) string {
	var value struct {
		Message string `json:"message"`
		Data    *struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &value) == nil {
		if value.Message != "" {
			return value.Message
		}
		if value.Data != nil && value.Data.Message != "" {
			return value.Data.Message
		}
	}
	if len(raw) == 0 {
		return "unknown error"
	}
	return string(raw)
}

func invalidToolMessage(input json.RawMessage) string {
	var value struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(input, &value) == nil && value.Error != "" {
		return value.Error
	}
	return "Invalid tool call"
}
