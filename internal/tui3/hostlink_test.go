package tui3

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── 1. the note on the status line ──────────────────────────────────────────

// A HEALTHY LINK SAYS NOTHING BEFORE ITS FIRST MEASUREMENT. The emptiness law
// applied to a whole segment: no badge, no icon, no "connected" word and no
// guessed `0ms` while the first reply is still out.
func TestAWorkingLinkDrawsNothingBeforeItsFirstMeasurement(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.host = "devbox"
	a.link = LinkSeam{Note: func() string { return "" }}
	a.width = 200

	for _, part := range a.telemetry(a.width) {
		if part.kind == segLink {
			t.Fatalf("a working link drew %q", part.text)
		}
	}
	line := plain(a.status(a.width))
	for _, banned := range []string{"connected", "reconnect", "·  ·", "—"} {
		if strings.Contains(line, banned) {
			t.Fatalf("the status line says %q about a link with nothing to say:\n%s", banned, line)
		}
	}
	// AND A LOCAL SESSION HAS NO SEAM AT ALL, which must be the same nothing
	// rather than a panic on a nil function.
	local := newTestApp(&fakeAgent{model: "m"})
	if local.linkSegment() != "" || local.linkNoting() {
		t.Fatal("a local session found something to say about a connection it does not have")
	}
}

// A LOCAL SURFACE NEVER DRAWS A ROUND TRIP. Even an accidentally retained
// cached duration cannot turn into a host segment without a host, which keeps
// the zero-value seam and the emptiness law aligned.
func TestALocalSurfaceNeverDrawsTheRoundTripSegment(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.linkLatency = 3 * time.Millisecond
	if got := a.linkSegment(); got != "" {
		t.Fatalf("a local surface drew %q", got)
	}
	if got := plain(a.status(200)); strings.Contains(got, "3ms") {
		t.Fatalf("a local status line drew a hosted round trip:\n%s", got)
	}
}

// A HOSTED SURFACE WAITS FOR THE ANSWER. The host alone is not permission to
// guess `0ms`; the first reply creates the segment and the same cached fact is
// written as a sentence by /status.
func TestAHostedSurfaceDrawsLatencyOnlyAfterAReply(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.host = "spark"
	a.link = LinkSeam{Ping: func() (time.Duration, error) { return 3 * time.Millisecond, nil }}
	a.width = 200

	if got := plain(a.status(a.width)); strings.Contains(got, "ms") || strings.Contains(got, "spark ·") {
		t.Fatalf("the hosted status line guessed before a reply:\n%s", got)
	}
	a.linkPingBack(linkPingMsg{elapsed: 3 * time.Millisecond})
	if got := plain(a.status(a.width)); !strings.Contains(got, "spark · 3ms") {
		t.Fatalf("the hosted status line missed the answered round trip:\n%s", got)
	}
	if got := a.statusText(); !strings.Contains(got, "the round trip to spark is about 3ms") {
		t.Fatalf("/status does not say the measured fact in a sentence:\n%s", got)
	}
}

// THE ESTIMATE IS A HANDFUL, NOT THE LAST PACKET. With a four-sample EWMA, a
// new eight-millisecond answer moves a four-millisecond reading to five.
func TestTheRoundTripEstimateRollsOverAHandful(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.host = "spark"
	a.linkPingBack(linkPingMsg{elapsed: 4 * time.Millisecond})
	a.linkPingBack(linkPingMsg{elapsed: 8 * time.Millisecond})
	if got := latencyWord(a.linkLatency); got != "5ms" {
		t.Fatalf("rolling estimate = %q, want 5ms", got)
	}
}

// THE RECONNECTING SENTENCE WINS AND THE METER STAYS OFF THE WIRE. The old
// estimate is hidden, and a cadence landing during the gap schedules no ping.
func TestReconnectingWinsOverLatencyAndSkipsPing(t *testing.T) {
	const note = "reconnecting to spark — trying for up to 5 minutes"
	called := 0
	a := newTestApp(&fakeAgent{model: "m"})
	a.host = "spark"
	a.linkLatency = 3 * time.Millisecond
	a.link = LinkSeam{
		Note: func() string { return note },
		Ping: func() (time.Duration, error) {
			called++
			return time.Millisecond, nil
		},
	}
	if got := a.linkSegment(); got != note {
		t.Fatalf("link segment = %q, want reconnecting sentence", got)
	}
	if cmd := a.linkPingKick(); cmd != nil {
		t.Fatal("a reconnecting link prepared a ping")
	}
	if called != 0 {
		t.Fatalf("a reconnecting link made %d pings", called)
	}
}

// AND WHILE IT IS BEING REDIALLED THE SENTENCE IS ON THE LINE, verbatim. The
// words are internal/remote's, built there from the window the redialling
// actually uses, and this surface neither shortens nor rebuilds them.
func TestALinkBeingRedialledPutsItsSentenceOnTheStatusLine(t *testing.T) {
	const note = "reconnecting to devbox — trying for up to 5 minutes"
	a := newTestApp(&fakeAgent{model: "m"})
	a.host = "devbox"
	a.link = LinkSeam{Note: func() string { return note }}
	a.width = 200

	if got := plain(a.status(a.width)); !strings.Contains(got, note) {
		t.Fatalf("the status line does not say the link is being redialled:\n%s", got)
	}
	if !a.linkNoting() {
		t.Fatal("the paint clock was not told there is something moving")
	}
	// AND IT IS PAINTED ONE STEP UP FROM THE CLUSTER, because the age ramp has
	// nothing to say about a fact that is true for exactly as long as it is
	// drawn — and would have left it dim forever.
	painted := a.paintPart(hudPart{kind: segLink, text: note})
	if painted == a.pal.dim(note) {
		t.Fatal("the link segment was painted as furniture")
	}
	if painted != a.pal.accent(note) {
		t.Fatalf("the link segment is painted %q", painted)
	}
	// AND IT IS NEVER THE SEGMENT A NARROW FRAME GIVES UP. Everything droppable
	// is dropped around it, because it is the reason none of those numbers are
	// moving.
	ledger, alive := lineParts(a.telemetry(hudWide))
	for a.shrink(&ledger, &alive, len(dropOrder)) {
	}
	found := false
	for _, part := range alive {
		found = found || part.kind == segLink
	}
	if !found {
		t.Fatal("the link segment was dropped to make room for telemetry")
	}
}

// AND THE PHONE READS IT ON THE SHEET, which is where nine of the eleven facts
// on this line already live: fifty cells of sentence do not go on a forty-four
// column row.
func TestTheLinkSentenceReachesTheStatusSheetAndTheStatusCommand(t *testing.T) {
	const note = "reconnecting to devbox — trying for up to 5 minutes"
	a := newTestApp(&fakeAgent{model: "m"})
	a.host = "devbox"
	a.link = LinkSeam{Note: func() string { return note }}

	found := false
	for _, item := range a.deckItems() {
		if item.value == note {
			found = true
			if strings.TrimSpace(item.label) == "" {
				t.Fatal("the link's row hangs under no label")
			}
		}
	}
	if !found {
		t.Fatal("the sheet does not carry the link")
	}
	if got := a.statusText(); !strings.Contains(got, note) {
		t.Fatalf("/status does not say the link is being redialled:\n%s", got)
	}
}

// ── 2. the notice, once ─────────────────────────────────────────────────────

// IT DRAINS, AND ONE PLACE DRAINS IT. The seam forgets the sentence as it hands
// it over, so a surface that read it twice would not say it twice — it would
// lose it. This drives the loop the way the program does and counts.
func TestTheLinkNoticeIsShownOnceAndAskedForOnce(t *testing.T) {
	const said = "the connection came back, but devbox does not keep a turn running while nothing is attached"
	asked := 0
	a := newTestApp(&fakeAgent{model: "m"})
	a.host = "devbox"
	a.link = LinkSeam{Notice: func() string {
		asked++
		if asked == 1 {
			return said
		}
		return ""
	}}

	drive(t, a, key("a"), key("b"), key("c"))

	if asked < 3 {
		t.Fatalf("the notice was asked for %d times over three messages — it is read on every one", asked)
	}
	notes := 0
	for _, e := range a.entries {
		if e.kind == entryNote && e.text == said {
			notes++
		}
	}
	if notes != 1 {
		t.Fatalf("the sentence is in the conversation %d times, wanted exactly 1", notes)
	}
}

// AND A CONNECTION WITH NOTHING TO SAY WRITES NOTHING. A seam that answers the
// empty string must leave the transcript exactly as it was.
func TestAQuietConnectionWritesNoNote(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.link = LinkSeam{Notice: func() string { return "" }}
	drive(t, a, key("a"))
	for _, e := range a.entries {
		if e.kind == entryNote {
			t.Fatalf("a quiet connection wrote %q", e.text)
		}
	}
}

// ── 3. the questions that waited ────────────────────────────────────────────

// A HELD CARD IS THE CARD IT WOULD HAVE BEEN LIVE. The far machine kept the
// event; the surface replays it through the same door, so the offer, the keys
// and the id that answers it are the ones they always were.
func TestAHeldQuestionIsDrawnAsTheCardItWouldHaveBeen(t *testing.T) {
	agent, a := wired()
	a.host = "devbox"
	a.link = LinkSeam{Held: func() ([]HeldQuestion, error) {
		return []HeldQuestion{{
			Kind:  heldConsent,
			Event: consentEvent(7, "bash", "bash rm -rf build", `bash pattern "rm -rf *"`),
			Since: a.now().Add(-4 * time.Hour),
		}}, nil
	}}

	drive(t, a, runCmd(a.askHeld())...)

	if !a.asking() {
		t.Fatal("a question that waited four hours is not being asked")
	}
	got := plain(frame(a))
	for _, want := range []string{"rm -rf build", "allow? [y] yes", "[n] no"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the held card is missing %q:\n%s", want, got)
		}
	}
	// AND HOW LONG IT WAITED IS SAID, because that is the difference between a
	// question from four seconds ago and one from four hours ago.
	if !strings.Contains(got, "waiting 4 hours") {
		t.Fatalf("the card does not say how long it waited:\n%s", got)
	}
	// AND THE KEY THAT ANSWERS IT IS THE KEY THAT ALWAYS ANSWERED IT.
	drive(t, a, key("y"))
	if len(agent.answers) != 1 || agent.answers[0].id != 7 || !agent.answers[0].allow {
		t.Fatalf("answering the held card resolved %+v", agent.answers)
	}
}

// A QUESTION RAISED MOMENTS AGO SAYS NOTHING ABOUT HAVING WAITED, which is the
// emptiness law: "waiting 0 minutes" is a fact nobody asked for.
func TestAQuestionRaisedMomentsAgoSaysNothingAboutWaiting(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.host = "devbox"
	a.link = LinkSeam{Held: func() ([]HeldQuestion, error) {
		return []HeldQuestion{{
			Kind:  heldConsent,
			Event: consentEvent(3, "bash", "ls", ""),
			Since: a.now().Add(-2 * time.Second),
		}}, nil
	}}

	drive(t, a, runCmd(a.askHeld())...)

	for _, e := range a.entries {
		if e.kind == entryNote && strings.Contains(e.text, "waiting") {
			t.Fatalf("a question raised two seconds ago says %q", e.text)
		}
	}
	if !a.asking() {
		t.Fatal("the card itself was not drawn")
	}
}

// A KIND THIS BUILD DOES NOT DRAW IS SKIPPED AND LEFT WAITING. That is the
// wire's stated contract: a card a person can see and cannot answer is worse
// than one they were never shown, so there is no fallback rendering.
func TestAQuestionOfAnUnknownKindIsLeftWaiting(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.host = "devbox"
	a.link = LinkSeam{Held: func() ([]HeldQuestion, error) {
		return []HeldQuestion{
			{Kind: "something-a-newer-build-holds", Event: consentEvent(9, "bash", "ls", ""), Since: a.now().Add(-time.Hour)},
			{Kind: heldConsent, Event: consentEvent(4, "bash", "make test", ""), Since: a.now().Add(-time.Hour)},
		}, nil
	}}

	drive(t, a, runCmd(a.askHeld())...)

	if len(a.asks) != 1 || a.asks[0].id != 4 {
		t.Fatalf("the surface drew %d questions: %+v", len(a.asks), a.asks)
	}
	if got := plain(frame(a)); strings.Contains(got, "something-a-newer-build-holds") {
		t.Fatalf("the unknown kind was drawn as something:\n%s", got)
	}
}

// A FAR END THAT DID NOT ANSWER SAYS NOTHING. A link that is down is already
// saying so on the status line in its own words, and a second sentence about a
// failed question about questions is machinery talking.
func TestAWaitingRoomThatCouldNotBeReadIsSilent(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.host = "devbox"
	a.link = LinkSeam{Held: func() ([]HeldQuestion, error) { return nil, errors.New("the connection is gone") }}

	drive(t, a, runCmd(a.askHeld())...)

	for _, e := range a.entries {
		if e.kind == entryNote {
			t.Fatalf("a failed reading wrote %q", e.text)
		}
	}
	if a.asking() {
		t.Fatal("a failed reading invented a question")
	}
}

// AND A LOCAL SESSION NEVER ASKS AT ALL: no seam, no command, no waiting room.
func TestALocalSessionAsksNothingAboutAWaitingRoom(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	if a.askHeld() != nil {
		t.Fatal("a local session asked the machine it is on what it was holding")
	}
}

// ── the wait, in words ──────────────────────────────────────────────────────

func TestWaitedWordSaysASpanTheWayAPersonWouldSayIt(t *testing.T) {
	for _, row := range []struct {
		in   time.Duration
		want string
	}{
		{2 * time.Second, ""},
		{59 * time.Second, ""},
		{time.Minute, "1 minute"},
		{7 * time.Minute, "7 minutes"},
		{time.Hour + 20*time.Minute, "1 hour"},
		{4 * time.Hour, "4 hours"},
		{50 * time.Hour, "2 days"},
	} {
		if got := waitedWord(row.in); got != row.want {
			t.Errorf("%s reads %q, wanted %q", row.in, got, row.want)
		}
	}
}

// The compile-time promise the door depends on: a held question carries an
// unwrapped session event, because the card is drawn from the event itself.
var _ = HeldQuestion{Kind: heldStanding, Event: session.Event{}, Since: time.Time{}}
var _ = []string{heldConsent, heldStanding, heldHarness, heldConnect}
