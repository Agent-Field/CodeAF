package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A CONVERSATION'S ID IS ITS FOLDER'S NAME, IN EITHER LAYOUT. A cell keeps its
// transcript at <id>/.cell/transcript.jsonl and a legacy folder at
// <id>/transcript.jsonl; both answer <id>. A cell's transcript parent (".cell")
// is never an id: one id for every cell conversation made each new one answer
// for all the others.
func TestAppAgentIDIsTheFolderInBothLayouts(t *testing.T) {
	bucket := t.TempDir()
	for _, tc := range []struct{ transcript, want string }{
		{filepath.Join(bucket, "5e55a0f1", cell.TranscriptPath), "5e55a0f1"},
		{filepath.Join(bucket, "6f66b1a2", "transcript.jsonl"), "6f66b1a2"},
	} {
		if got := appAgentID(tc.transcript); got != tc.want {
			t.Errorf("appAgentID(%s) = %q, want %q", tc.transcript, got, tc.want)
		}
	}
}

// TWO NEW CONVERSATIONS ARE TWO IDS WHEN CELLS ARE ON. Each create answers its
// own folder's name, never ".cell", and each id finds its own conversation.
func TestAppCreateGivesEachCellConversationItsFolderName(t *testing.T) {
	defer cell.UseAvailabilityInTests(true)()
	useLocalHome(t)
	rec := newAppRecorder()
	backend := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	})
	// The real mint: each create gets a folder of its own under this test's home.
	backend.mint = appMint
	backend.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	defer backend.Close()
	ws := t.TempDir()
	one, err := backend.CreateAgent(appwire.CreateAsk{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	two, err := backend.CreateAgent(appwire.CreateAsk{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	if one.ID == two.ID || one.ID == cell.StateDir || two.ID == cell.StateDir {
		t.Fatalf("two creates answered %q and %q", one.ID, two.ID)
	}
	for _, id := range []string{one.ID, two.ID} {
		a, err := backend.find(id)
		if err != nil {
			t.Fatalf("%s does not find its conversation: %v", id, err)
		}
		if filepath.Base(filepath.Dir(a.transcript)) != cell.StateDir || filepath.Base(session.DirOf(a.transcript)) != id {
			t.Fatalf("%s is not its own cell folder: %s", id, a.transcript)
		}
		// Its media are its own folder's: listed under its id, kept in its folder, never in ".cell".
		scope, roots, err := backend.mediaScope(id)
		if err != nil {
			t.Fatal(err)
		}
		folder := session.DirOf(a.transcript)
		if scope != id {
			t.Fatalf("%s's media are listed under %q", id, scope)
		}
		for _, root := range roots[1:] {
			if !strings.HasPrefix(root, folder+string(filepath.Separator)) || strings.Contains(root, string(filepath.Separator)+cell.StateDir+string(filepath.Separator)+cell.StateDir) {
				t.Fatalf("%s's media root %s is not under its folder %s", id, root, folder)
			}
		}
	}
}

// A FACTORY JOB'S CHATS NAME ITS TALK'S FOLDER WHEN THE TALK IS A CELL.
func TestAJobsChatsNameTheCellTalksFolder(t *testing.T) {
	defer cell.UseAvailabilityInTests(true)()
	s := newLocalSetup(t)
	s.backend.open = func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	st := s.start()
	it, err := s.backend.local.host.floor.Create(factory.Item{Repo: "acme/api", Num: 8, Title: "Fix the ledger", Kind: factory.KindIssue, State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	var chats appwire.FactoryJobChats
	s.must("factory.job_chats", map[string]any{"factory": st.Factory, "job": it.ID}, &chats)
	kept, err := s.backend.local.host.floor.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(filepath.Dir(kept.Talk)) != cell.StateDir {
		t.Fatalf("the job's talk is not a cell: %s", kept.Talk)
	}
	if want := filepath.Base(session.DirOf(kept.Talk)); chats.Agent != want || chats.Agent == cell.StateDir {
		t.Fatalf("job chats answered agent %q, want the talk's folder %q", chats.Agent, want)
	}
	if !s.backend.isHeld(chats.Agent) {
		t.Fatal("the job's agent is not held open under its folder's id")
	}
}
