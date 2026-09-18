package guard

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is the log sink for the one assertion that reads what another
// goroutine wrote.
type syncBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (s *syncBuffer) Write(payload []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buffer.Write(payload)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buffer.String()
}

// captureLog redirects the standard logger for one test and returns what the
// guard wrote there.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buffer := &bytes.Buffer{}
	flags, writer := log.Flags(), log.Writer()
	log.SetOutput(buffer)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(writer)
		log.SetFlags(flags)
	})
	return buffer
}

func TestGoAbsorbsPanicAndLogsIt(t *testing.T) {
	buffer := &syncBuffer{}
	flags, writer := log.Flags(), log.Writer()
	log.SetOutput(buffer)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(writer)
		log.SetFlags(flags)
	})

	// The panic is absorbed above fn's own defers, so the only durable signal
	// that the guard ran is the line it writes.
	Go("narrator", func() { panic("narration blew up") })

	var logged string
	for attempt := 0; attempt < 200; attempt++ {
		if logged = buffer.String(); logged != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if logged == "" {
		t.Fatal("the guarded goroutine logged nothing")
	}
	if !strings.Contains(logged, `scope="narrator"`) {
		t.Fatalf("log missing the scope: %q", logged)
	}
	if !strings.Contains(logged, "narration blew up") {
		t.Fatalf("log missing the panic value: %q", logged)
	}
	if !strings.Contains(logged, "guard_test.go") {
		t.Fatalf("log missing the stack: %q", logged)
	}
}

func TestGoRunsFunctionThatDoesNotPanic(t *testing.T) {
	captureLog(t)
	done := make(chan struct{})
	Go("quiet", func() { close(done) })
	<-done
}

func TestRecoverLetsTheCallerSurvive(t *testing.T) {
	captureLog(t)
	survived := false
	func() {
		defer func() { survived = true }()
		defer Recover("tick")
		panic("tick blew up")
	}()
	if !survived {
		t.Fatal("Recover did not absorb the panic")
	}
}

func TestNoteBecomesAnErrorTheCallerCanRecord(t *testing.T) {
	captureLog(t)
	err := Note("resident/runner leaf job-1", "slice bounds out of range [:-1]")
	if err == nil {
		t.Fatal("Note returned no error")
	}
	if !IsFault(err) {
		t.Fatalf("IsFault said no for %v", err)
	}
	want := "internal fault in resident/runner leaf job-1: slice bounds out of range [:-1]"
	if err.Error() != want {
		t.Fatalf("error text\n got: %s\nwant: %s", err.Error(), want)
	}
	if !IsFault(fmt.Errorf("wrapped: %w", err)) {
		t.Fatal("IsFault lost the fault through a wrap")
	}
	if IsFault(errors.New("an ordinary failure")) {
		t.Fatal("IsFault claimed an ordinary error was a fault")
	}
}

func TestFaultWithoutScopeStillReads(t *testing.T) {
	captureLog(t)
	err := Note("  ", 42)
	if err.Error() != "internal fault in codeaf: 42" {
		t.Fatalf("unexpected text: %s", err.Error())
	}
}

// TestGoCallsTheFaultHookOnceWithTheGoroutineStack holds the hook's whole
// contract: a guarded goroutine that panics calls the fault hook exactly once, with
// the scope Recover was given and the stack of the goroutine that faulted —
// the same bytes the log line carries, because a reporter above this package
// has nothing else to group the fault by.
func TestGoCallsTheFaultHookOnceWithTheGoroutineStack(t *testing.T) {
	captureLog(t)
	t.Cleanup(func() { SetOnFault(nil) })

	type hookCall struct {
		scope string
		stack []byte
	}
	// Buffered for two so a second call is a readable failure rather than a
	// goroutine blocked on a send nobody drains.
	calls := make(chan hookCall, 2)
	SetOnFault(func(scope string, stack []byte) { calls <- hookCall{scope: scope, stack: stack} })

	Go("narrator", func() { panic("narration blew up") })

	var got hookCall
	select {
	case got = <-calls:
	case <-time.After(5 * time.Second):
		t.Fatal("the fault hook was never called")
	}
	if got.scope != "narrator" {
		t.Fatalf("hook scope = %q, want narrator", got.scope)
	}
	if len(got.stack) == 0 {
		t.Fatal("the hook was called with an empty stack")
	}
	if !strings.Contains(string(got.stack), "guard_test.go") {
		t.Fatalf("the stack is not the recovered goroutine's: %q", got.stack)
	}

	select {
	case second := <-calls:
		t.Fatalf("the hook was called twice, second scope %q", second.scope)
	case <-time.After(50 * time.Millisecond):
	}
}
