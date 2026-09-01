package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// homeDropLab is home with one project on it and a folder to drop files out of.
// The picture is the file every test here drags: a screenshot is what a person
// actually drops on this screen.
func homeDropLab(t *testing.T, files ...string) (*app, string) {
	t.Helper()
	lab := newHomeLab(t)
	lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())
	a := lab.app("")
	drop := t.TempDir()
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(drop, name), make([]byte, 12), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a.openHome()
	return a, drop
}

// THE WHOLE POINT OF THIS FIX, ON THE SCREEN IT WAS BROKEN ON. Home's box took
// a dropped screenshot as the raw escaped path the terminal wrote; it takes the
// same door the conversation's draft has always taken.
func TestDroppingAPictureOnHomeAttachesItAndLeavesTheToken(t *testing.T) {
	a, drop := homeDropLab(t, "shot.png")
	drive(t, a, tea.PasteMsg{Content: filepath.Join(drop, "shot.png")})

	if got := a.home.box.String(); got != "[image #1] " {
		t.Fatalf("home's box holds %q, want the path replaced by its token", got)
	}
	if want := []string{"shot.png"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("chips are %v, want %v", chipNames(a), want)
	}
	frame := homeText(a)
	if !strings.Contains(frame, "#1 shot.png") {
		t.Fatalf("home drew no tray over its box:\n%s", frame)
	}
	// AND THE LIST IS STILL THE LIST. The token matches no conversation on the
	// machine, so a filter that read it would empty the column at the moment a
	// picture landed in it.
	if !strings.Contains(frame, "Pricing Research") {
		t.Fatalf("the dropped picture emptied home's list:\n%s", frame)
	}
}

// A terminal escapes the spaces in a dropped path and a desktop's own drag
// protocol percent-escapes them; home reads both, because it is the same
// parsing the draft already had.
func TestHomeReadsAnEscapedAndAPercentEscapedDrop(t *testing.T) {
	name := "Screenshot 2026-08-31 at 5.21.40 PM.png"
	for _, spell := range []func(string) string{
		func(path string) string { return strings.ReplaceAll(path, " ", `\ `) },
		func(path string) string { return "file://" + strings.ReplaceAll(path, " ", "%20") },
	} {
		a, drop := homeDropLab(t, name)
		drive(t, a, tea.PasteMsg{Content: spell(filepath.Join(drop, name))})
		if got := a.home.box.String(); got != "[image #1] " {
			t.Fatalf("%q left home's box as %q", spell(name), got)
		}
		if want := []string{name}; !equalStrings(chipNames(a), want) {
			t.Fatalf("%q attached %v", spell(name), chipNames(a))
		}
	}
}

// AN ORDINARY FILE RIDES THE TRAY AND WRITES NO WORD, so the one thing that
// says home is holding something is the tray itself — and the action row has to
// count it, or a person who dropped a log file is looking at a screen that
// thinks nothing was typed.
func TestDroppingAFileOnHomeLightsStartWithNothingTyped(t *testing.T) {
	a, drop := homeDropLab(t, "server.log")
	drive(t, a, tea.PasteMsg{Content: filepath.Join(drop, "server.log")})

	if got := a.home.box.String(); got != "" {
		t.Fatalf("the local path stayed in home's box: %q", got)
	}
	if want := []string{"server.log"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("chips are %v, want %v", chipNames(a), want)
	}
	line, ok := a.home.focusedLine()
	if !ok || line.kind != homeAction {
		t.Fatalf("the cursor rests on %v, want the action row", line.kind)
	}
	if !strings.Contains(homeText(a), "server.log") {
		t.Fatalf("home drew no tray over its box:\n%s", homeText(a))
	}
}

// A HOME START CARRIES THE PICTURES. The chips go through [app.renew] with the
// person and out through the tray's own send door, so the model gets the pixels
// and the sentence names them.
func TestAHomeStartCarriesTheDroppedPictureIntoTheConversation(t *testing.T) {
	a, drop := homeDropLab(t, "shot.png")
	sees := &imageAgent{fakeAgent: &fakeAgent{model: "vendor/sees"}}
	a.start = func(workspace string) (Conversation, error) {
		return Conversation{Agent: sees, SessionFile: filepath.Join(drop, "transcript.jsonl")}, nil
	}
	drive(t, a, tea.PasteMsg{Content: filepath.Join(drop, "shot.png")})
	drive(t, a, key("enter"))

	if sees.calls != 1 {
		t.Fatalf("SubmitImage was called %d times, want once", sees.calls)
	}
	if len(sees.images) != 1 || len(sees.images[0].Bytes) != 12 {
		t.Fatalf("the conversation was handed %v, want the picture's bytes", sees.images)
	}
	if !strings.Contains(sees.text, imageToken(1)) {
		t.Fatalf("the sentence sent was %q, want the picture named in it", sees.text)
	}
}

// A FILE-ONLY DROP IS STILL A MESSAGE, and enter sends it with an empty
// sentence exactly as the conversation's own enter does.
func TestEnterOnHomeSendsAFileWithNoSentence(t *testing.T) {
	a, drop := homeDropLab(t, "server.log")
	sees := &imageAgent{fakeAgent: &fakeAgent{model: "vendor/sees"}}
	a.start = func(workspace string) (Conversation, error) {
		return Conversation{Agent: sees, SessionFile: filepath.Join(drop, "transcript.jsonl")}, nil
	}
	drive(t, a, tea.PasteMsg{Content: filepath.Join(drop, "server.log")})
	drive(t, a, key("enter"))

	if sees.calls != 1 {
		t.Fatalf("SubmitImage was called %d times, want once", sees.calls)
	}
	if !strings.Contains(sees.text, "server.log") {
		t.Fatalf("the sentence sent was %q, want the file named in it", sees.text)
	}
}

// THE DROP THAT CAME FROM ANOTHER MACHINE SAYS SO. iTerm2 → ssh → tmux hands a
// Linux TUI a Mac path; this used to insert it in silence, which reads as the
// drop having done nothing.
func TestADropOnHomeThatIsNotOnThisMachineSaysSo(t *testing.T) {
	a, drop := homeDropLab(t)
	path := filepath.Join(drop, "Screenshot.png")
	drive(t, a, tea.PasteMsg{Content: path})

	if !strings.Contains(homeText(a), "Screenshot.png is not on this machine") {
		t.Fatalf("home said nothing about the missing file:\n%s", homeText(a))
	}
	if got := a.home.box.String(); got != path {
		t.Fatalf("home's box holds %q, want the text left where it landed", got)
	}
}

// The same sentence in the conversation — and never over a sentence that merely
// mentions a file, which is the paste this surface sees a thousand times more
// often.
func TestAMissingDropSaysSoInTheDraftAndASentenceDoesNot(t *testing.T) {
	a, _, dir := attachLab(t, nil)
	path := filepath.Join(dir, "gone", "shot.png")
	pasteText(t, a, path)
	if said := strings.Join(plainRows(a), "\n"); !strings.Contains(said, "shot.png is not on this machine") {
		t.Fatalf("the conversation said nothing about the missing file:\n%s", said)
	}
	if got := a.input.String(); got != path {
		t.Fatalf("the draft holds %q, want the text left where it landed", got)
	}

	a, _, dir = attachLab(t, nil)
	pasteText(t, a, "have a look at "+filepath.Join(dir, "gone", "shot.png"))
	if said := strings.Join(plainRows(a), "\n"); strings.Contains(said, "not on this machine") {
		t.Fatalf("a sentence mentioning a path was answered as a drop:\n%s", said)
	}
}

// A DROP THAT REACHED ENTER IS STILL A DROP, on home as in the conversation.
// Some terminals type a dragged file in character by character, and home's own
// dispatcher answered the result with `unknown command`.
func TestADroppedPathTypedIntoHomeIsNotAnUnknownCommand(t *testing.T) {
	a, drop := homeDropLab(t, "shot.png")
	path := filepath.Join(drop, "shot.png")
	a.home.box.setText(path)
	a.home.build()
	if word := a.home.runLabel(path); word != "" {
		t.Fatalf("home offered to run a dropped path: %q", word)
	}
	drive(t, a, key("enter"))
	if want := []string{"shot.png"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("enter on a dropped path attached %v", chipNames(a))
	}
	if got := a.home.box.String(); got != "[image #1] " {
		t.Fatalf("home's box holds %q, want the token", got)
	}
}

// AND THE ERRAND PANE HAS A TRAY OF ITS OWN, because it is its own conversation
// with its own next message.
func TestAnErrandCarriesThePictureDroppedIntoIt(t *testing.T) {
	lab, a := exchangeLab(t)
	ex := theExchange(a)
	if !ex.focused {
		t.Fatal("asking here should put the keyboard in the pane")
	}
	drop := t.TempDir()
	path := filepath.Join(drop, "chart.png")
	if err := os.WriteFile(path, make([]byte, 9), 0o600); err != nil {
		t.Fatal(err)
	}
	drive(t, a, tea.PasteMsg{Content: path})
	if got := ex.box.String(); got != "[image #1] " {
		t.Fatalf("the pane's box holds %q, want the token", got)
	}
	if len(ex.chips) != 1 || ex.chips[0].name() != "chart.png" {
		t.Fatalf("the pane holds %v, want the picture", ex.chips)
	}
	if !strings.Contains(homeText(a), "#1 chart.png") {
		t.Fatalf("the pane drew no tray over its box:\n%s", homeText(a))
	}
	drive(t, a, key("enter"))
	if len(lab.agent.images) != 1 || len(lab.agent.images[0].Bytes) != 9 {
		t.Fatalf("the errand was handed %v, want the picture's bytes", lab.agent.images)
	}
	if len(ex.chips) != 0 {
		t.Fatalf("the tray still holds %v after the send", ex.chips)
	}
}
