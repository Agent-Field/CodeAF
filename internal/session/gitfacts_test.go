package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// THE FACTS RIDE `# Project` AND NOWHERE ELSE. A model looking for where it is
// reads one heading; git's account of that same directory has to be under it,
// or it is a second place to look for one fact.
func TestGitFactsRideTheProjectHeading(t *testing.T) {
	config := Config{Workspace: "/code/app"}
	config.gitFacts = "- Git here: on dev, clean\n"
	rendered := renderSystemAt(config, time.Date(2026, 9, 12, 9, 0, 0, 0, time.UTC))

	heading := strings.Index(rendered, "\n# Project\n")
	if heading < 0 {
		t.Fatal("the prompt has no # Project heading")
	}
	line := strings.Index(rendered, "- Git here: on dev, clean")
	if line < heading {
		t.Fatalf("the git line is not under # Project (heading at %d, line at %d)", heading, line)
	}
	// AND UNDER THE DIRECTORY IT IS ABOUT, because the two lines are one fact
	// read twice and a reader should not have to hold them apart.
	if where := strings.Index(rendered, "- Working directory: /code/app"); where < 0 || line < where {
		t.Fatalf("the git line does not follow the working directory it describes")
	}
}

// AND A CONVERSATION WITH NOTHING TO SAY SAYS NOTHING. The emptiness law: a
// plain folder is not a repository, and a heading with a blank line under it is
// a prompt paying for a fact it does not have.
func TestAProjectWithNoGitFactsRendersNoExtraLine(t *testing.T) {
	config := Config{Workspace: "/code/app"}
	rendered := renderSystemAt(config, time.Date(2026, 9, 12, 9, 0, 0, 0, time.UTC))
	if strings.Contains(rendered, "- Git ") {
		t.Fatalf("a conversation with no git facts rendered a git line:\n%s", promptTail(rendered))
	}
	if strings.Contains(rendered, "- Working directory: /code/app\n\n") {
		t.Fatal("an empty git block left a blank line under the working directory")
	}
}

// THE LINE READS AS A SENTENCE, in the three shapes git answers with: a branch
// tracking something and behind it, a clean detached checkout, and a branch with
// no upstream at all. The parsing is of git's own `## ` header, so these are the
// strings git actually prints.
func TestTheGitLineReadsAsASentence(t *testing.T) {
	for _, row := range []struct{ header, want string }{
		{"## simplify-A...origin/simplify [ahead 2, behind 1]", "on simplify-A"},
		{"## simplify-A...origin/simplify [ahead 2, behind 1]", "2 ahead and 1 behind of origin/simplify"},
		{"## dev...origin/dev", ""},
		{"## HEAD (no branch)", ""},
	} {
		if row.want == "" {
			if got := gitUpstreamWord(row.header); got != "" {
				t.Errorf("%q said %q about its upstream, want nothing", row.header, got)
			}
			continue
		}
		if !strings.Contains(gitBranchWord(row.header)+" "+gitUpstreamWord(row.header), row.want) {
			t.Errorf("%q reads as %q + %q, want it to contain %q",
				row.header, gitBranchWord(row.header), gitUpstreamWord(row.header), row.want)
		}
	}
	if got := gitBranchWord("## HEAD (no branch)"); got != "on no branch" {
		t.Errorf("a detached checkout reads as %q", got)
	}
}

// A READING IS A SUBPROCESS, SO THE HEAD START IS NOT TAKEN FOR EVERY AGENT.
// [newAgent] runs for every task node a conversation hands out and for every
// agent a test builds — a thousand in one suite — and forking `git` for each of
// those to move a convenience fact one turn earlier is not a trade worth
// making. The turn's own refresh covers everything this declines.
func TestTheGitHeadStartIsOnlyForAConversationInARepository(t *testing.T) {
	plain := t.TempDir()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("staging a repository: %v", err)
	}
	for _, row := range []struct {
		what   string
		config Config
		want   bool
	}{
		{"a conversation in a repository", Config{Workspace: repo}, true},
		{"a conversation in a plain folder", Config{Workspace: plain}, false},
		{"a conversation with no workspace", Config{}, false},
		{"a task node in a repository", Config{Workspace: repo, InTask: true}, false},
	} {
		if got := worthAHeadStart(row.config); got != row.want {
			t.Errorf("%s takes a head start: %v, want %v", row.what, got, row.want)
		}
	}
}
