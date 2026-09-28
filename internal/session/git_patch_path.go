package session

// git_patch_path.go is THE ONE READER OF WHICH FILE A PATCH SECTION IS ABOUT,
// shared by the work tab's filter here ([planWorkPatch]) and the task room that
// draws the same patch (internal/tui3's planWorkLines).
//
// A PATCH HAS NO NUL FORM. `-z` changes git's name lists and never a unified
// diff, so the name has to be read off the section itself, and it is read the
// way git's own `apply` reads it. Taking the text after the last ` b/` in the
// `diff --git` line was the old reading, and it read a person's
// `x b/plandb.db` as `plandb.db`: the harness filter then dropped the file as
// codeaf's own store, and the room drew the wrong name. A name git quoted came
// through as its octal escapes.
//
// ONE READER, BECAUSE TWO WOULD DISAGREE. The filter that decides a section is
// codeaf's own and the label a person reads over it are the same question, and
// the day they were answered twice was the day a file could be hidden under
// one name and drawn under another.

import "strings"

// PatchSections splits a unified patch into one piece per file, each starting
// at its `diff --git` line, so a file's header travels with its body.
func PatchSections(patch string) []string {
	var sections []string
	start := -1
	for at := 0; at < len(patch); {
		end := strings.IndexByte(patch[at:], '\n')
		line := patch[at:]
		next := len(patch)
		if end >= 0 {
			line = patch[at : at+end]
			next = at + end + 1
		}
		if strings.HasPrefix(line, "diff --git ") {
			if start >= 0 {
				sections = append(sections, patch[start:at])
			}
			start = at
		}
		at = next
	}
	if start >= 0 {
		sections = append(sections, patch[start:])
	} else if strings.TrimSpace(patch) != "" {
		sections = append(sections, patch)
	}
	return sections
}

// PatchSectionPath is the path one section is about, as the file stands now.
//
// A rename or copy says its new name on a line of its own (`rename to`,
// `copy to`), which is never ambiguous, so that line wins. Otherwise the
// `diff --git` line names the file twice. When git quoted it, the second
// quoted token is the name. When it did not, the two halves are the same
// name, so the line is measured rather than split: `a/P b/P` is two plus P,
// three, and P again, which finds P even when P itself holds ` b/`.
func PatchSectionPath(section string) string {
	head, rest, _ := strings.Cut(section, "\n")
	for rest != "" {
		line, tail, _ := strings.Cut(rest, "\n")
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") ||
			strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "Binary files") {
			break
		}
		for _, prefix := range []string{"rename to ", "copy to "} {
			if strings.HasPrefix(line, prefix) {
				name := strings.TrimPrefix(line, prefix)
				if decoded, ok := unquoteGitPath(name); ok {
					return decoded
				}
				return name
			}
		}
		rest = tail
	}
	name := strings.TrimPrefix(head, "diff --git ")
	if strings.HasPrefix(name, `"`) {
		if end := quotedGitTokenEnd(name); end > 0 && end < len(name) && name[end] == ' ' {
			destination := name[end+1:]
			if strings.HasPrefix(destination, `"`) {
				if tokenEnd := quotedGitTokenEnd(destination); tokenEnd > 0 {
					destination = destination[:tokenEnd]
				}
			}
			if decoded, ok := unquoteGitPath(destination); ok {
				return strings.TrimPrefix(decoded, "b/")
			}
			return strings.TrimPrefix(destination, "b/")
		}
	}
	if len(name) >= 5 && (len(name)-5)%2 == 0 {
		n := (len(name) - 5) / 2
		if strings.HasPrefix(name, "a/") && name[2+n:5+n] == " b/" && name[2:2+n] == name[5+n:] {
			return name[5+n:]
		}
	}
	// Older patch engines can write two unequal bare halves without extended
	// rename headers; their last destination prefix is the best available path.
	if at := strings.LastIndex(name, " b/"); at >= 0 {
		return name[at+3:]
	}
	return name
}

// quotedGitTokenEnd finds a C-quoted token's closing quote without mistaking
// an escaped quote for the end of the path.
func quotedGitTokenEnd(s string) int {
	if len(s) == 0 || s[0] != '"' {
		return -1
	}
	for i := 1; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == '"' {
			return i + 1
		}
	}
	return -1
}

// unquoteGitPath decodes Git's C quoting byte for byte, including octal UTF-8
// bytes, so the returned string is the filename Git actually saw.
func unquoteGitPath(s string) (string, bool) {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", false
	}
	var out strings.Builder
	for i := 1; i < len(s)-1; i++ {
		if s[i] != '\\' {
			out.WriteByte(s[i])
			continue
		}
		i++
		if i >= len(s)-1 {
			return "", false
		}
		switch s[i] {
		case '"', '\\':
			out.WriteByte(s[i])
		case 'a':
			out.WriteByte('\a')
		case 'b':
			out.WriteByte('\b')
		case 'f':
			out.WriteByte('\f')
		case 'n':
			out.WriteByte('\n')
		case 'r':
			out.WriteByte('\r')
		case 't':
			out.WriteByte('\t')
		case 'v':
			out.WriteByte('\v')
		case '0', '1', '2', '3', '4', '5', '6', '7':
			if i+2 >= len(s)-1 || s[i+1] < '0' || s[i+1] > '7' || s[i+2] < '0' || s[i+2] > '7' {
				return "", false
			}
			out.WriteByte((s[i]-'0')*64 + (s[i+1]-'0')*8 + s[i+2] - '0')
			i += 2
		default:
			return "", false
		}
	}
	return out.String(), true
}
