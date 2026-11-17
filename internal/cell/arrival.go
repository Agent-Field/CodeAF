package cell

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// DefaultArrivalStallBound is the fallback stall bound for waiting on arriving
// workspace files when a caller supplies none.
var DefaultArrivalStallBound = 2 * time.Minute

type arrivalMarkerRecord struct {
	PID   int    `json:"pid"`
	Start string `json:"start,omitempty"`
}

// WorkspaceArrival coordinates tools and captures with a background workspace
// fetch after a chat has opened from its conversation records (C1, C3, C4).
type WorkspaceArrival struct {
	mu           sync.Mutex
	notify       chan struct{}
	roots        []string
	markers      []string
	ready        bool
	running      bool
	err          error
	lastProgress time.Time
	failedAt     time.Time
	stallBound   func() time.Duration
	retry        func()
}

var arrivals = struct {
	sync.Mutex
	byPath map[string]*WorkspaceArrival
}{byPath: map[string]*WorkspaceArrival{}}

func cleanArrivalKey(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	return filepath.Clean(p)
}

func arrivalKeys(p string) []string {
	k := cleanArrivalKey(p)
	if k == "" {
		return nil
	}
	if eval, err := filepath.EvalSymlinks(k); err == nil {
		eval = cleanArrivalKey(eval)
		if eval != "" && eval != k {
			return []string{k, eval}
		}
	}
	return []string{k}
}

func processStatus(pid int) (startToken string, alive bool) {
	if pid <= 0 {
		return "", false
	}
	if raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
		if idx := bytes.LastIndexByte(raw, ')'); idx >= 0 && idx+2 < len(raw) {
			fields := strings.Fields(string(raw[idx+2:]))
			if len(fields) > 0 && (fields[0] == "Z" || fields[0] == "X" || fields[0] == "x") {
				return "", false
			}
			if len(fields) > 19 {
				return fields[19], true
			}
		}
		return "", true
	} else if errors.Is(err, os.ErrNotExist) {
		if _, selfErr := os.Stat("/proc/self/stat"); selfErr == nil {
			return "", false
		}
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return "", false
	}
	if sigErr := proc.Signal(syscall.Signal(0)); sigErr != nil && !errors.Is(sigErr, os.ErrPermission) && !errors.Is(sigErr, syscall.EPERM) {
		return "", false
	}
	return "", true
}

func arrivalOwnerAlive(rec arrivalMarkerRecord) bool {
	start, alive := processStatus(rec.PID)
	if !alive {
		return false
	}
	if rec.Start != "" && start != "" && rec.Start != start {
		return false
	}
	return true
}

func writeArrivalMarker(path string) error {
	pid := os.Getpid()
	start, _ := processStatus(pid)
	return writeArrivalMarkerRecord(path, arrivalMarkerRecord{PID: pid, Start: start})
}

func writeArrivalMarkerRecord(path string, rec arrivalMarkerRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}

func validArrivalMarker(path string, bound time.Duration) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var rec arrivalMarkerRecord
	if json.Unmarshal(raw, &rec) != nil || !arrivalOwnerAlive(rec) {
		_ = os.Remove(path)
		return false
	}
	return time.Since(info.ModTime()) < bound
}

// ArrivalMarkerPath returns the local-only cross-process arrival marker path
// for dir (a cell root or its workspace), or "" when dir does not belong to a
// cell. The marker lives under the engine's per-cell local store directory
// (<home>/v3/stores/<cellID>/), never inside the cell's .cell/ or workspace
// tree, so it can never be sealed, captured or published.
func ArrivalMarkerPath(dir string) string {
	return arrivalMarkerPath(dir)
}

func arrivalMarkerPath(dir string) string {
	k := cleanArrivalKey(dir)
	if k == "" {
		return ""
	}
	var cellRoot string
	if info, err := os.Stat(filepath.Join(k, StateDir)); err == nil && info.IsDir() {
		cellRoot = k
	} else if info, err := os.Stat(filepath.Join(filepath.Dir(k), StateDir)); err == nil && info.IsDir() {
		cellRoot = filepath.Dir(k)
	} else {
		return ""
	}
	keys := arrivalKeys(cellRoot)
	canon := keys[len(keys)-1]
	sum := sha256.Sum256([]byte(canon))
	hash := hex.EncodeToString(sum[:16])
	cellID := filepath.Base(canon)

	sep := string(filepath.Separator)
	v3Marker := sep + "v3" + sep
	if idx := strings.Index(canon, v3Marker); idx >= 0 {
		v3Dir := canon[:idx+len(v3Marker)-1]
		return filepath.Join(v3Dir, "stores", cellID, ".arriving-"+hash)
	}
	return filepath.Join(filepath.Dir(canon), ".stores", cellID, ".arriving-"+hash)
}

// BeginWorkspaceArrival registers an in-flight workspace fetch for cellRoot and
// workspace. Tools and full-tree captures wait on it until Complete is called.
func BeginWorkspaceArrival(cellRoot, workspace string, stallBound func() time.Duration, retry func()) *WorkspaceArrival {
	now := time.Now()
	a := &WorkspaceArrival{
		notify:       make(chan struct{}),
		running:      true,
		lastProgress: now,
		stallBound:   stallBound,
		retry:        retry,
	}
	arrivals.Lock()
	defer arrivals.Unlock()
	for _, p := range []string{cellRoot, workspace} {
		for _, k := range arrivalKeys(p) {
			a.roots = append(a.roots, k)
			arrivals.byPath[k] = a
		}
		if m := arrivalMarkerPath(p); m != "" {
			if writeArrivalMarker(m) == nil {
				a.markers = append(a.markers, m)
			}
		}
	}
	return a
}

// LookupWorkspaceArrival returns the active arrival gate for any of paths, or nil.
func LookupWorkspaceArrival(paths ...string) *WorkspaceArrival {
	arrivals.Lock()
	defer arrivals.Unlock()
	for _, p := range paths {
		for _, k := range arrivalKeys(p) {
			if a := arrivals.byPath[k]; a != nil && !a.isReady() {
				return a
			}
			if parent := filepath.Dir(k); parent != k {
				if a := arrivals.byPath[parent]; a != nil && !a.isReady() {
					return a
				}
			}
		}
	}
	return nil
}

func externalArrivalMarker(paths ...string) string {
	bound := DefaultArrivalStallBound
	if bound <= 0 {
		bound = 2 * time.Minute
	}
	for _, p := range paths {
		if m := arrivalMarkerPath(p); m != "" {
			if validArrivalMarker(m, bound) {
				return m
			}
		}
	}
	return ""
}

type arrivalOwnerKey struct{}

// WithArrivalOwner marks ctx as belonging to the background arrival worker
// itself, so operations it performs before completing the arrival do not
// deadlock waiting on their own gate.
func WithArrivalOwner(ctx context.Context) context.Context {
	return context.WithValue(ctx, arrivalOwnerKey{}, true)
}

// IsArrivalOwner reports whether ctx belongs to the background arrival worker.
func IsArrivalOwner(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(arrivalOwnerKey{}).(bool)
	return v
}

func isArrivalOwner(ctx context.Context) bool {
	return IsArrivalOwner(ctx)
}

// WorkspaceArriving reports whether workspace files are still arriving (or
// waiting on a failed arrival's retry) for any of paths.
func WorkspaceArriving(paths ...string) bool {
	if LookupWorkspaceArrival(paths...) != nil {
		return true
	}
	return externalArrivalMarker(paths...) != ""
}

// WaitWorkspace waits for any arriving workspace covering paths to be put in
// place. It returns nil immediately when no background arrival is pending.
func WaitWorkspace(ctx context.Context, paths ...string) error {
	if isArrivalOwner(ctx) {
		return nil
	}
	if a := LookupWorkspaceArrival(paths...); a != nil {
		return a.Wait(ctx)
	}
	marker := externalArrivalMarker(paths...)
	if marker == "" {
		return nil
	}
	bound := DefaultArrivalStallBound
	if bound <= 0 {
		bound = 2 * time.Minute
	}
	ticker := time.NewTicker(15 * time.Millisecond)
	defer ticker.Stop()
	for {
		info, err := os.Stat(marker)
		if err != nil {
			return nil
		}
		raw, err := os.ReadFile(marker)
		if err != nil {
			return nil
		}
		var rec arrivalMarkerRecord
		if json.Unmarshal(raw, &rec) != nil || !arrivalOwnerAlive(rec) {
			_ = os.Remove(marker)
			return nil
		}
		if time.Since(info.ModTime()) >= bound {
			return fmt.Errorf("workspace files stalled: nothing came through for %s", bound)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (a *WorkspaceArrival) isReady() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ready
}

func (a *WorkspaceArrival) broadcastLocked() {
	close(a.notify)
	a.notify = make(chan struct{})
}

// BindPaths associates additional root or workspace paths with this arrival.
func (a *WorkspaceArrival) BindPaths(paths ...string) {
	if a == nil {
		return
	}
	arrivals.Lock()
	defer arrivals.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ready {
		return
	}
	for _, p := range paths {
		for _, k := range arrivalKeys(p) {
			a.roots = append(a.roots, k)
			arrivals.byPath[k] = a
		}
		if m := arrivalMarkerPath(p); m != "" {
			if writeArrivalMarker(m) == nil {
				a.markers = append(a.markers, m)
			}
		}
	}
}

// SetRetry sets or updates the retry callback for this arrival.
func (a *WorkspaceArrival) SetRetry(retry func()) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.retry = retry
}

// Touch notes that bytes or progress arrived, resetting the stall clock.
func (a *WorkspaceArrival) Touch() {
	if a == nil {
		return
	}
	now := time.Now()
	a.mu.Lock()
	a.lastProgress = now
	markers := append([]string(nil), a.markers...)
	a.broadcastLocked()
	a.mu.Unlock()
	for _, m := range markers {
		_ = os.Chtimes(m, now, now)
	}
}

// BeginRetry marks a failed arrival as running again.
func (a *WorkspaceArrival) BeginRetry() {
	if a == nil {
		return
	}
	a.mu.Lock()
	if !a.ready {
		a.running = true
		a.err = nil
		a.lastProgress = time.Now()
		a.broadcastLocked()
	}
	a.mu.Unlock()
}

// Retry invokes the arrival's retry callback when failed.
func (a *WorkspaceArrival) Retry() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	fn := a.retry
	ready := a.ready
	a.mu.Unlock()
	if ready || fn == nil {
		return false
	}
	fn()
	return true
}

// Fail records a background fetch failure. The gate remains closed so tools
// never run against a partial workspace tree (C3, C4).
func (a *WorkspaceArrival) Fail(err error) {
	if a == nil {
		return
	}
	a.mu.Lock()
	if !a.ready {
		a.running = false
		a.err = err
		a.failedAt = time.Now()
		a.broadcastLocked()
	}
	a.mu.Unlock()
}

// Complete marks the workspace as fully arrived and wakes waiting callers.
func (a *WorkspaceArrival) Complete() {
	if a == nil {
		return
	}
	arrivals.Lock()
	a.mu.Lock()
	a.ready = true
	a.running = false
	a.err = nil
	roots := a.roots
	markers := a.markers
	a.roots = nil
	a.markers = nil
	a.broadcastLocked()
	a.mu.Unlock()
	for _, k := range roots {
		if arrivals.byPath[k] == a {
			delete(arrivals.byPath, k)
		}
	}
	arrivals.Unlock()
	for _, m := range markers {
		_ = os.Remove(m)
	}
}

// Ready reports whether the workspace has arrived.
func (a *WorkspaceArrival) Ready() bool {
	if a == nil {
		return true
	}
	return a.isReady()
}

// Err returns the current background failure error, if any.
func (a *WorkspaceArrival) Err() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.err
}

func (a *WorkspaceArrival) bound() time.Duration {
	if a.stallBound != nil {
		if d := a.stallBound(); d > 0 {
			return d
		}
	}
	if DefaultArrivalStallBound > 0 {
		return DefaultArrivalStallBound
	}
	return 2 * time.Minute
}

// Wait blocks until the workspace has arrived and been put in place, bounded by
// the move stall guard. When the background fetch fails, Wait keeps waiting up
// to the bound so a retry can still complete the tree, and then reports why in
// plain words rather than letting a tool run on a partial tree (C3, C4).
func (a *WorkspaceArrival) Wait(ctx context.Context) error {
	if a == nil {
		return nil
	}
	for {
		a.mu.Lock()
		if a.ready {
			a.mu.Unlock()
			return nil
		}
		ch := a.notify
		running := a.running
		err := a.err
		last := a.lastProgress
		failedAt := a.failedAt
		a.mu.Unlock()

		bound := a.bound()
		var wait time.Duration
		if running {
			wait = bound - time.Since(last)
			if wait <= 0 {
				return fmt.Errorf("workspace files stalled: nothing came through for %s", bound)
			}
		} else if err != nil {
			wait = bound - time.Since(failedAt)
			if wait <= 0 {
				return fmt.Errorf("workspace files could not arrive: %w", err)
			}
		} else {
			wait = bound
		}

		timer := time.NewTimer(wait)
		select {
		case <-ch:
			timer.Stop()
		case <-timer.C:
			a.mu.Lock()
			ready := a.ready
			curRunning := a.running
			curErr := a.err
			curLast := a.lastProgress
			a.mu.Unlock()
			if ready {
				return nil
			}
			if !curRunning && curErr != nil {
				return fmt.Errorf("workspace files could not arrive: %w", curErr)
			}
			if curRunning && time.Since(curLast) >= bound {
				return fmt.Errorf("workspace files stalled: nothing came through for %s", bound)
			}
		case <-ctx.Done():
			timer.Stop()
			a.mu.Lock()
			curErr := a.err
			a.mu.Unlock()
			if curErr != nil {
				return fmt.Errorf("workspace files could not arrive: %w", curErr)
			}
			return ctx.Err()
		}
	}
}
