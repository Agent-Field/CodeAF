package serve

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestP1_6_OneAuditLinePerOperation(t *testing.T) {
	g := newGitRig(t, nil)
	path := filepath.Join(g.st.Root(), "hosted-audit.jsonl")
	for i, service := range []string{"git-upload-pack", "git-receive-pack"} {
		if w := g.git("GET", "org/repo.git/info/refs?service="+service, g.access); w.Code != 200 {
			t.Fatalf("discovery: %d %s", w.Code, w.Body)
		}
		for range 3 {
			if w := g.git("POST", "org/repo/"+service, g.access); w.Code != 200 {
				t.Fatalf("pack: %d %s", w.Code, w.Body)
			}
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(raw), "\n") != i+1 || strings.Contains(string(raw), g.access) || strings.Contains(string(raw), personGitKey) {
			t.Fatal("audit line count or credential isolation failed")
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("audit mode = %o", info.Mode().Perm())
	}
	entries := gitAuditEntries(t, g, "abir")
	if len(entries) != 2 {
		t.Fatalf("got %d audit entries, want 2", len(entries))
	}
	for i, entry := range entries {
		want := []string{"git-upload-pack", "git-receive-pack"}[i]
		at, err := time.Parse(time.RFC3339Nano, entry.At)
		if err != nil || at.Location() != time.UTC || entry.Person != "abir" || entry.Chat != g.chat ||
			entry.Kind != "git" || entry.Host != g.host || entry.Repo != "org/repo" || entry.Service != want {
			t.Errorf("audit entry %d = %#v", i, entry)
		}
	}
	if got := gitAuditEntries(t, g, "mia"); len(got) != 0 {
		t.Errorf("another person saw audit entries: %#v", got)
	}
	if w := g.send("", "GET", "/v1/hosted/audit", nil, nil); w.Code != 401 {
		t.Errorf("unsigned audit request: %d %s", w.Code, w.Body)
	}
}

func TestP1_6_AuditShowsOnlyTheLatest200OwnLines(t *testing.T) {
	g := newGitRig(t, nil)
	for i := range 203 {
		g.h.hostedAudit("abir", g.chat, "git", g.host, fmt.Sprintf("org/repo-%03d", i), "git-upload-pack")
		g.h.hostedAudit("mia", "another-chat", "git", g.host, "org/other", "git-upload-pack")
	}
	entries := gitAuditEntries(t, g, "abir")
	if len(entries) != 200 {
		t.Fatalf("returned %d audit entries, want 200", len(entries))
	}
	for i, entry := range entries {
		if entry.Person != "abir" || entry.Repo != fmt.Sprintf("org/repo-%03d", i+3) {
			t.Fatalf("wrong audit entry at %d: %#v", i, entry)
		}
	}
}

func gitAuditEntries(t *testing.T, g *gitRig, person string) []hostedAuditEntry {
	t.Helper()
	w := g.send(person, "GET", "/v1/hosted/audit", nil, nil)
	var body struct {
		Entries []hostedAuditEntry `json:"entries"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Entries == nil {
		t.Fatalf("audit as %s: %d %s", person, w.Code, w.Body)
	}
	return body.Entries
}
