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

// homeDropKeys is [homeDropLab] with a clock that does not move on its own, so
// a run of characters can be delivered with provably nothing between them and
// the fold's wakeup settled deliberately. It is dropkeys_test.go's `dropLab`
// arrangement pointed at the other box.
func homeDropKeys(t *testing.T, files ...string) (*app, string, func(time.Duration)) {
	t.Helper()
	a, drop := homeDropLab(t, files...)
	at := time.Now()
	a.clock = func() time.Time { return at }
	return a, drop, func(d time.Duration) { at = at.Add(d) }
}

// ISSUE #163, ON THE SCREEN IT WAS REPORTED ON. Some terminals TYPE a dragged
// file in character by character, and home held the raw path until enter —
// filtering its list by a path no conversation on this machine matches. The
// fold converts it two frames after it goes quiet, through the one door.
func TestAPictureTypedIntoHomeBecomesAChipWhileItArrives(t *testing.T) {
	a, drop, tick := homeDropKeys(t, "shot.png")
	typeBurst(a, filepath.Join(drop, "shot.png"))
	settleDrop(t, a, tick)

	if got := a.home.box.String(); got != "[image #1] " {
		t.Fatalf("home's box holds %q, want the typed path replaced by its token", got)
	}
	if want := []string{"shot.png"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("chips are %v, want %v", chipNames(a), want)
	}
	frame := homeText(a)
	if !strings.Contains(frame, "#1 shot.png") {
		t.Fatalf("home drew no tray over its box:\n%s", frame)
	}
	// AND THE LIST IS THE LIST AGAIN. It empties while the path is arriving,
	// because the box IS the query; the conversion is what gives it back.
	if !strings.Contains(frame, "Pricing Research") {
		t.Fatalf("the typed drop left home's list empty:\n%s", frame)
	}
}

// AND ENTER INSIDE THE QUIET WINDOW STILL MEANS THE DROP, which is input.go's
// law about enter said at home's own send door: two frames is not a wait
// somebody owes before pressing a key.
func TestATypedDropOnHomeEnteredAtOnceStillAttachesAndStarts(t *testing.T) {
	a, drop, _ := homeDropKeys(t, "shot.png")
	sees := &imageAgent{fakeAgent: &fakeAgent{model: "vendor/sees"}}
	a.start = func(workspace string) (Conversation, error) {
		return Conversation{Agent: sees, SessionFile: filepath.Join(drop, "transcript.jsonl")}, nil
	}
	typeBurst(a, filepath.Join(drop, "shot.png"))
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
	if got := plain(frame(a)); strings.Contains(got, unknownCommandLead) {
		t.Fatalf("a typed drop on home was refused as a command:\n%s", got)
	}
}

// AND THE ERRAND PANE'S BOX TAKES A TYPED DROP TOO, onto the errand's own tray
// and never the conversation's.
func TestATypedDropIntoTheErrandPaneBecomesAChip(t *testing.T) {
	_, a := exchangeLab(t)
	ex := theExchange(a)
	if !ex.focused {
		t.Fatal("asking here should put the keyboard in the pane")
	}
	now := time.Now()
	a.clock = func() time.Time { return now }
	drop := t.TempDir()
	path := filepath.Join(drop, "chart.png")
	if err := os.WriteFile(path, make([]byte, 9), 0o600); err != nil {
		t.Fatal(err)
	}
	typeBurst(a, path)
	settleDrop(t, a, func(d time.Duration) { now = now.Add(d) })

	if got := ex.box.String(); got != "[image #1] " {
		t.Fatalf("the pane's box holds %q, want the token", got)
	}
	if len(ex.chips) != 1 || ex.chips[0].name() != "chart.png" {
		t.Fatalf("the pane holds %v, want the picture", ex.chips)
	}
	if len(a.chips) != 0 {
		t.Fatalf("the pane's drop landed on the conversation's tray: %v", chipNames(a))
	}
}

// ONE FOLD, ONE BOX. A run left standing on a screen that went away is never
// spent into the box that took the keyboard after it — the token would be
// written into a line nobody was looking at when they dropped anything.
func TestAFoldOpenedOnHomeIsNeverSpentIntoTheDraft(t *testing.T) {
	a, drop, tick := homeDropKeys(t, "shot.png")
	path := filepath.Join(drop, "shot.png")
	typeBurst(a, path)
	a.closeHome()
	settleDrop(t, a, tick)

	if len(a.chips) != 0 {
		t.Fatalf("a fold left on home attached %v after home closed", chipNames(a))
	}
	if got := a.input.String(); got != "" {
		t.Fatalf("the draft holds %q, want nothing written into it", got)
	}
}

// AND THE SAME LAW IN THE OTHER DIRECTION.
func TestAFoldOpenedInTheDraftIsNeverSpentIntoHome(t *testing.T) {
	a, drop, tick := homeDropKeys(t, "shot.png")
	a.closeHome()
	path := filepath.Join(drop, "shot.png")
	typeBurst(a, path)
	a.openHome()
	settleDrop(t, a, tick)

	if len(a.chips) != 0 {
		t.Fatalf("a fold left in the draft attached %v after home opened", chipNames(a))
	}
	if got := a.home.box.String(); got != "" {
		t.Fatalf("home's box holds %q, want nothing written into it", got)
	}
	if got := a.input.String(); got != path {
		t.Fatalf("the draft holds %q, want the characters exactly as typed", got)
	}
}

// ISSUE #164. A START IN A PASTED FOLDER CARRIES THE TRAY WITH THE PERSON. The
// files were dropped on HOME, for the conversation home is about to open, and
// the aside that steps the previous one out of the way used to take them.
func TestAStartInAPastedFolderCarriesTheTrayWithThePerson(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	drop, where := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(drop, "server.log"), make([]byte, 12), 0o600); err != nil {
		t.Fatal(err)
	}
	a.openHome()
	drive(t, a, tea.PasteMsg{Content: filepath.Join(drop, "server.log")})
	if want := []string{"server.log"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("the drop did not land on home's tray: %v", chipNames(a))
	}
	pasteText(t, a, where)
	drive(t, a, key("enter"))

	if a.workspace != where {
		t.Fatalf("the row opened %q, want a conversation in the typed folder", a.workspace)
	}
	if want := []string{"server.log"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("the conversation the row opened holds %v, want the dropped file", chipNames(a))
	}
	// AND THE ONE THAT STEPPED ASIDE DOES NOT HOLD IT. Its own draft is still
	// its own — that is a different law and it is untouched — but a file
	// dropped on home was never its.
	if len(a.behind) == 0 {
		t.Fatal("nothing stepped aside, so the tray had nowhere wrong to go")
	}
	for key, held := range a.behind {
		if len(held.side.chips) != 0 {
			t.Fatalf("%s kept %v", key, held.side.chips)
		}
	}
	// AND NOTHING WAS SENT — a send empties the tray, and the assertion above is
	// that it is still full. What was typed named a PLACE and not a sentence, so
	// there was never a message to send: the file is in front of the person in
	// the conversation they asked for, waiting for the words it goes with.
	if got := a.input.String(); got != "" {
		t.Fatalf("the new conversation's draft holds %q, want the place spent", got)
	}
}
