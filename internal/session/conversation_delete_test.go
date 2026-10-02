package session

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/teams"
)

func deletionFixture(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	profile := filepath.Join(root, "profile")
	dir := filepath.Join(root, "projects", "project", "conversation")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := SaveMeta(dir, Meta{ID: "conversation", Workspace: root}); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, placeTranscript)
	if err := os.WriteFile(file, []byte("{\"type\":\"header\",\"version\":1,\"id\":\"conversation\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := teams.Save(profile, []teams.Team{{ID: "aaaaaaaaaaaa", Name: "team", Members: []teams.Member{{Key: file, File: file, Handle: "worker"}}}}); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "projects"), profile, file
}

func TestConversationDeleteStopsOwnerPreservesHistoryAndPreventsRecreation(t *testing.T) {
	root, profile, file := deletionFixture(t)
	if err := teams.AppendTraffic(profile, "aaaaaaaaaaaa", teams.Entry{Kind: teams.KindNote, From: "worker", To: teams.ToRoom, Text: "copied history"}); err != nil {
		t.Fatal(err)
	}
	journal, _, err := openSessionFile(file, root, "test/model", "conversation")
	if err != nil {
		t.Fatal(err)
	}
	stopped := false
	err = DeleteConversationUnder(root, profile, file, nil, func(got string) error { stopped = got == file; return journal.Close() })
	if err != nil {
		t.Fatal(err)
	}
	if !stopped {
		t.Fatal("owner was not stopped")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("transcript survived: %v", err)
	}
	f, _ := teams.Load(profile)
	if f.Teams[0].Holds(file) {
		t.Fatal("membership survived")
	}
	history, _ := teams.ReadTraffic(profile, "aaaaaaaaaaaa", "", 0)
	if len(history) != 1 || history[0].Text != "copied history" {
		t.Fatal("history was lost")
	}
	if _, _, err := openSessionFile(file, root, "test/model", "conversation"); err == nil {
		t.Fatal("stale tab recreated deleted transcript")
	}
}

func TestConversationDeleteRefusalPreservesTranscriptAndMemberships(t *testing.T) {
	root, profile, file := deletionFixture(t)
	if err := teams.Update(profile, func(f *teams.File) error { return f.SetManager("aaaaaaaaaaaa", file) }); err != nil {
		t.Fatal(err)
	}
	stopped := false
	if err := DeleteConversationUnder(root, profile, file, nil, func(string) error { stopped = true; return nil }); err == nil {
		t.Fatal("missing manager choice accepted")
	}
	if stopped {
		t.Fatal("refused deletion stopped the owner")
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal(err)
	}
	// Failure before staging leaves the store intact.
	if err := os.Mkdir(file+".delete-pending", 0700); err != nil {
		t.Fatal(err)
	}
	if err := DeleteConversationUnder(root, profile, file, map[string]string{"aaaaaaaaaaaa": ""}, nil); err == nil {
		t.Fatal("unwritable staging accepted")
	}
	f, _ := teams.Load(profile)
	if !f.Teams[0].Holds(file) || f.Teams[0].Closed() {
		t.Fatal("filesystem refusal mutated teams")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(file), conversationDeletedFile)); !os.IsNotExist(err) {
		t.Fatal("refusal retained tombstone")
	}
}

func TestConversationDeleteSerializesConcurrentCallersAndRetainsTombstone(t *testing.T) {
	root, profile, file := deletionFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- DeleteConversationUnder(root, profile, file, nil, func(string) error { close(entered); <-release; return nil })
	}()
	<-entered
	if err := DeleteConversationUnder(root, profile, file, nil, nil); err == nil {
		t.Fatal("parallel deletion accepted")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(file), conversationDeletedFile)); err != nil {
		t.Fatal("second caller removed tombstone")
	}
}

func TestConversationDeleteRemovesJournalFileSymlinkMemberships(t *testing.T) {
	root, profile, file := deletionFixture(t)
	aliasDir := filepath.Join(root, "alias")
	if err := os.Mkdir(aliasDir, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(aliasDir, placeTranscript)
	if err := os.Symlink(file, alias); err != nil {
		t.Fatal(err)
	}
	if err := teams.Update(profile, func(f *teams.File) error { return f.AddMember("aaaaaaaaaaaa", teams.Member{Key: alias, File: alias}) }); err != nil {
		t.Fatal(err)
	}
	if err := DeleteConversationUnder(root, profile, file, nil, nil); err != nil {
		t.Fatal(err)
	}
	f, _ := teams.Load(profile)
	if len(f.Teams[0].Members) != 0 {
		t.Fatalf("symlink membership survived: %+v", f.Teams[0].Members)
	}
}

func TestConversationDeleteFreshManagerScopeIsRecheckedBeforeStopping(t *testing.T) {
	root, profile, file := deletionFixture(t)
	if err := teams.Update(profile, func(f *teams.File) error {
		if err := f.SetManager("aaaaaaaaaaaa", file); err != nil {
			return err
		}
		f.Teams = append(f.Teams, teams.Team{ID: "bbbbbbbbbbbb", Name: "new child", Parent: "aaaaaaaaaaaa"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	stopped := false
	err := DeleteConversationUnder(root, profile, file, map[string]string{"aaaaaaaaaaaa": ""}, func(string) error { stopped = true; return errors.New("should not stop") }, map[string][]string{"aaaaaaaaaaaa": {"aaaaaaaaaaaa"}})
	if err == nil || stopped {
		t.Fatal("changed tree was not refused before stopping")
	}
	if _, err = os.Stat(file); err != nil {
		t.Fatal(err)
	}
}

func TestConversationDeleteStaleOwnerRequestIsDiscarded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, conversationDeleteRequest)
	if err := os.WriteFile(path, []byte(time.Now().Add(-2*time.Minute).UTC().Format(time.RFC3339Nano)), 0600); err != nil {
		t.Fatal(err)
	}
	a := &Agent{config: Config{Place: Place{Dir: dir}}}
	a.drainConversationDeletion()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("stale request was kept")
	}
}
