package tui3

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestKnowledgeSourcesRenderMarkdownWithoutChangingOriginal(t *testing.T) {
	a := knowledgeArticleApp(t)
	path := filepath.Join(a.agent.(*knowledgeTestAgent).vault.Directory(), "knowledge", "vault.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	quote := "## Retrieval\n\n**Pinecone** stores vectors.\n\n- Preserve citations\n- Follow connections\n\n```text\nfirst block line\n\nlast block line\n```\n\nRead [the guide](https://example.com/guide)."
	encoded, err := json.Marshal(quote)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.ReplaceAll(string(raw), `"Durable records use SQLite."`, string(encoded)))
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	a.height = 60
	drive(t, a, key("ctrl+r"), key("ctrl+o"))
	frame := ansi.Strip(a.View().Content)
	for _, marker := range []string{"**Pinecone**", "## Retrieval", "```text", "[the guide]("} {
		if strings.Contains(frame, marker) {
			t.Errorf("source reader exposes Markdown syntax %q: %s", marker, frame)
		}
	}
	for _, want := range []string{"Pinecone", "Retrieval", "Preserve citations", "first block line", "last block line", "https://example.com/guide"} {
		if !strings.Contains(frame, want) {
			t.Errorf("rendered source lost %q: %s", want, frame)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(raw) {
		t.Fatal("reading formatted sources changed the original vault")
	}
}
