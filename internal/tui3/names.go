package tui3

import "strings"

// WHAT A SESSION IS CALLED, AND WHAT IT IS CALLED ON SCREEN.
//
// A session names itself once, from its first exchange, in eight lowercase words
// (internal/session's title.go). That is the name in the file and the name this
// surface draws — except when it is not: a session named by something other than
// that namer, or by a model that answered the instruction with a slug, arrives
// here as ONE TOKEN with its words welded together —
//
//	port_b_parser_fix        →  Port B Parser Fix
//	fix-the-nil-map          →  Fix The Nil Map
//	porting the parser       →  porting the parser   (already words; untouched)
//	20260816-150405_a1b2c3   →  20260816-150405_a1b2c3   (an id; untouched)
//
// — and a machine name in the identity cluster is the surface saying its own
// filing system out loud. So a one-token name is read back as words WHERE IT IS
// DRAWN, and nowhere else: the raw string stays in the file, in the resume path
// and in whatever slug some other tool minted it from, because those are
// identifiers and an identifier that is prettied is an identifier that no longer
// matches.
//
// THE GUARD IS THE WHOLE DESIGN. Only a token whose every segment reads as a
// WORD is expanded — a segment of digits, a hex tail, an extension — any of them
// and the name is left exactly as it is. The failure this prevents is the one
// that matters: a session file's own name, which is a timestamp and a random
// tail, turned into "20260816 150405 A1b2c3" and offered to a person as a title.

// sessionName is the conversation's name as the frame draws it: the session's
// own, read back as words when it arrived as one token.
func (a *app) sessionName() string { return humanName(a.title) }

// humanName turns a one-token machine name into words, and leaves everything
// else exactly as it found it.
func humanName(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" || strings.ContainsAny(name, " \t.") {
		// A name with a space in it is already words, and one with a dot in it is
		// a file — neither is this function's business.
		return name
	}
	if !strings.ContainsAny(name, "_-") {
		// A single plain word is a name somebody chose. Capitalizing it here
		// would be this surface deciding it knew better.
		return name
	}
	segments := strings.FieldsFunc(name, func(r rune) bool { return r == '_' || r == '-' })
	if len(segments) < 2 {
		return name
	}
	words := make([]string, 0, len(segments))
	for _, segment := range segments {
		if !wordish(segment) {
			return name
		}
		words = append(words, upFirst(segment))
	}
	return strings.Join(words, " ")
}

// wordish reports whether one segment reads as a word: it starts with a letter
// and carries nothing but letters and digits after it. "parser" and "b2" pass;
// "150405" and "a1b2c3" — a timestamp and a random tail — do not, because a
// segment that starts with a letter and then runs on into digits is what an id's
// hex tail looks like.
func wordish(segment string) bool {
	if segment == "" || !isLetter(rune(segment[0])) {
		return false
	}
	digits := 0
	for _, r := range segment {
		switch {
		case isLetter(r):
		case r >= '0' && r <= '9':
			digits++
		default:
			return false
		}
	}
	// A word may end in a number ("v2", "b2"); a word is not mostly numbers.
	return digits*2 <= len([]rune(segment))
}

func isLetter(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }

// upFirst raises a segment's first letter and touches nothing else: "parser"
// becomes "Parser" and "nilMap" stays "NilMap" rather than being re-spelled by a
// function that was asked for one capital.
func upFirst(segment string) string {
	if segment == "" {
		return segment
	}
	head := segment[:1]
	return strings.ToUpper(head) + segment[1:]
}
