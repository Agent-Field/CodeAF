package tui3

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/manual"
)

// folders_journey_test.go is the J01–J08 command harness that compiles against
// current tui3. Product logic for the panel lives on the TUI lane.
//
// Options.Folders is the frozen interface (CONTRACTS.md). This worktree may
// not have that field yet. Skip the slash-table assertion until it exists;
// after integrate (storage → service → wiring → tui → proof) this test must
// run and require /folders as its own row. RECORD: if it still skips after
// that apply order, TUI did not land Options.Folders. If it runs and fails,
// /folders is missing from commands.go or was aliased to /folder.

func foldersInterfacePresent() bool {
	_, ok := reflect.TypeOf(Options{}).FieldByName("Folders")
	return ok
}

func TestFoldersIsRegisteredAndIsNotAnAliasOfFolder(t *testing.T) {
	if !foldersInterfacePresent() {
		t.Skip("Options.Folders is not on this branch; TUI lane owns the interface. After integrate this test must run.")
	}
	var foldersRows int
	for _, c := range commands {
		if c.name == "folders" {
			foldersRows++
		}
		for _, word := range c.alias {
			if word == "folders" && c.name == "folder" {
				t.Fatal("/folders must not be an alias of /folder")
			}
		}
	}
	if foldersRows == 0 {
		t.Fatal("commands.go must register /folders as its own row, not an alias of /folder")
	}
	if got := canonicalCommand("folders"); got != "folders" {
		t.Fatalf("canonicalCommand(\"folders\") = %q; /folders is not /folder", got)
	}
	if canonicalCommand("place") != "folder" || canonicalCommand("dir") != "folder" {
		t.Fatal("/place and /dir must still reach /folder")
	}
	if err := checkCommands(commands); err != nil {
		t.Fatalf("the command table is broken: %v", err)
	}
}

func TestFoldersJourneyFrozenNamesStandInTheManual(t *testing.T) {
	if !manual.Chat().Mentions("logical groups of chats · /folders create Billing") {
		t.Fatal("the folders emptiness whisper is not in the chat manual")
	}
	if !manual.Chat().Mentions("/folders create") || !manual.Chat().Mentions("/folders add") {
		t.Fatal("the chat manual does not mention /folders create|add")
	}
	if !manual.Chat().Mentions("`n` new chat here") {
		t.Fatal("the chat manual does not quote the folders verb strip")
	}
}
