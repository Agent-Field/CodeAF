package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoFixedSubjectList fails if the stage grows a list of this person's
// subjects. Those words belong to one library; the parity test may replay
// them as input, and the stage itself may not.
func TestNoFixedSubjectList(t *testing.T) {
	banned := []string{"carnatic", "pareto", "telegram", "wisp", "sonnet", "cloudflare"}
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		low := strings.ToLower(string(body))
		for _, word := range banned {
			if strings.Contains(low, word) {
				t.Errorf("%s contains %q; the stage does not carry a fixed subject list", path, word)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
