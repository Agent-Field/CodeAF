package tui3

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// SubmitImage answers the seam for every test that never attaches anything: a
// message with no pictures is exactly Submit, which is what session.Agent
// documents and what the surface relies on.

func (f *fakeAgent) SubmitImage(ctx context.Context, text string, images []session.Image) (<-chan session.Event, error) {
	f.imageText = append(f.imageText, text)
	f.images = append(f.images, append([]session.Image(nil), images...))
	return f.Submit(ctx, text)
}

// SubmitStanding is the marked door: the same turn, remembered separately, so a
// test can tell which of the two a keystroke used (standmark.go).
func (f *fakeAgent) SubmitStanding(ctx context.Context, text string) (<-chan session.Event, error) {
	f.marked = append(f.marked, text)
	return f.Submit(ctx, text)
}

// imageAgent is the scripted session for this file: it records the message the
// tray assembled and can refuse it the way the vision gate does.
type imageAgent struct {
	*fakeAgent
	text   string
	images []session.Image
	calls  int
	refuse error
}

func (i *imageAgent) SubmitImage(ctx context.Context, text string, images []session.Image) (<-chan session.Event, error) {
	i.calls++
	i.text, i.images = text, images
	if i.refuse != nil {
		return nil, i.refuse
	}
	return i.fakeAgent.Submit(ctx, text)
}

// attachLab is a workspace with pictures in it: the surface, the agent, and the
// directory the completion walks.
func attachLab(t *testing.T, files map[string]int) (*app, *imageAgent, string) {
	t.Helper()
	dir := t.TempDir()
	for name, size := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	agent := &imageAgent{fakeAgent: &fakeAgent{model: "vendor/sees"}}
	a := newApp(context.Background(), Options{Agent: agent, Workspace: dir, ProfileDir: t.TempDir()})
	a.width, a.height = 60, 20
	a.pal = newPalette(tokens.ANSI256, false)
	a.entries = nil
	a.welcome = welcome{spent: true}
	a.touch()
	return a, agent, dir
}

// typeText types without pressing enter.
func typeText(t *testing.T, a *app, text string) {
	t.Helper()
	for _, r := range text {
		drive(t, a, key(string(r)))
	}
}

func tab() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyTab} }

func chipNames(a *app) []string {
	out := make([]string, 0, len(a.chips))
	for _, c := range a.chips {
		out = append(out, c.name())
	}
	return out
}

// THE ROW SAYS WHAT ENTER WILL DO. An image row is tagged, because choosing it
// attaches a file instead of typing a path, and a list where one row means
// something else without saying so is a list that surprises people.
func TestTheCompletionTagsThePicturesItWouldAttach(t *testing.T) {
	a, _, _ := attachLab(t, map[string]int{"shot.png": 12, "notes.md": 12})
	drive(t, a, key("@"), key("s"), key("h"))
	drive(t, a, filesLoadedMsg{paths: []string{"shot.png", "notes.md"}})

	rows := a.comp.rows(a.width, completeRows, a.pal, -1, "")
	found := ""
	for _, r := range rows {
		if strings.Contains(plain(r), "shot.png") {
			found = plain(r)
		}
		if strings.Contains(plain(r), "notes.md") && strings.Contains(plain(r), "img") {
			t.Fatalf("a markdown file was offered as a picture: %q", plain(r))
		}
	}
	if found == "" || !strings.Contains(found, "img") {
		t.Fatalf("the picture's row carries no img tag: %q\n%v", found, rows)
	}
}

// A PICTURE IS NOT TEXT: choosing one takes the half-typed token out of the
// sentence and puts a chip above the box.
func TestChoosingAPictureAttachesItInsteadOfTypingIt(t *testing.T) {
	a, _, dir := attachLab(t, map[string]int{"shot.png": 12})
	typeText(t, a, "look at @sh")
	drive(t, a, filesLoadedMsg{paths: []string{"shot.png"}})
	drive(t, a, key("enter"))

	if got := a.input.String(); got != "look at " {
		t.Fatalf("the draft is %q, want the @token gone and the sentence kept", got)
	}
	if want := []string{"shot.png"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("chips are %v, want %v", chipNames(a), want)
	}
	if got := a.chips[0].path; got != filepath.Join(dir, "shot.png") {
		t.Fatalf("the chip holds %q, want the resolved path under the workspace", got)
	}
	if strip := plain(a.chipStrip(a.width)); !strings.Contains(strip, "shot.png") {
		t.Fatalf("the tray reads %q, want the picture named in it", strip)
	}
	// The tray is a row of the input block, which is what keeps the frame, the
	// hit-testing and the height from disagreeing about where it is.
	if !strings.Contains(plain(frame(a)), "▣ #1 shot.png") {
		t.Fatalf("the tray is not on screen:\n%s", plain(frame(a)))
	}
}

// A NON-PICTURE STILL TYPES ITSELF. The @ completion's whole rule is unchanged
// for every file that is not an image.
func TestChoosingAFileStillWritesThePath(t *testing.T) {
	a, _, _ := attachLab(t, map[string]int{"notes.md": 12})
	typeText(t, a, "read @no")
	drive(t, a, filesLoadedMsg{paths: []string{"notes.md"}})
	drive(t, a, key("enter"))

	if got := a.input.String(); got != "read @notes.md" {
		t.Fatalf("the draft is %q, want the path typed into it", got)
	}
	if len(a.chips) != 0 {
		t.Fatalf("a markdown file was attached: %v", chipNames(a))
	}
}

// BACKSPACE ON AN EMPTY BOX IS THE TRAY'S. With nothing typed, the thing behind
// the caret is the last chip.
func TestBackspaceOnAnEmptyBoxTakesTheLastChipOff(t *testing.T) {
	a, _, dir := attachLab(t, map[string]int{"one.png": 12, "two.png": 12})
	a.attach(filepath.Join(dir, "one.png"))
	a.attach(filepath.Join(dir, "two.png"))

	drive(t, a, key("backspace"))
	if want := []string{"one.png"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("chips are %v, want %v", chipNames(a), want)
	}

	// With a sentence in the box it is the character behind the caret again.
	typeText(t, a, "hi")
	drive(t, a, key("backspace"))
	if got, want := a.input.String(), "h"; got != want {
		t.Fatalf("the draft is %q, want %q — backspace ate a chip instead of a letter", got, want)
	}
	if len(a.chips) != 1 {
		t.Fatalf("chips are %v, want the one still attached", chipNames(a))
	}
}

// A CLICK TAKES OFF THE CHIP IT LANDED ON, which is the whole reason this one
// gesture carries a column as well as a row.
func TestAClickOnAChipTakesThatChipOff(t *testing.T) {
	a, _, dir := attachLab(t, map[string]int{"one.png": 12, "two.png": 12})
	a.attach(filepath.Join(dir, "one.png"))
	a.attach(filepath.Join(dir, "two.png"))

	// The tray is the input block's FIRST row, read off the layout's own marks
	// rather than counted back from the foot of the chrome: the breathing blank
	// moved under the box on 2026-09-09 and a count would be a row out
	// (attach.go's [app.chipTrayTarget] says the whole of it).
	y := trayRow(a)
	// The second chip starts after the first label and the gap between them.
	x := len(inputPad) + ansi.StringWidth(chipLabels(a.chips, a.pal)[0]) + len(chipGap) + 1

	drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	if want := []string{"one.png"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("chips are %v, want %v — the click removed the wrong one", chipNames(a), want)
	}
}

// SUBMIT SENDS THE BYTES. The surface reads the files and hands the session
// pictures, not paths, and the tray is empty afterwards.
func TestSubmitSendsTheAttachedBytesAndEmptiesTheTray(t *testing.T) {
	a, agent, dir := attachLab(t, map[string]int{"shot.png": 64})
	a.attach(filepath.Join(dir, "shot.png"))
	typeLine(t, a, "what is this")

	if agent.calls != 1 {
		t.Fatalf("SubmitImage was called %d times, want once", agent.calls)
	}
	// The picture came off /image, so it had no token in the sentence and gets one
	// appended: "image 1" has to mean something whichever door it came in by.
	if agent.text != "what is this [image #1]" {
		t.Fatalf("the message read %q", agent.text)
	}
	if len(agent.images) != 1 {
		t.Fatalf("the message carried %d images, want 1", len(agent.images))
	}
	if got := len(agent.images[0].Bytes); got != 64 {
		t.Fatalf("the image carried %d bytes, want the file's 64", got)
	}
	if got := agent.images[0].Path; got != filepath.Join(dir, "shot.png") {
		t.Fatalf("the image's path is %q", got)
	}
	if len(a.chips) != 0 {
		t.Fatalf("the tray still holds %v after a message went out", chipNames(a))
	}
}

// THE TRANSCRIPT NAMES THE PICTURES above the thumbnail, so the sentence's
// number, the marker and its file door remain one correspondence.
func TestAnImageMessageMarksItsPicturesInTheTranscript(t *testing.T) {
	a, _, dir := attachLab(t, map[string]int{"shot.png": 8, "chart.png": 8})
	// Whether a link is written at all is read off TERM at construction
	// (pathlink.go), and TERM is whatever the shell that ran the tests exported.
	// This test asserts on both the dim paint and the link, so it says which.
	a.pathLinks = true
	// Wide enough that the sentence, its two tokens and its two markers land on
	// one row: this test is about what is drawn, not about where it wraps.
	a.width = 120
	a.attach(filepath.Join(dir, "shot.png"))
	a.attach(filepath.Join(dir, "chart.png"))
	typeLine(t, a, "what is wrong here")

	body := strings.Join(plainRows(a), "\n")
	// The markers carry the NUMBER as well as the name, because the sentence that
	// went with them carries `[image #1]` and a reader has to be able to see
	// which file that was (imagepaste.go).
	for _, want := range []string{"what is wrong here", "[#1 shot.png]", "[#2 chart.png]"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the transcript is missing %q:\n%s", want, body)
		}
	}
	// The markers follow the words rather than replacing them.
	if strings.Index(body, "[#1 shot.png]") < strings.Index(body, "what is wrong here") {
		t.Fatalf("the markers landed before the sentence:\n%s", body)
	}
	// And they are DIM inside the person's own bold line: the sentence is what
	// was said, the file names are the surface saying what went with it.
	//
	// The frame is read with its hyperlinks taken off first, because a marker
	// naming a file that is really there is also a door into it (pathlink.go) —
	// so the run carries an anchor and an underline the dim span did not use to
	// have. What this test is about is unchanged: the dim opens before the
	// markers and closes after them, with only the sentence's own bytes between.
	if !strings.Contains(unlinked(frame(a)), a.pal.dim("[#1 shot.png] [#2 chart.png]")) {
		t.Fatal("the markers are not drawn dim")
	}
	// AND THEY ARE DOORS. A person who attached the wrong screenshot finds out
	// by opening the one named in the transcript.
	if !strings.Contains(frame(a), "\x1b]8;;"+fileURI(filepath.Join(dir, "chart.png"))) {
		t.Fatal("an attached picture's marker is not a link to it")
	}
}

// unlinked is one frame with its hyperlinks and their underline taken off, for
// a test that is about some OTHER paint on the same run of text.
func unlinked(painted string) string {
	var out strings.Builder
	for i := 0; i < len(painted); {
		if n := escLen(painted, i); n > 0 {
			switch seq := painted[i : i+n]; {
			case strings.HasPrefix(seq, "\x1b]8;"), seq == sgrUnderOn, seq == sgrUnderOff:
			default:
				out.WriteString(seq)
			}
			i += n
			continue
		}
		out.WriteByte(painted[i])
		i++
	}
	return out.String()
}

// A REPLAYED PLACEHOLDER RENDERS AS IT ARRIVED. The journal holds a REFERENCE
// and never the bytes (session/sessionfile.go), so a picture whose file has
// moved comes back as a sentence — and the surface's job is to draw it, not to
// improve on it.
func TestReplayDrawsTheJournalsImagePlaceholder(t *testing.T) {
	past := []session.DisplayEntry{
		{Role: "user", Text: "[image /tmp/gone.png — file changed or gone]"},
	}
	agent := &imageAgent{fakeAgent: &fakeAgent{model: "vendor/sees", past: past}}
	a := newTestApp(agent)
	a.replay()

	body := strings.Join(plainRows(a), "\n")
	if !strings.Contains(body, "[image /tmp/gone.png — file changed or gone]") {
		t.Fatalf("the placeholder is not in the transcript:\n%s", body)
	}
}

// A REFUSED MESSAGE KEEPS ITS PICTURES. The gate answers before anything is
// journaled, so nothing was sent — and a surface that also lost the
// attachments would make the person go and find the files again.
func TestAGateRefusalKeepsTheChipsAndNamesTheModel(t *testing.T) {
	a, agent, dir := attachLab(t, map[string]int{"shot.png": 8})
	// A connection echoes before the far end accepts the turn, which is the
	// withdrawal flow this contract is about.
	a.host, a.localRoot = "devbox", dir
	agent.model = "vendor/blind"
	agent.refuse = errors.New(
		"session: vendor/blind cannot read images — switch to a model with vision, or describe what the picture shows")
	a.attach(filepath.Join(dir, "shot.png"))
	typeLine(t, a, "look at this")

	if want := []string{"shot.png"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("chips are %v, want them still attached after a refusal", chipNames(a))
	}
	body := strings.Join(plainRows(a), "\n")
	if !strings.Contains(body, "vendor/blind") || !strings.Contains(body, "cannot read images") {
		t.Fatalf("the refusal does not name the model:\n%s", body)
	}
	if a.state != stateIdle {
		t.Fatalf("the surface is %v after a refusal, want idle", a.state)
	}
	// Q7: withdrawing a refused user line also withdraws the thumbnail it owns.
	for _, e := range a.entries {
		if e.kind == entryUser {
			t.Fatalf("the refused message left its user block on the page: %+v", e)
		}
	}
}

// Q7: a refusal also withdraws a picture when another row landed after the
// echoed message, forcing the in-place branch that cannot remove its entry.
func TestARefusedMessageWithdrawsItsPictureBehindALaterRow(t *testing.T) {
	a, agent, dir := attachLab(t, nil)
	a.host, a.localRoot = "devbox", dir
	a.pal = newPalette(tokens.TrueColor, false)
	agent.model = "vendor/blind"
	agent.refuse = errors.New("session: vendor/blind cannot read images")
	path := writePicture(t, dir, "shot.png", wideTestPicture())
	a.attach(path)
	cmd := a.submitImages("look at this")
	if len(a.entries) != 1 || len(a.entries[0].pictures) != 1 || !a.entries[0].pending {
		t.Fatalf("the hosted picture was not echoed before submission: %+v", a.entries)
	}
	a.note("another row landed while the bytes were travelling")
	a.adopt(runSubmit(t, cmd))

	if len(a.entries) < 2 || a.entries[0].kind != entryUser {
		t.Fatalf("the in-place branch was not exercised: %+v", a.entries)
	}
	withdrawn := a.entries[0]
	if withdrawn.text != "" || len(withdrawn.pictures) != 0 || withdrawn.picturesHere || withdrawn.pending {
		t.Fatalf("the withdrawn block kept message state: %+v", withdrawn)
	}
	if body := frame(a); strings.Contains(body, halfBlock) || strings.Contains(plain(body), "[#1 shot.png]") {
		t.Fatalf("the refused picture remained on the page:\n%s", plain(body))
	}
}

// THE SIZE GUARD IS THE SESSION'S, ENFORCED AT THE DOOR, and it names the file:
// a person holding four chips needs to know which one the message is stuck on.
func TestAnOversizePictureIsRefusedByName(t *testing.T) {
	a, agent, dir := attachLab(t, map[string]int{"huge.png": maxAttachBytes + 1, "small.png": 8})
	a.attach(filepath.Join(dir, "small.png"))
	a.attach(filepath.Join(dir, "huge.png"))
	typeLine(t, a, "look")

	if agent.calls != 0 {
		t.Fatal("an oversize picture reached the session")
	}
	body := strings.Join(plainRows(a), "\n")
	if !strings.Contains(body, "huge.png") || !strings.Contains(body, "10MB") {
		t.Fatalf("the refusal does not name the file and the limit:\n%s", body)
	}
	if want := []string{"small.png", "huge.png"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("chips are %v, want %v — a refusal keeps the tray", chipNames(a), want)
	}
}

// AN EMPTY BOX WITH A PICTURE IN THE TRAY IS A MESSAGE. "What is this?" is
// often the picture itself.
func TestEnterSendsAPictureWithNoWords(t *testing.T) {
	a, agent, dir := attachLab(t, map[string]int{"shot.png": 8})
	a.attach(filepath.Join(dir, "shot.png"))
	drive(t, a, key("enter"))

	if agent.calls != 1 {
		t.Fatalf("SubmitImage was called %d times, want once", agent.calls)
	}
	// It goes out as its token alone. The picture still has to be nameable — a
	// follow-up turn saying "crop image 1" has to resolve — and a bare token is
	// what a message of no words and one picture honestly is.
	if agent.text != "[image #1]" {
		t.Fatalf("the message read %q, want the picture's token alone", agent.text)
	}
	if len(agent.images) != 1 {
		t.Fatalf("the message carried %d images", len(agent.images))
	}
}

// /attach IS THE OTHER DOOR: a path this directory's walk never offered, and
// a picture handed to it is a picture (/image, the word that took only
// pictures, is gone).
func TestTheAttachCommandAttachesAPicturePath(t *testing.T) {
	a, _, dir := attachLab(t, map[string]int{"shot.png": 8, "notes.md": 8})
	typeLine(t, a, "/attach shot.png")
	if want := []string{"shot.png"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("chips are %v, want %v", chipNames(a), want)
	}
	if got := a.chips[0].path; got != filepath.Join(dir, "shot.png") {
		t.Fatalf("the chip holds %q, want it resolved against the workspace", got)
	}

	typeLine(t, a, "/attach notes.md")
	if len(a.chips) != 2 || a.chips[1].name() != "notes.md" {
		t.Fatalf("a markdown file did not join the tray as a file: %v", chipNames(a))
	}

	typeLine(t, a, "/attach missing.png")
	if body := strings.Join(plainRows(a), "\n"); !strings.Contains(body, "no such file") {
		t.Fatalf("a path that is not there said nothing:\n%s", body)
	}
}

// TAB COMPLETES THE COMMAND'S PATH, and enter belongs to the line under it: a
// path typed out in full must not be swapped for whatever the list ranked first.
func TestTabCompletesTheAttachCommandsPath(t *testing.T) {
	a, _, dir := attachLab(t, map[string]int{"pictures/shot.png": 8})
	// An argument with nothing typed after it does not open a list of its own
	// accord — six hundred rows over an empty query is a list nobody asked for.
	typeText(t, a, "/attach ")
	if a.comp.open {
		t.Fatal("the path list opened over an empty argument")
	}
	drive(t, a, tab())
	if !a.comp.open || !a.comp.arg {
		t.Fatal("tab did not open the path list")
	}
	typeText(t, a, "pictures/sh")
	drive(t, a, tab())
	if got, want := a.input.String(), "/attach pictures/shot.png"; got != want {
		t.Fatalf("the draft is %q, want %q", got, want)
	}
	if a.comp.open {
		t.Fatal("the list stayed open on top of its own answer")
	}
	drive(t, a, key("enter"))
	if want := []string{"shot.png"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("chips are %v, want %v", chipNames(a), want)
	}
	if got := a.chips[0].path; got != filepath.Join(dir, "pictures", "shot.png") {
		t.Fatalf("the chip holds %q", got)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
