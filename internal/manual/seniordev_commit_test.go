package manual

import (
	"os"
	"strings"
	"testing"
)

// The retrieved commit section stands alone: people need the repository
// scope, refusals and interrupted-run ending without reading its neighbors.
func TestTheSeniorDevCommitSectionExplainsScopeRefusalsAndEndings(t *testing.T) {
	body, err := os.ReadFile("chat/senior-dev.md")
	if err != nil {
		t.Fatal(err)
	}
	heading := "## Does senior-dev commit or push"
	_, section, ok := strings.Cut(string(body), heading)
	if !ok {
		t.Fatal("commit section is missing")
	}
	section = heading + strings.SplitN(section, "\n## ", 2)[0]
	for _, want := range []string{"git repository", "no git history", "stash", "reset", "clean", "check out", "8 KiB", "UTF-8", "NUL", "credit lines", "plain file", "already tracked", "did not pass", "run's ending"} {
		if !strings.Contains(section, want) {
			t.Errorf("commit section omits %q", want)
		}
	}
	if len([]rune(section)) > 2000 {
		t.Errorf("commit section has %d characters, want at most 2000", len([]rune(section)))
	}
	if !strings.HasSuffix(section, "\n") {
		t.Error("commit section needs a blank line before its next heading")
	}
	if strings.Contains(string(body), "commit its last edits") {
		t.Error("dying-host section still says senior-dev commits its last edits")
	}
	delegates, err := os.ReadFile("chat/delegates.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(delegates), "did not pass") {
		t.Error("delegate work section omits non-pass endings")
	}
}
