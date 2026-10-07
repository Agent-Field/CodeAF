package gitidentity

import "testing"

// The normalizer's whole claim: every spelling of ONE repository folds to ONE
// name, so a project's memories are one project's whatever road it was cloned
// by. The path after the host keeps its case — case-sensitive hosts exist,
// and folding theirs would collide two projects.
func TestNormalizeRemoteURLFoldsTheSpellingsOfOneRepository(t *testing.T) {
	same := []string{
		"git@github.com:Agent-Field/codeaf.git",
		"ssh://git@github.com/Agent-Field/codeaf",
		"https://github.com/Agent-Field/codeaf.git",
		"http://github.com/Agent-Field/codeaf/",
		"https://git@github.com/Agent-Field/codeaf",
		"ssh://git@github.com:22/Agent-Field/codeaf",
	}
	want := "github.com/Agent-Field/codeaf"
	for _, raw := range same {
		if got := NormalizeRemoteURL(raw); got != want {
			t.Errorf("%q -> %q, want %q", raw, got, want)
		}
	}
	// A different host is a different name, lower-cased as the host's own rule
	// usually spells it.
	if got := NormalizeRemoteURL("git@gitlab.company.com:grp/proj.git"); got != "gitlab.company.com/grp/proj" {
		t.Errorf("gitlab -> %q", got)
	}
	if got := NormalizeRemoteURL(""); got != "" {
		t.Errorf("empty -> %q, want empty", got)
	}
}
