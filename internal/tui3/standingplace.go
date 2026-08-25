package tui3

// THE STANDING PLACE IS A PURE READING OF RECORDS THE HOME CLOCK ALREADY HOLDS.
// It never reaches through the standing seam and never reads time for itself,
// so moving the cursor can redraw this place without touching disk or making
// two ages on one frame disagree.

import (
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// standingShown is the number of rows the place shows before quiet records
// become one door. Four leaves room for the selected item's last look without
// turning a reference place into another scrolling sheet.
const standingShown = 4

type standingReading struct {
	views  []StandingItemView
	week   map[string]standing.Spend
	now    time.Time
	header string
}

// standingVerb is one action the shared verb strip may put behind a visible
// key. The router owns effects; this reading owns only which words are true of
// the row it drew.
type standingVerb struct {
	key  rune
	word string
}

// The shared table is the one vocabulary both state selection and future
// routing adapters can use. Keeping keys beside their
// words prevents a receipt and its visible invitation from drifting apart.
var standingVerbWords = map[rune]string{
	'p': "pause",
	'r': "resume",
	't': "trust it alone",
	'x': "retire",
	'y': "let it",
	'n': "not this time",
}

// readStanding adopts and orders a snapshot without changing the caller's
// slice. Needs-you and moving rows stay above quiet rows because the glyph and
// the row's position are two statements of the same fact.
func readStanding(views []StandingItemView, week map[string]standing.Spend, now time.Time) standingReading {
	ordered := append([]StandingItemView(nil), views...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return standingPlaceRank(ordered[i]) > standingPlaceRank(ordered[j])
	})
	r := standingReading{views: ordered, week: week, now: now}
	if len(ordered) == 0 {
		return r
	}
	waiting := 0
	for _, view := range ordered {
		if strings.TrimSpace(view.Item.NeedsPerson) != "" {
			waiting++
		}
	}
	r.header = "things aforge does without being asked. " + itoa(len(ordered)) + " standing"
	if waiting > 0 {
		r.header += ", " + itoa(waiting) + " waiting to be stood up"
	}
	r.header += "."
	return r
}

func standingPlaceRank(view StandingItemView) int {
	switch {
	case strings.TrimSpace(view.Item.NeedsPerson) != "":
		return 3
	case view.Running:
		return 2
	default:
		return 1
	}
}

// rows composes fixed columns from the widest edge inward. At narrow widths
// cadence gives way first and rope second, preserving the person's words and
// the measured cost while the one shared ellipsis grammar clips the subject.
func (r standingReading) rows(width int, cursor int, pal palette) []string {
	if width < 1 || len(r.views) == 0 {
		return nil
	}
	out := []string{pal.dim(fit(r.header, width))}
	shown := min(standingShown, len(r.views))
	for i := 0; i < shown; i++ {
		out = append(out, r.row(r.views[i], width, pal))
	}
	if folded := len(r.views) - shown; folded > 0 {
		clause := ""
		quiet := true
		for _, view := range r.views[shown:] {
			if r.week[view.Item.ID].Fired > 0 {
				quiet = false
				break
			}
		}
		if quiet {
			clause = "all quiet this week"
		}
		word := foldLine(folded, clause)
		out = append(out, pal.dim(fit(word, width)))
	}
	if view, ok := r.at(cursor); ok {
		if line := standing.LastLookLine(view.Item, r.now); line != "" {
			out = append(out, "")
			name := strings.TrimSpace(view.Item.Title())
			section := name + ", last look"
			if age := sinceAt(view.Item.LastFired, r.now); age != "" {
				section += " · " + age
			}
			out = append(out, pal.dim(fit(section, width)), pal.ink(fit(line, width)))
		}
	}
	return out
}

func (r standingReading) row(view StandingItemView, width int, pal palette) string {
	item := view.Item
	glyph, glyphInk := tokens.GlyphQueued, pal.dim
	switch {
	case strings.TrimSpace(item.NeedsPerson) != "":
		glyph, glyphInk = tokens.GlyphNeedsHuman, pal.warn
	case view.Running:
		glyph, glyphInk = tokens.GlyphWorking, pal.accent
	case item.Status == standing.StatusPaused:
		glyph = tokens.GlyphPaused
	}
	rope := standing.RopeWord(item)
	ropeInk := pal.dim
	if rope == standing.RopeAsksFirst {
		ropeInk = pal.ink
	}
	cadence := standingCadence(item)
	cost := standing.CostPerRunWord(r.week[item.ID])
	if width < 80 {
		cadence = ""
		if standingPlaceMinimum(item.Words, rope, cost) > width {
			rope = ""
		}
	}

	// Each optional cell pays for its own separating space. The remaining room
	// belongs to the verbatim sentence, which is the row's permanent identity.
	tails := []struct {
		word string
		ink  func(string) string
	}{{rope, ropeInk}, {cadence, pal.dim}, {cost, placeMoneyInk(pal)}}
	// An authored tail is still optional prose, so it first fits the frame and
	// then yields whole cells in the same cadence, rope, cost order as narrow
	// rows. This keeps an unusually long schedule from consuming the title.
	for i := range tails {
		tails[i].word = fit(tails[i].word, max(0, width-ansi.StringWidth(glyph)-1-8))
	}
	for standingTailWidth(tails)+ansi.StringWidth(glyph)+1+8 > width {
		dropped := false
		for _, at := range []int{1, 0, 2} {
			if tails[at].word != "" {
				tails[at].word = ""
				dropped = true
				break
			}
		}
		if !dropped {
			break
		}
	}
	tailWidth := 0
	for _, tail := range tails {
		if tail.word != "" {
			tailWidth += 1 + ansi.StringWidth(tail.word)
		}
	}
	leadWidth := ansi.StringWidth(glyph) + 1
	words, wordsWidth := fitWidth(strings.TrimSpace(item.Words), max(0, width-leadWidth-tailWidth))
	line := glyphInk(glyph) + " " + pal.ink(words)
	used := leadWidth + wordsWidth
	for _, tail := range tails {
		if tail.word == "" {
			continue
		}
		line += " " + tail.ink(tail.word)
		used += 1 + ansi.StringWidth(tail.word)
	}
	if cost != "" && used < width {
		// Money is the right edge's stable landmark, so any spare cells sit
		// immediately before it rather than after the row.
		costPainted := " " + placeMoneyInk(pal)(cost)
		line = strings.TrimSuffix(line, costPainted) + strings.Repeat(" ", width-used) + costPainted
	}
	return fit(line, width)
}

func standingTailWidth(tails []struct {
	word string
	ink  func(string) string
}) int {
	width := 0
	for _, tail := range tails {
		if tail.word != "" {
			width += 1 + ansi.StringWidth(tail.word)
		}
	}
	return width
}

func standingPlaceMinimum(words, rope, cost string) int {
	return 2 + ansi.StringWidth(fit(strings.TrimSpace(words), standWordsFloor)) +
		1 + ansi.StringWidth(rope) + 1 + ansi.StringWidth(cost)
}

// standingCadence repeats the item's authored cadence and adds the one firing
// clock a daily row can know. Other schedules keep their own words rather than
// having a cron expression translated differently on a second surface.
func standingCadence(item standing.Item) string {
	words := strings.TrimSpace(item.When.Words)
	if strings.EqualFold(words, "daily") && !item.LastFired.IsZero() {
		return words + " · fired " + standingClock(item.LastFired)
	}
	return words
}

func standingClock(at time.Time) string {
	word := strings.ToLower(at.Format("3:04pm"))
	return strings.Replace(word, ":00", "", 1)
}

// at resolves only rows a cursor may stop on. The header and fold are doors or
// prose, while detail rows are consequences of the selected item and never
// acquire an identity of their own.
func (r standingReading) at(i int) (StandingItemView, bool) {
	at := i - 1 // The header is row zero.
	if at < 0 || at >= min(standingShown, len(r.views)) {
		return StandingItemView{}, false
	}
	return r.views[at], true
}

func (r standingReading) verbs(i int) []standingVerb {
	view, ok := r.at(i)
	if !ok {
		return nil
	}
	verbs := make([]standingVerb, 0, 5)
	if strings.TrimSpace(view.Item.NeedsPerson) != "" {
		verbs = append(verbs,
			standingVerb{key: 'y', word: standingVerbWords['y']},
			standingVerb{key: 'n', word: standingVerbWords['n']})
	}
	if view.Item.Status == standing.StatusPaused {
		verbs = append(verbs, standingVerb{key: 'r', word: standingVerbWords['r']})
	} else {
		verbs = append(verbs, standingVerb{key: 'p', word: standingVerbWords['p']})
	}
	if standing.RopeWord(view.Item) == standing.RopeAsksFirst {
		verbs = append(verbs, standingVerb{key: 't', word: standingVerbWords['t']})
	}
	return append(verbs, standingVerb{key: 'x', word: standingVerbWords['x']})
}

// standingTeach spends an empty place on the three facts that let somebody
// make the first standing order, without drawing a count or an empty heading.
func standingTeach(pal palette) []string {
	return []string{
		pal.dim("standing orders are reminders, watches, routines and rules."),
		pal.dim("they keep working after the chat that made them ends."),
		pal.dim("make one by asking in any chat."),
	}
}
