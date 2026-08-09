// Package mergecoordinator is a bug-for-bug port of
// src/session/merge-coordinator.ts:1-132 (swe-pro 3b25a1a). It computes a
// structural merge-risk score and serializes merge work in ascending-risk
// order after a 1500ms settling window.
//
// TypeScript's event loop makes queue transitions atomic. Go callers may
// submit and complete work concurrently, so MergeCoordinator protects all
// queue, timer, and processing state with one mutex. User work and result
// waits never run while that mutex is held. Clock reads, log output, and timer
// creation happen at the same synchronous queue-transition points as their JS
// counterparts.
//
// Pure seams:
//   - ComputeMergeRiskFromNumstat is the decision core behind Process.run.
//   - OrderByMergeRisk is the exact stable comparator used at dispatch.
//
// Both are pinned against executions of the real TS exports. DiffRunner,
// Clock, and TimerFactory make the I/O shell and settling behavior
// deterministic in tests.
//
// Fidelity notes:
//   - Numstat is trimmed with JS whitespace and split only on "\n"; each line
//     is then split with JS /\s+/, not Go's narrower regexp \s.
//   - Number.parseInt(..., 10) is a decimal-prefix parse into a JS number, so
//     scores are float64 and can exceed exact-integer range or become ±Inf/NaN.
//   - New arrivals do not restart an already-armed settling timer. Once a
//     batch starts, arrivals during work drain back-to-back without another
//     delay. The next arrival after a fully empty drain starts a new window.
//   - A work item that synchronously calls Run on this same coordinator and
//     waits for it deadlocks, as does the analogous nested awaited Promise in
//     the TS source. No re-entrancy escape hatch is added.
package mergecoordinator

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/logshim"
)

const (
	settlingMS  = 1500
	defaultRisk = 1_000_000
)

var log = logshim.Create(map[string]any{"service": "session.merge-coordinator"})

// DiffResult is the Process.run result used by ComputeMergeRiskWithRunner.
type DiffResult struct {
	Code   int
	Stdout []byte
	Stderr []byte
}

// DiffRunner is the single git-diff process seam.
type DiffRunner func(argv []string, cwd string) (DiffResult, error)

// ComputeMergeRisk executes git diff --numstat and returns the TS risk score.
// All process failures collapse to the pessimistic default, never an error.
func ComputeMergeRisk(worktreePath, baseSHA string) float64 {
	return ComputeMergeRiskWithRunner(worktreePath, baseSHA, defaultDiffRunner)
}

// ComputeMergeRiskWithRunner is ComputeMergeRisk with an injectable process
// seam for deterministic tests.
func ComputeMergeRiskWithRunner(worktreePath, baseSHA string, run DiffRunner) float64 {
	if run == nil {
		run = defaultDiffRunner
	}
	result, err := run([]string{"git", "diff", "--numstat", baseSHA, "HEAD"}, worktreePath)
	if err != nil {
		return defaultRisk
	}
	return ComputeMergeRiskFromNumstat(string(result.Stdout), result.Code)
}

// ComputeMergeRiskFromNumstat is the pure decision core:
//
//	files_touched * total_LOC_changed * max(1, top_level_dirs)
func ComputeMergeRiskFromNumstat(stdout string, exitCode int) float64 {
	if exitCode != 0 {
		return defaultRisk
	}
	trimmed := jscompat.Trim(strings.ToValidUTF8(stdout, "\uFFFD"))
	rawLines := strings.Split(trimmed, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return 0
	}

	totalLines := float64(0)
	topDirs := map[string]struct{}{}
	for _, line := range lines {
		parts := splitJSWhitespaceRuns(line)
		adds := numstatCount(parts, 0)
		dels := numstatCount(parts, 1)
		totalLines += adds + dels

		file := ""
		if len(parts) > 2 {
			file = strings.Join(parts[2:], " ")
		}
		top := file
		if slash := strings.Index(top, "/"); slash >= 0 {
			top = top[:slash]
		}
		if top != "" {
			topDirs[top] = struct{}{}
		}
	}
	dirFactor := len(topDirs)
	if dirFactor < 1 {
		dirFactor = 1
	}
	return float64(len(lines)) * totalLines * float64(dirFactor)
}

func numstatCount(parts []string, index int) float64 {
	if index >= len(parts) || parts[index] == "-" {
		return 0
	}
	n := parseInt10(parts[index])
	// `parseInt(...) || 0`: NaN, +0, and -0 all select positive zero.
	if math.IsNaN(n) || n == 0 {
		return 0
	}
	return n
}

// parseInt10 mirrors Number.parseInt(text, 10) for a token produced by
// split(/\s+/): optional sign, then the longest ASCII-decimal prefix.
func parseInt10(text string) float64 {
	if text == "" {
		return math.NaN()
	}
	i := 0
	if text[0] == '+' || text[0] == '-' {
		i++
	}
	startDigits := i
	for i < len(text) && text[i] >= '0' && text[i] <= '9' {
		i++
	}
	if i == startDigits {
		return math.NaN()
	}
	n, err := strconv.ParseFloat(text[:i], 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return math.NaN()
	}
	return n
}

// splitJSWhitespaceRuns mirrors String.prototype.split(/\s+/). Unlike
// strings.Fields it preserves the leading/trailing empty fields produced by a
// match at either edge.
func splitJSWhitespaceRuns(text string) []string {
	if text == "" {
		return []string{""}
	}
	parts := []string{}
	start := 0
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		if !isJSWhitespace(r) {
			i += size
			continue
		}
		parts = append(parts, text[start:i])
		i += size
		for i < len(text) {
			r, size = utf8.DecodeRuneInString(text[i:])
			if !isJSWhitespace(r) {
				break
			}
			i += size
		}
		start = i
	}
	parts = append(parts, text[start:])
	return parts
}

func isJSWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ',
		0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func defaultDiffRunner(argv []string, cwd string) (DiffResult, error) {
	if len(argv) == 0 {
		return DiffResult{Code: 1, Stderr: []byte("Command is required")}, nil
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return DiffResult{Code: 0, Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return DiffResult{
			Code: exitErr.ExitCode(), Stdout: stdout.Bytes(), Stderr: stderr.Bytes(),
		}, nil
	}
	// Process.run(..., {nothrow:true}) returns code 1 for spawn errors.
	return DiffResult{Code: 1, Stdout: stdout.Bytes(), Stderr: []byte(err.Error())}, nil
}

// RiskOrderItem is the pure view of a pending merge used by
// OrderByMergeRisk. Arrived is Date.now() in milliseconds.
type RiskOrderItem struct {
	TaskID    string  `json:"taskID"`
	RiskScore float64 `json:"riskScore"`
	Arrived   float64 `json:"arrived"`
}

// OrderByMergeRisk returns a stable sorted copy using the coordinator's exact
// risk-then-arrival comparator.
func OrderByMergeRisk(items []RiskOrderItem) []RiskOrderItem {
	out := append([]RiskOrderItem{}, items...)
	sort.SliceStable(out, func(i, j int) bool {
		return compareRiskOrder(out[i], out[j]) < 0
	})
	return out
}

func compareRiskOrder(a, b RiskOrderItem) float64 {
	// JS !== treats NaN as different from everything, including itself.
	if a.RiskScore != b.RiskScore {
		return a.RiskScore - b.RiskScore
	}
	return a.Arrived - b.Arrived
}

// Clock is the Date.now seam and returns milliseconds.
type Clock func() float64

// Timer is the opaque setTimeout handle. The coordinator never clears its
// settling timer, but Stop keeps fake and real handles consistent with other
// timer-backed ports.
type Timer interface {
	Stop()
}

// TimerFactory is the setTimeout seam; ms is always 1500 in this module.
type TimerFactory func(ms float64, fn func()) Timer

type realTimer struct{ timer *time.Timer }

func (t realTimer) Stop() { t.timer.Stop() }

// Options supplies the coordinator's clock and timer. Nil fields use runtime
// defaults.
type Options struct {
	Now          Clock
	TimerFactory TimerFactory
}

// Work is one serialized merge.
type Work func() (any, error)

type pending struct {
	taskID    string
	riskScore float64
	arrived   float64
	work      Work
	future    *Future
}

// Future is the Promise returned by Submit.
type Future struct {
	done  chan struct{}
	value any
	err   error
}

// Await blocks until the submitted merge resolves or rejects.
func (f *Future) Await() (any, error) {
	<-f.done
	return f.value, f.err
}

// InspectResult is the test/debug queue snapshot.
type InspectResult struct {
	Queued     int  `json:"queued"`
	Processing bool `json:"processing"`
}

// MergeCoordinator owns one in-memory serialized merge queue.
type MergeCoordinator struct {
	mu            sync.Mutex
	pending       []*pending
	processing    bool
	settlingTimer Timer
	now           Clock
	newTimer      TimerFactory
}

// New constructs an idle coordinator. It is variadic to mirror the TS
// no-argument constructor while still permitting deterministic seams.
func New(options ...Options) *MergeCoordinator {
	var opts Options
	if len(options) > 0 {
		opts = options[0]
	}
	now := opts.Now
	if now == nil {
		now = func() float64 { return float64(time.Now().UnixMilli()) }
	}
	newTimer := opts.TimerFactory
	if newTimer == nil {
		newTimer = func(ms float64, fn func()) Timer {
			return realTimer{timer: time.AfterFunc(time.Duration(ms*float64(time.Millisecond)), fn)}
		}
	}
	return &MergeCoordinator{
		pending:  [](*pending){},
		now:      now,
		newTimer: newTimer,
	}
}

// Submit enqueues a merge synchronously and returns its future, matching the
// immediate Promise construction in MergeCoordinator.run.
func (c *MergeCoordinator) Submit(taskID string, riskScore float64, work Work) *Future {
	future := &Future{done: make(chan struct{})}
	item := &pending{
		taskID: taskID, riskScore: riskScore, work: work, future: future,
	}

	c.mu.Lock()
	item.arrived = c.now()
	c.pending = append(c.pending, item)
	c.kickLocked()
	c.mu.Unlock()
	return future
}

// Run submits a merge and waits for its completion.
func (c *MergeCoordinator) Run(taskID string, riskScore float64, work Work) (any, error) {
	return c.Submit(taskID, riskScore, work).Await()
}

// Inspect returns queued count and active-processing state under the mutex.
func (c *MergeCoordinator) Inspect() InspectResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	return InspectResult{Queued: len(c.pending), Processing: c.processing}
}

func (c *MergeCoordinator) kickLocked() {
	if c.processing || c.settlingTimer != nil || len(c.pending) == 0 {
		return
	}
	c.settlingTimer = c.newTimer(settlingMS, func() {
		c.mu.Lock()
		c.settlingTimer = nil
		head := c.takeNextLocked()
		c.mu.Unlock()
		if head != nil {
			c.drain(head)
		}
	})
}

func (c *MergeCoordinator) takeNextLocked() *pending {
	if c.processing || len(c.pending) == 0 {
		return nil
	}
	c.processing = true
	sort.SliceStable(c.pending, func(i, j int) bool {
		a := RiskOrderItem{
			TaskID: c.pending[i].taskID, RiskScore: c.pending[i].riskScore,
			Arrived: c.pending[i].arrived,
		}
		b := RiskOrderItem{
			TaskID: c.pending[j].taskID, RiskScore: c.pending[j].riskScore,
			Arrived: c.pending[j].arrived,
		}
		return compareRiskOrder(a, b) < 0
	})
	head := c.pending[0]
	c.pending[0] = nil
	c.pending = c.pending[1:]
	log.Info("merge dispatched", map[string]any{
		"taskID": head.taskID, "riskScore": head.riskScore,
		"remaining": len(c.pending),
	})
	return head
}

func (c *MergeCoordinator) drain(head *pending) {
	for head != nil {
		value, err := invokeWork(head.work)

		c.mu.Lock()
		// TS sets false, then synchronously calls processNext before another
		// event-loop turn. Keep that transition inside this critical section
		// so a concurrent Submit cannot accidentally arm a new window.
		c.processing = false
		next := c.takeNextLocked()
		c.mu.Unlock()

		// Promise continuations cannot run until the JS processNext continuation
		// above has finished its state transition. Closing only after that
		// transition gives Go waiters the same observable Inspect/Submit state.
		head.future.value, head.future.err = value, err
		close(head.future.done)
		head = next
	}
}

func invokeWork(work Work) (value any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if recoveredErr, ok := recovered.(error); ok {
				err = recoveredErr
			} else {
				err = fmt.Errorf("%v", recovered)
			}
		}
	}()
	return work()
}
