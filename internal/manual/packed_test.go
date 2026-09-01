package manual

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryPageReadsBackWhole checks the corpus the way the running chat does
// — through the indexed text rather than through the archive — so a page that
// unpacked short would fail here even if its bytes matched.
func TestEveryPageReadsBackWhole(t *testing.T) {
	for corpus, folder := range map[*Corpus]string{
		resident: "pages",
		chat:     "chat",
	} {
		entries, err := os.ReadDir(folder)
		if err != nil {
			t.Fatalf("read %s: %v", folder, err)
		}
		for _, entry := range entries {
			name := strings.TrimSuffix(entry.Name(), ".md")
			source, err := os.ReadFile(filepath.Join(folder, entry.Name()))
			if err != nil {
				t.Fatalf("read %s: %v", entry.Name(), err)
			}
			text, ok := corpus.Page(name)
			if !ok {
				t.Errorf("%s/%s is not in the corpus", folder, entry.Name())
				continue
			}
			want := strings.TrimSpace(strings.ReplaceAll(string(source), "\r\n", "\n"))
			if text != want {
				t.Errorf("%s/%s reads back as %d bytes, the file is %d", folder, entry.Name(), len(text), len(want))
			}
		}
	}
}
