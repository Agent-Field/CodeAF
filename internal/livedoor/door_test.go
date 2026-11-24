package livedoor

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"testing"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// TestAPairedViewerOpensAChatAnotherProcessHoldsAndSeesItsTail verifies contract
// V1 (router half): opening a channel to a held chat delivers the holder's
// welcome followed by its replayed event lines in order, and the rewritten
// hello the holder receives has Join:true, New:false, Back:true, the
// authenticated Remote stamp from Peer, the holder's Session and Workspace, and
// the viewer's Resume and ClientID preserved while overriding any spoofed
// viewer values for Remote, Watch, New, or Session.
func TestAPairedViewerOpensAChatAnotherProcessHoldsAndSeesItsTail(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gotHelloCh := make(chan remote.Hello, 1)
	wantSession := "/work/state/v3/projects/p/chat-v1/transcript.jsonl"
	wantWorkspace := "/work/project-v1"

	resolver := fakeResolverFunc(func(_ context.Context, chat string) (net.Conn, Route, error) {
		routerSide, holderSide := net.Pipe()
		guard.Go("livedoor.test.v1.holder", func() {
			defer holderSide.Close()
			scan := newLineScanner(holderSide, MaxLine)
			line, err := scan.ReadLine(FirstLineMax)
			if err != nil {
				return
			}
			var f remote.Frame
			if err := json.Unmarshal(bytes.TrimRight(line, "\r\n"), &f); err != nil {
				return
			}
			var h remote.Hello
			if err := json.Unmarshal(f.Payload, &h); err != nil {
				return
			}
			gotHelloCh <- h

			welcomePayload, _ := json.Marshal(remote.Welcome{
				Version:     remote.Version,
				Workspace:   h.Workspace,
				SessionFile: h.Session,
				Model:       "model-v1",
				Title:       "Tail Conversation",
				Remote:      h.Remote,
			})
			welcomeFrame, _ := json.Marshal(remote.Frame{
				Kind:    "welcome",
				Payload: welcomePayload,
			})
			if _, err := holderSide.Write(append(welcomeFrame, '\n')); err != nil {
				return
			}

			for i, text := range []string{"tail-part-1", "tail-part-2", "tail-part-3"} {
				evPayload, _ := json.Marshal(remote.WireEvent(session.Event{
					Kind: session.EventTextDelta,
					Text: text,
				}))
				evFrame, _ := json.Marshal(remote.Frame{
					Kind:    "event",
					ID:      1,
					Seq:     uint64(i + 1),
					Payload: evPayload,
				})
				if _, err := holderSide.Write(append(evFrame, '\n')); err != nil {
					return
				}
			}
		})
		return routerSide, Route{
			Kind:      "daemon",
			PID:       5001,
			Session:   wantSession,
			Workspace: wantWorkspace,
		}, nil
	})

	vConn, hConn := net.Pipe()
	t.Cleanup(func() {
		_ = vConn.Close()
		_ = hConn.Close()
	})

	// Peer is watch-only (Drive:false), while the viewer attempts to spoof
	// Drive:true, Watch:false, New:true, and Session:"/etc/x" in its Hello.
	peer := Peer{
		Device: "dev-paired-v1",
		Label:  "airbook",
		Permit: pair.Permit{Watch: true, Drive: false},
		Class:  pair.ClassLegacy,
	}
	guard.Go("livedoor.test.v1.serve", func() {
		_ = Serve(ctx, hConn, peer, ServeConfig{
			Resolver: resolver,
			Host:     "studio",
			Road:     "relay",
		})
	})

	vMux := NewMux(vConn, Viewer, MuxConfig{})
	t.Cleanup(func() { _ = vMux.Close() })
	ack, err := vMux.WaitHelloAck(ctx)
	if err != nil {
		t.Fatalf("WaitHelloAck failed: %v", err)
	}
	if !ack.Watch || ack.Drive || ack.Road != "relay" {
		t.Fatalf("HelloAck = %+v, want Watch=true Drive=false Road=relay", ack)
	}

	vCh, err := vMux.Open(ctx, "chat-v1")
	if err != nil {
		t.Fatalf("Open(chat-v1) failed: %v", err)
	}

	spoofedHello := remote.Hello{
		Version:         remote.Version,
		Workspace:       "/etc/shadow-dir",
		Session:         "/etc/x",
		SessionInstance: "inst-42",
		ClientID:        "client-keep-me",
		Surface:         "airbook-surface",
		Encodings:       []string{"gzip"},
		Resume:          []remote.StreamCursor{{Stream: 9, Seq: 4}},
		Join:            false,
		New:             true,
		Watch:           false,
		Back:            false,
		Remote: &remote.RemoteViewer{
			Device: "spoofed-root",
			Label:  "spoofed",
			Watch:  true,
			Drive:  true,
			Road:   "local",
		},
	}
	hPayload, _ := json.Marshal(spoofedHello)
	hLine, _ := json.Marshal(remote.Frame{Kind: "hello", Payload: hPayload})
	if _, err := vCh.Write(append(hLine, '\n')); err != nil {
		t.Fatalf("writing viewer hello failed: %v", err)
	}

	gotHello := <-gotHelloCh
	if !gotHello.Join || gotHello.New || !gotHello.Back || !gotHello.Watch {
		t.Fatalf("rewritten hello flags: Join=%v New=%v Back=%v Watch=%v, want Join=true New=false Back=true Watch=true",
			gotHello.Join, gotHello.New, gotHello.Back, gotHello.Watch)
	}
	if gotHello.Session != wantSession || gotHello.Workspace != wantWorkspace {
		t.Fatalf("rewritten hello Session=%q Workspace=%q, want %q and %q",
			gotHello.Session, gotHello.Workspace, wantSession, wantWorkspace)
	}
	wantRemote := remote.RemoteViewer{
		Device: peer.Device,
		Label:  peer.Label,
		Watch:  true,
		Drive:  false,
		Road:   "relay",
	}
	if gotHello.Remote == nil || *gotHello.Remote != wantRemote {
		t.Fatalf("rewritten hello Remote = %+v, want %+v", gotHello.Remote, wantRemote)
	}
	if gotHello.ClientID != "client-keep-me" || gotHello.SessionInstance != "inst-42" || gotHello.Surface != "airbook-surface" || gotHello.Version != remote.Version {
		t.Fatalf("rewritten hello lost pass-through fields: %+v", gotHello)
	}
	if len(gotHello.Resume) != 1 || gotHello.Resume[0] != (remote.StreamCursor{Stream: 9, Seq: 4}) {
		t.Fatalf("rewritten hello Resume = %+v, want [{Stream:9 Seq:4}]", gotHello.Resume)
	}
	if len(gotHello.Encodings) != 1 || gotHello.Encodings[0] != "gzip" {
		t.Fatalf("rewritten hello Encodings = %+v, want [gzip]", gotHello.Encodings)
	}

	vScan := newLineScanner(vCh, MaxLine)
	wLine, err := vScan.ReadLine(MaxLine)
	if err != nil {
		t.Fatalf("reading welcome line failed: %v", err)
	}
	var wFrame remote.Frame
	if err := json.Unmarshal(bytes.TrimRight(wLine, "\r\n"), &wFrame); err != nil || wFrame.Kind != "welcome" {
		t.Fatalf("first reply frame = %+v (err=%v), want welcome", wFrame, err)
	}

	wantTexts := []string{"tail-part-1", "tail-part-2", "tail-part-3"}
	for i, wantText := range wantTexts {
		evLine, err := vScan.ReadLine(MaxLine)
		if err != nil {
			t.Fatalf("reading event %d failed: %v", i+1, err)
		}
		var evFrame remote.Frame
		if err := json.Unmarshal(bytes.TrimRight(evLine, "\r\n"), &evFrame); err != nil {
			t.Fatalf("unmarshaling event %d failed: %v", i+1, err)
		}
		if evFrame.Kind != "event" || evFrame.Seq != uint64(i+1) {
			t.Fatalf("event %d frame = {Kind:%q Seq:%d}, want event Seq=%d", i+1, evFrame.Kind, evFrame.Seq, i+1)
		}
		var ew remote.EventWire
		if err := json.Unmarshal(evFrame.Payload, &ew); err != nil || ew.Text != wantText {
			t.Fatalf("event %d Text = %q (err=%v), want %q", i+1, ew.Text, err, wantText)
		}
	}
}

// TestATurnRunningOnTheHostReachesTheViewerBeforeItEnds verifies contract V2:
// when the holder emits a live turn event line and then blocks on an unclosed
// gate before finishing the turn, the viewer reads the event line while the
// turn gate is still closed.
func TestATurnRunningOnTheHostReachesTheViewerBeforeItEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	turnGate := make(chan struct{})
	defer close(turnGate)

	resolver := fakeResolverFunc(func(_ context.Context, chat string) (net.Conn, Route, error) {
		routerSide, holderSide := net.Pipe()
		guard.Go("livedoor.test.v2.holder", func() {
			defer holderSide.Close()
			scan := newLineScanner(holderSide, MaxLine)
			line, err := scan.ReadLine(FirstLineMax)
			if err != nil {
				return
			}
			var f remote.Frame
			if json.Unmarshal(bytes.TrimRight(line, "\r\n"), &f) != nil {
				return
			}
			var h remote.Hello
			if json.Unmarshal(f.Payload, &h) != nil {
				return
			}
			wPayload, _ := json.Marshal(remote.Welcome{
				Version:     remote.Version,
				Workspace:   h.Workspace,
				SessionFile: h.Session,
				Live:        1,
				Remote:      h.Remote,
			})
			wFrame, _ := json.Marshal(remote.Frame{Kind: "welcome", Payload: wPayload})
			if _, err := holderSide.Write(append(wFrame, '\n')); err != nil {
				return
			}

			deltaPayload, _ := json.Marshal(remote.WireEvent(session.Event{
				Kind: session.EventTextDelta,
				Text: "streaming-before-turn-ends",
			}))
			deltaFrame, _ := json.Marshal(remote.Frame{
				Kind:    "event",
				ID:      1,
				Seq:     1,
				Payload: deltaPayload,
			})
			if _, err := holderSide.Write(append(deltaFrame, '\n')); err != nil {
				return
			}

			// Wait on turnGate before ending the turn.
			<-turnGate
		})
		return routerSide, Route{
			Kind:      "daemon",
			PID:       5002,
			Session:   "/work/v2/transcript.jsonl",
			Workspace: "/work/v2",
		}, nil
	})

	vConn, hConn := net.Pipe()
	t.Cleanup(func() {
		_ = vConn.Close()
		_ = hConn.Close()
	})
	peer := Peer{
		Device: "dev-v2",
		Label:  "laptop",
		Permit: pair.Permit{Watch: true, Drive: true},
	}
	guard.Go("livedoor.test.v2.serve", func() {
		_ = Serve(ctx, hConn, peer, ServeConfig{
			Resolver: resolver,
			Host:     "workstation",
			Road:     "relay",
		})
	})

	vMux := NewMux(vConn, Viewer, MuxConfig{})
	t.Cleanup(func() { _ = vMux.Close() })
	if _, err := vMux.WaitHelloAck(ctx); err != nil {
		t.Fatalf("WaitHelloAck failed: %v", err)
	}
	vCh, err := vMux.Open(ctx, "chat-v2")
	if err != nil {
		t.Fatalf("Open(chat-v2) failed: %v", err)
	}
	hPayload, _ := json.Marshal(remote.Hello{Version: remote.Version})
	hLine, _ := json.Marshal(remote.Frame{Kind: "hello", Payload: hPayload})
	if _, err := vCh.Write(append(hLine, '\n')); err != nil {
		t.Fatalf("writing hello failed: %v", err)
	}

	vScan := newLineScanner(vCh, MaxLine)
	if _, err := vScan.ReadLine(MaxLine); err != nil {
		t.Fatalf("reading welcome failed: %v", err)
	}

	// Read the event while turnGate is still closed.
	evLine, err := vScan.ReadLine(MaxLine)
	if err != nil {
		t.Fatalf("reading mid-turn event failed: %v", err)
	}
	select {
	case <-turnGate:
		t.Fatal("turnGate was already open when event arrived")
	default:
	}
	var evFrame remote.Frame
	if err := json.Unmarshal(bytes.TrimRight(evLine, "\r\n"), &evFrame); err != nil || evFrame.Kind != "event" {
		t.Fatalf("mid-turn frame = %+v (err=%v), want event", evFrame, err)
	}
	var ew remote.EventWire
	if err := json.Unmarshal(evFrame.Payload, &ew); err != nil || ew.Text != "streaming-before-turn-ends" {
		t.Fatalf("mid-turn EventWire.Text = %q (err=%v), want %q", ew.Text, err, "streaming-before-turn-ends")
	}
}

// TestAWatchOnlyPeerIsRefusedCallsThatTypeAndToldWhy verifies contract V11
// (router half): a peer with Permit{Watch:true, Drive:false} is stamped with
// Drive:false, a Submit call is intercepted at the router without reaching the
// holder and answered with a result frame carrying the same call ID and
// remote.GuestRefusal(host, "Submit", false), and an allowed read call (Ping)
// is forwarded to the holder.
func TestAWatchOnlyPeerIsRefusedCallsThatTypeAndToldWhy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const hostName = "mini-server"
	gotStampCh := make(chan remote.RemoteViewer, 1)
	holderCalls := make(chan remote.Frame, 4)

	resolver := fakeResolverFunc(func(_ context.Context, chat string) (net.Conn, Route, error) {
		routerSide, holderSide := net.Pipe()
		guard.Go("livedoor.test.v11.holder", func() {
			defer holderSide.Close()
			scan := newLineScanner(holderSide, MaxLine)
			line, err := scan.ReadLine(FirstLineMax)
			if err != nil {
				return
			}
			var f remote.Frame
			if json.Unmarshal(bytes.TrimRight(line, "\r\n"), &f) != nil {
				return
			}
			var h remote.Hello
			if json.Unmarshal(f.Payload, &h) != nil || h.Remote == nil {
				return
			}
			gotStampCh <- *h.Remote

			wPayload, _ := json.Marshal(remote.Welcome{
				Version:     remote.Version,
				Workspace:   h.Workspace,
				SessionFile: h.Session,
				Remote:      h.Remote,
			})
			wFrame, _ := json.Marshal(remote.Frame{Kind: "welcome", Payload: wPayload})
			if _, err := holderSide.Write(append(wFrame, '\n')); err != nil {
				return
			}

			for {
				cLine, err := scan.ReadLine(MaxLine)
				if err != nil {
					return
				}
				var call remote.Frame
				if json.Unmarshal(bytes.TrimRight(cLine, "\r\n"), &call) == nil {
					holderCalls <- call
					resFrame, _ := json.Marshal(remote.Frame{
						Kind: "result",
						ID:   call.ID,
					})
					_, _ = holderSide.Write(append(resFrame, '\n'))
				}
			}
		})
		return routerSide, Route{
			Kind:      "daemon",
			PID:       5003,
			Session:   "/work/v11/transcript.jsonl",
			Workspace: "/work/v11",
		}, nil
	})

	vConn, hConn := net.Pipe()
	t.Cleanup(func() {
		_ = vConn.Close()
		_ = hConn.Close()
	})
	peer := Peer{
		Device: "dev-watch-only",
		Label:  "ipad",
		Permit: pair.Permit{Watch: true, Drive: false},
	}
	guard.Go("livedoor.test.v11.serve", func() {
		_ = Serve(ctx, hConn, peer, ServeConfig{
			Resolver: resolver,
			Host:     hostName,
			Road:     "relay",
		})
	})

	vMux := NewMux(vConn, Viewer, MuxConfig{})
	t.Cleanup(func() { _ = vMux.Close() })
	if _, err := vMux.WaitHelloAck(ctx); err != nil {
		t.Fatalf("WaitHelloAck failed: %v", err)
	}
	vCh, err := vMux.Open(ctx, "chat-v11")
	if err != nil {
		t.Fatalf("Open(chat-v11) failed: %v", err)
	}
	hPayload, _ := json.Marshal(remote.Hello{Version: remote.Version})
	hLine, _ := json.Marshal(remote.Frame{Kind: "hello", Payload: hPayload})
	if _, err := vCh.Write(append(hLine, '\n')); err != nil {
		t.Fatalf("writing hello failed: %v", err)
	}

	vScan := newLineScanner(vCh, MaxLine)
	if _, err := vScan.ReadLine(MaxLine); err != nil {
		t.Fatalf("reading welcome failed: %v", err)
	}

	stamp := <-gotStampCh
	if !stamp.Watch || stamp.Drive {
		t.Fatalf("holder saw stamp %+v, want Watch=true Drive=false", stamp)
	}

	// 1. Send a disallowed Submit call (ID 41) and an allowed Ping call (ID 42).
	submitCall, _ := json.Marshal(remote.Frame{
		Kind:   "call",
		ID:     41,
		Method: remote.MethodSubmit,
	})
	if _, err := vCh.Write(append(submitCall, '\n')); err != nil {
		t.Fatalf("writing Submit call failed: %v", err)
	}

	res1Line, err := vScan.ReadLine(MaxLine)
	if err != nil {
		t.Fatalf("reading Submit refusal result failed: %v", err)
	}
	var res1 remote.Frame
	if err := json.Unmarshal(bytes.TrimRight(res1Line, "\r\n"), &res1); err != nil {
		t.Fatalf("unmarshaling Submit result failed: %v", err)
	}
	wantRefusal := remote.GuestRefusal(hostName, remote.MethodSubmit, false)
	if res1.Kind != "result" || res1.ID != 41 || res1.Error != wantRefusal {
		t.Fatalf("Submit result = %+v, want Kind=result ID=41 Error=%q", res1, wantRefusal)
	}

	// 2. Send an allowed Ping call (ID 42) and verify the holder receives ONLY Ping.
	pingCall, _ := json.Marshal(remote.Frame{
		Kind:   "call",
		ID:     42,
		Method: remote.MethodPing,
	})
	if _, err := vCh.Write(append(pingCall, '\n')); err != nil {
		t.Fatalf("writing Ping call failed: %v", err)
	}

	res2Line, err := vScan.ReadLine(MaxLine)
	if err != nil {
		t.Fatalf("reading Ping result failed: %v", err)
	}
	var res2 remote.Frame
	if err := json.Unmarshal(bytes.TrimRight(res2Line, "\r\n"), &res2); err != nil {
		t.Fatalf("unmarshaling Ping result failed: %v", err)
	}
	if res2.Kind != "result" || res2.ID != 42 || res2.Error != "" {
		t.Fatalf("Ping result = %+v, want Kind=result ID=42 Error=\"\"", res2)
	}

	forwarded := <-holderCalls
	if forwarded.ID != 42 || forwarded.Method != remote.MethodPing {
		t.Fatalf("holder received call %+v, want only Ping (ID 42), never Submit", forwarded)
	}
	select {
	case extra := <-holderCalls:
		t.Fatalf("holder received unexpected extra call: %+v", extra)
	default:
	}
}
