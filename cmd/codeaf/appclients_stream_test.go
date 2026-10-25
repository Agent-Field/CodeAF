package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

func TestPhoneStreamPublishesTrailingTextAtMostTwicePerSecond(t *testing.T) {
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
	first.Text = "ong"
	stream.delta(first, changed)
	if got := stream.entry("chat"); got.Text != "p" {
		t.Fatalf("unthrottled text: %q", got.Text)
	}
	if wakes != 1 || fire == nil || wait != 500*time.Millisecond {
		t.Fatalf("trailing timer: publications=%d wait=%s scheduled=%t", wakes, wait, fire != nil)
	}
	now = now.Add(wait)
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
	if wakes != 2 || fire == nil || wait != 500*time.Millisecond {
		t.Fatal("a new message bypassed the per-chat throttle")
	}
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
	now = now.Add(500 * time.Millisecond)
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
