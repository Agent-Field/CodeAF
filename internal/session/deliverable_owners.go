package session

import "bytes"

// deliverNeedleMax is the most owners a listing tests line by line with one
// byte search each. A larger set (a union of teams) is tested by reading the
// line's own "session" values once, so the cost of a line does not grow with
// the number of owners.
const deliverNeedleMax = 8

var sessionKey = []byte(`"session":"`)

// sessionValues is an owner set spelled as written rows hold it
// ([mediaSessionValue]), which is what [lineNamesSession] compares against.
func sessionValues(owners map[string]bool) map[string]bool {
	out := make(map[string]bool, len(owners))
	for owner := range owners {
		out[mediaSessionValue(owner)] = true
	}
	return out
}

// lineNamesSession reports whether any "session" value in line, spelled as
// written, is in values ([sessionValues]). It is a prefilter with the meaning of
// the per-owner [mediaNeedle] search it replaces: a quote inside a JSON string
// is escaped, so the key cannot appear inside text, and a value ends at the
// first quote no backslash escapes, so an escaped id is read whole. The caller
// still decodes a hit and compares the session exactly.
func lineNamesSession(line []byte, values map[string]bool) bool {
	for rest := line; ; {
		at := bytes.Index(rest, sessionKey)
		if at < 0 {
			return false
		}
		rest = rest[at+len(sessionKey):]
		end := -1
		for i := 0; i < len(rest); i++ {
			if rest[i] == '\\' {
				i++
			} else if rest[i] == '"' {
				end = i
				break
			}
		}
		if end < 0 {
			return false
		}
		if values[string(rest[:end])] {
			return true
		}
		rest = rest[end:]
	}
}
