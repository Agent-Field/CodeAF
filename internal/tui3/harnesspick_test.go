package tui3

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

// The /harness picker, the chip it writes into the tray, and the run that chip
// starts.
//
// Everything here drives the real surface against a real registry in a
// temporary directory, on harnesspanel_test.go's terms: the list's whole job is
// to say what is on disk in the order it last happened, and a test that stubbed
// the store would be a test of the stub.

// pickAgent is a scripted session that can also run a harness by name — the
// optional half of the seam ([harnessRunner]), which the ordinary fake has
// never heard of.
type pickAgent struct {
	*fakeAgent
	// asked is what RunHarnessRequest was handed, and runs how many times.
	name  string
	text  string
	model string
	runs  int
	// refuse is what it answers with instead of a stream.
	refuse error
}

func (p *pickAgent) RunHarnessRequest(_ context.Context, name, text, model string) (<-chan session.Event, error) {
	p.runs++
	p.name, p.text, p.model = name, text, model
	if p.refuse != nil {
		return nil, p.refuse
	}
	out := make(chan session.Event, 4)
	out <- session.Event{Kind: session.EventHarnessRun, Text: name}
	out <- session.Event{Kind: session.EventTextDelta, Text: "the report"}
	out <- session.Event{Kind: session.EventTurnDone}
	close(out)
	return out, nil
}

// pickApp is a surface with a registry behind /harness and an agent that can
// run one.
func pickApp(t *testing.T, saved ...subharness.Harness) (*app, *pickAgent, *subharness.Store) {
	t.Helper()
	store := subharness.At(t.TempDir())
	for _, one := range saved {
		if _, err := store.Save(one); err != nil {
			t.Fatalf("seed %s: %v", one.Id.Name, err)
		}
	}
	agent := &pickAgent{fakeAgent: &fakeAgent{}}
	a := newTestApp(agent)
	a.harn = store
	a.width = 100
	return a, agent, store
}

// ranAt records one run of a harness at a moment, which is what the list sorts
// by.
func ranAt(t *testing.T, store *subharness.Store, name string, when time.Time) {
	t.Helper()
	if _, err := store.SaveRun(subharness.Trace{
		Id:      subharness.Id{Name: name, Version: 1},
		Started: when,
		Status:  subharness.StatusOK,
	}); err != nil {
		t.Fatalf("seed a run of %s: %v", name, err)
	}
}

// ── the list ────────────────────────────────────────────────────────────────

// THE SPACE IS THE DOOR, and what opens is the LATEST harnesses: the list is
// recency order before anybody types a query into it.
func TestTheHarnessPickerOpensLatestFirst(t *testing.T) {
	a, _, store := pickApp(t,
		demoHarness("review-diff", "read a diff"),
		demoHarness("triage-flake", "chase a flaky test"))
	ranAt(t, store, "review-diff", time.Now().Add(-48*time.Hour))
	ranAt(t, store, "triage-flake", time.Now().Add(-2*time.Hour))

	typeInto(t, a, "/harness ")
	if !a.harnPick.open {
		t.Fatal("/harness with a space after it opened nothing")
	}
	if a.menu.open {
		t.Fatal("the command list stayed up under the picker")
	}
	if got := a.harnPick.rows[0].name; got != "triage-flake" {
		t.Fatalf("the list opens on %q rather than on the latest harness", got)
	}
	screen := strings.Join(plainOverlay(a), "\n")
	for _, want := range []string{"triage-flake", "chase a flaky test", "2h · finished", "review-diff"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the list does not say %q:\n%s", want, screen)
		}
	}
	// AND THE LAST ROW IS ALWAYS THE WAY TO THE CARDS.
	if !strings.Contains(screen, harnessBrowseWord) {
		t.Fatalf("the list has no door onto the registry:\n%s", screen)
	}
}

// A HARNESS NOBODY HAS RUN SAYS NOTHING THERE, which is the emptiness law and
// what `/subharness` has always drawn: the row is its name and what it is for.
// `never run` under every fresh row was the screen counting its own silences.
func TestTheHarnessPickerSaysNothingAboutAHarnessNobodyHasRun(t *testing.T) {
	a, _, _ := pickApp(t, demoHarness("review-diff", "read a diff"))
	typeInto(t, a, "/harness ")
	screen := strings.Join(plainOverlay(a), "\n")
	if !strings.Contains(screen, "read a diff") {
		t.Fatalf("the row lost what the harness is for:\n%s", screen)
	}
	if strings.Contains(screen, "never run") || strings.Contains(screen, "0 runs") {
		t.Fatalf("a harness with no history counted its own silence:\n%s", screen)
	}
}

// TYPING NARROWS IT, on the model picker's own rungs: a prefix beats a
// substring, whatever the recency order was.
func TestTypingFiltersTheHarnessPicker(t *testing.T) {
	a, _, store := pickApp(t,
		demoHarness("release-notes", "write the release notes"),
		demoHarness("diff-review", "read a diff"),
		demoHarness("review-diff", "look over a change"))
	// review-diff is the most recent, so it leads the unfiltered list — which
	// is what makes the reordering below a fact about the ranking.
	ranAt(t, store, "review-diff", time.Now())

	typeInto(t, a, "/harness diff")
	if !a.harnPick.open {
		t.Fatal("the picker closed while a query was being typed")
	}
	first, ok := a.harnPick.at(0)
	if !ok || first.name != "diff-review" {
		t.Fatalf("the prefix match is not first: %+v", a.harnPick.hits)
	}
	if len(a.harnPick.hits) != 2 {
		t.Fatalf("the query kept %d rows, not the two that carry the word", len(a.harnPick.hits))
	}
	if screen := strings.Join(plainOverlay(a), "\n"); strings.Contains(screen, "release-notes") {
		t.Fatalf("a row that matches nothing is still on screen:\n%s", screen)
	}
}

// esc CLOSES THE LIST AND LEAVES THE SENTENCE, because a dismiss key that also
// took the draft away is a key nobody presses twice.
func TestEscapeLeavesTheHarnessDraftAlone(t *testing.T) {
	a, _, _ := pickApp(t, demoHarness("triage-flake", "chase a flaky test"))
	typeInto(t, a, "/harness tri")
	drive(t, a, key("esc"))
	if a.harnPick.open {
		t.Fatal("esc left the picker up")
	}
	if got := a.input.String(); got != "/harness tri" {
		t.Fatalf("esc rewrote the draft to %q", got)
	}
	if a.harnChip != "" {
		t.Fatalf("esc picked %q", a.harnChip)
	}
}

// ── the chip ────────────────────────────────────────────────────────────────

// ENTER WRITES THE CHIP AND CLEARS THE COMMAND, so the box is ready for the
// request and the name is not in the sentence.
func TestEnterOnTheHarnessPickerWritesAChip(t *testing.T) {
	a, _, _ := pickApp(t, demoHarness("triage-flake", "chase a flaky test"))
	typeInto(t, a, "/harness tri")
	drive(t, a, key("enter"))

	if a.harnPick.open {
		t.Fatal("the picker stayed up over the chip it wrote")
	}
	if a.harnChip != "triage-flake" {
		t.Fatalf("the chip holds %q", a.harnChip)
	}
	if got := a.input.String(); got != "" {
		t.Fatalf("the draft still reads %q", got)
	}
	strip := plain(a.chipStrip(a.width))
	for _, want := range []string{glyphHarnessChip, "triage-flake", glyphChipDrop, harnessPickHint} {
		if !strings.Contains(strip, want) {
			t.Fatalf("the tray row does not say %q: %q", want, strip)
		}
	}
	// AND THE TRAY IS PART OF THE BOX, one row above the draft (input.go).
	block, _, caretRow := a.inputBlock(a.width)
	if len(block) < 2 || caretRow != 1 || !strings.Contains(plain(block[0]), "triage-flake") {
		t.Fatalf("the chip is not the input block's first row: %q", block)
	}
}

// A LONG NAME IS ELLIPSIZED rather than pushing the row past the frame.
func TestALongHarnessNameIsCutInTheTray(t *testing.T) {
	long := "extremely-long-harness-name-to-cut"
	a, _, _ := pickApp(t, demoHarness(long, "a great deal of work"))
	typeInto(t, a, "/harness ")
	drive(t, a, key("enter"))

	strip := plain(a.chipStrip(a.width))
	if !strings.Contains(strip, glyphMore) {
		t.Fatalf("a %d-cell name was not cut: %q", len(long), strip)
	}
	if strings.Contains(strip, long) {
		t.Fatalf("the whole name is still on the row: %q", strip)
	}
}

// ONE CHIP AT A TIME: picking a second harness replaces the first.
func TestPickingASecondHarnessReplacesTheChip(t *testing.T) {
	a, _, _ := pickApp(t,
		demoHarness("triage-flake", "chase a flaky test"),
		demoHarness("review-diff", "read a diff"))
	typeInto(t, a, "/harness triage")
	drive(t, a, key("enter"))
	typeInto(t, a, "/harness review")
	drive(t, a, key("enter"))

	if a.harnChip != "review-diff" {
		t.Fatalf("the tray holds %q after a second pick", a.harnChip)
	}
	if strings.Count(plain(a.chipStrip(a.width)), glyphHarnessChip) != 1 {
		t.Fatalf("the tray drew two harnesses: %q", plain(a.chipStrip(a.width)))
	}
}

// BACKSPACE ON AN EMPTY BOX TAKES IT OFF, the way it takes a picture off: the
// tray is what is behind the caret when nothing is typed.
func TestBackspaceOnAnEmptyBoxDropsTheHarnessChip(t *testing.T) {
	a, _, _ := pickApp(t, demoHarness("triage-flake", "chase a flaky test"))
	typeInto(t, a, "/harness ")
	drive(t, a, key("enter"))
	drive(t, a, key("backspace"))

	if a.harnChip != "" {
		t.Fatalf("backspace left %q in the tray", a.harnChip)
	}
	if strip := a.chipStrip(a.width); strip != "" {
		t.Fatalf("the tray row survived the chip: %q", plain(strip))
	}
}

// A CLICK ON THE CHIP TAKES IT OFF TOO, which is the pointer's half of the same
// gesture (attach.go).
func TestClickingTheHarnessChipDropsIt(t *testing.T) {
	a, _, _ := pickApp(t, demoHarness("triage-flake", "chase a flaky test"))
	typeInto(t, a, "/harness ")
	drive(t, a, key("enter"))

	_, height := a.size()
	rows, _, _, _ := a.chrome(a.width)
	at := len(rows) - 1 - a.overlayHeight() - a.inputHeight()
	if _, took := a.chipPress(len(inputPad), height-len(rows)+at); !took {
		t.Fatal("the tray row did not answer a press on the chip")
	}
	if a.harnChip != "" {
		t.Fatalf("the press left %q in the tray", a.harnChip)
	}
}

// ── running it ──────────────────────────────────────────────────────────────

// SUBMIT WITH A CHIP RUNS THAT HARNESS ON EXACTLY WHAT WAS TYPED, and the chip
// clears: a harness is picked for a request, not for a conversation.
func TestSubmitWithAChipRunsThatHarness(t *testing.T) {
	a, agent, _ := pickApp(t, demoHarness("triage-flake", "chase a flaky test"))
	typeInto(t, a, "/harness ")
	drive(t, a, key("enter"))

	typeLine(t, a, "the login test flakes on CI")
	drive(t, a, streamClosedMsg{gen: a.gen})

	if agent.runs != 1 {
		t.Fatalf("the harness ran %d times", agent.runs)
	}
	if agent.name != "triage-flake" || agent.text != "the login test flakes on CI" {
		t.Fatalf("the run was handed (%q, %q)", agent.name, agent.text)
	}
	// NOTHING WENT THROUGH THE ORDINARY TURN. That is what "bypassing detection"
	// means, and a Submit here would be the person paying for both.
	if len(agent.sent) != 0 {
		t.Fatalf("the sentence was also submitted: %q", agent.sent)
	}
	if a.harnChip != "" {
		t.Fatalf("the chip survived the submit: %q", a.harnChip)
	}
	if screen := strings.Join(plainRows(a), "\n"); !strings.Contains(screen, "the login test flakes on CI") {
		t.Fatalf("the request is not in the transcript:\n%s", screen)
	}
}

// ENTER WITH A CHIP AND AN EMPTY BOX DOES NOTHING. The tray's hint already says
// to type the request, and a run on no words is a run nobody described.
func TestEnterWithAChipAndNoWordsRunsNothing(t *testing.T) {
	a, agent, _ := pickApp(t, demoHarness("triage-flake", "chase a flaky test"))
	typeInto(t, a, "/harness ")
	drive(t, a, key("enter"))
	drive(t, a, key("enter"))

	if agent.runs != 0 {
		t.Fatalf("an empty box ran the harness %d times", agent.runs)
	}
	if a.harnChip != "triage-flake" {
		t.Fatalf("the chip was spent on nothing: %q", a.harnChip)
	}
}

// A SESSION THAT CANNOT RUN ONE SAYS SO, and says the sentence /harness has
// always said. The capability is absent rather than broken.
func TestAHarnessChipOnASurfaceThatCannotRunOne(t *testing.T) {
	store := subharness.At(t.TempDir())
	if _, err := store.Save(demoHarness("triage-flake", "chase a flaky test")); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(&fakeAgent{})
	a.harn, a.width = store, 100
	typeInto(t, a, "/harness ")
	drive(t, a, key("enter"))
	typeLine(t, a, "have a look")

	if screen := strings.Join(plainRows(a), "\n"); !strings.Contains(screen, harnessUnavailableWord) {
		t.Fatalf("nothing said why the run did not start:\n%s", screen)
	}
	if a.harnChip != "" {
		t.Fatalf("the chip survived a refusal: %q", a.harnChip)
	}
}

// ── the doors ───────────────────────────────────────────────────────────────

// EVERY WORD FOR /harness OPENS THE SAME PICKER, and the list of those words is
// the command table's own row rather than a second list beside it
// ([harnessPickWords]). `/subharness` and `/sub` are deliberately NOT among them
// any more: they name the typed programs and their intake card (subharness.go),
// which is a different thing from a saved shape of work.
func TestEveryHarnessWordOpensTheSamePicker(t *testing.T) {
	for _, word := range []string{"/harness ", "/harnesses "} {
		a, _, _ := pickApp(t, demoHarness("triage-flake", "chase a flaky test"))
		typeInto(t, a, word)
		if !a.harnPick.open {
			t.Fatalf("%q opened no picker", word)
		}
		if got := a.harnPick.rows[0].name; got != "triage-flake" {
			t.Fatalf("%q listed %q", word, got)
		}
	}
}

// BARE /harness IS STILL THE PANEL. The word alone is "what have I got"; the
// word with a space after it is "run that one".
func TestBareHarnessStillOpensThePanel(t *testing.T) {
	a, _, _ := pickApp(t, demoHarness("triage-flake", "chase a flaky test"))
	typeLine(t, a, "/harness")
	if !a.harnPanel.open {
		t.Fatal("bare /harness no longer opens the registry panel")
	}
	if a.harnPick.open {
		t.Fatal("bare /harness opened the picker instead")
	}
}

// AND THE LAST ROW OPENS IT TOO, which is the picker's own way back to the
// cards.
func TestTheBrowseRowOpensTheRegistryPanel(t *testing.T) {
	a, _, _ := pickApp(t, demoHarness("triage-flake", "chase a flaky test"))
	typeInto(t, a, "/harness ")
	drive(t, a, key("down"))
	if !a.harnPick.browsing() {
		t.Fatal("the cursor did not reach the browse row")
	}
	drive(t, a, key("enter"))
	if !a.harnPanel.open {
		t.Fatal("the browse row opened nothing")
	}
	if a.harnChip != "" {
		t.Fatalf("the browse row picked %q", a.harnChip)
	}
	if got := a.input.String(); got != "" {
		t.Fatalf("the command is still in the draft: %q", got)
	}
}

// A CHIP DOES NOT SURVIVE /new. It is a choice made about the next message of
// THIS conversation, and a fresh session has no next message of that one.
func TestTheHarnessChipDoesNotSurviveNew(t *testing.T) {
	a, _, _ := pickApp(t, demoHarness("triage-flake", "chase a flaky test"))
	a.fresh = func() (Agent, string, error) { return &fakeAgent{}, "", nil }
	typeInto(t, a, "/harness ")
	drive(t, a, key("enter"))
	typeLine(t, a, "/new")

	if a.harnChip != "" {
		t.Fatalf("/new left %q in the tray", a.harnChip)
	}
	if strip := a.chipStrip(a.width); strip != "" {
		t.Fatalf("the tray row survived /new: %q", plain(strip))
	}
}

// A SURFACE WITH NO REGISTRY OPENS NO LIST AT ALL, rather than an empty overlay
// over a draft (harnesspanel.go's own law, one door over).
func TestTheHarnessPickerNeedsARegistry(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.width = 100
	typeInto(t, a, "/harness ")
	if a.harnPick.open {
		t.Fatal("a surface with no registry opened a picker")
	}
}
