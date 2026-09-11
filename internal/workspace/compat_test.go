package workspace

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// newerStore writes a store at the newest version this build knows, then
// stamps it one version newer the way a later build would: an extra table this
// build has never heard of, a row in it, and a store_meta declaration naming
// the oldest permitted reader (none at all when minimum is empty). The
// caller's rows are written first through the ordinary doors.
func newerStore(t *testing.T, path string, minimum string, populate func(*Store)) {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureDirection(); err != nil {
		t.Fatal(err)
	}
	if populate != nil {
		populate(s)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	meta := fmt.Sprintf("DELETE FROM %s WHERE key='%s';", storeMetaTable, minReaderVersion)
	if minimum != "" {
		meta = fmt.Sprintf("INSERT OR REPLACE INTO %s(key,value) VALUES ('%s','%s');", storeMetaTable, minReaderVersion, minimum)
	}
	writeRawDatabase(t, path, fmt.Sprintf(`%s
CREATE TABLE later_records (id TEXT PRIMARY KEY, body TEXT NOT NULL);
INSERT INTO later_records(id,body) VALUES ('kept','written by a newer build');
PRAGMA user_version=%d`, meta, schemaVersion+1))
}

// A ROLLBACK MUST NOT STRAND THE PERSON'S FOLDERS. A newer build that declares
// this one a permitted reader is opened, every door this version has works on
// it, and the newer build's own table and stamp are left exactly as they were:
// this build neither downgrades the store nor touches what it does not know.
func TestAnOlderBuildOpensANewerStoreThatDeclaresItReadable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "newer.db")
	var product Collection
	var record ContextRecord
	chat := Ref{Kind: ConversationKind, ID: "chat"}
	newerStore(t, path, fmt.Sprint(schemaVersion), func(s *Store) {
		product = createTestCollection(t, s, "Product")
		if err := s.AddPlacement(ctx, product.ID, chat); err != nil {
			t.Fatal(err)
		}
		record = createTestContext(t, s, "Kept", "Written before the newer build.", chat, Ref{Kind: CollectionKind, ID: product.ID})
	})
	for _, open := range []func(string) (*Store, error){Open, OpenExisting} {
		s, err := open(path)
		if err != nil {
			t.Fatalf("a newer store that declared this build readable was refused: %v", err)
		}
		launch := createTestCollection(t, s, "Launch")
		if err := s.Rename(ctx, launch.ID, "Launch plan"); err != nil {
			t.Fatal(err)
		}
		if err := s.Add(ctx, launch.ID, chat); err != nil {
			t.Fatal(err)
		}
		if err := s.Add(ctx, product.ID, Ref{Kind: CollectionKind, ID: launch.ID}); err != nil {
			t.Fatal(err)
		}
		if members, err := s.Members(ctx, launch.ID); err != nil || len(members) != 1 {
			t.Fatalf("members %v, %v", members, err)
		}
		if parents, err := s.CollectionsFor(ctx, chat); err != nil || len(parents) != 1 {
			t.Fatalf("parents %v, %v", parents, err)
		}
		if err := s.Remove(ctx, launch.ID, chat); err != nil {
			t.Fatal(err)
		}
		task := Ref{Kind: TaskKind, ID: "1", SessionID: "chat"}
		if err := s.AddPlacement(ctx, launch.ID, task); err != nil {
			t.Fatal(err)
		}
		governing, err := s.GoverningCollections(ctx, chat)
		if err != nil || len(governing) != 1 || governing[0].ID != product.ID {
			t.Fatalf("governing %v, %v", governing, err)
		}
		if err := s.RemovePlacement(ctx, launch.ID, task); err != nil {
			t.Fatal(err)
		}
		current, err := s.Context(ctx, record.ID)
		if err != nil {
			t.Fatal(err)
		}
		revised, err := s.ReviseContext(ctx, record.ID, current.Revision, "Kept", "Revised by the older build.", chat, []Ref{chat})
		if err != nil {
			t.Fatal(err)
		}
		if applies, err := s.ContextApplies(ctx, record.ID, revised.Revision, []Ref{chat}, false); err != nil || !applies {
			t.Fatalf("applies %v, %v", applies, err)
		}
		if page, err := s.ContextPage(ctx, []Ref{chat}, true, 0, 0); err != nil || len(page.Records) != 1 {
			t.Fatalf("page %+v, %v", page, err)
		}
		if _, err := s.WithdrawContext(ctx, record.ID, revised.Revision); err != nil {
			t.Fatal(err)
		}
		if history, err := s.ContextHistory(ctx, record.ID); err != nil || len(history) < 3 {
			t.Fatalf("history %v, %v", history, err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		record = createTestContext(t, openTestStore(t, path), "Again", "A second pass starts from a live record.", chat, chat)
	}
	if got := readUserVersion(t, path); got != schemaVersion+1 {
		t.Fatalf("the older build moved the stamp to %d", got)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var body, minimum string
	if err := db.QueryRow("SELECT body FROM later_records WHERE id='kept'").Scan(&body); err != nil || body != "written by a newer build" {
		t.Fatalf("the newer build's row: %q, %v", body, err)
	}
	if err := db.QueryRow("SELECT value FROM "+storeMetaTable+" WHERE key=?", minReaderVersion).Scan(&minimum); err != nil || minimum != fmt.Sprint(schemaVersion) {
		t.Fatalf("the declaration: %q, %v", minimum, err)
	}
}

// A newer store that has not declared this build readable is refused BY NAME,
// so a caller can say "this was written by a newer aforge" rather than a
// generic failure, and the file is left byte for byte as it was found — which
// also means the journal mode was not switched on a store that was refused.
func TestANewerStoreThisBuildMayNotReadIsRefusedByNameAndLeftAlone(t *testing.T) {
	for _, fixture := range []struct {
		name, minimum string
	}{
		{"needs a newer reader", fmt.Sprint(schemaVersion + 1)},
		{"says nothing about readers", ""},
		{"declares nonsense", "soon"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "newer.db")
			newerStore(t, path, fixture.minimum, nil)
			// The builder's own open left the store in WAL; put it back so the
			// refusal is judged against a store no build of this version touched.
			writeRawDatabase(t, path, "PRAGMA journal_mode=DELETE")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, open := range []func(string) (*Store, error){Open, OpenExisting} {
				s, err := open(path)
				if err == nil {
					_ = s.Close()
					t.Fatal("opened a newer store that did not declare this build readable")
				}
				if !errors.Is(err, ErrNewerStore) {
					t.Fatalf("refused without the name a caller can test for: %v", err)
				}
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("rewrote a newer store it refused: %v", err)
			}
		})
	}
	// Permission to read is not a promise the tables are there: a newer store
	// that declares this build readable and has lost one of its tables is
	// refused like a damaged store of this version.
	path := filepath.Join(t.TempDir(), "damaged-newer.db")
	newerStore(t, path, fmt.Sprint(schemaVersion), nil)
	writeRawDatabase(t, path, "DROP TABLE placements")
	if s, err := Open(path); err == nil {
		_ = s.Close()
		t.Fatal("opened a newer store with a table this build needs missing")
	}
}

// WAL IS SET ONCE AND KEPT IN THE FILE. A handle opened afterwards with none of
// this package's settings — an older build, a person's sqlite3 — finds the store
// already in WAL, which is what makes it a property of the store rather than of
// whichever process happened to open it.
func TestTheStoreIsInWriteAheadLoggingAndStaysThere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.db")
	s := openTestStore(t, path)
	var mode string
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal mode %q, %v", mode, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("a fresh handle found journal mode %q, %v", mode, err)
	}
}

// A READER HOLDING ITS SNAPSHOT DOES NOT LOCK A WRITER OUT. The reader keeps
// one snapshot open for longer than the one-second lock bound while another
// handle commits. Under the rollback journal the commit needs every reader gone
// and fails as "database is locked" once the bound runs out; the first subtest
// shows that is exactly what this store used to do, the second that it no
// longer does, and that the reader still sees only what it started with.
func TestAHeldReadSnapshotDoesNotLockAWriterOut(t *testing.T) {
	hold := busyTimeout + busyTimeout/2
	run := func(t *testing.T, reader, writer *sql.DB) error {
		t.Helper()
		ctx := context.Background()
		tx, err := reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		var before int
		if err := tx.QueryRow("SELECT count(*) FROM collections").Scan(&before); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() {
			_, err := writer.Exec("INSERT INTO collections(id,name) VALUES ('written','Written')")
			done <- err
		}()
		time.Sleep(hold)
		var during int
		if err := tx.QueryRow("SELECT count(*) FROM collections").Scan(&during); err != nil {
			t.Fatal(err)
		}
		if during != before {
			t.Fatalf("the snapshot moved from %d to %d rows while it was held", before, during)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		return <-done
	}
	t.Run("the rollback journal this replaces", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "rollback.db")
		if err := openTestStore(t, path).Close(); err != nil {
			t.Fatal(err)
		}
		writeRawDatabase(t, path, "PRAGMA journal_mode=DELETE")
		reader, writer := rawStoreHandle(t, path), rawStoreHandle(t, path)
		err := run(t, reader, writer)
		if err == nil || !strings.Contains(err.Error(), "locked") {
			t.Fatalf("expected the rollback journal to lock the writer out, got %v", err)
		}
	})
	t.Run("write-ahead logging", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "wal.db")
		reader, writer := openTestStore(t, path), openTestStore(t, path)
		if err := run(t, reader.db, writer.db); err != nil {
			t.Fatalf("a writer was locked out by a reader's snapshot: %v", err)
		}
	})
}

// rawStoreHandle opens a connection with this store's own settings and none of
// its initialization, so the journal mode it finds is the one on the disk.
func rawStoreHandle(t *testing.T, path string) *sql.DB {
	t.Helper()
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", storeDSN(absolute))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// One writer filing work while several handles read the organization — the
// shape of per-turn reads beside a person or a tick writing — finishes without
// a single lock refusal on either side. Writers still queue behind one another
// for the one-second bound; that is SQLite's single-writer rule, unchanged by
// the journal mode, and not what this test is about.
func TestConcurrentReadersBesideAWriterSeeNoLockRefusal(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "busy.db")
	writer := openTestStore(t, path)
	folder := createTestCollection(t, writer, "Folder")
	const readers, rounds = 4, 120
	var wg sync.WaitGroup
	failures := make(chan string, (readers*2+1)*rounds)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			chat := Ref{Kind: ConversationKind, ID: fmt.Sprintf("chat-%d", i)}
			if err := writer.AddPlacement(ctx, folder.ID, chat); err != nil {
				failures <- fmt.Sprintf("writer round %d: %v", i, err)
			}
		}
	}()
	for h := 0; h < readers; h++ {
		reader := openTestStore(t, path)
		wg.Add(1)
		go func(h int) {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				if _, err := reader.GoverningCollections(ctx, Ref{Kind: ConversationKind, ID: fmt.Sprintf("chat-%d", i)}); err != nil {
					failures <- fmt.Sprintf("reader %d round %d: %v", h, i, err)
				}
				if _, err := reader.Collections(ctx); err != nil {
					failures <- fmt.Sprintf("reader %d round %d: %v", h, i, err)
				}
			}
		}(h)
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Errorf("refused beside a concurrent writer: %s", failure)
	}
}

// THE PLANNER'S STATISTICS ARE REFRESHED AT CLOSE. A handle that used the
// placement index leaves sqlite_stat1 describing it, so the next process plans
// the governing walk from numbers rather than defaults.
func TestClosingAHandleRefreshesThePlannersStatistics(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "stats.db")
	s := openTestStore(t, path)
	folder := createTestCollection(t, s, "Folder")
	for i := 0; i < 50; i++ {
		if err := s.AddPlacement(ctx, folder.ID, Ref{Kind: ConversationKind, ID: fmt.Sprintf("chat-%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.GoverningCollections(ctx, Ref{Kind: ConversationKind, ID: "chat-7"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var rows int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_stat1 WHERE tbl='placements'").Scan(&rows); err != nil || rows == 0 {
		t.Fatalf("close left no statistics for placements: %d, %v", rows, err)
	}
}
