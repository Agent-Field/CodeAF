package telemetry

import (
	"os"
	"path/filepath"
	"strings"
)

// InstallIDHash returns sha256 of the install id, creating the id first if
// this machine has never sent anything. The raw id never leaves this function
// and is never returned; the hash is the only identity the wire ever sees.
func InstallIDHash() string {
	return hashHex(installID())
}

// installID reads ~/.codeaf/telemetry/install_id, creating it on first use:
// 32 random bytes as hex, a directory at 0700 and the file at 0600. A file
// that cannot be read and cannot be created is answered with a fresh random
// id for this call alone — telemetry loses its continuity but never gains a
// reason to fail the run, and nothing is written saying an id that does not
// exist.
func installID() string {
	path := telemetryFile("install_id")
	if body, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(body)); len(id) == 64 {
			return id
		}
	}
	id := randomHex(32)
	if err := writeFilePrivate(path, []byte(id)); err != nil {
		return id
	}
	_ = os.Chmod(path, 0o600)
	return id
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
	switch method {
	case "script", "source":
	default:
		method = "unknown"
	}
	body, _ := jsonMarshal(map[string]string{"install_method": method})
	_ = writeAtomicPrivate(telemetryFile("install.json"), body)
}

// telemetryFile names a file inside the telemetry directory, under the state
// root home.Dir answers for.
func telemetryFile(name string) string {
	return filepath.Join(telemetryDir(), name)
}
