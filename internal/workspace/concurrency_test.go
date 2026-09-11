package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// C1 — Separate commands open separate handles before adding membership, so
// all eighty racing writes must land instead of losing whichever opens time out.
func TestEightyConcurrentAddsFromSeparateHandlesAllLand(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "collections.db")
	seed := openTestStore(t, path)
	collection := createTestCollection(t, seed, "Storm")

	const writers = 80
	start := make(chan struct{})
	failures := make([]error, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			s, err := Open(path)
			if err != nil {
				failures[i] = err
				return
			}
			defer s.Close()
			failures[i] = s.Add(ctx, collection.ID, Ref{Kind: ConversationKind, ID: fmt.Sprintf("chat-%d", i)})
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range failures {
		if err != nil {
			t.Errorf("writer %d lost its membership: %v", i, err)
		}
	}
	members, err := seed.Members(ctx, collection.ID)
	if err != nil || len(members) != writers {
		t.Fatalf("racing writers kept %d of %d memberships: %v", len(members), writers, err)
	}
}

// C2 — BEGIN IMMEDIATE reserves the writer without changing state, and reads
// must still open and answer while that reservation belongs to another handle.
func TestReadsNeverAskForTheWriterLock(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "collections.db")
	writer := openTestStore(t, path)
	collection := createTestCollection(t, writer, "Inbox")
	ref := Ref{Kind: ConversationKind, ID: "chat"}
	if err := writer.Add(ctx, collection.ID, ref); err != nil {
		t.Fatal(err)
	}
	tx, err := writer.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	reader, err := Open(path)
	if err != nil {
		t.Fatalf("read open asked for the writer: %v", err)
	}
	defer reader.Close()
	collections, err := reader.Collections(ctx)
	if err != nil || len(collections) != 1 || collections[0] != collection {
		t.Fatalf("list while a writer is reserved: %v, %v", collections, err)
	}
	members, err := reader.Members(ctx, collection.ID)
	if err != nil || len(members) != 1 || members[0] != ref {
		t.Fatalf("show while a writer is reserved: %v, %v", members, err)
	}
	parents, err := reader.CollectionsFor(ctx, ref)
	if err != nil || len(parents) != 1 || parents[0] != collection {
		t.Fatalf("find while a writer is reserved: %v, %v", parents, err)
	}
}

// C3 — An existing zero-byte file is still blank storage, so reads and edits
// that cannot succeed must leave every byte alone until Create has work to keep.
func TestBlankStoreStaysUninitializedUntilCreate(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "empty.db")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	assertBlank := func(after string) {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("%s could not stat the blank file: %v", after, err)
		}
		if info.Size() != 0 {
			t.Fatalf("%s changed the blank file to %d bytes", after, info.Size())
		}
	}

	collections, err := s.Collections(ctx)
	if err != nil || collections == nil || len(collections) != 0 {
		t.Fatalf("blank list: %v, %v", collections, err)
	}
	assertBlank("Collections")
	if _, err := s.Members(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("blank members: %v", err)
	}
	assertBlank("Members")
	parents, err := s.CollectionsFor(ctx, Ref{Kind: ConversationKind, ID: "chat"})
	if err != nil || parents == nil || len(parents) != 0 {
		t.Fatalf("blank find: %v, %v", parents, err)
	}
	assertBlank("CollectionsFor")
	for name, edit := range map[string]func() error{
		"Add":    func() error { return s.Add(ctx, "missing", Ref{Kind: ConversationKind, ID: "chat"}) },
		"Remove": func() error { return s.Remove(ctx, "missing", Ref{Kind: ConversationKind, ID: "chat"}) },
		"Rename": func() error { return s.Rename(ctx, "missing", "Renamed") },
	} {
		if err := edit(); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s on a blank store: %v", name, err)
		}
		assertBlank(name)
	}
	if _, err := s.Create(ctx, "First"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Create did not leave storage: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("Create left storage at zero bytes")
	}
}

// C4 — The driver code, not its unstable English, identifies a lock failure;
// the translated answer carries the one configured bound in words a person can use.
func TestSQLiteBusyIsTranslatedWithoutWaitingTenSeconds(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "collections.db")
	s := openTestStore(t, path)
	createTestCollection(t, s, "Inbox")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Set("mode", "rw")
	q.Add("_pragma", "busy_timeout(1)")
	q.Set("_txlock", "immediate")
	u.RawQuery = q.Encode()
	blocked, err := sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer blocked.Close()
	_, driverErr := blocked.BeginTx(ctx, nil)
	if driverErr == nil {
		t.Fatal("second writer took the reserved lock")
	}
	err = storeError(driverErr)
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("busy error is not discoverable: %v", err)
	}
	for _, leaked := range []string{"SQLITE_BUSY", "(5)", "database is locked"} {
		if strings.Contains(err.Error(), leaked) {
			t.Errorf("busy answer leaked %q: %v", leaked, err)
		}
	}
	if want := fmt.Sprintf("%s (waited %s)", ErrBusy, busyTimeout); err.Error() != want {
		t.Fatalf("busy answer %q, want %q", err, want)
	}
}

// C5 — Open failures must name the selected path and the actionable reason,
// never SQLite's numeric code or its false report that memory ran out.
func TestOpenFailuresNameTheFileAndWhyItCouldNotOpen(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "directory")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	junk := filepath.Join(root, "junk.db")
	if err := os.WriteFile(junk, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		path string
		want string
	}{
		{"directory", directory, fmt.Sprintf("open collections: %s is not a regular database file", directory)},
		{"junk", junk, fmt.Sprintf("open collections: %s is not a collections database", junk)},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Open(test.path)
			if err == nil || err.Error() != test.want {
				t.Fatalf("open error %q, want %q", err, test.want)
			}
			for _, leaked := range []string{"(14)", "(26)", "(8)", "out of memory"} {
				if strings.Contains(err.Error(), leaked) {
					t.Errorf("open answer leaked %q: %v", leaked, err)
				}
			}
		})
	}

	if os.Geteuid() == 0 {
		t.Log("permission case skipped as root, where mode 0400 remains writable")
		return
	}
	readOnly := filepath.Join(root, "readonly.db")
	if err := os.WriteFile(readOnly, nil, 0400); err != nil {
		t.Fatal(err)
	}
	_, err := Open(readOnly)
	want := fmt.Sprintf("open collections: %s: permission denied", readOnly)
	if err == nil || err.Error() != want {
		t.Fatalf("open error %q, want %q", err, want)
	}
}

// C2/C6 — A reader arriving while a peer is building the schema must see the
// database before it or the database after it, never the half of a commit that
// has landed. Asked as loose statements the two pragmas can straddle another
// process's initialization, and our own version over an application id that has
// not appeared yet reads exactly like a database belonging to somebody else —
// which this store refuses. So the reader must never be refused here.
func TestAReaderArrivingDuringInitializationIsNeverToldTheStoreIsForeign(t *testing.T) {
	ctx := context.Background()
	// The driver initializes itself once per process, and doing that from nine
	// goroutines at once is a race inside modernc.org/sqlite rather than
	// anything this store decides. One store opened first takes it out of the
	// window under test.
	openTestStore(t, filepath.Join(t.TempDir(), "warm.db"))
	for round := 0; round < 40; round++ {
		path := filepath.Join(t.TempDir(), "first.db")
		start := make(chan struct{})
		var wg sync.WaitGroup
		const readers = 8
		refused := make([]error, readers+1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			s, err := Open(path)
			if err != nil {
				refused[readers] = err
				return
			}
			defer s.Close()
			_, refused[readers] = s.Create(ctx, "Inbox")
		}()
		for i := 0; i < readers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				s, err := Open(path)
				if err != nil {
					refused[i] = err
					return
				}
				defer s.Close()
				_, refused[i] = s.Collections(ctx)
			}(i)
		}
		close(start)
		wg.Wait()
		for i, err := range refused {
			if err != nil {
				t.Fatalf("round %d handle %d was refused during initialization: %v", round, i, err)
			}
		}
	}
}
