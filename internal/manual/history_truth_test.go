package manual

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Owner decisions 4 and 5 (2026-11-17). Documents and canvases keep History
// with Restore, and Add to Library never depends on learning. The gates only
// check that a name is mentioned, never that the claim around it is true, so
// this one reads the corpus, the workstream conversation's page and the file
// tools' words for the old denials: a chat repeats what it is told, and E3
// heard both denials long after they stopped being true.
func TestNothingStillDeniesHistoryOrTiesTheLibraryToLearning(t *testing.T) {
	denials := regexp.MustCompile(`(?i)no version history|mentions versions|keeps no earlier|bring an earlier version back|only when your Library is turned on|needs Library consent|still needs existing knowledge consent`)
	files, err := filepath.Glob("chat/*.md")
	if err != nil || len(files) == 0 {
		t.Fatalf("chat corpus: %v", err)
	}
	files = append(files, "../session/workstream_mark.go", "../session/tools_workstream_work.go", "../session/prompts/system.md")
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if m := denials.FindString(line); m != "" {
				t.Errorf("%s:%d still says %q", f, i+1, m)
			}
		}
	}
}
