package github

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// A stored reason must equal the next poll's identical reason after a JSON
// round trip, or the job is rewritten on every poll.
func TestStoredTextSurvivesTheStoreUnchanged(t *testing.T) {
	for _, in := range []string{
		strings.Repeat("a", 699) + "é and more",
		"GitHub said: \xff\xfe not text",
		strings.Repeat("😀", 800),
	} {
		got := storedText(in, 700)
		if !utf8.ValidString(got) || utf8.RuneCountInString(got) > 700 {
			t.Fatalf("not clipped to valid text: %q", got)
		}
		raw, _ := json.Marshal(got)
		var back string
		_ = json.Unmarshal(raw, &back)
		if back != got || storedText(in, 700) != back {
			t.Fatalf("changed through the store: %q -> %q", got, back)
		}
	}
}
