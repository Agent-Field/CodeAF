package teams

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeletedConversationCannotBeReadmittedByStaleTeamWriter(t *testing.T) {
	for _, mode := range []string{"member", "new-team", "manager", "save"} {
		t.Run(mode, func(t *testing.T) {
			profile, dir := t.TempDir(), t.TempDir()
			file := filepath.Join(dir, "session.jsonl")
			member := Member{Key: file, File: file, Word: "Deleted conversation"}
			initial := Team{ID: "team", Name: "Team"}
			if mode == "manager" {
				initial.Members = []Member{member}
			}
			if err := Save(profile, []Team{initial}); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, ConversationDeletedFile), []byte("deleted"), 0600); err != nil {
				t.Fatal(err)
			}
			mutate := func(f *File) error {
				switch mode {
				case "new-team":
					f.Teams = append(f.Teams, Team{ID: "new", Name: "New", Members: []Member{member}})
				case "manager":
					f.Teams[0].Manager = file
				default:
					f.Teams[0].Members = append(f.Teams[0].Members, member)
				}
				return nil
			}
			var err error
			if mode == "save" {
				snapshot, _ := Snapshot(profile)
				mutate(snapshot)
				err = Save(profile, snapshot.Teams)
			} else {
				err = Update(profile, mutate)
			}
			if err == nil || !strings.Contains(err.Error(), "permanently deleted") {
				t.Fatalf("late admission allowed: %v", err)
			}
			snapshot, err := Snapshot(profile)
			if err != nil || len(snapshot.Teams) != 1 || snapshot.Teams[0].Manager != "" || len(snapshot.Teams[0].Members) != len(initial.Members) {
				t.Fatal("refused write changed persisted membership")
			}
			// An unrelated edit can still repair older files or retain their history.
			if err = Update(profile, func(f *File) error { f.Teams[0].Name = "Renamed"; return nil }); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDeletedCanonicalConversationCannotBeReadmittedThroughJournalAlias(t *testing.T) {
	profile, canonicalDir, aliasDir := t.TempDir(), t.TempDir(), t.TempDir()
	canonical := filepath.Join(canonicalDir, "transcript.jsonl")
	alias := filepath.Join(aliasDir, "transcript.jsonl")
	if err := os.WriteFile(canonical, []byte("history"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(canonical, alias); err != nil {
		t.Fatal(err)
	}
	if err := Save(profile, []Team{{ID: "team", Name: "Team"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonicalDir, ConversationDeletedFile), []byte("deleted"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(canonical); err != nil {
		t.Fatal(err)
	}
	err := Update(profile, func(f *File) error {
		f.Teams[0].Members = []Member{{Key: canonical, File: alias, Word: "Deleted conversation"}}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "permanently deleted") {
		t.Fatalf("dangling journal alias readmitted: %v", err)
	}
	f, err := Snapshot(profile)
	if err != nil || len(f.Teams[0].Members) != 0 {
		t.Fatal("refused alias changed membership")
	}
}
