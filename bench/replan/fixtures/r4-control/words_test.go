package words

import "testing"

func TestCount(t *testing.T) {
	for text, want := range map[string]int{
		"one two three":    3,
		"":                 0,
		"   ":              0,
		"  lead and trail ": 3,
		"double  space":    2,
		"tab\tseparated":   2,
		"line\nbreak":      2,
	} {
		if got := Count(text); got != want {
			t.Errorf("Count(%q) = %d, want %d", text, got, want)
		}
	}
}
