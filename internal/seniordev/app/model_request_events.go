//go:build !windows

package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/seniordev/bus"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/orclient"
)

// This is an event-only observer. No field is persisted into model messages,
// used for routing, or read by the budget/exit machinery. IDs are process-local
// and deliberately do not consume the model-message ID generator.
type modelRequestEvent struct {
	RequestID           string `json:"requestID"`
	SessionID           string `json:"sessionID"`
	Agent               string `json:"agent"`
	RequestedProvider   string `json:"requestedProvider"`
	RequestedModel      string `json:"requestedModel"`
	Phase               string `json:"phase"`
	Status              string `json:"status,omitempty"`
	ErrorStage          string `json:"errorStage,omitempty"`
	ResponseID          string `json:"responseID,omitempty"`
	ServedModel         string `json:"servedModel,omitempty"`
	Provider            string `json:"provider,omitempty"`
	FinishReason        string `json:"finishReason,omitempty"`
	ElapsedMS           int64  `json:"elapsedMS"`
	FirstDeltaMS        *int64 `json:"firstDeltaMS,omitempty"`
	LastDeltaMS         *int64 `json:"lastDeltaMS,omitempty"`
	TextCharacters      int64  `json:"textCharacters"`
	ReasoningCharacters int64  `json:"reasoningCharacters"`
	SubstantiveDeltas   int64  `json:"substantiveDeltas"`
}

type modelRequestSink func(modelRequestEvent)

var modelRequestEventDefinition = bus.Define("session.model.request", modelRequestEvent{})
var modelRequestSequence atomic.Uint64

func newModelRequestSink(instance *bus.Bus) modelRequestSink {
	if instance == nil {
		return nil
	}
	return func(event modelRequestEvent) { instance.Publish(modelRequestEventDefinition, event) }
}

type modelRequestObservation struct {
	mu            sync.Mutex
	ctx           context.Context
	sink          modelRequestSink
	event         modelRequestEvent
	start         time.Time
	end           time.Time
	done          bool
	streamError   error
	providerError bool
	aborted       bool
	sawEOF        bool
	sawFinish     bool
}

func beginModelRequest(ctx context.Context, sink modelRequestSink, session, agent, provider, model string) *modelRequestObservation {
	if sink == nil {
		return nil
	}
	o := &modelRequestObservation{ctx: ctx, sink: sink, start: time.Now(), event: modelRequestEvent{
		RequestID: fmt.Sprintf("request-%d", modelRequestSequence.Add(1)),
		SessionID: modelRequestLabel(session), Agent: modelRequestLabel(agent),
		RequestedProvider: modelRequestLabel(provider), RequestedModel: modelRequestLabel(model),
		Phase: "begin",
	}}
	o.emit(o.event)
	return o
}

// Provider-origin fields are bounded identifiers, never arbitrary metadata.
// Reject rather than truncate malformed values, so they cannot resemble a valid
// generation ID after clipping. No raw error, header, usage object, text, tool
// argument, annotation, or reasoning record enters this event.
func modelRequestLabel(value string) string {
	if len(value) > 200 {
		return ""
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '-' || c == '_' || c == '.' || c == '/' || c == ':' || c == ' ' || c == '(' || c == ')') {
			return ""
		}
	}
	return value
}

func modelRequestFinish(value string) string {
	switch value {
	case "stop", "length", "tool-calls", "content-filter", "error", "other", "unknown":
		return value
	default:
		return "unknown"
	}
}

func (o *modelRequestObservation) observe(part orclient.StreamPart, err error) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.done {
		return
	}
	switch value := part.(type) {
	case orclient.TextDeltaPart:
		o.recordDelta(value.Delta, false)
	case orclient.ReasoningDeltaPart:
		o.recordDelta(value.Delta, true)
	case orclient.ResponseMetadataPart:
		if value.IsModel {
			o.event.ServedModel = modelRequestLabel(value.ModelID)
		} else {
			o.event.ResponseID = modelRequestLabel(value.ID)
		}
	case orclient.FinishPart:
		o.sawFinish = true
		o.event.FinishReason = modelRequestFinish(value.FinishReason.Unified)
		if value.Metadata.Provider != nil {
			o.event.Provider = modelRequestLabel(*value.Metadata.Provider)
		}
		if o.end.IsZero() {
			o.end = time.Now()
		}
	case orclient.ErrorPart:
		o.providerError = true
		if o.end.IsZero() {
			o.end = time.Now()
		}
	case orclient.AbortPart:
		o.aborted = true
		if o.end.IsZero() {
			o.end = time.Now()
		}
	}
	if err != nil {
		if errors.Is(err, io.EOF) {
			o.sawEOF = true
		} else if o.streamError == nil {
			o.streamError = err
		}
		if o.end.IsZero() {
			o.end = time.Now()
		}
	}
}

// Called under mu; count Unicode code points without retaining content.
// Empty deltas and tool-input deltas are deliberately not substantive here.
func (o *modelRequestObservation) recordDelta(value string, reasoning bool) {
	if value == "" {
		return
	}
	elapsed := time.Since(o.start).Milliseconds()
	if o.event.FirstDeltaMS == nil {
		first := elapsed
		o.event.FirstDeltaMS = &first
	}
	o.event.LastDeltaMS = &elapsed
	o.event.SubstantiveDeltas++
	if reasoning {
		o.event.ReasoningCharacters += int64(utf8.RuneCountInString(value))
	} else {
		o.event.TextCharacters += int64(utf8.RuneCountInString(value))
	}
}

// Close is the ownership boundary, so it emits the one final observation even
// for a caller that abandons a canceled stream without reading its last part.
// Delay publication until Close to include cleanup errors; elapsed time ends at
// the first stream terminal observation, excluding subsequent tool settlement.
func (o *modelRequestObservation) finish(stage string, err error) {
	if o == nil {
		return
	}
	o.mu.Lock()
	if o.done {
		o.mu.Unlock()
		return
	}
	o.done = true
	if o.end.IsZero() {
		o.end = time.Now()
	}
	event := o.event
	event.Phase = "end"
	event.ElapsedMS = o.end.Sub(o.start).Milliseconds()
	if o.streamError != nil {
		err, stage = o.streamError, "stream"
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		event.Status, event.ErrorStage = "deadline", stage
	case errors.Is(err, context.Canceled):
		event.Status, event.ErrorStage = "canceled", stage
	case err != nil:
		event.Status, event.ErrorStage = "error", stage
	case o.providerError:
		event.Status, event.ErrorStage = "provider-error", "stream"
	case o.aborted:
		event.Status, event.ErrorStage = "aborted", "stream"
	case o.sawFinish:
		event.Status = "finished"
	case errors.Is(o.ctx.Err(), context.DeadlineExceeded):
		event.Status, event.ErrorStage = "deadline", "close"
	case errors.Is(o.ctx.Err(), context.Canceled):
		event.Status, event.ErrorStage = "canceled", "close"
	case o.sawEOF:
		event.Status = "eof-without-finish"
	default:
		event.Status = "closed-without-finish"
	}
	o.mu.Unlock()
	o.emit(event)
}

func (o *modelRequestObservation) emit(event modelRequestEvent) {
	// Optional telemetry failures must never change model success, errors, or
	// cleanup. The concrete sink only publishes to the existing local bus.
	defer func() { _ = recover() }()
	o.sink(event)
}
