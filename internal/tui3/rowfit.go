package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ── ROWFIT — ONE LINE, LAID OUT BY WHAT IT IS FOR ───────────────────────────
//
// Every list on this surface draws the same shape: a thing's NAME on the left,
// and a dim tail of facts about it on the right. What was missing was an answer
// to the only question a narrow terminal asks — WHICH FACTS GO FIRST — and in
// its absence every row answered it by accident. The picker's tail was joined
// into one string and then cut with an ellipsis, so a sixty-cell frame drew
// `1M · $3/$15 per…` (a price sliced in half, which is a wrong number) beside
// `anthropic/claude-son…` (a name that could be either of two models). Both
// halves cut, neither readable, and the row's whole purpose — telling one model
// from another — was the first thing spent.
//
// THE LAW THIS FILE HOLDS:
//
//  1. THE IDENTITY IS WHOLE OR THE ROW IS POINTLESS. The primary — the model's
//     id, the lane's name — keeps every cell it asks for before any fact gets
//     one, and it is truncated only when the frame cannot hold it ALONE. A
//     person scanning a list is matching names; a name they cannot match is a
//     row they have to open to read.
//
//  2. FACTS DEGRADE BEFORE THEY DISAPPEAR. Each fact carries up to three
//     spellings — the full one, a short one, a bare glyph or number — and the
//     fitter takes the longest that fits. `$0.08/$0.15 per M` becomes `$0.15/M`
//     becomes `$0.15`, and the fact stays on the row three widths longer than
//     it otherwise would.
//
//  3. THE TAIL IS A PREFIX OF ITSELF. Facts are added in priority order and the
//     first one that will not fit ENDS the tail; nothing later is skipped
//     forward into the gap. This is what makes a narrow row honest — every row
//     at every width shows the same ranked facts from the top — and it is what
//     keeps a person from reading `elo 1300` and concluding the price is
//     unpublished when it merely did not fit.
//
//  4. UNKNOWN IS NOT NARROW. A fact nobody published has no spellings at all,
//     draws nothing, costs nothing and does NOT end the tail (the emptiness
//     law, design-law-v2 §16). Absence for want of a figure and absence for
//     want of a cell are the same silence on the screen, so the ranked prefix
//     is the only thing that keeps the second kind explicable.
//
// The fitter is pure and deterministic: the same plan at the same width is the
// same two strings, which is what lets a test paste a row.

// rowSep is what joins two facts, and rowGutter is the least space between the
// name and the first of them. Both are the values every list on this surface
// already used; they are named here because the arithmetic now happens in one
// place instead of in each caller's head.
const (
	rowSep    = " · "
	rowGutter = 1
)

// rowUnbounded is the width of a frame with no edge — what [rowAll] fits
// against when a caller wants the row as it would read with room to spare
// (`aforge models`, the tail a test pins). It is a number and not a special
// case so that one code path draws every row.
const rowUnbounded = 1 << 30

// rowField is ONE FACT in up to three spellings, longest first. An empty
// spelling is not a spelling: a field with `full` empty is a fact nobody
// published and is skipped whole (law 4), and a field with only `full` set is a
// fact that cannot be said shorter and either fits or goes.
type rowField struct {
	full  string
	short string
	tiny  string
}

// rowSay is a field from its spellings, longest first. Callers pass one, two or
// three; passing none is the unknown field, which every builder in this package
// uses to say "nobody published this" without an `if` around the append.
func rowSay(spellings ...string) rowField {
	field := rowField{}
	for at, spelling := range spellings {
		switch at {
		case 0:
			field.full = spelling
		case 1:
			field.short = spelling
		case 2:
			field.tiny = spelling
		}
	}
	return field
}

// known reports whether this field has anything to say at all.
func (f rowField) known() bool { return f.full != "" || f.short != "" || f.tiny != "" }

// rowTail is the facts alone, in room cells: each in the longest spelling that
// still fits, stopping at the first that fits in none (law 3).
//
// It is the whole mechanism for anything that is a LIST OF FACTS with no name
// in front of it — the status line's served segment is exactly that, and calls
// this directly ([servedFields]).
func rowTail(fields []rowField, room int) string {
	if room <= 0 {
		return ""
	}
	sep := ansi.StringWidth(rowSep)
	var out strings.Builder
	spent := 0
	for _, field := range fields {
		// THE UNKNOWN FIELD IS NOT A FIELD. It draws nothing, spends nothing,
		// and — this is the half that matters — does not end the tail: a model
		// with no published price must still show its window.
		if !field.known() {
			continue
		}
		said := false
		for _, spelling := range [...]string{field.full, field.short, field.tiny} {
			if spelling == "" {
				continue
			}
			cost := ansi.StringWidth(spelling)
			if spent > 0 {
				cost += sep
			}
			if spent+cost > room {
				continue
			}
			if spent > 0 {
				out.WriteString(rowSep)
			}
			out.WriteString(spelling)
			spent += cost
			said = true
			break
		}
		if !said {
			// LAW 3: the tail is a prefix. A lower-ranked fact is not promoted
			// into the space a higher-ranked one could not use.
			break
		}
	}
	return out.String()
}

// rowLed is [rowTail] for a segment whose FIRST field is a LEAD — a name with a
// word of grammar in front of it, where the grammar is worth less than any fact
// behind it.
//
// THE DEFECT IT FIXES. [rowTail] is greedy field by field, which is right when
// every field is a fact: each one says the most it can and the row stops where
// it stops. It is wrong for a lead, because the lead's own longest spelling is
// not a fact — "via coreweave" and "coreweave" name the same machine — so
// spending three cells on `via ` can cost the whole of the fact after it. At
// thirty-two columns the greedy answer is `via coreweave · 3.1s` and the honest
// one is `coreweave · 3.1s → parasail 4.4s`, which is the same name and the
// thing a person was actually looking for.
//
// THE RULE IS ONE LINE: try the lead in each of its spellings and keep the
// rendering that says the MOST, measured in cells actually used. A tie keeps the
// longer lead, because between two rows that say as much the more identifying
// name is the better one. It is not a second ladder — every rung is still
// [rowTail]'s, and this only decides which spelling of the head it is handed.
func rowLed(fields []rowField, room int) string {
	if len(fields) == 0 || !fields[0].known() {
		return rowTail(fields, room)
	}
	lead, rest := fields[0], fields[1:]
	best, widest := "", -1
	for _, spelling := range [...]string{lead.full, lead.short, lead.tiny} {
		if spelling == "" {
			continue
		}
		said := rowTail(append([]rowField{{full: spelling}}, rest...), room)
		if width := ansi.StringWidth(said); width > widest {
			best, widest = said, width
		}
	}
	return best
}

// rowAll is every known fact at its longest spelling — the row as a frame with
// no edge would draw it.
func rowAll(fields []rowField) string { return rowTail(fields, rowUnbounded) }

// rowPlan is one row before it knows how wide it will be: what the row IS, and
// what is known about it in the order a person chooses on.
type rowPlan struct {
	// primary is the identity — the model id, the lane's name — and law 1 is
	// about this string.
	primary string
	// suffix rides the primary and is never cut: the reasoning level the person
	// dialled in (`:high`) is not a fact about the model, it is what THEY asked
	// for, and a level clipped off the end is a knob that looks like it did
	// nothing.
	suffix string
	// author says the primary is spelled `author/slug` AND that the slug alone
	// still names one row on this list. It is the first thing given up when the
	// name will not fit, because `nemotron-3.5-lightning` identifies a model and
	// `nvidia/nemotron-3.5…` identifies a company.
	author bool
	// fields are the facts, highest priority first.
	fields []rowField
}

// fit lays the plan out in room cells and returns the row's two halves: the
// label the list draws on the left, and the tail it right-aligns in what is
// left. room excludes the caller's own furniture — an overlay's two-cell lead,
// a fold's indent — because those cells were never the row's to spend.
func (p rowPlan) fit(room int) (string, string) {
	if room <= 0 {
		return "", ""
	}
	name, cut := rowTrim(p.primary, room-ansi.StringWidth(p.suffix), p.author)
	label := name + p.suffix
	// LAW 1: a name that had to be CUT takes the whole row. Dropping the author
	// is not that cut — it is the row shedding the part that identifies nothing
	// — so a shortened-but-complete name still leaves its remainder to the
	// facts; a name with an ellipsis in it has already spent the one thing the
	// row was drawn to say, and a throughput beside it would be a second loss.
	if cut {
		return label, ""
	}
	return label, rowTail(p.fields, room-ansi.StringWidth(label)-rowGutter)
}

// label is the identity half alone, for the tiers that give the tail a line of
// its own ([overlayLines] at tierPhone).
func (p rowPlan) label(room int) string {
	name, _ := rowTrim(p.primary, room-ansi.StringWidth(p.suffix), p.author)
	return name + p.suffix
}

// rowTrim cuts a name to room cells by giving up the LEAST IDENTIFYING part
// first.
//
// An id is `author/slug` and the slug is what a person is looking for: they
// remember `kimi-k3` and `v4-flash`, never `moonshotai` — which is a fact about
// six hundred rows and about none of them. So the author goes first, and only
// where dropping it still leaves one row answering to that slug ([rowPlan]'s
// own `author` field decides that, because this function cannot see the list).
//
// What is left is cut in the MIDDLE, keeping the tail, for the same reason:
// `nemotron-3.5-lightning` cut from the right is `nemotron-3.5…`, which is the
// half every model in that family shares. The tail is snapped to a word
// boundary when one is close enough that the fragment still reads as a word.
// It reports whether the name lost more than its author — whether, that is,
// there is an ellipsis in the answer.
func rowTrim(name string, room int, author bool) (string, bool) {
	if room <= 0 {
		return "", true
	}
	if ansi.StringWidth(name) <= room {
		return name, false
	}
	if author {
		if at := strings.IndexByte(name, '/'); at >= 0 && at+1 < len(name) {
			name = name[at+1:]
			if ansi.StringWidth(name) <= room {
				return name, false
			}
		}
	}
	return rowMiddle(name, room), true
}

// rowMiddle is the middle ellipsis: a head, the mark, and as much of the tail
// as the room allows.
//
// THE HEAD IS A THIRD AND THE TAIL IS THE REST, because the distinguishing end
// of a model's name is its end — the version, the size, the flavour — while the
// head only has to be enough to tell two families apart. When the author could
// NOT be dropped (two rows share the slug) the head is what carries it, which
// is the case this split is sized for.
func rowMiddle(text string, room int) string {
	mark := ansi.StringWidth(glyphMore)
	if room <= mark+1 {
		return fit(text, room)
	}
	runes := []rune(text)
	head := room / 3
	if head < 1 {
		head = 1
	}
	tail := room - head - mark
	cut := len(runes) - tail
	if cut <= head {
		return fit(text, room)
	}
	// A BOUNDARY IF ONE IS NEAR. `nemo…-lightning` reads; `nemo…lightning` reads;
	// `nemo…ightning` is a fragment. The search never gives up more than half the
	// tail, so a name with no separator in reach keeps the longer ending.
	for at := cut; at < len(runes) && at <= cut+tail/2; at++ {
		if at > 0 && strings.ContainsRune("-._/", runes[at-1]) {
			cut = at
			break
		}
	}
	head = room - mark - ansi.StringWidth(string(runes[cut:]))
	if head < 1 || head >= cut {
		return fit(text, room)
	}
	return string(runes[:head]) + glyphMore + string(runes[cut:])
}

// rowSlug is the part of an id after the author, and the whole id when it has
// no author.
func rowSlug(id string) string {
	if at := strings.IndexByte(id, '/'); at >= 0 && at+1 < len(id) {
		return id[at+1:]
	}
	return id
}

// sharedSlugs are the slugs carried by more than one of these rows — the
// answer to "may this row drop its author", asked once for a whole list.
//
// It walks the list rather than being kept as the list is built because the
// question is about the SET: a slug is safe to stand alone exactly when no
// second row answers to it, and no row can know that about itself.
func sharedSlugs(models []Model) map[string]bool {
	if len(models) < 2 {
		return nil
	}
	seen := make(map[string]int, len(models))
	for _, model := range models {
		seen[rowSlug(model.ID)]++
	}
	shared := map[string]bool{}
	for slug, count := range seen {
		if count > 1 {
			shared[slug] = true
		}
	}
	if len(shared) == 0 {
		return nil
	}
	return shared
}

// rowHalves is a plan as the two halves ONE LIST ROW draws, honouring the
// two-line law at tierPhone: there the label owns its line whole and the tail
// is fitted to the indented line under it, so the fitter — and not an ellipsis
// — decides which facts a phone shows.
//
// indent is the caller's own leading pad inside the row (the fold's lane rows
// hang four cells in); the overlay's two-cell lead is subtracted here because
// every list on this surface pays it.
func rowHalves(plan rowPlan, width, indent int) (string, string) {
	if phoneList(width) {
		return plan.label(width - 2 - indent), rowTail(plan.fields, width-overlayIndent)
	}
	return plan.fit(width - 2 - indent)
}

// ── THE STATUS LINE'S SERVED SEGMENT ────────────────────────────────────────

// servedFields is the served rider as ranked facts, for the status line to draw
// through [rowTail] at whatever the segment has left:
//
//	via coreweave · thinking 4s · 0.6s · 61 t/s
//	coreweave · thinking 4s · 0.6s
//	coreweave
//
// THE MACHINE OUTRANKS THE CLOCK. When a person looks down at that line mid
// answer they are asking two questions in order — who is answering me, and is
// it moving — and every number after those is a thing they will not act on.
// The lane keeps the `via` lead while there is room for it, because `via
// coreweave` is attribution and a bare `coreweave` is a word that could be
// anything; the lead is the first thing the segment gives up, and the name
// itself is the last.
//
// Each argument is already spelled by the caller (render.go's servedRider knows
// what a phase is called and how long it has been going); an empty string is a
// fact nobody has yet, and draws nothing.
func servedFields(lane, phase, first, rate string) []rowField {
	fields := make([]rowField, 0, 4)
	if lane != "" {
		fields = append(fields, rowSay("via "+strings.ToLower(lane), strings.ToLower(lane)))
	}
	fields = append(fields, rowSay(phase), rowSay(first), rowSay(rate))
	return fields
}

// rowShort is every known fact at its SHORTEST spelling, and it is [rowAll]'s
// other end: the row as a frame with barely any edge would draw it.
//
// IT EXISTS FOR THE LADDER THAT MEASURES RATHER THAN THE ONE THAT IS TOLD A
// WIDTH. The legend's hint slot is offered a line and either fits it whole or
// drops the slot entirely (render.go's [app.legend]), so a caller there cannot
// hand the fitter a room — it has no room to hand it until the line is built.
// What it can do is offer a second, shorter rendering of the same facts, and
// this is that rendering: still every fact, each in a spelling the row itself
// owns, and never a word cut in half.
func rowShort(fields []rowField) string {
	brief := make([]rowField, 0, len(fields))
	for _, field := range fields {
		switch {
		case field.tiny != "":
			brief = append(brief, rowField{full: field.tiny})
		case field.short != "":
			brief = append(brief, rowField{full: field.short})
		default:
			brief = append(brief, rowField{full: field.full})
		}
	}
	return rowAll(brief)
}
