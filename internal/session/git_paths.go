package session

// git_paths.go is THE ONE READER OF THE PATHS GIT NAMES IN ITS NUL-TERMINATED
// ANSWERS, for every question on the landing road about which files are the
// work.
//
// GIT'S HUMAN OUTPUT IS FOR PEOPLE, AND IT QUOTES. `git status --porcelain`
// without `-z` writes a name holding a space, a quote, an accent or a newline
// inside double quotes, with C escapes: `odd name é'q.txt` arrives as
// `"odd name \303\251'q.txt"`. A reader that stripped the outer quotes handed
// `git add` a literal pathspec naming no file on disk, the add refused it, the
// per-path retry read it as a file that had never existed, and the landing
// commit went out without it and without a word. The `-z` form writes every
// name as its own bytes, so it is the only form read here.
//
// A NAME IS NEVER TRIMMED. A leading or trailing space is part of a file's name
// on every filesystem git runs on, and a trim is how the work tab listed
// ` lead.txt` as `lead.txt`, a file that was not there.

import "strings"

// gitNULPaths reads a `-z` name list (`--name-only -z`, `ls-files -z`) into
// the names it holds. Only the empty fields are dropped, which are the list's
// own terminator and never a file.
func gitNULPaths(out string) []string {
	var paths []string
	for _, path := range strings.Split(out, "\x00") {
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

// porcelainEntry is one record of `git status --porcelain -z`: its two status
// columns, the path as it stands now, and for a rename or a copy the path it
// came from. Both ends are kept because the questions differ: what to stage
// wants the name that exists now, and what a run touched wants both.
type porcelainEntry struct {
	Code string
	Path string
	From string
}

// porcelainEntries reads a `git status --porcelain -z` answer. The two status
// columns are read by position, never after a trim, because the first column
// is a space for a change that is not staged, and a record whose third byte is
// not the separating space is not one git writes. A rename or copy in either
// column is followed by one more field, the name it came from.
func porcelainEntries(out string) []porcelainEntry {
	fields := strings.Split(out, "\x00")
	var entries []porcelainEntry
	for i := 0; i < len(fields); i++ {
		entry := fields[i]
		if len(entry) < 4 || entry[2] != ' ' {
			continue
		}
		item := porcelainEntry{Code: entry[:2], Path: entry[3:]}
		if strings.ContainsAny(item.Code, "RC") && i+1 < len(fields) {
			i++
			item.From = fields[i]
		}
		entries = append(entries, item)
	}
	return entries
}
