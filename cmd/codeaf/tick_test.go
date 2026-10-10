package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
)

// The old timer's verb and the clock are machinery, not commands, so neither
// appears in the list of things a person can usefully type — for the same
// reason `engine` does not.
func TestTickAndClockAreAbsentFromTheUsageText(t *testing.T) {
	for _, word := range []string{"codeaf tick", " tick ", "codeaf clock"} {
		if strings.Contains(usageText, word) {
			t.Fatalf("the usage text offers %q", word)
		}
	}
}

// keyless is a machine that has never been given an API key, whatever the shell
// running the tests exports.
//
// THERE ARE EXACTLY THREE PLACES A KEY CAN COME FROM and all three are shut
// here, because the alternative is a test that quietly buys a real judgment on
// somebody's account: OPENROUTER_API_KEY, OPENAI_API_KEY, and the `api_key`
// field of `config.json` in the profile, which is pointed at an empty folder.
func keyless(t *testing.T) {
	t.Helper()
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv(config.ProfileDirEnv, t.TempDir())
}

// THE CLOCK TAKES NO ARGUMENTS, and one that finds another clock holding the
// lock leaves without a word.
func TestClockTakesNoArguments(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	if err := runClock([]string{"--now"}); err == nil {
		t.Fatal("an argument was accepted by a door that has none")
	}
}
