package summarypath

import (
	"path/filepath"
	"testing"
)

// Text contract C5: punctuation is prose; native absolute paths are artifacts.
func TestTextContractSummaryPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.md")
	if got := Parse("(\"" + path + "\")."); got != path {
		t.Fatalf("path = %q, want %q", got, path)
	}
	for _, field := range []string{"report.md", "https://example.com/report.md", "cas://artifact"} {
		if got := Parse(field); got != "" {
			t.Fatalf("accepted non-native absolute path %q", field)
		}
	}
}
