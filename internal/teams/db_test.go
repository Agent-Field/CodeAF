package teams

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// watchRows records every row the store inserts, updates or deletes from here
// on, by table and team, in a table of the test's own beside the store's.
func watchRows(t *testing.T, dir string) func() []string {
	t.Helper()
	h, err := acquire(Path(dir), false)
	must(t, err)
	defer h.release()
	stmts := []string{"CREATE TABLE IF NOT EXISTS touched (what TEXT NOT NULL)"}
	for _, table := range []string{"teams", "members", "briefs"} {
		id := "team_id"
		if table == "teams" {
			id = "id"
		}
		for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
			row := "NEW"
			if op == "DELETE" {
				row = "OLD"
			}
			stmts = append(stmts, fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS watch_%s_%s AFTER %s ON %s BEGIN INSERT INTO touched (what) VALUES ('%s ' || %s.%s); END`,
				table, strings.ToLower(op), op, table, table, row, id))
		}
	}
	for _, stmt := range stmts {
		_, err := h.db.Exec(stmt)
		must(t, err)
	}
	return func() []string {
		t.Helper()
		h, err := acquire(Path(dir), false)
		must(t, err)
		defer h.release()
		rows, err := h.db.Query("SELECT what FROM touched")
		must(t, err)
		defer rows.Close()
		var out []string
		for rows.Next() {
			var what string
			must(t, rows.Scan(&what))
			out = append(out, what)
		}
		_, err = h.db.Exec("DELETE FROM touched")
		must(t, err)
		sort.Strings(out)
		return out
	}
}

func manyTeams(n int) []Team {
	out := make([]Team, n)
	for i := range out {
		out[i] = Team{ID: fmt.Sprintf("team%08d", i), Name: fmt.Sprint("crew ", i), Made: time.Unix(int64(i), 0).UTC(), hued: true, Hue: float64(i)}
		for j := range 3 {
			out[i].Members = append(out[i].Members, Member{Key: fmt.Sprintf("/s/%d/%d", i, j), Word: fmt.Sprint("member ", j)})
		}
	}
	return out
}

// A WRITE TOUCHES ITS OWN ROWS AND NO OTHER. Renaming one team rewrites that
// team's row; adding a member inserts one row; removing a team deletes its
// rows; a write that changes nothing writes nothing and leaves the stamp.
func TestAWriteTouchesOnlyItsOwnRows(t *testing.T) {
	dir := t.TempDir()
	must(t, Save(dir, manyTeams(40)))
	touched := watchRows(t, dir)
	stamp := Stamp(dir)

	must(t, Update(dir, func(f *File) error { return nil }))
	if got := touched(); len(got) != 0 || Stamp(dir) != stamp {
		t.Fatalf("an empty update wrote %v (stamp %q → %q)", got, stamp, Stamp(dir))
	}

	must(t, Update(dir, func(f *File) error {
		i := Index(f.Teams, "team00000017")
		f.Teams[i].Name = "renamed"
		return nil
	}))
	if got := touched(); fmt.Sprint(got) != "[teams team00000017]" {
		t.Fatalf("a rename wrote %v", got)
	}

	must(t, Update(dir, func(f *File) error {
		return f.AddMember("team00000003", Member{Key: "/s/new", Word: "newcomer"})
	}))
	// The member's row, and the team's revision so readers decode it again.
	if got := touched(); fmt.Sprint(got) != "[members team00000003 teams team00000003]" {
		t.Fatalf("an added member wrote %v", got)
	}

	must(t, Update(dir, func(f *File) error {
		i := Index(f.Teams, "team00000021")
		f.Teams = append(f.Teams[:i], f.Teams[i+1:]...)
		return nil
	}))
	for _, what := range touched() {
		if !strings.HasSuffix(what, " team00000021") {
			t.Fatalf("removing one team wrote %q", what)
		}
	}
	if Stamp(dir) == stamp {
		t.Fatal("the writes did not move the stamp")
	}
	f := mustLoad(t, dir)
	if len(f.Teams) != 39 || f.Teams[17].Name != "renamed" || len(f.Teams[3].Members) != 4 {
		t.Fatalf("after the writes: %d teams", len(f.Teams))
	}
}

// TWO WRITERS NEVER LOSE EACH OTHER'S CHANGE, in one process or two. The app's
// engine and a terminal are two processes on one store: each takes the lock,
// reads the rows as they are and writes its own.
func TestTwoWritersNeverLoseAChange(t *testing.T) {
	dir := t.TempDir()
	must(t, Save(dir, []Team{{ID: "aaaaaaaaaaaa", Name: "shared"}}))
	const each = 20
	child := exec.Command(os.Args[0], "-test.run=^TestTwoWritersHelperProcess$", "-test.count=1")
	child.Env = append(os.Environ(), "TEAMS_TEST_WRITER_DIR="+dir, fmt.Sprintf("TEAMS_TEST_WRITER_COUNT=%d", each))
	var out strings.Builder
	child.Stdout, child.Stderr = &out, &out
	must(t, child.Start())
	var wg sync.WaitGroup
	for w := range 2 {
		wg.Go(func() {
			for i := range each {
				key := fmt.Sprintf("/here/%d/%d", w, i)
				if err := writeRetrying(dir, key); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
	if err := child.Wait(); err != nil {
		t.Fatalf("the other process: %v\n%s", err, out.String())
	}
	f := mustLoad(t, dir)
	t0, _ := f.Team("aaaaaaaaaaaa")
	if len(t0.Members) != 3*each {
		t.Fatalf("%d members, want %d", len(t0.Members), 3*each)
	}
	seen := map[string]bool{}
	for _, m := range t0.Members {
		seen[m.Key] = true
	}
	for i := range each {
		for _, key := range []string{fmt.Sprintf("/here/0/%d", i), fmt.Sprintf("/here/1/%d", i), fmt.Sprintf("/there/%d", i)} {
			if !seen[key] {
				t.Fatalf("lost %s", key)
			}
		}
	}
}

// writeRetrying adds key to the shared team, trying again while the lock is
// busy past its wait (a slow machine under a full test run).
func writeRetrying(dir, key string) error {
	for {
		err := Update(dir, func(f *File) error {
			return f.AddMember("aaaaaaaaaaaa", Member{Key: key})
		})
		if !errors.Is(err, ErrBusy) {
			return err
		}
	}
}

// TestTwoWritersHelperProcess is the second process of
// [TestTwoWritersNeverLoseAChange]; on its own it does nothing.
func TestTwoWritersHelperProcess(t *testing.T) {
	dir := os.Getenv("TEAMS_TEST_WRITER_DIR")
	if dir == "" {
		t.Skip("run by TestTwoWritersNeverLoseAChange")
	}
	var n int
	fmt.Sscan(os.Getenv("TEAMS_TEST_WRITER_COUNT"), &n)
	for i := range n {
		must(t, writeRetrying(dir, fmt.Sprintf("/there/%d", i)))
	}
}

// A STORE ANOTHER PROCESS WROTE IS READ AGAIN, never answered from what this
// process held: the revision is the store's, not the process's.
func TestAnotherProcessesWriteIsSeen(t *testing.T) {
	dir := t.TempDir()
	must(t, Save(dir, []Team{{ID: "aaaaaaaaaaaa", Name: "before"}}))
	if f := mustLoad(t, dir); f.Teams[0].Name != "before" {
		t.Fatal(f.Teams[0].Name)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestRenameHelperProcess$", "-test.count=1")
	child.Env = append(os.Environ(), "TEAMS_TEST_RENAME_DIR="+dir)
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if f := mustLoad(t, dir); f.Teams[0].Name != "after" {
		t.Fatalf("this process still reads %q", f.Teams[0].Name)
	}
}

// TestRenameHelperProcess is the other process of
// [TestAnotherProcessesWriteIsSeen]; on its own it does nothing.
func TestRenameHelperProcess(t *testing.T) {
	dir := os.Getenv("TEAMS_TEST_RENAME_DIR")
	if dir == "" {
		t.Skip("run by TestAnotherProcessesWriteIsSeen")
	}
	must(t, Update(dir, func(f *File) error { f.Teams[0].Name = "after"; return nil }))
}

// BRIEFS ARE READ ONLY WHERE THEY ARE SHOWN. A factory stage's opening brief,
// however large, is a row of its own: a load leaves it out, [WithBriefs] reads
// it, a write that does not give it leaves it as it was, a member who moves
// to another team takes it along and a member who leaves takes it nowhere.
// Nothing caps how large it may be.
func TestBriefsAreReadOnlyWhereShown(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("review every file in the change. ", 100_000) // ~3.3 MB
	must(t, Save(dir, []Team{
		{ID: "aaaaaaaaaaaa", Name: "item", FactoryManaged: true, Members: []Member{{Key: "/stage", Handle: "review", FactoryStage: true, FactoryBrief: big}}},
		{ID: "bbbbbbbbbbbb", Name: "other"},
	}))
	f := mustLoad(t, dir)
	m, _ := f.Teams[0].Member("/stage")
	if m.FactoryBrief != "" || !m.FactoryStage {
		t.Fatalf("a load carried a brief of %d bytes", len(m.FactoryBrief))
	}
	if snap, _, err := ReadSnapshot(context.Background(), dir); err != nil || snap.Teams[0].Members[0].FactoryBrief != "" {
		t.Fatalf("a snapshot carried the brief: %v", err)
	}
	shown, err := WithBriefs(dir, "aaaaaaaaaaaa", m)
	must(t, err)
	if shown.FactoryBrief != big {
		t.Fatalf("the brief read back as %d bytes", len(shown.FactoryBrief))
	}

	// A write that does not carry the brief leaves it.
	must(t, Update(dir, func(f *File) error { f.Teams[0].Name = "item, renamed"; return nil }))
	if shown, _ = WithBriefs(dir, "aaaaaaaaaaaa", m); shown.FactoryBrief != big {
		t.Fatal("an unrelated write lost the brief")
	}

	// A move takes it along.
	must(t, Update(dir, func(f *File) error {
		moved := f.Teams[0].Members[0]
		f.Teams[0].Members = nil
		f.Teams[1].Members = append(f.Teams[1].Members, moved)
		return nil
	}))
	if shown, _ = WithBriefs(dir, "aaaaaaaaaaaa", m); shown.FactoryBrief != "" {
		t.Fatal("the brief stayed with the team the member left")
	}
	if shown, _ = WithBriefs(dir, "bbbbbbbbbbbb", m); shown.FactoryBrief != big {
		t.Fatal("the brief did not move with its member")
	}

	// Leaving takes it nowhere.
	must(t, Update(dir, func(f *File) error { return f.RemoveMember("bbbbbbbbbbbb", "/stage") }))
	if shown, _ = WithBriefs(dir, "bbbbbbbbbbbb", m); shown.FactoryBrief != "" {
		t.Fatal("a member who left kept its brief row")
	}
	h, err := acquire(Path(dir), false)
	must(t, err)
	defer h.release()
	var left int
	must(t, h.db.QueryRow("SELECT count(*) FROM briefs").Scan(&left))
	if left != 0 {
		t.Fatalf("%d brief rows outlived their members", left)
	}
}

// A HOME THAT STILL HAS teams.json STARTS EMPTY. There is no migration: the
// old file, and the first build's spaces.json, are never read, written,
// renamed or removed, and the store is made beside them on the first write.
func TestALeftoverTeamsFileIsIgnoredAndUntouched(t *testing.T) {
	dir := t.TempDir()
	old := []byte(`{"version":2,"teams":[{"id":"aaaaaaaaaaaa","name":"from json","members":[{"key":"k1"}]}]}`)
	spaces := []byte(`{"spaces":[{"name":"older"}]}`)
	oldPath, spacesPath := filepath.Join(dir, "teams.json"), filepath.Join(dir, "spaces.json")
	must(t, os.WriteFile(oldPath, old, 0o600))
	must(t, os.WriteFile(spacesPath, spaces, 0o600))
	pinned := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	must(t, os.Chtimes(oldPath, pinned, pinned))
	must(t, os.Chtimes(spacesPath, pinned, pinned))

	if f := mustLoad(t, dir); len(f.Teams) != 0 {
		t.Fatalf("a fresh store read %d teams from the old file", len(f.Teams))
	}
	if f, _, err := ReadSnapshot(context.Background(), dir); err != nil || len(f.Teams) != 0 {
		t.Fatalf("a snapshot read %+v %v", f, err)
	}
	if Stamp(dir) != MissingStamp {
		t.Fatalf("no store yet, but the stamp is %q", Stamp(dir))
	}
	must(t, Update(dir, func(f *File) error {
		f.Teams = append(f.Teams, Team{ID: "bbbbbbbbbbbb", Name: "new"})
		return nil
	}))
	if f := mustLoad(t, dir); len(f.Teams) != 1 || f.Teams[0].Name != "new" {
		t.Fatalf("after a write: %+v", f.Teams)
	}
	for path, want := range map[string][]byte{oldPath: old, spacesPath: spaces} {
		raw, err := os.ReadFile(path)
		must(t, err)
		info, err := os.Stat(path)
		must(t, err)
		if string(raw) != string(want) || !info.ModTime().Equal(pinned) {
			t.Fatalf("%s changed: %q at %v", filepath.Base(path), raw, info.ModTime())
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".migrated") || strings.Contains(e.Name(), ".unreadable") {
			t.Fatalf("the profile grew %s", e.Name())
		}
	}
}

// A LOAD OF A STORE THAT GREW OLD DECODES ONLY WHAT CHANGED: nobody wrote
// since the last read, and nothing is decoded however many teams there are;
// another process changed one team, and that team alone is decoded again.
func TestALoadDecodesOnlyTheTeamsThatChanged(t *testing.T) {
	dir := t.TempDir()
	must(t, Save(dir, manyTeams(300)))
	mustLoad(t, dir)
	before := storeReads.Load()
	for range 20 {
		if f := mustLoad(t, dir); len(f.Teams) != 300 {
			t.Fatal(len(f.Teams))
		}
	}
	if got := storeReads.Load() - before; got != 0 {
		t.Fatalf("%d teams decoded from an unchanged store", got)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestRenameHelperProcess$", "-test.count=1")
	child.Env = append(os.Environ(), "TEAMS_TEST_RENAME_DIR="+dir)
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	before = storeReads.Load()
	f := mustLoad(t, dir)
	if got := storeReads.Load() - before; got != 1 || f.Teams[0].Name != "after" || len(f.Teams) != 300 || len(f.Teams[299].Members) != 3 {
		t.Fatalf("%d teams decoded after one changed (%q, %d teams)", got, f.Teams[0].Name, len(f.Teams))
	}
}

// A SHORT FILE THAT IS NOT A DATABASE IS NOT BUILT OVER. SQLite reads a file
// shorter than its header as an empty database; the store refuses it first,
// so a stray byte is never mistaken for a store with no teams.
func TestAShortForeignFileIsUnreadableAndUntouched(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(Path(dir), []byte("["), 0o600))
	if _, err := Load(dir); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("a one-byte file loaded with %v", err)
	}
	if err := Update(dir, func(f *File) error { f.Teams = append(f.Teams, Team{ID: "aaaaaaaaaaaa"}); return nil }); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("a one-byte file took a write: %v", err)
	}
	if raw, _ := os.ReadFile(Path(dir)); string(raw) != "[" {
		t.Fatalf("the file became %q", raw)
	}
}

// A PROCESS THAT CLOSES THE STORE ON ITS WAY OUT LEAVES NO WAL BEHIND. While
// the store is open its writes sit in teams.db-wal; Close is the last
// connection leaving, which folds them into teams.db and removes the WAL, and
// what was written is all there on the next open.
func TestCloseFoldsTheWalIntoTheDatabase(t *testing.T) {
	dir := t.TempDir()
	must(t, Save(dir, manyTeams(20)))
	if _, err := os.Stat(Path(dir) + "-wal"); err != nil {
		t.Fatalf("an open store has no WAL: %v", err)
	}
	Close()
	if _, err := os.Stat(Path(dir) + "-wal"); !os.IsNotExist(err) {
		t.Fatalf("the WAL outlived the close: %v", err)
	}
	// A handle in use when Close comes closes as it is released.
	h, err := acquire(Path(dir), false)
	must(t, err)
	Close()
	var n int
	must(t, h.db.QueryRow("SELECT count(*) FROM teams").Scan(&n))
	h.release()
	if n != 20 {
		t.Fatalf("%d teams after closing", n)
	}
	if _, err := os.Stat(Path(dir) + "-wal"); !os.IsNotExist(err) {
		t.Fatalf("the WAL outlived the last release: %v", err)
	}
	if f := mustLoad(t, dir); len(f.Teams) != 20 {
		t.Fatalf("reopened with %d teams", len(f.Teams))
	}
}

// UPDATEOPEN NEITHER READS NOR WRITES A CLOSED TEAM. It is handed the open
// teams; leaving one out deletes it, a closed one stays whatever the list
// says, a team it adds goes after every team in the store, and one it closes
// is written closed and leaves the open teams.
func TestUpdateOpenWritesOnlyOpenTeams(t *testing.T) {
	dir := t.TempDir()
	list := manyTeams(6)
	list[1].State, list[1].ClosedAt = TeamClosed, time.Unix(5, 0).UTC()
	list[4].State, list[4].ClosedAt = TeamClosed, time.Unix(5, 0).UTC()
	must(t, Save(dir, list))
	touched := watchRows(t, dir)
	var seen []string
	must(t, UpdateOpen(dir, func(f *File) error {
		for _, team := range f.Teams {
			seen = append(seen, team.ID)
		}
		i := Index(f.Teams, "team00000005")
		f.Teams = append(f.Teams[:i:i], f.Teams[i+1:]...) // drops team 5
		f.Teams = append(f.Teams, Team{ID: "new000000000", Name: "new"})
		return f.Close("team00000003", time.Unix(9, 0), "")
	}))
	if fmt.Sprint(seen) != "[team00000000 team00000002 team00000003 team00000005]" {
		t.Fatalf("handed %v", seen)
	}
	for _, what := range touched() {
		if strings.HasSuffix(what, "team00000001") || strings.HasSuffix(what, "team00000004") {
			t.Fatalf("a closed team was written: %s", what)
		}
	}
	f := mustLoad(t, dir)
	if got := ids(f); got != "team,team00000001,team00000002,team00000003,team00000004,new" {
		t.Fatalf("after: %s", got)
	}
	if c, _ := f.Team("team00000003"); !c.Closed() {
		t.Fatal("the close was not written")
	}
	if open := mustOpen(t, dir); ids(open) != "team,team00000002,new" {
		t.Fatalf("open after: %s", ids(open))
	}
}

func mustOpen(t *testing.T, dir string) *File {
	t.Helper()
	f, err := LoadOpen(dir)
	must(t, err)
	return f
}
