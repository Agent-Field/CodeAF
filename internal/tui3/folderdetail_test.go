package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestFolderInstructionsSection(t *testing.T) {
	fake := billingSecurityFolders()
	fake.root.Folders[0].Purpose = "invoices and receipts"
	a := newLiveLab(t).open()
	a.folders = fake
	a.readHomeFolders()
	a.home.build()
	homeText(a)
	a.enterFolder("col-billing")
	frame := homeText(a)
	if !strings.Contains(frame, folderInstructHead) {
		t.Fatalf("folder detail dropped the instructions heading:\n%s", frame)
	}
	if !strings.Contains(frame, folderInstructWhisper) {
		t.Fatalf("empty instructions dropped the whisper:\n%s", frame)
	}
	if strings.Contains(frame, "no instructions yet") {
		t.Fatalf("empty instructions broke the emptiness law:\n%s", frame)
	}
	if strings.Contains(frame, "invoices and receipts") {
		t.Fatalf("purpose text was drawn as an instruction:\n%s", frame)
	}

	a.width, a.height = 80, 40
	a.home.build()
	frame = homeText(a)
	if homeGridCols(a.width) != 1 {
		t.Fatalf("80-col home has %d columns", homeGridCols(a.width))
	}
	if !strings.Contains(frame, folderInstructHead) || !strings.Contains(frame, folderInstructWhisper) {
		t.Fatalf("80-col folder detail hid instructions:\n%s", frame)
	}
}

func TestEnterFolderOpensTheFoldersPanelSoMembersStayReachable(t *testing.T) {
	fake := billingSecurityFolders()
	a := newLiveLab(t).open()
	a.width, a.height = 80, 40
	a.folders = fake
	a.readHomeFolders()
	a.home.build()
	homeText(a)
	a.enterFolder("col-billing")
	if !a.home.openedOn || a.home.opened != panelFolders {
		t.Fatalf("enterFolder left folders folded openedOn=%v opened=%v", a.home.openedOn, a.home.opened)
	}
	frame := homeText(a)
	if !strings.Contains(frame, "Porting the Resume Picker") {
		t.Fatalf("80-col drill-in folded the chat member away:\n%s", frame)
	}
	if strings.Contains(frame, "2 more") && !strings.Contains(frame, "Porting the Resume Picker") {
		t.Fatalf("folder members were only a fold count:\n%s", frame)
	}
}

func TestFolderInstructCommand(t *testing.T) {
	fake := billingSecurityFolders()
	a := newLiveLab(t).open()
	a.folders = fake
	a.readHomeFolders()
	a.home.build()
	homeText(a)
	if cmd := a.runFoldersCommand("instruct Security customers must authenticate"); cmd != nil {
		t.Fatal("instruct returned a command")
	}
	if len(fake.instructs) != 1 || fake.instructs[0] != [2]string{"col-security", "customers must authenticate"} {
		t.Fatalf("instruct called %v", fake.instructs)
	}
	a.enterFolder("col-security")
	frame := homeText(a)
	if !strings.Contains(frame, folderInstructHead) {
		t.Fatalf("instructed folder lost the heading:\n%s", frame)
	}
	if !strings.Contains(frame, "customers must authenticate") {
		t.Fatalf("instructed folder hid the standing line:\n%s", frame)
	}
	if strings.Contains(frame, folderInstructWhisper) {
		t.Fatalf("a written instruction still whispered emptiness:\n%s", frame)
	}
}

func TestFolderInstructVerb(t *testing.T) {
	fake := billingSecurityFolders()
	a := newLiveLab(t).open()
	a.folders = fake
	a.readHomeFolders()
	a.home.build()
	homeText(a)
	a.home.box.setText("reject mailing raw URLs")
	row := homeLine{kind: homeFolderRow, dir: "col-billing", project: "Billing"}
	if cmd := a.instructThisFolder(row); cmd != nil {
		t.Fatal("i returned a command")
	}
	if len(fake.instructs) != 1 || fake.instructs[0] != [2]string{"col-billing", "reject mailing raw URLs"} {
		t.Fatalf("i called %v", fake.instructs)
	}
	if a.home.box.String() != "" {
		t.Fatalf("i left the instruction in the composer: %q", a.home.box.String())
	}
	a.enterFolder("col-billing")
	frame := homeText(a)
	if !strings.Contains(frame, "reject mailing raw URLs") {
		t.Fatalf("i did not draw the instruction:\n%s", frame)
	}
}

func TestFolderIndexCopy(t *testing.T) {
	if got := folderIndexCopy(FolderIndex{}); got != "" {
		t.Fatalf("caught-up empty index drew %q", got)
	}
	if got := folderIndexCopy(FolderIndex{Passages: 10, Vectors: 10}); got != "" {
		t.Fatalf("caught-up index drew %q", got)
	}
	delayed := folderIndexCopy(FolderIndex{Passages: 100, Vectors: 100, Delayed: true, Detail: "100% checked"})
	if delayed == "" {
		t.Fatal("delayed index drew nothing")
	}
	if strings.Contains(delayed, "100%") {
		t.Fatalf("delayed catch-up faked 100%%: %q", delayed)
	}
	if strings.Contains(strings.ToLower(delayed), "checked") {
		t.Fatalf("delayed index claimed checked: %q", delayed)
	}
	if !strings.Contains(delayed, folderDelayedWord) {
		t.Fatalf("delayed index hid %q: %q", folderDelayedWord, delayed)
	}
	if !strings.Contains(delayed, "100 passages") || !strings.Contains(delayed, "100 vectors") {
		t.Fatalf("delayed index dropped software counters: %q", delayed)
	}

	catchup := folderIndexCopy(FolderIndex{Passages: 10, Vectors: 9})
	if strings.Contains(catchup, "100%") {
		t.Fatalf("partial index faked 100%%: %q", catchup)
	}
	if !strings.Contains(catchup, "10 passages") || !strings.Contains(catchup, "9 vectors") {
		t.Fatalf("partial index dropped counters: %q", catchup)
	}

	degraded := folderIndexCopy(FolderIndex{Degraded: true, Passages: 4})
	if !strings.Contains(degraded, folderDegradedWord) {
		t.Fatalf("degraded index hid the label: %q", degraded)
	}
	if strings.Contains(strings.ToLower(degraded), "checked") {
		t.Fatalf("degraded index claimed checked: %q", degraded)
	}

	fake := billingSecurityFolders()
	fake.index = FolderIndex{Passages: 12, Vectors: 3, Delayed: true}
	a := newLiveLab(t).open()
	a.folders = fake
	a.readHomeFolders()
	a.home.build()
	frame := homeText(a)
	if !strings.Contains(frame, folderDelayedWord) {
		t.Fatalf("folders heading hid discovery delayed:\n%s", frame)
	}
	if strings.Contains(frame, "100%") || strings.Contains(strings.ToLower(frame), "checked") {
		t.Fatalf("folders heading claimed a finished check:\n%s", frame)
	}
}

func TestFolderOrganizerWhyHere(t *testing.T) {
	why := FolderWhy{
		Origin:   folderOrganizerOrigin,
		Reason:   "authenticated receipt links",
		Evidence: "Security passage",
		Actor:    "session-1",
		At:       "2026-09-19T00:00:00Z",
	}
	got := folderWhyLine(why)
	if !strings.HasPrefix(got, folderOrganizerOrigin+" · ") {
		t.Fatalf("organizer why-here started %q", got)
	}
	for _, want := range []string{folderOrganizerOrigin, "authenticated receipt links", "Security passage"} {
		if !strings.Contains(got, want) {
			t.Fatalf("organizer why-here %q dropped %q", got, want)
		}
	}

	fake := billingSecurityFolders()
	fake.whys = map[string]FolderWhy{"col-security/aaaa000000000001": why}
	a := newLiveLab(t).open()
	a.folders = fake
	member := homeLine{kind: homeSession, row: session.SessionRow{ID: "aaaa000000000001"}, cell: &homeCell{panel: panelFolders, key: "col-security"}}
	a.folderWhyHere(member)
	if !strings.Contains(a.home.msg, folderOrganizerOrigin) || !strings.Contains(a.home.msg, "authenticated receipt links") {
		t.Fatalf("w said %q", a.home.msg)
	}
	if askCount(a) != 0 {
		t.Fatalf("why-here raised %d approval cards", askCount(a))
	}
}

func TestFolderQuietFiling(t *testing.T) {
	fake := billingSecurityFolders()
	a := newLiveLab(t).open()
	a.folders = fake
	a.home.folderOpen = "col-security"
	a.readHomeFolders()
	a.home.build()
	homeText(a)
	if askCount(a) != 0 {
		t.Fatalf("setup already held %d questions", askCount(a))
	}
	fake.members["col-security"] = append(fake.members["col-security"], FolderPlacement{
		CollectionID: "col-security",
		RefID:        "bbbb000000000002",
		Title:        "Emailed receipt links",
	})
	a.refreshFolderMemo()
	frame := homeText(a)
	if !strings.Contains(frame, "Emailed receipt links") {
		t.Fatalf("quiet filing hid the new placement:\n%s", frame)
	}
	if askCount(a) != 0 {
		t.Fatalf("organizer filing raised %d approval cards", askCount(a))
	}
}

func TestFolderPreviewLaunchesNoAI(t *testing.T) {
	fake := billingSecurityFolders()
	a := newLiveLab(t).open()
	a.folders = fake
	a.readHomeFolders()
	a.home.build()
	homeText(a)
	a.pointFolderID("col-billing")
	instructs := len(fake.instructs)
	fake.freeze()
	homeText(a)
	a.home.hover = a.home.cursor
	homeText(a)
	a.home.move(1)
	homeText(a)
	if len(fake.instructs) != instructs {
		t.Fatalf("preview/hover instructed a folder: %v", fake.instructs)
	}
}

func TestFolderWave2VerbsKeepWave1(t *testing.T) {
	a := newLiveLab(t).open()
	a.folders = billingSecurityFolders()
	row := homeLine{kind: homeFolderRow, dir: "col-billing", project: "Billing"}
	keys := ""
	for _, v := range a.folderVerbs(row) {
		keys += string(v.key)
	}
	if keys != "nfei" {
		t.Fatalf("folder-row verbs %q, want nfei", keys)
	}
	member := homeLine{kind: homeSession, row: session.SessionRow{ID: "aaaa000000000001"}, cell: &homeCell{panel: panelFolders, key: "col-billing"}}
	keys = ""
	for _, v := range a.folderVerbs(member) {
		keys += string(v.key)
	}
	if keys != "nfmwx" {
		t.Fatalf("member verbs %q, want nfmwx", keys)
	}
}

func TestFolderDetailFunctionsStayUnderTheCeiling(t *testing.T) {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, "folderdetail.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		got := folderDetailComplexity(function.Body)
		if got > 15 {
			t.Errorf("%s is %d, ceiling is 15", function.Name.Name, got)
		}
	}
}

func folderDetailComplexity(body *ast.BlockStmt) int {
	decisions := 1
	ast.Inspect(body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
			decisions++
		case *ast.CaseClause:
			if typed.List != nil {
				decisions++
			}
		case *ast.BinaryExpr:
			if typed.Op.String() == "&&" || typed.Op.String() == "||" {
				decisions++
			}
		}
		return true
	})
	return decisions
}
