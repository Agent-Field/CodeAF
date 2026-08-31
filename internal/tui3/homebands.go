package tui3

// THE RIGHT COLUMN OF HOME IS A STACK OF BANDS, AND EVERY BAND IS ITS OWN FILE.
//
// home.go's [app.homeDetail] owns the two lines nothing may displace — the
// title and the place — and then asks this registry for the rest. A band says
// what kind of thing it is about (a conversation, a standing item, a project),
// what order it sits at, and how to draw itself for a given width; the registry
// composes them top-down with one blank row between, and a frame too short for
// all of them drops whole bands from the bottom ([homeBands]), so the most
// decision-relevant band is the one nearest the title.
//
// ── THE LAWS ──
//
//   - A BAND DRAWS NOTHING RATHER THAN A PLACEHOLDER. The emptiness law: a
//     conversation that ran no tasks has no work band, one that spent nothing
//     has no spend line. An empty slice is the whole answer and costs no row.
//
//   - A BAND IS AT MOST A FEW ROWS, AND A LIST-SHAPED BAND FOLDS. Nothing on
//     this column may grow with the data. A band with more rows than its
//     allowance draws the allowance and one fold line — `▸ …N more <what>` —
//     through [bandFold], which keeps the fold state by band and by subject so
//     the fold a person opened stays open while the cursor is on that row.
//
//   - A BAND'S ORDER IS A KEY, NOT A POSITION. The keys below are spaced so a
//     band can be added between two others without renumbering, and they are
//     the one place the column's reading order is decided.
//
//   - A BAND NEVER BLOCKS. It is drawn on every frame the cursor rests on a
//     row. Anything that reads a file or runs a command caches on the app by
//     subject id (see [app.leftOffBand]'s use of [homeView.last] for the shape)
//     and answers from the cache.
//
//   - A BAND IS REGISTERED FROM ITS OWN FILE'S init, so two lanes adding two
//     bands never edit one line. The registry sorts by order once, lazily.

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// bandClauses lays whole facts into the fewest rows that hold them. THE LAST
// FACT NEVER PAYS FOR A NARROW CARD: only a fact that cannot fit on an empty
// row is clipped, because there is no honest boundary inside it to break at.
func bandClauses(width, indent int, ink func(string) string, clauses ...string) []string {
	if width < 1 {
		return nil
	}
	kept := make([]string, 0, len(clauses))
	for _, clause := range clauses {
		if clause = strings.TrimSpace(clause); clause != "" {
			kept = append(kept, clause)
		}
	}
	var rows []string
	for at := 0; at < len(kept); {
		lead := 0
		if len(rows) > 0 {
			lead = indent
		}
		room := width - lead
		if room < 1 {
			room = width
			lead = 0
		}
		run := kept[at]
		at++
		for at < len(kept) {
			candidate := run + " · " + kept[at]
			if ansi.StringWidth(candidate) > room {
				break
			}
			run = candidate
			at++
		}
		rows = append(rows, strings.Repeat(" ", lead)+ink(fit(run, room)))
	}
	return rows
}

// bandSides keeps a label and its trailing fact on one row when both retain
// the label's floor. When they cannot share, the fact gets a complete row of
// its own, aligned to the right whenever it fits there.
func bandSides(width, indent, floor int, label, tail string, labelInk, tailInk func(string) string) []string {
	return bandSidesWithSeparator(width, indent, floor, "", label, tail, labelInk, tailInk)
}

func bandSidesWithSeparator(width, indent, floor int, separator, label, tail string, labelInk, tailInk func(string) string) []string {
	label, tail = strings.TrimSpace(label), strings.TrimSpace(tail)
	if width < 1 || (label == "" && tail == "") {
		return nil
	}
	if tail == "" {
		return []string{labelInk(fit(label, width))}
	}
	sharedTail := separator + tail
	tailWidth := ansi.StringWidth(sharedTail)
	labelRoom := width - tailWidth - 1
	if label != "" && labelRoom >= floor {
		shown := fit(label, labelRoom)
		gap := width - ansi.StringWidth(shown) - tailWidth
		return []string{labelInk(shown) + strings.Repeat(" ", gap) + tailInk(sharedTail)}
	}
	rows := []string{labelInk(fit(label, width))}
	room := width - indent
	if room < 1 {
		room, indent = width, 0
	}
	shown := fit(tail, room)
	lead := width - ansi.StringWidth(shown)
	if lead < indent {
		lead = indent
	}
	return append(rows, strings.Repeat(" ", lead)+tailInk(shown))
}

// fitLeft clips a place from the left because the basename at its end is the
// part that distinguishes it from its neighbours.
func fitLeft(text string, width int) string {
	if width < 1 {
		return ""
	}
	if ansi.StringWidth(text) <= width {
		return text
	}
	if width == 1 {
		return glyphMore
	}
	runes := []rune(text)
	for len(runes) > 0 && ansi.StringWidth(glyphMore+string(runes)) > width {
		runes = runes[1:]
	}
	return glyphMore + string(runes)
}

// bandKind is what a band is about. A band declares the kinds it draws for and
// is skipped for every other subject.
type bandKind uint8

const (
	// bandKindSession is the card for one conversation — the row under the cursor.
	bandKindSession bandKind = iota + 1
	// bandKindItem is the card for one standing item.
	bandKindItem
	// bandKindProject is the card for a whole project: the cursor is on a heading
	// or on a folded project line.
	bandKindProject
)

// THERE WAS A FOURTH KIND AND IT IS RETIRED. `bandKindMachine` was the card for
// THE MACHINE ITSELF — the column when the cursor stood on no row at all, which
// is where home's list left it after `↑` off the top row. That state is gone:
// `↑` reaches the TAB BAR now (pages.go's [barCursor]), and each of the three
// questions the machine's card answered has a room of its own on that bar. Every
// kind here is a row somebody is pointing at, which is what a card is for.

// bandSubject is the thing under the cursor. Exactly the fields its kind names
// are set.
type bandSubject struct {
	kind bandKind
	// row is the conversation, for bandKindSession.
	row session.SessionRow
	// item is the standing item, for bandKindItem.
	item StandingItemView
	// project is the whole project, for bandKindProject.
	project string
	// world is the reading the rows were built from, for bands that look past
	// the subject (a project card counting its sessions).
	world session.World
	// dir is the project's directory, for the place line and for anything that
	// asks the filesystem.
	dir string
}

// id is what fold state and caches key on: the transcript for a conversation,
// the item id for an item, the directory for a project.
func (s bandSubject) id() string {
	switch s.kind {
	case bandKindSession:
		return s.row.Transcript
	case bandKindItem:
		return s.item.Item.ID
	case bandKindProject:
		return s.dir
	}
	return ""
}

// bandContext is everything a band needs to draw, handed in whole so a band's
// signature never grows when a new one needs one more thing.
type bandContext struct {
	subject bandSubject
	width   int
	now     time.Time
	pal     palette
}

// homeBand is one registered band.
type homeBand struct {
	// name is the band's own word, used for its fold key and in tests.
	name string
	// order is its place in the column. See the keys below.
	order int
	// kinds is what it draws for. Empty is bandKindSession only.
	kinds []bandKind
	// draw answers the band's rows, already painted, or nil for nothing.
	draw func(a *app, ctx bandContext) []string
}

// The reading order of the column, top to bottom. A lane adding a band picks a
// key between its neighbours and does not move the others.
const (
	bandOrderGone        = 5   // the folder this project lived in is not there any more
	bandOrderState       = 10  // what it is doing right now, and what it is stopped on
	bandOrderAnswer      = 15  // the question it is stopped on, answerable from here
	bandOrderNews        = 30  // what happened since you last looked
	bandOrderWork        = 40  // the tasks it ran, with what they came to
	bandOrderDeliverable = 50  // the files it produced
	bandOrderNextUp      = 60  // the standing items that will wake, and when
	bandOrderLeftOff     = 70  // where the conversation left off
	bandOrderRepo        = 80  // where the repository stands
	bandOrderSpend       = 90  // what it has cost
	bandOrderThinking    = 93  // a standing item: the rung it thinks at
	bandOrderKeys        = 100 // what the keyboard does here, always last
)

var (
	homeBandRegistry []homeBand
	homeBandsSorted  bool
)

// registerHomeBand is called from a band file's init. Order ties keep
// registration order, which is file-name order, which is deliberate enough.
func registerHomeBand(band homeBand) {
	homeBandRegistry = append(homeBandRegistry, band)
	homeBandsSorted = false
}

// homeBandsFor answers the registered bands that draw for one kind, in order.
func homeBandsFor(kind bandKind) []homeBand {
	if !homeBandsSorted {
		sort.SliceStable(homeBandRegistry, func(i, j int) bool {
			return homeBandRegistry[i].order < homeBandRegistry[j].order
		})
		homeBandsSorted = true
	}
	var out []homeBand
	for _, band := range homeBandRegistry {
		if band.draws(kind) {
			out = append(out, band)
		}
	}
	return out
}

func (b homeBand) draws(kind bandKind) bool {
	if len(b.kinds) == 0 {
		return kind == bandKindSession
	}
	for _, k := range b.kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// drawHomeBands draws every band for the subject, each as its own slice, in
// order. Empty bands are dropped here so [homeBands] only ever sees rows.
func (a *app) drawHomeBands(ctx bandContext) [][]string {
	var out [][]string
	for _, band := range homeBandsFor(ctx.subject.kind) {
		rows := band.draw(a, ctx)
		if len(rows) == 0 {
			continue
		}
		out = append(out, rows)
	}
	return out
}

// ── folding ─────────────────────────────────────────────────────────────────

// bandFoldGlyph is the fold mark a list-shaped band draws, the same one the
// left column's quiet tail and the task column use — one gesture, one mark.
const bandFoldGlyph = "▸"

// bandFoldOpenGlyph marks a fold somebody opened.
const bandFoldOpenGlyph = "▾"

// bandFoldKey is how one band's fold on one subject is remembered.
func bandFoldKey(band string, subject bandSubject) string { return band + "\x00" + subject.id() }

// bandFolded answers whether a band's list is folded for this subject. Folded
// is the default; opening is a thing a person did and is kept on the app for
// as long as home is up ([homeView] is zeroed when it closes).
func (a *app) bandFolded(band string, subject bandSubject) bool {
	if a.home.bandOpen == nil {
		return true
	}
	return !a.home.bandOpen[bandFoldKey(band, subject)]
}

// toggleBandFold opens or folds one band for one subject.
func (a *app) toggleBandFold(band string, subject bandSubject) {
	if a.home.bandOpen == nil {
		a.home.bandOpen = map[string]bool{}
	}
	key := bandFoldKey(band, subject)
	a.home.bandOpen[key] = !a.home.bandOpen[key]
}

// setAllBandFolds is the keyboard's way in: `→` on a card opens every fold on
// it, and `←` folds them all back (home.go's [app.homeKey]), the same pair of
// arrows the left column folds with — one gesture at every scale this screen
// has. The column has no cursor of its own, so the keys act on the card rather
// than on a line; a click on a fold line acts on that line alone
// ([app.bandFoldAt]).
func (a *app) setAllBandFolds(subject bandSubject, open bool) {
	if a.home.bandOpen == nil {
		a.home.bandOpen = map[string]bool{}
	}
	for _, band := range homeBandsFor(subject.kind) {
		a.home.bandOpen[bandFoldKey(band.name, subject)] = open
	}
}

// anyBandFoldOpen answers whether the card holds at least one opened band —
// which is exactly the question `←` asks before folding them, so a card with
// everything already folded lets the key fall through to whatever the arrow
// means on the list.
func (a *app) anyBandFoldOpen(subject bandSubject) bool {
	for _, band := range homeBandsFor(subject.kind) {
		if !a.bandFolded(band.name, subject) {
			return true
		}
	}
	return false
}

// allBandFoldsOpen is [app.anyBandFoldOpen]'s other end: `→` acts only while
// something on the card is still folded.
func (a *app) allBandFoldsOpen(subject bandSubject) bool {
	for _, band := range homeBandsFor(subject.kind) {
		if a.bandFolded(band.name, subject) {
			return false
		}
	}
	return true
}

// bandFold draws a list-shaped band: the first `show` rows when folded, all of
// them when open, and — whenever there is more than fits — one fold line that
// says how many are hidden and what they are. `what` is the plural noun the
// fold line uses: "tasks", "files", "things". The fold line is recorded on the
// app so a click on it can be told apart from a click on anything else
// ([app.noteBandFoldLine]); the caller passes the band's name for that.
func (a *app) bandFold(ctx bandContext, band string, rows []string, show int, what string) []string {
	if len(rows) <= show {
		return rows
	}
	folded := a.bandFolded(band, ctx.subject)
	rest := rows[:show]
	if !folded {
		rest = rows
	}
	label := bandFoldWord(len(rows)-show, what, folded)
	out := append([]string(nil), rest...)
	out = append(out, ctx.pal.dim(fit(bandFoldMark(ctx.pal, folded)+" "+label, ctx.width)))
	a.noteBandFoldLine(band, ctx.subject, strings.TrimSpace(label))
	return out
}

// bandFoldGroups is [app.bandFold] over GROUPS rather than over rows, and it is
// the right shape for any band whose entries are taller than one line.
//
// A ROW-COUNTED FOLD CUTS WHEREVER THE ARITHMETIC LANDS, and on a band that
// draws a task as a name with its outcome under it that is a cut straight
// through a task: a sentence left hanging under the fold line with nothing
// above it saying what it was about. So the caller hands its entries whole, the
// cut is always at a boundary, and `show` is a number of THINGS — three tasks —
// which is also the number a person would say out loud.
//
// The groups are joined with ONE BLANK ROW between them and none after the
// last, which is the separation the card uses between its bands, one rung down.
func (a *app) bandFoldGroups(ctx bandContext, band string, groups [][]string, show int, what string) []string {
	join := func(groups [][]string) []string {
		var out []string
		for _, group := range groups {
			if len(group) == 0 {
				continue
			}
			if len(out) > 0 {
				out = append(out, "")
			}
			out = append(out, group...)
		}
		return out
	}
	if len(groups) <= show {
		return join(groups)
	}
	folded := a.bandFolded(band, ctx.subject)
	rest := groups[:show]
	if !folded {
		rest = groups
	}
	label := bandFoldWord(len(groups)-show, what, folded)
	out := join(rest)
	out = append(out, ctx.pal.dim(fit(bandFoldMark(ctx.pal, folded)+" "+label, ctx.width)))
	a.noteBandFoldLine(band, ctx.subject, strings.TrimSpace(label))
	return out
}

// bandFoldPacked is the group-counted fold for compact rows. It preserves a
// multi-row item's boundary without inserting the blank line the work band
// deliberately uses between tasks.
func (a *app) bandFoldPacked(ctx bandContext, band string, groups [][]string, show int, what string) []string {
	join := func(groups [][]string) []string {
		var out []string
		for _, group := range groups {
			out = append(out, group...)
		}
		return out
	}
	if len(groups) <= show {
		return join(groups)
	}
	folded := a.bandFolded(band, ctx.subject)
	rest := groups[:show]
	if !folded {
		rest = groups
	}
	label := bandFoldWord(len(groups)-show, what, folded)
	out := join(rest)
	out = append(out, ctx.pal.dim(fit(bandFoldMark(ctx.pal, folded)+" "+label, ctx.width)))
	a.noteBandFoldLine(band, ctx.subject, strings.TrimSpace(label))
	return out
}

// bandFoldMark is the arrow a fold line wears: shut when it is hiding things,
// open when somebody opened it. One gesture, one mark, everywhere on this
// surface.
func bandFoldMark(pal palette, folded bool) string {
	if pal.ascii {
		if folded {
			return ">"
		}
		return "v"
	}
	if folded {
		return bandFoldGlyph
	}
	return bandFoldOpenGlyph
}

// bandFoldWord is what a fold line SAYS: how many are hidden and what they are,
// or how many are being held open. It is one function because two folds spell
// it, and two spellings of one sentence is two things to keep in step.
func bandFoldWord(hidden int, what string, folded bool) string {
	if !folded {
		return "…" + strconv.Itoa(hidden) + " fewer"
	}
	return "…" + strconv.Itoa(hidden) + " more " + what
}

// bandFoldLine is one fold line drawn this frame, so a click can find it by
// its text. The column is repainted every frame and the record with it.
type bandFoldLine struct {
	band    string
	subject bandSubject
	text    string
}

// noteBandFoldLine records one painted fold line as a door of the card. The
// registry it goes into is the card's ONE door registry (carddoors.go), which
// the work rows share: what lights under the pointer has to be what a press
// acts on, and two registries would be two answers to that.
func (a *app) noteBandFoldLine(band string, subject bandSubject, text string) {
	a.noteCardDoor(cardDoor{
		kind: cardDoorFold,
		text: text,
		fold: bandFoldLine{band: band, subject: subject, text: strings.TrimSpace(ansi.Strip(text))},
	})
}

// bandFoldAt answers the fold line whose text is in a painted row, for a click.
func (a *app) bandFoldAt(rowText string) (bandFoldLine, bool) {
	door, ok := a.cardDoorOfKind(rowText, cardDoorFold)
	if !ok {
		return bandFoldLine{}, false
	}
	return door.fold, true
}

// ── the bands that were home.go's own, now registered ──────────────────────

func init() {
	registerHomeBand(homeBand{name: "state", order: bandOrderState, draw: drawStateBand})
}

// drawStateBand: STATE IS THE LOUDEST CONTENT LINE, because it is the only band
// that is about right now. A conversation stopped on a question says so here
// and then says what it is stopped on, in ink.
func drawStateBand(a *app, ctx bandContext) []string {
	row, width, pal := ctx.subject.row, ctx.width, ctx.pal
	var state []string
	if word := a.homeHolding(row); word != "" {
		ink := pal.dim
		if row.NeedsPerson() {
			ink = pal.accent
		}
		state = append(state, ink(fit(word, width)))
	}
	if reason := row.Reason(); reason != "" {
		for _, wrapped := range wrap(reason, width) {
			state = append(state, pal.ink(wrapped))
		}
	}
	return state
}

// itemView is a convenience for bands that draw for items.
func (s bandSubject) itemOrNil() *standing.Item {
	if s.kind != bandKindItem {
		return nil
	}
	return &s.item.Item
}
