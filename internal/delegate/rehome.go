package delegate

import (
	"sort"
	"strings"
)

// RehomeBrief rewrites every mention of the folder a task was proposed on into
// the working copy the program was handed, so the brief a program reads names
// only the folder it works in.
//
// IT EXISTS BECAUSE A PROGRAM DID WHAT ITS BRIEF SAID. A conversation briefed
// senior-dev on "the checkout at /Users/…/happy-dom-task", which was the task's
// folder and so exactly right as a description; codeaf then handed senior-dev
// a copy of that folder, and senior-dev's model, reading the path, ran its git
// commands in the person's checkout instead. It committed there, made branches
// there, and the copy's own work would not merge over what it had done. The
// copy IS that folder as far as the work is concerned, so the brief is made to
// say so: the one fact the program needs to find its work is where it stands,
// and a path it cannot use is a path it is better never told.
//
// from is every spelling of the folder (as proposed, resolved, under ~); to is
// the copy. A mention is replaced only where it is the whole path or a path
// inside it — `/a/b` is rewritten in `/a/b` and `/a/b/src`, never in `/a/bc`
// or `/x/a/b` — so a sibling folder or a longer path is left as it was.
func RehomeBrief(brief string, from []string, to string) string {
	to = strings.TrimRight(strings.TrimSpace(to), "/")
	if to == "" || brief == "" {
		return brief
	}
	spellings := make([]string, 0, len(from))
	for _, spelling := range from {
		spelling = strings.TrimRight(strings.TrimSpace(spelling), "/")
		if spelling != "" && spelling != to && strings.ContainsRune(spelling, '/') {
			spellings = append(spellings, spelling)
		}
	}
	// THE LONGEST SPELLING FIRST, so a resolved path that contains a shorter
	// one is rewritten whole rather than half by the shorter.
	sort.Slice(spellings, func(i, j int) bool { return len(spellings[i]) > len(spellings[j]) })
	for _, spelling := range spellings {
		brief = rehomeOne(brief, spelling, to)
	}
	return brief
}

// rehomeOne rewrites one spelling wherever it stands as a whole path.
func rehomeOne(brief, from, to string) string {
	var out strings.Builder
	rest := brief
	for {
		at := strings.Index(rest, from)
		if at < 0 {
			out.WriteString(rest)
			return out.String()
		}
		end := at + len(from)
		whole := (at == 0 || !pathByte(rest[at-1])) && endsName(rest[end:])
		out.WriteString(rest[:at])
		if whole {
			out.WriteString(to)
		} else {
			out.WriteString(from)
		}
		rest = rest[end:]
	}
}

// endsName says the text after a match does not continue its last name: it is
// empty, a separator, or a sentence's full stop and not a file's extension.
func endsName(after string) bool {
	switch {
	case after == "":
		return true
	case after[0] == '.':
		return len(after) == 1 || !nameByte(after[1])
	}
	return !nameByte(after[0])
}

// nameByte is a byte a path's last name continues through, so a match
// followed by one is a longer name and not the folder.
func nameByte(b byte) bool {
	return b == '-' || b == '_' || b == '.' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// pathByte is a byte a path runs through before a match, so a match preceded
// by one is the tail of a longer path.
func pathByte(b byte) bool { return nameByte(b) || b == '/' || b == '~' }
