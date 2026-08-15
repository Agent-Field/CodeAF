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

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// SubmitImage answers the seam for every test that never attaches anything: a
// message with no pictures is exactly Submit, which is what session.Agent
// documents and what the surface relies on.
func (f *fakeAgent) SubmitImage(ctx context.Context, text string, _ []session.Image) (<-chan session.Event, error) {
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
	a := newApp(context.Background(), Options{Agent: agent, Workspace: dir})
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

	rows := a.comp.rows(a.width, completeRows, a.pal, -1)
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
	if !strings.Contains(plain(frame(a)), "▣ shot.png") {
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

	width, height := a.size()
	rows, _, _, _ := a.chrome(width)
	at := len(rows) - 1 - a.overlayHeight() - a.inputHeight()
	y := height - len(rows) + at
	// The second chip starts after the first label and the gap between them.
	x := len(inputPad) + ansi.StringWidth(chipLabels(a.chips, a.pal)[0]) + len(chipGap) + 1

	drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
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
	if agent.text != "what is this" {
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

// THE TRANSCRIPT NAMES THE PICTURES. A terminal cell cannot show one, and the
// honest thing to draw is the file the person pointed at.
func TestAnImageMessageMarksItsPicturesInTheTranscript(t *testing.T) {
	a, _, dir := attachLab(t, map[string]int{"shot.png": 8, "chart.png": 8})
	a.attach(filepath.Join(dir, "shot.png"))
	a.attach(filepath.Join(dir, "chart.png"))
	typeLine(t, a, "what is wrong here")

	body := strings.Join(plainRows(a), "\n")
	for _, want := range []string{"what is wrong here", "[shot.png]", "[chart.png]"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the transcript is missing %q:\n%s", want, body)
		}
	}
	// The markers follow the words rather than replacing them.
	if strings.Index(body, "[shot.png]") < strings.Index(body, "what is wrong here") {
		t.Fatalf("the markers landed before the sentence:\n%s", body)
	}
	// And they are DIM inside the person's own bold line: the sentence is what
	// was said, the file names are the surface saying what went with it.
	if !strings.Contains(frame(a), a.pal.dim("[shot.png] [chart.png]")) {
		t.Fatal("the markers are not drawn dim")
	}
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
	if agent.text != "" {
		t.Fatalf("the message read %q, want the words empty", agent.text)
	}
	if len(agent.images) != 1 {
		t.Fatalf("the message carried %d images", len(agent.images))
	}
}

// /image IS THE OTHER DOOR: a path this directory's walk never offered.
func TestTheImageCommandAttachesAPath(t *testing.T) {
	a, _, dir := attachLab(t, map[string]int{"shot.png": 8, "notes.md": 8})
	typeLine(t, a, "/image shot.png")
	if want := []string{"shot.png"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("chips are %v, want %v", chipNames(a), want)
	}
	if got := a.chips[0].path; got != filepath.Join(dir, "shot.png") {
		t.Fatalf("the chip holds %q, want it resolved against the workspace", got)
	}

	typeLine(t, a, "/image notes.md")
	if len(a.chips) != 1 {
		t.Fatalf("a markdown file was attached: %v", chipNames(a))
	}
	if body := strings.Join(plainRows(a), "\n"); !strings.Contains(body, "not a picture") {
		t.Fatalf("nothing said why:\n%s", body)
	}

	typeLine(t, a, "/image missing.png")
	if body := strings.Join(plainRows(a), "\n"); !strings.Contains(body, "no such picture") {
		t.Fatalf("a path that is not there said nothing:\n%s", body)
	}
}

// TAB COMPLETES THE COMMAND'S PATH, and enter belongs to the line under it: a
// path typed out in full must not be swapped for whatever the list ranked first.
func TestTabCompletesTheImageCommandsPath(t *testing.T) {
	a, _, dir := attachLab(t, map[string]int{"pictures/shot.png": 8})
	// An argument with nothing typed after it does not open a list of its own
	// accord — six hundred rows over an empty query is a list nobody asked for.
	typeText(t, a, "/image ")
	if a.comp.open {
		t.Fatal("the path list opened over an empty argument")
	}
	drive(t, a, tab())
	if !a.comp.open || !a.comp.arg {
		t.Fatal("tab did not open the path list")
	}
	typeText(t, a, "pictures/sh")
	drive(t, a, tab())
	if got, want := a.input.String(), "/image pictures/shot.png"; got != want {
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
