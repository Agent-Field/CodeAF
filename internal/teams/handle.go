package teams

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// A handle is how a member is named inside its team, in the Traffic log and to
// the team tools: short enough to type, stable enough to refer back to. It is
// derived from the member's title when the member first has one, and it is
// never changed automatically after that, so a line in the log keeps meaning
// the member it meant. [File.SetHandle] is the only way to change one.
//
// A member that joins before its conversation has a title has no handle yet; it
// takes one the first time it is saved with a title.

// Handle lengths, in characters.
const (
	HandleMin = 2
	HandleMax = 12
)

// Reserved addresses are words the Traffic log uses for someone who is not a
// member, so no member may take one as its handle.
var reservedHandles = map[string]bool{
	FromManager: true, FromYou: true, FromSystem: true, ToEveryone: true, ToRoom: true,
}

// stopWords are dropped when a handle is derived from a title.
var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true, "but": true,
	"of": true, "to": true, "in": true, "on": true, "for": true, "with": true,
	"at": true, "by": true, "from": true, "into": true, "about": true, "as": true,
	"is": true, "are": true, "be": true, "it": true, "this": true, "that": true,
	"my": true, "our": true, "your": true, "its": true, "please": true,
	"can": true, "could": true, "would": true, "should": true, "will": true,
	"i": true, "we": true, "you": true, "me": true, "us": true, "let": true,
	"lets": true, "some": true, "how": true, "what": true, "why": true,
}

// ValidHandle says what is wrong with h as a handle, or nil. A handle is
// lowercase letters, digits and hyphens, starts with a letter or digit, is
// HandleMin to HandleMax characters, and is not a reserved address.
func ValidHandle(h string) error {
	if err := handleShape(h); err != nil {
		return err
	}
	if reservedHandles[h] {
		return fmt.Errorf("%s is reserved", h)
	}
	return nil
}

// handleShape is [ValidHandle] without the reserved words.
func handleShape(h string) error {
	if len(h) < HandleMin || len(h) > HandleMax {
		return fmt.Errorf("a handle is %d to %d characters", HandleMin, HandleMax)
	}
	for i, r := range h {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '-' && i > 0:
		default:
			return errors.New("a handle is lowercase letters, digits and hyphens")
		}
	}
	return nil
}

// handleProblem says why h cannot be the handle of the member with key in t.
func handleProblem(t Team, key, h string) error {
	if err := ValidHandle(h); err != nil {
		return err
	}
	for _, m := range t.Members {
		if m.Key != key && m.Handle == h {
			return fmt.Errorf("%s is already the handle of another member", h)
		}
	}
	return nil
}

// DeriveHandle is the handle a title suggests, before collisions and reserved
// words are taken into account: its first meaningful word, joined to the
// second when the first is short, lowercased and cut to HandleMax. A title
// with no usable word gives "chat"; an empty title gives "".
func DeriveHandle(title string) string {
	if strings.TrimSpace(title) == "" {
		return ""
	}
	words := strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	var meaningful []string
	for _, w := range words {
		if !stopWords[w] {
			meaningful = append(meaningful, w)
		}
	}
	if len(meaningful) == 0 {
		meaningful = words
	}
	if len(meaningful) == 0 {
		return "chat"
	}
	base := meaningful[0]
	if len(base) < 4 && len(meaningful) > 1 && len(base)+1+len(meaningful[1]) <= HandleMax {
		base += "-" + meaningful[1]
	}
	base = cutHandle(base, HandleMax)
	if handleShape(base) != nil {
		return "chat"
	}
	return base
}

// cutHandle is h cut to n characters with no hyphen left at the end.
func cutHandle(h string, n int) string {
	if len(h) > n {
		h = h[:n]
	}
	return strings.TrimRight(h, "-")
}

// uniqueHandle is base, or base with the lowest number from 2 that no member
// of t other than key uses, cut so the whole stays within HandleMax.
func uniqueHandle(t Team, key, base string) string {
	if handleProblem(t, key, base) == nil {
		return base
	}
	for n := 2; ; n++ {
		suffix := strconv.Itoa(n)
		h := cutHandle(base, HandleMax-len(suffix)) + suffix
		if handleProblem(t, key, h) == nil {
			return h
		}
	}
}

// assignHandles clears every handle in t that is invalid or repeats one an
// earlier member has, then gives each member with a title and no handle one,
// in member order. It reports whether it changed anything.
func assignHandles(t *Team) bool {
	changed := false
	seen := map[string]bool{}
	for i := range t.Members {
		h := t.Members[i].Handle
		if h == "" {
			continue
		}
		if ValidHandle(h) != nil || seen[h] {
			t.Members[i].Handle = ""
			changed = true
			continue
		}
		seen[h] = true
	}
	for i := range t.Members {
		m := &t.Members[i]
		if m.Handle != "" {
			continue
		}
		base := DeriveHandle(m.Word)
		if base == "" {
			continue
		}
		m.Handle = uniqueHandle(*t, m.Key, base)
		changed = true
	}
	return changed
}
