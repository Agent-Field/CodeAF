package cell

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/fsreplace"
)

// DefaultArrivalStallBound is the fallback stall bound for waiting on arriving
// workspace files when a caller supplies none.
var DefaultArrivalStallBound = 2 * time.Minute

// ErrWorkspaceArrivalAbandoned is what a waiter gets when the files of a chat
// failed to arrive and the process that was bringing them is gone. Nothing is
// left to retry them, and the tree on disk is a partial one, so every caller is
// refused until a new arrival replaces the failure (BeginWorkspaceArrival over
// the same cell) and completes. Match it with errors.Is.
var ErrWorkspaceArrivalAbandoned = errors.New("the chat's files did not finish arriving")

// arrivalMarkerRecord is what the owner of an arrival leaves on disk for every
// other process: who it is (so a waiter can tell a running owner from a dead
// one) and, once the files failed to arrive, that they failed and why. A marker
// only ever exists in the local store, never in the tree, so adding fields to it
// is not a wire or persisted-format change.
type arrivalMarkerRecord struct {
	PID   int    `json:"pid"`
	Start string `json:"start,omitempty"`
	// Failed says the fetch failed and its owner is still there to retry it. A
	// failed marker does not age out the way a running one does: the files have
	// not arrived, and that stays true until a retry succeeds.
	Failed bool   `json:"failed,omitempty"`
	Reason string `json:"reason,omitempty"`
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
	// abandoned says this process gave the arrival up without its files: the
	// failure stays on disk, owned by nobody, until a new arrival replaces it.
	abandoned bool
	// owner is this process's own identity as the markers record it.
	owner arrivalMarkerRecord
	// fileMu serializes every write and removal of the markers, so a late write
	// can never bring back the marker of an arrival that already completed.
	// Lock order: fileMu before mu; arrivals before mu.
	fileMu sync.Mutex
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

// processStatus says whether the process pid is running and, when the platform
// can say, a token for when it started, so a pid handed out again to some other
// process is not mistaken for the owner. The rule on every platform is the same:
// only proof that the process is gone says "dead". A probe that cannot tell
// answers "alive", and then the stall bound decides, because calling a running
// owner dead lets another process run tools on files that are still arriving.
func processStatus(pid int) (startToken string, alive bool) {
	if pid <= 0 {
		return "", false
	}
	return platformProcessStatus(pid)
}

// signalProbeAlive reads the answer of a signal-0 probe of a process. Only "no
// such process" proves a death. Permission denied means it exists, and any
// other answer (Windows, for one, answers that signals are not supported) means
// the probe could not tell, which is not the same as dead.
func signalProbeAlive(err error) bool {
	if err == nil {
		return true
	}
	return !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH)
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

// ownArrivalRecord is this process, as an arrival marker names its owner.
func ownArrivalRecord() arrivalMarkerRecord {
	pid := os.Getpid()
	start, _ := processStatus(pid)
	return arrivalMarkerRecord{PID: pid, Start: start}
}

// writeArrivalMarkerRecord replaces the marker whole. It is written to a
// neighbouring file and renamed over the marker, so a reader in another process
// sees the old marker or the new one and never half of one, which it would take
// for a dead owner's junk and delete.
func writeArrivalMarkerRecord(path string, rec arrivalMarkerRecord) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".arriving-write-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := fsreplace.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// arrivalMarkerView is what a process that does not own an arrival makes of its
// marker.
type arrivalMarkerView struct {
	// present is false when there is no marker or its owner is gone: nothing is
	// arriving. It is true whenever an owner may still be putting files in place.
	present bool
	// unreadable says the marker exists but could not be read right now. The
	// owner is then unknown, which is never taken to mean gone.
	unreadable bool
	failed     bool
	reason     string
	// abandoned says the arrival failed and its owner is gone, so nobody is left
	// to retry it. The failure outlives its owner: the files did not arrive, and
	// that stays true until a new arrival over this cell completes.
	abandoned bool
	// age is how long ago the owner last wrote to the marker: the last progress
	// of a running arrival, or the moment a failed one failed.
	age time.Duration
}

// viewArrivalMarker reads the marker at path. A RUNNING marker whose owner is
// dead, or that is not a marker at all, is removed on the spot: that is what
// keeps a crashed owner from stalling everyone for the whole stall bound. A
// FAILED marker whose owner is dead is kept and reported as abandoned instead,
// because it records that the files did not arrive: removing it would hand a
// partial tree to every seal and tool in every process, and a seal could publish
// it as the chat's files.
func viewArrivalMarker(path string) arrivalMarkerView {
	f, err := fsreplace.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return arrivalMarkerView{}
		}
		return arrivalMarkerView{present: true, unreadable: true}
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return arrivalMarkerView{present: true, unreadable: true}
	}
	if info.IsDir() {
		return arrivalMarkerView{}
	}
	raw, err := io.ReadAll(f)
	if err != nil {
		return arrivalMarkerView{present: true, unreadable: true}
	}
	var rec arrivalMarkerRecord
	if json.Unmarshal(raw, &rec) != nil {
		_ = fsreplace.Remove(path)
		return arrivalMarkerView{}
	}
	age := time.Since(info.ModTime())
	if !arrivalOwnerAlive(rec) {
		if rec.Failed {
			return arrivalMarkerView{present: true, failed: true, abandoned: true, reason: rec.Reason, age: age}
		}
		_ = fsreplace.Remove(path)
		return arrivalMarkerView{}
	}
	return arrivalMarkerView{present: true, failed: rec.Failed, reason: rec.Reason, age: age}
}

// validArrivalMarker reports whether the marker at path still holds the gate
// shut. A running arrival holds it while its owner lives and made progress
// within bound. A failed one holds it however old the failure is, and whether or
// not its owner lives: the files have not arrived, and letting the bound pass for
// it would send a seal in another process onto a partial or empty tree.
func validArrivalMarker(path string, bound time.Duration) bool {
	v := viewArrivalMarker(path)
	if !v.present {
		return false
	}
	return v.failed || v.unreadable || v.age < bound
}

// arrivalFailure is the refusal for files that could not arrive, in the words
// the in-process gate uses, so every process says the same thing.
func arrivalFailure(reason string) error {
	if reason == "" {
		return errors.New("workspace files could not arrive")
	}
	return fmt.Errorf("workspace files could not arrive: %s", reason)
}

// abandonedArrivalError is the refusal for files that failed to arrive after
// the window that was bringing them closed. It says what happened, and what the
// person does about it, in the words of the person's own screen, and it matches
// [ErrWorkspaceArrivalAbandoned].
type abandonedArrivalError struct{ reason string }

func (e *abandonedArrivalError) Error() string {
	const next = "the window that was fetching them has closed, so move the chat here again to fetch them"
	if e.reason == "" {
		return fmt.Sprintf("workspace files could not arrive: %v; %s", ErrWorkspaceArrivalAbandoned, next)
	}
	return fmt.Sprintf("workspace files could not arrive: %s (%v); %s", e.reason, ErrWorkspaceArrivalAbandoned, next)
}

func (e *abandonedArrivalError) Is(target error) bool { return target == ErrWorkspaceArrivalAbandoned }

func arrivalAbandoned(reason string) error { return &abandonedArrivalError{reason: reason} }

// AbandonedReason is why the files of an abandoned arrival did not arrive, in
// the words the first failure used, when err is the refusal for one.
func AbandonedReason(err error) (string, bool) {
	var abandoned *abandonedArrivalError
	if errors.As(err, &abandoned) {
		return abandoned.reason, true
	}
	return "", false
}

// arrivalStall is the refusal for files that stopped coming.
func arrivalStall(bound time.Duration) error {
	return fmt.Errorf("workspace files stalled: nothing came through for %s", bound)
}

// defaultArrivalBound is the stall bound a process uses for an arrival it does
// not own.
func defaultArrivalBound() time.Duration {
	if DefaultArrivalStallBound > 0 {
		return DefaultArrivalStallBound
	}
	return 2 * time.Minute
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
		owner:        ownArrivalRecord(),
	}
	arrivals.Lock()
	defer arrivals.Unlock()
	for _, p := range []string{cellRoot, workspace} {
		for _, k := range arrivalKeys(p) {
			a.roots = append(a.roots, k)
			arrivals.byPath[k] = a
		}
		if m := arrivalMarkerPath(p); m != "" {
			if writeArrivalMarkerRecord(m, a.markerRecordLocked()) == nil {
				a.markers = append(a.markers, m)
			}
		}
	}
	return a
}

// markerRecordLocked is what the markers of this arrival say right now: that
// its owner is here and either still fetching or failed, and why. The caller
// holds a.mu, or owns a that nobody else has seen yet.
func (a *WorkspaceArrival) markerRecordLocked() arrivalMarkerRecord {
	rec := a.owner
	if !a.running && !a.ready {
		rec.Failed = true
		if a.err != nil {
			rec.Reason = a.err.Error()
		}
	}
	return rec
}

// writeMarkers brings every marker of this arrival up to date with its state,
// so a process that does not own the arrival hears of a failure, and of the
// retry that cleared it, as well as of progress.
func (a *WorkspaceArrival) writeMarkers() {
	a.fileMu.Lock()
	defer a.fileMu.Unlock()
	a.mu.Lock()
	if a.ready || a.abandoned {
		a.mu.Unlock()
		return
	}
	rec := a.markerRecordLocked()
	markers := append([]string(nil), a.markers...)
	a.mu.Unlock()
	for _, m := range markers {
		_ = writeArrivalMarkerRecord(m, rec)
	}
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
	bound := defaultArrivalBound()
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

// AbandonedArrival is the refusal a seal or a tool gets for the chat at any of
// paths when its files failed to arrive and nothing is left to bring them: the
// process that was fetching them is gone, so the failure stays on disk and
// every workspace tool and seal in every process refuses until a new arrival
// replaces it. It matches [ErrWorkspaceArrivalAbandoned] and carries the
// failure's first words ([AbandonedReason]). Every other answer is nil.
//
// IT NEVER WAITS, which is what an open needs and [WaitWorkspace] cannot give
// it: that waits on a running arrival for as long as its owner lives, and an
// open must not stand behind the files it is about to let arrive. It answers nil
// for an arrival a live owner still has in flight in this process or another,
// for a failure whose owner can still retry it, and for a chat with no arrival
// at all, so the only chats it names are the ones that will stay refused unless
// somebody starts the fetch again.
func AbandonedArrival(paths ...string) error {
	if LookupWorkspaceArrival(paths...) != nil {
		return nil
	}
	for _, p := range paths {
		if m := arrivalMarkerPath(p); m != "" {
			if v := viewArrivalMarker(m); v.abandoned {
				return arrivalAbandoned(v.reason)
			}
		}
	}
	return nil
}

// WaitWorkspace waits for any arriving workspace covering paths to be put in
// place. It returns nil immediately when no background arrival is pending, in
// this process or in another one that still lives.
//
// A running arrival is waited on until it completes or goes silent for the stall
// bound. A FAILED one refuses, with the failure's reason, for as long as its
// owner lives: the files have not arrived, so a seal or a tool in any process
// must never be handed a partial tree, and the bound only decides how long a
// retry is given a chance before the refusal is returned. It ends when a retry
// succeeds. If the owner process is gone the failure is ABANDONED: it is refused
// at once with [ErrWorkspaceArrivalAbandoned], and stays refused, until a new
// arrival over the same cell replaces it. (A running owner that crashed is a
// different case: its marker is removed and nobody waits on it.)
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
	bound := defaultArrivalBound()
	var unreadableSince time.Time
	ticker := time.NewTicker(15 * time.Millisecond)
	defer ticker.Stop()
	for {
		v := viewArrivalMarker(marker)
		if !v.present {
			return nil
		}
		switch {
		case v.abandoned:
			// The owner is gone, so no retry is coming and waiting would change
			// nothing: refuse at once, and keep refusing until a new arrival over
			// this cell replaces the marker.
			return arrivalAbandoned(v.reason)
		case v.failed:
			// The owner is alive and the files have not arrived. A retry may still
			// complete them, so a failure this recent is waited on; one older than
			// the bound is refused at once. Either way nobody gets to proceed: the
			// failure has no clock that runs out.
			if v.age >= bound {
				return arrivalFailure(v.reason)
			}
		case v.unreadable:
			// The owner cannot be told apart from a dead one, so it is taken as
			// alive, and the stall bound decides how long to wait on it.
			if unreadableSince.IsZero() {
				unreadableSince = time.Now()
			}
			if time.Since(unreadableSince) >= bound {
				return arrivalStall(bound)
			}
		default:
			unreadableSince = time.Time{}
			if v.age >= bound {
				return arrivalStall(bound)
			}
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
			if writeArrivalMarkerRecord(m, a.markerRecordLocked()) == nil {
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
	running := a.running
	markers := append([]string(nil), a.markers...)
	a.broadcastLocked()
	a.mu.Unlock()
	if !running {
		// Late traffic from a fetch that already failed is not progress: the
		// failed marker's age is when it failed, and that must not be pushed back.
		return
	}
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
	retrying := !a.ready && !a.abandoned
	if retrying {
		a.running = true
		a.err = nil
		a.lastProgress = time.Now()
		a.broadcastLocked()
	}
	a.mu.Unlock()
	if retrying {
		// The old failure is over: other processes wait on a running arrival again,
		// and hear of the new failure if this attempt fails too.
		a.writeMarkers()
	}
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
// never run against a partial workspace tree (C3, C4), in this process and in
// every other: the failure and its reason go into the marker, which then holds
// the gate shut until a retry succeeds or this process is gone, however long
// that takes.
func (a *WorkspaceArrival) Fail(err error) {
	if a == nil {
		return
	}
	a.mu.Lock()
	failed := !a.ready && !a.abandoned
	if failed {
		a.running = false
		a.err = err
		a.failedAt = time.Now()
		a.broadcastLocked()
	}
	a.mu.Unlock()
	if failed {
		a.writeMarkers()
	}
}

// Abandon gives an arrival up WITHOUT its files, for a take that began a
// replacement arrival over an abandoned failure and then could not go on. The
// failure is not forgotten and the files are not declared arrived: the marker
// goes back to being a failed arrival whose owner is nobody, which refuses every
// waiter in every process (this one included) until a new arrival over the same
// cell replaces it. Complete is for files that arrived.
func (a *WorkspaceArrival) Abandon(reason string) {
	if a == nil {
		return
	}
	arrivals.Lock()
	a.mu.Lock()
	if a.ready || a.abandoned {
		a.mu.Unlock()
		arrivals.Unlock()
		return
	}
	a.abandoned = true
	a.running = false
	a.err = errors.New(reason)
	roots := a.roots
	markers := append([]string(nil), a.markers...)
	a.roots = nil
	a.broadcastLocked()
	a.mu.Unlock()
	for _, k := range roots {
		if arrivals.byPath[k] == a {
			delete(arrivals.byPath, k)
		}
	}
	arrivals.Unlock()
	a.fileMu.Lock()
	defer a.fileMu.Unlock()
	for _, m := range markers {
		_ = writeArrivalMarkerRecord(m, arrivalMarkerRecord{PID: 0, Failed: true, Reason: reason})
	}
}

// Complete marks the workspace as fully arrived and wakes waiting callers.
func (a *WorkspaceArrival) Complete() {
	if a == nil {
		return
	}
	arrivals.Lock()
	a.mu.Lock()
	if a.abandoned {
		// An abandoned arrival never brought its files: the failure it gave
		// back stays until a new arrival replaces it.
		a.mu.Unlock()
		arrivals.Unlock()
		return
	}
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
	a.fileMu.Lock()
	defer a.fileMu.Unlock()
	for _, m := range markers {
		_ = fsreplace.Remove(m)
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
	return defaultArrivalBound()
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
		if a.abandoned {
			reason := ""
			if a.err != nil {
				reason = a.err.Error()
			}
			a.mu.Unlock()
			return arrivalAbandoned(reason)
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
				return arrivalStall(bound)
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
				return arrivalStall(bound)
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
