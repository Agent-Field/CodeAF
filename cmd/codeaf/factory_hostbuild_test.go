package main

// A WINDOW NEVER ATTACHES TO AN ENGINE OF ANOTHER BUILD.
//
// On 2026-10-08 the owner's window, a build with the factory, attached to an
// engine started two days earlier from an older binary. Every conversation it
// opened ran on that engine, whose belt had no `factory_add` and whose manual
// had no factory page, so "ship it to the factory" ran an old saved harness
// instead. These tests hold the launch road to leaving such an engine to the
// chats it already has and opening this window's own in this process.

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/remote"
)

// welcomingHost is a stand-in engine of THIS build as the stale-host question
// sees it (so the dial joins it), whose welcome names whatever build the test
// says. It counts the hellos it welcomed and the stand-downs it was asked for.
type welcomingHost struct {
	welcome remote.Welcome

	mu         sync.Mutex
	hellos     int
	standDowns int
	listener   net.Listener
	once       sync.Once
}

func welcomeAs(t *testing.T, workspace string, welcome remote.Welcome) *welcomingHost {
	t.Helper()
	if _, err := enginehost.Dir(workspace); err != nil {
		t.Fatalf("make somewhere for a host to live: %v", err)
	}
	socket, err := enginehost.SocketPath(workspace)
	if err != nil {
		t.Fatalf("resolve the socket: %v", err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen on the socket: %v", err)
	}
	welcome.Version = remote.Version
	if welcome.Workspace == "" {
		welcome.Workspace = workspace
	}
	host := &welcomingHost{welcome: welcome, listener: listener}
	t.Cleanup(host.close)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go host.answer(conn, workspace)
		}
	}()
	return host
}

func (h *welcomingHost) answer(conn net.Conn, workspace string) {
	defer conn.Close()
	lines := bufio.NewScanner(conn)
	lines.Buffer(make([]byte, 0, 64*1024), 1<<20)
	if !lines.Scan() {
		return
	}
	var frame remote.Frame
	if err := json.Unmarshal(lines.Bytes(), &frame); err != nil {
		return
	}
	if frame.Kind == "hello" {
		h.mu.Lock()
		h.hellos++
		h.mu.Unlock()
		reply, _ := json.Marshal(remote.Frame{Kind: "welcome", Payload: mustStandInJSON(h.welcome)})
		_, _ = conn.Write(append(reply, '\n'))
		// Hold the connection until the window lets go of it.
		for lines.Scan() {
		}
		return
	}
	var ask remote.WhoIs
	_ = json.Unmarshal(frame.Payload, &ask)
	if ask.StandDown {
		h.mu.Lock()
		h.standDowns++
		h.mu.Unlock()
	}
	self := remote.HostSelf{
		Version:   remote.Version,
		Build:     buildinfo.Identity(),
		BuiltAt:   enginehost.BuildMoment(),
		Binary:    enginehost.ThisBinary(),
		Workspace: workspace,
	}
	reply, _ := json.Marshal(remote.Frame{Kind: "whoami", Payload: mustStandInJSON(self)})
	_, _ = conn.Write(append(reply, '\n'))
}

func (h *welcomingHost) counts() (hellos, standDowns int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.hellos, h.standDowns
}

func (h *welcomingHost) close() { h.once.Do(func() { _ = h.listener.Close() }) }

// asThisWindow stands in for a build of this window for one test.
func asThisWindow(t *testing.T, identity, revision string) {
	t.Helper()
	stock := thisWindowBuild
	t.Cleanup(func() { thisWindowBuild = stock })
	thisWindowBuild = func() (string, string) { return identity, revision }
}

func TestAWindowLeavesAnEngineOfAnotherCleanBuildAndRunsItsOwn(t *testing.T) {
	shortEngineHome(t)
	workspace := t.TempDir()
	asThisWindow(t, "abc12345", "abc12345")
	const old = "0ld0ld00 built 2026-10-06 09:00"
	host := welcomeAs(t, workspace, remote.Welcome{Identity: "0ld0ld00", Build: old})

	err := openChatV3Local(localLaunch{workspace: workspace})
	var another *hostAnotherBuild
	if !errors.As(err, &another) {
		t.Fatalf("the launch answered %v, want the in-process road for another build", err)
	}
	want := "this workspace's engine is another build (" + old + ") · this window runs its own · " +
		"the old engine keeps the chats it already has; stop it when they are done: codeaf engine --stop --workspace " + workspace
	if another.sentence != want {
		t.Fatalf("the notice is\n  %q\nwant\n  %q", another.sentence, want)
	}
	// The caller's road for this answer is the in-process one, with the
	// sentence on the entry notice; nothing else may read it as a refusal.
	var refusal *hostRefusal
	var unreachable *hostUnreachable
	if errors.As(err, &refusal) || errors.As(err, &unreachable) {
		t.Fatalf("another build was answered as a refusal or a failure: %v", err)
	}
	if hellos, standDowns := host.counts(); hellos != 1 || standDowns != 0 {
		t.Fatalf("the old engine saw %d hellos and %d stand-downs, want one hello and none", hellos, standDowns)
	}
	if !hostAnswers(workspace) {
		t.Fatal("the old engine was stopped; it keeps the chats it already has")
	}
}

// The same build, and a dirty or dev build on either side, attach as they
// always have. A launch shape the stand-in did not open with is how the test
// sees that the road went past the build question: the shape refusal is the
// next thing an attached window asks.
func TestAWindowAttachesToItsOwnBuildAndToDirtyOrDevBuilds(t *testing.T) {
	cases := []struct {
		name               string
		identity, revision string
		welcome            remote.Welcome
	}{
		{"same build", "abc12345", "abc12345", remote.Welcome{Identity: "abc12345", Build: "abc12345 built 2026-10-06 09:00"}},
		{"engine dirty", "abc12345", "abc12345", remote.Welcome{Identity: "0ld0ld00/true/2026-10-06T09:00:00Z", Build: "0ld0ld00 (dirty) built 2026-10-06 09:00"}},
		{"engine dev", "abc12345", "abc12345", remote.Welcome{Build: "dev"}},
		{"window dirty", "abc12345/true/2026-10-08T09:00:00Z", "abc12345", remote.Welcome{Identity: "0ld0ld00", Build: "0ld0ld00 built 2026-10-06 09:00"}},
		{"window dev", "/false/2026-10-08T09:00:00Z", "", remote.Welcome{Identity: "0ld0ld00", Build: "0ld0ld00 built 2026-10-06 09:00"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			shortEngineHome(t)
			workspace := t.TempDir()
			asThisWindow(t, c.identity, c.revision)
			host := welcomeAs(t, workspace, c.welcome)

			err := openChatV3Local(localLaunch{workspace: workspace, shape: &remote.LaunchShape{Yolo: true}})
			var another *hostAnotherBuild
			if errors.As(err, &another) {
				t.Fatalf("the window left an engine it should attach to: %q", another.sentence)
			}
			var taken *hostShapeTaken
			if !errors.As(err, &taken) {
				t.Fatalf("the launch answered %v, want it attached (and then the shape question)", err)
			}
			if strings.Contains(taken.sentence, "another build") {
				t.Fatalf("an attached window spoke of another build: %q", taken.sentence)
			}
			if hellos, _ := host.counts(); hellos != 1 {
				t.Fatalf("the engine saw %d hellos, want one", hellos)
			}
		})
	}
}

// --no-host is still the in-process road, and it is the road a window of
// another build takes: no host is asked or started.
func TestNoHostStillOpensInThisProcess(t *testing.T) {
	workspace := t.TempDir()
	if v3TakeHostRoad(workspace, v3HostChoice{noHost: true}) {
		t.Fatal("--no-host took the host road")
	}
	if !v3TakeHostRoad(workspace, v3HostChoice{}) {
		t.Fatal("a plain interactive launch did not take the host road")
	}
}
