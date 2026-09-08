package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The thinking WINDOW and the person's HUE — the two things that make a glance
// at this surface answer "who is speaking, and where is the model now".

// ── the reading window ──────────────────────────────────────────────────────

// reasoningLines streams one reasoning delta per line into a running turn and
// returns the surface, still streaming (nothing non-reasoning has arrived, so
// the block has not collapsed).
func reasoningLines(t *testing.T, lines ...string) *app {
	t.Helper()
	events := make([]session.Event, 0, len(lines))
	for _, line := range lines {
		events = append(events, text(session.EventReasoning, line+"\n"))
	}
	_, a := wired(events)
	typeLine(t, a, "think about it")
	return a
}

// thoughtBlockRows is the streaming block's rows: its header, then its window.
func thoughtBlockRows(t *testing.T, a *app) []row {
	t.Helper()
	at := thoughtAt(t, a)
	var out []row
	for _, r := range rows(a) {
		if r.entry == at {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		t.Fatal("the thinking block drew nothing")
	}
	return out
}

// THE WINDOW. However long the think runs, three lines of it are on screen —
// the last three, newest at the bottom, under one header row.
func TestTheStreamingThoughtShowsOnlyItsLastThreeLines(t *testing.T) {
	a := reasoningLines(t, "one", "two", "three", "four", "five")
	showLiveWork(t, a)

	block := thoughtBlockRows(t, a)
	if len(block) != 1+thoughtLive {
		t.Fatalf("want a header and %d lines, got %d rows:\n%s",
			thoughtLive, len(block), strings.Join(plainRows(a), "\n"))
	}
	if head := strings.TrimLeft(plain(block[0].text), " "); !strings.HasPrefix(head, glyphThought+" thinking · ") {
		t.Fatalf("the header is wrong: %q", head)
	}
	body := make([]string, 0, thoughtLive)
	for _, r := range block[1:] {
		body = append(body, strings.TrimSpace(plain(r.text)))
	}
	if want := []string{"three", "four", "five"}; strings.Join(body, ",") != strings.Join(want, ",") {
		t.Fatalf("the window is %v, want the last three lines %v", body, want)
	}
	for _, gone := range []string{"one", "two"} {
		for _, line := range body {
			if line == gone {
				t.Fatalf("%q is still in the window: %v", gone, body)
			}
		}
	}

	// The window is a live cue only. Once the block settles, its own disclosure
	// opens the whole thought; nothing was thrown away to draw three lines of it.
	drive(t, a, streamEventMsg{gen: a.gen, ev: text(session.EventTextDelta, "done")})
	if !a.toggleLatestThought() {
		t.Fatal("the thought has no disclosure")
	}
	drawn := strings.Join(plainRows(a), "\n")
	for _, want := range []string{"one", "two", "three", "four", "five"} {
		if !strings.Contains(drawn, want) {
			t.Fatalf("the expansion lost %q:\n%s", want, drawn)
		}
	}
}

// A think shorter than the window opens at the NEWEST stop rather than starting
// faint: the gradient says where you are, not how much there is.
func TestAShortThoughtTakesTheNewestStopFirst(t *testing.T) {
	a := reasoningLines(t, "just the one")
	showLiveWork(t, a)
	a.pal = newPalette(tokens.TrueColor, false)
	a.markStale(thoughtAt(t, a))
	a.touch()

	block := thoughtBlockRows(t, a)
	if len(block) != 2 {
		t.Fatalf("want a header and one line, got %d rows", len(block))
	}
	if !strings.Contains(block[1].text, trueColorSGR(thoughtFade[len(thoughtFade)-1])) {
		t.Fatalf("the only line is not at the newest stop: %q", block[1].text)
	}
}

// trueColorSGR is one hue's truecolor foreground sequence — what a test looks
// for when the colour IS the subject.
func trueColorSGR(h hue) string {
	return "\x1b[38;2;" + itoa(int(h.r)) + ";" + itoa(int(h.g)) + ";" + itoa(int(h.b)) + "m"
}

// THE RAMP, as authored. The stops are derived from the dim ink (styles.go), so
// this is the test that keeps the table in that file's comment honest.
func TestTheFadeStopsAreTheAuthoredHexes(t *testing.T) {
	want := []string{"#25282D", "#40444D", "#5B616D"}
	for i, hex := range want {
		r, g, b, ok := parseHex(hex)
		if !ok {
			t.Fatalf("test hex %q is malformed", hex)
		}
		got := thoughtFade[i]
		if got.r != r || got.g != g || got.b != b {
			t.Fatalf("stop %d is #%02X%02X%02X, want %s", i, got.r, got.g, got.b, hex)
		}
	}
	// The three have to be three: a ramp whose stops collapse onto one index is
	// a gradient nobody can see, and the 256-colour rung is where that happens.
	if a, b, c := thoughtFade[0].idx, thoughtFade[1].idx, thoughtFade[2].idx; a == b || b == c || a == c {
		t.Fatalf("the stops collapse on the 256-colour rung: %d %d %d", a, b, c)
	}
}

// THREE STOPS ON A TRUECOLOR TERMINAL, and none at all where there is no
// colour — where the window is three plain lines, newest last, which is the
// fact the gradient was drawing.
func TestTheWindowFadesInTruecolorAndNotUnderNoColor(t *testing.T) {
	for _, tc := range []struct {
		name    string
		profile tokens.Profile
		faded   bool
	}{
		{"truecolor", tokens.TrueColor, true},
		{"no colour", tokens.NoColor, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := reasoningLines(t, "one", "two", "three", "four")
			showLiveWork(t, a)
			a.pal = newPalette(tc.profile, false)
			a.markStale(thoughtAt(t, a))
			a.touch()

			block := thoughtBlockRows(t, a)
			body := block[1:]
			if len(body) != thoughtLive {
				t.Fatalf("want %d lines, got %d", thoughtLive, len(body))
			}
			for i, r := range body {
				got := strings.Contains(r.text, trueColorSGR(thoughtFade[i]))
				if got != tc.faded {
					t.Fatalf("line %d fade = %v, want %v: %q", i, got, tc.faded, r.text)
				}
			}
			if !tc.faded {
				for _, r := range block {
					if strings.Contains(r.text, "\x1b") {
						t.Fatalf("NO_COLOR drew an escape sequence: %q", r.text)
					}
				}
			}
			// Whatever the terminal can say, the newest line is last.
			if last := strings.TrimSpace(plain(body[len(body)-1].text)); last != "four" {
				t.Fatalf("the newest line is not at the bottom: %q", last)
			}
		})
	}
}

// THE COUNTER accumulates while the think streams and lands on the collapsed
// row, which is the whole reason it is kept: how much a model wrote is a fact
// about a block that is no longer showing its words.
func TestTheThoughtTokenCounterAccumulatesAndSurvivesTheCollapse(t *testing.T) {
	_, a := wired([]session.Event{
		text(session.EventReasoning, strings.Repeat("a", 40)),
		text(session.EventReasoning, strings.Repeat("b", 40)),
	})
	typeLine(t, a, "count it")
	showLiveWork(t, a)

	at := thoughtAt(t, a)
	if got := thoughtCount(&a.entries[at]); got != "20 tok" {
		// 80 bytes at the four-bytes-a-token estimate (thinking.go).
		t.Fatalf("the counter reads %q, want %q", got, "20 tok")
	}
	head := plain(thoughtBlockRows(t, a)[0].text)
	if strings.TrimLeft(head, " ") != glyphThought+" thinking · 20 tok · ctrl+e" {
		t.Fatalf("the live header reads %q", head)
	}

	// One more delta, and the header has moved. A reasoning delta is part of the
	// flood, so it reaches the screen on the frame clock and not on arrival
	// (app.go's paint) — which is why the tick is delivered by hand here.
	drive(t, a,
		streamEventMsg{gen: a.gen, ev: text(session.EventReasoning, strings.Repeat("c", 40))},
		frameMsg{})
	if head := plain(thoughtBlockRows(t, a)[0].text); strings.TrimLeft(head, " ") != glyphThought+" thinking · 30 tok · ctrl+e" {
		t.Fatalf("the counter did not accumulate: %q", head)
	}

	// The first non-reasoning word collapses it, and the figure comes with it.
	drive(t, a, streamEventMsg{gen: a.gen, ev: text(session.EventTextDelta, "yes")})
	got := plain(frame(a))
	if !strings.Contains(got, "· 30 tok · ctrl+e") {
		t.Fatalf("the collapsed row lost the count:\n%s", got)
	}
	if !strings.Contains(got, glyphThought+" thought for ") {
		t.Fatalf("the collapsed row lost its sentence:\n%s", got)
	}
}

// ── the person's hue ────────────────────────────────────────────────────────

// accentSGR is the accent's escape sequence on this surface's palette.
func accentSGR(a *app) string {
	painted := a.pal.accent("x")
	return painted[:strings.Index(painted, "x")]
}

// IDENTITY OWNS HUE, MARKDOWN OWNS WEIGHT. A reply full of bold can never again
// read as the person's own message, because the person's message is the only
// thing on screen painted in the accent.
func TestTheUserSpeaksInTheAccentAndNeverInBold(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		text(session.EventTextDelta, "the config is read at boot"),
		{Kind: session.EventTurnDone},
	}}}
	a := newTestApp(agent)
	runTurn(t, a, agent, "where is the config read?")

	accent := accentSGR(a)
	var sawUser bool
	for _, r := range rows(a) {
		if r.entry < 0 {
			continue
		}
		switch a.entries[r.entry].kind {
		case entryUser:
			sawUser = true
			if !strings.Contains(r.text, accent) {
				t.Fatalf("a user row is not in the accent: %q", r.text)
			}
			if strings.Contains(r.text, "\x1b[1m") {
				t.Fatalf("a user row is bold: %q", r.text)
			}
		case entryAssistant:
			if strings.Contains(r.text, accent) {
				t.Fatalf("an assistant row wears the person's hue: %q", r.text)
			}
		}
	}
	if !sawUser {
		t.Fatal("no user row was drawn")
	}
}

// THE OTHER HALF OF THE FIX: the model's bold is still bold. Hue was taken off
// the person's message precisely so that weight could stay markdown's, and a
// reply that lost its emphasis in the trade would have paid for the distinction
// with the thing the distinction was protecting.
//
// It is asserted against a STATED styler rather than through the surface,
// because markdown's profile is detected from the environment once per process
// (markdown.go) and a test that read it would be a test of the machine.
func TestTheModelsBoldSurvivesTheUserHue(t *testing.T) {
	rows := renderMarkdownWith(mdStyler(tokens.TrueColor), "**the config** is read at boot\n", 60)
	if len(rows) == 0 {
		t.Fatal("the reply rendered to nothing")
	}
	row := rows[0]
	if !strings.Contains(row, "\x1b[1m") {
		t.Fatalf("the reply's **bold** did not render bold: %q", row)
	}
	// And it is not the person's hue that carries it.
	if strings.Contains(row, trueColorSGR(hueAccent)) {
		t.Fatalf("a rendered reply wears the person's accent: %q", row)
	}
}

// A REPLAYED CONVERSATION IS THE SAME CONVERSATION. A resumed session's user
// rows are drawn by the same renderer, so they carry the same hue — a replay
// that painted the person differently would be a screen that changed its mind
// about who said what when it was reopened.
func TestReplayedUserRowsMatchLiveOnes(t *testing.T) {
	line := "where is the config read?"
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		text(session.EventTextDelta, "at boot"),
		{Kind: session.EventTurnDone},
	}}}
	live := newTestApp(agent)
	runTurn(t, live, agent, line)

	agent.past = []session.DisplayEntry{
		{Role: "user", Text: line},
		{Role: "assistant", Text: "at boot"},
	}
	replayed := newTestApp(agent)
	replayed.entries = nil
	replayed.replay()
	replayed.touch()

	pick := func(a *app) []string {
		var out []string
		for _, r := range rows(a) {
			if r.entry >= 0 && a.entries[r.entry].kind == entryUser {
				out = append(out, r.text)
			}
		}
		return out
	}
	liveRows, replayRows := pick(live), pick(replayed)
	if len(liveRows) == 0 {
		t.Fatal("the live surface drew no user rows")
	}
	if strings.Join(liveRows, "\n") != strings.Join(replayRows, "\n") {
		t.Fatalf("replay draws the person differently:\nlive:   %q\nreplay: %q", liveRows, replayRows)
	}
	for _, r := range replayRows {
		if !strings.Contains(r, accentSGR(replayed)) {
			t.Fatalf("a replayed user row is not in the accent: %q", r)
		}
		if strings.Contains(r, "\x1b[1m") {
			t.Fatalf("a replayed user row is bold: %q", r)
		}
	}
}
