package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// stubCommander is the smallest thing the TUI will accept as a commander, plus
// the one capability under test.
type stubCommander struct {
	state     Residency
	successor Commander
	asked     int
}

func (s *stubCommander) Models() []string              { return nil }
func (s *stubCommander) Catalog() []ModelChoice        { return nil }
func (s *stubCommander) CurrentModel(string) string    { return "" }
func (s *stubCommander) SetModel(string, string) error { return nil }
func (s *stubCommander) Notebook(int) []store.Fact     { return nil }
func (s *stubCommander) NewSession() (string, error)   { return "", nil }
func (s *stubCommander) Cancel(string) error           { return nil }
func (s *stubCommander) NodeTrace(string, int) string  { return "" }
func (s *stubCommander) Residency() (Residency, Commander) {
	s.asked++
	successor := s.successor
	if successor != nil {
		// Handing a successor back and still calling yourself a visitor is not
		// a state the real surface can be in: the role moved.
		s.successor, s.state = nil, Residency{}
	}
	return s.state, successor
}

// A window that is not the one answering has to say so on screen. The stderr
// notice it used to rely on is written under an alt screen that never shows it,
// which is how a perfectly healthy second window became indistinguishable from
// an application that had died.
func TestVisitorHeaderSaysWhichWindowThisIs(t *testing.T) {
	commander := &stubCommander{state: Residency{Visitor: true, PID: 4711}}
	model := NewWithCommander(&fakeBackend{}, "visitor", commander)
	model.setSize(140, 30)
	model.applyPoll(pollResultMsg{residency: commander.state, residencyRead: true})

	header := ansi.Strip(model.renderTopBar())
	if !strings.Contains(header, "second window") || !strings.Contains(header, "pid 4711") {
		t.Fatalf("the header does not say this is a second window: %q", header)
	}
	// It survives every width. A narrow frame drops the pointer, never the fact.
	for _, width := range []int{120, 100, 80, 60, 40} {
		model.setSize(width, 30)
		if narrow := ansi.Strip(model.renderTopBar()); !strings.Contains(narrow, "second window") {
			t.Fatalf("width %d lost the visitor label: %q", width, narrow)
		}
	}
}

// The note takes the tail of the label while something is in motion, so the
// window says what is happening to the role without ever stopping saying what
// it is. A narrow frame keeps the fact and drops the tail.
func TestResidencyNoteTakesTheTailOfTheLabel(t *testing.T) {
	moving := Residency{Visitor: true, PID: 12, Note: "taking over as resident"}
	if got := moving.label(); got != "second window · taking over as resident" {
		t.Fatalf("label = %q", got)
	}
	if got := moving.shortLabel(); got != "second window" {
		t.Fatalf("short label = %q", got)
	}
	plain := Residency{Visitor: true, PID: 12}
	if got := plain.label(); got != "second window · resident is pid 12" {
		t.Fatalf("label = %q", got)
	}
	if got := (Residency{}).label(); got != "" {
		t.Fatalf("the only window said %q", got)
	}
}

// The ordinary single window is the zero value, and it renders as it always
// has: nothing about residency reaches the header at all.
func TestTheOnlyWindowSaysNothingAboutResidency(t *testing.T) {
	model := New(&fakeBackend{}, "solo")
	model.setSize(140, 30)
	if header := ansi.Strip(model.renderTopBar()); strings.Contains(header, "window") {
		t.Fatalf("a single window advertised its residency: %q", header)
	}
	if model.residencySource != nil {
		t.Fatal("a commander-less model claimed a residency source")
	}
}

// Promotion arrives through the poll: the surface is handed a new commander and
// adopts it whole, because a window that has just taken the resident role has
// capabilities the one it replaced never had.
func TestPollAdoptsTheCommanderHandedBackOnPromotion(t *testing.T) {
	promoted := &stubCommander{state: Residency{}}
	commander := &stubCommander{state: Residency{Visitor: true, PID: 88}, successor: promoted}
	model := NewWithCommander(&fakeBackend{}, "visitor", commander)
	model.setSize(140, 30)

	state, successor := model.residencySource.Residency()
	model.applyPoll(pollResultMsg{residency: state, residencyRead: true, adopt: successor})

	if model.commander != Commander(promoted) {
		t.Fatal("the window kept the commander it was told to replace")
	}
	if !model.streamRearm {
		t.Fatal("a promoted window did not re-arm its stream listener")
	}
	if model.residency.Visitor {
		t.Fatalf("residency after promotion = %+v", model.residency)
	}
	if header := ansi.Strip(model.renderTopBar()); strings.Contains(header, "second window") {
		t.Fatalf("a promoted window still calls itself second: %q", header)
	}
}

// A quiet cycle reads no rows at all, and it must still ask: which process is
// running the brain can change without a single event reaching the journal.
func TestQuietPollStillAsksWhoIsResident(t *testing.T) {
	commander := &stubCommander{state: Residency{Visitor: true, PID: 5}}
	model := NewWithCommander(&fakeBackend{}, "visitor", commander)
	model.setSize(140, 30)
	model.applyPoll(pollResultMsg{
		quiet: true, journalRead: true, journalSeq: 9,
		residency: Residency{Visitor: true, PID: 6}, residencyRead: true,
	})
	if model.residency.PID != 6 {
		t.Fatalf("a quiet poll dropped the residency reading: %+v", model.residency)
	}
}
