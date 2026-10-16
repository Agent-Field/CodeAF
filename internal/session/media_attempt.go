package session

import (
	"context"
	"sync/atomic"
)

// THIS CALL MADE A RENDER RECORD, SAID AS A FACT ON THE CALL'S OWN RESULT.
//
// A surface that wants the record a media call produced must not find it by
// the tool's name or by reading the result's words: a name says what was asked
// for, not whether a record exists, and a refused, rejected or failed-to-start
// call has none. So the code that wrote the record says so, the way
// toolrejection.go's mark is said: a call-local flag planted by dispatchTool,
// set only after a render record with this call's run and call id exists, and
// carried on the result occurrence ([ToolResultMediaAttempt]). There is no map
// and no goroutine; the flag lives and dies with the one call.

type mediaAttemptKey struct{}

// withMediaAttempt plants a fresh mark for one call.
func withMediaAttempt(ctx context.Context) (context.Context, *atomic.Bool) {
	mark := &atomic.Bool{}
	return context.WithValue(ctx, mediaAttemptKey{}, mark), mark
}

// markMediaAttempt records that this call has a render record. A context with
// no mark (a direct handler call in a test, the command line's door) records
// nothing.
func markMediaAttempt(ctx context.Context) {
	if ctx == nil {
		return
	}
	if mark, _ := ctx.Value(mediaAttemptKey{}).(*atomic.Bool); mark != nil {
		mark.Store(true)
	}
}
