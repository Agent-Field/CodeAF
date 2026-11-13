package teams

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

// A SNAPSHOT OF NOTHING IS EMPTY AND MAKES NOTHING.
func TestSnapshotMissingIsEmptyAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	f, rev, err := ReadSnapshot(context.Background(), dir)
	if err != nil || f == nil || len(f.Teams) != 0 || rev != MissingStamp {
		t.Fatalf("missing store: %+v %q %v", f, rev, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a snapshot of nothing left %v", entries)
	}
}

// A SNAPSHOT REPAIRS NOTHING AND WRITES NOTHING: a store holding a team a load
// would repair answers it as stored, at the stamp a reader compares with.
func TestSnapshotIsTheStoreAsItIs(t *testing.T) {
	dir := t.TempDir()
	must(t, storeRaw(dir, `{"version":2,"teams":[{"id":"aaaaaaaaaaaa","name":"a","manager":"gone","members":[{"key":"k1"}]}]}`))
	before := Stamp(dir)
	f, rev, err := ReadSnapshot(context.Background(), dir)
	must(t, err)
	if string(rev) != before || len(f.Teams) != 1 || f.Teams[0].Manager != "gone" {
		t.Fatalf("snapshot %q (stamp %q): %+v", rev, before, f.Teams)
	}
	if Stamp(dir) != before {
		t.Fatal("a snapshot wrote")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := ReadSnapshot(ctx, dir); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled read answered %v", err)
	}
}

// THE OVERVIEW'S PAGES ARE CUT IN SQL: newest made first and then by id, the
// root, closed teams and the factory's teams left out, each page bounded and
// the count the whole collection's.
func TestPageTeamsCutsInOrderAndCountsAll(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	teams := []Team{{ID: "root00000000", Name: "All teams", Root: true, Manager: "boss", Members: []Member{{Key: "boss", Handle: "boss"}}}}
	for i := range 25 {
		teams = append(teams, Team{ID: fmt.Sprintf("crew%08d", i), Name: fmt.Sprint("crew ", i), Parent: "root00000000", Made: base.Add(time.Duration(i) * time.Hour),
			Members: []Member{{Key: fmt.Sprint("k", i), Handle: fmt.Sprint("m", i)}}})
	}
	teams = append(teams,
		Team{ID: "closed000000", Name: "gone", Parent: "root00000000", State: TeamClosed, Made: base.Add(99 * time.Hour)},
		Team{ID: "factory00000", Name: "factory", Parent: "root00000000", FactoryManaged: true, Made: base.Add(98 * time.Hour)},
		Team{ID: "kid000000000", Name: "kid", Parent: "crew00000024", Made: base},
	)
	must(t, Save(dir, teams))
	var seen []string
	offset, stamp := 0, Revision(Stamp(dir))
	for {
		page, err := PageTeams(context.Background(), dir, "", 10, func(rev Revision, parent Team, total int) (int, error) {
			if parent.ID != "root00000000" || total != 25 || rev != stamp {
				return 0, fmt.Errorf("asked at %q under %q of %d", rev, parent.ID, total)
			}
			return offset, nil
		})
		must(t, err)
		if page.Root == nil || page.Root.Manager != "boss" || len(page.Root.Members) != 1 || page.Parent != "root00000000" {
			t.Fatalf("root %+v parent %q", page.Root, page.Parent)
		}
		if len(page.Teams) > 10 {
			t.Fatalf("a page of %d", len(page.Teams))
		}
		for _, team := range page.Teams {
			seen = append(seen, team.ID)
			if len(team.Members) != 1 {
				t.Fatalf("%s came with %d members", team.ID, len(team.Members))
			}
		}
		// The root counts its open children, the factory's among them.
		if page.Open["root00000000"] != 26 || offset == 0 && page.Open["crew00000024"] != 1 {
			t.Fatalf("open children %v", page.Open)
		}
		offset += len(page.Teams)
		if offset >= page.Total {
			break
		}
	}
	if len(seen) != 25 || seen[0] != "crew00000024" || seen[24] != "crew00000000" {
		t.Fatalf("paged %v", seen)
	}
	if _, err := PageTeams(context.Background(), dir, "nope00000000", 10, func(Revision, Team, int) (int, error) { return 0, nil }); !errors.Is(err, ErrNoTeam) {
		t.Fatalf("a missing parent answered %v", err)
	}
	if _, err := PageTeams(context.Background(), dir, "closed000000", 10, func(Revision, Team, int) (int, error) { return 0, nil }); !errors.Is(err, ErrClosed) {
		t.Fatalf("a closed parent answered %v", err)
	}
}

// A TEAM'S MEMBERS PAGE ONCE PER CONVERSATION, FIRST STORED FIRST.
func TestPageMembersIsOncePerConversation(t *testing.T) {
	dir := t.TempDir()
	members := []Member{{Key: "a", Handle: "aa"}, {Key: ""}, {Key: "b", Handle: "bb"}, {Key: "c", Handle: "cc"}}
	must(t, storeRaw(dir, `{"version":2,"teams":[{"id":"aaaaaaaaaaaa","name":"crew","members":[`+
		`{"key":"a","handle":"aa"},{"key":""},{"key":"b","handle":"bb"},{"key":"a","handle":"again"},{"key":"c","handle":"cc"}]}]}`))
	var got []string
	for offset := 0; ; {
		page, err := PageMembers(context.Background(), dir, "aaaaaaaaaaaa", 2, func(_ Revision, team Team, total int) (int, error) {
			if team.ID != "aaaaaaaaaaaa" || total != 3 {
				return 0, fmt.Errorf("team %q of %d", team.ID, total)
			}
			return offset, nil
		})
		must(t, err)
		for _, m := range page.Members {
			got = append(got, m.Key+"="+m.Handle)
		}
		offset += len(page.Members)
		if offset >= page.Total {
			break
		}
	}
	if fmt.Sprint(got) != "[a=aa b=bb c=cc]" {
		t.Fatalf("members %v (stored %d)", got, len(members))
	}
}
