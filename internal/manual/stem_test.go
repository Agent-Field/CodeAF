package manual

import "testing"

func TestTheStemmerMeetsItself(t *testing.T) {
	mustMeet := [][2]string{
		{"refuse", "refused"},
		{"refuse", "refusing"},
		{"refuse", "refuses"},
		{"size", "sizing"},
		{"type", "typed"},
		{"close", "closed"},
		{"save", "saved"},
		{"move", "moved"},
		{"delete", "deleted"},
		{"remove", "removed"},
		{"compile", "compiled"},
		{"rename", "renamed"},
		{"share", "shared"},
		{"note", "noted"},
	}
	// Pause, change, merge, settle, queue and paste remain accepted misses: the
	// two Porter steps deliberately do not reduce those pairs to one stem.
	for _, pair := range mustMeet {
		left, right := stem(pair[0]), stem(pair[1])
		if left != right {
			t.Errorf("stem(%q) = %q and stem(%q) = %q; they must meet", pair[0], left, pair[1], right)
		}
	}

	mustNotMerge := [][2]string{
		{"paste", "past"},
		{"bare", "bar"},
		{"plane", "plan"},
		{"pane", "pan"},
		{"lane", "lan"},
		{"note", "not"},
		{"care", "car"},
	}
	for _, pair := range mustNotMerge {
		left, right := stem(pair[0]), stem(pair[1])
		if left == right {
			t.Errorf("stem(%q) = %q and stem(%q) = %q; they must not merge", pair[0], left, pair[1], right)
		}
	}
	if words := tokenize("on"); len(words) != 0 {
		t.Errorf("tokenize(%q) = %q; the stop word must be empty", "on", words)
	}
	if words := tokenize("one"); len(words) == 0 {
		t.Errorf("tokenize(%q) is empty; the ordinary word must remain", "one")
	}
}
