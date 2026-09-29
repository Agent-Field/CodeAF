package tui3

// programbrief.go draws a program's brief as a DOCUMENT: what `▸ brief` opens on
// a program's room (programroom.go). The brief is the work order codeaf handed
// the program — often thousands of words — and it used to be drawn between the
// head's rules, pinned, cut to half the frame with a count for the rest. The
// owner asked on 2026-09-28 for the whole of it, scrollable and well formatted,
// so while the dropdown is open the brief IS the room's body: the room's own
// scroll reads it, and the steps come back, where they were, when it shuts.
//
// IT IS NOT RENDERED AS MARKDOWN. A brief is a model's plain text about files,
// and markdown would read `__init__.py` as bold `init.py` and run a list the
// model wrote one item to a line into one paragraph. What it gets instead is
// the little a document needs: each part of a generated work order under its
// plain heading in bold (taskrequest.go's [requestSections]), every line the
// brief has kept as its own, a blank line between paragraphs, a list item's
// wrapped lines hung under its text rather than its marker, and a line that
// opens in capitals — `DONE WHEN`, `FIRST ACTION:` — with those words in bold.

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// briefDocumentMost is the widest a brief's lines are laid, in cells. A brief
// is read like a page, and a paragraph a hundred and fifty cells wide is one
// the eye loses its place in.
const briefDocumentMost = 100

// briefListMarker is a list item's marker at the head of a line: a dash, a
// star or a bullet, a number or a letter with a stop or a bracket, or a number
// in brackets — with the space after it.
var briefListMarker = regexp.MustCompile(`^([-*•·]|\d{1,3}[.)]|\(\d{1,3}\)|[a-z][.)])\s+`)

// programBriefDocument is the brief as the room's body draws it: every row
// already painted and fitted to width, with a blank row between its parts.
func (a *app) programBriefDocument(desc string, width int) []string {
	text := strings.TrimSpace(desc)
	if text == "" {
		return nil
	}
	width = min(max(width, 1), briefDocumentMost)
	sections, ok := requestSections(text)
	if !ok {
		sections = []requestSection{{body: text}}
	}
	var out []string
	for _, s := range sections {
		if len(out) > 0 {
			out = append(out, "")
		}
		if s.name != "" {
			out = append(out, a.pal.bold(a.pal.ink(fit(s.name, width))), "")
		}
		out = append(out, a.briefBodyRows(s.body, width)...)
	}
	return out
}

// briefBodyRows is one part's body: its lines as the brief wrote them, runs of
// blank lines said as one.
func (a *app) briefBodyRows(body string, width int) []string {
	var out []string
	gap := false
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			gap = true
			continue
		}
		if gap && len(out) > 0 {
			out = append(out, "")
		}
		gap = false
		out = append(out, a.briefLineRows(line, width)...)
	}
	return out
}

// briefLineRows is one line of the brief, wrapped: kept at its own indent, a
// list item's wrapped lines hung under its text, and a capital lead in bold.
func (a *app) briefLineRows(line string, width int) []string {
	trimmed := strings.TrimLeft(line, " \t")
	indent := min(ansi.StringWidth(line)-ansi.StringWidth(trimmed), 8)
	marker := briefListMarker.FindString(trimmed)
	text := strings.TrimSpace(trimmed[len(marker):])
	marker = strings.TrimSpace(marker)
	hang := indent
	if marker != "" {
		hang += ansi.StringWidth(marker) + 1
	}
	room := max(width-hang, 8)
	wrapped := wrap(text, room)
	if len(wrapped) == 0 {
		wrapped = []string{""}
	}
	lead := briefCapitalLead(text)
	out := make([]string, len(wrapped))
	for i, part := range wrapped {
		painted := a.pal.ink(part)
		if i == 0 && lead > 0 && lead <= len(part) {
			painted = a.pal.bold(a.pal.ink(part[:lead])) + a.pal.ink(part[lead:])
		}
		switch {
		case i == 0 && marker != "":
			out[i] = strings.Repeat(" ", indent) + a.pal.dim(marker) + " " + painted
		default:
			pad := hang
			if i == 0 {
				pad = indent
			}
			out[i] = strings.Repeat(" ", pad) + painted
		}
	}
	return out
}

// briefCapitalLead is how many bytes at the head of a line are words in
// capitals — `DONE WHEN`, `FIRST ACTION:`, `SETTLED WITH THE OWNER` — and 0 for
// a line that opens any other way. A lead is two such words or more, or one
// that ends in a colon (`NOTE:`), so a sentence that opens on `I`, `OK` or
// `README` is not taken for a heading. The colon stays in the lead.
func briefCapitalLead(text string) int {
	end, words, colon := 0, 0, false
	for at := 0; at < len(text) && !colon; {
		for at < len(text) && text[at] == ' ' {
			at++
		}
		next := strings.IndexByte(text[at:], ' ')
		if next < 0 {
			next = len(text) - at
		}
		word := text[at : at+next]
		if word == "" || !briefCapitalWord(word) {
			break
		}
		at += next
		end, words, colon = at, words+1, strings.HasSuffix(word, ":")
	}
	if words >= 2 || colon {
		return end
	}
	return 0
}

// briefCapitalWord says a word has a letter and no lowercase one: `DONE`,
// `D1`, `WHAT'S`, `ACTION:`.
func briefCapitalWord(word string) bool {
	letter := false
	for _, r := range word {
		if unicode.IsLower(r) {
			return false
		}
		letter = letter || unicode.IsLetter(r)
	}
	return letter
}
