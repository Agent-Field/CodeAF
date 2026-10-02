package e2e

// relayenv_test.go is the one place an end-to-end test learns which relay it runs against.
//
// A test that fell back to a relay of its own when nothing was set put load, and a fresh identity
// per run, on a service nobody chose. So the relay is CODEAF_RELAY, the same variable the scripts
// read (scripts/measure/rigenv.py), and when it is missing the test stops and says so.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// relayEnv is the variable that names the relay under test, for every script and every test.
const relayEnv = "CODEAF_RELAY"

// relayFrom is the relay a lookup names, or the error that says which variable to set.
func relayFrom(lookup func(string) string) (string, error) {
	if relay := strings.TrimSpace(lookup(relayEnv)); relay != "" {
		return relay, nil
	}
	return "", errors.New(relayEnv + " is not set: it is the relay under test, for example https://relay.example.com (docs/testing-anywhere.md)")
}

// relayUnderTest skips the test, with the reason, when no relay was named: a skip is honest here
// because the run needs a service that only the person running it can choose.
func relayUnderTest(t *testing.T) string {
	t.Helper()
	relay, err := relayFrom(os.Getenv)
	if err != nil {
		t.Skip(err.Error())
	}
	return relay
}

func TestRelayFromEnvNamesTheVariableWhenUnset(t *testing.T) {
	_, err := relayFrom(func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), relayEnv) {
		t.Fatalf("an unset relay must stop with a message naming %s, got %v", relayEnv, err)
	}
}

func TestRelayFromEnvReadsOnlyTheNamedVariable(t *testing.T) {
	got, err := relayFrom(func(k string) string {
		if k == relayEnv {
			return " https://relay.example.com "
		}
		return "https://other.example.com"
	})
	if err != nil || got != "https://relay.example.com" {
		t.Fatalf("got %q, %v", got, err)
	}
}

// TestNoRelayHiddenInTheSuite keeps every test's relay coming through relayUnderTest: no file may
// carry a relay address of its own or read a second variable for it.
func TestNoRelayHiddenInTheSuite(t *testing.T) {
	files, _ := filepath.Glob("*_test.go")
	for _, f := range files {
		if f == "relayenv_test.go" {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{"workers.dev", "CODEAF_HOSTED_URL"} {
			if strings.Contains(string(src), banned) {
				t.Errorf("%s names %q: take the relay from relayUnderTest instead", f, banned)
			}
		}
	}
}
