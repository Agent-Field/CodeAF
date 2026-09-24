package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A PROGRAM WORKS IN THE FOLDER IT WAS GIVEN. A chat opened in a plain folder
// made ~/Desktop/pong, named it as ground and said `in place`; the ladder's
// `in place` rung answered with the conversation's folder before ground was
// read, and senior-dev was handed the person's home folder. A program's folder
// is its ground, or the conversation's folder when it names none, and `where`
// is not read for it.
func TestAProgramWorksInTheGroundItWasGivenAndNowhereElse(t *testing.T) {
	conversation := t.TempDir()
	ground := newTestRepo(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = conversation
		config.Delegates = testPrograms("fake")
	})
	for _, where := range []string{"in place", "", filepath.Join(conversation, "elsewhere")} {
		stand := agent.resolveTaskGround(taskSpec{via: "fake", where: where, ground: ground, brief: "build it", deliverable: "the game", acceptance: "it runs"})
		if stand.refusal != "" || stand.ask != "" || stand.dir != canonicalPath(ground) || stand.mode != TaskModeWorktree {
			t.Fatalf("where %q: stand = %+v, want a copy of the ground %s", where, stand, canonicalPath(ground))
		}
	}
	stand := agent.resolveTaskGround(taskSpec{via: "fake", where: "in place", brief: "build it", deliverable: "the game", acceptance: "it runs"})
	if stand.refusal != "" || stand.dir != canonicalPath(conversation) {
		t.Fatalf("no ground: stand = %+v, want the conversation's folder %s", stand, canonicalPath(conversation))
	}
	if stand := agent.resolveTaskGround(taskSpec{via: "fake", ground: filepath.Join(conversation, "missing"), brief: "b", deliverable: "d", acceptance: "a"}); !strings.Contains(stand.refusal, "not there") {
		t.Fatalf("a ground that is not there: stand = %+v, want the refusal", stand)
	}
}

// A PROGRAM IS NEVER HANDED THE HOME FOLDER, or one above it: it is not a
// project, and senior-dev on a folder with no git history snapshots all of it.
// Both doors refuse it and say what to do instead; a folder under it is fine.
func TestAProgramIsNeverHandedTheHomeFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := filepath.Join(home, "Desktop", "pong")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = home
		config.Delegates = testPrograms("fake")
	})
	spec := taskSpec{via: "fake", where: "in place", brief: "build pong", deliverable: "the game", acceptance: "it runs"}
	want := "fake works in one project's folder, and " + canonicalPath(home) + " is your home folder; say which folder the work is in, as ground"
	if stand := agent.resolveTaskGround(spec); stand.refusal != want {
		t.Fatalf("the conversation's folder is home: refusal = %q, want %q", stand.refusal, want)
	}
	above := spec
	above.ground = filepath.Dir(home)
	if stand := agent.resolveTaskGround(above); !strings.Contains(stand.refusal, canonicalPath(filepath.Dir(home))+" holds your home folder") {
		t.Fatalf("a ground above home: stand = %+v", stand)
	}
	under := spec
	under.ground = project
	if stand := agent.resolveTaskGround(under); stand.refusal != "" || stand.dir != canonicalPath(project) {
		t.Fatalf("a ground under home: stand = %+v, want %s", stand, canonicalPath(project))
	}
	_, _, _, err := agent.StartDelegate(context.Background(), "fake", "build pong")
	if err == nil || !strings.Contains(err.Error(), "is your home folder; open codeaf in that folder") {
		t.Fatalf("/fake typed in the home folder: err = %v", err)
	}
}

// THE RECEIPT NAMES THE FOLDER, and whether it has a history is read off the
// folder rather than off a live run a program that died at once has left.
func TestAProgramsReceiptNamesItsFolder(t *testing.T) {
	tree := testPrograms("fake")[0]
	repo := newTestRepo(t)
	plain := t.TempDir()
	if got, want := delegateReceipt(repo, tree), "It is fake's: it works alone in a copy of "+repo+", and when it ends its work is left on the task's own branch; nothing is merged into the checkout."; got != want {
		t.Fatalf("the receipt for a copy = %q, want %q", got, want)
	}
	if got, want := delegateReceipt(plain, tree), "It is fake's: it works alone in "+plain+" itself, which has no git history, so its changes are there as it makes them."; got != want {
		t.Fatalf("the receipt for a plain folder = %q, want %q", got, want)
	}
	got := delegateStartedReceipt(3, "Pong", delegateReceipt(plain, tree), "")
	if !strings.HasPrefix(got, "task 3 started: Pong\nIt is fake's: it works alone in "+plain+" itself") || strings.Contains(got, "a copy of its own") || !strings.Contains(got, taskHandoffWakeSentence) {
		t.Fatalf("the started receipt = %q", got)
	}
}

// THE CARD NAMES THE PROJECT. A program's card said `where:` and the path its
// copy would have under codeaf's state; it says the folder, or a copy of it.
func TestAProgramsCardNamesTheProject(t *testing.T) {
	repo, plain := newTestRepo(t), t.TempDir()
	config := Config{Workspace: t.TempDir(), Delegates: testPrograms("fake")}
	if got := taskCardWhere(config, 1, taskSpec{via: "fake", ground: repo}); got != "a copy of "+repo {
		t.Fatalf("a repository's card says where: %q", got)
	}
	if got := taskCardWhere(config, 1, taskSpec{via: "fake", ground: plain}); got != plain {
		t.Fatalf("a plain folder's card says where: %q", got)
	}
}
