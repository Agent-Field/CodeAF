package tui3

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
	"github.com/Agent-Field/aforge-v2/internal/workspaceview"
)

// ── THE FOLDERS PLACE, AS A PERSON MEETS IT ─────────────────────────────────
//
// Every test here drives keys and presses into a surface over a lab machine and
// reads the frame back. The folders themselves come from a seam standing in for
// the engine, because what is under test is the place — browsing, returning, the
// inspector, the doors — and the engine's readings are held to their own laws in
// internal/workspaceview and internal/remote.

const (
	collectionLabShared = "cccc000000000001"
	collectionLabRoad   = "cccc000000000002"
	collectionLabStray  = "cccc000000000003"
)

// collectionLab is the demo's shape in miniature: Startup holding Product and
// Marketing, one chat filed in both, a finite piece of work, ongoing work placed
// in Product, ongoing work both filed and placed in Marketing, a file, and a chat
// filed nowhere.
type collectionLab struct {
	a      *app
	pages  map[string]workspaceview.FolderPage
	items  int
	files  int
	opens  []string
	shared string
	spec   string
}

func newCollectionLab(t *testing.T, width int) *collectionLab {
	t.Helper()
	lab := newHomeLab(t)
	now := time.Now()
	startup := lab.workspace("startup")
	shared := lab.session("-startup", collectionLabShared, "pricing and positioning", startup, now.Add(-time.Hour))
	lab.session("-startup", collectionLabRoad, "roadmap notes", startup, now.Add(-2*time.Hour))
	stray := lab.session("-startup", collectionLabStray, "tar flags", startup, now.Add(-3*time.Hour))
	lab.task("-startup", session.TaskIndexEntry{ID: "1", Title: "draft the onboarding faq", Label: "draft the onboarding faq",
		Status: string(session.TaskDone), SessionID: collectionLabRoad, EndedAt: now.Add(-90 * time.Minute)})
	spec := filepath.Join(startup, "product", "spec.md")
	if err := os.MkdirAll(filepath.Dir(spec), 0o700); err != nil {
		t.Fatal(err)
	}

	f := &collectionLab{shared: shared, spec: spec}
	folder := func(id, name string) workspace.ResolvedRef {
		return workspace.ResolvedRef{Ref: workspace.Ref{Kind: workspace.CollectionKind, ID: id}, Title: name, Available: true}
	}
	chat := func(id, title string) workspace.ResolvedRef {
		return workspace.ResolvedRef{Ref: workspace.Ref{Kind: workspace.ConversationKind, ID: id}, Title: title, Location: startup, Available: true}
	}
	standingRow := func(id, title string) workspace.ResolvedRef {
		return workspace.ResolvedRef{Ref: workspace.Ref{Kind: workspace.StandingKind, ID: id}, Title: title, State: "active", Location: startup, Available: true}
	}
	f.pages = map[string]workspaceview.FolderPage{
		"": {Rows: []workspaceview.FolderRow{{ResolvedRef: folder("startup", "Startup")}}},
		"startup": {Folder: workspace.Collection{ID: "startup", Name: "Startup"}, Rows: []workspaceview.FolderRow{
			{ResolvedRef: folder("product", "Product"), Filed: true},
			{ResolvedRef: folder("marketing", "Marketing"), Filed: true},
		}},
		"product": {Folder: workspace.Collection{ID: "product", Name: "Product"}, Rows: []workspaceview.FolderRow{
			{ResolvedRef: chat(collectionLabRoad, "roadmap notes"), Filed: true},
			{ResolvedRef: chat(collectionLabShared, "pricing and positioning"), Filed: true},
			{ResolvedRef: workspace.ResolvedRef{Ref: workspace.Ref{Kind: workspace.TaskKind, ID: "1", SessionID: collectionLabRoad},
				Title: "draft the onboarding faq", State: "done", Available: true}, Filed: true},
			{ResolvedRef: workspace.ResolvedRef{Ref: workspace.Ref{Kind: workspace.ArtifactKind, ID: spec},
				Title: "spec.md", Location: filepath.Dir(spec), Available: true}, Filed: true},
			{ResolvedRef: standingRow("digest", "keep the product digest current"), Placed: true},
			{ResolvedRef: workspace.ResolvedRef{Ref: workspace.Ref{Kind: workspace.ConversationKind, ID: "dddd000000000009"},
				Unavailable: "That conversation is not on this machine."}, Filed: true},
		}},
		"marketing": {Folder: workspace.Collection{ID: "marketing", Name: "Marketing"}, Rows: []workspaceview.FolderRow{
			{ResolvedRef: chat(collectionLabShared, "pricing and positioning"), Filed: true},
			{ResolvedRef: standingRow("review", "review the launch copy"), Filed: true, Placed: true},
		}},
	}

	a := lab.app(stray)
	a.width, a.height = width, 30
	a.foldersArm = func(gen int) tea.Cmd {
		return func() tea.Msg { return foldersItemTickMsg{gen: gen} }
	}
	a.collections = CollectionSeam{
		Page: func(_ context.Context, id string, _ int) (workspaceview.FolderPage, error) {
			page, ok := f.pages[id]
			if !ok {
				return workspaceview.FolderPage{}, workspace.ErrNotFound
			}
			return page, nil
		},
		Item: func(_ context.Context, ref workspace.Ref) (workspaceview.FolderItem, error) {
			f.items++
			item := workspaceview.FolderItem{Ref: ref}
			if ref.ID == collectionLabShared {
				item.FiledIn = []workspace.Collection{{ID: "product", Name: "Product"}, {ID: "marketing", Name: "Marketing"}}
			}
			return item, nil
		},
		File: func(_ context.Context, path string) (workspaceview.ArtifactPreview, error) {
			f.files++
			return workspaceview.ArtifactPreview{Path: path, Size: 40, Text: "# Product spec\n- Offline support: desktop\n"}, nil
		},
	}
	opened := a.open
	a.open = func(workspace, transcript string) (Conversation, error) {
		f.opens = append(f.opens, transcript)
		return opened(workspace, transcript)
	}
	f.a = a
	return f
}

// walk opens the place and walks the named folders from the top.
func (f *collectionLab) walk(t *testing.T, names ...string) {
	t.Helper()
	drive(t, f.a, runCmd(f.a.showPage(pageFolders))...)
	for _, name := range names {
		f.pick(t, name)
		drive(t, f.a, key("enter"))
	}
}

// pick puts the cursor on the row with this title.
func (f *collectionLab) pick(t *testing.T, title string) {
	t.Helper()
	for i, row := range f.a.browse.page.Rows {
		if row.Title == title {
			for f.a.browse.cursor < i {
				drive(t, f.a, key("down"))
			}
			for f.a.browse.cursor > i {
				drive(t, f.a, key("up"))
			}
			return
		}
	}
	t.Fatalf("no row %q in %+v", title, f.a.browse.page.Rows)
}

func (f *collectionLab) frame() string { return placeFrameText(f.a) }

// WALKING IN, OUT, AWAY AND BACK LANDS WHERE YOU WERE. The one thing a folder is
// for is being returned to, so the walk and the row chosen in each folder survive
// the place closing.
func TestTheFoldersPlaceKeepsThePathAndTheRowAcrossLeavingIt(t *testing.T) {
	f := newCollectionLab(t, 120)
	f.walk(t, "Startup", "Product")
	f.pick(t, "pricing and positioning")
	screen := f.frame()
	for _, want := range []string{"folders / Startup / Product", "roadmap notes", "pricing and positioning", "ongoing work", "placed"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the Product page does not say %q:\n%s", want, screen)
		}
	}

	drive(t, f.a, key("esc"))
	if f.a.pageShowing() {
		t.Fatal("esc did not leave the folders place")
	}
	drive(t, f.a, runCmd(f.a.showPage(pageFolders))...)
	row, ok := f.a.browse.selected()
	if !ok || row.Ref.ID != collectionLabShared || f.a.browse.current() != "product" {
		t.Fatalf("coming back landed on %+v in %q", row, f.a.browse.current())
	}

	drive(t, f.a, key("left"))
	if f.a.browse.current() != "startup" {
		t.Fatalf("← walked to %q, not back out to Startup", f.a.browse.current())
	}
	if row, _ := f.a.browse.selected(); row.Ref.ID != "product" {
		t.Fatalf("walking back out landed on %+v, not the folder it came from", row)
	}
}

// ONE CHAT, TWO FOLDERS, ONE CONVERSATION. Opening the shared chat from Product
// and then from Marketing reaches the same transcript, and the second door brings
// the conversation this window already holds forward rather than opening a copy.
// Coming back lands in the folder it was opened from.
func TestTheSharedChatOpensAsOneConversationFromEitherFolder(t *testing.T) {
	f := newCollectionLab(t, 120)
	f.walk(t, "Startup", "Product")
	f.pick(t, "pricing and positioning")
	drive(t, f.a, key("enter"))
	if f.a.pageShowing() || f.a.file != f.shared {
		t.Fatalf("enter on the shared chat left the window on page %v, file %q", f.a.page, f.a.file)
	}

	drive(t, f.a, runCmd(f.a.showPage(pageFolders))...)
	if row, _ := f.a.browse.selected(); f.a.browse.current() != "product" || row.Ref.ID != collectionLabShared {
		t.Fatalf("coming back from the chat landed on %+v in %q", row, f.a.browse.current())
	}
	drive(t, f.a, key("left"))
	f.pick(t, "Marketing")
	drive(t, f.a, key("enter"))
	f.pick(t, "pricing and positioning")
	drive(t, f.a, key("enter"))
	if f.a.pageShowing() || f.a.file != f.shared {
		t.Fatalf("the shared chat from Marketing opened %q", f.a.file)
	}
	if len(f.opens) != 1 || f.opens[0] != f.shared {
		t.Fatalf("the two doors opened %v; one conversation should have been opened once", f.opens)
	}

	drive(t, f.a, runCmd(f.a.showPage(pageFolders))...)
	if row, _ := f.a.browse.selected(); f.a.browse.current() != "marketing" || row.Ref.ID != collectionLabShared {
		t.Fatalf("coming back landed on %+v in %q, not where the chat was opened from", row, f.a.browse.current())
	}
}

// A WIDE WINDOW RESERVES THE INSPECTOR AND THE LIST DOES NOT MOVE. The column's
// width is the window's alone, so the rows sit in the same cells whichever row
// is selected, and what the inspector says changes with the selection.
func TestAWideWindowDrawsTheInspectorBesideAListThatDoesNotMove(t *testing.T) {
	f := newCollectionLab(t, 120)
	f.walk(t, "Startup", "Product")
	f.pick(t, "roadmap notes")
	first := f.frame()
	f.pick(t, "pricing and positioning")
	second := f.frame()
	if screenColumn(first, "spec.md") != screenColumn(second, "spec.md") || screenColumn(first, "spec.md") < 0 {
		t.Fatalf("the list moved when the selection did:\n%s\n---\n%s", first, second)
	}
	if !strings.Contains(second, "Chat") || !strings.Contains(second, "Product · Marketing") {
		t.Fatalf("the inspector does not say where the shared chat is filed:\n%s", second)
	}
	if !strings.Contains(second, "filed here") {
		t.Fatalf("the inspector does not say how the chat is bound to this folder:\n%s", second)
	}
	f.pick(t, "keep the product digest current")
	if screen := f.frame(); !strings.Contains(screen, "placed here · this folder's rules") || !strings.Contains(screen, "reach it") {
		t.Fatalf("placed work does not say this folder's rules reach it:\n%s", screen)
	}
}

// A NARROW WINDOW HAS NO COLUMN AND A DETAIL VIEW A KEY AWAY. `→` shows what the
// row can do, `d` reads it across the page, and esc comes back to the list with
// the cursor where it was.
func TestANarrowWindowReadsTheDetailOnItsOwnPage(t *testing.T) {
	f := newCollectionLab(t, 60)
	f.walk(t, "Startup", "Product")
	f.pick(t, "pricing and positioning")
	screen := f.frame()
	if strings.Contains(screen, "Product · Marketing") {
		t.Fatalf("a narrow window drew the inspector beside the list:\n%s", screen)
	}
	// AND THE NARROW ROW KEEPS `placed`, giving up the record's state instead,
	// and the foot keeps the verbs that are the only road to the details.
	for _, want := range []string{"ongoing work · placed", "→ verbs"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("a narrow window lost %q:\n%s", want, screen)
		}
	}
	drive(t, f.a, key("right"), key("d"))
	if !f.a.browse.detail {
		t.Fatal("→ d did not open the detail view")
	}
	if screen := f.frame(); !strings.Contains(screen, "Product · Marketing") {
		t.Fatalf("the detail view does not say where the chat is filed:\n%s", screen)
	}
	drive(t, f.a, key("esc"))
	if row, _ := f.a.browse.selected(); f.a.browse.detail || !f.a.pageShowing() || row.Ref.ID != collectionLabShared {
		t.Fatalf("esc from the detail view left detail=%v page=%v row=%+v", f.a.browse.detail, f.a.page, row)
	}
}

// WORK OPENS THROUGH ITS OWNER. Ongoing work opens on the standing place; a
// finite piece of work opens its record on the tasks place.
func TestWorkOpensOnThePlaceThatOwnsIt(t *testing.T) {
	f := newCollectionLab(t, 120)
	f.walk(t, "Startup", "Product")
	f.pick(t, "keep the product digest current")
	drive(t, f.a, key("enter"))
	if !f.a.at(pageStanding) {
		t.Fatalf("ongoing work opened page %v, not the standing place", f.a.page)
	}

	drive(t, f.a, runCmd(f.a.showPage(pageFolders))...)
	f.pick(t, "draft the onboarding faq")
	drive(t, f.a, key("enter"))
	if !f.a.at(pageTasks) || !f.a.taskSheet.detailOn || f.a.taskSheet.detail.ID != "1" {
		t.Fatalf("finite work opened page %v with record %+v", f.a.page, f.a.taskSheet.detail)
	}
}

// A FILE IS PREVIEWED FROM THE ENGINE'S READING, and a missing record keeps its
// row and says why.
func TestAnArtifactIsPreviewedAndAMissingRecordSaysSo(t *testing.T) {
	f := newCollectionLab(t, 120)
	f.walk(t, "Startup", "Product")
	f.pick(t, "spec.md")
	if screen := f.frame(); !strings.Contains(screen, "Offline support: desktop") || !strings.Contains(screen, "Artifact") {
		t.Fatalf("the inspector does not preview the file:\n%s", screen)
	}
	if f.files == 0 {
		t.Fatal("the preview did not come from the seam")
	}
	drive(t, f.a, key("enter"))
	if !f.a.browse.detail || !strings.Contains(f.frame(), "# Product spec") {
		t.Fatalf("enter on a file did not preview it across the page:\n%s", f.frame())
	}
	drive(t, f.a, key("esc"), key("down"), key("down"))
	row, _ := f.a.browse.selected()
	if row.Available {
		t.Fatalf("the cursor is on %+v, not the missing chat", row)
	}
	screen := f.frame()
	if !strings.Contains(screen, "missing") || !strings.Contains(screen, "That conversation is not on this") {
		t.Fatalf("a missing record does not say so:\n%s", screen)
	}
}

// AN ANSWER FOR A FOLDER ALREADY LEFT IS DROPPED, and so is an answer about a row
// the cursor has moved off.
func TestAStaleFolderAnswerIsNeverDrawn(t *testing.T) {
	f := newCollectionLab(t, 120)
	f.walk(t, "Startup")
	stale := f.a.browse.gen
	drive(t, f.a, key("enter")) // into Product
	f.a.foldersPageLanded(foldersPageMsg{gen: stale, id: "startup", page: f.pages["startup"]})
	if f.a.browse.current() != "product" || f.a.browse.page.Folder.ID != "product" {
		t.Fatalf("an old answer for Startup was drawn over Product: %+v", f.a.browse.page.Folder)
	}
	f.pick(t, "roadmap notes")
	f.a.foldersItemLanded(foldersItemMsg{gen: f.a.browse.itemGen, ref: workspace.Ref{Kind: workspace.ConversationKind, ID: collectionLabShared},
		item: workspaceview.FolderItem{FiledIn: []workspace.Collection{{Name: "Elsewhere"}}}})
	if strings.Contains(f.frame(), "Elsewhere") {
		t.Fatal("a reading about another row was drawn beside this one")
	}
}

// A REFRESH THAT FAILS KEEPS THE FOLDER IT HAD, AND A SURFACE WITH NO SEAM SAYS
// SO. Neither is ever drawn as a folder with nothing in it.
func TestAFailedOrAbsentReadingIsSaidAndNeverAnEmptyFolder(t *testing.T) {
	f := newCollectionLab(t, 120)
	f.walk(t, "Startup", "Product")
	f.a.foldersPageLanded(foldersPageMsg{gen: f.a.browse.gen, id: "product", err: errors.New("engine: the link dropped")})
	screen := f.frame()
	if !strings.Contains(screen, "roadmap notes") || !strings.Contains(screen, foldersFailedWord) {
		t.Fatalf("a failed refresh emptied the folder or said nothing:\n%s", screen)
	}

	bare := newCollectionLab(t, 120)
	bare.a.collections = CollectionSeam{}
	drive(t, bare.a, runCmd(bare.a.showPage(pageFolders))...)
	if screen := bare.frame(); !strings.Contains(screen, "Folders cannot be read here") {
		t.Fatalf("a surface with no folders seam did not say so:\n%s", screen)
	}
}

// A PRESS SELECTS AND NEVER OPENS, and the row it lands on is read closely.
func TestAPressSelectsARowAndReadsItWithoutOpeningIt(t *testing.T) {
	f := newCollectionLab(t, 120)
	f.walk(t, "Startup", "Product")
	f.frame()
	before := f.items
	line := -1
	for i, at := range f.a.browse.owner {
		if at == 1 {
			line = i
		}
	}
	if line < 0 {
		t.Fatalf("the second row was not drawn: %v", f.a.browse.owner)
	}
	cmd, took := f.a.placeBodyPress(line + placeHeadRows)
	if !took || f.a.browse.cursor != 1 || !f.a.pageShowing() {
		t.Fatalf("a press on row 1 took=%v cursor=%d page=%v", took, f.a.browse.cursor, f.a.page)
	}
	drive(t, f.a, runCmd(cmd)...)
	if f.items == before {
		t.Fatal("the pressed row was never read closely")
	}
}

// NOTHING ABOUT THE WALK FILES A NEW CHAT. Words typed on this page start a
// conversation where every new conversation starts, and the page is left.
func TestWordsTypedOnTheFoldersPlaceStartAChatThatIsNotFiled(t *testing.T) {
	f := newCollectionLab(t, 120)
	f.walk(t, "Startup", "Product")
	started := ""
	f.a.start = func(workspace string) (Conversation, error) {
		started = workspace
		return Conversation{Agent: &switchAgent{fakeAgent: &fakeAgent{model: "m"}},
			SessionFile: filepath.Join(t.TempDir(), "transcript.jsonl"), Workspace: workspace}, nil
	}
	typeInto(t, f.a, "hello")
	drive(t, f.a, key("enter"))
	if f.a.pageShowing() {
		t.Fatal("enter with words in the box did not start a conversation")
	}
	if strings.Contains(started, "Product") || strings.Contains(started, "product") {
		t.Fatalf("the new conversation was started in %q, which names the folder being browsed", started)
	}
}

// column is where a string first appears on the frame's lines, or -1.
func screenColumn(screen, needle string) int {
	for _, line := range strings.Split(screen, "\n") {
		if at := strings.Index(line, needle); at >= 0 {
			return len([]rune(line[:at]))
		}
	}
	return -1
}
