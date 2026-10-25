package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

func TestPhoneStreamPublishesTrailingTextAtMostTwicePerSecond(t *testing.T) {
	var stream appClientStream
	wakes := make(chan struct{}, 10)
	changed := func() { wakes <- struct{}{} }
	first := appwire.TurnDelta{Agent: "chat", Turn: 1, Msg: 0, Kind: appwire.DeltaText, Text: "p"}
	stream.delta(first, changed)
	defer stream.stop()
	<-wakes
	first.Text = "ong"
	stream.delta(first, changed)
	if got := stream.entry("chat"); got.Text != "p" {
		t.Fatalf("unthrottled text: %q", got.Text)
	}
	select {
	case <-wakes:
		t.Fatal("partial published within 500ms")
	case <-time.After(400 * time.Millisecond):
	}
	select {
	case <-wakes:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("trailing partial never published")
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
	select {
	case <-wakes:
		t.Fatal("a new message bypassed the per-chat throttle")
	case <-time.After(100 * time.Millisecond):
	}
}
