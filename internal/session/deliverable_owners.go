package session

import "bytes"

// deliverNeedleMax is the most owners a listing tests line by line with one
// byte search each. A larger set (a union of teams) is tested by reading the
// line's own "session" values once, so the cost of a line does not grow with
// the number of owners.
const deliverNeedleMax = 8

var sessionKey = []byte(`"session":"`)

// lineNamesSession reports whether any "session" value in line is in owners. It
// is a prefilter with the meaning of the per-owner byte search it replaces: a
// quote inside a JSON string is escaped, so the key cannot appear inside text.
func lineNamesSession(line []byte, owners map[string]bool) bool {
	for rest := line; ; {
		at := bytes.Index(rest, sessionKey)
		if at < 0 {
			return false
		}
		rest = rest[at+len(sessionKey):]
		end := bytes.IndexByte(rest, '"')
		if end < 0 {
			return false
		}
		if owners[string(rest[:end])] {
			return true
		}
		rest = rest[end:]
	}
}
