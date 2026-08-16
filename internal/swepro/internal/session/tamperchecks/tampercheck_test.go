package tampercheck

import "testing"

func TestKeptSpacesOutsideQuotesBypass(t *testing.T) {
	got := CheckTamper(TamperInput{
		ChangedFiles: []string{` "tox.ini" `},
		TaskText:     "fix app",
	})
	if !got.Clean {
		t.Fatalf("quote-order bypass was repaired: %+v", got)
	}
}

func TestKeptGitHubSegmentsNeedNotBeAdjacent(t *testing.T) {
	got := CheckTamper(TamperInput{
		ChangedFiles: []string{"docs/.github/notes/workflows/readme.md"},
		TaskText:     "fix docs",
	})
	if got.Clean || len(got.Findings) != 1 {
		t.Fatalf("non-adjacent segments no longer flagged: %+v", got)
	}
}

func TestRawHunkKeyUsesNullishNotTruthyFallback(t *testing.T) {
	got := CheckTamper(TamperInput{
		ChangedFiles: []string{`"package.json"`},
		TaskText:     "fix app",
		Hunks: map[string]string{
			`"package.json"`: "",
			"package.json":   `-"test": "jest"` + "\n" + `+"test": "true"`,
		},
	})
	if !got.Clean {
		t.Fatalf("empty raw-key hunk should suppress normalized fallback: %+v", got)
	}
}
