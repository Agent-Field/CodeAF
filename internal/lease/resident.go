// Package lease elects the one process currently serving as a resident for a
// durable Aforge store. The lock is only coordination; the journal remains the
// source of truth and another process may take the role as soon as it is free.
package lease

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/buildinfo"
	"github.com/Agent-Field/aforge-v2/internal/filelock"
)

const (
	// legacyLockName is the directory-wide lock this package used before the
	// role was keyed to the store it serves. It is only ever read now — see
	// legacyHolder — and never taken.
	legacyLockName  = "resident.lock"
	lockSuffix      = ".resident.lock"
	maxPayloadBytes = 16 << 10

	// StuckAfter is how long a holder may go without stamping a completed pass
	// before the role is considered abandoned. It is several resident poll
	// intervals over, deliberately: a busy pass, a slow model call and a paused
	// laptop all take longer than one interval, and taking the role away from a
	// process that is merely working would be far worse than waiting.
	StuckAfter = 5 * time.Minute
)

// Resident describes the process whose open file descriptor currently holds
// resident.lock. A payload left behind without a live flock is stale and is
// deliberately ignored by ProbeResident.
type Resident struct {
	PID        int       `json:"pid"`
	Host       string    `json:"host"`
	Surface    string    `json:"surface"`
	AcquiredAt time.Time `json:"acquired_at"`
	// Store is the database this holder is the resident for. It is written so a
	// process that finds the role taken can say which store it was taken for —
	// a lock that names nothing is exactly what made a whole grid of headless
	// runs sit at nodes:0 for their entire wall with no way to see why. An empty
	// value means an older build wrote the payload.
	Store string `json:"store,omitempty"`
	// LastTick is when the holder last finished a resident pass. Zero means the
	// holder never said — an flock proves a process is alive, never that it is
	// still doing the work — and silence is deliberately read as unknown rather
	// than as dead, so a surface that does not stamp is never taken from.
	LastTick time.Time `json:"last_tick,omitempty"`

	// Build identifies the binary the holder is running. A missing stamp means
	// the holder is an older build that never wrote one, and — exactly as with
	// LastTick — silence is read as unknown rather than as old.
	Build Build `json:"build,omitempty"`

	// Stuck is derived at probe time and never serialized: the holder is alive,
	// has stamped a pass at some point, and has not stamped one since.
	Stuck bool `json:"-"`
}

// Build is the identity of a running binary, kept deliberately small.
//
// The obvious signal is the shared build revision, and it rides along here
// because it is the only part a human reading the lock file can act on. It
// cannot be the deciding one: a `go build` of a tree with
// uncommitted work stamps the revision of the commit underneath it, or nothing
// at all, so the rebuild that actually caused a handover to be needed is the
// one case where two binaries share a revision. Two revisions also do not
// order — deciding which of them is newer needs the repository, which a lock
// file does not have.
//
// The executable's own mtime has none of those problems. It always exists, a
// rebuild always moves it forward, and it compares with a single operator. So
// mtime decides, size disambiguates a same-second rebuild, and the revision is
// a label.
type Build struct {
	ModTime  time.Time `json:"mod_time,omitempty"`
	Size     int64     `json:"size,omitempty"`
	Revision string    `json:"revision,omitempty"`
}

// LocalBuild stamps the binary this process is running. Everything it reads can
// fail on an exotic platform, and every failure degrades to the zero value —
// which the comparison below reads as "would not claim to be newer".
func LocalBuild() Build {
	build := Build{Revision: localRevision()}
	executable, err := os.Executable()
	if err != nil {
		return build
	}
	info, err := os.Stat(executable)
	if err != nil {
		return build
	}
	build.ModTime = info.ModTime().UTC()
	build.Size = info.Size()
	return build
}

// NewerThan asks whether this build should be allowed to displace another.
// It answers no whenever it cannot answer yes: an unstamped holder is an older
// binary that predates handover entirely, and taking the role from it on a
// guess would be the same mistake as treating a silent heartbeat as a dead
// process. Those residents are reclaimed by the stale-heartbeat path instead.
func (b Build) NewerThan(other Build) bool {
	if b.ModTime.IsZero() || other.ModTime.IsZero() {
		return false
	}
	if b.ModTime.After(other.ModTime) {
		return true
	}
	// A same-second rebuild is common on a fast machine with a coarse
	// filesystem timestamp. A different size is then the only honest evidence
	// that the binary changed at all, and it is evidence of difference rather
	// than of direction — so it counts only when the revisions also disagree.
	return b.ModTime.Equal(other.ModTime) && b.Size != other.Size && b.Revision != other.Revision
}

// localRevision reads the same source identity every other build surface uses.
// An empty string is the ordinary answer for an unstamped test binary with no
// useful Go build record.
func localRevision() string {
	return buildinfo.Revision()
}

// LockPath is where the lock for one store lives. It is keyed to the store
// file, not to the directory holding it: two databases that happen to share a
// directory are two stores, they need two residents, and a directory-wide lock
// made the second one wait on a brain that was serving somebody else's journal.
func LockPath(store string) (string, error) {
	store = strings.TrimSpace(store)
	if store == "" {
		return "", fmt.Errorf("empty store path")
	}
	store = filepath.Clean(store)
	return filepath.Join(filepath.Dir(store), filepath.Base(store)+lockSuffix), nil
}

// AcquireResident attempts to become the resident for one store. On success
// heldBy is nil and release relinquishes the role. If another live process
// holds the lock, release is nil and heldBy describes that process.
func AcquireResident(store, surface string) (release func() error, heldBy *Resident, err error) {
	path, err := LockPath(store)
	if err != nil {
		return nil, nil, fmt.Errorf("acquire resident: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("acquire resident: create store directory: %w", err)
	}
	// A live holder of the old directory-wide lock is an older binary, and it
	// may well be serving this very store. Taking the store-keyed lock beside it
	// would put two brains on one journal, which is the one thing this lease
	// exists to prevent, so the older process keeps the role until it exits.
	if legacy := legacyHolder(dir); legacy != nil && !legacy.Stuck {
		return nil, legacy, nil
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("acquire resident: open lock: %w", err)
	}
	if err := filelock.Lock(file, true, true); err != nil {
		if !lockBusy(err) {
			_ = file.Close()
			return nil, nil, fmt.Errorf("acquire resident: lock: %w", err)
		}
		holder, readErr := readSteadyResident(file)
		_ = file.Close()
		if readErr != nil {
			return nil, nil, fmt.Errorf("acquire resident: read holder: %w", readErr)
		}
		markStuck(holder, time.Now())
		return nil, holder, nil
	}

	host, _ := os.Hostname()
	resident := Resident{
		PID:        os.Getpid(),
		Host:       strings.TrimSpace(host),
		Surface:    strings.TrimSpace(surface),
		AcquiredAt: time.Now().UTC(),
		Store:      absoluteStore(store),
		Build:      LocalBuild(),
	}
	if resident.Surface == "" {
		resident.Surface = "unknown"
	}
	if err := writeResident(file, resident); err != nil {
		_ = filelock.Unlock(file)
		_ = file.Close()
		return nil, nil, fmt.Errorf("acquire resident: write holder: %w", err)
	}

	var once sync.Once
	var releaseErr error
	release = func() error {
		once.Do(func() {
			unlockErr := filelock.Unlock(file)
			closeErr := file.Close()
			releaseErr = errors.Join(unlockErr, closeErr)
		})
		return releaseErr
	}
	return release, nil, nil
}

// ProbeResident reports the live holder of resident.lock. It never trusts the
// JSON by itself: if a non-blocking flock succeeds, any payload is stale and
// the resident role is free.
func ProbeResident(store string) (*Resident, error) {
	path, err := LockPath(store)
	if err != nil {
		return nil, fmt.Errorf("probe resident: %w", err)
	}
	if legacy := legacyHolder(filepath.Dir(path)); legacy != nil && !legacy.Stuck {
		return legacy, nil
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("probe resident: open lock: %w", err)
	}
	defer file.Close()

	if err := filelock.Lock(file, true, true); err == nil {
		if unlockErr := filelock.Unlock(file); unlockErr != nil {
			return nil, fmt.Errorf("probe resident: unlock probe: %w", unlockErr)
		}
		return nil, nil
	} else if !lockBusy(err) {
		return nil, fmt.Errorf("probe resident: lock: %w", err)
	}
	holder, err := readSteadyResident(file)
	if err != nil {
		return nil, fmt.Errorf("probe resident: read holder: %w", err)
	}
	markStuck(holder, time.Now())
	return holder, nil
}

// NoteResidentTick stamps a completed resident pass onto the lock the calling
// process holds. It is deliberately stateless and deliberately fussy about who
// may write: only the holder stamps its own liveness, so a second process
// cannot make a wedged resident look alive.
func NoteResidentTick(store string, at time.Time) error {
	path, err := LockPath(store)
	if err != nil {
		return fmt.Errorf("note resident tick: %w", err)
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("note resident tick: open lock: %w", err)
	}
	defer file.Close()

	holder, err := readSteadyResident(file)
	if err != nil {
		return fmt.Errorf("note resident tick: read holder: %w", err)
	}
	if holder.PID != os.Getpid() {
		return nil
	}
	holder.LastTick = at.UTC()
	if err := writeResident(file, *holder); err != nil {
		return fmt.Errorf("note resident tick: write holder: %w", err)
	}
	return nil
}

// absoluteStore names the store a holder serves as plainly as it can. An
// absolute path is what makes the diagnostic actionable — the reader is in some
// other directory by definition — and a path that cannot be resolved is written
// as it was given rather than dropped.
func absoluteStore(store string) string {
	store = strings.TrimSpace(store)
	absolute, err := filepath.Abs(store)
	if err != nil {
		return filepath.Clean(store)
	}
	return absolute
}

// legacyHolder reports a live holder of the pre-store-keyed lock, or nil. It is
// the whole of the compatibility story: nothing writes that lock any more, so a
// process holding it is a binary from before the key changed, and the only
// question worth asking about it is whether it is still alive.
func legacyHolder(dir string) *Resident {
	file, err := os.OpenFile(filepath.Join(dir, legacyLockName), os.O_RDWR, 0o600)
	if err != nil {
		return nil
	}
	defer file.Close()
	if err := filelock.Lock(file, true, true); err == nil {
		_ = filelock.Unlock(file)
		return nil
	} else if !lockBusy(err) {
		return nil
	}
	holder, err := readSteadyResident(file)
	if err != nil {
		return nil
	}
	markStuck(holder, time.Now())
	return holder
}

// markStuck decides whether a live holder has stopped serving. A holder that
// has never stamped a pass is left alone: the flock is the only thing we know
// about it, and treating "said nothing" as "died" would let a wake pass run
// beside a perfectly healthy resident.
func markStuck(holder *Resident, now time.Time) {
	if holder == nil || holder.LastTick.IsZero() {
		return
	}
	holder.Stuck = now.Sub(holder.LastTick) > StuckAfter
}

func lockBusy(err error) bool {
	return filelock.IsBusy(err)
}

// readSteadyResident reads the payload of a lock somebody else is holding, and
// tolerates catching them in the act of writing it.
//
// A holder rewrites the payload without any lock of its own — it is the holder,
// so nothing else may write — but a reader is not excluded, and the moments
// after a role changes hands are exactly when several processes are reading a
// lock that is being rewritten. A reader that landed inside that window used to
// return "unexpected end of JSON input", which a promoting window read as a
// broken lock and gave up on. The write is one small syscall wide, so a couple
// of retries is the whole fix; only a payload that will not parse across all of
// them is genuinely corrupt.
const readAttempts = 5

func readSteadyResident(file *os.File) (*Resident, error) {
	var resident *Resident
	var err error
	for attempt := 0; attempt < readAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Millisecond)
		}
		if resident, err = readResident(file); err == nil {
			return resident, nil
		}
	}
	return nil, err
}

func readResident(file *os.File) (*Resident, error) {
	reader := io.NewSectionReader(file, 0, maxPayloadBytes+1)
	payload, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if len(payload) > maxPayloadBytes {
		return nil, fmt.Errorf("lock payload exceeds %d bytes", maxPayloadBytes)
	}
	var resident Resident
	if err := json.Unmarshal(payload, &resident); err != nil {
		return nil, err
	}
	if resident.PID <= 0 {
		return nil, fmt.Errorf("lock payload has invalid pid %d", resident.PID)
	}
	return &resident, nil
}

func writeResident(file *os.File, resident Resident) error {
	payload, err := json.Marshal(resident)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	// Written before the file is shortened, never after. Truncating first
	// leaves a window in which a concurrent reader sees an empty lock and
	// concludes the payload is broken; this way the file is never shorter than
	// what is already in it, and the worst a reader can catch is a payload
	// halfway between two good ones — which the retry above rides out.
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := file.Write(payload); err != nil {
		return err
	}
	if err := file.Truncate(int64(len(payload))); err != nil {
		return err
	}
	return file.Sync()
}
