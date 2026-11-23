package exec

import (
	"slices"
	"strings"
	"testing"
)

func envNames(environment []string) []string {
	names := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		names = append(names, name)
	}
	return names
}

func TestAgentShellsDoNotInheritCredentials(t *testing.T) {
	t.Setenv(AllowProviderKeysInShell, "")
	given := []string{"GH_TOKEN=x", "GITHUB_TOKEN=x", "AWS_SECRET_ACCESS_KEY=x", "AWS_ACCESS_KEY_ID=x", "NPM_TOKEN=x", "OPENROUTER_API_KEY=x", "PATH=/bin", "HOME=/h", "SSH_AUTH_SOCK=/a", "AWS_REGION=us"}
	got := envNames(JobShellEnv(given))
	for _, gone := range []string{"GH_TOKEN", "GITHUB_TOKEN", "AWS_SECRET_ACCESS_KEY", "AWS_ACCESS_KEY_ID", "NPM_TOKEN", "OPENROUTER_API_KEY"} {
		if slices.Contains(got, gone) {
			t.Fatalf("%s reached an agent's shell: %v", gone, got)
		}
	}
	for _, kept := range []string{"PATH", "HOME", "SSH_AUTH_SOCK", "AWS_REGION"} {
		if !slices.Contains(got, kept) {
			t.Fatalf("%s was taken from an agent's shell: %v", kept, got)
		}
	}
}

// The existing opt-in is the way out for a machine whose only login is GH_TOKEN.
func TestTheExistingOptInKeepsCredentialsInTheShell(t *testing.T) {
	t.Setenv(AllowProviderKeysInShell, "1")
	got := envNames(JobShellEnv([]string{"GH_TOKEN=x", "PATH=/bin"}))
	if !slices.Contains(got, "GH_TOKEN") {
		t.Fatalf("the opt-in did not keep GH_TOKEN: %v", got)
	}
}
