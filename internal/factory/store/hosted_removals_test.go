package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHostedRemovalKeepsTheLatestMark(t *testing.T) {
	root := t.TempDir()
	st, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := st.HostedRemovals(); err != nil || rows == nil || len(rows) != 0 {
		t.Fatalf("a table never written = %v, %v; want empty", rows, err)
	}
	earlier := time.Date(2026, 12, 1, 9, 30, 0, 0, time.UTC)
	later := earlier.Add(time.Second)
	for _, since := range []time.Time{earlier, later, earlier} {
		if err := st.MarkHostedRemoval("mia", since); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.MarkHostedRemoval("santosh", earlier); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.ClearHostedRemoval("mia", earlier); err != nil {
		t.Fatal(err)
	}
	if rows, err := reopened.HostedRemovals(); err != nil || len(rows) != 2 || !rows["mia"].Since.Equal(later) || !rows["santosh"].Since.Equal(earlier) {
		t.Fatalf("after a stale clear = %v, %v; want both marks and the later time for mia", rows, err)
	}
	if err := reopened.ClearHostedRemoval("mia", later); err != nil {
		t.Fatal(err)
	}
	if rows, err := st.HostedRemovals(); err != nil || len(rows) != 1 || !rows["santosh"].Since.Equal(earlier) {
		t.Fatalf("after a matching clear = %v, %v; want only the other mark", rows, err)
	}
	if info, err := os.Stat(filepath.Join(root, hostedRemovalsFile)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the file's mode = %v, %v; want 0600", info, err)
	}
}

func TestHostedRemovalReplacesAnUnreadableTable(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(st.Root(), hostedRemovalsFile)
	for _, raw := range []string{`{"rows":`, `null`, `{"other":{}}`, `{"rows":{"mia":{}}}`} {
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if rows, err := st.HostedRemovals(); rows != nil || !errors.Is(err, ErrHostedRemovalsUnreadable) {
			t.Fatalf("an unreadable table = %v, %v; want ErrHostedRemovalsUnreadable", rows, err)
		}
	}
	since := time.Now()
	if err := st.MarkHostedRemoval("mia", since); err != nil {
		t.Fatal(err)
	}
	if rows, err := st.HostedRemovals(); err != nil || len(rows) != 1 || !rows["mia"].Since.Equal(since) {
		t.Fatalf("after replacing an unreadable table = %v, %v; want the new mark", rows, err)
	}
}
