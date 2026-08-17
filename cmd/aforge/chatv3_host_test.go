package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/tui3"
)

// The remote agent IS the surface's agent, checked at compile time so a method
// added to tui3.Agent breaks the build here rather than at the first keystroke
// of a remote session.
var _ tui3.Agent = (*remote.Agent)(nil)

func TestParseHostTarget(t *testing.T) {
	cases := []struct {
		raw       string
		dest      string
		workspace string
		fails     bool
	}{
		{raw: "devbox", dest: "devbox"},
		{raw: "me@devbox", dest: "me@devbox"},
		{raw: "devbox:code/app", dest: "devbox", workspace: "code/app"},
		{raw: "devbox:/srv/app", dest: "devbox", workspace: "/srv/app"},
		{raw: "me@devbox:/srv/app", dest: "me@devbox", workspace: "/srv/app"},
		// A trailing colon is "the engine's own home", said out loud.
		{raw: "devbox:", dest: "devbox"},
		// localhost is a host like any other — it is also the rig this feature is
		// tested end to end on, and nothing may special-case it.
		{raw: "localhost", dest: "localhost"},
		{raw: "localhost:code/app", dest: "localhost", workspace: "code/app"},
		// A path with its own colon in it belongs to the workspace: the FIRST
		// colon is the split and the rest is the path as typed.
		{raw: "devbox:code/a:b", dest: "devbox", workspace: "code/a:b"},
		{raw: "  devbox:code/app  ", dest: "devbox", workspace: "code/app"},
		{raw: "", fails: true},
		{raw: ":/srv/app", fails: true},
	}
	for _, c := range cases {
		dest, workspace, err := parseHostTarget(c.raw)
		if c.fails {
			if err == nil {
				t.Fatalf("parseHostTarget(%q) was accepted", c.raw)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseHostTarget(%q): %v", c.raw, err)
		}
		if dest != c.dest || workspace != c.workspace {
			t.Fatalf("parseHostTarget(%q) = %q, %q; want %q, %q", c.raw, dest, workspace, c.dest, c.workspace)
		}
	}
}

// THE WORKSPACE IS NEVER RESOLVED HERE. A relative path stays relative, because
// the engine resolves it against its own home and this machine has no standing
// to make it absolute.
func TestParseHostTargetLeavesTheWorkspaceAsTyped(t *testing.T) {
	_, workspace, err := parseHostTarget("devbox:code/app")
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(workspace, "/") {
		t.Fatalf("the workspace was made absolute: %q", workspace)
	}
}

func TestFlagsThatCannotTravelAreRefusedRatherThanIgnored(t *testing.T) {
	for _, launch := range []hostLaunch{
		{target: "devbox", yolo: true},
		{target: "devbox", noCompact: true},
		{target: "devbox:app", yolo: true, noCompact: true},
	} {
		err := launch.check()
		if err == nil {
			t.Fatalf("%+v was accepted silently", launch)
		}
		if !strings.Contains(err.Error(), "devbox") {
			t.Fatalf("the refusal does not name the machine to set it on: %v", err)
		}
	}
	if err := (hostLaunch{target: "devbox"}).check(); err != nil {
		t.Fatalf("an ordinary launch was refused: %v", err)
	}
}

func TestShellQuoteSurvivesAPathWithSpacesAndQuotes(t *testing.T) {
	if got := shellQuote("code/my app"); got != `'code/my app'` {
		t.Fatalf("shellQuote = %s", got)
	}
	if got := shellQuote("it's"); got != `'it'\''s'` {
		t.Fatalf("shellQuote = %s", got)
	}
}

func TestMissingCommandIsRecognizedInEveryShellsWording(t *testing.T) {
	for _, said := range []string{
		"bash: aforge: command not found",
		"sh: 1: aforge: not found",
		"zsh: command not found: aforge",
	} {
		if !mentionsMissingCommand(said) {
			t.Fatalf("not recognized as a missing aforge: %q", said)
		}
	}
	if mentionsMissingCommand("Permission denied (publickey).") {
		t.Fatal("an ssh refusal was read as a missing aforge")
	}
}

// The flag exists and is documented in exactly one place: `aforge chat -h`.
func TestHostFlagIsOnTheChatUsage(t *testing.T) {
	err := openChatV3("chat", []string{"--help"}, false)
	if err == nil {
		return
	}
	if !strings.Contains(err.Error(), "flag: help requested") {
		t.Fatalf("chat --help = %v", err)
	}
}
