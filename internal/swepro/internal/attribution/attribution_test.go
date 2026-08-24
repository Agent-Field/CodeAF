package attribution

import (
	"net/http"
	"strings"
	"testing"
)

// The four pairs are the whole of it, and no environment reaches them: the
// variables below used to name the app and used to switch it off entirely, and
// an engine that inherits somebody's shell is where an app quietly becomes two
// apps or none.
func TestOpenRouterHeadersAreConstantAgainstEveryOldOverride(t *testing.T) {
	t.Setenv("AGENTFIELD_OPENROUTER_ATTRIBUTION", "off")
	t.Setenv("AGENTFIELD_OPENROUTER_SITE_URL", "https://primary.example")
	t.Setenv("AGENTFIELD_OPENROUTER_APP_NAME", "Impostor")
	t.Setenv("AGENTFIELD_OPENROUTER_CATEGORIES", "roleplay")
	t.Setenv("OR_SITE_URL", "https://fallback.example")
	t.Setenv("OR_APP_NAME", "Fallback")
	t.Setenv("OR_CATEGORIES", "game")

	pairs := OpenRouterHeaderPairs()
	want := [][2]string{
		{"HTTP-Referer", "https://agentfield.ai"},
		{"X-OpenRouter-Title", "AgentField AI"},
		{"X-Title", "AgentField AI"},
		{"X-OpenRouter-Categories", "cli-agent,programming-app"},
	}
	if len(pairs) != len(want) {
		t.Fatalf("pairs = %v, want %v", pairs, want)
	}
	for i := range want {
		if pairs[i] != want[i] {
			t.Errorf("pairs[%d] = %v, want %v", i, pairs[i], want[i])
		}
	}
}

func TestApplyOpenRouterHeadersKeepsCallerValues(t *testing.T) {
	header := http.Header{}
	header.Set("HTTP-Referer", "https://caller.example")
	ApplyOpenRouterHeaders(header)
	if got := header.Get("HTTP-Referer"); got != "https://caller.example" {
		t.Errorf("caller referer clobbered: %q", got)
	}
	if got := header.Get("X-Title"); got != "AgentField AI" {
		t.Errorf("X-Title = %q", got)
	}
}

func TestAppendCommitTrailer(t *testing.T) {
	t.Setenv("AGENTFIELD_COMMIT_ATTRIBUTION", "")

	message := AppendCommitTrailer("leaf 7: do the thing")
	if !strings.HasPrefix(message, "leaf 7: do the thing\n\n") {
		t.Errorf("subject line mangled: %q", message)
	}
	if !strings.HasSuffix(message, "Co-Authored-By: SWE AF <noreply@agentfield.ai>") {
		t.Errorf("missing co-author trailer: %q", message)
	}
	if !strings.Contains(message, "https://agentfield.ai") {
		t.Errorf("missing link: %q", message)
	}
	if again := AppendCommitTrailer(message); again != message {
		t.Errorf("append is not idempotent:\n%q\n%q", message, again)
	}

	t.Setenv("AGENTFIELD_COMMIT_ATTRIBUTION", "0")
	if got := AppendCommitTrailer("plain"); got != "plain" {
		t.Errorf("kill switch ignored: %q", got)
	}
}

func TestAppendCommitTrailerToArgv(t *testing.T) {
	t.Setenv("AGENTFIELD_COMMIT_ATTRIBUTION", "")

	argv := AppendCommitTrailerToArgv([]string{"git", "commit", "-m", "leaf 7: x"})
	if len(argv) != 4 || !strings.Contains(argv[3], "Co-Authored-By: SWE AF") {
		t.Errorf("detached -m not rewritten: %v", argv)
	}

	argv = AppendCommitTrailerToArgv([]string{"git", "merge", "--no-ff", "b", "--message=m1"})
	if !strings.Contains(argv[4], "Co-Authored-By: SWE AF") {
		t.Errorf("attached --message= not rewritten: %v", argv)
	}

	untouched := []string{"git", "rebase", "--continue"}
	if got := AppendCommitTrailerToArgv(untouched); !equalArgv(got, untouched) {
		t.Errorf("rebase argv rewritten: %v", got)
	}
	status := []string{"git", "status", "-m"}
	if got := AppendCommitTrailerToArgv(status); !equalArgv(got, status) {
		t.Errorf("non-commit argv rewritten: %v", got)
	}
}

func TestCommitPromptInstruction(t *testing.T) {
	t.Setenv("AGENTFIELD_COMMIT_ATTRIBUTION", "")
	if instruction := CommitPromptInstruction(); !strings.Contains(instruction, CommitCoAuthorTrailer) {
		t.Errorf("instruction lacks trailer: %q", instruction)
	}
	t.Setenv("AGENTFIELD_COMMIT_ATTRIBUTION", "false")
	if instruction := CommitPromptInstruction(); instruction != "" {
		t.Errorf("instruction should be empty when disabled: %q", instruction)
	}
}

func TestCommitterIdentityDefaultsAndOverrides(t *testing.T) {
	for _, tc := range []struct {
		label, envName, envEmail, wantName, wantEmail string
	}{
		{
			label:    "unset falls back to the SWE-AF default",
			wantName: DefaultCommitterName, wantEmail: DefaultCommitterEmail,
		},
		{
			label:   "both overridden",
			envName: "AgentField Bot", envEmail: "bot@agentfield.ai",
			wantName: "AgentField Bot", wantEmail: "bot@agentfield.ai",
		},
		{
			label:   "name only overridden leaves the default email",
			envName: "AgentField Bot",
			// #nosec — not a credential, just the fallback address.
			wantName: "AgentField Bot", wantEmail: DefaultCommitterEmail,
		},
		{
			label:    "email only overridden leaves the default name",
			envEmail: "bot@agentfield.ai",
			wantName: DefaultCommitterName, wantEmail: "bot@agentfield.ai",
		},
		{
			label:   "whitespace-only counts as unset",
			envName: "   ", envEmail: "\t",
			wantName: DefaultCommitterName, wantEmail: DefaultCommitterEmail,
		},
		{
			label:   "values are trimmed",
			envName: "  AgentField Bot  ", envEmail: " bot@agentfield.ai ",
			wantName: "AgentField Bot", wantEmail: "bot@agentfield.ai",
		},
	} {
		t.Run(tc.label, func(t *testing.T) {
			t.Setenv(EnvCommitterName, tc.envName)
			t.Setenv(EnvCommitterEmail, tc.envEmail)
			if got := CommitterName(); got != tc.wantName {
				t.Errorf("CommitterName() = %q, want %q", got, tc.wantName)
			}
			if got := CommitterEmail(); got != tc.wantEmail {
				t.Errorf("CommitterEmail() = %q, want %q", got, tc.wantEmail)
			}
			wantFlags := []string{
				"-c", "user.name=" + tc.wantName,
				"-c", "user.email=" + tc.wantEmail,
			}
			if got := CommitterFlags(); !equalArgv(got, wantFlags) {
				t.Errorf("CommitterFlags() = %v, want %v", got, wantFlags)
			}
			wantArgv := append([]string{"git"}, append(wantFlags, "commit", "-m", "x")...)
			if got := GitArgv("commit", "-m", "x"); !equalArgv(got, wantArgv) {
				t.Errorf("GitArgv() = %v, want %v", got, wantArgv)
			}
		})
	}
}

// No commit the engine authors may carry the internal engine branding.
func TestCommitterIdentityCarriesNoEngineBranding(t *testing.T) {
	t.Setenv(EnvCommitterName, "")
	t.Setenv(EnvCommitterEmail, "")
	for _, arg := range GitArgv("commit") {
		if strings.Contains(strings.ToLower(arg), "codeaf") {
			t.Errorf("default identity leaks engine branding: %q", arg)
		}
	}
}

// GitArgv must not alias or mutate a shared backing array: two independent
// calls have to stay independent even though both start from CommitterFlags.
func TestGitArgvCallsDoNotAlias(t *testing.T) {
	first := GitArgv("commit", "-m", "first")
	second := GitArgv("commit", "-m", "second")
	if first[len(first)-1] != "first" || second[len(second)-1] != "second" {
		t.Errorf("argv aliased: first=%v second=%v", first, second)
	}
}

func equalArgv(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
