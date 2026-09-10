//go:build e2e

package e2e

// livekey_test.go is THE ONE PLACE THIS PACKAGE ASKS WHETHER THE MACHINE HAS A
// PROVIDER KEY, and it asks the question the product asks.
//
// THE DEFECT THIS FILE CLOSES (#576) IS A SUITE THAT REPORTS GREEN WITHOUT
// RUNNING — the same shape as #184. Every lane here used to read
// `os.Getenv("OPENROUTER_API_KEY")` and skip when it was empty, while the
// product resolves a key THREE ways (internal/config's [config.APIKeyAt]): the
// OpenRouter variable, the OpenAI variable, then the `api_key` row in the
// profile. A key pasted into the first-run setup or typed into /settings lives
// only on the third road, so a machine that launches aforge and talks to a model
// all day long skipped this whole suite and printed `ok`. A skip is a promise
// that the suite COULD NOT be honest; a skip on a machine that has a key is a
// lie, and it is indistinguishable from a pass in CI.
//
// So the gate and the product read the same three roads, in the same order, and
// the key the rigs export is the key a launch on this machine would talk with.

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/home"
)

// machineKey is what a launch on THIS machine would authenticate with, read once
// as the test binary starts.
//
// IT IS READ AT INIT AND NOT AT THE GATE because lanes here move the state root
// out from under themselves: [newWorld] points AFORGE_HOME at a throwaway
// directory, and a resolution run after that would be answering for the fixture
// rather than for the machine. The question this package has to ask is the one a
// person's own launch answers, and that is the environment the binary started in.
//
// THE PROFILE ROAD GOES THROUGH [home.InheritedDir], NOT [config.ProfileDir].
// ProfileDir is AFORGE_PROFILE_DIR, which almost nobody exports, and the empty
// string falls through home.Dir() — which a test binary will point at a
// throwaway of its own (#402). InheritedDir is the ungated person home this
// process started with, so a key that lives only in ~/.aforge/config.json is
// still the key a launch on this machine would talk with.
var machineKey = strings.TrimSpace(config.APIKeyAt(liveProfileDir()))

// liveProfileDir is the profile a launch on this machine would read: an explicit
// AFORGE_PROFILE_DIR when the process was started with one, otherwise the
// inherited state root. Empty ProfileDir must NOT fall through home.Dir().
func liveProfileDir() string {
	if dir := strings.TrimSpace(config.ProfileDir()); dir != "" {
		return dir
	}
	return home.InheritedDir()
}

// noLiveKeyReason is the sentence a lane skips with, and it names every road it
// looked down — so a person who HAS a key and sees this line knows the key is
// somewhere neither the product nor this suite reads.
const noLiveKeyReason = "no provider key on this machine by any road the product reads " +
	"(OPENROUTER_API_KEY, OPENAI_API_KEY, or the profile's api_key row): " +
	"this lane drives a real model or it says nothing"

// liveKey is the key this lane talks with, or a skip when the machine has none.
//
// EVERY GATE IN THIS PACKAGE GOES THROUGH IT, and internal/e2e's untagged
// [TestEveryLaneAsksForItsKeyTheWayTheProductDoes] fails the pull request on a
// lane that reaches for a key variable itself.
func liveKey(t *testing.T) string {
	t.Helper()
	if machineKey == "" {
		t.Skip(noLiveKeyReason)
	}
	return machineKey
}
