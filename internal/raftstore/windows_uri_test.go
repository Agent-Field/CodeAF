package raftstore

import "testing"

func TestA4WindowsStoreUsesAbsoluteFileURI(t *testing.T) {
	for _, path := range []string{`C:\Users\Abir\relay store\raft\id_x\1.db`, `C:/Users/Abir/relay store/raft/id_x/1.db`} {
		u := sqliteURI(path)
		if got, want := u.String(), "file:///C:/Users/Abir/relay%20store/raft/id_x/1.db"; got != want {
			t.Errorf("A4: %q becomes %q, want %q", path, got, want)
		}
		if u.Host != "" {
			t.Errorf("A4: drive letter became URI authority: %q", u.Host)
		}
	}
}

func TestA4UnixColonDirectoryRemainsAnAbsolutePath(t *testing.T) {
	u := sqliteURI("/:/relay/1.db")
	if got := u.String(); got != "file:///:/relay/1.db" {
		t.Fatalf("A4: Unix colon directory was treated as a drive: %q", got)
	}
}
