package main

import (
	"fmt"
	"os"
	"strings"
)

// THE RELEASE PAGE HAS A CEILING AND THE CHANGELOG DOES NOT. GitHub refuses a
// release body longer than 125,000 characters, and a section of CHANGELOG.md
// carries every fold of every entry, so the first roll-up in this repository
// rendered at 1.7 MB and would have failed the stable publish at the last
// step, after six binaries were built. So the notes are a rendering of the
// section and not the section: whole when it fits, the headline layer when the
// folds do not fit, and a cut at a line boundary when even that is too long —
// always ending with the line that says where the rest is.
//
// The ceiling here is bytes and GitHub's is characters, so this bound is the
// conservative one: an em-dash is one character and three bytes. The workflow
// appends its own install paragraph after these notes, so this leaves room.
const notesCapBytes = 120_000

// notesTail is the line every shortened body ends with, so a reader on the
// release page knows the page is the summary and the changelog is the record.
func notesTail(tag string) string {
	return fmt.Sprintf("The whole section, with every fold, is in [CHANGELOG.md](%s/blob/%s/CHANGELOG.md).", repoURL, tag)
}

// Section returns the body of one version's section of CHANGELOG.md: every
// line after `## <tag> ` up to the next `## ` heading. The trailing space in
// the match is deliberate — `v0.2.0` must not match `v0.2.0-rc.1`. ok is false
// when there is no such section, which is the workflow's cue to fall back to
// generated notes rather than publish a page that says nothing.
func Section(changelog, tag string) (string, bool) {
	prefix := "## " + tag + " "
	var b strings.Builder
	found := false
	for _, line := range strings.Split(changelog, "\n") {
		if found {
			if strings.HasPrefix(line, "## ") {
				break
			}
			b.WriteString(line)
			b.WriteByte('\n')
			continue
		}
		if strings.HasPrefix(line, prefix) {
			found = true
		}
	}
	return strings.TrimSpace(b.String()), found
}

// Notes renders one version's section for the release page, under capBytes.
//
// Three shapes, tried in order, and the first that fits is the answer: the
// section as written; the section with its `<details>` folds removed, so every
// headline line and every kind heading survives; and that headline layer cut at
// a line boundary. The last two end with notesTail so nothing shortened
// pretends to be whole.
func Notes(changelog, tag string, capBytes int) (string, error) {
	section, ok := Section(changelog, tag)
	if !ok {
		return "", fmt.Errorf("%s has no `## %s ` section", changelogFile, tag)
	}
	if len(section) <= capBytes {
		return section + "\n", nil
	}

	tail := notesTail(tag)
	headlines := stripFolds(section)
	if len(headlines)+1+len(tail) <= capBytes {
		return headlines + "\n\n" + tail + "\n", nil
	}

	// Cut at a line boundary. Room is what is left after the tail and the
	// blank line before it, and a line is kept only when it fits whole.
	room := capBytes - len(tail) - 2
	var b strings.Builder
	for _, line := range strings.Split(headlines, "\n") {
		if b.Len()+len(line)+1 > room {
			break
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n") + "\n\n" + tail + "\n", nil
}

// stripFolds removes every `<details>` block the renderer wrote, leaving the
// headline lines and the kind headings, with runs of blank lines collapsed so
// the result reads as a list and not as a list with holes in it.
func stripFolds(section string) string {
	var out []string
	inFold := false
	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "<details>") {
			inFold = true
		}
		if inFold {
			if strings.HasPrefix(trimmed, "</details>") {
				inFold = false
			}
			continue
		}
		if trimmed == "" && len(out) > 0 && out[len(out)-1] == "" {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// notes is the command: print the release notes for one tag, or fail with
// nothing on stdout when the changelog has no section for it.
func notes(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("name the release tag, for example v0.2.0")
	}
	raw, err := os.ReadFile(changelogFile)
	if err != nil {
		return err
	}
	body, err := Notes(string(raw), args[0], notesCapBytes)
	if err != nil {
		return err
	}
	fmt.Print(body)
	return nil
}
