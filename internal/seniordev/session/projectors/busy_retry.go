//go:build !windows

// The SQLite cold-start retry policy. It lives beside the projector database
// because the only retried operation is that database's first journal_mode
// pragma.
package projectors

import (
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"strings"
	"time"
)

const (
	defaultMaxAttempts = 5
	defaultBaseDelayMS = 100
	defaultMaxDelayMS  = 400
)

// BusyRetryOptions controls WithBusyRetry. Nil numeric fields select the
// defaults; pointers preserve the distinction between omitted and explicitly
// zero options.
type BusyRetryOptions struct {
	MaxAttempts *int
	BaseDelayMS *int
	MaxDelayMS  *int
	DBPath      string
	Random      func() float64
	Sleep       func(time.Duration)
	Log         io.Writer
}

type sqliteCodeError interface {
	Code() int
}

type namedCodeError interface {
	CodeName() string
}

// IsBusyError reports whether err or one of at most depth wrapped causes is a
// SQLite BUSY-class error. The default depth is 5, meaning the outer error
// plus five causes are inspected.
func IsBusyError(err error, depth ...int) bool {
	limit := 5
	if len(depth) > 0 {
		limit = depth[0]
	}
	for i := 0; i <= limit && err != nil; i++ {
		if isBusyErrorShallow(err) {
			return true
		}
		err = unwrapOnce(err)
	}
	return false
}

func isBusyErrorShallow(err error) bool {
	if named, ok := err.(namedCodeError); ok {
		switch named.CodeName() {
		case "SQLITE_BUSY", "SQLITE_BUSY_RECOVERY", "SQLITE_BUSY_SNAPSHOT", "SQLITE_BUSY_TIMEOUT":
			return true
		}
	}
	if coded, ok := err.(sqliteCodeError); ok {
		code := coded.Code()
		if code&0xff == 5 || code == 5 {
			return true
		}
	}
	return strings.Contains(strings.ToUpper(err.Error()), "SQLITE_BUSY")
}

type unwrapper interface {
	Unwrap() error
}

func unwrapOnce(err error) error {
	if wrapped, ok := err.(unwrapper); ok {
		return wrapped.Unwrap()
	}
	return nil
}

// BusyRetryError is returned after all BUSY-class attempts are exhausted.
// Unwrap preserves the last SQLite failure as the cause.
type BusyRetryError struct {
	Message string
	Cause   error
}

func (e *BusyRetryError) Error() string { return e.Message }
func (e *BusyRetryError) Unwrap() error { return e.Cause }

// WithBusyRetry runs fn and retries only BUSY-class errors, sleeping a
// 100–400 ms (inclusive, by default) jitter between attempts.
func WithBusyRetry[T any](fn func() (T, error), opts BusyRetryOptions) (T, error) {
	attempts := defaultMaxAttempts
	if opts.MaxAttempts != nil {
		attempts = max(1, *opts.MaxAttempts)
	}
	base := defaultBaseDelayMS
	if opts.BaseDelayMS != nil {
		base = *opts.BaseDelayMS
	}
	upper := defaultMaxDelayMS
	if opts.MaxDelayMS != nil {
		upper = *opts.MaxDelayMS
	}
	upper = max(base, upper)
	random := opts.Random
	if random == nil {
		random = rand.Float64
	}
	sleep := opts.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	log := opts.Log
	if log == nil {
		log = os.Stderr
	}
	span := upper - base + 1

	var zero T
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		value, err := fn()
		if err == nil {
			return value, nil
		}
		if !IsBusyError(err) {
			return zero, err
		}
		lastErr = err
		if attempt >= attempts {
			break
		}
		delay := base + int(random()*float64(span))
		where := ""
		if opts.DBPath != "" {
			where = " on " + opts.DBPath
		}
		fmt.Fprintf(log, "[busy-retry] SQLite busy%s (attempt %d/%d), backing off %dms\n",
			where, attempt, attempts, delay)
		sleep(time.Duration(delay) * time.Millisecond)
	}

	where := ""
	if opts.DBPath != "" {
		where = " on database " + opts.DBPath
	}
	lastCode := ""
	if named, ok := lastErr.(namedCodeError); ok && named.CodeName() != "" {
		lastCode = " (last error: " + named.CodeName() + ")"
	}
	message := fmt.Sprintf(
		"SQLite remained BUSY%s after %d attempts%s. This is a cold-start contention race between concurrent processes opening the same database — reduce launch concurrency or stagger process starts, then retry.",
		where, attempts, lastCode,
	)
	return zero, &BusyRetryError{Message: message, Cause: lastErr}
}
