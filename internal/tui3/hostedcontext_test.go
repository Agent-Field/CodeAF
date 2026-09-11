package tui3

// hostedcontext_test.go is the ONE context chooser seen across a connection:
// files are on the machine in front of the person and may travel, while folders
// cannot become lasting context for the conversation on the other machine.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// A BARE /attach OVER A CONNECTION OPENS THIS MACHINE'S BROWSING SHEET. It is
// a request about a file, so no folder refusal belongs in the conversation.
func TestABareAttachOverAConnectionOpensTheLocalSheet(t *testing.T) {
	a, _, dir := fileLab(t, "devbox", map[string]int{"notes.txt": 8})
	before := len(a.entries)

	settleFolder(t, a, a.slash("/attach"))

	if !a.folder.open || !a.folder.browsing {
		t.Fatalf("a bare hosted /attach opened browsing=%v open=%v", a.folder.browsing, a.folder.open)
	}
	if a.folder.cols.dir != dir {
		t.Fatalf("the chooser opened on %s, want this machine's %s", a.folder.cols.dir, dir)
	}
	if len(a.entries) != before {
		t.Fatalf("opening the chooser wrote %d lines into the conversation", len(a.entries)-before)
	}
}

// EVERY SPELLING OF /folder STILL REFUSES OVER A CONNECTION, including when a
// query was supplied. A query changes where a local sheet starts and cannot
// turn a far conversation into somewhere this machine's folders may be kept.
func TestEveryFolderCommandStillRefusesOverAConnection(t *testing.T) {
	for _, command := range []string{"/folder project", "/place project", "/dir project"} {
		t.Run(command, func(t *testing.T) {
			a, _, _ := fileLab(t, "devbox", nil)
			if cmd := a.slash(command); cmd != nil {
				t.Fatalf("%s started work", command)
			}
			if a.folder.open {
				t.Fatalf("%s opened the chooser", command)
			}
			if got := plain(lastNote(t, a)); got != folderRemoteWord {
				t.Fatalf("%s said %q", command, got)
			}
		})
	}
}

// A FOLDER MARKED ON A HOSTED FILE SHEET REFUSES AT CONFIRM. The sheet and the
// marks remain because the person still has a valid file chooser in front of
// them and may change the selection without starting over.
func TestAHostedFolderConfirmRefusesAndKeepsTheMarks(t *testing.T) {
	a, agent, _ := fileLab(t, "devbox", map[string]int{"folder/inside.txt": 8})
	settleFolder(t, a, a.slash("/attach"))
	onFolderRow(t, a, "folder")
	drive(t, a, key(folderMarkKey))
	before := len(agent.places)

	if cmd := a.folderConfirm(); cmd != nil {
		t.Fatal("a hosted folder confirm started registration work")
	}

	if got := plain(lastNote(t, a)); got != folderRemoteWord {
		t.Fatalf("the confirm said %q", got)
	}
	if !a.folder.open {
		t.Fatal("the refusal closed the sheet")
	}
	if len(a.folder.marks) != 1 || !a.folder.marks[0].dir {
		t.Fatalf("the refusal discarded the person's marks: %+v", a.folder.marks)
	}
	if len(agent.places) != before {
		t.Fatalf("the far conversation gained %+v", agent.places[before:])
	}
}

// A FILE MARKED ON THAT SAME HOSTED SHEET STILL REACHES THE NEXT MESSAGE'S
// tray. The folder wall is not a wall in front of bytes that already travel.
func TestAHostedFileConfirmStillReachesTheTray(t *testing.T) {
	a, _, _ := fileLab(t, "devbox", map[string]int{"notes.txt": 8})
	settleFolder(t, a, a.slash("/attach"))
	onFolderRow(t, a, "notes.txt")
	drive(t, a, key(folderMarkKey))

	settleFolder(t, a, a.folderConfirm())

	if got := strings.Join(chipNames(a), ","); got != "notes.txt" {
		t.Fatalf("the tray holds %q", got)
	}
}

// A DIRECTORY TYPED AFTER /attach IS LOCAL TO THE MACHINE IN FRONT OF THE
// person. It is refused over a connection, while the same gesture locally
// remains the folder door and says that it succeeded.
func TestAttachOnADirectoryRefusesOnlyOverAConnection(t *testing.T) {
	hosted, far, _ := fileLab(t, "devbox", map[string]int{"folder/inside.txt": 8})
	before := len(far.places)
	hosted.attachFilePath("folder")
	if got := plain(lastNote(t, hosted)); got != folderRemoteWord {
		t.Fatalf("the hosted attach said %q", got)
	}
	if len(far.places) != before {
		t.Fatalf("the far conversation gained %+v", far.places[before:])
	}

	local, here, dir := fileLab(t, "", map[string]int{"folder/inside.txt": 8})
	local.attachFilePath("folder")
	want := filepath.Join(dir, "folder")
	if len(here.places) != 1 || here.places[0].Path != want {
		t.Fatalf("the local conversation gained %+v, want %s", here.places, want)
	}
	if got := plain(lastNote(t, local)); got != folderChoseWord+want {
		t.Fatalf("the local attach said %q", got)
	}
}

// AN OWNED CONVERSATION OPENS THE CHOOSER ON THE WINDOW'S DIRECTORY, not on
// aforge's own work and journal files. A folder the conversation already holds
// still wins the ladder ahead of that directory.
func TestAnOwnedConversationChoosesFromTheWindowsFolder(t *testing.T) {
	root := t.TempDir()
	window := filepath.Join(root, "person")
	state := filepath.Join(root, ".aforge", "v3", "projects", "encoded", "id", "work")
	held := filepath.Join(root, "held")
	for _, dir := range []string{window, state, held} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(window, "mine.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"meta.json", "presence.json", "transcript.jsonl"} {
		if err := os.WriteFile(filepath.Join(state, name), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(window)

	agent := &fakeAgent{model: "m"}
	a := newApp(t.Context(), Options{Agent: agent, Workspace: state, Owned: true})
	a.width, a.height = 140, 34
	a.pal = newPalette(tokens.ANSI256, false)
	a.entries = nil
	a.folderStoreRead = true
	a.touch()

	settleFolder(t, a, a.openFolderPick(""))
	if a.folder.cols.dir != window {
		t.Fatalf("the chooser opened on %s, want the window's %s", a.folder.cols.dir, window)
	}
	drawn := strings.Join(append(append([]string{}, a.folder.cols.here.names...), fileNames(a.folder.cols.here.files)...), ",")
	if strings.Contains(drawn, "meta.json") || !strings.Contains(drawn, "mine.txt") {
		t.Fatalf("the first rows are %q", drawn)
	}

	a.folder.close()
	agent.places = []session.PlaceRef{{Path: held, Arrival: session.PlaceSaid}}
	settleFolder(t, a, a.openFolderPick(""))
	if a.folder.cols.dir != held {
		t.Fatalf("a held folder lost the ladder to %s", a.folder.cols.dir)
	}
}

// fileNames is the names from one folder listing's file half.
func fileNames(files []folderFile) []string {
	out := make([]string, 0, len(files))
	for _, file := range files {
		out = append(out, file.name)
	}
	return out
}

// ONE DECISION CHOOSES EVERY FOLDER REFUSAL. A connection is the more specific
// fact, a local conversation with no door says what it lacks, and a local
// conversation with a door may take the folder.
func TestOnePlaceChoosesTheMostSpecificFolderRefusal(t *testing.T) {
	hosted, _, _ := fileLab(t, "devbox", nil)
	if got := hosted.placeRefusal(); got != folderRemoteWord {
		t.Fatalf("the connection answered %q", got)
	}

	doorless, _, _ := fileLab(t, "", nil)
	doorless.agent = doorlessAgent{doorless.agent}
	if got := doorless.placeRefusal(); got != folderNoDoorWord {
		t.Fatalf("the doorless conversation answered %q", got)
	}

	local, _, _ := fileLab(t, "", nil)
	if got := local.placeRefusal(); got != "" {
		t.Fatalf("the local folder door answered %q", got)
	}
}
