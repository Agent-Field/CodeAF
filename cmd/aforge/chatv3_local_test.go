package main

// THE WHEN-TO-TAKE RULE, asked every way a launch can meet it.
//
// The rule decides which door a person's `aforge chat` goes through. The host
// road is the ordinary one — it is what makes the work outlive the terminal —
// and the launches that keep the in-process door keep it because something real
// about them lives in this process: onboarding, --once, --debug, --no-host.

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/enginehost"
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

// The ordinary launch takes the host road. That is the default, and it is what
// makes work outlive the terminal it was started in.
func TestAnOrdinaryLaunchTakesTheHostRoad(t *testing.T) {
	shortStateRoot(t)
	workspace := t.TempDir()
	if !v3TakeHostRoad(workspace, v3HostChoice{}) {
		t.Fatal("an ordinary launch opened its conversation in this process")
	}
	if v3TakeHostRoad("", v3HostChoice{}) {
		t.Fatal("a workspace that could not be resolved sent the launch down the host road")
	}
}

// A machine that is not set up yet stays here. Connecting a provider is a
// conversation with the person at this terminal (internal/tui3's firstrun.go) and
// a host has no terminal to have it in, so a launch that might need onboarding
// must not daemonize before there is a key.
func TestAnUnconfiguredMachineKeepsTheInProcessDoor(t *testing.T) {
	shortStateRoot(t)
	workspace := t.TempDir()
	if v3TakeHostRoad(workspace, v3HostChoice{setup: true}) {
		t.Fatal("a launch that may still have to set this machine up spawned a host")
	}
}

// And that answer is read off the machine rather than assumed: with no key
// anywhere the setup gate is on, and with one it is off.
func TestTheSetupGateReadsWhetherThisMachineCanTalkToAModel(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	if v3MachineIsSetUp() {
		t.Fatal("a machine with no key anywhere said it was set up")
	}
	t.Setenv("OPENROUTER_API_KEY", "sk-test-not-a-real-key")
	if !v3MachineIsSetUp() {
		t.Fatal("a machine with a key said it still had to be set up")
	}
}

// --debug records the model calls THIS PROCESS makes; over a socket the calls
// are the host's, and it was never told to record. So the flag keeps the launch
// where it is real rather than writing nothing.
func TestDebugKeepsTheInProcessDoor(t *testing.T) {
	shortStateRoot(t)
	workspace := t.TempDir()
	if v3TakeHostRoad(workspace, v3HostChoice{debug: true}) {
		t.Fatal("--debug went down a road where the record would have been empty")
	}
}

// --once never starts a host — a resident process left behind by a headless
// command is a surprise — but it joins one that is there, so a scripted message
// lands in the conversation a person is actually in.
func TestOnceJoinsAHostButNeverStartsOne(t *testing.T) {
	root := shortStateRoot(t)
	workspace := t.TempDir()
	if v3TakeHostRoad(workspace, v3HostChoice{once: true}) {
		t.Fatal("a headless message started a session host")
	}
	// And the question left nothing on disk where a host would live.
	if entries, err := os.ReadDir(filepath.Join(root, "v3", "hosts")); err == nil && len(entries) > 0 {
		t.Fatalf("asking whether a host is there made %d directories", len(entries))
	}
	answeringHost(t, workspace)
	if !v3TakeHostRoad(workspace, v3HostChoice{once: true}) {
		t.Fatal("a host was answering and the headless message opened its own conversation")
	}
}

// --no-host is the escape hatch and it beats everything: it is what a person
// types on the day the host itself is the thing that is wrong.
func TestNoHostKeepsTheInProcessDoor(t *testing.T) {
	shortStateRoot(t)
	workspace := t.TempDir()
	answeringHost(t, workspace)
	if v3TakeHostRoad(workspace, v3HostChoice{noHost: true}) {
		t.Fatal("--no-host still went looking for a session host")
	}
}

// The per-launch posture travels, and a launch that asked for nothing sends
// nothing: nil is the engine's own defaults, which is what every remote surface
// sends and what the engine reads.
func TestTheLaunchShapeIsCarriedOnlyWhenSomethingWasAskedFor(t *testing.T) {
	if shape := v3LaunchShape(false, false, false, 0, 0, false); shape != nil {
		t.Fatalf("a bare launch carried a shape: %+v", shape)
	}
	if bare := v3LaunchShape(false, false, false, 0, 0, true); bare == nil || !bare.Interactive {
		t.Fatal("bare interactive launch lost its mode")
	}
	shape := v3LaunchShape(true, false, true, 0, 12.5, true)
	if shape == nil || !shape.Yolo || !shape.OneModel || !shape.Interactive || shape.MaxCost != 12.5 || shape.NoCompact {
		t.Fatalf("the shape carried %+v", shape)
	}
	if shape.Same(nil) {
		t.Fatal("a shape with flags in it read as the engine's defaults")
	}
	// A --once probe is the one dial that is not steered, and the shape is
	// where the engine learns it: Same would otherwise read a probe and a
	// surface as one conversation.
	probe := v3LaunchShape(true, false, true, 0, 12.5, false)
	if probe.Same(shape) {
		t.Fatal("a --once probe and a steered surface read as the same posture")
	}
	if said := launchShapeWords(shape); !strings.Contains(said, "interactive chat") {
		t.Fatalf("an interactive shape is not named in the words a person reads: %q", said)
	}
}

// A conversation already open keeps the shape it was opened with, and the
// sentence a person reads names both postures: the one running and the one they
// asked for. Neither half alone is actionable.
func TestTheShapeRefusalNamesWhatIsRunningAndWhatWasAsked(t *testing.T) {
	said := hostShapeSentence(v3LaunchShape(true, false, false, 0, 0, true), nil)
	for _, want := range []string{"--yolo", "the default posture", "ends when this terminal does"} {
		if !strings.Contains(said, want) {
			t.Fatalf("the sentence %q does not say %q", said, want)
		}
	}
}

// A FALLBACK NOTICE SPEAKS TO THE PERSON, NOT ABOUT THE HOST MACHINERY. The
// path refusal names its limit and its way out, a host that never appeared says
// what happened plainly, and a sentence already written for a person is not
// rewritten on the way to the screen.
func TestALongStatePathIsSaidInWordsAndNotInTheEnginesOwn(t *testing.T) {
	tooLong := hostFallbackReason(enginehost.ErrSocketPathTooLong)
	if !strings.Contains(tooLong, "AFORGE_HOME moves it somewhere shorter") {
		t.Fatalf("the path refusal offers no way out: %q", tooLong)
	}
	if !strings.Contains(tooLong, strconv.Itoa(enginehost.SocketLimit)) {
		t.Fatalf("the path refusal does not name the %d-byte limit: %q", enginehost.SocketLimit, tooLong)
	}
	noHost := hostFallbackReason(enginehost.ErrNoHostAnswered)
	for name, said := range map[string]string{"too-long refusal": tooLong, "no-host refusal": noHost} {
		if strings.Contains(said, "engine host") || strings.Contains(said, "no host answered") {
			t.Fatalf("the %s speaks in machinery words: %q", name, said)
		}
	}
	written := busyEngineHostSentence()
	if got := hostFallbackReason(errors.New(written)); got != written {
		t.Fatalf("a sentence already written for a person became %q, want %q", got, written)
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
