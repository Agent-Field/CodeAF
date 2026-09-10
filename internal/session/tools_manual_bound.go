package session

// Bounding the manual's page lookup.
//
// Every other read on the belt is bounded: a file comes back at 2000 lines or
// 50KB and says how to ask for the next part. A named manual page was the one
// read with no ceiling at all, and the pages it can return are larger than the
// files — the biggest is 205 KB, four times what the read tool will hand over
// — so a single lookup could spend a conversation's whole context on one page
// that mentioned the word the model was after. The tool's own refusals made
// that likelier, because they hand the model the list of page names and a name
// is exactly what invites the unbounded call.
//
// So a page is bounded by the SAME NUMBER a file read quotes, and the rest of
// it is addressable rather than lost: the cut says what it cut and lists that
// page's headings, and one of those headings comes back whole.

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

const (
	// manualPageCap is the ceiling on one page lookup. It is the read tool's
	// cap, not a number of its own: a result is a result whatever it read, and
	// two numbers for one bargain drift apart.
	manualPageCap = bare.ResultByteCap
	// manualListCap bounds the lists that tell the model what to ask for next
	// — the page names, a page's headings. HALF A PAGE, which is room for every
	// heading of the longest page there is with a page's worth of growth left in
	// it, so the list is whole in practice and cannot become the flood it exists
	// to prevent.
	//
	// IT WAS A QUARTER, AND A QUARTER STOPPED BEING TRUE. `tasks` is the longest
	// page and its headings — deliberately long, because a heading is the search
	// index and people search in their own words — reached 12,798 bytes of a
	// 12,800-byte budget. Two bytes. The next `##` anybody added to that page
	// pushed four headings out of the cut notice and turned
	// TestEverySectionTheCutNamesComesBackWhole red on dev, over a change that had
	// nothing to do with this file (#771, 2026-09-09) — and a bound that fails a
	// stranger's pull request for writing a manual section is a bound that will be
	// worked around rather than read.
	//
	// THE LIST IS WHAT THE CUT IS FOR. A page held back with headings it will not
	// name is a page whose rest is unreachable, which is the exact failure this
	// whole file exists to stop; the notice is subtracted from the page text
	// shown, so what a bigger list spends is prose the reader can ask for by name
	// afterwards. That is the right way round.
	manualListCap = manualPageCap / 2
)

// boundedPage returns a page whole when it fits and a cut one when it does not,
// with the cut saying so and naming the sections the rest is in. The headings
// are collected only when there is a cut to explain, so the common read pays
// nothing for them.
func boundedPage(name, text string) string {
	if len(text) <= manualPageCap {
		return text
	}
	sections := boundedList(manualHeadings(name))
	return bounded(text, func(shown, total int) string { return pageCutNotice(shown, total, sections) })
}

// boundedSection holds a section to the same ceiling. A section is written to
// sit far under it — the manual's own rule is about two thousand characters,
// self-contained — so this is a guard rather than a road anybody travels: if one
// ever grows past a whole read it is cut like everything else, instead of
// becoming through the back door the flood the page bound just closed.
func boundedSection(body string) string {
	return bounded(body, sectionCutNotice)
}

// bounded is the cut itself. The notice is rendered twice on purpose: the first
// pass reserves room using the text's own length, which is the widest the shown
// figure can ever be, so the second pass — with the figure that turned out true
// — is the same size or smaller and the whole result stays under the cap.
func bounded(text string, notice func(shown, total int) string) string {
	if len(text) <= manualPageCap {
		return text
	}
	reserved := len(notice(len(text), len(text)))
	shown := cutAtLineBoundary(text, manualPageCap-reserved)
	return shown + notice(len(shown), len(text))
}

// pageCutNotice says the two things the model needs and nothing else: that what
// it is holding is a part, and how to ask for the rest by name.
func pageCutNotice(shown, total int, sections string) string {
	return fmt.Sprintf("\n\n[Cut: %d bytes of %d. The rest of this page is in its sections — "+
		"ask for the same page again with section set to one of:%s]", shown, total, sections)
}

// sectionCutNotice has no such offer to make, because a section is already the
// smallest thing the manual can be asked for. So it says the one true thing.
func sectionCutNotice(shown, total int) string {
	return fmt.Sprintf("\n\n[Cut: %d bytes of %d. A section is meant to be shorter than one read, "+
		"and this one is not.]", shown, total)
}

// boundedList writes out names for a model to choose from, and stops when the
// list stops being a signpost. One per line, because a heading in this manual
// carries commas, dashes and middle dots of its own and every inline separator
// that was tried is a sequence some heading already contains — which would make
// the exact name the next call has to give ambiguous.
func boundedList(items []string) string {
	var list strings.Builder
	for index, item := range items {
		if list.Len()+len(item)+len(listItemPrefix) > manualListCap {
			return list.String() + fmt.Sprintf("\n(… and %d more)", len(items)-index)
		}
		list.WriteString(listItemPrefix)
		list.WriteString(item)
	}
	return list.String()
}

// listItemPrefix is what every item is written behind. The budget above counts
// it by measuring it, so the two cannot say different things about the same
// three bytes.
const listItemPrefix = "\n- "

// cutAtLineBoundary keeps the last whole line inside the budget, so the part
// that arrives ends where the writing does rather than mid-word, and never ends
// on half of a character or half of a code fence.
func cutAtLineBoundary(text string, budget int) string {
	if budget <= 0 {
		return ""
	}
	if len(text) <= budget {
		return text
	}
	cut := text[:budget]
	if at := strings.LastIndexByte(cut, '\n'); at > 0 {
		return outsideACodeFence(cut[:at])
	}
	return strings.ToValidUTF8(cut, "")
}

// outsideACodeFence backs the cut up to before a fence it left open. A page cut
// mid-fence hands the model a notice that is INSIDE a block of sample output,
// where it reads as part of the example rather than as a message about the
// read — and the headings it offers read as sample text too.
func outsideACodeFence(text string) string {
	opened, inside, at := 0, false, 0
	for _, line := range strings.SplitAfter(text, "\n") {
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), "```") {
			if !inside {
				opened = at
			}
			inside = !inside
		}
		at += len(line)
	}
	if !inside {
		return text
	}
	return strings.TrimRight(text[:opened], "\n")
}
