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
	return DefaultUnder(base)
}

// DefaultUnder is the state root a login whose home directory is base gets
// when AFORGE_HOME says nothing. It is exported for the one caller that has to
// name it for a login it is not resolving from the environment — a background
// timer written before its definition carried a home ticked exactly this — so
// the directory's name stays spelled in one place.
func DefaultUnder(base string) string { return filepath.Join(base, ".aforge") }

// Join names a file inside the state root.
func Join(elements ...string) string {
	return filepath.Join(append([]string{Dir()}, elements...)...)
}

// StoreDir names one of a store's own directories — the workspace its jobs
// write into, the scratch they spill into — beside the store file.
//
// It is keyed to the STORE and not to the store's directory, which is the whole
// point. `--db` is a narrow override that moves the journal without moving the
// state root, so two stores pointed at one folder used to share one `workspace`
// underneath it: four probe databases in /tmp all wrote into /tmp/workspace,
// and each run's deliverables landed among the others' with nothing on disk
// saying which brain produced which file. A directory named after the store can
// only ever hold one store's work.
func StoreDir(store, kind string) string {
	base := filepath.Base(store)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if stem == "" || stem == "." || stem == string(filepath.Separator) {
		// A store path with no usable name of its own. The kind alone still
		// namespaces nothing, but it is a directory rather than an error, and
		// this is a shape no caller in the product produces.
		return filepath.Join(filepath.Dir(store), kind)
	}
	return filepath.Join(filepath.Dir(store), stem+"-"+kind)
}
