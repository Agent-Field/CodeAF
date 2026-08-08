// Package home resolves the one directory aforge owns: its state root.
//
// Everything durable the resident keeps — the journal, the workspace, the CAS,
// the craft repo, measured profiles, the model catalog, the router ledger, the
// promoted skills shelf — lives under a single directory so that "where does
// aforge keep my things" has exactly one answer. That answer is ~/.aforge, and
// AFORGE_HOME moves it wholesale.
//
// The override exists for the same reason the directory exists: a disposable
// run — the UX suite driving the real binary, a second brain on the same
// laptop, a sandbox — must be able to move every file aforge writes without
// moving the user's HOME, and without a per-file flag for each of them. Narrow
// overrides that already exist (chat --db, AFORGE_PROFILE_DIR) still win where
// they apply; this only changes the default they fall back to.
package home

import (
	"os"
	"path/filepath"
	"strings"
)

// EnvVar names the override. It is exported so help text and doctor output can
// say the same word the code reads.
const EnvVar = "AFORGE_HOME"

// Dir is the state root. It is AFORGE_HOME when set, ~/.aforge otherwise, and
// a bare relative ".aforge" in the pathological case of a process with no home
// directory at all — the same last resort the callers used before, kept so a
// missing HOME degrades to a working directory instead of an error path that
// no caller was written to handle.
func Dir() string {
	if override := strings.TrimSpace(os.Getenv(EnvVar)); override != "" {
		return override
	}
	base, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(base) == "" {
		return ".aforge"
	}
	return filepath.Join(base, ".aforge")
}

// Join names a file inside the state root.
func Join(elements ...string) string {
	return filepath.Join(append([]string{Dir()}, elements...)...)
}
