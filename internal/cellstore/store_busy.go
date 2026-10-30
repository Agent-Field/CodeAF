package cellstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/home"
)

// ErrStoreBusy means another process holds the engine store's writer lock.
var ErrStoreBusy = errors.New("engine store is busy")

const (
	storeWriterLockName = "writer.lock"
	maxLockPayloadBytes = 4 << 10
)

// StoreHolder identifies the process holding a store writer lock.
type StoreHolder struct {
	PID     int    `json:"pid,omitempty"`
	Command string `json:"command,omitempty"`
	Lock    string `json:"lock,omitempty"`
}

// Describe formats the known parts of a holder and answers empty when neither
// command nor pid is known.
func (h StoreHolder) Describe() string {
	cmd := strings.TrimSpace(h.Command)
	switch {
	case cmd != "" && h.PID > 0:
		return fmt.Sprintf("%s, pid %d", cmd, h.PID)
	case cmd != "":
		return cmd
	case h.PID > 0:
		return fmt.Sprintf("pid %d", h.PID)
	default:
		return ""
	}
}

// StoreBusyError reports that DataDir's writer lock is held by another process.
type StoreBusyError struct {
	DataDir string
	Holder  StoreHolder
	Cause   error
}

func (e *StoreBusyError) Error() string {
	if who := e.Holder.Describe(); who != "" {
		return fmt.Sprintf("engine store is busy (held by %s)", who)
	}
	return ErrStoreBusy.Error()
}

func (e *StoreBusyError) Unwrap() []error {
	if e.Cause != nil {
		return []error{ErrStoreBusy, e.Cause}
	}
	return []error{ErrStoreBusy}
}

// SharedStoreDir returns the default shared engine store directory for the
// active state root.
func SharedStoreDir() string {
	return home.Join("v3", "stores", "shared")
}

// HoldStoreWriter acquires DataDir's writer lock and stamps this process as its
// holder until release runs.
func HoldStoreWriter(dataDir, command string) (func() error, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("empty store data directory")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(dataDir, storeWriterLockName)
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := filelock.Lock(file, true, true); err != nil {
		_ = file.Close()
		if filelock.IsBusy(err) {
			holder, _ := InspectStoreBusy(dataDir, "")
			return nil, &StoreBusyError{DataDir: dataDir, Holder: holder, Cause: err}
		}
		return nil, err
	}
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		cmd = formatHolderCommand(os.Args)
	}
	payload, _ := json.Marshal(StoreHolder{PID: os.Getpid(), Command: cmd})
	payload = append(payload, '\n')
	_, _ = file.Seek(0, io.SeekStart)
	_, _ = file.Write(payload)
	_ = file.Truncate(int64(len(payload)))
	_ = file.Sync()

	var once sync.Once
	var releaseErr error
	return func() error {
		once.Do(func() {
			removeErr := os.Remove(lockPath)
			if errors.Is(removeErr, os.ErrNotExist) {
				removeErr = nil
			}
			unlockErr := filelock.Unlock(file)
			closeErr := file.Close()
			releaseErr = errors.Join(removeErr, unlockErr, closeErr)
		})
		return releaseErr
	}, nil
}

// InspectStoreBusy checks whether another live process holds an advisory writer
// lock on dataDir. A daemon serving ownSocket is treated as this client's own
// transport rather than a competing holder.
func InspectStoreBusy(dataDir, ownSocket string) (StoreHolder, bool) {
	if strings.TrimSpace(dataDir) == "" {
		return StoreHolder{}, false
	}
	for _, lockPath := range storeLockCandidates(dataDir, ownSocket) {
		holder, busy := probeStoreLockBusy(lockPath, ownSocket)
		if busy {
			return holder, true
		}
	}
	return StoreHolder{}, false
}

func classifyStoreBusyError(dataDir, ownSocket string, err error) error {
	if err == nil {
		return nil
	}
	var existing *StoreBusyError
	if errors.As(err, &existing) {
		return err
	}
	if isEngineStoreBusyText(err.Error()) {
		holder, _ := InspectStoreBusy(dataDir, ownSocket)
		return &StoreBusyError{DataDir: dataDir, Holder: holder, Cause: err}
	}
	return err
}

func isEngineStoreBusyText(text string) bool {
	lower := strings.ToLower(text)
	for _, needle := range []string{
		"store writer is still busy",
		"engine store is busy",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func storeLockCandidates(dataDir, ownSocket string) []string {
	var paths []string
	add := func(matches ...string) {
		for _, m := range matches {
			if m != "" {
				paths = append(paths, m)
			}
		}
	}
	add(dataDir + ".lock")
	for _, pattern := range []string{
		filepath.Join(dataDir, "*.lock"),
		filepath.Join(dataDir, "store-v1", "*.lock"),
		filepath.Join(dataDir, "store-v1", "*", "*.lock"),
	} {
		matches, _ := filepath.Glob(pattern)
		add(matches...)
	}
	v3Dir := filepath.Dir(filepath.Dir(filepath.Clean(dataDir)))
	if filepath.Base(filepath.Dir(filepath.Clean(dataDir))) == "stores" {
		socks, _ := filepath.Glob(filepath.Join(v3Dir, "engine-*.sock.lock"))
		for _, sockLock := range socks {
			if ownSocket != "" && filepath.Clean(sockLock) == filepath.Clean(ownSocket+".lock") {
				continue
			}
			// Another engine daemon socket lock only blocks this store when no
			// socket is owned here or when inspecting stale locks across v3.
			if ownSocket == "" {
				add(sockLock)
			}
		}
	}
	return paths
}

func probeStoreLockBusy(lockPath, ownSocket string) (StoreHolder, bool) {
	if strings.HasSuffix(lockPath, ".sock.lock") {
		// An active engine daemon socket lock is the normal daemon lease, not a
		// stuck store writer, unless its socket file is missing.
		sock := strings.TrimSuffix(lockPath, ".lock")
		if !heldWithoutSocket(sock) {
			return StoreHolder{}, false
		}
	}
	file, err := os.OpenFile(lockPath, os.O_RDWR, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return StoreHolder{}, false
		}
		file, err = os.Open(lockPath)
		if err != nil {
			return StoreHolder{}, false
		}
	}
	defer file.Close()

	if err := filelock.Lock(file, true, true); err == nil {
		_ = filelock.Unlock(file)
		return StoreHolder{}, false
	} else if !filelock.IsBusy(err) {
		return StoreHolder{}, false
	}

	holder := lockHolderFromOS(lockPath)
	if holder.PID == os.Getpid() {
		return StoreHolder{}, false
	}
	if holder.PID > 0 && pidServesSocket(holder.PID, ownSocket) {
		return StoreHolder{}, false
	}
	if holder.PID == 0 || holder.Command == "" {
		recorded := readLockPayload(file)
		if recorded.PID == os.Getpid() {
			return StoreHolder{}, false
		}
		if recorded.PID > 0 && pidServesSocket(recorded.PID, ownSocket) {
			return StoreHolder{}, false
		}
		if holder.PID == 0 {
			holder.PID = recorded.PID
		}
		if holder.Command == "" {
			holder.Command = recorded.Command
		}
	}
	if holder.Command == "" && holder.PID > 0 {
		holder.Command = describeProcess(holder.PID)
	}
	holder.Lock = lockPath
	return holder, true
}

func readLockPayload(file *os.File) StoreHolder {
	raw, err := io.ReadAll(io.NewSectionReader(file, 0, maxLockPayloadBytes))
	if err != nil {
		return StoreHolder{}
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return StoreHolder{}
	}
	var holder StoreHolder
	if json.Unmarshal([]byte(text), &holder) == nil && (holder.PID > 0 || holder.Command != "") {
		return holder
	}
	if pid, err := strconv.Atoi(text); err == nil && pid > 0 {
		return StoreHolder{PID: pid}
	}
	return StoreHolder{}
}

func formatHolderCommand(argv []string) string {
	var clean []string
	for _, arg := range argv {
		arg = strings.TrimSpace(arg)
		if arg != "" {
			clean = append(clean, arg)
		}
	}
	if len(clean) == 0 {
		return ""
	}
	prog := filepath.Base(clean[0])
	if storageProgramName.MatchString(prog) {
		prog = "furrow"
	}
	if len(clean) > 1 && !strings.HasPrefix(clean[1], "-") && !strings.Contains(clean[1], string(filepath.Separator)) {
		return prog + " " + clean[1]
	}
	return prog
}
