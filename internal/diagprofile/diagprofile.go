// Package diagprofile is the developer-only, opt-in, bounded CPU (and
// optionally heap) profile of one codeaf process, for real performance
// investigation.
//
// IT IS OFF, EXACTLY. With CODEAF_DIAGNOSTIC_PROFILE_DIR unset or empty,
// [Start] reads the environment, returns an inert [Session], and does nothing
// else: no goroutine, no file, no directory, no profiler, no stderr. A Session
// that never started is safe to Stop any number of times.
//
// It is generic on purpose. The hook is the process's own main, so every
// command surface (chat, do, exec, run, engine, app, ...) is covered by the same
// two lines and none of them knows this package exists. Nothing here is an app
// API, a port, or a protocol field.
//
// The knobs are three environment variables, all read once, at start:
//
//	CODEAF_DIAGNOSTIC_PROFILE_DIR      absolute path of a private directory
//	CODEAF_DIAGNOSTIC_PROFILE_SECONDS  optional, whole seconds 1..60, default 30
//	CODEAF_DIAGNOSTIC_PROFILE_HEAP     optional, exactly "1" to also write a heap profile
//
// A variable that is set and wrong is REFUSED ([Start] returns an *Error whose
// text is a fixed sentence): a developer who asked for a profile and silently
// got none would investigate a number that was never measured. The sentence
// never quotes the value, so a mistyped path or a pasted secret is not echoed
// into a log.
//
// The inheritance rule is documented rather than hidden: the variables are NOT
// cleared, so a codeaf process started by a codeaf process (the engine daemon a
// chat launch spawns, a worker's own `codeaf` call) profiles itself too, into
// the same directory. That is safe because every file name is unique per
// process and per file, and every file is created exclusively. Unset the
// variable for a child you do not want measured.
package diagprofile

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"sync"
	"time"
)

const (
	// EnvDir, EnvSeconds and EnvHeap are the whole public surface.
	EnvDir     = "CODEAF_DIAGNOSTIC_PROFILE_DIR"
	EnvSeconds = "CODEAF_DIAGNOSTIC_PROFILE_SECONDS"
	EnvHeap    = "CODEAF_DIAGNOSTIC_PROFILE_HEAP"

	// DefaultSeconds is the interval when EnvSeconds is unset; MinSeconds and
	// MaxSeconds bound it. A profile is a measurement of a window, and an
	// unbounded window is a profiler left on by accident.
	DefaultSeconds = 30
	MinSeconds     = 1
	MaxSeconds     = 60

	// FilePrefix and FileSuffix make the files findable: codeaf-cpu-*.pprof and
	// codeaf-heap-*.pprof.
	FilePrefix = "codeaf-"
	FileSuffix = ".pprof"

	// createAttempts bounds the retry when a freshly drawn name already exists.
	// With a 64-bit random component one retry is already astronomically rare.
	createAttempts = 4
)

// Error is a refusal. Its text is fixed per cause and never contains a value
// the environment supplied.
type Error struct{ Reason string }

func (e *Error) Error() string { return "diagnostic profile refused: " + e.Reason }

func refuse(reason string) error { return &Error{Reason: reason} }

// Lookup is how the environment is read. The product passes the codeaf
// environment door; tests pass a map.
type Lookup func(name string) (value string, set bool)

// Config is the parsed environment. The zero value is "off".
type Config struct {
	Dir      string
	Duration time.Duration
	Heap     bool
}

// Enabled reports whether any profiling was asked for.
func (c Config) Enabled() bool { return c.Dir != "" }

// ParseConfig reads and validates the three variables without touching the
// filesystem. Unset or empty EnvDir is off whatever the others say.
func ParseConfig(lookup Lookup) (Config, error) {
	dir, _ := lookup(EnvDir)
	if dir == "" {
		return Config{}, nil
	}
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return Config{}, refuse("the directory must be an absolute, clean path")
	}
	cfg := Config{Dir: dir, Duration: DefaultSeconds * time.Second}
	if raw, _ := lookup(EnvSeconds); raw != "" {
		n, ok := parseSeconds(raw)
		if !ok {
			return Config{}, refuse(fmt.Sprintf("the duration must be a whole number of seconds from %d to %d", MinSeconds, MaxSeconds))
		}
		cfg.Duration = time.Duration(n) * time.Second
	}
	switch raw, _ := lookup(EnvHeap); raw {
	case "", "0":
	case "1":
		cfg.Heap = true
	default:
		return Config{}, refuse(`the heap switch must be "1" or unset`)
	}
	return cfg, nil
}

// parseSeconds accepts plain ASCII digits only: no sign, no space, no unit.
func parseSeconds(raw string) (int, bool) {
	if len(raw) > 2 {
		return 0, false
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < MinSeconds || n > MaxSeconds {
		return 0, false
	}
	return n, true
}

// Session is one process's profile. Stop is idempotent and safe on a nil or
// inert Session.
type Session struct {
	cfg     Config
	warn    func(string)
	cancel  context.CancelFunc
	cpu     *os.File
	done    chan struct{} // closed when the finishing goroutine has returned
	stopOne sync.Once
}

// Start validates the environment, prepares the directory, creates the CPU
// profile file exclusively, starts the CPU profiler and ONE goroutine that
// owns the interval and the finishing work. warn receives fixed sentences about
// failures after start (it is never given a value from the environment, and it
// must not write to stdout); it may be nil.
//
// The returned Session is non-nil on success even when profiling is off. On
// error nothing was started and nothing remains on disk but, at most, a
// directory Start created itself.
//
// Cancelling ctx finishes the profile early exactly as Stop does.
func Start(ctx context.Context, lookup Lookup, warn func(string)) (*Session, error) {
	cfg, err := ParseConfig(lookup)
	if err != nil {
		return nil, err
	}
	return start(ctx, cfg, warn)
}

// start is Start after parsing, so a test can pick an interval shorter than
// the one second the environment allows.
func start(ctx context.Context, cfg Config, warn func(string)) (*Session, error) {
	if !cfg.Enabled() {
		return &Session{}, nil
	}
	if warn == nil {
		warn = func(string) {}
	}
	if err := preparePrivateDir(cfg.Dir); err != nil {
		return nil, err
	}
	nonce, err := newNonce()
	if err != nil {
		return nil, refuse("a unique file name could not be drawn")
	}
	cpu, _, err := createExclusive(cfg.Dir, "cpu", nonce)
	if err != nil {
		return nil, err
	}
	if err := pprof.StartCPUProfile(cpu); err != nil {
		cpu.Close()
		removeOwn(cpu)
		return nil, refuse("a CPU profile is already running in this process")
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Session{cfg: cfg, warn: warn, cancel: cancel, cpu: cpu, done: make(chan struct{})}
	go s.run(ctx, nonce)
	return s, nil
}

// run is the single goroutine. It waits for the interval or a cancellation,
// then finishes once. The expensive CPU stop therefore happens here, never on
// the caller's goroutine unless the caller chose to wait in Stop.
func (s *Session) run(ctx context.Context, nonce string) {
	defer close(s.done)
	timer := time.NewTimer(s.cfg.Duration)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
	s.finish(nonce)
}

func (s *Session) finish(nonce string) {
	pprof.StopCPUProfile()
	if err := s.cpu.Close(); err != nil {
		s.warn("diagnostic profile: the CPU profile could not be closed")
	}
	if !s.cfg.Heap {
		return
	}
	// A heap SNAPSHOT: the runtime's already-sampled allocation record as of
	// the last completed collection. runtime.GC is deliberately not called;
	// forcing a collection would distort the very behaviour being measured.
	heap, _, err := createExclusive(s.cfg.Dir, "heap", nonce)
	if err != nil {
		s.warn("diagnostic profile: the heap profile file could not be created")
		return
	}
	if err := pprof.Lookup("heap").WriteTo(heap, 0); err != nil {
		s.warn("diagnostic profile: the heap profile could not be written")
	}
	if err := heap.Close(); err != nil {
		s.warn("diagnostic profile: the heap profile could not be closed")
	}
}

// Stop ends profiling early (or collects it if the interval already ran out)
// and waits for the files to be complete and closed. It is idempotent and
// concurrency-safe. Call it on the way out of the process, before os.Exit.
func (s *Session) Stop() {
	if s == nil || s.done == nil {
		return
	}
	s.stopOne.Do(s.cancel)
	<-s.done
}

// Active reports whether this Session started a profiler.
func (s *Session) Active() bool { return s != nil && s.done != nil }

func newNonce() (string, error) {
	var raw [8]byte
	if _, err := io.ReadFull(rand.Reader, raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

// createExclusive creates codeaf-<kind>-<time>-p<pid>-<nonce>.pprof with
// O_EXCL, never truncating or following anything, mode 0600. The pid and a
// fresh random nonce are both in the name so a child process, or a second run
// in the same second, cannot name a file its parent or sibling already made;
// the nonce is shared by one process's cpu and heap file so they pair up.
func createExclusive(dir, kind, nonce string) (*os.File, string, error) {
	for attempt := 0; attempt < createAttempts; attempt++ {
		name := fmt.Sprintf("%s%s-%s-p%d-%s%s", FilePrefix, kind, time.Now().UTC().Format("20060102T150405Z"), os.Getpid(), nonce, FileSuffix)
		f, err := openNew(filepath.Join(dir, name))
		if err == nil {
			return f, name, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, "", refuse("the profile file could not be created in the directory")
		}
		if nonce, err = newNonce(); err != nil {
			break
		}
	}
	return nil, "", refuse("a unique profile file name could not be created")
}

// removeOwn deletes a file this process just created and has not written to.
func removeOwn(f *os.File) { os.Remove(f.Name()) }
