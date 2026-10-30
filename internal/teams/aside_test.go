package teams

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A NEWER BUILD'S STORE IS NEVER SET ASIDE. An older build that opens it is
// told so ([ErrNewer]), reads and writes nothing, and the doors that move an
// unreadable store aside leave this one where the newer build will find it.
func TestANewerStoreIsNeverSetAside(t *testing.T) {
	dir := t.TempDir()
	must(t, Save(dir, []Team{{ID: "aaaaaaaaaaaa", Name: "a"}}))
	h, err := acquire(Path(dir), false)
	must(t, err)
	_, err = h.db.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion+1))
	h.release()
	must(t, err)
	Close()
	if _, err := LoadHued(dir, reservedForTest); !errors.Is(err, ErrNewer) || errors.Is(err, ErrUnreadable) {
		t.Fatalf("a newer store loaded with %v", err)
	}
	if aside, err := SetAside(dir); err == nil || aside != "" {
		t.Fatalf("a newer store was set aside to %q (%v)", aside, err)
	}
	if _, err := os.Stat(Path(dir)); err != nil {
		t.Fatalf("teams.db is gone: %v", err)
	}
	if err := Update(dir, func(*File) error { return nil }); !errors.Is(err, ErrNewer) {
		t.Fatalf("updated a newer store with %v", err)
	}
}

// AN I/O FAILURE IS NOT AN UNREADABLE STORE. A store SQLite cannot open for
// the moment (here a directory it cannot make its shared-memory file in) is
// the error it is, and nothing sets it aside for it.
func TestAnIOFailureIsNotUnreadable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root writes into a read-only directory")
	}
	dir := t.TempDir()
	must(t, Save(dir, []Team{{ID: "aaaaaaaaaaaa", Name: "a"}}))
	Close()
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(Path(dir) + suffix); err == nil {
			t.Fatalf("%s left after Close", suffix)
		}
	}
	must(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	_, err := Load(dir)
	if err == nil {
		t.Skip("this SQLite reads a WAL store in a read-only directory")
	}
	if errors.Is(err, ErrUnreadable) {
		t.Fatalf("an I/O failure is ErrUnreadable: %v", err)
	}
	must(t, os.Chmod(dir, 0o700))
	if aside, err := SetAside(dir); err == nil || aside != "" {
		t.Fatalf("a store that reads was set aside to %q (%v)", aside, err)
	}
	if f := mustLoad(t, dir); len(f.Teams) != 1 {
		t.Fatalf("the store lost its team: %+v", f.Teams)
	}
}

// foreignDatabase writes an SQLite database some other program made at path,
// in its own journal mode, and answers its bytes.
func foreignDatabase(t *testing.T, path string, pragmas ...string) []byte {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	must(t, err)
	for _, q := range append(pragmas, "CREATE TABLE things (id TEXT)", "INSERT INTO things VALUES ('x')") {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	must(t, db.Close())
	raw, err := os.ReadFile(path)
	must(t, err)
	return raw
}

// A FOREIGN DATABASE IS LOOKED AT, NEVER CHANGED. It is opened read-only and
// refused; WAL is turned on only for a blank store or this store's own, so
// the file keeps every byte and no -wal or -shm appears beside it. It is the
// kind of store that may be set aside.
func TestAForeignDatabaseIsRefusedUnchanged(t *testing.T) {
	for name, pragmas := range map[string][]string{
		"another program's": {"PRAGMA application_id = 7"},
		"no application id": nil,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			want := foreignDatabase(t, Path(dir), pragmas...)
			if _, err := Load(dir); !errors.Is(err, ErrUnreadable) {
				t.Fatalf("a foreign database loaded with %v", err)
			}
			if err := Update(dir, func(*File) error { return nil }); !errors.Is(err, ErrUnreadable) {
				t.Fatalf("a foreign database took a write: %v", err)
			}
			if got, _ := os.ReadFile(Path(dir)); !bytes.Equal(got, want) {
				t.Fatalf("the foreign file changed (header bytes 18-19 %v, were %v)", got[18:20], want[18:20])
			}
			for _, suffix := range []string{"-wal", "-shm"} {
				if _, err := os.Stat(Path(dir) + suffix); err == nil {
					t.Fatalf("%s appeared beside a foreign database", suffix)
				}
			}
			aside, err := SetAside(dir)
			must(t, err)
			if got, _ := os.ReadFile(aside); !bytes.Equal(got, want) {
				t.Fatal("the foreign file was set aside changed")
			}
		})
	}
}

// A DAMAGED STORE OF THIS BUILD'S IS UNREADABLE BUT STAYS: only a file that
// is not a database, or is another program's, is set aside.
func TestADamagedStoreIsUnreadableButNotSetAside(t *testing.T) {
	dir := t.TempDir()
	must(t, Save(dir, []Team{{ID: "aaaaaaaaaaaa", Name: "a"}}))
	h, err := acquire(Path(dir), false)
	must(t, err)
	_, err = h.db.Exec("DROP TABLE briefs")
	h.release()
	must(t, err)
	Close()
	if _, err := Load(dir); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("a damaged store loaded with %v", err)
	}
	if aside, err := SetAside(dir); err == nil || aside != "" {
		t.Fatalf("a damaged store was set aside to %q (%v)", aside, err)
	}
	if _, err := os.Stat(Path(dir)); err != nil {
		t.Fatalf("teams.db is gone: %v", err)
	}
}

// SETTING ASIDE WAITS ON THE TEAMS LOCK, so no writer is between its look and
// its renames, and moves the WAL with the store: what a writer committed but
// never folded into teams.db goes aside with it.
func TestSetAsideHoldsTheLockAndKeepsTheWal(t *testing.T) {
	dir := t.TempDir()
	foreignDatabase(t, Path(dir), "PRAGMA application_id = 7")
	release, held := make(chan struct{}), make(chan struct{})
	go func() {
		_ = withLock(dir, lockWait, func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	done := make(chan error, 1)
	go func() {
		_, err := SetAside(dir)
		done <- err
	}()
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(Path(dir)); err != nil {
		close(release)
		t.Fatalf("set aside while a writer held the lock: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	// The WAL goes with it: a foreign WAL-mode database whose last commit is
	// still in its WAL is whole where it was set aside.
	dir = t.TempDir()
	db, err := sql.Open("sqlite", Path(dir))
	must(t, err)
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, q := range []string{"PRAGMA journal_mode = WAL", "PRAGMA wal_autocheckpoint = 0", "PRAGMA application_id = 7", "CREATE TABLE things (id TEXT)", "INSERT INTO things VALUES ('kept')"} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	aside, err := SetAside(dir)
	must(t, err)
	check, err := sql.Open("sqlite", aside)
	must(t, err)
	defer check.Close()
	var got string
	if err := check.QueryRow("SELECT id FROM things").Scan(&got); err != nil || got != "kept" {
		t.Fatalf("the set-aside store lost its WAL's commit: %q %v (%s)", got, err, filepath.Base(aside))
	}
}
