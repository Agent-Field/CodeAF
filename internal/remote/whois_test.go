package remote

// The version exchange, tested on a real pipe against the real server: what a
// host says about itself, what a pipe engine says instead, what a build that
// never heard the question says, and the clause the mismatch refusal grew the
// day it lied to somebody.

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// answered runs one server over net.Pipe and hands back the client's side.
func answered(t *testing.T, opts AttachOptions) net.Conn {
	t.Helper()
	surface, engine := net.Pipe()
	go func() { _ = ServeAttach(engine, engine, opts) }()
	t.Cleanup(func() {
		_ = surface.Close()
		_ = engine.Close()
	})
	_ = surface.SetDeadline(time.Now().Add(10 * time.Second))
	return surface
}

func TestAHostSaysWhichBuildItIsBeforeAnybodySaysHello(t *testing.T) {
	var asked WhoIs
	conn := answered(t, AttachOptions{
		Open: func(Hello) (*Session, error) { t.Fatal("the question opened a conversation"); return nil, nil },
		Host: func(ask WhoIs) HostSelf {
			asked = ask
			return HostSelf{Workspace: "/srv/app", Busy: true}
		},
	})

	self, err := AskHost(conn, WhoIs{})
	if err != nil {
		t.Fatalf("ask the host: %v", err)
	}
	// THE ANSWERING SIDE FILLS IN ITS OWN PROTOCOL. A host that could be told
	// what version to claim would be no answer at all.
	if self.Version != Version {
		t.Fatalf("the host said it speaks %d, want %d", self.Version, Version)
	}
	if !self.Busy {
		t.Fatal("a host with work in flight said it had none")
	}
	if self.Workspace != "/srv/app" {
		t.Fatalf("the host named %q as its workspace", self.Workspace)
	}
	if asked.StandDown {
		t.Fatal("a question that only asked was read as asking it to go")
	}
}

func TestAskingAHostToStandDownReachesTheHostAndComesBack(t *testing.T) {
	var asked WhoIs
	conn := answered(t, AttachOptions{
		Host: func(ask WhoIs) HostSelf {
			asked = ask
			return HostSelf{Retiring: true}
		},
	})

	self, err := AskHost(conn, WhoIs{StandDown: true, Anyway: true})
	if err != nil {
		t.Fatalf("ask the host to go: %v", err)
	}
	if !asked.StandDown || !asked.Anyway {
		t.Fatalf("the host was asked %+v", asked)
	}
	if !self.Retiring {
		t.Fatal("a host that agreed to go did not say so")
	}
}

// A bare `aforge engine` on a pipe is nobody's host: nothing it holds outlives
// the connection, so it can never be the stale middle half — and the answer
// says exactly that rather than pretending to be a host with nothing to do.
func TestAPipeEngineIsNobodysHost(t *testing.T) {
	surface, engine := net.Pipe()
	go func() {
		_ = Serve(engine, engine, Options{Boot: func(Hello) (*Engine, error) {
			t.Error("the question opened a conversation")
			return nil, nil
		}})
	}()
	defer surface.Close()
	_ = surface.SetDeadline(time.Now().Add(10 * time.Second))

	if _, err := AskHost(surface, WhoIs{}); err != ErrNoHostThere {
		t.Fatalf("a pipe engine answered the exchange with %v", err)
	}
}

// THE BUILD THIS WHOLE LANE EXISTS FOR: one that never heard the question. Its
// answer has always been the same refusal about a first frame that is not a
// hello, and reading that as "not this build" is what lets a rebuild recover.
func TestABuildFromBeforeTheQuestionReadsAsNotThisBuild(t *testing.T) {
	surface, engine := net.Pipe()
	go func() {
		defer engine.Close()
		lines := bufio.NewScanner(engine)
		if !lines.Scan() {
			return
		}
		refusal, _ := json.Marshal(Frame{
			Kind:  "fatal",
			Error: `engine: the first frame was "whois", not a hello`,
		})
		_, _ = engine.Write(append(refusal, '\n'))
	}()
	defer surface.Close()
	_ = surface.SetDeadline(time.Now().Add(10 * time.Second))

	if _, err := AskHost(surface, WhoIs{}); err != ErrNoHostThere {
		t.Fatalf("an older build's refusal came back as %v", err)
	}
}

// And a far end that says nothing at all reads the same way, because it is the
// same fact: whatever is on that socket cannot answer this question.
func TestAFarEndThatHangsUpReadsAsNotThisBuild(t *testing.T) {
	surface, engine := net.Pipe()
	go func() {
		_, _ = io.Copy(io.Discard, engine)
	}()
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = engine.Close()
	}()
	defer surface.Close()
	_ = surface.SetDeadline(time.Now().Add(10 * time.Second))

	if _, err := AskHost(surface, WhoIs{}); err == nil {
		t.Fatal("a far end that hung up was read as an answer")
	}
}

// ── the clause the refusal was missing ──────────────────────────────────────

// The sentence that trapped somebody: `aforge version` on the far machine said
// the new build, both halves WERE the same build, and the refusal still told
// them to update the older one — because the half doing the refusing was a host
// left running from before the rebuild. When there is a host behind the
// connection, the refusal says the other thing that is true.
func TestTheMismatchRefusalNamesTheOlderBuildStillRunningHere(t *testing.T) {
	conn := answered(t, AttachOptions{
		Open: func(Hello) (*Session, error) { return nil, nil },
		Host: func(WhoIs) HostSelf { return HostSelf{} },
	})
	reason := helloRefusal(t, conn, Hello{Version: Version + 1})
	if !strings.Contains(reason, "still running the older one") {
		t.Fatalf("the refusal from a host said %q", reason)
	}
	if !strings.Contains(reason, "aforge engine --stop") {
		t.Fatalf("the refusal named no way out: %q", reason)
	}
	if !strings.Contains(reason, "the two halves have to be the same build") {
		t.Fatalf("the refusal lost the fact it always stated: %q", reason)
	}
}

// A pipe engine keeps the sentence it always had, because for a pipe it is the
// whole truth: that process IS the build on that machine.
func TestTheMismatchRefusalOnAPipeSaysNothingAboutAHost(t *testing.T) {
	surface, engine := net.Pipe()
	go func() { _ = Serve(engine, engine, Options{Boot: func(Hello) (*Engine, error) { return nil, nil }}) }()
	defer surface.Close()
	_ = surface.SetDeadline(time.Now().Add(10 * time.Second))

	reason := helloRefusal(t, surface, Hello{Version: Version + 1})
	if strings.Contains(reason, "still running the older one") {
		t.Fatalf("a pipe engine claimed to be holding an older build: %q", reason)
	}
}

// helloRefusal says hello on a raw connection and returns the reason the far end
// refused with.
func helloRefusal(t *testing.T, conn net.Conn, hello Hello) string {
	t.Helper()
	payload, err := json.Marshal(hello)
	if err != nil {
		t.Fatalf("encode the hello: %v", err)
	}
	line, err := json.Marshal(Frame{Kind: "hello", Payload: payload})
	if err != nil {
		t.Fatalf("encode the frame: %v", err)
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		t.Fatalf("say hello: %v", err)
	}
	lines := bufio.NewScanner(conn)
	lines.Buffer(make([]byte, 0, 4*1024), 1<<20)
	if !lines.Scan() {
		t.Fatalf("no answer came back: %v", lines.Err())
	}
	var frame Frame
	if err := json.Unmarshal(lines.Bytes(), &frame); err != nil {
		t.Fatalf("the answer did not parse: %v", err)
	}
	if frame.Kind != "fatal" {
		t.Fatalf("the far end answered with a %q where a refusal belongs", frame.Kind)
	}
	return frame.Error
}
