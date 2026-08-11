package main

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/tui"
	"github.com/Agent-Field/aforge-v2/internal/tui2/chat"
)

// The switch is the only thing standing between the two surfaces, so it is
// tested for both directions: the old path must be unreachable by accident,
// and the new one must be unreachable without asking.
func TestWantChatV2(t *testing.T) {
	env := func(pairs map[string]string) func(string) string {
		return func(key string) string { return pairs[key] }
	}
	none := env(nil)
	set := env(map[string]string{chatV2Env: "1"})
	off := env(map[string]string{chatV2Env: "0"})

	cases := []struct {
		name string
		args []string
		get  func(string) string
		want bool
		rest []string
	}{
		{name: "plain chat stays on the old surface", args: []string{"--session", "new"}, get: none,
			want: false, rest: []string{"--session", "new"}},
		{name: "--v2 opens the new one", args: []string{"--v2"}, get: none, want: true, rest: []string{}},
		{name: "-v2 too", args: []string{"-v2", "--db", "x"}, get: none, want: true, rest: []string{"--db", "x"}},
		{name: "the environment opens it", args: nil, get: set, want: true, rest: []string{}},
		{name: "an explicit false closes it again", args: []string{"--v2=false"}, get: set,
			want: false, rest: []string{}},
		{name: "a false environment is not a request", args: nil, get: off, want: false, rest: []string{}},
		{name: "the flag outranks a quiet environment", args: []string{"--v2=1"}, get: off,
			want: true, rest: []string{}},
		{name: "a flag that merely starts with v2 is untouched", args: []string{"--v2x"}, get: none,
			want: false, rest: []string{"--v2x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, rest := wantChatV2(tc.args, tc.get)
			if got != tc.want {
				t.Fatalf("wantChatV2 = %v, want %v", got, tc.want)
			}
			if !reflect.DeepEqual(rest, tc.rest) {
				t.Fatalf("remaining args = %#v, want %#v", rest, tc.rest)
			}
		})
	}
}

func TestTruthyEnv(t *testing.T) {
	for _, value := range []string{"", "0", "false", "no", "off", " OFF "} {
		if truthyEnv(value) {
			t.Fatalf("%q should not open the surface", value)
		}
	}
	for _, value := range []string{"1", "true", "yes", "on", "anything"} {
		if !truthyEnv(value) {
			t.Fatalf("%q should open the surface", value)
		}
	}
}

// The bridge is the only place the two surfaces' vocabularies meet, so it is
// tested for the two things that would be invisible if they broke: a boundary
// that maps to the wrong phase, and a room key that fails to travel.
func TestStreamKindV2MapsEveryBoundary(t *testing.T) {
	cases := map[tui.StreamEventKind]chat.StreamKind{
		tui.StreamStarted:  chat.StreamStarted,
		tui.StreamDelta:    chat.StreamDelta,
		tui.StreamThinking: chat.StreamThinking,
		tui.StreamFinished: chat.StreamFinished,
		tui.StreamFailed:   chat.StreamFailed,
	}
	for from, want := range cases {
		if got := streamKindV2(from); got != want {
			t.Fatalf("streamKindV2(%v) = %v, want %v", from, got, want)
		}
	}
}

func TestBridgeStreamEventsCarriesTheRoomKey(t *testing.T) {
	source := make(chan tui.StreamEvent, 4)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	out := make(chan chat.StreamEvent, 4)
	bridgeStreamEventsInto(ctx, source, out)

	source <- tui.StreamEvent{Kind: tui.StreamDelta, Delta: "hi", Session: "room-7"}
	select {
	case event := <-out:
		if event.Kind != chat.StreamDelta || event.Delta != "hi" || event.Session != "room-7" {
			t.Fatalf("bridged event = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("the bridge delivered nothing")
	}

	// A closed engine feed ends its own forwarding without closing the window's
	// feed: the window outlives the engines that pass through it, and a bridge
	// that closed the surface's channel would tell the window there is no more
	// talking on the very cycle a promotion is telling it the opposite.
	close(source)
	select {
	case event, open := <-out:
		t.Fatalf("the window's feed was disturbed by an engine going away: %+v open=%v", event, open)
	case <-time.After(50 * time.Millisecond):
	}

	// A second engine takes over the same feed, which is what a promotion does.
	promoted := make(chan tui.StreamEvent, 4)
	bridgeStreamEventsInto(ctx, promoted, out)
	promoted <- tui.StreamEvent{Kind: tui.StreamStarted, Session: "room-7"}
	select {
	case event := <-out:
		if event.Kind != chat.StreamStarted {
			t.Fatalf("the promoted engine's feed did not reach the window: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("the promoted engine's feed never reached the window")
	}
}

func TestBridgeStreamEventsIsNilWithoutAFeed(t *testing.T) {
	// A window with no engine behind it forwards nothing, and asking for it
	// costs neither a goroutine nor a panic.
	bridgeStreamEventsInto(context.Background(), nil, make(chan chat.StreamEvent))
	if streamEventsOf(nil) != nil {
		t.Fatal("a window with no commander was given a feed")
	}
}
