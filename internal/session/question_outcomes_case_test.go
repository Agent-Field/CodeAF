package session

import "testing"

// A receipt repeats the person's choice. A path or a code word keeps its case,
// because "Src/c.txt" names a different file than the one that was chosen.
func TestReceiptWordsKeepPathsAndCodeWordsLiteral(t *testing.T) {
	for in, want := range map[string]string{
		"not now":    "Not now",
		"src/c.txt":  "src/c.txt",
		"notes.md":   "notes.md",
		"json":       "json",
		"Allow once": "Allow once",
		"":           "",
	} {
		if got := sentenceCase(in); got != want {
			t.Errorf("sentenceCase(%q) = %q, want %q", in, got, want)
		}
	}
}
