package workspace

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

func directionTables(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name LIKE 'direction%' OR name=?", storeMetaTable).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// AN ORDINARY OPEN CREATES NO DIRECTION TABLE. The store a person already has
// stays at version 3 — readable by every build before this one — until a door
// that writes direction asks for version 4. A fresh store is made at version 3
// too, for the same reason.
func TestAnOrdinaryOpenLeavesTheStoreAtVersionThree(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ordinary.db")
	s := openTestStore(t, path)
	createTestCollection(t, s, "Kept")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenExisting(path); err != nil {
		t.Fatal(err)
	}
	if got := readUserVersion(t, path); got != ordinaryVersion {
		t.Fatalf("an ordinary open left version %d", got)
	}
	if n := directionTables(t, path); n != 0 {
		t.Fatalf("an ordinary open created %d direction objects", n)
	}
}

// THE VERSION 4 UPGRADE ADDS AND STAMPS IN ONE IMMEDIATE TRANSACTION. Rows
// written at version 3 read back unchanged; the store declares version 3
// builds permitted readers, and passes exactly the check a version 3 build
// makes of a newer store; and an ordinary open afterwards reads the version 4
// store as it stands rather than rejecting or rewriting it.
func TestEnsureDirectionUpgradesAVersionThreeStoreAndKeepsItReadableByVersionThree(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v3.db")
	s := openTestStore(t, path)
	folder := createTestCollection(t, s, "Folder")
	chat := Ref{Kind: ConversationKind, ID: "chat"}
	if err := s.AddPlacement(ctx, folder.ID, chat); err != nil {
		t.Fatal(err)
	}
	record := createTestContext(t, s, "Kept", "Written at version 3.", chat, chat)
	if err := s.EnsureDirection(); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureDirection(); err != nil {
		t.Fatalf("a second EnsureDirection on a version 4 store: %v", err)
	}
	if got := readUserVersion(t, path); got != schemaVersion {
		t.Fatalf("version %d after EnsureDirection", got)
	}
	if governing, err := s.GoverningCollections(ctx, chat); err != nil || len(governing) != 1 || governing[0].ID != folder.ID {
		t.Fatalf("placements after the upgrade: %v, %v", governing, err)
	}
	if found, err := s.ContextFor(ctx, []Ref{chat}); err != nil || len(found) != 1 || found[0].ID != record.ID {
		t.Fatalf("context after the upgrade: %v, %v", contextIDs(found), err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTestStore(t, path)
	if got := readUserVersion(t, path); got != schemaVersion {
		t.Fatalf("an ordinary open moved a version 4 store to %d", got)
	}
	var declared string
	if err := reopened.db.QueryRow("SELECT value FROM "+storeMetaTable+" WHERE key=?", minReaderVersion).Scan(&declared); err != nil {
		t.Fatal(err)
	}
	minimum, err := strconv.Atoi(declared)
	if err != nil || minimum != directionMinReader || minimum > ordinaryVersion {
		t.Fatalf("version 4 declares minimum reader %q", declared)
	}
	// This is the whole of what a version 3 build checks before using a newer
	// store: the declaration, then its own tables.
	if err := verifyVersion(reopened.db, minimum); err != nil {
		t.Fatalf("a version 3 build's own check fails on a version 4 store: %v", err)
	}
}

// AN INJECTED FAILURE LEAVES VERSION 3 INTACT. store_meta is the last object
// version 4 creates, so a foreign table under that name lets every direction
// table be created inside the transaction and then fails it. Afterwards the
// stamp is 3, not one direction table exists, the version 3 rows are there,
// and an ordinary open still works on the store.
func TestAFailedDirectionUpgradeLeavesAWorkingVersionThreeStore(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "interrupted.db")
	s := openTestStore(t, path)
	kept := createTestCollection(t, s, "Kept")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	writeRawDatabase(t, path, "CREATE TABLE store_meta (something TEXT)")
	s = openTestStore(t, path)
	if err := s.EnsureDirection(); err == nil {
		t.Fatal("upgraded onto an object it could not create")
	}
	if got := readUserVersion(t, path); got != ordinaryVersion {
		t.Fatalf("a failed upgrade stamped version %d", got)
	}
	if n := directionTables(t, path); n != 1 { // only the foreign store_meta
		t.Fatalf("a failed upgrade left %d direction objects behind", n-1)
	}
	if got, err := s.Collections(ctx); err != nil || len(got) != 1 || got[0] != kept {
		t.Fatalf("a failed upgrade lost version 3 rows: %v, %v", got, err)
	}
	if err := s.AddPlacement(ctx, kept.ID, Ref{Kind: ConversationKind, ID: "after"}); err != nil {
		t.Fatalf("the store stopped working as version 3: %v", err)
	}
}

// A store that claims version 4 and lost a direction table is refused, and not
// repaired underneath its owner.
func TestAVersionFourStoreMissingADirectionTableIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "damaged.db")
	s := openTestStore(t, path)
	if err := s.EnsureDirection(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	writeRawDatabase(t, path, "DROP TABLE direction_live; PRAGMA journal_mode=DELETE")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if s, err := Open(path); err == nil {
		_ = s.Close()
		t.Fatal("opened a version 4 store without direction_live")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("rewrote a store it refused: %v", err)
	}
}

// A version 1 store asked for direction goes to version 4 in one transaction,
// and racing handles agree on one schema.
func TestConcurrentDirectionUpgradesFromVersionOneHappenExactlyOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "race.db")
	writeRawDatabase(t, path, fmt.Sprintf(`%s
INSERT INTO collections(id,name) VALUES ('kept','Kept');
PRAGMA application_id=%d; PRAGMA user_version=1`, versionOneSchema, applicationID))
	const handles = 8
	var wg sync.WaitGroup
	failures := make([]error, handles)
	for i := 0; i < handles; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s, err := Open(path)
			if err != nil {
				failures[i] = err
				return
			}
			defer s.Close()
			failures[i] = s.EnsureDirection()
		}(i)
	}
	wg.Wait()
	for i, err := range failures {
		if err != nil {
			t.Errorf("handle %d: %v", i, err)
		}
	}
	if got := readUserVersion(t, path); got != schemaVersion {
		t.Fatalf("version %d", got)
	}
	s := openTestStore(t, path)
	var metaRows int
	if err := s.db.QueryRow("SELECT count(*) FROM " + storeMetaTable).Scan(&metaRows); err != nil || metaRows != 1 {
		t.Fatalf("store_meta rows %d, %v", metaRows, err)
	}
	if got, err := s.Collections(context.Background()); err != nil || len(got) != 1 {
		t.Fatalf("collections %v, %v", got, err)
	}
}
