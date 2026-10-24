package plandb

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestOpenReadsCommittedPlanWhileAnotherWriterIsHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.db")
	first, err := Open(path, "mock", "root", "Mock task", "")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	tx, err := first.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE tasks SET title='not committed' WHERE id='root'"); err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	second, err := Open(path, "", "", "", "")
	if err != nil {
		t.Fatalf("reading open waited behind writer (%s): %v", time.Since(began), err)
	}
	defer second.Close()
	if time.Since(began) > time.Second {
		t.Fatalf("reading open waited %s", time.Since(began))
	}
	root, err := second.Show("root")
	if err != nil || root.Title != "Mock task" {
		t.Fatalf("committed snapshot: %+v %v", root, err)
	}
}

func TestOpenPreservesLiteralDatabasePath(t *testing.T) {
	for _, name := range []string{"plan%23.db", "plan#question?.db", "plan & space ü.db"} {
		t.Run(name, func(t *testing.T) {
			if runtime.GOOS == "windows" && strings.Contains(name, "?") {
				t.Skip("question mark is not a valid Windows filename")
			}
			path := filepath.Join(t.TempDir(), name)
			store, err := Open(path, "mock", "root", "Mock task", "")
			if err != nil {
				t.Fatal(err)
			}
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err = os.Stat(path); err != nil {
				t.Fatalf("literal database missing: %v", err)
			}
			reopened, err := Open(path, "", "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
		})
	}
}

func TestOpenRelativeDatabasePathStaysInWorkingDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	store, err := Open("plan%23.db", "mock", "root", "Mock task", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("plan%23.db"); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open("plan%23.db", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
}

func TestOpenBesideWriterStillRefusesContendedWritesTruthfully(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.db")
	first, err := Open(path, "mock", "root", "Mock task", "")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	tx, err := first.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	second, err := Open(path, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	// Shorten only this test's existing SQLite timeout; production retains5s.
	if _, err = second.db.Exec("PRAGMA busy_timeout=25"); err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	if tx, err := second.beginWriteUntil(time.Now().Add(10 * time.Millisecond)); err == nil {
		tx.Rollback()
		t.Fatal("contended write falsely reported success")
	} else if !isBusy(err) || !strings.Contains(err.Error(), "retry this command") {
		t.Fatalf("write exhaustion lost cause or recovery guidance: %v", err)
	}
	if time.Since(began) > time.Second {
		t.Fatal("write ignored its configured busy bound")
	}
	root, err := second.Show("root")
	if err != nil || root.Description != "" {
		t.Fatalf("failed write changed plan: %+v %v", root, err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err = second.Amend("root", "lands after release"); err != nil {
		t.Fatalf("write did not recover after lock release: %v", err)
	}
	if _, err = second.rdb.Exec("UPDATE tasks SET title='unexpected'"); err == nil {
		t.Fatal("read handle accepted a write")
	}
}
