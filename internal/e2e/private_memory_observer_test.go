//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateMemoryObserverKeepsReplyAcrossAuthoredNotes(t *testing.T) {
	home := t.TempDir()
	raw := `{"role":"user","content":"Draft a Quartz example"}
{"role":"assistant","toolCalls":[{"function":{"name":"manual"}}]}
{"role":"tool","content":"Mock manual evidence"}
{"role":"user","content":"Session state changed","note":true}
{"role":"assistant","content":"Use mock customer identifiers."}
`
	if err := os.WriteFile(filepath.Join(home, "transcript.jsonl"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	answer, tools := privateMemoryJournalAnswer(home, "Draft a Quartz example;")
	if answer != "Use mock customer identifiers." || len(tools) != 1 || tools[0] != "manual" {
		t.Fatalf("observer lost completed reply across punctuation or authored note: answer=%q tools=%v", answer, tools)
	}
	if privateMemoryLookupUsed(tools) {
		t.Fatal("manual lookup was misclassified as memory-assisted recall")
	}
	if !privateMemoryLookupUsed([]string{"search_memories"}) {
		t.Fatal("memory lookup was not classified as assisted recall")
	}
}

func TestPrivateMemoryGroundTruthNeverCreditsLaterSearchResults(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "logs", "trace", "mock-run", "calls")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	first := `{"ts":"2026-01-01T00:00:01Z","request":{"messages":[{"role":"system","content":"<memory>\n- \"Other mock rule\"\n</memory>"},{"role":"user","content":"Explain Quartz"}],"tools":[{}]}}`
	later := `{"ts":"2026-01-01T00:00:02Z","request":{"messages":[{"role":"system","content":"<memory>\n- \"Other mock rule\"\n</memory>"},{"role":"user","content":"Explain Quartz"},{"role":"tool","content":"Quartz uses mock amber identifiers"}],"tools":[{}]}}`
	for name, raw := range map[string]string{"one.json": first, "two.json": later} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	present, block, path := privateMemoryPromptGroundTruth(home, "Explain Quartz;", "Quartz uses mock amber identifiers")
	if present == nil || *present || block == "" || filepath.Base(path) != "one.json" {
		t.Fatalf("later lookup incorrectly counted as anticipatory context: present=%v path=%s", present, path)
	}
}
