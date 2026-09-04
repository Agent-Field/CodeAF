package main

// testenv_test.go is the ONE PLACE this package's tests are given a machine to
// run on, and it exists because the suite was running on the DEVELOPER'S
// machine instead.
//
// WHAT IT COST. Several doors here are exercised end to end — `aforge exec "a
// prompt"`, the rename rows in vocabulary_test.go — under the belief, written
// into those tests, that every one of them "stops at a missing key". That is
// only true of a process with no key. On a laptop with OPENROUTER_API_KEY
// exported the same rows reached a live provider: real model calls, real money,
// ninety seconds per row, and a deliverable (PROMPT.md) plus a state directory
// written into the checkout the suite was running in. An offline suite that
// spends tokens is not a suite anybody can trust to run before a landing, so the
// isolation belongs here rather than in each test that happens to remember it.
//
// IT IS THE FLOOR AND NOT A CEILING. Everything below can still be overridden by
// a test that means to: t.Setenv wins for that test's duration, and a variable
// this process was deliberately started with (AFORGE_HOME, the call log) is left
// exactly as it was found.

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/home"
)

// isolateTestEnvironment puts this test binary on a machine of its own: no
// provider credentials, and a state root under a directory that is thrown away
// with the run. It answers a cleanup the caller runs last.
func isolateTestEnvironment() func() {
	clearTestCredentials()
	root, err := os.MkdirTemp("", "aforge-cmd-tests-")
	if err != nil {
		// A machine with no temp directory is not one these tests can be made
		// safe on; they run against whatever the environment says, exactly as
		// they did before this file, and the credential clearing above still
		// holds.
		return func() {}
	}
	// HOME MOVES WITH THE STATE ROOT, and both are needed. AFORGE_HOME answers
	// where aforge keeps its things (internal/home), while the profile's own
	// resolution and every `~` a door expands still read HOME — so a suite that
	// moved only the first would go on reading the developer's saved key out of
	// ~/.aforge/config.json ([config.PersistedAPIKey]).
	restore := pinTestEnv(map[string]string{
		home.EnvVar: filepath.Join(root, "state"),
		"HOME":      filepath.Join(root, "home"),
	})
	for _, dir := range []string{filepath.Join(root, "state"), filepath.Join(root, "home")} {
		_ = os.MkdirAll(dir, 0o755)
	}
	return func() {
		restore()
		_ = os.RemoveAll(root)
	}
}

// clearTestCredentials empties every variable this build reads a secret out of.
//
// THE LIST IS ASKED FOR RATHER THAN WRITTEN DOWN: internal/config already marks
// which settings rows are credentials and names the variable each one reads
// ([config.Setting.Secret]), so a provider added there is covered here without
// anybody remembering this file. The one variable no row names is the model
// key's second spelling, which [config.APIKeyAt] reads and the registry does
// not (its own comment says why).
func clearTestCredentials() {
	names := map[string]bool{"OPENAI_API_KEY": true}
	for _, row := range config.NewSettings(config.SettingsOptions{}).Rows() {
		if row.Secret && strings.TrimSpace(row.Env) != "" {
			names[row.Env] = true
		}
	}
	for name := range names {
		os.Unsetenv(name)
	}
}

// pinTestEnv sets each variable that this process was not deliberately started
// with, and answers the undo. A variable the caller pinned is left alone: a run
// that says AFORGE_HOME means it, which is how the UX suite drives this binary.
func pinTestEnv(values map[string]string) func() {
	undo := map[string]*string{}
	for name, value := range values {
		if was, pinned := os.LookupEnv(name); pinned && strings.TrimSpace(was) != "" {
			continue
		}
		was, existed := os.LookupEnv(name)
		if existed {
			kept := was
			undo[name] = &kept
		} else {
			undo[name] = nil
		}
		os.Setenv(name, value)
	}
	return func() {
		for name, was := range undo {
			if was == nil {
				os.Unsetenv(name)
				continue
			}
			os.Setenv(name, *was)
		}
	}
}
