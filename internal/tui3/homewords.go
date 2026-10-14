package tui3

// homewords.go is the handful of small words and joins home and its places
// share. They outlived the standing orders whose files they were first written
// in, and they live here so no surviving row has to reach into a file about a
// feature that is gone.

import (
	"os"
	"path/filepath"
	"strings"
)

// rowWordsFloor is how many cells a row keeps for the person's own words
// whatever its trailing facts want. Eighteen is about three words and an
// ellipsis — enough to tell two rows apart, which is the only job the label has
// on a column this narrow.
const rowWordsFloor = 18

// joinDot joins two clauses with the separator this whole surface uses, and
// drops either of them when it is not there.
func joinDot(left, right string) string {
	switch {
	case left == "":
		return right
	case right == "":
		return left
	}
	return left + " · " + right
}

// homeStripWord is HOW A FOOT NAMES THE ROW'S `→` STRIP, and it is one function
// because several feet name it.
//
// EVERY CLAUSE ON A HINT LINE IS ONE KEY AND WHAT IT DOES. A foot that put the
// strip's second verb in the slot where a clause's KEY goes would have a person
// read that verb as one with no key, press its letter, and get nothing — the
// letters belong to the strip and only appear once `→` has drawn it. What is
// offered is one key over one strip, and the verbs behind it are listed under it
// in the grammar the card beside it already uses ([homeVerbsWord], home.go).
func homeStripWord(words ...string) string {
	if len(words) == 0 {
		return ""
	}
	return homeVerbsWord + ": " + strings.Join(words, ", ")
}

// bareName is what a heading says for a folder: its last element, and `~` for
// the home directory itself. It is [session.projectName]'s answer said again on
// this side of the seam, because that function is unexported and a heading that
// named the same directory two different ways on two rows of one screen would be
// the screen arguing with itself.
func bareName(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	if house, err := os.UserHomeDir(); err == nil && filepath.Clean(house) == filepath.Clean(path) {
		return "~"
	}
	if name := filepath.Base(path); name != "" && name != "." && name != string(filepath.Separator) {
		return name
	}
	return path
}
