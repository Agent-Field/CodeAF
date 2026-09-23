//go:build !windows

package projectors

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"
)

type codedError struct {
	name  string
	code  int
	msg   string
	cause error
}

func (e *codedError) Error() string {
	if e.msg != "" {
		return e.msg
	}
	return e.name
}
func (e *codedError) Code() int        { return e.code }
func (e *codedError) CodeName() string { return e.name }
func (e *codedError) Unwrap() error    { return e.cause }

func intPtr(value int) *int { return &value }

func TestIsBusyError(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		depth []int
		want  bool
	}{
		{"named recovery", &codedError{name: "SQLITE_BUSY_RECOVERY"}, nil, true},
		{"primary numeric", &codedError{code: 5}, nil, true},
		{"extended numeric", &codedError{code: 261}, nil, true},
		{"message token", errors.New("Failed to run: SQLITE_BUSY: database is locked"), nil, true},
		{"constraint", &codedError{name: "SQLITE_CONSTRAINT", code: 19}, nil, false},
		{"ioerr", &codedError{code: 266}, nil, false},
		{"plain locked message", errors.New("database is locked"), nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsBusyError(tt.err, tt.depth...); got != tt.want {
				t.Fatalf("IsBusyError() = %v, want %v", got, tt.want)
			}
		})
	}

	deep := &codedError{msg: "outer", cause: &codedError{msg: "mid", cause: &codedError{code: 261}}}
	if !IsBusyError(deep) {
		t.Fatal("two-level wrapped busy error was not recognized")
	}
	var chain error = &codedError{code: 261}
	for i := 0; i < 6; i++ {
		chain = &codedError{msg: "wrapper", cause: chain}
	}
	if IsBusyError(chain) {
		t.Fatal("busy cause beyond the default depth was recognized")
	}
	if !IsBusyError(chain, 10) {
		t.Fatal("explicitly deeper cause was not recognized")
	}
}

func TestWithBusyRetry(t *testing.T) {
	calls := 0
	sleeps := []time.Duration{}
	var logs bytes.Buffer
	got, err := WithBusyRetry(func() (string, error) {
		calls++
		if calls < 3 {
			return "", &codedError{name: "SQLITE_BUSY_RECOVERY", code: 261}
		}
		return "ok", nil
	}, BusyRetryOptions{
		Random: func() float64 { return 0.5 },
		Sleep:  func(delay time.Duration) { sleeps = append(sleeps, delay) },
		Log:    &logs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "ok" || calls != 3 {
		t.Fatalf("got %q after %d calls", got, calls)
	}
	if want := []time.Duration{250 * time.Millisecond, 250 * time.Millisecond}; !reflect.DeepEqual(sleeps, want) {
		t.Fatalf("sleeps = %v, want %v", sleeps, want)
	}
	wantLog := "" +
		"[busy-retry] SQLite busy (attempt 1/5), backing off 250ms\n" +
		"[busy-retry] SQLite busy (attempt 2/5), backing off 250ms\n"
	if logs.String() != wantLog {
		t.Fatalf("log:\n%q\nwant:\n%q", logs.String(), wantLog)
	}
}

func TestWithBusyRetryFailureModes(t *testing.T) {
	calls := 0
	sleeps := 0
	wantErr := errors.New("real bug: not busy")
	_, err := WithBusyRetry(func() (int, error) {
		calls++
		return 0, wantErr
	}, BusyRetryOptions{
		Sleep: func(time.Duration) { sleeps++ },
		Log:   io.Discard,
	})
	if !errors.Is(err, wantErr) || calls != 1 || sleeps != 0 {
		t.Fatalf("non-busy result: err=%v calls=%d sleeps=%d", err, calls, sleeps)
	}

	last := &codedError{name: "SQLITE_BUSY_RECOVERY", code: 261}
	calls = 0
	_, err = WithBusyRetry(func() (int, error) {
		calls++
		return 0, last
	}, BusyRetryOptions{
		MaxAttempts: intPtr(2),
		Random:      func() float64 { return 0 },
		Sleep:       func(time.Duration) {},
		DBPath:      "/tmp/senior-dev.db",
		Log:         io.Discard,
	})
	var exhausted *BusyRetryError
	if !errors.As(err, &exhausted) {
		t.Fatalf("error type = %T, want *BusyRetryError", err)
	}
	wantMessage := "SQLite remained BUSY on database /tmp/senior-dev.db after 2 attempts (last error: SQLITE_BUSY_RECOVERY). " +
		"This is a cold-start contention race between concurrent processes opening the same database — " +
		"reduce launch concurrency or stagger process starts, then retry."
	if exhausted.Error() != wantMessage {
		t.Fatalf("message:\n%s\nwant:\n%s", exhausted, wantMessage)
	}
	if !errors.Is(exhausted, last) || calls != 2 {
		t.Fatalf("cause/calls: cause=%v calls=%d", errors.Unwrap(exhausted), calls)
	}
}
