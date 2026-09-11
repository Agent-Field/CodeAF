package enginehost

// news_test.go is the one question this package owns about the live status row:
// A HOST RUNS MANY CONVERSATIONS AND THE READERS THAT MEASURE THEM ARE
// PROCESS-GLOBAL.
//
// internal/session keeps one phase desk and one lane desk for the whole build,
// deliberately (its lanenews.go says why), and one process with one window
// needs nothing more. A host is the build where that stops being enough:
// several conversations, several connections, and one reader for all of them.
// A piece of news that could not name its own conversation would be drawn on
// every window at once — somebody else's clock, somebody else's machine, under
// an answer this person is waiting for.
//
// THE FRAMES ARE READ RAW AND NOT THROUGH A CLIENT. internal/remote's client
// puts what it receives straight onto THIS process's own desk, which is exactly
// right in a surface and useless in a test that has to say which of two
// connections a frame went down. So each surface here is a pipe and a scanner.

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// newsStub is the do-nothing conversation with a name for its news, which is
// what makes it reachable from the newsroom at all (internal/remote's news.go).
type newsStub struct {
	stubAgent
	key string
}

func (a newsStub) NewsKey() string { return a.key }

// rawSurface is one connection read frame by frame, with nothing between the
// wire and the test.
type rawSurface struct {
	conn   net.Conn
	frames chan remote.Frame
}

// openRawSurface attaches one connection to the host and says hello on it,
// asking for the conversation named by transcript.
func openRawSurface(t *testing.T, host *Host, transcript, name string) *rawSurface {
	t.Helper()
	surface, engine := net.Pipe()
	go host.attach(engine)
	s := &rawSurface{conn: surface, frames: make(chan remote.Frame, 256)}
	go func() {
		scan := bufio.NewScanner(surface)
		scan.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for scan.Scan() {
			var frame remote.Frame
			if json.Unmarshal(scan.Bytes(), &frame) != nil {
				continue
			}
			s.frames <- frame
		}
		close(s.frames)
	}()
	t.Cleanup(func() { _ = surface.Close() })
	s.write(t, remote.Frame{Kind: "hello", Payload: mustNewsJSON(t, remote.Hello{
		Version: remote.Version, Surface: name, Session: transcript,
	})})
	if welcome := s.next(t, 5*time.Second); welcome == nil || welcome.Kind != "welcome" {
		t.Fatalf("the host answered the hello with %v, want a welcome", welcome)
	}
	return s
}

func (s *rawSurface) write(t *testing.T, frame remote.Frame) {
	t.Helper()
	line, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("encode frame: %v", err)
	}
	if _, err := s.conn.Write(append(line, '\n')); err != nil && err != io.EOF {
		t.Fatalf("write frame: %v", err)
	}
}

// next is the next frame, or nil when nothing comes within the wait.
func (s *rawSurface) next(t *testing.T, wait time.Duration) *remote.Frame {
	t.Helper()
	select {
	case frame, open := <-s.frames:
		if !open {
			return nil
		}
		return &frame
	case <-time.After(wait):
		return nil
	}
}

// awaitKind is the next frame of one kind, stepping over the pushes the engine
// makes on its own — a fact set, a driver note — that this test is not about.
func (s *rawSurface) awaitKind(t *testing.T, kind string, wait time.Duration) *remote.Frame {
	t.Helper()
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		frame := s.next(t, time.Until(deadline))
		if frame == nil {
			return nil
		}
		if frame.Kind == kind {
			return frame
		}
	}
	return nil
}

func mustNewsJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return raw
}

// newsHost is a host holding one conversation per transcript, each with a name
// for its news of its own — which is what a real host does: every conversation
// is a different agent with a different journal.
func newsHost(t *testing.T, workspace string) *Host {
	t.Helper()
	shortHome(t)
	host := stubHost(t, workspace)
	host.opts.Key = func(hello remote.Hello) string { return hello.Session }
	host.opts.Boot = func(hello remote.Hello) (*remote.Engine, error) {
		return &remote.Engine{
			Agent:       newsStub{key: "news-" + hello.Session},
			Workspace:   workspace,
			SessionFile: workspace + "/" + hello.Session,
		}, nil
	}
	t.Cleanup(func() {
		for _, sess := range host.held() {
			_ = sess.Close()
		}
	})
	return host
}

// EACH CONNECTION RECEIVES ONLY ITS OWN CONVERSATION'S NEWS.
func TestOneHostSendsEachConversationsNewsDownItsOwnConnection(t *testing.T) {
	workspace := "/home/somebody/api"
	host := newsHost(t, workspace)

	here := openRawSurface(t, host, "one.jsonl", "macbook")
	there := openRawSurface(t, host, "two.jsonl", "studio")
	waitUntil(t, "both conversations opened", func() bool { return len(host.held()) == 2 })
	waitUntil(t, "both windows have a live row", func() bool {
		for _, sess := range host.held() {
			if sess.Attached() == 0 {
				return false
			}
		}
		return true
	})
	// The outbox opens at the very end of an arrival, after the welcome is on
	// the wire, so a phase raised in the instant between the two is dropped —
	// right in a running build, where a phase says itself again every second,
	// and a race in a test that posts once. Saying it repeatedly is what the
	// engine does, so that is what this does.
	said := make(chan struct{})
	var once sync.Once
	go func() {
		for {
			select {
			case <-said:
				return
			default:
			}
			session.TellPhase(session.PhaseNews{
				Phase: session.PhaseRunning, Model: "openai/gpt-5", Role: lane.RoleTalk,
				Lane: "friendli", Rate: 38, Detail: "go test",
				Session: "news-one.jsonl", At: time.Now(),
			})
			time.Sleep(20 * time.Millisecond)
		}
	}()
	defer once.Do(func() { close(said) })

	frame := here.awaitKind(t, "phase", 5*time.Second)
	if frame == nil {
		t.Fatal("the conversation the phase named received no phase frame")
	}
	var wire remote.PhaseWire
	if err := json.Unmarshal(frame.Payload, &wire); err != nil {
		t.Fatalf("the phase frame did not parse: %v", err)
	}
	if wire.Lane != "friendli" || wire.Rate != 38 {
		t.Fatalf("the phase crossed as %+v, want friendli at 38 tok/s", wire)
	}
	once.Do(func() { close(said) })

	// AND THE OTHER WINDOW HEARD NOTHING ABOUT IT. Its own turn is what its row
	// is about, and somebody else's clock under this person's answer is worse
	// than no clock at all.
	if frame := there.awaitKind(t, "phase", 300*time.Millisecond); frame != nil {
		t.Fatalf("the other conversation was sent a phase frame: %s", frame.Payload)
	}
}

// THE SIGHTING GOES THE SAME WAY, which is what puts `via <machine>` back on
// the seam and the `served` row back in /status on a hosted conversation.
func TestOneHostSendsEachConversationsSightingDownItsOwnConnection(t *testing.T) {
	workspace := "/home/somebody/api"
	host := newsHost(t, workspace)

	here := openRawSurface(t, host, "one.jsonl", "macbook")
	there := openRawSurface(t, host, "two.jsonl", "studio")
	waitUntil(t, "both conversations opened", func() bool { return len(host.held()) == 2 })

	said := make(chan struct{})
	var once sync.Once
	go func() {
		for {
			select {
			case <-said:
				return
			default:
			}
			session.TellLane(session.LaneNews{
				Model: "openai/gpt-5", Lane: "coreweave", Role: lane.RoleTalk,
				TTFT: 3100 * time.Millisecond, Rate: 61,
				Session: "news-one.jsonl", At: time.Now(),
			})
			time.Sleep(20 * time.Millisecond)
		}
	}()
	defer once.Do(func() { close(said) })

	frame := here.awaitKind(t, "lane", 5*time.Second)
	if frame == nil {
		t.Fatal("the conversation the sighting named received no lane frame")
	}
	var wire remote.LaneWire
	if err := json.Unmarshal(frame.Payload, &wire); err != nil {
		t.Fatalf("the lane frame did not parse: %v", err)
	}
	if wire.Lane != "coreweave" || wire.TTFTMS != 3100 {
		t.Fatalf("the sighting crossed as %+v, want coreweave at 3.1s", wire)
	}
	once.Do(func() { close(said) })

	if frame := there.awaitKind(t, "lane", 300*time.Millisecond); frame != nil {
		t.Fatalf("the other conversation was sent a lane frame: %s", frame.Payload)
	}
}

// A WINDOW THAT HAS STOPPED READING DOES NOT STALL THE TURN IT IS MEASURING.
//
// The fan-out runs on a turn's own stream goroutine, between two deltas, so a
// connection whose far end has gone must not be able to hold up the answer the
// status row is describing. This drives it through the real host: a surface is
// attached and then never reads another byte, and the poster has to keep
// returning at once however much news is raised at it.
func TestNewsForAWindowThatStoppedReadingDoesNotStallThePoster(t *testing.T) {
	workspace := "/home/somebody/api"
	host := newsHost(t, workspace)

	surface, engine := net.Pipe()
	go host.attach(engine)
	t.Cleanup(func() { _ = surface.Close() })
	line, err := json.Marshal(remote.Frame{Kind: "hello", Payload: mustNewsJSON(t, remote.Hello{
		Version: remote.Version, Surface: "gone", Session: "one.jsonl",
	})})
	if err != nil {
		t.Fatalf("encode the hello: %v", err)
	}
	// The hello goes out and NOTHING IS EVER READ BACK. A net.Pipe is
	// unbuffered, so this surface is the worst case there is: every write the
	// engine makes blocks forever.
	go func() { _, _ = surface.Write(append(line, '\n')) }()
	waitUntil(t, "the conversation opened", func() bool { return len(host.held()) == 1 })
	waitUntil(t, "the window attached", func() bool {
		for _, sess := range host.held() {
			if sess.Attached() == 1 {
				return true
			}
		}
		return false
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 500; i++ {
			session.TellPhase(session.PhaseNews{
				Phase: session.PhaseRunning, Model: "openai/gpt-5", Role: lane.RoleTalk,
				Session: "news-one.jsonl", At: time.Now(),
			})
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a window that stopped reading held up the turn that was being measured")
	}
}
