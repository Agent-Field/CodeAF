package provider

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/calllog"
	"github.com/Agent-Field/codeaf/internal/trace"
)

// mediaCallKey keeps the one logical media call attached to the request context
// until the response path has the status and provider cost to close it.
type mediaCallKey struct{}

// mediaCallLog pairs a media request's start row with the single end row every
// response or transport failure must produce.
type mediaCallLog struct {
	id    string
	model string
	start time.Time
	once  sync.Once
}

// beginMediaCall writes the start row before waiting for a connection or sending
// bytes, so a paid media request is visible even while it is in flight.
func beginMediaCall(ctx context.Context, model string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	call := &mediaCallLog{id: calllog.NewID(), model: model, start: time.Now()}
	calllog.Append(calllog.Record{
		Time:  callLogTime(call.start),
		ID:    call.id,
		Run:   trace.RunFrom(ctx),
		Phase: calllog.PhaseStart,
		Tag:   "media",
		Model: model,
	})
	return context.WithValue(ctx, mediaCallKey{}, call)
}

// finishMediaCall writes the response row once, even if a response and a
// transport cleanup path both try to close the same request.
func finishMediaCall(ctx context.Context, status int, err error, usage *ai.Usage) {
	if ctx == nil {
		return
	}
	call, _ := ctx.Value(mediaCallKey{}).(*mediaCallLog)
	if call == nil {
		return
	}
	call.once.Do(func() {
		record := calllog.Record{
			Time:   callLogTime(time.Now()),
			ID:     call.id,
			Run:    trace.RunFrom(ctx),
			Tag:    "media",
			Model:  call.model,
			Status: status,
			Millis: time.Since(call.start).Milliseconds(),
		}
		if err != nil {
			record.Error = calllog.ClipError(err.Error())
		}
		if usage != nil && usage.Cost != nil {
			record.Cost = *usage.Cost
		}
		calllog.Append(record)
	})
}

// finishMediaResponse finds the request context that doEndpoint carried through
// the HTTP response and closes its media call log entry.
func finishMediaResponse(response *http.Response, status int, err error, usage *ai.Usage) {
	if response == nil || response.Request == nil {
		return
	}
	finishMediaCall(response.Request.Context(), status, err, usage)
}

// callLogTime uses the millisecond timestamp format shared by model call rows.
func callLogTime(now time.Time) string {
	return now.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}
