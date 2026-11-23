package identity

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// L4-H2a: indexing an empty conversation does not turn startup bookkeeping into a chat.
func TestL4EmptySessionIndexCanJoin(t *testing.T) {
	h := t.TempDir()
	makeGraph(t, h, []string{`CREATE TABLE sessions (id TEXT)`})
	// This synthetic graph predates the actual event schema. The real first-start
	// regression covers startup events through Store.Open instead.
	db, err := sql.Open("sqlite", filepath.Join(h, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE events`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	put(t, filepath.Join(h, "device-name"), "desk-b")
	if ok, err := Joinable(h); err != nil || !ok {
		t.Fatalf("empty indexed chat: %v %v", ok, err)
	}
	db, err = sql.Open("sqlite", filepath.Join(h, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions VALUES ('sent')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if ok, err := Joinable(h); ok || err != nil {
		t.Fatalf("stored session: %v %v", ok, err)
	}
	put(t, filepath.Join(h, cellDir, "transcript.jsonl"), startedFiles[cellDir+"/transcript.jsonl"]+`{"type":"message","role":"user","content":"hello"}`+"\n")
	if ok, err := Joinable(h); err != nil || ok {
		t.Fatalf("submitted chat: %v %v", ok, err)
	}
}

// L4-H2b: counting chats never scans user repositories or counts their transcript files.
func TestL4CountOnlyChatTranscripts(t *testing.T) {
	h := t.TempDir()
	sent := `{"type":"message","role":"user","content":"hello"}` + "\n"
	put(t, filepath.Join(h, "v3/projects/p/chat/.cell/transcript.jsonl"), sent)
	put(t, filepath.Join(h, "v3/projects/p/chat/.cell/env/repo/transcript.jsonl"), sent)
	put(t, filepath.Join(h, "v3/projects/p/empty/.cell/transcript.jsonl"), `{"type":"session"}`+"\n")
	if n, err := ChatCount(h); err != nil || n != 1 {
		t.Fatalf("count=%d %v", n, err)
	}
}
