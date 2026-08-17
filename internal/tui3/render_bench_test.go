package tui3

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// The surface repaints at 30 Hz for as long as anything is alive on it — a
// running turn, an animating task, an open room — and every one of those ticks
// sets a.dirty, which throws the whole row list away and lays the transcript
// out again from the first entry. So the per-frame cost is not a detail of the
// draw: it is the steady-state CPU of the process, multiplied by thirty.
//
// These are the first benchmarks in this package. They drive the real
// ingestion path (a.event) to build the transcript, then time exactly what the
// paint clock times: a.paint() to invalidate, a.frame() to rebuild.

// benchTranscript feeds the app a session of the shape a working turn
// produces: a user line, an assistant reply, and a pair of tool calls whose
// output is the bulk of the bytes.
func benchTranscript(a *app, turns int) {
	for index := 0; index < turns; index++ {
		a.entries = append(a.entries, entry{kind: entryUser, text: fmt.Sprintf("do step %d please", index)})
		a.turn++
		a.event(text(session.EventTextDelta, strings.Repeat("a settled paragraph of reply. ", 12)+"\n\n"))
		a.event(toolBegin("read", "internal/tui3/render.go"))
		a.event(toolEnd("read", strings.Repeat("a line of file output\n", 60)))
		a.event(toolBegin("edit", "internal/tui3/view.go"))
		a.event(toolEnd("edit", strings.Repeat("- old line\n+ new line\n", 25)))
		a.settle()
	}
	a.touch()
}

func benchApp(turns int) *app {
	agent := &fakeAgent{model: "bench/model"}
	a := newApp(nil, Options{Agent: agent, Workspace: "/tmp/lab"})
	a.width, a.height = 100, 40
	a.tmux = false
	a.entries = nil
	a.welcome = welcome{spent: true}
	benchTranscript(a, turns)
	return a
}

// BenchmarkFrameIdle is the frame the paint clock draws when the conversation
// has not changed — a background task animating, a room open on a live node.
// Nothing on screen is different from the last frame, so ideally this costs
// almost nothing; what it actually costs is a full relayout.
func BenchmarkFrameIdle(b *testing.B) {
	for _, turns := range []int{4, 20, 60} {
		b.Run(fmt.Sprintf("turns=%d", turns), func(b *testing.B) {
			a := benchApp(turns)
			a.frame()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				a.dirty = true
				a.frame()
			}
		})
	}
}

// BenchmarkFrameStreaming is the frame drawn while a reply is arriving: one
// delta lands, then one frame. This is the per-frame cost that a turn pays
// thirty times a second for its whole length, and the one that decides whether
// a long reply gets more expensive the longer it gets.
// The live block is held at a fixed length rather than let grow: a reply that
// gets longer every iteration makes the per-op figure depend on b.N, which is
// exactly the property a before-and-after comparison cannot have. The growth
// itself is BenchmarkAppendText's subject.
func BenchmarkFrameStreaming(b *testing.B) {
	for _, turns := range []int{4, 20} {
		b.Run(fmt.Sprintf("turns=%d", turns), func(b *testing.B) {
			a := benchApp(turns)
			a.state = stateWorking
			a.appendText(strings.Repeat("a paragraph of the reply so far. ", 60))
			live := a.entries[a.live].text
			a.frame()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				a.entries[a.live].text = live
				a.appendText("more tokens arriving ")
				a.dirty = true
				a.frame()
			}
		})
	}
}

// BenchmarkLayout isolates the transcript relayout from the chrome, which is
// where a regression would otherwise hide: chrome is a constant per frame and
// layout is the part that scales with the conversation.
func BenchmarkLayout(b *testing.B) {
	for _, turns := range []int{4, 20, 60} {
		b.Run(fmt.Sprintf("turns=%d", turns), func(b *testing.B) {
			a := benchApp(turns)
			a.visible(a.bodyWidth())
			width := a.bodyWidth()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				a.dirty = true
				a.visible(width)
			}
		})
	}
}

// BenchmarkAppendText is the accumulator a streaming reply grows through, once
// per delta. A provider that sends a token per event calls this thousands of
// times in a turn, so a copy here is a copy of the whole reply so far.
func BenchmarkAppendText(b *testing.B) {
	for _, deltas := range []int{500, 4000} {
		b.Run(fmt.Sprintf("deltas=%d", deltas), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				a := benchApp(1)
				a.state = stateWorking
				b.StartTimer()
				for d := 0; d < deltas; d++ {
					a.appendText("token ")
				}
			}
		})
	}
}
