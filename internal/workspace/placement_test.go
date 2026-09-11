package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestGoverningPlacementIsExplicitAndKeepsAlternatePaths(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "scope.db"))
	root := createTestCollection(t, s, "Company")
	a := createTestCollection(t, s, "Product")
	b := createTestCollection(t, s, "Marketing")
	chat := Ref{Kind: ConversationKind, ID: "chat"}
	for _, c := range []Collection{a, b} {
		if err := s.Add(ctx, c.ID, chat); err != nil {
			t.Fatal(err)
		}
		if err := s.Add(ctx, root.ID, Ref{Kind: CollectionKind, ID: c.ID}); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := s.GoverningCollections(ctx, chat); err != nil || len(got) != 0 {
		t.Fatalf("reference filing acquired authority: %v, %v", got, err)
	}
	for _, c := range []Collection{a, b} {
		if err := s.AddPlacement(ctx, c.ID, chat); err != nil {
			t.Fatal(err)
		}
		if err := s.AddPlacement(ctx, c.ID, chat); err != nil {
			t.Fatal(err)
		}
		if err := s.AddPlacement(ctx, root.ID, Ref{Kind: CollectionKind, ID: c.ID}); err != nil {
			t.Fatal(err)
		}
	}
	want := []GoverningCollection{{a, 0}, {b, 0}, {root, 1}}
	if got, err := s.GoverningCollections(ctx, chat); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, %v; want %v", got, err, want)
	}
	if err := s.AddPlacement(ctx, root.ID, chat); err != nil {
		t.Fatal(err)
	}
	want = []GoverningCollection{{root, 0}, {a, 0}, {b, 0}}
	if got, err := s.GoverningCollections(ctx, chat); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("shortest paths %v, %v", got, err)
	}
	for i := 0; i < 2; i++ {
		if err := s.RemovePlacement(ctx, a.ID, chat); err != nil {
			t.Fatal(err)
		}
	}
	if refs, err := s.Members(ctx, a.ID); err != nil || !reflect.DeepEqual(refs, []Ref{chat}) {
		t.Fatalf("removal altered references: %v, %v", refs, err)
	}
	if err := s.Remove(ctx, b.ID, chat); err != nil {
		t.Fatal(err)
	}
	want = []GoverningCollection{{root, 0}, {b, 0}}
	if got, err := s.GoverningCollections(ctx, chat); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("reference removal altered scope: %v, %v", got, err)
	}
}

// WHAT A PROPOSAL SAYS WILL GOVERN IS WHAT GOVERNS ONCE IT IS PLACED. A card
// names the folders its work will be placed in before anything exists to place,
// so it reads GoverningIfPlaced; the run reads GoverningCollections of the
// placed reference. The two must be the same answer, ancestry included.
func TestGoverningIfPlacedIsWhatAPlacementThereGoverns(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "scope.db"))
	company := createTestCollection(t, s, "Company")
	launch := createTestCollection(t, s, "Launch")
	marketing := createTestCollection(t, s, "Marketing")
	if err := s.AddPlacement(ctx, company.ID, Ref{Kind: CollectionKind, ID: launch.ID}); err != nil {
		t.Fatal(err)
	}
	for _, seed := range [][]string{{launch.ID}, {launch.ID, marketing.ID}, {marketing.ID}} {
		work := Ref{Kind: StandingKind, ID: fmt.Sprintf("work%d", len(seed))}
		if len(seed) == 1 && seed[0] == marketing.ID {
			work.ID = "marketing-only"
		}
		for _, id := range seed {
			if err := s.AddPlacement(ctx, id, work); err != nil {
				t.Fatal(err)
			}
		}
		before, err := s.GoverningIfPlaced(ctx, seed)
		if err != nil {
			t.Fatal(err)
		}
		placed, err := s.GoverningCollections(ctx, work)
		if err != nil || !reflect.DeepEqual(before, placed) {
			t.Fatalf("seed %v: before placing %v, placed %v (%v)", seed, before, placed, err)
		}
	}
	if got, err := s.GoverningIfPlaced(ctx, []string{launch.ID}); err != nil || !reflect.DeepEqual(got, []GoverningCollection{{launch, 0}, {company, 1}}) {
		t.Fatalf("Launch brings %v, %v; want Launch and its parent Company", got, err)
	}
	if _, err := s.GoverningIfPlaced(ctx, []string{launch.ID, "no-such-folder"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown folder was dropped from the seed instead of refused: %v", err)
	}
	if got, err := s.GoverningIfPlaced(ctx, nil); err != nil || len(got) != 0 {
		t.Fatalf("no folder governs nothing: %v, %v", got, err)
	}
}

func TestConcurrentGoverningParentsCannotCreateACycle(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "scope.db")
	s := openTestStore(t, path)
	peer := openTestStore(t, path)
	a := createTestCollection(t, s, "A")
	b := createTestCollection(t, s, "B")
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for _, edge := range []struct {
		s             *Store
		parent, child string
	}{{s, a.ID, b.ID}, {peer, b.ID, a.ID}} {
		workers.Add(1)
		go func(s *Store, parent, child string) {
			defer workers.Done()
			<-start
			results <- s.AddPlacement(ctx, parent, Ref{Kind: CollectionKind, ID: child})
		}(edge.s, edge.parent, edge.child)
	}
	close(start)
	workers.Wait()
	close(results)
	succeeded, rejected := 0, 0
	for err := range results {
		if err == nil {
			succeeded++
		} else if errors.Is(err, ErrCycle) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("success=%d cycle=%d", succeeded, rejected)
	}
	if err := s.AddPlacement(ctx, a.ID, Ref{Kind: CollectionKind, ID: a.ID}); !errors.Is(err, ErrCycle) {
		t.Fatalf("self cycle: %v", err)
	}
	if err := s.AddPlacement(ctx, a.ID, Ref{Kind: CollectionKind, ID: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing collection: %v", err)
	}
}

func TestGoverningPlacementKeepsTaskIdentityAndSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "scope.db")
	s := openTestStore(t, path)
	c := createTestCollection(t, s, "Project")
	one := Ref{Kind: TaskKind, ID: "1", SessionID: "one"}
	two := Ref{Kind: TaskKind, ID: "1", SessionID: "two"}
	if err := s.AddPlacement(ctx, c.ID, one); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTestStore(t, path)
	if got, err := s.GoverningCollections(ctx, two); err != nil || len(got) != 0 {
		t.Fatalf("scope crossed owning chats: %v, %v", got, err)
	}
	if got, err := s.GoverningCollections(ctx, one); err != nil || len(got) != 1 || got[0].ID != c.ID {
		t.Fatalf("scope lost: %v, %v", got, err)
	}
}

func TestPlacementMigrationNeverPromotesReferenceMemberships(t *testing.T) {
	for _, version := range []int{1, 2} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "legacy.db")
			schema := collectionSchema
			if version == 2 {
				schema += contextSchema
			}
			writeRawDatabase(t, path, schema+fmt.Sprintf(`
INSERT INTO collections(id,name) VALUES ('folder','Existing folder');
INSERT INTO memberships(collection_id,kind,ref_id,session_id) VALUES ('folder','conversation','chat','');
PRAGMA application_id=%d; PRAGMA user_version=%d;`, applicationID, version))
			s := openTestStore(t, path)
			ref := Ref{Kind: ConversationKind, ID: "chat"}
			if got, err := s.GoverningCollections(ctx, ref); err != nil || len(got) != 0 {
				t.Fatalf("migration granted scope: %v, %v", got, err)
			}
			if got, err := s.Members(ctx, "folder"); err != nil || !reflect.DeepEqual(got, []Ref{ref}) {
				t.Fatalf("migration lost references: %v, %v", got, err)
			}
			if version := readUserVersion(t, path); version != ordinaryVersion {
				t.Fatalf("version=%d", version)
			}
			if err := s.AddPlacement(ctx, "folder", ref); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPlacementMigrationPreservesContextAndRollsBackIncompleteUpgrade(t *testing.T) {
	ctx := context.Background()
	for _, collision := range []bool{false, true} {
		t.Run(fmt.Sprint(collision), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "v2.db")
			extra := ""
			if collision {
				extra = "CREATE TABLE placements(unexpected INTEGER);"
			}
			writeRawDatabase(t, path, collectionSchema+contextSchema+fmt.Sprintf(`
INSERT INTO contexts(id,revision) VALUES ('decision',1);
INSERT INTO context_revisions(context_id,revision,title,text,source_kind,source_id,source_session,withdrawn)
 VALUES ('decision',1,'A decision','Keep this source.','conversation','source-chat','',0);
INSERT INTO context_targets(context_id,revision,position,kind,ref_id,session_id)
 VALUES ('decision',1,0,'conversation','target-chat','');
%s
PRAGMA application_id=%d; PRAGMA user_version=2;`, extra, applicationID))
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			s, err := Open(path)
			if collision {
				if err == nil {
					s.Close()
					t.Fatal("accepted colliding placement schema")
				}
				after, readErr := os.ReadFile(path)
				if readErr != nil || !bytes.Equal(before, after) {
					t.Fatalf("failed upgrade changed the store: %v", readErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			records, err := s.ContextFor(ctx, []Ref{{Kind: ConversationKind, ID: "target-chat"}})
			if err != nil || len(records) != 1 || records[0].Source.ID != "source-chat" || records[0].Text != "Keep this source." {
				t.Fatalf("lost context during upgrade: %v, %v", records, err)
			}
		})
	}
}

// WHAT IS PLACED IN A FOLDER IS READ FROM THE FOLDER. Only direct placements
// are listed, never a reference and never a placement one folder down, and the
// read walks the placements key rather than the table.
func TestPlacedListsOnlyWhatIsPlacedDirectlyInTheFolder(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "placed.db"))
	top := createTestCollection(t, s, "Company")
	alpha := createTestCollection(t, s, "Alpha")
	work := Ref{Kind: StandingKind, ID: "f15796736d0826e5"}
	filed := Ref{Kind: ConversationKind, ID: "chat"}
	if err := s.AddPlacement(ctx, alpha.ID, work); err != nil {
		t.Fatal(err)
	}
	if err := s.AddPlacement(ctx, top.ID, Ref{Kind: CollectionKind, ID: alpha.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(ctx, alpha.ID, filed); err != nil {
		t.Fatal(err)
	}
	if got, more, err := s.Placed(ctx, alpha.ID, 0, 10); err != nil || more || !reflect.DeepEqual(got, []Ref{work}) {
		t.Fatalf("placed in Alpha: %+v, %v, %v", got, more, err)
	}
	if got, more, err := s.Placed(ctx, top.ID, 0, 10); err != nil || more || !reflect.DeepEqual(got, []Ref{{Kind: CollectionKind, ID: alpha.ID}}) {
		t.Fatalf("placed in Company: %+v, %v, %v", got, more, err)
	}
	if _, _, err := s.Placed(ctx, "deadbeef", 0, 10); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a missing folder answered %v", err)
	}
	// A WINDOW IS CUT IN KEY ORDER AND SAYS WHETHER MORE FOLLOWS. Five things
	// placed in Beta read back two at a time, and a window with no room still
	// answers whether anything is there.
	beta := createTestCollection(t, s, "Beta")
	var all []Ref
	for i := 0; i < 5; i++ {
		ref := Ref{Kind: StandingKind, ID: fmt.Sprintf("item-%d", i)}
		all = append(all, ref)
		if err := s.AddPlacement(ctx, beta.ID, ref); err != nil {
			t.Fatal(err)
		}
	}
	for _, w := range []struct {
		offset, limit int
		want          []Ref
		more          bool
	}{{0, 2, all[:2], true}, {2, 2, all[2:4], true}, {4, 2, all[4:], false}, {9, 2, []Ref{}, false}, {0, 0, []Ref{}, true}} {
		if got, more, err := s.Placed(ctx, beta.ID, w.offset, w.limit); err != nil || more != w.more || !reflect.DeepEqual(got, w.want) {
			t.Fatalf("the window %d+%d read %+v more=%v (%v), want %+v more=%v", w.offset, w.limit, got, more, err, w.want, w.more)
		}
	}
	if _, _, err := s.Placed(ctx, beta.ID, -1, 2); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a negative window answered %v", err)
	}
	rows, err := s.db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+placedQuery, alpha.ID, 11, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if len(plan) != 1 || !strings.HasPrefix(plan[0], "SEARCH placements USING ") || !strings.Contains(plan[0], "(collection_id=?)") {
		t.Fatalf("the placed read's plan is %q", plan)
	}
}
