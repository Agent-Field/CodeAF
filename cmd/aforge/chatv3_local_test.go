package main

// THE WHEN-TO-TAKE RULE, asked the four ways a launch can meet it.
//
// The rule decides which door a person's `aforge chat` goes through, and it is
// the one decision in this lane a mistake in would be invisible: taking the
// host road when nothing is there costs a spawned process and every capability
// a hosted conversation does not have, and NOT taking it when something is
// there is the defect the whole lane exists to end.

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/enginehost"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// shortStateRoot is a state root a unix socket can be named in.
//
// It is NOT t.TempDir, for internal/enginehost's own stated reason: Go names a
// temp directory after the test, this tree's test names are sentences, and a
// socket path has about a hundred bytes to spend. A test that ran out of them
// would be measuring the honest refusal a deep AFORGE_HOME gets rather than the
// rule.
func shortStateRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("", "afl")
	if err != nil {
		t.Fatalf("make a state root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	t.Setenv("AFORGE_HOME", root)
	return root
}

// answeringHost is a socket where this workspace's host would be, and nothing
// behind it. The rule asks one question — is anybody there — so a listener is
// the whole of what it takes to be somebody.
func answeringHost(t *testing.T, workspace string) {
	t.Helper()
	// [enginehost.Dir] and not SocketPath alone: naming the socket makes
	// nothing now, and a host makes the directory it listens in itself.
	if _, err := enginehost.Dir(workspace); err != nil {
		t.Fatalf("make somewhere for a host to live: %v", err)
	}
	socket, err := enginehost.SocketPath(workspace)
	if err != nil {
		t.Fatalf("name this workspace's socket: %v", err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("stand in for a host: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
}

// A host that answers is a conversation already open in this workspace, and the
// second terminal goes to it rather than opening one of its own.
func TestTheHostRoadIsTakenWhenAHostAnswers(t *testing.T) {
	shortStateRoot(t)
	workspace := t.TempDir()
	if v3TakeHostRoad(workspace, v3HostChoice{}) {
		t.Fatal("the host road was taken with nothing holding this workspace")
	}
	answeringHost(t, workspace)
	if !v3TakeHostRoad(workspace, v3HostChoice{}) {
		t.Fatal("a host answered and the launch opened its own conversation anyway")
	}
}

// A JOURNAL ANOTHER WINDOW IS HOLDING NEVER STARTS A HOST, which is the rule
// this lane deliberately does NOT have. A host cannot open a file an in-process
// window has flocked, so spawning one to reach a held conversation buys a
// slower version of the same refusal and a process that lingers for its idle
// span. The way to that conversation is to move it here (issue #71).
func TestALockedJournalDoesNotSendTheLaunchLookingForAHost(t *testing.T) {
	root := shortStateRoot(t)
	workspace := t.TempDir()
	place, err := v3MintSession(t.TempDir(), workspace, workspace, false)
	if err != nil {
		t.Fatal(err)
	}
	held, err := v3OpenSession(session.Config{
		Workspace: workspace, Model: "test/model", APIKey: "test-key",
		BaseURL: "https://example.invalid/v1",
		Place:   place, SessionFile: place.Transcript(),
	})
	if err != nil {
		t.Fatalf("the first window did not open: %v", err)
	}
	defer func() { _ = held.Close() }()
	if !session.InUse(place.Transcript()) {
		t.Fatal("the fixture is wrong: nothing is holding the journal")
	}

	if v3TakeHostRoad(workspace, v3HostChoice{}) {
		t.Fatal("a locked journal sent the launch looking for a session host")
	}
	// AND NOTHING WAS PUT ON DISK WHERE A HOST WOULD LIVE. The rule asks one
	// question of a socket that is not there; it must not have made a home for
	// one on the way past.
	if entries, err := os.ReadDir(filepath.Join(root, "v3", "hosts")); err == nil && len(entries) > 0 {
		t.Fatalf("the rule left %d host directories behind", len(entries))
	}
}

// The lone terminal, which is most of them: nothing is open here, so nothing
// changes and the in-process door keeps every capability a hosted conversation
// does not have.
func TestALoneTerminalKeepsTheInProcessDoor(t *testing.T) {
	shortStateRoot(t)
	workspace := t.TempDir()
	if v3TakeHostRoad(workspace, v3HostChoice{}) {
		t.Fatal("a quiet workspace sent the launch down the host road")
	}
	if v3TakeHostRoad("", v3HostChoice{}) {
		t.Fatal("a workspace that could not be resolved sent the launch down the host road")
	}
}

// --no-host is the escape hatch and it beats everything: it is what a person
// types on the day the host itself is the thing that is wrong.
func TestNoHostKeepsTheInProcessDoorEvenWhenAHostAnswers(t *testing.T) {
	shortStateRoot(t)
	workspace := t.TempDir()
	answeringHost(t, workspace)
	if !v3TakeHostRoad(workspace, v3HostChoice{}) {
		t.Fatal("the fixture is wrong: no host answered")
	}
	if v3TakeHostRoad(workspace, v3HostChoice{noHost: true}) {
		t.Fatal("--no-host still went looking for a session host")
	}
}

// A flag about HOW THE SESSION IS BUILT keeps the launch in this process,
// because none of them can travel a wire and a launch that carried one down the
// host road would drop it in silence — the one outcome worse than a refusal.
func TestASessionShapedFlagKeepsTheInProcessDoor(t *testing.T) {
	shortStateRoot(t)
	workspace := t.TempDir()
	answeringHost(t, workspace)
	if v3TakeHostRoad(workspace, v3HostChoice{shaped: true}) {
		t.Fatal("--yolo and its neighbours travelled a wire that carries none of them")
	}
}

// --no-host is about the session host on THIS machine, so naming it beside a
// machine is two different statements about where the conversation is. A flag
// that could not honour itself is a refusal in this tree and never a shrug.
func TestNoHostAndAFarMachineAreRefusedRatherThanIgnored(t *testing.T) {
	for _, flags := range [][]string{
		{"--no-host", "--host", "devbox"},
		{"--no-host", "--at", "otter-lamp-42"},
	} {
		err := openChatV3("chat", flags, false)
		if err == nil {
			t.Fatalf("%v opened a conversation", flags)
		}
		if !strings.Contains(err.Error(), "--no-host") {
			t.Fatalf("%v was refused with %q, which does not name the flag", flags, err)
		}
	}
}

// AND THE USAGE LINE OFFERS IT, because a flag a person cannot discover is a
// flag they cannot reach on the day the host is the thing that is wrong.
func TestChatAndResumeBothOfferNoHostInTheirUsage(t *testing.T) {
	for _, name := range []string{"chat", "resume"} {
		err := openChatV3(name, []string{"a-word-no-flag-takes"}, name == "resume")
		if err == nil || !strings.Contains(err.Error(), "--no-host") {
			t.Fatalf("aforge %s's usage line reads %v", name, err)
		}
	}
}
