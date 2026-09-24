package tui3

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestHomeFolderPasteOffersOneEnterToStartThere(t *testing.T) {
	a, dir := homeDropLab(t)
	var started string
	agent := &fakeAgent{model: "test/model"}
	a.start = func(where string) (Conversation, error) {
		started = where
		return Conversation{Agent: agent, SessionFile: filepath.Join(t.TempDir(), "transcript.jsonl")}, nil
	}
	pasteText(t, a, dir)
	for i := 0; i < 3; i++ {
		a.home.build()
		if a.home.startLabel() != homeStartWord+" in "+dir {
			t.Fatal("the offer did not survive an idle rebuild")
		}
	}
	drive(t, a, key("enter"))
	if started != dir || a.at(pageHome) || len(agent.sent) != 0 {
		t.Fatalf("Enter started %q, home=%v, sent=%v", started, a.at(pageHome), agent.sent)
	}
}

func TestAnyOtherKeyDismissesTheHomeFolderPasteOffer(t *testing.T) {
	for _, press := range []string{"a", " ", "backspace", "left", "right", "up", "down", "ctrl+a", "shift+enter", "alt+p"} {
		t.Run(press, func(t *testing.T) {
			a, dir := homeDropLab(t)
			pasteText(t, a, dir)
			drive(t, a, key(press))
			if a.home.pastedProject() != "" || strings.HasPrefix(a.home.startLabel(), homeStartWord+" in ") {
				t.Fatal("a non-Enter key kept the folder offer active")
			}
			// Returning to the same text cannot infer a new offer from it.
			a.home.box.setText(dir)
			a.home.build()
			if strings.HasPrefix(a.home.startLabel(), homeStartWord+" in ") {
				t.Fatal("the old path reactivated the offer")
			}
		})
	}
}

func TestHomeFolderPasteIntoExistingTextNeverOffersAProject(t *testing.T) {
	for _, before := range []string{"use ", " ", "\n"} {
		a, dir := homeDropLab(t)
		a.home.box.setText(before)
		pasteText(t, a, dir)
		if a.home.pastedProject() != "" || a.home.box.String() != before+dir {
			t.Fatalf("pasting into %q activated the offer or lost text", before)
		}
	}
}

func TestDismissedFolderPasteSendsTheEditedTextAtTheChosenProject(t *testing.T) {
	a, dir := homeDropLab(t)
	target := t.TempDir()
	a.target.where = target
	var started string
	agent := &fakeAgent{model: "test/model"}
	a.start = func(where string) (Conversation, error) {
		started = where
		return Conversation{Agent: agent, SessionFile: filepath.Join(t.TempDir(), "transcript.jsonl")}, nil
	}
	pasteText(t, a, dir)
	drive(t, a, key(" "))
	pasteText(t, a, "explain this project")
	want := dir + " explain this project"
	if got := a.home.startLabel(); !strings.HasPrefix(got, homeStartWord+": ") {
		t.Fatalf("edited folder text was not an ordinary message: %q", got)
	}
	drive(t, a, key("enter"))
	if started != target || len(agent.sent) != 1 || agent.sent[0] != want {
		t.Fatalf("started %q and sent %v, want %q in %q", started, agent.sent, want, target)
	}
}

func TestHomeFolderPasteRearmsOnlyAfterClearingAndPasting(t *testing.T) {
	a, dir := homeDropLab(t)
	pasteText(t, a, dir)
	pasteText(t, a, " more")
	if a.home.pastedProject() != "" {
		t.Fatal("a second paste kept the offer active")
	}
	a.home.box.reset()
	a.home.build()
	pasteText(t, a, dir)
	if a.home.pastedProject() != dir {
		t.Fatal("a fresh paste into an empty box did not offer its project")
	}
}

func TestTypingAFolderPathDoesNotOfferAProject(t *testing.T) {
	a, dir := homeDropLab(t)
	typeHome(a, dir)
	if a.home.pastedProject() != "" || strings.HasPrefix(a.home.startLabel(), homeStartWord+" in ") {
		t.Fatal("typing a path activated the paste-only offer")
	}
}
