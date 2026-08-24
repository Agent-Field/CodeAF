package tui3

import (
	"encoding/json"
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

// BenchmarkFrameStreamingPromoted is the frame [BenchmarkFrameStreaming] draws
// once the reply has been going long enough to have been promoted — which, at
// [markdownThrottle], is every reply older than a second and a half, and so
// nearly every reply anybody watches.
//
// It is a second benchmark rather than a change to that one because the two
// measure different halves of the same frame. Before a promotion the live block
// is plain text and the frame's cost is the wrap; after one it is a MARKDOWN
// RENDER of the settled prefix — sanitize, goldmark, chroma — and that render is
// what a delta arriving between two promotions was making the frame redo.
func BenchmarkFrameStreamingPromoted(b *testing.B) {
	for _, turns := range []int{4, 20} {
		b.Run(fmt.Sprintf("turns=%d", turns), func(b *testing.B) {
			a := benchApp(turns)
			a.state = stateWorking
			// A prefix with the structure a real reply has — headings, a list, a
			// fence — because the promoted half is rendered as markdown and flat
			// prose would measure none of what markdown costs.
			a.appendText(strings.Repeat("## a section of the reply\n\n"+
				"a paragraph about what was found, long enough to wrap at a hundred columns and then a little more.\n\n"+
				"- one finding\n- another finding\n\n"+
				"```go\nfunc answer() int { return 42 }\n```\n\n", 6))
			// Promoted the way the paint clock promotes it: the throttle's own
			// clock is wound back, so the cut is the one the next frame would
			// have taken anyway.
			a.mdAt = a.mdAt.Add(-2 * markdownThrottle)
			a.promoteMarkdown()
			if a.entries[a.live].mdCut == 0 {
				b.Fatal("the live block was not promoted, so this would measure the wrong frame")
			}
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

// BenchmarkFrameLiveTools is the frame drawn while a call is LIVE on screen —
// an edit whose diff is previewed under its row, over a transcript of finished
// calls that are laid out again on the same tick.
//
// Tool rows are the one block on this surface that is not cached per entry
// (render.go's [app.entryRows] says why), so this is the frame that redoes the
// JSON, the diff and the chroma of every call in the conversation thirty times
// a second for as long as the turn lasts.
func BenchmarkFrameLiveTools(b *testing.B) {
	for _, turns := range []int{4, 20} {
		b.Run(fmt.Sprintf("turns=%d", turns), func(b *testing.B) {
			a := benchApp(turns)
			a.state = stateWorking
			a.turn++
			args := benchEditArgs(40)
			a.event(announced("edit", "edit internal/tui3/render.go", args))
			a.event(beginWith("edit", "edit internal/tui3/render.go", args))
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

// benchEditArgs is an edit call's payload with a replacement big enough to cost
// what a real one costs: the diff between two n-line blocks is n² work before a
// single row is painted.
func benchEditArgs(lines int) string {
	var old, want strings.Builder
	for i := 0; i < lines; i++ {
		fmt.Fprintf(&old, "\tif got := answer(%d); got != want {\n", i)
		fmt.Fprintf(&want, "\tif got := answer(%d); got != wanted {\n", i)
	}
	payload, err := json.Marshal(map[string]any{
		"path":  "internal/tui3/render.go",
		"edits": []map[string]string{{"oldText": old.String(), "newText": want.String()}},
	})
	if err != nil {
		panic(err)
	}
	return string(payload)
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
