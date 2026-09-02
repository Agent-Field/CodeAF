package manual

import "testing"

func TestTheStemmerMeetsItself(t *testing.T) {
	pairs := [][2]string{
		{"refuse", "refused"},
		{"refuse", "refusing"},
		{"refuse", "refuses"},
		{"size", "sizing"},
		{"pause", "paused"},
		{"close", "closed"},
		{"save", "saved"},
		{"move", "moved"},
		{"delete", "deleted"},
		{"remove", "removed"},
		{"merge", "merged"},
		{"settle", "settled"},
		{"compile", "compiled"},
		{"paste", "pasted"},
		{"queue", "queued"},
		{"change", "changed"},
		{"type", "typed"},
		{"share", "shared"},
		{"rename", "renamed"},
	}
	for _, pair := range pairs {
		left, right := stem(pair[0]), stem(pair[1])
		if left != right {
			t.Errorf("stem(%q) = %q and stem(%q) = %q; they must meet", pair[0], left, pair[1], right)
		}
	}

	notPairs := [][2]string{
		{"one", "on"},
		{"use", "us"},
	}
	for _, pair := range notPairs {
		left, right := stem(pair[0]), stem(pair[1])
		if left == right {
			t.Errorf("stem(%q) = %q and stem(%q) = %q; they must not collapse", pair[0], left, pair[1], right)
		}
	}
	if words := tokenize("on"); len(words) != 0 {
		t.Errorf("tokenize(%q) = %q; the stop word must be empty", "on", words)
	}
	if words := tokenize("one"); len(words) == 0 {
		t.Errorf("tokenize(%q) is empty; the ordinary word must remain", "one")
	}
}
