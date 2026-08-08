package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// jargonWords is the design filter written as something a test can run. These
// are the names this system uses for itself, and docs/JOURNEY.md's filter says
// a capability ships as behaviour and never as terminology: if explaining a
// feature needs a word we invented, the design is wrong. The surfaces below are
// the durable Go literals — the strings that never got the anti-jargon pass the
// model-composed ones did — and this is the pin that keeps them plain.
var jargonWords = regexp.MustCompile(`(?i)\b(worker|workers|charter|charters|rail|rails|leaf|leaves|graph|graphs|firing|firings|craft|crafts|splice|splices|spliced|node|nodes)\b`)

func assertPlain(t *testing.T, where string, surfaces ...string) {
	t.Helper()
	for _, surface := range surfaces {
		if found := jargonWords.FindString(surface); found != "" {
			t.Errorf("%s speaks the implementation's language (%q): %q", where, found, surface)
		}
	}
}

// TestTheRestingSurfacesSpeakPlainly covers what a person reads before they
// have typed anything, plus every idle tip and every self-page lane. The
// composer placeholder is the highest-traffic string in the product and used to
// read "Ask the graph…" — a word we invented, handed over in the first second.
func TestTheRestingSurfacesSpeakPlainly(t *testing.T) {
	assertPlain(t, "the composer placeholder", composerPlaceholder)
	assertPlain(t, "the empty thread", welcomeLines...)

	tips := make([]string, 0, len(curatedTips))
	for _, tip := range curatedTips {
		tips = append(tips, tip.text)
	}
	assertPlain(t, "an idle tip", tips...)

	lanes := make([]string, 0, len(selfSections)*3)
	for _, section := range selfSections {
		lanes = append(lanes, section.title, section.explain, section.empty)
	}
	assertPlain(t, "a self-page lane", lanes...)
}

// TestTheGuideSpeaksPlainly pins the surface a person reaches for when
// something is unclear — the worst possible moment to hand them a word out of
// the store schema.
func TestTheGuideSpeaksPlainly(t *testing.T) {
	rows := make([]string, 0, 64)
	for _, category := range helpCategories() {
		rows = append(rows, category.title)
		for _, row := range category.rows {
			// A slash command's own name is an identifier a person TYPES, not
			// prose the product speaks; /graph and /node are spellings, and
			// renaming them is a different change from speaking plainly. What
			// the guide SAYS about them is held to the filter either way.
			if !strings.HasPrefix(row.key, "/") {
				rows = append(rows, row.key)
			}
			rows = append(rows, row.meaning)
		}
	}
	assertPlain(t, "the guide", rows...)
}

// TestRefusalStatusLinesSpeakPlainly covers the lines a window that is not the
// resident shows instead of acting. They used to end in "no Commander" — a Go
// interface name, on screen, in answer to a person pressing a key.
func TestRefusalStatusLinesSpeakPlainly(t *testing.T) {
	model := New(&fakeBackend{}, "plain-status")
	model.setSize(110, 34)
	for name, act := range map[string]func(){
		"settings":    func() { _ = model.openSettings() },
		"new session": func() { _ = model.slashNew(nil) },
	} {
		model.status = ""
		act()
		if strings.TrimSpace(model.status) == "" {
			t.Fatalf("%s refused without saying anything", name)
		}
		assertPlain(t, "the "+name+" refusal", model.status)
	}
}

// TestTheWorkCardFooterSpeaksPlainly pins the one line under every job card.
// It used to say "compiling the job graph…", which is the product naming its
// own data structure at the exact moment a person is waiting on it.
func TestTheWorkCardFooterSpeaksPlainly(t *testing.T) {
	model := New(&fakeBackend{}, "plain-card")
	model.setSize(110, 34)
	card := jobCard{ID: "job", Title: "Draft the launch note", State: cardWorking}
	for _, expanded := range []bool{false, true} {
		frame := ansi.Strip(model.renderJobCard(card, 100, expanded, 0, false, false))
		for _, line := range strings.Split(frame, "\n") {
			if strings.Contains(line, "▸") || strings.Contains(line, "⟨×⟩") {
				assertPlain(t, "the work card footer", line)
			}
		}
	}
}
