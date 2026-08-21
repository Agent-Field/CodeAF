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
//     subject id (see [app.homeLast] for the shape) and answers from the cache.
//
//   - A BAND IS REGISTERED FROM ITS OWN FILE'S init, so two lanes adding two
//     bands never edit one line. The registry sorts by order once, lazily.

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

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
	bandOrderState       = 10  // what it is doing right now, and what it is stopped on
	bandOrderAnswer      = 15  // the question it is stopped on, answerable from here
	bandOrderNews        = 30  // what happened since you last looked
	bandOrderWork        = 40  // the tasks it ran, with what they came to
	bandOrderDeliverable = 50  // the files it produced
	bandOrderNextUp      = 60  // the standing items that will wake, and when
	bandOrderLeftOff     = 70  // where the conversation left off
	bandOrderRepo        = 80  // where the repository stands
	bandOrderSpend       = 90  // what it has cost
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

// toggleAllBandFolds is the keyboard's way in: `m` on a card opens every fold
// on it, and `m` again closes them. The column has no cursor of its own, so the
// key acts on the card rather than on a line; a click on a fold line acts on
// that line alone ([app.bandFoldAt]).
func (a *app) toggleAllBandFolds(subject bandSubject) {
	open := true
	for _, band := range homeBandsFor(subject.kind) {
		if !a.bandFolded(band.name, subject) {
			open = false
			break
		}
	}
	if a.home.bandOpen == nil {
		a.home.bandOpen = map[string]bool{}
	}
	for _, band := range homeBandsFor(subject.kind) {
		a.home.bandOpen[bandFoldKey(band.name, subject)] = open
	}
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
	glyph, rest := bandFoldGlyph, rows[:show]
	if !folded {
		glyph, rest = bandFoldOpenGlyph, rows
	}
	if ctx.pal.ascii {
		glyph = ">"
		if !folded {
			glyph = "v"
		}
	}
	label := "…" + strconv.Itoa(len(rows)-show) + " more " + what
	if !folded {
		label = "…" + strconv.Itoa(len(rows)-show) + " fewer"
	}
	out := append([]string(nil), rest...)
	out = append(out, ctx.pal.dim(fit(glyph+" "+label, ctx.width)))
	a.noteBandFoldLine(band, ctx.subject, strings.TrimSpace(label))
	return out
}

// bandFoldLine is one fold line drawn this frame, so a click can find it by
// its text. The column is repainted every frame and the record with it.
type bandFoldLine struct {
	band    string
	subject bandSubject
	text    string
}

func (a *app) noteBandFoldLine(band string, subject bandSubject, text string) {
	a.home.foldLines = append(a.home.foldLines, bandFoldLine{band: band, subject: subject, text: text})
}

// resetBandFoldLines is called at the top of every detail paint.
func (a *app) resetBandFoldLines() { a.home.foldLines = a.home.foldLines[:0] }

// bandFoldAt answers the fold line whose text is in a painted row, for a click.
func (a *app) bandFoldAt(rowText string) (bandFoldLine, bool) {
	plain := strings.TrimSpace(rowText)
	for _, line := range a.home.foldLines {
		if strings.Contains(plain, line.text) {
			return line, true
		}
	}
	return bandFoldLine{}, false
}

// ── the four bands that were home.go's own, now registered ─────────────────

func init() {
	registerHomeBand(homeBand{name: "state", order: bandOrderState, draw: drawStateBand})
	registerHomeBand(homeBand{name: "work", order: bandOrderWork, draw: drawWorkBand})
	registerHomeBand(homeBand{name: "facts", order: bandOrderSpend, draw: drawFactsBand})
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

// drawWorkBand: THE WORK, WITH WHAT IT CAME TO. The outcome sentence is the most
// informative text this program holds about a finished task and nothing used to
// draw it; a row that says "done" and nothing else makes a person open the
// conversation to find out what "done" meant. The layout lane replaces this
// body (name first, the outcome under it, a blank between tasks, and the fold).
func drawWorkBand(a *app, ctx bandContext) []string {
	row, width, now, pal := ctx.subject.row, ctx.width, ctx.now, ctx.pal
	var work []string
	for _, entry := range row.Tasks.Rows {
		work = append(work, homeTaskLine(entry, row, now, width, pal))
		if outcome := strings.TrimSpace(entry.Outcome); outcome != "" && width > homeOutcomeIndent+16 {
			work = append(work, strings.Repeat(" ", homeOutcomeIndent)+
				pal.dim(fit(outcome, width-homeOutcomeIndent)))
		}
	}
	return a.bandFold(ctx, "work", work, homeTaskRows*2, "tasks")
}

// drawFactsBand is the dim arithmetic under the card.
func drawFactsBand(a *app, ctx bandContext) []string {
	facts := homeFacts(ctx.subject.row, ctx.now)
	if facts == "" {
		return nil
	}
	return []string{ctx.pal.dim(fit(facts, ctx.width))}
}

// itemView is a convenience for bands that draw for items.
func (s bandSubject) itemOrNil() *standing.Item {
	if s.kind != bandKindItem {
		return nil
	}
	return &s.item.Item
}
