package tui3

import (
	"strings"
	"testing"
)

func TestFoldersPanelDrawsTheEmptyWhisper(t *testing.T) {
	a := newLiveLab(t).open()
	a.folders = nil
	a.readHomeFolders()
	a.home.build()
	frame := homeText(a)
	if !strings.Contains(frame, "folders") {
		t.Fatalf("home has no folders heading:\n%s", frame)
	}
	if !strings.Contains(frame, folderWhisperWord) {
		t.Fatalf("empty folders whisper is not %q:\n%s", folderWhisperWord, frame)
	}
	if strings.Contains(frame, "no folders yet") {
		t.Fatalf("empty folders broke the emptiness law:\n%s", frame)
	}
	if strings.Contains(folderWhisperWord, "…") || strings.Contains(folderWhisperWord, "...") {
		t.Fatal("the folders whisper carries an ellipsis")
	}
}

func TestFoldersPanelSaysAlsoInOnADualPlacement(t *testing.T) {
	lab := newLiveLab(t)
	a := lab.open()
	a.folders = billingSecurityFolders()
	a.home.folderOpen = "col-billing"
	a.readHomeFolders()
	a.home.build()
	frame := homeText(a)
	if !strings.Contains(frame, "also in Security") {
		t.Fatalf("dual placement did not say also in Security:\n%s", frame)
	}
	if !strings.Contains(frame, folderBackWord) {
		t.Fatalf("drilled-in folder has no back row:\n%s", frame)
	}
}

func TestFoldersPanelIsSequentialAtEightyColumns(t *testing.T) {
	lab := newLiveLab(t)
	a := lab.open()
	a.width, a.height = 80, 40
	a.folders = billingSecurityFolders()
	a.readHomeFolders()
	a.home.build()
	homeText(a)
	if got := homeGridCols(a.width); got != 1 {
		t.Fatalf("80-col home has %d columns, want 1", got)
	}
	a.pointFolderID("col-billing")
	a.enterFolder("col-billing")
	homeText(a)
	if a.home.cols != 1 {
		t.Fatalf("drilling in at 80 columns grew %d columns", a.home.cols)
	}
	if homeGridCols(a.width) != 1 {
		t.Fatal("80-col sequential browse used a triple column")
	}
	frame := homeText(a)
	if !strings.Contains(frame, folderBackWord) {
		t.Fatalf("80-col drill-in has no back row:\n%s", frame)
	}
	if strings.Count(frame, "folders") > 4 && a.home.cols > 1 {
		t.Fatalf("80-col folders browse is not sequential:\n%s", frame)
	}
	a.leaveFolder()
	if a.home.folderOpen != "" {
		t.Fatalf("esc back left folderOpen %q", a.home.folderOpen)
	}
}

func TestFoldersPanelDoesNotReadTheStoreInView(t *testing.T) {
	lab := newLiveLab(t)
	fake := billingSecurityFolders()
	a := lab.open()
	a.folders = fake
	a.readHomeFolders()
	a.home.build()
	homeText(a)
	reads := fake.reads
	fake.freeze()
	homeText(a)
	a.home.move(1)
	homeText(a)
	if fake.reads != reads {
		t.Fatalf("View or a cursor move read Folders (%d -> %d)", reads, fake.reads)
	}
}

func TestFoldersRowVerbsMatchTheContract(t *testing.T) {
	lab := newLiveLab(t)
	a := lab.open()
	a.folders = billingSecurityFolders()
	a.home.folderOpen = "col-billing"
	a.readHomeFolders()
	a.home.build()
	homeText(a)
	var member homeLine
	for _, line := range a.home.lines {
		if line.kind == homeSession && line.cell != nil && line.cell.panel == panelFolders {
			member = line
			break
		}
	}
	if member.kind == 0 {
		t.Fatal("no member row on the folders panel")
	}
	a.home.cursor = -1
	for at, line := range a.home.lines {
		if line.sameRow(member) {
			a.home.cursor = at
		}
	}
	verbs := a.folderVerbs(member)
	want := []struct {
		key  rune
		word string
	}{
		{'n', folderNewChatWord},
		{'f', folderAddHereWord},
		{'m', folderMoveWord},
		{'w', folderWhyWord},
		{'x', folderRemoveWord},
	}
	if len(verbs) != len(want) {
		t.Fatalf("got %d verbs, want %d", len(verbs), len(want))
	}
	for i, v := range want {
		if verbs[i].key != v.key || verbs[i].word != v.word {
			t.Fatalf("verb %d is %q %q, want %q %q", i, string(verbs[i].key), verbs[i].word, string(v.key), v.word)
		}
	}
}

func TestFoldersPanelListsRootFolders(t *testing.T) {
	lab := newLiveLab(t)
	a := lab.open()
	a.folders = billingSecurityFolders()
	a.readHomeFolders()
	a.home.build()
	frame := homeText(a)
	for _, name := range []string{"Billing", "Security"} {
		if !strings.Contains(frame, name) {
			t.Fatalf("root folders panel missing %q:\n%s", name, frame)
		}
	}
	if strings.Contains(frame, "Receipts") {
		t.Fatalf("Root listed a nested folder:\n%s", frame)
	}
	if strings.Contains(strings.ToLower(frame), "no folders yet") {
		t.Fatalf("a full folders panel said it was empty:\n%s", frame)
	}
}
