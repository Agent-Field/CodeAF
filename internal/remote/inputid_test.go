package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
)

// persistedFakeAgent wraps fakeAgent with the marked submission doors and
// appends accepted person messages (including their ClientID) to a shared
// persisted transcript slice so a restarted holder (a new Session) can scan the
// transcript tail.
type persistedFakeAgent struct {
	*fakeAgent
	transcriptMu *sync.Mutex
	persisted    *[]session.DisplayEntry
	submissions  []session.Submission
}

func newPersistedFakeAgent(sharedMu *sync.Mutex, shared *[]session.DisplayEntry) *persistedFakeAgent {
	if sharedMu == nil {
		sharedMu = &sync.Mutex{}
	}
	if shared == nil {
		var list []session.DisplayEntry
		shared = &list
	}
	return &persistedFakeAgent{
		fakeAgent:    &fakeAgent{model: "a/b"},
		transcriptMu: sharedMu,
		persisted:    shared,
	}
}

func (p *persistedFakeAgent) recordSubmission(message session.Submission) {
	p.mu.Lock()
	p.submissions = append(p.submissions, message)
	p.mu.Unlock()
	p.transcriptMu.Lock()
	var mark *session.PersonMark
	if message.ClientID != "" || message.From != "" || message.Queued {
		mark = &session.PersonMark{
			ClientID: message.ClientID,
			From:     message.From,
			Queued:   message.Queued,
		}
	}
	*p.persisted = append(*p.persisted, session.DisplayEntry{
		Role:   "user",
		Text:   message.Text,
		Person: mark,
	})
	p.transcriptMu.Unlock()
}

func (p *persistedFakeAgent) SubmitMessage(ctx context.Context, message session.Submission) (<-chan session.Event, error) {
	p.recordSubmission(message)
	return p.fakeAgent.Submit(ctx, message.Text)
}

func (p *persistedFakeAgent) FollowUpMessage(message session.Submission) (<-chan session.Event, error) {
	p.recordSubmission(message)
	return p.fakeAgent.FollowUp(message.Text)
}

func (p *persistedFakeAgent) SteerMessage(message session.Submission) (<-chan session.Event, error) {
	p.recordSubmission(message)
	return p.fakeAgent.Steer(message.Text)
}

func (p *persistedFakeAgent) Transcript() []session.DisplayEntry {
	p.transcriptMu.Lock()
	defer p.transcriptMu.Unlock()
	out := make([]session.DisplayEntry, len(*p.persisted))
	copy(out, *p.persisted)
	return out
}

// swallowNextResultConn wraps the server side of a Pipe. When swallow is armed
// and the server writes a "result" frame, the wrapper drops that frame and
// closes both halves of the pipe so the client never receives the result.
type swallowNextResultConn struct {
	io.Reader
	out     io.WriteCloser
	peer    io.Closer
	swallow *atomic.Bool
	cut     chan struct{}
	cutOnce sync.Once
}

func (c *swallowNextResultConn) Write(p []byte) (int, error) {
	if c.swallow != nil && c.swallow.Load() {
		var frame Frame
		if json.Unmarshal(bytes.TrimSpace(p), &frame) == nil && frame.Kind == "result" {
			c.swallow.Store(false)
			_ = c.out.Close()
			_ = c.peer.Close()
			c.cutOnce.Do(func() { close(c.cut) })
			return 0, io.ErrClosedPipe
		}
	}
	return c.out.Write(p)
}

func (c *swallowNextResultConn) Close() error {
	return c.out.Close()
}

// TestAnInputSentBeforeADropIsNeverAppliedTwiceAfterResume verifies V23: a
// remote guest send (Submit, FollowUp, or Steer) carrying a ClientID whose
// result frame is lost to a dropped link is resent on resume and answered from
// the holder's repeat table (or from the persisted transcript tail after a
// holder restart) without delivering a second message or starting a second
// turn.
func TestAnInputSentBeforeADropIsNeverAppliedTwiceAfterResume(t *testing.T) {
	savedBackoff := firstBackoff
	firstBackoff = 5 * time.Millisecond
	t.Cleanup(func() { firstBackoff = savedBackoff })

	const sessionFile = "/home/somebody/.codeaf/v3/sessions/one.jsonl"
	guestStamp := &RemoteViewer{
		Device: "dev-phone",
		Label:  "phone",
		Watch:  true,
		Drive:  true,
		Road:   "relay",
	}

	t.Run("scenario A: Submit dropped before result is resent and applied once", func(t *testing.T) {
		agent := &fakeAgent{model: "a/b"}
		eng := engineOn(agent)
		eng.SessionFile = sessionFile
		sess := NewSession(eng, true)

		var swallow atomic.Bool
		var dials atomic.Int32
		cut := make(chan struct{})
		dialer := func() (io.ReadWriteCloser, error) {
			n := dials.Add(1)
			surface, engine := Pipe()
			var serverOut io.ReadWriteCloser = engine
			if n == 1 {
				serverOut = &swallowNextResultConn{
					Reader:  engine,
					out:     engine,
					peer:    surface,
					swallow: &swallow,
					cut:     cut,
				}
			}
			guard.Go("remote/test-v23-a", func() {
				_ = ServeAttach(serverOut, serverOut, AttachOptions{
					Open: func(Hello) (*Session, error) { return sess, nil },
				})
				_ = engine.Close()
			})
			return surface, nil
		}

		client, err := Roam("devbox", Hello{
			Session: sessionFile,
			Join:    true,
			Remote:  guestStamp,
		}, Roaming{
			Dial:             dialer,
			Window:           time.Second,
			ResendUnanswered: true,
		})
		if err != nil {
			t.Fatalf("roam: %v", err)
		}
		t.Cleanup(func() { _ = client.Close() })
		if !client.Welcome().MessageRepeat {
			t.Fatal("holder welcome did not advertise MessageRepeat")
		}

		swallow.Store(true)
		events, err := client.Agent().SubmitMessage(context.Background(), session.Submission{
			Text:     "deploy once",
			ClientID: "client-id-a",
		})
		if err != nil {
			t.Fatalf("SubmitMessage after drop and resend failed: %v", err)
		}
		<-cut
		if got := dials.Load(); got < 2 {
			t.Fatalf("expected redial after cut, got %d dials", got)
		}

		agent.mu.Lock()
		sent := append([]string(nil), agent.sent...)
		streamsCount := len(agent.streams)
		turn := agent.streams[0]
		agent.mu.Unlock()

		if !reflect.DeepEqual(sent, []string{"deploy once"}) {
			t.Fatalf("agent received messages %q, want exactly one delivery", sent)
		}
		if streamsCount != 1 {
			t.Fatalf("agent opened %d streams, want 1", streamsCount)
		}

		turn <- session.Event{Kind: session.EventTextDelta, Text: "deployed"}
		agent.finish(turn)
		var text []string
		for ev := range events {
			if ev.Kind == session.EventTextDelta {
				text = append(text, ev.Text)
			}
		}
		if strings.Join(text, "") != "deployed" {
			t.Fatalf("caller stream received %q, want %q", strings.Join(text, ""), "deployed")
		}
	})

	t.Run("scenario B: FollowUp and Steer dropped before result are resent and applied once", func(t *testing.T) {
		agent := &fakeAgent{model: "a/b"}
		eng := engineOn(agent)
		eng.SessionFile = sessionFile
		sess := NewSession(eng, true)

		var swallow atomic.Bool
		dialer := func() (io.ReadWriteCloser, error) {
			surface, engine := Pipe()
			serverOut := &swallowNextResultConn{
				Reader:  engine,
				out:     engine,
				peer:    surface,
				swallow: &swallow,
				cut:     make(chan struct{}),
			}
			guard.Go("remote/test-v23-b", func() {
				_ = ServeAttach(serverOut, serverOut, AttachOptions{
					Open: func(Hello) (*Session, error) { return sess, nil },
				})
				_ = engine.Close()
			})
			return surface, nil
		}

		client, err := Roam("devbox", Hello{
			Session: sessionFile,
			Join:    true,
			Remote:  guestStamp,
		}, Roaming{
			Dial:             dialer,
			Window:           time.Second,
			ResendUnanswered: true,
		})
		if err != nil {
			t.Fatalf("roam: %v", err)
		}
		t.Cleanup(func() { _ = client.Close() })

		// FollowUp with empty ClientID mints an rc- id and survives a drop before result.
		swallow.Store(true)
		followEvents, err := client.Agent().FollowUp("queue this once")
		if err != nil {
			t.Fatalf("FollowUp after drop and resend: %v", err)
		}
		if followEvents == nil {
			t.Fatal("FollowUp returned nil stream")
		}

		// Steer with empty ClientID mints an rc- id and survives a drop before result.
		swallow.Store(true)
		steerEvents, err := client.Agent().Steer("turn left once")
		if err != nil {
			t.Fatalf("Steer after drop and resend: %v", err)
		}
		if steerEvents == nil {
			t.Fatal("Steer returned nil stream")
		}

		agent.mu.Lock()
		follows := append([]string(nil), agent.follows...)
		steered := append([]string(nil), agent.steered...)
		streamsCount := len(agent.streams)
		agent.mu.Unlock()

		if !reflect.DeepEqual(follows, []string{"queue this once"}) {
			t.Fatalf("agent received follow-ups %q, want 1", follows)
		}
		if !reflect.DeepEqual(steered, []string{"turn left once"}) {
			t.Fatalf("agent received steers %q, want 1", steered)
		}
		if streamsCount != 2 {
			t.Fatalf("agent opened %d streams for FollowUp+Steer, want 2", streamsCount)
		}
	})

	t.Run("scenario C: holder replaced between send and resume finds ClientID in persisted transcript tail", func(t *testing.T) {
		var sharedMu sync.Mutex
		var persisted []session.DisplayEntry

		agent1 := newPersistedFakeAgent(&sharedMu, &persisted)
		eng1 := engineOn(agent1.fakeAgent)
		eng1.Agent = agent1
		eng1.SessionFile = sessionFile
		sess1 := NewSession(eng1, true)

		agent2 := newPersistedFakeAgent(&sharedMu, &persisted)
		eng2 := engineOn(agent2.fakeAgent)
		eng2.Agent = agent2
		eng2.SessionFile = sessionFile
		sess2 := NewSession(eng2, true)

		var swallow atomic.Bool
		var dials atomic.Int32
		dialer := func() (io.ReadWriteCloser, error) {
			n := dials.Add(1)
			surface, engine := Pipe()
			target := sess1
			var serverOut io.ReadWriteCloser = engine
			if n == 1 {
				serverOut = &swallowNextResultConn{
					Reader:  engine,
					out:     engine,
					peer:    surface,
					swallow: &swallow,
					cut:     make(chan struct{}),
				}
			} else {
				target = sess2
			}
			guard.Go("remote/test-v23-c", func() {
				_ = ServeAttach(serverOut, serverOut, AttachOptions{
					Open: func(Hello) (*Session, error) { return target, nil },
				})
				_ = engine.Close()
			})
			return surface, nil
		}

		client, err := Roam("devbox", Hello{
			Session: sessionFile,
			Join:    true,
			Remote:  guestStamp,
		}, Roaming{
			Dial:             dialer,
			Window:           time.Second,
			ResendUnanswered: true,
		})
		if err != nil {
			t.Fatalf("roam: %v", err)
		}
		t.Cleanup(func() { _ = client.Close() })

		swallow.Store(true)
		events, err := client.Agent().SubmitMessage(context.Background(), session.Submission{
			Text:     "survive holder restart",
			ClientID: "durable-id-c",
		})
		if err != nil {
			t.Fatalf("SubmitMessage across holder restart: %v", err)
		}
		for range events {
		}

		agent1.mu.Lock()
		sent1 := append([]string(nil), agent1.sent...)
		agent1.mu.Unlock()
		agent2.mu.Lock()
		sent2 := append([]string(nil), agent2.sent...)
		agent2.mu.Unlock()

		if !reflect.DeepEqual(sent1, []string{"survive holder restart"}) {
			t.Fatalf("first holder received %q, want 1 delivery", sent1)
		}
		if len(sent2) != 0 {
			t.Fatalf("restarted holder delivered repeat again: %q", sent2)
		}
	})

	t.Run("scenario D: two different devices with the same ClientID deliver two messages", func(t *testing.T) {
		agent := &fakeAgent{model: "a/b"}
		eng := engineOn(agent)
		eng.SessionFile = sessionFile
		sess := NewSession(eng, true)

		for i, dev := range []string{"dev-one", "dev-two"} {
			conn := dialSession(t, sess)
			welcome := conn.hello(Hello{
				Version: Version,
				Session: sessionFile,
				Join:    true,
				Remote: &RemoteViewer{
					Device: dev,
					Label:  dev,
					Watch:  true,
					Drive:  true,
					Road:   "relay",
				},
			})
			if welcome.Kind != "welcome" {
				t.Fatalf("device %s hello: %s", dev, welcome.Error)
			}
			conn.ok(uint64(i+1), MethodTake, nil)
			res := conn.ok(uint64(i+10), MethodSubmit, SubmitArgs{
				Text:     "from " + dev,
				ClientID: "same-client-id",
			})
			ref := decode[StreamRef](t, res.Payload)
			if ref.Stream == 0 {
				t.Fatalf("device %s got zero stream", dev)
			}
		}

		agent.mu.Lock()
		sent := append([]string(nil), agent.sent...)
		agent.mu.Unlock()
		if !reflect.DeepEqual(sent, []string{"from dev-one", "from dev-two"}) {
			t.Fatalf("agent received %q, want both deliveries", sent)
		}
	})

	t.Run("scenario E: with ResendUnanswered false the in-flight send fails on drop", func(t *testing.T) {
		agent := &fakeAgent{model: "a/b"}
		eng := engineOn(agent)
		eng.SessionFile = sessionFile
		sess := NewSession(eng, true)

		var swallow atomic.Bool
		dialer := func() (io.ReadWriteCloser, error) {
			surface, engine := Pipe()
			serverOut := &swallowNextResultConn{
				Reader:  engine,
				out:     engine,
				peer:    surface,
				swallow: &swallow,
				cut:     make(chan struct{}),
			}
			guard.Go("remote/test-v23-e", func() {
				_ = ServeAttach(serverOut, serverOut, AttachOptions{
					Open: func(Hello) (*Session, error) { return sess, nil },
				})
				_ = engine.Close()
			})
			return surface, nil
		}

		client, err := Roam("devbox", Hello{
			Session: sessionFile,
			Join:    true,
			Remote:  guestStamp,
		}, Roaming{
			Dial:             dialer,
			Window:           time.Second,
			ResendUnanswered: false,
		})
		if err != nil {
			t.Fatalf("roam: %v", err)
		}
		t.Cleanup(func() { _ = client.Close() })

		swallow.Store(true)
		_, err = client.Agent().SubmitMessage(context.Background(), session.Submission{
			Text:     "no resend",
			ClientID: "id-e",
		})
		if err == nil || !strings.Contains(err.Error(), "reconnecting to devbox") {
			t.Fatalf("expected roaming refusal when ResendUnanswered is false, got %v", err)
		}
	})

	t.Run("concurrency: 20 goroutines repeating one (device, id) produce one delivery", func(t *testing.T) {
		agent := &fakeAgent{model: "a/b"}
		eng := engineOn(agent)
		eng.SessionFile = sessionFile
		sess := NewSession(eng, true)
		sess.mu.Lock()
		// Leave driver nil so every concurrent connection from this device may send.
		sess.driver = nil
		sess.mu.Unlock()

		const workers = 20
		start := make(chan struct{})
		refs := make([]StreamRef, workers)
		errs := make([]error, workers)
		var wg sync.WaitGroup
		wg.Add(workers)
		for i := 0; i < workers; i++ {
			i := i
			guard.Go("remote/test-v23-concurrent", func() {
				defer wg.Done()
				s := &server{
					out:     io.Discard,
					session: sess,
					remote: &RemoteViewer{
						Device: "dev-concurrent",
						Label:  "phone",
						Watch:  true,
						Drive:  true,
						Road:   "relay",
					},
				}
				<-start
				raw, err := s.invoke(Frame{
					Kind:   "call",
					ID:     uint64(i + 1),
					Method: MethodSubmit,
					Payload: mustJSON(SubmitArgs{
						Text:     "concurrent send",
						ClientID: "shared-key",
					}),
				})
				if err != nil {
					errs[i] = err
					return
				}
				var ref StreamRef
				if err := json.Unmarshal(raw, &ref); err != nil {
					errs[i] = err
					return
				}
				refs[i] = ref
			})
		}
		close(start)
		wg.Wait()

		for i, err := range errs {
			if err != nil {
				t.Fatalf("worker %d failed: %v", i, err)
			}
		}
		wantStream := refs[0].Stream
		if wantStream == 0 {
			t.Fatal("expected non-zero stream id")
		}
		for i, ref := range refs {
			if ref.Stream != wantStream {
				t.Fatalf("worker %d got stream %d, want %d", i, ref.Stream, wantStream)
			}
		}
		agent.mu.Lock()
		sent := append([]string(nil), agent.sent...)
		streamsCount := len(agent.streams)
		agent.mu.Unlock()
		if !reflect.DeepEqual(sent, []string{"concurrent send"}) || streamsCount != 1 {
			t.Fatalf("agent got %d deliveries (%q) and %d streams, want 1", len(sent), sent, streamsCount)
		}
	})

	t.Run("table bound: 256 entries evicts the oldest and transcript scan bounds to last 200", func(t *testing.T) {
		var sharedMu sync.Mutex
		var persisted []session.DisplayEntry
		// Place an ancient entry before 200 newer entries so it sits outside the 200-entry tail scan.
		persisted = append(persisted, session.DisplayEntry{
			Role:   "user",
			Text:   "ancient",
			Person: &session.PersonMark{ClientID: "ancient-id"},
		})
		for i := 0; i < messageRepeatTailScan; i++ {
			persisted = append(persisted, session.DisplayEntry{
				Role: "assistant",
				Text: fmt.Sprintf("reply-%d", i),
			})
		}

		agent := newPersistedFakeAgent(&sharedMu, &persisted)
		eng := engineOn(agent.fakeAgent)
		eng.Agent = agent
		eng.SessionFile = sessionFile
		sess := NewSession(eng, true)
		s := &server{
			out:     io.Discard,
			session: sess,
			remote:  guestStamp,
		}

		// Fill the 256-entry table with id-0 .. id-255.
		for i := 0; i < messageRepeatCap; i++ {
			id := fmt.Sprintf("id-%d", i)
			if _, err := s.invoke(Frame{
				Kind:    "call",
				ID:      uint64(i + 1),
				Method:  MethodSteer,
				Payload: mustJSON(SubmitArgs{Text: "steer", ClientID: id}),
			}); err != nil {
				t.Fatalf("steer %d: %v", i, err)
			}
		}
		// Adding one more evicts id-0 from the in-memory table.
		if _, err := s.invoke(Frame{
			Kind:    "call",
			ID:      999,
			Method:  MethodSteer,
			Payload: mustJSON(SubmitArgs{Text: "steer-new", ClientID: "id-256"}),
		}); err != nil {
			t.Fatalf("steer 256: %v", err)
		}
		sess.repeatMu.Lock()
		tableLen := len(sess.repeats)
		_, hasOldest := sess.repeats[messageRepeatKey{device: guestStamp.Device, clientID: "id-0"}]
		_, hasSecond := sess.repeats[messageRepeatKey{device: guestStamp.Device, clientID: "id-1"}]
		sess.repeatMu.Unlock()
		if tableLen != messageRepeatCap || hasOldest || !hasSecond {
			t.Fatalf("repeat table len=%d hasOldest=%v hasSecond=%v", tableLen, hasOldest, hasSecond)
		}

		// And an entry older than the last 200 transcript entries is not treated as a recent repeat.
		if sess.transcriptHasClientID(agent, "ancient-id") {
			t.Fatal("transcript scan looked past the last 200 entries")
		}
		// While an entry inside the last 200 entries is found.
		if !sess.transcriptHasClientID(agent, "id-256") {
			t.Fatal("transcript scan missed an entry inside the last 200 entries")
		}
	})
}
