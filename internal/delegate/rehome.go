package delegate

import (
	"sort"
	"strings"
)

// Rehome is one spelling of a folder a brief may name, and the folder in the
// program's working copy it stands for: the folder the task was proposed on
// maps to where that folder is inside the copy, and the repository around it
// maps to the copy's root.
type Rehome struct {
	From string
	To   string
}

// RehomeBrief rewrites every mention of the folders a task was proposed on
// into the working copy the program was handed, so the brief a program reads
// names only the folder it works in.
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
// A mention is replaced only where it is the whole path or a path inside it —
// `/a/b` is rewritten in `/a/b` and `/a/b/src`, never in `/a/bc` or `/x/a/b` —
// so a sibling folder or a longer path is left as it was. Where several
// spellings match at one place the longest wins, so a subfolder's spelling is
// read whole before the repository around it can be.
//
// THE BRIEF IS READ ONCE, LEFT TO RIGHT, AND WHAT IS WRITTEN IS NEVER READ
// AGAIN. It was rewritten one spelling at a time, each pass over the text the
// last had produced; a copy that lives INSIDE the folder (a repository at the
// home folder, whose copies sit under ~/.codeaf) starts with that folder's own
// spelling, so every pass found its own output again and a brief came back
// naming `…/trees/3/.codeaf/…/trees/3/.codeaf/…`. A path the brief already
// spells inside the copy is copied through as it stands for the same reason.
func RehomeBrief(brief string, moves []Rehome) string {
	if brief == "" {
		return brief
	}
	type spelling struct {
		text string
		to   string
		keep bool
	}
	seen := map[string]bool{}
	var spellings []spelling
	add := func(text, to string, keep bool) {
		text = strings.TrimRight(strings.TrimSpace(text), "/")
		if text == "" || !strings.ContainsRune(text, '/') || seen[text] {
			return
		}
		seen[text] = true
		spellings = append(spellings, spelling{text: text, to: to, keep: keep})
	}
	// THE COPY'S OWN PATHS FIRST, so a spelling of the folder that is also the
	// start of a path already in the copy never claims it.
	for _, move := range moves {
		to := strings.TrimRight(strings.TrimSpace(move.To), "/")
		if to != "" {
			add(to, to, true)
		}
	}
	for _, move := range moves {
		to := strings.TrimRight(strings.TrimSpace(move.To), "/")
		if to != "" {
			add(move.From, to, false)
		}
	}
	if len(spellings) == 0 {
		return brief
	}
	sort.SliceStable(spellings, func(i, j int) bool { return len(spellings[i].text) > len(spellings[j].text) })

	var out strings.Builder
	for at := 0; at < len(brief); {
		matched := false
		if at == 0 || !pathByte(brief[at-1]) {
			for _, one := range spellings {
				if strings.HasPrefix(brief[at:], one.text) && endsName(brief[at+len(one.text):]) {
					if one.keep {
						out.WriteString(one.text)
					} else {
						out.WriteString(one.to)
					}
					at += len(one.text)
					matched = true
					break
				}
			}
		}
		if !matched {
			out.WriteByte(brief[at])
			at++
		}
	}
	return out.String()
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
