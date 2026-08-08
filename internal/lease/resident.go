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
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	residentLockName = "resident.lock"
	maxPayloadBytes  = 16 << 10

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
// The obvious signal is vcs.revision out of runtime/debug.ReadBuildInfo, and it
// rides along here because it is the only part a human reading the lock file
// can act on. It cannot be the deciding one: a `go build` of a tree with
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

// localRevision reads the commit this binary was built from, if the toolchain
// recorded one. An empty string is the ordinary answer for a `go run`, a test
// binary, or a build from a tree without git.
func localRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return strings.TrimSpace(setting.Value)
		}
	}
	return ""
}

// AcquireResident attempts to become the resident for the store directory.
// On success heldBy is nil and release relinquishes the role. If another live
// process holds the lock, release is nil and heldBy describes that process.
func AcquireResident(dir, surface string) (release func() error, heldBy *Resident, err error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, nil, fmt.Errorf("acquire resident: empty store directory")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("acquire resident: create store directory: %w", err)
	}
	path := filepath.Join(dir, residentLockName)
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("acquire resident: open lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if !lockBusy(err) {
			_ = file.Close()
			return nil, nil, fmt.Errorf("acquire resident: lock: %w", err)
		}
		holder, readErr := readResident(file)
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
		Build:      LocalBuild(),
	}
	if resident.Surface == "" {
		resident.Surface = "unknown"
	}
	if err := writeResident(file, resident); err != nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
		return nil, nil, fmt.Errorf("acquire resident: write holder: %w", err)
	}

	var once sync.Once
	var releaseErr error
	release = func() error {
		once.Do(func() {
			unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
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
func ProbeResident(dir string) (*Resident, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("probe resident: empty store directory")
	}
	path := filepath.Join(dir, residentLockName)
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("probe resident: open lock: %w", err)
	}
	defer file.Close()

	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		if unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN); unlockErr != nil {
			return nil, fmt.Errorf("probe resident: unlock probe: %w", unlockErr)
		}
		return nil, nil
	} else if !lockBusy(err) {
		return nil, fmt.Errorf("probe resident: lock: %w", err)
	}
	holder, err := readResident(file)
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
func NoteResidentTick(dir string, at time.Time) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return fmt.Errorf("note resident tick: empty store directory")
	}
	path := filepath.Join(dir, residentLockName)
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("note resident tick: open lock: %w", err)
	}
	defer file.Close()

	holder, err := readResident(file)
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
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
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
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := file.Write(payload); err != nil {
		return err
	}
	return file.Sync()
}
