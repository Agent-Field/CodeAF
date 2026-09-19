package tui3

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
)

// fakeCollab is an in-memory Collab seam. freeze panics on any call so a
// test can prove View and a cursor move never read the store.
type fakeCollab struct {
	mu             sync.Mutex
	frozen         bool
	marks          []CollabMark
	activity       []CollabActivity
	participants   []CollabParticipant
	coordinated    []string
	markErr        error
	coordErr       error
	markedErr      error
	activityErr    error
	participantErr error
	reads          int
}

func (f *fakeCollab) freeze() { f.mu.Lock(); f.frozen = true; f.mu.Unlock() }

func (f *fakeCollab) touch(op string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.frozen {
		panic("Collab." + op + " called after freeze (View or cursor must not read the store)")
	}
	f.reads++
}

func (f *fakeCollab) Mark(_ context.Context, refID string) error {
	f.touch("Mark")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.markErr != nil {
		return f.markErr
	}
	for _, mark := range f.marks {
		if mark.RefID == refID {
			return nil
		}
	}
	f.marks = append(f.marks, CollabMark{RefID: refID})
	return nil
}

func (f *fakeCollab) Unmark(_ context.Context, refID string) error {
	f.touch("Unmark")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.markErr != nil {
		return f.markErr
	}
	kept := f.marks[:0]
	for _, mark := range f.marks {
		if mark.RefID != refID {
			kept = append(kept, mark)
		}
	}
	f.marks = kept
	return nil
}

func (f *fakeCollab) Marked(context.Context) ([]CollabMark, error) {
	f.touch("Marked")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.markedErr != nil {
		return nil, f.markedErr
	}
	return append([]CollabMark(nil), f.marks...), nil
}

func (f *fakeCollab) CoordinateMarked(_ context.Context, coordinatorID string) error {
	f.touch("CoordinateMarked")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.coordErr != nil {
		return f.coordErr
	}
	f.coordinated = append(f.coordinated, coordinatorID)
	return nil
}

func (f *fakeCollab) Activity(context.Context, string) ([]CollabActivity, error) {
	f.touch("Activity")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.activityErr != nil {
		return nil, f.activityErr
	}
	return append([]CollabActivity(nil), f.activity...), nil
}

func (f *fakeCollab) Participants(context.Context, string) ([]CollabParticipant, error) {
	f.touch("Participants")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.participantErr != nil {
		return nil, f.participantErr
	}
	return append([]CollabParticipant(nil), f.participants...), nil
}

func collabLab(t *testing.T) (*app, *fakeCollab) {
	t.Helper()
	fake := &fakeCollab{}
	a := newLiveLab(t).open()
	a.collab = fake
	a.folders = billingSecurityFolders()
	a.readHomeFolders()
	a.readCollab()
	a.home.build()
	homeText(a)
	return a, fake
}

func TestCoordinateFromAnExistingChat(t *testing.T) {
	a, fake := collabLab(t)
	if got := a.conversationRef(); got != "aaaa000000000001" {
		t.Fatalf("existing chat id %q, want aaaa000000000001", got)
	}
	member := homeLine{kind: homeSession, row: session.SessionRow{ID: "aaaa000000000002"}, cell: &homeCell{panel: panelFolders, key: "col-billing"}}
	if cmd := a.toggleCollabMark(member); cmd != nil {
		t.Fatal("mark returned a command")
	}
	if cmd := a.coordinateMarked(); cmd != nil {
		t.Fatal("coordinate returned a command")
	}
	if len(fake.coordinated) != 1 || fake.coordinated[0] != "aaaa000000000001" {
		t.Fatalf("coordinated %v, want the existing chat", fake.coordinated)
	}
}

func TestCoordinateMarkIsOptional(t *testing.T) {
	a, _ := collabLab(t)
	row := homeLine{kind: homeFolderRow, dir: "col-billing", project: "Billing"}
	for _, v := range a.folderVerbs(row) {
		if v.key == 'c' || v.word == collabCoordinateWord {
			t.Fatal("coordinate chrome appeared before anything was marked; marking is optional")
		}
	}
	member := homeLine{kind: homeSession, row: session.SessionRow{ID: "aaaa000000000001"}, cell: &homeCell{panel: panelFolders, key: "col-billing"}}
	keys := ""
	for _, v := range a.folderVerbs(member) {
		keys += string(v.key)
	}
	if keys != "nfmwxk" {
		t.Fatalf("unmarked member verbs %q, want nfmwxk (mark convenience only)", keys)
	}
}

func TestCoordinateRequestReplySentWithSource(t *testing.T) {
	a, fake := collabLab(t)
	fake.activity = []CollabActivity{
		{Kind: collabRequestWord, SourceRef: "Feature A", Body: "how's progress"},
		{Kind: collabReplyWord, SourceRef: "Feature A", Body: "waiting on tests"},
		{Kind: collabSentWord, ToTitle: "Feature B", Body: "use the new interface"},
		{Kind: "accepted", SourceRef: "hidden", Body: "must not paint"},
		{Kind: "recorded", Body: "must not paint"},
		{Kind: "processed", Body: "must not paint"},
	}
	a.closeHome()
	a.readCollab()
	a.touch()
	got := plain(frame(a))
	for _, want := range []string{collabRequestWord, collabReplyWord, collabSentWord, collabSourceWord, "Feature A", "Feature B", "how's progress", "waiting on tests", "use the new interface"} {
		if !strings.Contains(got, want) {
			t.Fatalf("activity hid %q:\n%s", want, got)
		}
	}
	for _, banned := range []string{"accepted", "recorded", "processed", "must not paint"} {
		if strings.Contains(got, banned) {
			t.Fatalf("activity painted machinery %q:\n%s", banned, got)
		}
	}
}

func TestParticipantLabelsOnANormalChat(t *testing.T) {
	a, fake := collabLab(t)
	fake.participants = []CollabParticipant{
		{ActorID: "act-1", Role: "planner", SourceTitle: "Feature B"},
		{ActorID: "act-2", Role: "critic", SourceTitle: "Feature D"},
	}
	a.closeHome()
	a.readCollab()
	a.touch()
	got := plain(frame(a))
	if !strings.Contains(got, "planner") || !strings.Contains(got, "Feature B") {
		t.Fatalf("participant labels missing planner:\n%s", got)
	}
	if !strings.Contains(got, "critic") || !strings.Contains(got, "Feature D") {
		t.Fatalf("participant labels missing critic:\n%s", got)
	}
	if strings.Contains(got, "0 participants") || strings.Contains(got, "group chat") || strings.Contains(got, "manager") {
		t.Fatalf("joint discussion grew product chrome:\n%s", got)
	}
}

func TestParticipantEmptyListDrawsNothing(t *testing.T) {
	a, _ := collabLab(t)
	a.closeHome()
	a.readCollab()
	a.touch()
	got := strings.ToLower(plain(frame(a)))
	if strings.Contains(got, "0 participants") || strings.Contains(got, "participants") {
		t.Fatalf("empty participants broke the emptiness law:\n%s", got)
	}
}

func TestFoldersCoordinateIsSequentialAtEightyColumns(t *testing.T) {
	a, fake := collabLab(t)
	a.width, a.height = 80, 40
	fake.marks = []CollabMark{{RefID: "aaaa000000000001", Title: "Porting the Resume Picker"}}
	a.readCollab()
	a.home.build()
	homeText(a)
	if got := homeGridCols(a.width); got != 1 {
		t.Fatalf("80-col home has %d columns, want 1", got)
	}
	a.pointFolderID("col-billing")
	a.enterFolder("col-billing")
	home := homeText(a)
	if a.home.cols != 1 || homeGridCols(a.width) != 1 {
		t.Fatalf("80-col collab browse grew %d columns", a.home.cols)
	}
	if !strings.Contains(home, folderBackWord) {
		t.Fatalf("80-col drill-in has no back row:\n%s", home)
	}
	if !strings.Contains(home, collabMarkedWord) {
		t.Fatalf("80-col marked member hid the convenience mark:\n%s", home)
	}
	a.closeHome()
	fake.participants = []CollabParticipant{{Role: "planner", SourceTitle: "Feature B"}}
	fake.activity = []CollabActivity{{Kind: collabRequestWord, SourceRef: "Feature A", Body: "how's progress"}}
	a.readCollab()
	a.touch()
	a.width = 80
	chat := plain(frame(a))
	for _, line := range strings.Split(chat, "\n") {
		if w := ansi.StringWidth(line); w > 80 {
			t.Fatalf("80-col participant/activity row is %d cells: %q", w, line)
		}
	}
}

func TestComposerAndSelectionSurviveCoordinateDeliveries(t *testing.T) {
	a, fake := collabLab(t)
	a.pointFolderID("col-billing")
	want, ok := a.home.focusedLine()
	if !ok || want.kind != homeFolderRow || want.dir != "col-billing" {
		t.Fatalf("cursor was not on Billing: %+v", want)
	}
	a.home.box.setText("keep this sentence")
	fake.activity = []CollabActivity{
		{Kind: collabRequestWord, SourceRef: "Feature A", Body: "how's progress"},
	}
	a.readCollab()
	a.home.build()
	homeText(a)
	if a.home.box.String() != "keep this sentence" {
		t.Fatalf("deliveries wiped the composer: %q", a.home.box.String())
	}
	a.home.box.setText("")
	a.home.build()
	homeText(a)
	a.pointFolderID("col-billing")
	got, ok := a.home.focusedLine()
	if !ok || got.kind != homeFolderRow || got.dir != "col-billing" {
		t.Fatalf("deliveries dropped Billing: %+v", got)
	}
	a.closeHome()
	a.input.setText("keep the draft")
	sel := a.sel
	fake.activity = append(fake.activity, CollabActivity{Kind: collabReplyWord, SourceRef: "Feature A", Body: "waiting on tests"})
	a.readCollab()
	a.touch()
	_ = frame(a)
	if a.input.String() != "keep the draft" {
		t.Fatalf("deliveries wiped the chat composer: %q", a.input.String())
	}
	if a.sel != sel {
		t.Fatalf("deliveries jumped chat selection from %d to %d", sel, a.sel)
	}
}

func TestNilCollabHasNoFolderCoordinateChrome(t *testing.T) {
	a := newLiveLab(t).open()
	a.folders = billingSecurityFolders()
	a.readHomeFolders()
	a.readCollab()
	a.home.build()
	homeText(a)
	row := homeLine{kind: homeFolderRow, dir: "col-billing", project: "Billing"}
	keys := ""
	for _, v := range a.folderVerbs(row) {
		keys += string(v.key)
	}
	if keys != "nfei" {
		t.Fatalf("nil Collab folder verbs %q, want nfei", keys)
	}
	member := homeLine{kind: homeSession, row: session.SessionRow{ID: "aaaa000000000001"}, cell: &homeCell{panel: panelFolders, key: "col-billing"}}
	keys = ""
	for _, v := range a.folderVerbs(member) {
		keys += string(v.key)
	}
	if keys != "nfmwx" {
		t.Fatalf("nil Collab member verbs %q, want nfmwx", keys)
	}
	a.closeHome()
	a.touch()
	got := plain(frame(a))
	if strings.Contains(got, collabRequestWord) || strings.Contains(got, collabMarkedWord) || strings.Contains(got, collabCoordinateWord) {
		t.Fatalf("nil Collab painted coordination chrome:\n%s", got)
	}
}

func TestCoordinateDoesNotReadTheStoreInView(t *testing.T) {
	a, fake := collabLab(t)
	fake.activity = []CollabActivity{{Kind: collabReplyWord, SourceRef: "Feature A", Body: "ok"}}
	a.readCollab()
	a.home.build()
	homeText(a)
	fake.freeze()
	homeText(a)
	a.home.move(1)
	homeText(a)
	a.closeHome()
	_ = frame(a)
}

func TestCoordinateFailedRefreshKeepsLastActivity(t *testing.T) {
	a, fake := collabLab(t)
	fake.activity = []CollabActivity{{Kind: collabRequestWord, SourceRef: "Feature A", Body: "how's progress"}}
	a.readCollab()
	fake.activityErr = errCollabProbe
	fake.activity = nil
	a.readCollab()
	a.closeHome()
	a.touch()
	got := plain(frame(a))
	if !strings.Contains(got, "how's progress") {
		t.Fatalf("failed refresh dropped last good activity:\n%s", got)
	}
}

func TestCoordinateDeliveriesPaintInTheManagementChat(t *testing.T) {
	a, fake := collabLab(t)
	a.closeHome()
	a.touch()
	hidden := plain(frame(a))
	if strings.Contains(hidden, collabRequestWord) || strings.Contains(hidden, "how's progress") {
		t.Fatalf("empty memo painted activity before the coordinating turn:\n%s", hidden)
	}
	// Adapter shape: Kind is the person word, SourceRef/ToTitle are chat ids.
	fake.activity = []CollabActivity{
		{Kind: collabRequestWord, SourceRef: "aaaa000000000002", ToTitle: "aaaa000000000002", Body: "how's progress"},
		{Kind: collabReplyWord, SourceRef: "aaaa000000000002", ToTitle: "aaaa000000000002", Body: "waiting on tests"},
		{Kind: collabSentWord, SourceRef: "bbbb000000000001", ToTitle: "bbbb000000000001", Body: "use the new interface"},
		{Kind: "accepted", SourceRef: "aaaa000000000002", Body: "must not paint"},
	}
	fake.participants = []CollabParticipant{
		{Role: "planner", SourceTitle: "aaaa000000000002"},
		{Role: "critic", SourceTitle: "bbbb000000000001"},
	}
	a.refreshCollabChrome()
	a.touch()
	got := plain(frame(a))
	for _, want := range []string{
		collabRequestWord, collabReplyWord, collabSentWord, collabSourceWord,
		"prime sieve", "pricing site", "how's progress", "waiting on tests",
		"use the new interface", "planner", "critic",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("management chat hid %q:\n%s", want, got)
		}
	}
	for _, banned := range []string{
		"aaaa000000000002", "bbbb000000000001", "accepted", "must not paint",
		"0 participants", "manager",
	} {
		if strings.Contains(got, banned) {
			t.Fatalf("management chat painted %q:\n%s", banned, got)
		}
	}
}

func TestCoordinateTurnSettleRereadsActivity(t *testing.T) {
	a, fake := collabLab(t)
	a.closeHome()
	fake.activity = []CollabActivity{
		{Kind: collabRequestWord, SourceRef: "aaaa000000000002", ToTitle: "aaaa000000000002", Body: "how's progress"},
		{Kind: collabReplyWord, SourceRef: "aaaa000000000002", Body: "waiting on tests"},
		{Kind: collabSentWord, ToTitle: "bbbb000000000001", Body: "use the new interface"},
	}
	_ = a.settle()
	got := plain(frame(a))
	for _, want := range []string{collabRequestWord, collabReplyWord, collabSentWord, "prime sieve", "pricing site"} {
		if !strings.Contains(got, want) {
			t.Fatalf("settling the coordinating turn hid %q:\n%s", want, got)
		}
	}
}

func TestFoldersIsStillNotACollabSlash(t *testing.T) {
	for _, c := range commands {
		if c.name == "coordinate" || c.name == "collab" || c.name == "mark" {
			t.Fatalf("new slash /%s; Wave 3 adds no slash command", c.name)
		}
	}
}

var errCollabProbe = errors.New("collab probe")
