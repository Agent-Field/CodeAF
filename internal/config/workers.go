package config

import (
	"os"
	"sort"
	"strings"
	"sync"
)

// THE WORKER ROSTER: WHICH LEAF WORKERS ARE INSTALLED IN THIS PROFILE.
//
// A build declares the workers it can construct — the generalist, the cheap
// whole-taker, the coding pipeline — and until this row existed, declaring one
// was the same act as installing it: every profile on the machine got every
// worker the binary had, and there was no sentence a person could write that
// meant "not that one here".
//
// That mattered because a worker is not a preference. Registration is what puts
// a worker in front of the model that chooses one, in front of the ladder that
// escalates onto one, and in front of the `--subharness` flag; a person who does
// not want the coding pipeline taking their work has no way to say so, and every
// road out of that ends in "and then it chose it anyway".
//
// So the roster is a SET FILTER OVER REGISTRATION and nothing else. Workers off
// it are never registered, and the codebase's own law then does the rest: a
// capability that cannot work is absent, not broken. An unregistered worker is
// off the menu the compiler reads, unknown to the escalation ladder, and named
// by the flag's own "not in this build" note — with no branch anywhere asking
// whether this or that particular worker is wanted. The mechanism does not know
// any worker's name, and that is the point: the next worker somebody adds is on
// the roster the day it is declared and off it the day somebody writes a line.
//
// THE GENERALIST IS NOT ON THE ROSTER AND CANNOT BE TAKEN OFF IT. It is not a
// registered subharness in the first place (internal/plan's size.go: linear is
// the baseline every node is already judged against, never an entry on a menu),
// so there is nothing here to filter — an empty roster is a profile that runs
// every leaf on the generalist, which is the shape the whole system ran on
// before there was a second worker.

// EnvWorkers pins the roster from the environment, which is what a measurement
// run wants: one arm of a benchmark says which workers exist for its own process
// without writing anything into the profile the next arm will read.
const EnvWorkers = "AFORGE_WORKERS"

// workerCatalog is the hook onto WHAT THIS BUILD HAS. It is a function rather
// than data for the reason exec's measured-history hook is one: the workers a
// build can construct are known to the surface that constructs them, and this
// package must never be the reason something imports the executor.
//
// A process that has not installed one — every test that builds the settings
// sheet on its own, and every tool that only wants to read a row — answers
// nothing, and the row simply has no receipt. Nothing else changes: the roster
// is applied where the workers are registered, not here.
var (
	workerCatalogMutex sync.RWMutex
	workerCatalog      func() []string
)

// UseInstalledWorkers installs the catalog hook. The surface calls it once,
// with every worker it could register, BEFORE the roster is applied — so what
// the row reports is the whole build and not the filtered result, which is the
// difference between a person being able to see the worker they turned off and
// a person having no way to find its name again.
func UseInstalledWorkers(catalog func() []string) {
	workerCatalogMutex.Lock()
	defer workerCatalogMutex.Unlock()
	workerCatalog = catalog
}

// InstalledWorkers is what this build can construct, in its own order. Empty
// when nothing installed the hook.
func InstalledWorkers() []string {
	workerCatalogMutex.RLock()
	catalog := workerCatalog
	workerCatalogMutex.RUnlock()
	if catalog == nil {
		return nil
	}
	return catalog()
}

// WorkersAt resolves the roster line in force: the environment pin, then what
// the profile persisted, then nothing at all.
//
// NOTHING AT ALL IS THE DEFAULT AND IT MEANS EVERY WORKER, which is why this
// returns the line rather than the list. The default is derived where the build
// knows its own workers ([WorkerRoster] with an empty line), so there is no
// literal list of worker names anywhere in this package to go stale.
func WorkersAt(profileDir string) string {
	if raw := strings.TrimSpace(os.Getenv(EnvWorkers)); raw != "" {
		return raw
	}
	if value, ok := persistedString(profileDir, KeyWorkers); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

// WorkerRoster reads one roster line against the names a build knows, and
// answers with the workers to install and the names it could not place.
//
// An EMPTY LINE is every known worker: a profile that has said nothing gets the
// build it was given, byte for byte what it got before this row existed.
//
// A line that places nothing — a typo, a retired worker's name, or a person
// naming only the generalist — keeps nothing, and the caller registers nothing.
// That is the honest reading of what was written rather than a silent widening
// back to everything: somebody who names workers has said which ones they want,
// and the generalist is always underneath whatever is left.
//
// Unknown names are ANSWERED, never refused. A roster is read once at startup
// before any command has run, and a build that died at boot because a profile
// still names a worker this binary no longer ships would be a rename turning
// into an unbootable install. The caller says the sentence; this says which
// names it is about.
//
// Matching is case-insensitive and separators are commas, semicolons or spaces,
// because this is a line a person types into a config file and `swe, bare` and
// `swe bare` are the same intention.
func WorkerRoster(line string, known []string) (kept []string, unknown []string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return append([]string(nil), known...), nil
	}
	byLowerName := make(map[string]string, len(known))
	for _, name := range known {
		byLowerName[strings.ToLower(strings.TrimSpace(name))] = name
	}
	wanted := map[string]bool{}
	seenUnknown := map[string]bool{}
	for _, field := range strings.FieldsFunc(line, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n'
	}) {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		name, ok := byLowerName[strings.ToLower(field)]
		if !ok {
			if !seenUnknown[field] {
				seenUnknown[field] = true
				unknown = append(unknown, field)
			}
			continue
		}
		wanted[name] = true
	}
	// The build's own order is kept rather than the order they were typed: the
	// roster is a SET, and a person reordering the line must not reorder the
	// menu a model reads.
	for _, name := range known {
		if wanted[name] {
			kept = append(kept, name)
		}
	}
	return kept, unknown
}

// workersReceipt is the dim fact beside the roster row: the workers this build
// has, so a person who turned one off can read its name back without hunting
// for it. It is derived from the build's own catalog every time it is asked —
// a second list written here would be the one that goes stale.
//
// A build that installed no catalog says NOTHING rather than "none", which is
// the emptiness law: unknown and zero are not the same fact.
func workersReceipt() string {
	installed := InstalledWorkers()
	if len(installed) == 0 {
		return ""
	}
	names := append([]string(nil), installed...)
	sort.Strings(names)
	return "installed here: " + strings.Join(names, ", ")
}
