package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/session"
)

// sealSurface is a surface whose seam is a real watch, so the tests below pin
// what a person sees for the outcomes the recorder reports.
func sealSurface(t *testing.T) (*app, *cellstore.SealWatch) {
	t.Helper()
	var watch cellstore.SealWatch
	a := newTestApp(&fakeAgent{model: "m"})
	a.seal = SealSeam{Failing: watch.Failing, Notice: watch.Take}
	a.width = 200
	return a, &watch
}

func sealNotes(a *app) []string {
	var said []string
	for _, e := range a.entries {
		if e.kind == entryNote && strings.Contains(e.text, "seal") {
			said = append(said, e.text)
		}
	}
	return said
}

// A SEAL THAT FAILED IS ON THE STATUS LINE FOR AS LONG AS IT IS TRUE, however
// many later seals fail the same way, and it is a sentence exactly once.
func TestAFailedSealStaysOnTheStatusLine(t *testing.T) {
	a, watch := sealSurface(t)
	if a.sealSegment() != "" {
		t.Fatalf("a healthy surface drew %q", a.sealSegment())
	}
	watch.Report(errString("disk full"))
	drive(t, a, key("a"))
	watch.Report(errString("disk full"))
	drive(t, a, key("b"))

	if got := plain(a.legend(a.width)); !strings.Contains(got, "not sealed") {
		t.Fatalf("the status line does not say sealing failed:\n%s", got)
	}
	if got := sealNotes(a); len(got) != 1 || !strings.Contains(got[0], "disk full") {
		t.Fatalf("notices %q, want the failure said once with its cause", got)
	}
}

// RECOVERY TAKES THE SEGMENT OFF AND SAYS SO.
func TestARecoveredSealClearsTheSegmentAndSaysSo(t *testing.T) {
	a, watch := sealSurface(t)
	watch.Report(errString("disk full"))
	drive(t, a, key("a"))
	watch.Report(nil)
	drive(t, a, key("b"))

	if got := plain(a.legend(a.width)); strings.Contains(got, "not sealed") {
		t.Fatalf("the segment outlived the failure:\n%s", got)
	}
	got := sealNotes(a)
	if len(got) != 2 || !strings.Contains(got[1], "works again") {
		t.Fatalf("notices %q, want the failure then the recovery", got)
	}
}

// A NEW CAUSE IS NEW NEWS while the segment stays up.
func TestAChangedSealCauseIsSaidAgain(t *testing.T) {
	a, watch := sealSurface(t)
	watch.Report(errString("disk full"))
	drive(t, a, key("a"))
	watch.Report(errString("engine not found"))
	drive(t, a, key("b"))

	got := sealNotes(a)
	if len(got) != 2 || !strings.Contains(got[1], "engine not found") {
		t.Fatalf("notices %q, want the second cause said", got)
	}
	if a.sealSegment() == "" {
		t.Fatal("the segment went away while sealing was still failing")
	}
}

// A SURFACE WITH NO SEAM DRAWS NOTHING and does not panic on the nil functions.
func TestASurfaceWithNoSealSeamDrawsNothing(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	drive(t, a, key("a"))
	if a.sealSegment() != "" {
		t.Fatal("a surface with no seam drew a segment")
	}
}

// TWO CELLS IN ONE PROCESS: one chat's failed seal is not another chat's. The
// window shows the seam of the conversation in front, so switching to the
// healthy one draws nothing and switching back brings the segment back.
func TestOneChatsFailedSealIsNotAnothers(t *testing.T) {
	var broken, healthy cellstore.SealWatch
	broken.Report(errString("disk full"))
	seamOf := func(w *cellstore.SealWatch) SealSeam { return SealSeam{Failing: w.Failing, Notice: w.Take} }

	a := newTestApp(&fakeAgent{model: "m"})
	a.width = 200
	a.takeUp(Conversation{Agent: &fakeAgent{model: "m"}, Seal: seamOf(&broken)}, true)
	drive(t, a, key("a"))
	if !strings.Contains(plain(a.legend(a.width)), "not sealed") {
		t.Fatal("the failing conversation does not show it")
	}

	a.takeUp(Conversation{Agent: &fakeAgent{model: "m"}, Seal: seamOf(&healthy)}, true)
	drive(t, a, key("b"))
	if got := plain(a.legend(a.width)); strings.Contains(got, "not sealed") {
		t.Fatalf("the healthy conversation shows the other one's failure:\n%s", got)
	}

	a.takeUp(Conversation{Agent: &fakeAgent{model: "m"}, Seal: seamOf(&broken)}, true)
	if !strings.Contains(plain(a.legend(a.width)), "not sealed") {
		t.Fatal("going back to the failing conversation lost the segment")
	}
	a.takeUp(Conversation{Agent: &fakeAgent{model: "m"}}, true)
	if a.sealSegment() != "" {
		t.Fatal("a conversation with no seam kept the last one's segment")
	}
}

// A sentence the seal watch sends on the turn's stream is addressed to the
// person: the feed keeps it as a told note, which the work group may not fold.
func TestToldNoticeIsATellingNoteAndAPlainOneIsNot(t *testing.T) {
	f := &feed{live: -1, think: -1, turn: 1}
	f.ingest(session.Event{Kind: session.EventNotice, Text: "zero.txt cannot be read here", Told: true})
	f.ingest(session.Event{Kind: session.EventNotice, Text: "trimmed the request and asked again"})

	if n := len(f.entries); n != 2 {
		t.Fatalf("two notices left %d entries", n)
	}
	if !f.entries[0].told || f.entries[1].told {
		t.Fatalf("told flags: %v then %v, want true then false", f.entries[0].told, f.entries[1].told)
	}
}
