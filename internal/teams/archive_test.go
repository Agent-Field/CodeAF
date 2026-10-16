package teams

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	archP = "pppppppppppp"
	archA = "aaaaaaaaaaaa"
	archB = "bbbbbbbbbbbb"
)

// family is a parent with two sub-teams, saved under dir.
func family(t *testing.T, dir string) {
	t.Helper()
	must(t, Save(dir, []Team{
		{ID: archP, Name: "harbor", Manager: "pm", Members: []Member{{Key: "pm", Handle: "boss"}}},
		{ID: archA, Name: "front", Parent: archP, Members: []Member{{Key: "web", Handle: "web"}}},
		{ID: archB, Name: "back", Parent: archP, Members: []Member{{Key: "api", Handle: "api"}}},
	}))
}

// ONLY A CLOSED TEAM IS ARCHIVED: an open one is refused and stays open, the
// root is refused, an unknown id is an error.
func TestArchiveRefusesWhatIsNotClosed(t *testing.T) {
	f := &File{Teams: []Team{
		{ID: archP, Name: "harbor", Members: []Member{{Key: "pm"}}},
		{ID: archA, Name: "all", Root: true},
	}}
	if err := f.Archive(archP, time.Unix(5, 0)); !errors.Is(err, ErrNotClosed) {
		t.Fatalf("archiving an open team = %v", err)
	}
	if p, _ := f.Team(archP); p.Closed() || p.Archived() {
		t.Fatalf("a refused archive changed the team: %+v", p)
	}
	if err := f.Archive(archA, time.Unix(5, 0)); !errors.Is(err, ErrRoot) {
		t.Fatalf("archiving the root = %v", err)
	}
	if err := f.Archive("missing", time.Unix(5, 0)); err == nil {
		t.Fatal("an unknown team was archived")
	}
}

// ARCHIVING MARKS THE ONE TEAM. Its closed sub-teams are not archived with it,
// its record is unchanged, and archiving again keeps the first time.
func TestArchiveMarksOnlyTheTargetAndKeepsTheRecord(t *testing.T) {
	dir := t.TempDir()
	family(t, dir)
	must(t, AppendTraffic(dir, archP, Entry{Kind: KindNote, From: "pm", To: ToRoom, Text: "kept"}))
	must(t, Update(dir, func(f *File) error { return f.Disband(archP, time.Unix(100, 0), "rep") }))
	// The sub-team, not the parent, is the selected row.
	must(t, Update(dir, func(f *File) error { return f.Archive(archA, time.Unix(200, 0)) }))
	f, err := Load(dir)
	must(t, err)
	front, _ := f.Team(archA)
	parent, _ := f.Team(archP)
	back, _ := f.Team(archB)
	if !front.Archived() || !front.ArchivedAt.Equal(time.Unix(200, 0)) || !front.Closed() || front.ClosedWith != archP {
		t.Fatalf("the target = %+v", front)
	}
	if parent.Archived() || back.Archived() || !parent.Closed() || !back.Closed() {
		t.Fatalf("the family was touched: parent %+v back %+v", parent, back)
	}
	must(t, Update(dir, func(f *File) error { return f.Archive(archA, time.Unix(900, 0)) }))
	f, _ = Load(dir)
	if again, _ := f.Team(archA); !again.ArchivedAt.Equal(time.Unix(200, 0)) {
		t.Fatalf("archiving again moved the time: %v", again.ArchivedAt)
	}
	if got := f.ArchivedTeams(); len(got) != 1 || got[0].ID != archA {
		t.Fatalf("archived teams = %+v", got)
	}
	if got := f.ClosedTeams(); len(got) != 3 {
		t.Fatalf("the terminal's closed list must still hold every closed team: %d", len(got))
	}
	if tail, _ := ReadTraffic(dir, archP, "", 0); len(tail) == 0 || tail[len(tail)-1].Text != "kept" {
		t.Fatalf("the traffic changed: %+v", tail)
	}
	if rep := parent.Report; rep != "rep" || len(parent.Members) != 1 {
		t.Fatalf("the parent's record changed: %+v", parent)
	}
}

// THE MARK IS ON DISK, SURVIVES A RELOAD AND AN UNKNOWN FIELD BESIDE IT, AND
// REOPENING CLEARS IT. A sub-team cannot reopen under a closed parent.
func TestArchivePersistsAndReopenClearsIt(t *testing.T) {
	dir := t.TempDir()
	family(t, dir)
	must(t, Update(dir, func(f *File) error {
		for _, id := range []string{archP, archA} {
			if err := f.Disband(id, time.Unix(100, 0), ""); err != nil {
				return err
			}
		}
		must(t, f.Archive(archP, time.Unix(300, 0)))
		return f.Archive(archA, time.Unix(300, 0))
	}))
	raw, err := os.ReadFile(Path(dir))
	must(t, err)
	if strings.Count(string(raw), `"archived_at"`) != 2 {
		t.Fatalf("archived_at is not on disk for both: %s", raw)
	}
	f, err := Load(dir)
	must(t, err)
	if p, _ := f.Team(archP); !p.Archived() || !p.ArchivedAt.Equal(time.Unix(300, 0)) {
		t.Fatalf("not archived after reload: %+v", p)
	}
	if err := Update(dir, func(f *File) error { return f.Reopen(archA) }); !errors.Is(err, ErrParentClosed) {
		t.Fatalf("a child reopened under a closed parent: %v", err)
	}
	if a, _ := mustLoad(t, dir).Team(archA); !a.Archived() {
		t.Fatal("a refused reopen cleared the archive mark")
	}
	must(t, Update(dir, func(f *File) error { return f.Reopen(archP) }))
	f = mustLoad(t, dir)
	for _, id := range []string{archP, archA} {
		if team, _ := f.Team(id); team.Closed() || team.Archived() || !team.ArchivedAt.IsZero() {
			t.Fatalf("%s after reopen: %+v", id, team)
		}
	}
	if raw, _ := os.ReadFile(Path(dir)); strings.Contains(string(raw), "archived_at") {
		t.Fatalf("a reopened team still carries the mark: %s", raw)
	}
}

// A FILE A LATER BUILD WROTE keeps the fields this one does not know when a
// team is archived beside them.
func TestArchiveKeepsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	family(t, dir)
	must(t, Update(dir, func(f *File) error { return f.Disband(archB, time.Unix(1, 0), "") }))
	raw, err := os.ReadFile(Path(dir))
	must(t, err)
	patched := strings.Replace(string(raw), `"id": "`+archB+`"`, `"id": "`+archB+`","future":{"x":1}`, 1)
	if patched == string(raw) {
		patched = strings.Replace(string(raw), `"id":"`+archB+`"`, `"id":"`+archB+`","future":{"x":1}`, 1)
	}
	must(t, os.WriteFile(Path(dir), []byte(patched), 0o600))
	must(t, Update(dir, func(f *File) error { return f.Archive(archB, time.Unix(2, 0)) }))
	after, _ := os.ReadFile(Path(dir))
	if !strings.Contains(string(after), `"future"`) || !strings.Contains(string(after), `"archived_at"`) {
		t.Fatalf("a field was lost: %s", after)
	}
}

func mustLoad(t *testing.T, dir string) *File {
	t.Helper()
	f, err := Load(dir)
	must(t, err)
	return f
}
