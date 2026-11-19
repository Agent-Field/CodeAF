// Package home resolves the one directory codeaf owns: its state root.
//
// Everything durable the resident keeps — the journal, the workspace, the CAS,
// the craft repo, measured profiles, the model catalog, the router ledger, the
// promoted skills shelf — lives under a single directory so that "where does
// codeaf keep my things" has exactly one answer. That answer is ~/.codeaf, and
// CODEAF_HOME moves it wholesale.
//
// The override exists for the same reason the directory exists: a disposable
// run — the UX suite driving the real binary, a second brain on the same
// laptop, a sandbox — must be able to move every file codeaf writes without
// moving the user's HOME, and without a per-file flag for each of them. Narrow
// overrides that already exist (chat --db, CODEAF_PROFILE_DIR) still win where
// they apply; this only changes the default they fall back to.
package home

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/env"
)

// EnvVar names the override. It is exported so help text and doctor output can
// say the same word the code reads.
const EnvVar = "CODEAF_HOME"

// Dir is the state root. It is CODEAF_HOME when set, ~/.codeaf otherwise, and
// a bare relative ".codeaf" in the pathological case of a process with no home
// directory at all — the same last resort the callers used before, kept so a
// missing HOME degrades to a working directory instead of an error path that
// no caller was written to handle.
//
// Inside a test binary it is that answer unless the answer is the root the
// process was handed rather than one the test chose, in which case it is a
// throwaway directory of this process's own. See undertest.go: a test may not
// resolve the state of whoever ran it. Outside a test binary the gate is not
// there at all and this is [resolve] exactly as it has always been.
func Dir() string {
	root := resolve()
	if underTest && Contains(inherited, root) {
		return quarantine
	}
	return root
}

// resolve is [Dir] without the gate — the plain rule, which the gate itself has
// to be able to ask for so it can capture the root this process was started
// with before any test moves anything.
func resolve() string {
	if override := strings.TrimSpace(env.Get(EnvVar)); override != "" {
		return override
	}
	base, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(base) == "" {
		return ".codeaf"
	}
	current := DefaultUnder(base)
	if directoryExists(current) {
		return current
	}
	legacy := legacyUnder(base)
	if directoryExists(legacy) {
		return legacy
	}
	return current
}

// DefaultUnder is the state root a login whose home directory is base gets
// when CODEAF_HOME says nothing. It is exported for the one caller that has to
// name it for a login it is not resolving from the environment — a background
// timer written before its definition carried a home ticked exactly this — so
// the directory's name stays spelled in one place.
func DefaultUnder(base string) string { return filepath.Join(base, ".codeaf") }

func legacyUnder(base string) string { return filepath.Join(base, ".aforge") } // legacy-name

func directoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// Moved says the state root is not the person's own default one: CODEAF_HOME
// names another, or this is a test binary, whose root is never the person's
// (undertest.go). A folder codeaf keeps outside the state root on a person's
// behalf — a program's copies under the cache folder — follows the state root
// when it has moved, so a disposable home takes everything codeaf writes with
// it, and a test never writes into the cache of whoever ran it.
func Moved() bool {
	return strings.TrimSpace(env.Get(EnvVar)) != "" || underTest
}

// Join names a file inside the state root.
func Join(elements ...string) string {
	return filepath.Join(append([]string{Dir()}, elements...)...)
}

// LoginEnvVar names the person's real login home when the state root was moved
// for a reason that is not a quarantine. The desktop app runs
// `codeaf app --home <dir>` so that its chats and settings stay apart from the
// CLI's, and that moves the state root and nothing else of the person's: the
// dot-folders other programs keep under their home are still where they were.
// [KeepLogin] sets it, [Login] reads it, and a child process inherits it with
// CODEAF_HOME, so the whole process tree answers [Login] the same way.
const LoginEnvVar = "CODEAF_LOGIN_HOME"

// inheritedLogin is LoginEnvVar as this process was STARTED with it, read once
// at initialisation for the same reason [inherited] is: a test binary run from
// inside an app's shell inherits the app's value, and a value nobody in the
// test chose is not a home the test may read (see [Login]).
var inheritedLogin = strings.TrimSpace(env.Get(LoginEnvVar))

// Login is THE person's login home: the "~" whose dot-folders hold Claude
// Code's, Codex's and their kin's skills and settings, which codeaf reads in
// place. Every read of another program's files goes through this and not
// through os.UserHomeDir, because the two differ exactly when the state root
// was moved, and a reader that asked the operating system would find the
// person's real files in a run that is meant to be quarantined from them.
//
// It answers, in order:
//
//   - the real login home [KeepLogin] recorded, when the state root was moved
//     by `codeaf app --home`: codeaf's state is elsewhere, the person's other
//     programs are not;
//   - CODEAF_HOME, when that alone is set. A disposable run or a test that
//     named a state root gets a login home of its own with it, so a scan of
//     "the person's" skills finds none of the real person's;
//   - otherwise the login home the state root itself is resolved from.
//
// It carries Dir's test-binary gate, aimed at the container instead of the
// root: a suite that named no home of its own gets the quarantine, not the
// home of whoever ran it, because a scan of a real home imports a real
// person's skills into a throwaway store. A test that pins HOME, CODEAF_HOME or
// LoginEnvVar — the ways a test says where its state goes — still gets exactly
// the home it asked for; a LoginEnvVar the process was merely handed does not.
func Login() (string, error) {
	if kept := keptLogin(); kept != "" {
		return kept, nil
	}
	return StateLogin()
}

// keptLogin is the real login home an `app --home` recorded, or "" when none
// did. Under a test binary a value the process inherited is not one a test
// chose, so it reads as unset and [Login] falls through to its quarantine.
func keptLogin() string {
	kept := strings.TrimSpace(env.Get(LoginEnvVar))
	if kept == "" || (underTest && kept == inheritedLogin) {
		return ""
	}
	if resolved, err := filepath.Abs(kept); err == nil {
		return resolved
	}
	return kept
}

// StateLogin is the home directory codeaf's OWN user skills folder,
// <home>/.codeaf/skills, is read under. It follows the state root's override —
// CODEAF_HOME moves it wholesale, the same way it moves everything else codeaf
// reads — and otherwise answers the login home the state root itself is
// resolved from. It is [Login] exactly, except under `codeaf app --home`, where
// the app's own folder keeps codeaf's skills (the app installs its own there)
// and Login names the person's home. Almost nothing wants this: a read of
// another program's files wants [Login].
func StateLogin() (string, error) {
	if override := strings.TrimSpace(env.Get(EnvVar)); override != "" {
		if resolved, err := filepath.Abs(override); err == nil {
			return resolved, nil
		}
		return override, nil
	}
	return osLogin()
}

// Kept says the state root was moved for a home the person keeps, the desktop
// app's `--home`, and not for a disposable one: [KeepLogin] recorded their login
// home beside it. A folder codeaf keeps outside the state root on a person's
// behalf lives where it would for the person's own home, not inside this one,
// because a kept home is backed up and a person opens it; a disposable home takes
// everything with it ([Moved]). A record a test binary was merely handed is not
// one, for the reason [Login] gives.
func Kept() bool { return keptLogin() != "" }

// OwnLogin is the home directory whose .codeaf/skills is codeaf's own user
// skills folder for a scan of the login home dir. Under `codeaf app --home`
// that is the app's folder when dir is the person's login home; a scan of any
// other directory — a fixture, a home a caller names — is a self-contained
// home and answers itself, and so does every process that did not move the
// state root for the app.
func OwnLogin(dir string) string {
	kept := keptLogin()
	if kept == "" || absolute(dir) != kept {
		return dir
	}
	if own, err := StateLogin(); err == nil {
		return own
	}
	return dir
}

// KeepLogin makes the person's real login home survive a move of the state
// root. `codeaf app --home` calls it after setting CODEAF_HOME: it records the
// home the operating system names, so [Login] still answers it, and the
// children this process starts inherit the record along with CODEAF_HOME. The
// answer is the restore, which puts the variable back as it was — a process
// ends and takes the record with it, but an in-process test of the app door
// must not leave one behind for the next test.
//
// The recorded home goes through the test-binary gate, so under `go test` with
// no HOME of its own it is the quarantine and never the real person's home. A
// process with no resolvable home records nothing and Login keeps answering the
// state root, which is what it did before.
func KeepLogin() (restore func()) {
	previous, had := env.Lookup(LoginEnvVar)
	restore = func() {
		if had {
			_ = os.Setenv(LoginEnvVar, previous)
		} else {
			_ = os.Unsetenv(LoginEnvVar)
		}
	}
	login, err := osLogin()
	if err != nil {
		return restore
	}
	_ = os.Setenv(LoginEnvVar, login)
	return restore
}

// osLogin is the login home the operating system names, behind the test-binary
// gate: the half of [Login] that does not depend on what the state root says.
func osLogin() (string, error) {
	base, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(base) == "" {
		if err == nil {
			err = errors.New("no home directory")
		}
		return "", fmt.Errorf("resolve the login home: %w", err)
	}
	// The inherited root lives INSIDE the login home it was resolved from,
	// so the containment runs the other way from Dir's gate: quarantining the
	// home that holds the inherited state root quarantines the scan that would
	// have read the person's real dot-folders out of it.
	if underTest && Contains(base, inherited) {
		return quarantine, nil
	}
	return base, nil
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

// Contains reports whether path is the directory root or something under it.
// It is here because "is this file inside that root" is the state root's own
// question, and it is asked by the packages that refuse to touch the state of
// whoever started a test binary — internal/lane's, today; internal/calllog
// still carries a private copy of this from #352, and should adopt this one, so
// that the same omission cannot be a defect in one place and not the other.
//
// It compares by path elements rather than by string prefix, so a sibling named
// like the root — /state/root-2 beside /state/root — is not mistaken for a
// child of it. An empty root contains nothing, which is what "the environment
// named no root" has to mean.
func Contains(root, path string) bool {
	if root = strings.TrimSpace(root); root == "" {
		return false
	}
	relative, err := filepath.Rel(absolute(root), absolute(path))
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// absolute is filepath.Abs with the error swallowed: the caller wants two paths
// it can compare, and a working directory that cannot be read is no reason to
// answer the question wrongly in the permissive direction — an unresolvable
// path stays as it is and fails the comparison.
func absolute(path string) string {
	if resolved, err := filepath.Abs(path); err == nil {
		return resolved
	}
	return path
}
