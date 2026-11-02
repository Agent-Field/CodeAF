package teams

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// narrowTree is a root over two crews, one with a sub-crew and a closed
// sub-crew, and a closed crew, each member a transcript in its own folder.
//
//	root (boss)
//	├── alpha (lead manages; web, shared)
//	│   ├── beta (web2)
//	│   └── gone (closed; old)
//	├── delta (shared)
//	└── shut (closed; web)
func narrowTree(t *testing.T, dir string) {
	t.Helper()
	conv := func(name string) Member {
		key := filepath.Join("/chats", name, "transcript.jsonl")
		return Member{Key: key, File: key, Handle: name, HandleBy: HandleByTyped}
	}
	made := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	must(t, Save(dir, []Team{
		{ID: "root00000000", Name: "All teams", Root: true, Made: made, Members: []Member{conv("boss")}, Manager: conv("boss").Key},
		{ID: "alpha0000000", Name: "alpha", Parent: "root00000000", Made: made, Members: []Member{conv("lead"), conv("web"), conv("shared")}, Manager: conv("lead").Key},
		{ID: "beta00000000", Name: "beta", Parent: "alpha0000000", Made: made, Members: []Member{conv("web2")}},
		{ID: "gone00000000", Name: "gone", Parent: "alpha0000000", Made: made, Members: []Member{conv("old")}, State: TeamClosed, ClosedAt: made},
		{ID: "delta0000000", Name: "delta", Parent: "root00000000", Made: made, Members: []Member{conv("shared")}},
		{ID: "shut00000000", Name: "shut", Parent: "root00000000", Made: made, Members: []Member{conv("web")}, State: TeamClosed, ClosedAt: made},
	}))
}

func ids(f *File) string {
	var out []string
	for _, t := range f.Teams {
		out = append(out, strings.TrimRight(t.ID, "0"))
	}
	return strings.Join(out, ",")
}

// THE NARROW READS ANSWER THEIR CORNER OF THE TREE, in the store's order.
func TestNarrowReadsAnswerTheirCorner(t *testing.T) {
	dir := t.TempDir()
	narrowTree(t, dir)
	for _, c := range []struct {
		name string
		load func() (*File, error)
		want string
	}{
		{"open", func() (*File, error) { return LoadOpen(dir) }, "root,alpha,beta,delta"},
		{"by id", func() (*File, error) { return LoadTeams(dir, "delta0000000", "alpha0000000", "nope") }, "alpha,delta"},
		{"lineage", func() (*File, error) { return LoadLineage(dir, "beta00000000") }, "root,alpha,beta"},
		{"subtree", func() (*File, error) { return LoadSubtree(dir, "alpha0000000") }, "alpha,beta,gone"},
		{"a member's teams", func() (*File, error) { return LoadTeamsOf(dir, "web", false) }, "alpha,shut"},
		{"another's", func() (*File, error) { return LoadTeamsOf(dir, "shared", false) }, "alpha,delta"},
		// web: its own teams, open or closed, the root above, beta below,
		// alpha's closed child (whether alpha can be dissolved), and delta,
		// where alpha's member shared is too (where its home may be).
		{"a conversation's view", func() (*File, error) { return LoadConversationView(dir, "web") }, "root,alpha,beta,gone,delta,shut"},
		{"a view of nobody", func() (*File, error) { return LoadConversationView(dir, "stranger") }, ""},
		{"around a crew", func() (*File, error) { return LoadAround(dir, "beta00000000") }, "root,alpha,beta"},
	} {
		got, err := c.load()
		if err != nil || ids(got) != c.want {
			t.Errorf("%s: %q (%v), want %q", c.name, ids(got), err, c.want)
		}
	}
	root, ok, err := LoadRoot(dir)
	if err != nil || !ok || root.ID != "root00000000" || !root.Holds("/chats/boss/transcript.jsonl") {
		t.Fatalf("root %+v %v %v", root, ok, err)
	}
	if any, err := AnyManager(dir); err != nil || !any {
		t.Fatalf("any manager: %v %v", any, err)
	}
	empty := t.TempDir()
	if f, err := LoadTeamsOf(empty, "web", true); err != nil || len(f.Teams) != 0 {
		t.Fatalf("a missing store: %+v %v", f, err)
	}
	if _, ok, err := LoadRoot(empty); ok || err != nil {
		t.Fatalf("a missing store has a root: %v %v", ok, err)
	}
	if any, err := AnyManager(empty); any || err != nil {
		t.Fatalf("a missing store has a manager: %v %v", any, err)
	}
}

// A STORE AN EARLIER BUILD WROTE IS STILL ANSWERED. Its rows carry no
// conversation names (or stale ones); a lookup reads every row instead, and
// the next write names them all again and goes back to the index.
func TestTheIndexIsRebuiltAfterAnEarlierBuildWrote(t *testing.T) {
	dir := t.TempDir()
	narrowTree(t, dir)
	h, err := acquire(Path(dir), false)
	must(t, err)
	// What an earlier build leaves: names it never wrote, and its own revision.
	_, err = h.db.Exec("UPDATE members SET conv = ''")
	must(t, err)
	_, err = h.db.Exec("UPDATE teams SET founder = ''")
	must(t, err)
	_, err = h.db.Exec("UPDATE meta SET rev = rev + 1")
	must(t, err)
	h.release()
	if f, err := LoadTeamsOf(dir, "web", false); err != nil || ids(f) != "alpha,shut" {
		t.Fatalf("unnamed rows: %q %v", ids(f), err)
	}
	if f, err := LoadConversationView(dir, "web"); err != nil || len(f.Teams) != 6 {
		t.Fatalf("an unnamed store's view is the whole store: %q %v", ids(f), err)
	}
	must(t, Update(dir, func(*File) error { return nil }))
	h, err = acquire(Path(dir), false)
	must(t, err)
	var unnamed int
	must(t, h.db.QueryRow("SELECT count(*) FROM members WHERE conv = ''").Scan(&unnamed))
	var rev, indexed int64
	must(t, h.db.QueryRow("SELECT rev, indexed FROM meta").Scan(&rev, &indexed))
	h.release()
	if unnamed != 0 || rev != indexed {
		t.Fatalf("%d rows unnamed, rev %d indexed %d", unnamed, rev, indexed)
	}
	if f, err := LoadConversationView(dir, "web"); err != nil || ids(f) != "root,alpha,beta,gone,delta,shut" {
		t.Fatalf("after the write: %q %v", ids(f), err)
	}
}

// A STORE THIS SCHEMA'S FIRST BUILD MADE GAINS THE COLUMNS AND INDEXES IT
// LACKS when it is opened, and keeps every team.
func TestAnEarlierStoreGainsItsColumnsAndIndexes(t *testing.T) {
	dir := t.TempDir()
	must(t, Save(dir, manyTeams(3)))
	h, err := acquire(Path(dir), false)
	must(t, err)
	for _, stmt := range []string{
		"DROP INDEX members_conv", "DROP INDEX teams_founder", "DROP INDEX teams_root", "DROP INDEX teams_open",
		"ALTER TABLE members DROP COLUMN conv", "ALTER TABLE teams DROP COLUMN founder", "ALTER TABLE meta DROP COLUMN indexed",
	} {
		_, err := h.db.Exec(stmt)
		must(t, err)
	}
	h.release()
	Close()
	if f := mustLoad(t, dir); len(f.Teams) != 3 {
		t.Fatalf("%d teams", len(f.Teams))
	}
	h, err = acquire(Path(dir), false)
	must(t, err)
	defer h.release()
	var n int
	must(t, h.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name IN ('members_conv','teams_founder','teams_root','teams_open')").Scan(&n))
	if n != 4 {
		t.Fatalf("%d of 4 indexes", n)
	}
	// manyTeams' members are /s/<team>/<n>: team 1's are all named "1".
	if f, err := LoadTeamsOf(dir, "1", false); err != nil {
		t.Fatal(err)
	} else if got := fmt.Sprint(len(f.Teams)); got != "1" {
		t.Fatalf("a member's teams after the columns came: %s", got)
	}
}
