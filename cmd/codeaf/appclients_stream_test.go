package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

func TestPhoneStreamPublishesTrailingTextAtMostFourTimesPerSecond(t *testing.T) {
	now := time.Unix(1, 0)
	var fire func()
	var wait time.Duration
	wakes := 0
	stream := appClientStream{
		now:   func() time.Time { return now },
		after: func(d time.Duration, fn func()) func() { wait = d; fire = fn; return func() { fire = nil } },
	}
	defer stream.stop()
	changed := func() { wakes++ }
	first := appwire.TurnDelta{Agent: "chat", Turn: 1, Msg: 0, Kind: appwire.DeltaText, Text: "p"}
	stream.delta(first, changed)
	if wakes != 1 {
		t.Fatalf("initial token publications = %d", wakes)
	}
	first.Text = "o"
	stream.delta(first, changed)
	if got := stream.entry("chat"); got.Text != "p" {
		t.Fatalf("unthrottled text: %q", got.Text)
	}
	if wakes != 1 || fire == nil || wait != 250*time.Millisecond {
		t.Fatalf("trailing timer: publications=%d wait=%s scheduled=%t", wakes, wait, fire != nil)
	}
	// More tokens inside the same interval must share the pending publication.
	now = now.Add(50 * time.Millisecond)
	first.Text = "ng"
	stream.delta(first, changed)
	if wakes != 1 || stream.entry("chat").Text != "p" {
		t.Fatal("a burst bypassed the throttle")
	}
	now = now.Add(200 * time.Millisecond)
	callback := fire
	fire = nil
	callback()
	if wakes != 2 {
		t.Fatalf("trailing publications = %d", wakes)
	}
	if got := stream.entry("chat"); got.Text != "pong" {
		t.Fatalf("trailing text = %q", got.Text)
	}
	stream.clear("chat")
	if got := stream.entry("chat"); got.Text != "" {
		t.Fatalf("completed partial remains: %+v", got)
	}
	first.Msg = 1
	first.Text = "again"
	stream.delta(first, changed)
	if wakes != 3 || stream.entry("chat").Text != "again" || fire != nil {
		t.Fatal("the first partial of a new message was delayed")
	}
	stream.delta(first, changed)
	stream.clear("chat")
	if fire != nil {
		t.Fatal("completed message retained a trailing timer")
	}
}

func TestPhoneStreamIgnoresAnOvertakenTimer(t *testing.T) {
	now := time.Unix(1, 0)
	var callback func()
	wakes := 0
	stream := appClientStream{now: func() time.Time { return now }, after: func(_ time.Duration, fn func()) func() { callback = fn; return func() {} }}
	defer stream.stop()
	d := appwire.TurnDelta{Agent: "chat", Turn: 1, Kind: appwire.DeltaText, Text: "p"}
	changed := func() { wakes++ }
	stream.delta(d, changed)
	d.Text = "o"
	stream.delta(d, changed)
	old := callback
	now = now.Add(250 * time.Millisecond)
	d.Text = "ng"
	stream.delta(d, changed)
	if wakes != 2 {
		t.Fatalf("publications=%d", wakes)
	}
	old()
	if wakes != 2 {
		t.Fatal("overtaken timer published again inside the throttle interval")
	}
}

func TestPhoneStreamMessageBoundaryPublishesImmediately(t *testing.T) {
	for _, boundary := range []string{"message", "turn"} {
		t.Run(boundary, func(t *testing.T) {
			now := time.Unix(1, 0)
			var callback func()
			wakes := 0
			stream := appClientStream{now: func() time.Time { return now }, after: func(_ time.Duration, fn func()) func() { callback = fn; return func() {} }}
			defer stream.stop()
			d := appwire.TurnDelta{Agent: "chat", Turn: 1, Kind: appwire.DeltaText, Text: "old"}
			changed := func() { wakes++ }
			stream.delta(d, changed)
			d.Text = " pending"
			stream.delta(d, changed)
			old := callback
			if boundary == "message" {
				d.Msg++
			} else {
				d.Turn++
			}
			d.Text = "new"
			stream.delta(d, changed)
			if got := stream.entry("chat"); got.Text != "new" || got.Turn != d.Turn || got.Msg == nil || *got.Msg != d.Msg || wakes != 2 {
				t.Fatalf("new %s first partial delayed: entry=%+v publications=%d", boundary, got, wakes)
			}
			old()
			if wakes != 2 {
				t.Fatal("previous message timer published after the boundary")
			}
		})
	}
}

func TestPhoneStreamPublishesImmediatelyAfterQuietGap(t *testing.T) {
	for _, gap := range []time.Duration{250 * time.Millisecond, time.Second} {
		t.Run(gap.String(), func(t *testing.T) {
			now := time.Time{}
			wakes, timers := 0, 0
			stream := appClientStream{now: func() time.Time { return now }, after: func(_ time.Duration, _ func()) func() { timers++; return func() {} }}
			defer stream.stop()
			d := appwire.TurnDelta{Agent: "chat", Turn: 1, Kind: appwire.DeltaText, Text: "p"}
			changed := func() { wakes++ }
			stream.delta(d, changed)
			if wakes != 1 || timers != 0 || stream.entry("chat").Text != "p" {
				t.Fatal("first token was delayed")
			}
			now = now.Add(gap)
			d.Text = "ong"
			stream.delta(d, changed)
			if wakes != 2 || timers != 0 || stream.entry("chat").Text != "pong" {
				t.Fatal("token after quiet gap was delayed")
			}
		})
	}
}

func TestPhoneStreamQuietGapStartsAtLastToken(t *testing.T) {
	now := time.Unix(1, 0)
	var callback func()
	wakes := 0
	stream := appClientStream{now: func() time.Time { return now }, after: func(_ time.Duration, fn func()) func() { callback = fn; return func() {} }}
	defer stream.stop()
	d := appwire.TurnDelta{Agent: "chat", Turn: 1, Kind: appwire.DeltaText, Text: "p"}
	changed := func() { wakes++ }
	stream.delta(d, changed)
	now = now.Add(100 * time.Millisecond)
	d.Text = "o"
	stream.delta(d, changed)
	now = now.Add(150 * time.Millisecond)
	callback()
	now = now.Add(100 * time.Millisecond)
	d.Text = "ng"
	stream.delta(d, changed)
	if wakes != 3 || stream.entry("chat").Text != "pong" {
		t.Fatal("trailing publication delayed the first token after a 250 ms quiet gap")
	}
}
