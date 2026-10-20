package tui3

import (
	"os"
	"path/filepath"
	"testing"
)

// Flat --session launches may have no meta.json or old engine citation. The
// raw launch spelling must travel with the composer even after normalization.
func TestReviewRawLaunchKeepsDraftWithoutCitations(t *testing.T) {
	workspace, alias, old, transcript := reviewDraftUpgrade(t)
	if err := os.RemoveAll(filepath.Join(os.Getenv("CODEAF_HOME"), "v3")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("a flat session's unfinished sentence"), 0600); err != nil {
		t.Fatal(err)
	}
	next, _, _ := roomApp(t)
	next.workspace, next.file = workspace, transcript
	next.draftFile = DraftFile(filepath.Dir(old), workspace)
	next.draftAliases = []string{alias}
	next.restoreDraft()
	if got := next.input.String(); got != "a flat session's unfinished sentence" {
		t.Fatalf("raw launch draft: %q", got)
	}
}
