package telemetry

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// InstallIDHash returns sha256 of the install id, creating the id first if
// this machine has never sent anything. The raw id never leaves this function
// and is never returned; the hash is the only identity the wire ever sees.
func InstallIDHash() string {
	return hashHex(installID())
}

// installID reads ~/.codeaf/telemetry/install_id, creating it on first use:
// 32 random bytes as hex, a directory at 0700 and the file at 0600. The create
// is O_EXCL, so two codeaf processes starting together cannot each mint an id
// and quietly use one that was never stored — the loser of the race reads the
// winner's id instead. A file that cannot be read and cannot be created is
// answered with a fresh random id for this call alone — telemetry loses its
// continuity but never gains a reason to fail the run, and nothing is written
// saying an id that does not exist.
func installID() string {
	path := telemetryFile("install_id")
	if body, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(body)); len(id) == 64 {
			return id
		}
	}
	id := randomHex(32)
	if err := ensureDir(); err != nil {
		return id
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	// Another process created it first: that id is this machine's id. It
	// may not have finished writing yet, so the read is retried briefly
	// rather than answering an id that was never stored.
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			for attempt := 0; attempt < 50; attempt++ {
				if stored := readStoredInstallID(path); stored != "" {
					return stored
				}
				time.Sleep(time.Millisecond)
			}
		}
		return id
	}
	defer file.Close()
	if _, err := file.WriteString(id); err != nil {
		return id
	}
	return id
}

// readStoredInstallID reads the id another process stored, answering "" when
// there is nothing usable — a race lost to a process that has not finished
// writing yet, or a file this process cannot read.
func readStoredInstallID(path string) string {
	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(body))
}

// FirstRunMarkerName is the file that records that first_run was sent for the
// current install id. It is exported for the status command the wiring job
// adds.
const FirstRunMarkerName = "first_run"

// firstRunMarkerPath is where the once-per-install marker lives.
func firstRunMarkerPath() string { return telemetryFile(FirstRunMarkerName) }

// FirstRunPending reports whether this install id has yet to send first_run.
// A marker naming a different install id — the id was deleted, the state moved
// — counts as pending again, because a new identity has not sent anything.
func FirstRunPending() bool {
	body, err := os.ReadFile(firstRunMarkerPath())
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(body)) != installID()
}

// MarkFirstRunSent writes the marker so first_run is emitted once per install
// id. A failure is silent: the worst case is one extra first_run after a
// crash, which no person ever sees.
func MarkFirstRunSent() {
	_ = writeAtomicPrivate(firstRunMarkerPath(), []byte(installID()))
}

// WriteInstallMethod records how codeaf arrived on this machine, as the
// installer would. Only the two contract values are kept; anything else is
// stored as unknown so a stray word from an installer cannot become a prop.
func WriteInstallMethod(method string) {
	path := telemetryFile("install.json")
	if body, err := os.ReadFile(path); err == nil && parsesAsObject(body) {
		return
	}
	switch method {
	case "script", "source":
	default:
		method = "unknown"
	}
	body, _ := jsonMarshal(map[string]string{"install_method": method})
	_ = writeAtomicPrivate(path, body)
}

// parsesAsObject reports whether body is a JSON object: the shape every
// record of this directory is written in, including the installer's own.
func parsesAsObject(body []byte) bool {
	var probe map[string]any
	return json.Unmarshal(body, &probe) == nil
}

// telemetryFile names a file inside the telemetry directory, under the state
// root home.Dir answers for.
func telemetryFile(name string) string {
	return filepath.Join(telemetryDir(), name)
}
