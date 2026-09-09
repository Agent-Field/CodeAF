package tui3

// attachfile_test.go is the file half of the tray: `/attach`, its refusals, the
// ceilings that only exist where bytes cross a connection, and the two shapes a
// message with a file on it takes — the local one that names a path and the
// remote one that carries bytes.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// fileAgent is a scripted session that can also be handed FILES — the seam a
// remote agent presents and a local one does not ([fileSubmitter]).
type fileAgent struct {
	*imageAgent
	text   string
	files  []remote.WireFile
	images []session.Image
	calls  int
}

func (f *fileAgent) SubmitFiles(ctx context.Context, text string, files []remote.WireFile, images []session.Image) (<-chan session.Event, error) {
	f.calls++
	f.text, f.files, f.images = text, files, images
	return f.fakeAgent.Submit(ctx, text)
}

// fileLab is [attachLab] with a machine name on it: empty is a local session,
// anything else is a session over a connection.
func fileLab(t *testing.T, host string, files map[string]int) (*app, *fileAgent, string) {
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
	agent := &fileAgent{imageAgent: &imageAgent{fakeAgent: &fakeAgent{model: "vendor/sees"}}}
	a := newApp(context.Background(), Options{Agent: agent, Workspace: dir, Host: host})
	// Over a connection a relative path is anchored to THIS machine and not to
	// the remote workspace ([app.pathRoot]), so the lab's directory has to be
	// this machine's for the test to be typing what a person would type.
	if host != "" {
		a.localRoot = dir
	}
	a.width, a.height = 60, 20
	a.pal = newPalette(tokens.ANSI256, false)
	a.entries = nil
	a.welcome = welcome{spent: true}
	a.touch()
	return a, agent, dir
}

// A LOCAL SESSION IS HANDED THE PATH AND NOTHING MOVES. The engine is on this
// machine, the path already means something to it, and copying the file would
// only give the person two of them.
func TestALocalAttachmentIsNamedByPathAndNothingTravels(t *testing.T) {
	a, agent, dir := fileLab(t, "", map[string]int{"server.log": 40})
	a.attachFilePath("server.log")
	if len(a.chips) != 1 || !a.chips[0].file {
		t.Fatalf("the tray holds %v", a.chips)
	}

	typeText(t, a, "why is this failing")
	drive(t, a, key("enter"))

	if agent.calls != 0 {
		t.Fatal("a local session must not be handed bytes")
	}
	if agent.imageAgent.calls != 1 {
		t.Fatalf("the message went through %d image submits", agent.imageAgent.calls)
	}
	want := "why is this failing\n\nattached file: " + filepath.Join(dir, "server.log")
	if agent.imageAgent.text != want {
		t.Fatalf("the message is\n%q\nwant\n%q", agent.imageAgent.text, want)
	}
	// AND THE SCREEN KEEPS THE PERSON'S OWN LINE. The path goes to the model and
	// the NAME goes in the transcript — a scrollback of absolute paths is a
	// scrollback nobody reads.
	said := ""
	for _, e := range a.entries {
		if e.kind == entryUser {
			said = plain(e.text)
		}
	}
	if !strings.Contains(said, "[server.log]") || strings.Contains(said, dir) {
		t.Fatalf("the transcript line is %q", said)
	}
}

// OVER A CONNECTION THE BYTES TRAVEL, because the far machine has never seen
// this disk. The surface composes no path sentence at all: the engine writes the
// files down and names the paths that are true where the journal is.
func TestARemoteAttachmentTravelsAsBytesAndNamesNoLocalPath(t *testing.T) {
	a, agent, _ := fileLab(t, "devbox", map[string]int{"notes.csv": 12})
	a.attachFilePath("notes.csv")
	typeText(t, a, "read this")
	drive(t, a, key("enter"))

	if agent.calls != 1 {
		t.Fatalf("the file seam was used %d times", agent.calls)
	}
	if agent.text != "read this" {
		t.Fatalf("the surface must not compose the engine's sentence: %q", agent.text)
	}
	if len(agent.files) != 1 {
		t.Fatalf("%d files travelled", len(agent.files))
	}
	// THE NAME IS A NAME AND NEVER A PATH: the engine joins it to a directory of
	// its own choosing, so a path here would be this surface asking a remote
	// machine to write wherever it liked.
	if agent.files[0].Name != "notes.csv" {
		t.Fatalf("the name travelled as %q", agent.files[0].Name)
	}
	if len(agent.files[0].Bytes) != 12 {
		t.Fatalf("%d bytes travelled", len(agent.files[0].Bytes))
	}
}

// A picture and a file on one tray are ONE message and one turn, and the
// picture's number counts pictures rather than chips.
func TestAPictureAndAFileRideOneMessage(t *testing.T) {
	a, agent, _ := fileLab(t, "devbox", map[string]int{"a.log": 8, "shot.png": 8, "b.csv": 8})
	a.attachFilePath("a.log")
	a.attachPath("shot.png")
	a.attachFilePath("b.csv")

	labels := chipLabels(a.chips, a.pal)
	if len(labels) != 3 || !strings.HasPrefix(labels[1], "▣ #1 ") {
		t.Fatalf("the picture is the FIRST picture whatever sits before it: %v", labels)
	}
	if strings.Contains(labels[0], "#") || strings.Contains(labels[2], "#") {
		t.Fatalf("a file carries no number: %v", labels)
	}

	typeText(t, a, "look")
	drive(t, a, key("enter"))
	if agent.calls != 1 {
		t.Fatalf("one message is one call, not %d", agent.calls)
	}
	if len(agent.files) != 2 || len(agent.images) != 1 {
		t.Fatalf("%d files and %d pictures travelled", len(agent.files), len(agent.images))
	}
	if !strings.Contains(agent.text, "[image #1]") {
		t.Fatalf("the picture's own token has to be in the sentence: %q", agent.text)
	}
}

// THE CEILING IS ASKED AT THE DOOR, where it can still be asked about one file
// rather than about a message — and it says the size and the limit, in the same
// shape the picture's own refusal uses.
func TestAFileOverTheCeilingIsRefusedAtTheDoorOverAConnection(t *testing.T) {
	a, _, _ := fileLab(t, "devbox", map[string]int{"dump.bin": maxAttachedFileBytes + 1})
	a.attachFilePath("dump.bin")
	if len(a.chips) != 0 {
		t.Fatal("a file too big to send must not sit on the tray pretending it will")
	}
	// THE SIZE IS ROUNDED UP: a file one byte past the ceiling must not be
	// described as being exactly the ceiling and over it.
	if got, want := lastNote(t, a), "dump.bin is 17MB and over the 16MB file limit"; plain(got) != want {
		t.Fatalf("the refusal is %q, want %q", plain(got), want)
	}
}

// AND NOT LOCALLY, because a limit exists where bytes cross a wire and refusing
// a file that is not going anywhere would be a rule invented for its own sake.
func TestALargeFileIsFineOnALocalSession(t *testing.T) {
	a, _, _ := fileLab(t, "", map[string]int{"dump.bin": maxAttachedFileBytes + 1})
	a.attachFilePath("dump.bin")
	if len(a.chips) != 1 {
		t.Fatal("a local session moves nothing and refuses nothing for weight")
	}
}

// The message's own ceiling, said about the message.
func TestTheFilesOnOneMessageHaveATotalCeiling(t *testing.T) {
	dir := t.TempDir()
	chips := make([]chip, 0, 3)
	for _, name := range []string{"one.bin", "two.bin", "three.bin"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, make([]byte, maxAttachedFileBytes), 0o644); err != nil {
			t.Fatal(err)
		}
		chips = append(chips, chip{path: path, file: true})
	}
	_, err := readFiles(chips)
	if err == nil {
		t.Fatal("three files at the per-file ceiling are over the message's")
	}
	if want := "the files on this message are over the 32MB limit"; err.Error() != want {
		t.Fatalf("the refusal is %q, want %q", err.Error(), want)
	}
}

func TestEveryOtherRefusalAttachCanGive(t *testing.T) {
	a, _, dir := fileLab(t, "", map[string]int{"server.log": 8, "sub/inner.txt": 8})
	for _, test := range []struct{ arg, want string }{
		{"nope.txt", "no such file: nope.txt"},
		// `sub` USED TO BE HERE, AND IT IS NOT A REFUSAL ANY MORE. A directory
		// after /attach is now the folder door
		// (TestAttachOnAFolderTakesTheFolderDoorInsteadOfRefusing).
		//
		// AND NEITHER IS A BARE `/attach`. `/attach takes a path · try /attach
		// server.log` used to be the first row here; the command now opens the
		// context browser with file intent instead of correcting somebody who
		// does not know the path
		// (TestABareAttachOpensTheBrowserWhereTheConversationStands).
	} {
		a.entries = nil
		a.attachFilePath(test.arg)
		if got := plain(lastNote(t, a)); got != test.want {
			t.Fatalf("/attach %q said %q, want %q", test.arg, got, test.want)
		}
	}
	a.entries = nil
	a.attachFilePath("server.log")
	a.attachFilePath(filepath.Join(dir, "server.log"))
	if got, want := plain(lastNote(t, a)), "server.log is already attached"; got != want {
		t.Fatalf("the second attach said %q, want %q", got, want)
	}
	if len(a.chips) != 1 {
		t.Fatalf("the same path twice is one chip, not %d", len(a.chips))
	}
}

// A PICTURE HANDED TO /attach IS STILL A PICTURE. Somebody who learned one word
// for putting a thing into a message should not have to find out this build has
// two, and a PNG sent as a file is a path a model can read bytes out of and
// never look at.
func TestAPictureHandedToAttachGoesOnAsAPicture(t *testing.T) {
	a, _, _ := fileLab(t, "", map[string]int{"shot.png": 8})
	a.attachFilePath("shot.png")
	if len(a.chips) != 1 || a.chips[0].file {
		t.Fatalf("the tray holds %v", a.chips)
	}
	if label := chipLabels(a.chips, a.pal)[0]; !strings.HasPrefix(label, "▣ #1 ") {
		t.Fatalf("the chip is %q", label)
	}
}

// Taking a file off the tray renumbers nothing, because a file never put a
// token in the sentence — and taking a PICTURE off still counts the ones behind
// it down.
func TestRemovingAFileLeavesThePictureNumbersAlone(t *testing.T) {
	a, _, _ := fileLab(t, "", map[string]int{"a.log": 8, "one.png": 8, "two.png": 8})
	a.attachFilePath("a.log")
	a.attachPath("one.png")
	a.attachPath("two.png")
	typeText(t, a, "compare [image #1] and [image #2]")

	a.removeChip(0) // the file
	if got := a.input.String(); got != "compare [image #1] and [image #2]" {
		t.Fatalf("removing a file rewrote the sentence: %q", got)
	}
	a.removeChip(0) // now the first picture
	if got := a.input.String(); got != "compare and [image #1]" {
		t.Fatalf("removing the first picture left %q", got)
	}
}

// A refusal hands the whole tray back, files included: a message the surface
// could not send is a message the person still holds.
func TestARefusedMessageGivesTheFilesBack(t *testing.T) {
	a, _, _ := fileLab(t, "", map[string]int{"a.log": 8})
	a.attachFilePath("a.log")
	sent := append([]chip(nil), a.chips...)
	a.chips, a.sent = nil, sent
	a.chipsSettled(os.ErrPermission)
	if len(a.chips) != 1 || !a.chips[0].file {
		t.Fatalf("the tray came back as %v", a.chips)
	}
}
