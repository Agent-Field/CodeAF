package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
)

// dialClientOnSession connects a live Client to sess with the given Hello so
// tests can exercise the real wire framing and client decoder end to end.
func dialClientOnSession(t *testing.T, sess *Session, hello Hello) *Client {
	t.Helper()
	surface, engineConn := Pipe()
	guard.Go("test/transcript-page-server", func() {
		_ = ServeAttach(engineConn, engineConn, AttachOptions{
			Open: func(Hello) (*Session, error) { return sess, nil },
		})
		_ = engineConn.Close()
	})
	client, err := Dial(surface, "devbox", hello)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// TestATranscriptPageReturnsOlderEntriesFromAnIndexInPages verifies V4: a
// 250-entry transcript serves the 40-entry tail on Before<=0, walks backward
// in 40-entry pages by absolute index until More is false so the union equals
// the whole-transcript read, caps oversized limits at 200, and serves the
// remaining prefix when Before is smaller than Limit.
func TestATranscriptPageReturnsOlderEntriesFromAnIndexInPages(t *testing.T) {
	const total = 250
	entries := make([]session.DisplayEntry, total)
	for i := range entries {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		entries[i] = session.DisplayEntry{
			Role: role,
			Text: fmt.Sprintf("entry-%03d", i),
		}
	}

	fake := &fakeAgent{transcript: entries}
	sess := heldSession(fake)
	t.Cleanup(func() { _ = fake.Close() })

	client := dialClientOnSession(t, sess, Hello{Surface: "macbook"})
	agent := client.Agent()
	ctx := context.Background()

	// Tail call with Before: 0 and Limit: 0 uses the 40-entry default from the end.
	tail, err := agent.TranscriptPage(ctx, 0, 0)
	if err != nil {
		t.Fatalf("tail TranscriptPage: %v", err)
	}
	if tail.Base != 210 || tail.Total != 250 || !tail.More || len(tail.Entries) != 40 {
		t.Fatalf("tail page = {Base:%d Total:%d More:%v len:%d}, want {Base:210 Total:250 More:true len:40}",
			tail.Base, tail.Total, tail.More, len(tail.Entries))
	}
	for i, got := range tail.Entries {
		if want := entries[210+i]; got.Role != want.Role || got.Text != want.Text {
			t.Fatalf("tail.Entries[%d] = %+v, want %+v", i, got, want)
		}
	}

	// Next older page from Before: 210 with Limit: 40 returns entries 170..209.
	prev, err := agent.TranscriptPage(ctx, 210, 40)
	if err != nil {
		t.Fatalf("TranscriptPage(210, 40): %v", err)
	}
	if prev.Base != 170 || prev.Total != 250 || !prev.More || len(prev.Entries) != 40 {
		t.Fatalf("page(210, 40) = {Base:%d Total:%d More:%v len:%d}, want {Base:170 Total:250 More:true len:40}",
			prev.Base, prev.Total, prev.More, len(prev.Entries))
	}
	for i, got := range prev.Entries {
		if want := entries[170+i]; got.Role != want.Role || got.Text != want.Text {
			t.Fatalf("prev.Entries[%d] = %+v, want %+v", i, got, want)
		}
	}

	// Walking back from the tail until More is false visits every entry once in order
	// and matches the whole-transcript read.
	var collected []session.DisplayEntry
	before := 0
	for steps := 0; ; steps++ {
		if steps > 20 {
			t.Fatal("paging backward did not terminate")
		}
		page, err := agent.TranscriptPage(ctx, before, 40)
		if err != nil {
			t.Fatalf("walk step %d (before=%d): %v", steps, before, err)
		}
		collected = append(append([]session.DisplayEntry(nil), page.Entries...), collected...)
		if !page.More {
			if page.Base != 0 {
				t.Fatalf("final page has More=false but Base=%d", page.Base)
			}
			break
		}
		if page.Base <= 0 || (before > 0 && page.Base >= before) {
			t.Fatalf("page Base=%d did not move backward from before=%d", page.Base, before)
		}
		before = page.Base
	}
	whole := agent.Transcript()
	if len(collected) != len(whole) {
		t.Fatalf("collected %d entries across pages, whole transcript has %d", len(collected), len(whole))
	}
	for i := range whole {
		if collected[i].Role != whole[i].Role || collected[i].Text != whole[i].Text {
			t.Fatalf("entry %d mismatch: paged=%+v whole=%+v", i, collected[i], whole[i])
		}
	}

	// Limit: 1000 is capped at 200.
	capped, err := agent.TranscriptPage(ctx, 250, 1000)
	if err != nil {
		t.Fatalf("TranscriptPage(250, 1000): %v", err)
	}
	if len(capped.Entries) != 200 || capped.Base != 50 || capped.Total != 250 || !capped.More {
		t.Fatalf("capped page = {Base:%d Total:%d More:%v len:%d}, want {Base:50 Total:250 More:true len:200}",
			capped.Base, capped.Total, capped.More, len(capped.Entries))
	}

	// Before > Total is also treated as the tail.
	overBefore, err := agent.TranscriptPage(ctx, 999, 40)
	if err != nil {
		t.Fatalf("TranscriptPage(999, 40): %v", err)
	}
	if overBefore.Base != 210 || len(overBefore.Entries) != 40 || !overBefore.More {
		t.Fatalf("page(999, 40) = {Base:%d More:%v len:%d}, want tail {Base:210 More:true len:40}",
			overBefore.Base, overBefore.More, len(overBefore.Entries))
	}

	// Before: 5, Limit: 40 returns entries 0..4 with Base: 0 and More: false.
	head, err := agent.TranscriptPage(ctx, 5, 40)
	if err != nil {
		t.Fatalf("TranscriptPage(5, 40): %v", err)
	}
	if head.Base != 0 || head.Total != 250 || head.More || len(head.Entries) != 5 {
		t.Fatalf("head page = {Base:%d Total:%d More:%v len:%d}, want {Base:0 Total:250 More:false len:5}",
			head.Base, head.Total, head.More, len(head.Entries))
	}
	for i, got := range head.Entries {
		if want := entries[i]; got.Role != want.Role || got.Text != want.Text {
			t.Fatalf("head.Entries[%d] = %+v, want %+v", i, got, want)
		}
	}
}

// TestATranscriptPageNeverExceedsItsByteBound verifies V4: a page of 10 KiB
// entries is cut from the newest end to fit the 192 KiB default ceiling while
// subsequent calls deliver the older entries, and a single 1 MiB entry comes
// back truncated with the remote-control marker within the byte ceiling.
func TestATranscriptPageNeverExceedsItsByteBound(t *testing.T) {
	ctx := context.Background()

	t.Run("100 entries of 10 KiB each arrive in byte-bounded pages from the newest end", func(t *testing.T) {
		const count = 100
		const entryTextBytes = 10 << 10
		entries := make([]session.DisplayEntry, count)
		for i := range entries {
			prefix := fmt.Sprintf("entry-%03d:", i)
			entries[i] = session.DisplayEntry{
				Role: "assistant",
				Text: prefix + strings.Repeat("x", entryTextBytes-len(prefix)),
			}
		}

		fake := &fakeAgent{transcript: entries}
		sess := heldSession(fake)
		t.Cleanup(func() { _ = fake.Close() })

		client := dialClientOnSession(t, sess, Hello{
			Surface: "tablet",
			Session: sess.engine.SessionFile,
			Join:    true,
			Remote: &RemoteViewer{
				Device: "dev-tablet",
				Label:  "tablet",
				Watch:  true,
				Road:   "relay",
			},
		})
		agent := client.Agent()

		first, err := agent.TranscriptPage(ctx, 0, count)
		if err != nil {
			t.Fatalf("first TranscriptPage: %v", err)
		}
		if len(first.Entries) >= count || len(first.Entries) < 15 || len(first.Entries) > 20 {
			t.Fatalf("first page returned %d entries, want about 19 (fewer than %d)", len(first.Entries), count)
		}
		if first.Base != count-len(first.Entries) || first.Total != count || !first.More {
			t.Fatalf("first page = {Base:%d Total:%d More:%v}, want {Base:%d Total:%d More:true}",
				first.Base, first.Total, first.More, count-len(first.Entries), count)
		}
		rawFirst, err := json.Marshal(first)
		if err != nil {
			t.Fatal(err)
		}
		if len(rawFirst) > maxTranscriptPageBytes+256 {
			t.Fatalf("first page marshaled to %d bytes, exceeding 192 KiB ceiling", len(rawFirst))
		}

		// Walk the remaining older pages and confirm all 100 entries arrive in order.
		collected := append([]session.DisplayEntry(nil), first.Entries...)
		before := first.Base
		for steps := 0; before > 0; steps++ {
			if steps > 20 {
				t.Fatal("byte-bounded paging did not terminate")
			}
			page, err := agent.TranscriptPage(ctx, before, count)
			if err != nil {
				t.Fatalf("TranscriptPage(before=%d): %v", before, err)
			}
			rawPage, err := json.Marshal(page)
			if err != nil {
				t.Fatal(err)
			}
			if len(rawPage) > maxTranscriptPageBytes+256 {
				t.Fatalf("page(before=%d) marshaled to %d bytes, exceeding 192 KiB ceiling", before, len(rawPage))
			}
			if page.Base+len(page.Entries) != before {
				t.Fatalf("page Base=%d + len=%d != before=%d", page.Base, len(page.Entries), before)
			}
			collected = append(append([]session.DisplayEntry(nil), page.Entries...), collected...)
			before = page.Base
			if !page.More && before != 0 {
				t.Fatalf("page reported More=false at Base=%d", before)
			}
		}
		if len(collected) != count {
			t.Fatalf("collected %d entries across byte-bounded pages, want %d", len(collected), count)
		}
		for i := range entries {
			if collected[i].Text != entries[i].Text {
				t.Fatalf("entry %d text mismatch after byte-bounded walk", i)
			}
		}
	})

	t.Run("one entry of 1 MiB comes back truncated with the marker within 192 KiB", func(t *testing.T) {
		const oneMiB = 1 << 20
		originalText := strings.Repeat("m", oneMiB)
		fake := &fakeAgent{
			transcript: []session.DisplayEntry{
				{Role: "assistant", Text: originalText},
			},
		}
		sess := heldSession(fake)
		t.Cleanup(func() { _ = fake.Close() })

		client := dialClientOnSession(t, sess, Hello{
			Surface: "phone",
			Session: sess.engine.SessionFile,
			Join:    true,
			Remote: &RemoteViewer{
				Device: "dev-phone",
				Label:  "phone",
				Watch:  true,
				Road:   "relay",
			},
		})

		page, err := client.Agent().TranscriptPage(ctx, 0, 40)
		if err != nil {
			t.Fatalf("TranscriptPage on 1 MiB entry: %v", err)
		}
		if len(page.Entries) != 1 || page.Base != 0 || page.Total != 1 || page.More {
			t.Fatalf("page = {Base:%d Total:%d More:%v len:%d}, want single entry at Base 0",
				page.Base, page.Total, page.More, len(page.Entries))
		}
		gotText := page.Entries[0].Text
		if !strings.HasSuffix(gotText, transcriptShortenedMarker) {
			t.Fatalf("truncated text does not end with %q", transcriptShortenedMarker)
		}
		rawPage, err := json.Marshal(page)
		if err != nil {
			t.Fatal(err)
		}
		const smallAllowance = 256
		if len(rawPage) > maxTranscriptPageBytes+smallAllowance {
			t.Fatalf("marshaled page length = %d, want <= %d", len(rawPage), maxTranscriptPageBytes+smallAllowance)
		}
		// The read must not have mutated the underlying transcript entry on the host.
		if gotOrig := fake.Transcript()[0].Text; len(gotOrig) != oneMiB {
			t.Fatalf("host transcript entry length mutated to %d, want %d", len(gotOrig), oneMiB)
		}
	})
}

// TestATranscriptPageIsOpenToAGuestWhileWholeTranscriptIsRefused verifies V4:
// a remote guest connection can read the transcript through Transcript.Page
// while its whole-transcript MethodTranscript call is refused with the guest
// refusal sentence, and a local surface's whole-transcript read is unchanged.
func TestATranscriptPageIsOpenToAGuestWhileWholeTranscriptIsRefused(t *testing.T) {
	entries := []session.DisplayEntry{
		{Role: "user", Text: "first question"},
		{Role: "assistant", Text: "first answer"},
	}
	fake := &fakeAgent{transcript: entries}
	sess := heldSession(fake)
	t.Cleanup(func() { _ = fake.Close() })

	guest := dialSession(t, sess)
	welcome := guest.hello(Hello{
		Version: Version,
		Surface: "guest-laptop",
		Session: sess.engine.SessionFile,
		Join:    true,
		Remote: &RemoteViewer{
			Device: "dev-guest",
			Label:  "guest-laptop",
			Watch:  true,
			Drive:  true,
			Road:   "relay",
		},
	})
	if welcome.Kind != "welcome" {
		t.Fatalf("guest handshake answered %q: %s", welcome.Kind, welcome.Error)
	}

	pageFrame := guest.ok(1, MethodTranscriptPage, TranscriptPageArgs{Before: 0, Limit: 40})
	page := decode[Page](t, pageFrame.Payload)
	if len(page.Entries) != 2 || page.Base != 0 || page.Total != 2 || page.More {
		t.Fatalf("guest Transcript.Page = %+v", page)
	}

	wholeFrame := guest.call(2, MethodTranscript, nil)
	wantRefusal := chatlist.LiveNotAllowedHere(HostName())
	if wholeFrame.Error != wantRefusal {
		t.Fatalf("guest MethodTranscript error = %q, want %q", wholeFrame.Error, wantRefusal)
	}

	local := dialSession(t, sess)
	if frame := local.hello(Hello{Version: Version, Surface: "macbook"}); frame.Kind != "welcome" {
		t.Fatalf("local handshake answered %q: %s", frame.Kind, frame.Error)
	}
	localWhole := decode[[]session.DisplayEntry](t, local.ok(1, MethodTranscript, nil).Payload)
	if len(localWhole) != 2 || localWhole[0].Text != "first question" || localWhole[1].Text != "first answer" {
		t.Fatalf("local MethodTranscript = %+v, want %+v", localWhole, entries)
	}
}

// TestATranscriptPageAgainstAnOldHostReturnsNoSuchMethod verifies V4: calling
// TranscriptPage against an older engine that answers "no such method" returns
// that error unchanged so the caller can fall back.
func TestATranscriptPageAgainstAnOldHostReturnsNoSuchMethod(t *testing.T) {
	client, eng := newEngine(t)
	wantErr := fmt.Sprintf("engine: no such method %q", MethodTranscriptPage)
	eng.fails[MethodTranscriptPage] = wantErr

	_, err := client.Agent().TranscriptPage(context.Background(), 0, 40)
	if err == nil {
		t.Fatal("TranscriptPage against old host succeeded, want error")
	}
	if err.Error() != wantErr {
		t.Fatalf("TranscriptPage error = %q, want %q", err.Error(), wantErr)
	}
}
