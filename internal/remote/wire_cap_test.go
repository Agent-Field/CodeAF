package remote

import (
	"bufio"
	"encoding/json"
	"net"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
)

// TestAnOutboundLineOverTheCapIsRefusedAndTheReaderIsBounded verifies V18:
// (a) a result frame of DefaultMaxLine+1 bytes to a remote guest arrives as the
// error result with the same call ID while one of exactly DefaultMaxLine bytes
// arrives intact; (b) an oversized event arrives at a remote guest as a notice
// event carrying the same stream ID and sequence number, the next event's
// sequence number is contiguous, and a local surface receives the original
// oversized event; (c) a Client built with MaxLine fails on a line longer than
// the cap with the "too large to read" error while working for lines at the
// cap, and a Client built without MaxLine reads a 1 MiB line as before.
func TestAnOutboundLineOverTheCapIsRefusedAndTheReaderIsBounded(t *testing.T) {
	t.Run("outbound result cap at DefaultMaxLine and DefaultMaxLine+1", func(t *testing.T) {
		agent := &fakeAgent{}
		sess := heldSession(agent)
		t.Cleanup(func() { _ = agent.Close() })

		guest := dialSession(t, sess)
		welcome := guest.hello(Hello{
			Version: Version,
			Surface: "laptop",
			Session: sess.engine.SessionFile,
			Join:    true,
			Remote: &RemoteViewer{
				Device: "dev-guest",
				Label:  "laptop",
				Watch:  true,
				Drive:  true,
				Road:   "relay",
			},
		})
		if welcome.Kind != "welcome" {
			t.Fatalf("guest handshake answered %q: %s", welcome.Kind, welcome.Error)
		}

		// Compute the exact title length that produces a serialised result frame
		// of DefaultMaxLine bytes for call ID 1.
		baseAtCap, err := plainFrameLineLen(Frame{Kind: "result", ID: 1, Payload: raw(t, "")})
		if err != nil {
			t.Fatal(err)
		}
		exactTitle := strings.Repeat("a", DefaultMaxLine-baseAtCap)
		agent.SetModel(exactTitle)
		atCap := guest.call(1, MethodModel, nil)
		if atCap.Error != "" {
			t.Fatalf("result frame at exact DefaultMaxLine (%d bytes) was refused: %s", DefaultMaxLine, atCap.Error)
		}
		if got := decode[string](t, atCap.Payload); got != exactTitle {
			t.Fatalf("result at cap payload length = %d, want %d", len(got), len(exactTitle))
		}

		// Now make a result frame of DefaultMaxLine+1 bytes for call ID 2.
		baseOverCap, err := plainFrameLineLen(Frame{Kind: "result", ID: 2, Payload: raw(t, "")})
		if err != nil {
			t.Fatal(err)
		}
		overTitle := strings.Repeat("b", DefaultMaxLine+1-baseOverCap)
		agent.SetModel(overTitle)
		overCap := guest.call(2, MethodModel, nil)
		const wantErr = "too large for remote control; use the paged read"
		if overCap.ID != 2 || overCap.Error != wantErr {
			t.Fatalf("oversized result (DefaultMaxLine+1) = {ID:%d Error:%q}, want {ID:2 Error:%q}", overCap.ID, overCap.Error, wantErr)
		}
		if len(overCap.Payload) != 0 {
			t.Fatalf("oversized result still carried payload of %d bytes", len(overCap.Payload))
		}

		// Verify the same cap holds when gzip encoding is negotiated so a highly
		// compressible payload whose inflated line is DefaultMaxLine+1 bytes is
		// still refused while DefaultMaxLine bytes after inflate arrives intact.
		agent.SetModel("")
		gzipGuest := dialSession(t, sess)
		gzipWelcome := gzipGuest.hello(Hello{
			Version:   Version,
			Surface:   "gzip-laptop",
			Session:   sess.engine.SessionFile,
			Join:      true,
			Encodings: []string{frameEncodingGzip},
			Remote: &RemoteViewer{
				Device: "dev-gzip",
				Label:  "gzip-laptop",
				Watch:  true,
				Drive:  true,
				Road:   "relay",
			},
		})
		if gzipWelcome.Kind != "welcome" {
			t.Fatalf("gzip guest handshake: %s", gzipWelcome.Error)
		}
		agent.SetModel(exactTitle)
		gzipAtCap := gzipGuest.call(1, MethodModel, nil)
		if gzipAtCap.Error != "" {
			t.Fatalf("gzip guest result at DefaultMaxLine refused: %s", gzipAtCap.Error)
		}
		if err := expandFrame(&gzipAtCap); err != nil {
			t.Fatal(err)
		}
		if got := decode[string](t, gzipAtCap.Payload); got != exactTitle {
			t.Fatalf("gzip guest result at cap len = %d, want %d", len(got), len(exactTitle))
		}
		agent.SetModel(overTitle)
		gzipOverCap := gzipGuest.call(2, MethodModel, nil)
		if gzipOverCap.ID != 2 || gzipOverCap.Error != wantErr {
			t.Fatalf("gzip guest oversized result = {ID:%d Error:%q}, want {ID:2 Error:%q}", gzipOverCap.ID, gzipOverCap.Error, wantErr)
		}

		// Any other oversized frame kind is dropped and counted without tearing
		// the connection.
		var sink strings.Builder
		srv := &server{
			out:    &sink,
			remote: &RemoteViewer{Device: "dev-count", Label: "guest", Watch: true},
		}
		if err := srv.send(Frame{Kind: "facts", Payload: raw(t, strings.Repeat("f", DefaultMaxLine))}); err != nil {
			t.Fatalf("dropping oversized non-result non-event frame returned error: %v", err)
		}
		if got := srv.droppedOversize.Load(); got != 1 {
			t.Fatalf("droppedOversize = %d, want 1", got)
		}
		if sink.Len() != 0 {
			t.Fatalf("dropped oversized frame wrote %d bytes", sink.Len())
		}
	})

	t.Run("oversized event becomes notice with same stream and seq for guest while local gets original", func(t *testing.T) {
		agent := &fakeAgent{}
		sess := heldSession(agent)
		t.Cleanup(func() { _ = agent.Close() })

		local := dialSession(t, sess)
		if frame := local.hello(Hello{Version: Version, Surface: "macbook"}); frame.Kind != "welcome" {
			t.Fatalf("local handshake: %s", frame.Error)
		}

		guest := dialSession(t, sess)
		if frame := guest.hello(Hello{
			Version: Version,
			Surface: "tablet",
			Session: sess.engine.SessionFile,
			Join:    true,
			Remote: &RemoteViewer{
				Device: "dev-tablet",
				Label:  "tablet",
				Watch:  true,
				Drive:  false,
				Road:   "relay",
			},
		}); frame.Kind != "welcome" {
			t.Fatalf("guest handshake: %s", frame.Error)
		}

		ref := decode[StreamRef](t, local.ok(1, MethodSubmit, SubmitArgs{Text: "produce large output"}).Payload)
		stream := agent.stream(0)
		if stream == nil {
			t.Fatal("Submit opened no stream")
		}

		hugeText := strings.Repeat("x", DefaultMaxLine)
		stream <- session.Event{Kind: session.EventTextDelta, Text: "first"}
		stream <- session.Event{Kind: session.EventTextDelta, Text: hugeText}
		stream <- session.Event{Kind: session.EventTextDelta, Text: "third"}
		agent.finish(stream)

		// Guest sees seq 1 ("first"), seq 2 (notice replacement on the same
		// stream and seq), and seq 3 ("third", contiguous).
		guestEv1 := guest.await(func(f Frame) bool { return f.Kind == "event" && f.ID == ref.Stream && f.Seq == 1 })
		if got := decode[EventWire](t, guestEv1.Payload).Unwire(); got.Kind != session.EventTextDelta || got.Text != "first" {
			t.Fatalf("guest seq 1 = %+v", got)
		}
		guestEv2 := guest.await(func(f Frame) bool { return f.Kind == "event" && f.ID == ref.Stream && f.Seq == 2 })
		gotNotice := decode[EventWire](t, guestEv2.Payload).Unwire()
		if gotNotice.Kind != session.EventNotice || gotNotice.Text != "an update was too large to show here" {
			t.Fatalf("guest seq 2 = {Kind:%v Text:%q}, want notice %q", gotNotice.Kind, gotNotice.Text, "an update was too large to show here")
		}
		guestEv3 := guest.await(func(f Frame) bool { return f.Kind == "event" && f.ID == ref.Stream && f.Seq == 3 })
		if got := decode[EventWire](t, guestEv3.Payload).Unwire(); got.Kind != session.EventTextDelta || got.Text != "third" {
			t.Fatalf("guest seq 3 = %+v", got)
		}

		// Local surface sees the original huge event untouched at seq 2.
		localEv1 := local.await(func(f Frame) bool { return f.Kind == "event" && f.ID == ref.Stream && f.Seq == 1 })
		if got := decode[EventWire](t, localEv1.Payload).Unwire(); got.Text != "first" {
			t.Fatalf("local seq 1 = %+v", got)
		}
		localEv2 := local.await(func(f Frame) bool { return f.Kind == "event" && f.ID == ref.Stream && f.Seq == 2 })
		if got := decode[EventWire](t, localEv2.Payload).Unwire(); got.Kind != session.EventTextDelta || got.Text != hugeText {
			t.Fatalf("local seq 2 kind=%v len=%d, want textDelta len=%d", got.Kind, len(got.Text), len(hugeText))
		}
		localEv3 := local.await(func(f Frame) bool { return f.Kind == "event" && f.ID == ref.Stream && f.Seq == 3 })
		if got := decode[EventWire](t, localEv3.Payload).Unwire(); got.Text != "third" {
			t.Fatalf("local seq 3 = %+v", got)
		}
	})

	t.Run("bounded client reader enforces MaxLine while default reader accepts 1 MiB", func(t *testing.T) {
		runScriptedHost := func(t *testing.T, replySizes []int) net.Conn {
			t.Helper()
			ours, theirs := net.Pipe()
			t.Cleanup(func() {
				_ = ours.Close()
				_ = theirs.Close()
			})
			guard.Go("test/scripted-maxline-host", func() {
				scan := bufio.NewScanner(theirs)
				scan.Buffer(make([]byte, 0, 64*1024), frameCap)
				callIndex := 0
				for scan.Scan() {
					var frame Frame
					if err := json.Unmarshal(scan.Bytes(), &frame); err != nil {
						return
					}
					switch frame.Kind {
					case "hello":
						welcome := Welcome{
							Version:     Version,
							Workspace:   "/srv/app",
							SessionFile: "/srv/j.jsonl",
							Model:       "a/b",
						}
						line, _ := json.Marshal(Frame{Kind: "welcome", Payload: mustClientJSON(welcome)})
						_, _ = theirs.Write(append(line, '\n'))
					case "call":
						target := 64
						if callIndex < len(replySizes) {
							target = replySizes[callIndex]
						}
						callIndex++
						base, _ := plainFrameLineLen(Frame{
							Kind:    "result",
							ID:      frame.ID,
							Payload: mustClientJSON([]session.DisplayEntry{{Role: "assistant", Text: ""}}),
						})
						textLen := target - base
						if textLen < 0 {
							textLen = 0
						}
						entries := []session.DisplayEntry{{Role: "assistant", Text: strings.Repeat("z", textLen)}}
						reply := Frame{Kind: "result", ID: frame.ID, Payload: mustClientJSON(entries)}
						line, _ := json.Marshal(reply)
						_, _ = theirs.Write(append(line, '\n'))
					}
				}
			})
			return ours
		}

		// 1. Bounded client with DefaultMaxLine works at exact DefaultMaxLine
		// and on a follow-up call, then fails on DefaultMaxLine+1 with the
		// "too large to read" error.
		boundedConn := runScriptedHost(t, []int{DefaultMaxLine, 128, DefaultMaxLine + 1})
		boundedClient, err := Dial(boundedConn, "devbox", Hello{Workspace: "app"}, WithMaxLine(DefaultMaxLine))
		if err != nil {
			t.Fatalf("Dial bounded client: %v", err)
		}
		t.Cleanup(func() { _ = boundedClient.Close() })

		// First call: line of exactly DefaultMaxLine bytes succeeds.
		var entriesAtCap []session.DisplayEntry
		rawAtCap, err := boundedClient.call(nil, MethodTranscript, nil)
		if err != nil {
			t.Fatalf("bounded client at exact DefaultMaxLine failed: %v", err)
		}
		if err := json.Unmarshal(rawAtCap, &entriesAtCap); err != nil || len(entriesAtCap) != 1 {
			t.Fatalf("unmarshal entries at cap: %v", err)
		}
		// Second call: client keeps working after a line at the cap.
		if _, err := boundedClient.call(nil, MethodTranscript, nil); err != nil {
			t.Fatalf("bounded client subsequent call after cap line failed: %v", err)
		}
		// Third call: line of DefaultMaxLine+1 bytes ends the connection with
		// the "too large to read" error.
		_, err = boundedClient.call(nil, MethodTranscript, nil)
		if err == nil {
			t.Fatal("bounded client accepted a line of DefaultMaxLine+1 bytes")
		}
		if !strings.Contains(err.Error(), "too large to read") {
			t.Fatalf("bounded client error = %q, want text containing 'too large to read'", err.Error())
		}
		if clientErr := boundedClient.Err(); clientErr == nil || !strings.Contains(clientErr.Error(), "too large to read") {
			t.Fatalf("boundedClient.Err() = %v, want 'too large to read'", clientErr)
		}

		// 2. Unbounded client (no MaxLine) reads a 1 MiB line as before.
		const oneMiB = 1 << 20
		unboundedConn := runScriptedHost(t, []int{oneMiB})
		unboundedClient, err := Dial(unboundedConn, "devbox", Hello{Workspace: "app"})
		if err != nil {
			t.Fatalf("Dial unbounded client: %v", err)
		}
		t.Cleanup(func() { _ = unboundedClient.Close() })

		rawOneMiB, err := unboundedClient.call(nil, MethodTranscript, nil)
		if err != nil {
			t.Fatalf("unbounded client failed on 1 MiB line: %v", err)
		}
		var entriesOneMiB []session.DisplayEntry
		if err := json.Unmarshal(rawOneMiB, &entriesOneMiB); err != nil || len(entriesOneMiB) != 1 {
			t.Fatalf("unbounded client 1 MiB decode: %v", err)
		}
		if gotFrameLen, _ := plainFrameLineLen(Frame{Kind: "result", ID: 1, Payload: rawOneMiB}); gotFrameLen != oneMiB {
			t.Fatalf("unbounded client decoded frame of %d bytes, want %d", gotFrameLen, oneMiB)
		}

		// 3. A compressed frame whose wire line is tiny but inflates past MaxLine
		// is also rejected by a bounded client with the "too large to read" error.
		gzipOurs, gzipTheirs := net.Pipe()
		t.Cleanup(func() {
			_ = gzipOurs.Close()
			_ = gzipTheirs.Close()
		})
		guard.Go("test/scripted-gzip-bomb-host", func() {
			scan := bufio.NewScanner(gzipTheirs)
			scan.Buffer(make([]byte, 0, 64*1024), frameCap)
			for scan.Scan() {
				var frame Frame
				if err := json.Unmarshal(scan.Bytes(), &frame); err != nil {
					return
				}
				switch frame.Kind {
				case "hello":
					welcome := Welcome{
						Version:     Version,
						Encoding:    frameEncodingGzip,
						Workspace:   "/srv/app",
						SessionFile: "/srv/j.jsonl",
						Model:       "a/b",
					}
					line, _ := json.Marshal(Frame{Kind: "welcome", Payload: mustClientJSON(welcome)})
					_, _ = gzipTheirs.Write(append(line, '\n'))
				case "call":
					entries := []session.DisplayEntry{{Role: "assistant", Text: strings.Repeat("g", DefaultMaxLine+64)}}
					reply := Frame{Kind: "result", ID: frame.ID, Payload: mustClientJSON(entries)}
					_ = compressFrame(&reply, frameEncodingGzip)
					line, _ := json.Marshal(reply)
					_, _ = gzipTheirs.Write(append(line, '\n'))
				}
			}
		})
		gzipBounded, err := Dial(gzipOurs, "devbox", Hello{Workspace: "app"}, WithMaxLine(DefaultMaxLine))
		if err != nil {
			t.Fatalf("Dial gzip bounded client: %v", err)
		}
		t.Cleanup(func() { _ = gzipBounded.Close() })
		if _, err := gzipBounded.call(nil, MethodTranscript, nil); err == nil || !strings.Contains(err.Error(), "too large to read") {
			t.Fatalf("gzip bounded client on inflated oversized frame = %v, want 'too large to read'", err)
		}
	})
}
