package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// INK SETTLES WHEN THE TURN ENDS (render.go's [app.assistantRows], styles.go's
// [hueLive]).
//
// While a reply is arriving its plain tail — the bytes since the last markdown
// promotion, which is the only part of the block that is still growing — is
// painted one lightness step above the body. The moment the turn settles the
// same words come back at the calm body tier, and the block that was lit is
// history without a single glyph changing.
//
// What is proved here is the TRANSITION and not the hex. The value itself is
// asserted once, in [TestTheLiveTierIsTheReadingLaddersStepAboveTheBody], where
// it can be read beside the body ink it is defined against; everything else
// asks the palette what it would paint, so a retune of either tier moves one
// line in styles.go and no line here.

// liveSGR is the 256-colour foreground the live tier writes, which is what the
// test app's profile ([newTestApp] pins ANSI256) actually emits.
func liveSGR() string { return "\x1b[38;5;" + itoa(int(hueLive.idx)) + "m" }

// settleLab is one assistant block mid-stream: a head the surface has already
// promoted to markdown, and a tail that is still arriving.
//
// The two are separated the way [promoteBlock] separates them — at the
// last newline — because that is the cut the renderer reads, and a test that
// invented its own cut would be testing a shape the surface never builds.
func settleLab(t *testing.T) (*app, int) {
	t.Helper()
	a := newTestApp(&fakeAgent{})
	a.width, a.height = 80, 40
	const head = "The parser is fixed.\n"
	a.entries = append(a.entries, entry{
		kind:  entryAssistant,
		text:  head + "It was reading the length prefix twice",
		mdCut: len(head),
	})
	at := len(a.entries) - 1
	a.live = at
	a.touch()
	return a, at
}

// A STREAMING REPLY LEADS, AND A SETTLED ONE DOES NOT. The same entry, the same
// width, the same bytes — only [entry.settled] moves between the two readings.
func TestAStreamingReplyLeadsAndASettledOneDoesNot(t *testing.T) {
	a, at := settleLab(t)
	e := &a.entries[at]

	rows := a.assistantRows(at, e, 60)
	var lit int
	for _, row := range rows {
		if strings.Contains(row, liveSGR()) {
			lit++
		}
	}
	if lit == 0 {
		t.Fatalf("the growing edge is not lit:\n%s", strings.Join(rows, "\n"))
	}
	// THE GROWING EDGE, AND ONLY IT. The promoted head carries prose's own
	// styling and is deliberately left alone (render.go says why), so exactly the
	// tail's rows may wear the live tier.
	tail := len(wrap(e.text[e.mdCut:], 60))
	if lit != tail {
		t.Fatalf("%d rows are lit, want the tail's %d:\n%s", lit, tail, strings.Join(rows, "\n"))
	}
	// And the words are still the words: lifting a row may not move a byte a
	// person reads.
	if !strings.Contains(plain(strings.Join(rows, "\n")), "length prefix twice") {
		t.Fatalf("the lit tail lost its text:\n%s", strings.Join(rows, "\n"))
	}

	e.settled = true
	settled := a.assistantRows(at, e, 60)
	for _, row := range settled {
		if strings.Contains(row, liveSGR()) {
			t.Fatalf("a settled answer is still lit:\n%s", strings.Join(settled, "\n"))
		}
	}
	if !strings.Contains(plain(strings.Join(settled, "\n")), "length prefix twice") {
		t.Fatalf("the settled answer lost its text:\n%s", strings.Join(settled, "\n"))
	}
}

// THE SETTLE DROPS THE ROWS THE STREAM LEFT BEHIND.
//
// [app.entryRows] hands back what it built last time unless the entry says it
// is stale, and a settled assistant block is not one of the shapes that bypass
// the cache. So the flag [app.closeLive] sets is the whole of the transition: a
// turn that ended without it would keep the bright rows on screen forever, and
// this is the test that would have caught it.
func TestTheSettleDropsTheRowsTheStreamLeftBehind(t *testing.T) {
	a, at := settleLab(t)

	d := a.conversation()
	cached := a.entryRows(d, at, 60)
	if !strings.Contains(strings.Join(cached, "\n"), liveSGR()) {
		t.Fatalf("the cached streaming rows are not lit:\n%s", strings.Join(cached, "\n"))
	}
	if !a.entries[at].built {
		t.Fatal("a streaming assistant block was never cached, so this test proves nothing")
	}

	a.closeLive()
	after := a.entryRows(a.conversation(), at, 60)
	if strings.Contains(strings.Join(after, "\n"), liveSGR()) {
		t.Fatalf("the cache served the stream's bright rows after the turn ended:\n%s",
			strings.Join(after, "\n"))
	}
	// The block did re-render rather than merely losing a colour: a settled reply
	// is one markdown document, and the two-line stream was two rows.
	if strings.Join(after, "\n") == strings.Join(cached, "\n") {
		t.Fatalf("the settled rows are byte-identical to the streaming ones:\n%s",
			strings.Join(after, "\n"))
	}
}

// THE LIVE TIER IS THE READING LADDER'S STEP ABOVE THE BODY, on both ladders,
// and this is the one place the values themselves are read.
//
// The assertion is `>=` and not `>` on purpose, and the reason is stated at
// [hueLive]: the hue is the ink's own pre-retune value, so until the body
// settles at its calmer hex the two are one colour and the effect is ABSENT
// rather than backwards. What may never happen is the live tier sinking BELOW
// the body — that would be a growing edge that receded while it grew.
func TestTheLiveTierIsTheReadingLaddersStepAboveTheBody(t *testing.T) {
	if hueLive.r != 0xD8 || hueLive.g != 0xDE || hueLive.b != 0xE9 {
		t.Fatalf("the live tier is #%02X%02X%02X, want #D8DEE9",
			hueLive.r, hueLive.g, hueLive.b)
	}
	if lightLive.r != 0x2E || lightLive.g != 0x34 || lightLive.b != 0x40 {
		t.Fatalf("the light live tier is #%02X%02X%02X, want #2E3440",
			lightLive.r, lightLive.g, lightLive.b)
	}
	// On a void the growing edge leads by being LIGHTER; on a page, by being
	// DARKER. Same step, mirrored, exactly as the whole light ladder is.
	if lightness(hueLive) < lightness(hueInk) {
		t.Fatalf("the live tier (L %.1f) sits under the body ink (L %.1f)",
			lightness(hueLive), lightness(hueInk))
	}
	if lightness(lightLive) > lightness(lightInk) {
		t.Fatalf("on a page the live tier (L %.1f) is lighter than the body (L %.1f)",
			lightness(lightLive), lightness(lightInk))
	}
	// A READING TIER ROUNDS ONTO THE GREY RAMP. Every step of this ladder means
	// loudness and nothing else, and a body that rounded into the colour cube
	// would be prose wearing a tint that looked like it meant something.
	for name, h := range map[string]hue{"live": hueLive, "light live": lightLive} {
		if h.idx < 232 {
			t.Fatalf("the %s tier resolves to xterm-256 %d, which is in the colour cube",
				name, h.idx)
		}
	}
	// The 256 neighbour check every entry in this table owes. [hueInk]'s own
	// index is the ONE this hue is allowed to meet, and only for as long as the
	// two are literally the same colour; every other role must round somewhere
	// else, or the fallback nobody looks at quietly becomes the effect nobody
	// gets.
	if lightLive.idx == lightInk.idx {
		t.Fatalf("on a page the live tier and the body ink both round to %d", lightLive.idx)
	}
	for name, h := range map[string]hue{
		"accent": hueAccent, "muted": hueMuted, "dim": hueDim, "add": hueAdd,
		"del": hueDel, "bad": hueBad, "ask": hueAsk, "warn": hueWarn,
		"data": hueData, "violet": hueViolet,
	} {
		if h.idx == hueLive.idx {
			t.Fatalf("the live tier and %s both resolve to xterm-256 %d", name, h.idx)
		}
	}
}

// lightness is HSL's L, in points, for a hue this table authored. It is here
// rather than in styles.go because nothing the surface DRAWS needs it — it is
// the measurement the palette's comments are written in, and the ladder laws
// are the only things that ask.
func lightness(h hue) float64 {
	hi := max(max(int(h.r), int(h.g)), int(h.b))
	lo := min(min(int(h.r), int(h.g)), int(h.b))
	return float64(hi+lo) / 2 / 255 * 100
}

// BELOW THE 256 RUNG THE EFFECT IS ABSENT, and absent is the whole answer.
//
// Every other hue in the table falls back to weight, and this one may not:
// WEIGHT BELONGS TO MARKDOWN, so a bolded live reply would be indistinguishable
// from one whose author opened in bold, and faint would say the opposite of
// what the tier means. With no hue to spend there is nothing honest to draw, so
// a sixteen-colour terminal and a NO_COLOR one both get the plain rows they got
// before this existed.
func TestBelowThe256RungAStreamingReplyIsUnpainted(t *testing.T) {
	for _, profile := range []tokens.Profile{tokens.ANSI16, tokens.NoColor} {
		a, at := settleLab(t)
		a.pal = newPalette(profile, false)
		e := &a.entries[at]
		for _, line := range wrap(e.text[e.mdCut:], 60) {
			if got := a.pal.live(line); got != line {
				t.Fatalf("at %v the live tier painted %q", profile, got)
			}
		}
		for _, row := range a.assistantRows(at, e, 60) {
			// The head is prose's, and prose answers the profile question for
			// itself; only the tail is this lane's to leave alone.
			if strings.Contains(plain(row), "length prefix") && row != plain(row) {
				t.Fatalf("at %v the streaming tail carries SGR: %q", profile, row)
			}
		}
	}
}

// ONE IDEOLOGY, EVERY CHAT SURFACE — A NODE'S ROOM SETTLES THE WAY THE
// CONVERSATION DOES.
//
// tui3 draws a conversation in more than one place: the transcript, a node's
// room, and a run's node journal inside one (room.go, roomorch.go). They are the
// same blocks drawn by the same renderers through [deck], which is what is
// SUPPOSED to make a law like this one hold everywhere for free — and "supposed
// to" is exactly the claim a test is for. A live tier that lit only the
// transcript would be a surface where the same event means two different things
// depending on which page a person happened to be standing on.
//
// The path is proved through [app.entryRows] rather than [app.assistantRows]
// directly, because the cache is half of what settling IS: [app.roomCloseLive]
// has to mark the block stale as well as settled, or the page keeps the rows it
// was drawn with mid-stream and the ink never dries.
func TestANodesRoomSettlesLikeTheConversation(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.width, a.height = 80, 40
	const head = "The parser is fixed.\n"
	a.room = &taskRoom{
		id: 7, title: "the node", live: 0, think: -1,
		unfolded: map[int]bool{}, workOpen: map[int]bool{},
		entries: []entry{{
			kind:  entryAssistant,
			text:  head + "It was reading the length prefix twice",
			mdCut: len(head),
		}},
	}

	rows := a.entryRows(a.room.deck(), 0, 60)
	if !strings.Contains(strings.Join(rows, "\n"), liveSGR()) {
		t.Fatalf("a node's growing edge is not lit, and the conversation's is:\n%s",
			strings.Join(rows, "\n"))
	}

	a.roomCloseLive()
	settled := a.entryRows(a.room.deck(), 0, 60)
	if strings.Contains(strings.Join(settled, "\n"), liveSGR()) {
		t.Fatalf("a node's finished answer kept the live tier — [app.roomCloseLive] "+
			"must mark the block stale as well as settled, or the page hands back "+
			"the rows it built mid-stream:\n%s", strings.Join(settled, "\n"))
	}
	if !strings.Contains(plain(strings.Join(settled, "\n")), "length prefix twice") {
		t.Fatalf("the settled page lost the answer:\n%s", strings.Join(settled, "\n"))
	}
}
