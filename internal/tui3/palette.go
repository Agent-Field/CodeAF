package tui3

import (
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/credits"
	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/fuzzy"
	"github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The model palette: /model with nothing after it, and omp's picker opens.
//
// It is called picker and not palette because [palette] in styles.go is already
// this surface's colour table. The FILE is palette.go because the thing it
// holds is the palette gesture — a filter box you type into, a short list under
// it, arrows to move, enter to switch, esc to close.
//
// Three properties are the whole design:
//
//   - It never fetches on its own. The list was resolved before it opened
//     (models.go), so the first frame after /model is a list and never a
//     spinner; the one fetch it makes is asked for with a key, and the list
//     stays usable while it runs (modelrefresh.go).
//   - It is bottom-anchored and takes the input line's place. The conversation
//     shrinks above it; nothing pops up over the middle of what somebody was
//     reading.
//   - ENTER APPLIES AND THE LIST STAYS UP; esc only closes, and undoes nothing.
//     Two models can be compared on their prices, chosen between and changed
//     back without the list going away ([app.pickerKey] argues it). What esc
//     does give back is the draft that was being typed and the frame — the
//     picker holds its own filter text, and the person's half-written sentence
//     is never in it. It used to close on enter and restore the model in use,
//     which made every comparison a round trip.
const pickerRows = 12

// picker is the overlay's whole state. The zero value is closed.
type picker struct {
	open bool

	// all is the list as it was resolved, and text the string each row is scored
	// against — the id, or the notice an unavailable row carries instead —
	// held once at open because deriving it per keystroke over a few hundred
	// rows is work this path does not need to repeat. The case fold the
	// matcher needs happens inside it, byte-wise, and allocates nothing.
	all  []Model
	text []string
	// score is per-model scratch, indexed by the same index as all, reused
	// across keystrokes.
	score []int
	// shared are the slugs MORE THAN ONE model on offer carries — `kimi-k3`
	// where both `moonshotai/kimi-k3` and a mirror of it are listed. A narrow
	// frame drops a row's author first (rowfit.go), and it may only do that
	// where the slug left behind still names one row.
	//
	// IT IS TAKEN OVER THE WHOLE LIST AND NOT OVER THE FILTER'S HITS, once,
	// when the list opens. A name that grew an author back because a keystroke
	// narrowed the list would be a row changing its own identity while somebody
	// was reading it, and the filter's hits change on every key.
	shared map[string]bool

	// hits are indexes into all, in rank order — the models actually on offer.
	hits []int
	// list is what is DRAWN: every hit, with the lanes of an unfolded model
	// standing in place under it ([pickRow]). The cursor and the scroll walk
	// this and not the hits, because a lane row is a row a person stops on.
	list []pickRow
	// cursor indexes list, and top is the first row drawn.
	cursor int
	top    int

	// unfold is the model whose lanes are open, empty when none is. ONE AT A
	// TIME on purpose: the fold is a way of looking closer at one row, and a
	// list with four models open is a list with no shape left.
	unfold string
	// machines is whether the `openrouter` row's OWN fold is open, showing the
	// machines behind that model.
	//
	// THE MACHINES SIT UNDER `openrouter` BECAUSE THEY ARE ITS. Every provider
	// in that list is one OpenRouter routes to; the row means "openrouter's
	// world", and naming one of its machines is a narrower answer inside that
	// world rather than a third thing beside it. `auto` is the other answer —
	// codeaf choosing — and it has no list under it because what it would
	// choose from is the same list.
	machines bool
	// lanes is what was believed about that model's lanes at the moment it was
	// opened, in the order they are drawn in.
	lanes []laneView
	// sort is which column the model list is ordered by and laneSort the same for
	// the providers inside an open fold. The zero value of each is its table's
	// first column — the name — ascending (pickersort.go's laws).
	sort     tableSort
	laneSort tableSort
	// typed is when the filter box last changed under somebody's hands, and it is
	// what tells EDITING from NAVIGATING ([picker.editing]). Zero is "nothing has
	// been typed into this list", which is navigating: there is no text to put a
	// caret in.
	typed time.Time
	// auto is the lane the CHOOSER would send the next turn to, taken with the
	// views at the moment the fold opened. It is not [bestLane]'s answer and
	// must not be: this file's own sort orders the rows a person reads, and the
	// chooser decides where a request goes — and the `auto` row is a claim about
	// the second of those. Empty when nothing is believed, which draws no name.
	auto string
	// pin is the lane ROW this conversation is held to, VERBATIM — a machine's
	// name, `auto` or `openrouter` — which is what tells the three rungs of the
	// fold apart ([picker.marked]). It is a snapshot taken when the list opened,
	// exactly as current is, and for the same reason: it answers "what am I on",
	// which cannot change while a modal overlay owns the keyboard.
	pin string
	// force is the machine the next request would actually DEMAND ([app.pinnedNow]),
	// and it is what every row that NAMES a machine draws. It differs from the
	// row above it exactly when the wire has retired the pairing: the row goes on
	// saying morph and no request asks for it, and the fold then marks `auto`,
	// which is where the requests are really going (issue #1022). A snapshot for
	// [picker.pin]'s reason.
	force string
	// guard is whether a slow answer may be rescued elsewhere. It rides here
	// because it is a fact about what `auto` PROMISES, and this list is where a
	// person decides whether to leave the choosing to it.
	guard bool
	// routing is the routing row this session was launched under ([app.routing]),
	// and it rides here for the same reason the guard does: under `simple` codeaf
	// makes no choice of its own at all, so what the `auto` row may honestly say
	// it does is a question only this row answers ([laneAutoSaid]). A snapshot,
	// like the rest of them — the row lands on the next session and cannot move
	// while a modal list owns the keyboard.
	routing string
	// laneSlot is the CONFIG SLOT whose lane row this list may write, and empty
	// when there is none.
	//
	// IT IS THE SUBJECT OF THE FOLD AND NOT THE NAME OF THE DOOR. This was a
	// `folds bool` set by /model alone, which made the settings panel's `your
	// model` row a plainer list than the overlay reached by a slash — the same
	// question, answered two ways, on one surface. It is not the door that
	// decides whether a fold means anything; it is whether enter inside it has
	// a row to write. /model and the panel's conversation row both answer
	// "which model, on which machine", so both carry [talkSlot] and both fold.
	// A media slot, a role, a task's model carry nothing: the registry has one
	// lane row ([config.LaneSlotTalk], and lanes.go's [laneSlotForRow] is the
	// whole map), so a fold under those would offer a gesture their enter could
	// not honour — and that is the emptiness law, not a door's permission.
	laneSlot string

	// current is the model in use. It is what the accent marks, and it is a
	// snapshot of what was true when the list opened — nothing running
	// underneath may move it, which is the whole of the freeze on this list.
	//
	// THE ONE THING THAT MOVES IT IS THE PERSON ([picker.restate]). Enter
	// chooses and leaves the list up, so the model in use can change while it
	// is open; a mark left on the row they had just left would be the one thing
	// on this list that was no longer true.
	current string

	// held is each model's row facts, frozen the first time this list drew
	// them. The chooser used to sample a fresh via on every frame; a running
	// turn still updates the ledger. Neither may rewrite a row somebody is
	// reading — the same snapshot law as current and pin.
	held map[string][]rowField
	// cells is the same freeze for the table's shape of row, and columns is the
	// measurement over all of them — both taken the first time this list is
	// drawn, which is the first moment [app.armLanes] has finished telling it
	// what routing is in force.
	cells   map[string][]string
	columns *colTable
	// fitted is that measurement laid out at one width, kept because the draw
	// path asks for it once for the heading and once per row and the answer
	// cannot differ between those asks. fitAt is the width it was laid out at,
	// and zero is no answer yet — a frame is never zero cells wide.
	fitted colTableFit
	fitAt  int
	// lanesFitted is the SAME measurement for the providers inside an open
	// fold, over [laneColumns] and over this model's machines alone. It is a
	// second table because it is a second question — the machines behind one
	// model are compared with each other and not with the models — and it is
	// rebuilt when the fold moves, which is the only time its rows change.
	lanesFitted colTableFit
	lanesFitAt  int
	lanesFor    string

	// task is the NODE this list is being chosen for, and 0 is the conversation —
	// which is every /model, every press on the status row out in the thread, and
	// every settings row. It is set only by [app.openTaskPicker], and what it
	// changes is where enter goes: one list, two subjects, and the subject is
	// decided when the list is opened rather than guessed at when it closes
	// ([app.pickerKey]). It is on the picker rather than on the app so that
	// [picker.close] forgets it with everything else — a target left behind by a
	// cancelled list is the next /model retargeting a task nobody was looking at.
	task uint64

	// refresh is whether this list may ask the door for today's list, and
	// fetching whether that ask is out (modelrefresh.go). Both are set by the
	// app that opened it and read by what it draws: a list with no refresh
	// behind it — the settings panel's, home's, a door with none — never names
	// the key.
	refresh  bool
	fetching bool

	// ft is the query as matcher terms, taken when [picker.rank] ran — so the
	// rows a frame draws can ask where the search landed without re-deriving
	// the query, and never disagreeing with the ranking they came from. Empty
	// when nothing is typed, and a row then simply draws as it always has.
	ft []fuzzy.Term
	// hitTerms, hitSpan and hitBuf are [picker.rowHit]'s scratch, reused across
	// rows and frames: the first two are rewritten by [fuzzy.ScoreHits], and
	// hitBuf holds the one row whose emphasis is being drawn.
	hitTerms []fuzzy.TermHit
	hitSpan  []int
	hitBuf   []int

	filter editor
}

// startFor opens the picker over the rows ONE SLOT can take: the list, narrowed
// by that slot's own question (models.go's [modelFilter]), with current marked.
//
// It is the single door every slot comes through — /model and the conversation
// rows pass [chatModel], the "looking" row passes [inspectsImages] — so the answer
// to "which models does this slot offer" is one predicate named at the call
// site rather than a list assembled there.
func (p *picker) startFor(models []Model, current string, keep modelFilter) {
	p.start(keepModels(models, keep), current)
}

// start opens the picker over models with current marked.
func (p *picker) start(models []Model, current string) {
	*p = picker{open: true, current: current}
	p.restock(models)
}

// restock puts a new list under an open picker and keeps everything else it
// holds — the filter text, the subject, the pin, the rows it has already drawn
// — which is what a refreshed list needs and what [picker.start] would forget.
// The list is ranked against the filter as typed.
func (p *picker) restock(models []Model) {
	p.all = models
	p.shared = sharedSlugs(models)
	// A NEW LIST IS A NEW MEASUREMENT. The columns were measured over the rows
	// that were on offer, and a refresh that brought a dearer model or a wider
	// name has changed that. The frozen CELLS stay, exactly as the frozen fields
	// beside them do: the freeze is against a row rewriting itself while
	// somebody reads it, not against the list being replaced — and a row whose
	// cells were kept is re-measured from those same kept cells.
	p.columns, p.fitAt = nil, 0
	p.text = make([]string, len(models))
	for i, model := range models {
		label := model.ID
		if model.Unavailable {
			label = model.Notice
		}
		p.text[i] = label
	}
	p.score = make([]int, len(models))
	// THE CURSOR GOES BACK TO THE MODEL IN USE WHATEVER IS TYPED. [picker.rank]
	// only does that for an empty box, because a keystroke that narrows the list
	// must not yank the cursor away from the row a person was walking towards —
	// but a landed list is not a keystroke, and the row they were on may no
	// longer exist. The model in use is the one row that is always there to land
	// on (the same rule [picker.start] opens with).
	p.rank()
	p.cursorToCurrent()
}

// cursorToCurrent puts the cursor on the model in use. A picker that opened on
// row zero would make enter — the key a person presses to confirm — a model
// change they did not ask for; and a box emptied back out with ctrl+u is the
// list the picker opened on, so it is the same rule again.
func (p *picker) cursorToCurrent() {
	for at, row := range p.list {
		if row.lane == laneNone && p.all[p.hits[row.hit]].ID == p.current {
			p.cursor = at
			break
		}
	}
	p.follow(pickerRows)
}

// pickRow is one drawn row: which hit it belongs to, and which of that model's
// lanes it is.
//
// laneNone is the model's own row. The two ends of an unfolded block are the
// `auto` row and the `openrouter` row rather than lanes, because neither of
// them is a machine — they are the two ways of declining to name one.
type pickRow struct {
	hit  int
	lane int
}

const (
	laneNone   = -1
	laneAutoAt = -2
	laneRoutAt = -3
	// laneDefaultAt is the last row inside the `openrouter` fold: no machine
	// named, the router's own default answering. It is a row among the machines
	// rather than a word beside them because it is the same KIND of choice —
	// "serve this from here" — and a person picking one down that list should
	// not have to leave it to pick the one that declines to pick.
	//
	// Its cells are empty, and honestly so: nothing has been measured about
	// "whatever the router feels like", because it is not one machine.
	laneDefaultAt = -4
)

// relist rebuilds the drawn rows from the hits and the fold. It is called
// wherever either changes, and nowhere else: a cursor walking a list that no
// longer exists is the whole class of bug this one function prevents.
func (p *picker) relist() {
	p.list = p.list[:0]
	for at, hit := range p.hits {
		p.list = append(p.list, pickRow{hit: at, lane: laneNone})
		if p.all[hit].Unavailable {
			continue
		}
		if p.unfold == "" || p.all[hit].ID != p.unfold {
			continue
		}
		// THE TWO ANSWERS THAT NAME NO MACHINE STAND TOGETHER, above the list
		// of machines. They used to sit at either end of it with fifteen
		// providers between them, and they are the two rows a person is
		// actually choosing BETWEEN — under the shipped routing row they even
		// send the same thing, and telling them apart means reading them side
		// by side rather than a screen apart.
		p.list = append(p.list, pickRow{hit: at, lane: laneAutoAt})
		p.list = append(p.list, pickRow{hit: at, lane: laneRoutAt})
		if !p.machines {
			continue
		}
		for i := range p.lanes {
			p.list = append(p.list, pickRow{hit: at, lane: i})
		}
		p.list = append(p.list, pickRow{hit: at, lane: laneDefaultAt})
	}
}

// close puts the picker away and forgets the filter. The next /model opens on
// the whole list, which is the only thing a person can predict; a picker that
// remembered last week's query would open onto a list with no explanation.
func (p *picker) close() { *p = picker{} }

// rank re-filters against the filter box: case-insensitive, EVERY TERM MUST
// MATCH, and each term is scored by the fzf alignment this repo keeps for all
// its pickers (internal/fuzzy).
//
// THE BOX SEARCHES NAMES AND NOTHING ELSE. It used to carry a small query
// language beside the search — `@cloudflare`, `<1s`, `>50t/s`, `$<0.3`, `fp8`,
// `tools`, `sees`, `draws`, and `fast` and `cheap` to reorder what was left —
// and every one of those words is now an ordinary thing to search for. The
// reason is the table: those terms were asking about the FACTS, and a person
// reading a column of first-token times or prices can see which rows answer
// them without describing the question in a syntax nobody can discover. What a
// person cannot see is where their model's name is in six hundred rows, and
// that is the one job left here.
//
// A QUERY LANGUAGE HAS TO BE LEARNED AND A NAME DOES NOT. `$<0.3` could only
// ever be typed by somebody who had read a page about it, while every person who
// opens this list already knows the name they are looking for — so the box that
// answers only the second is the box that answers for everybody. It also means
// there is no longer a token that silently means something other than itself:
// `fast` searches for `fast`, and the rows that come back are the rows carrying
// those letters.
//
// THE QUERY IS TOKENS AND NOT A PHRASE. A person hunting a model types the
// pieces they remember in the order they remember them, and the pieces are not
// adjacent in the id: "ds v4" is deepseek/deepseek-v4-flash, "claude 4.5" is
// anthropic/claude-sonnet-4.5. Whitespace splits, and the terms are ANDed — a
// second word narrows a list, which is the only thing typing more can sensibly
// do.
//
// THE SCORE IS THE ALIGNMENT AND NOT A LADDER OF RUNGS. The matcher pays for
// the best way a term's letters can sit in an id: a boundary bonus for landing
// after a word start, a `/` or a hyphen; a consecutive bonus for a tight run,
// floored so adjacency always beats a gap; the first character's boundary
// doubled, so a word beginning where the id begins is worth the most. A prefix
// therefore outranks a substring which outranks a scattered subsequence —
// the same ordering the tiers used to hold apart, produced by the bonus model
// instead of enforced by it, and "sonnet" over six hundred ids still puts the
// models that really carry the word above the ones that merely contain its
// letters. Higher is better, [GroupOrder] leads, and ties keep source order,
// which is the catalog's, so an empty box shows the list as handed over.
func (p *picker) rank() {
	// EVERY TOKEN IS A WORD TO RANK BY, because this box searches names and
	// nothing else — there is no grammar left for it to be read against.
	//
	// AND THE WORDS ARE FOLDED HERE, once per keystroke rather than once per
	// row. The shared matcher reads case smartly (fuzzy.Term: a capital pins
	// the word to bytes that carry it), which is right for a settings row and
	// wrong for a catalog whose every id is lowercase — `GPT` would find
	// nothing. Folding first asks the same question of the same matcher and
	// keeps the answer a person expects.
	tokens := strings.Fields(strings.ToLower(p.filter.String()))
	ft := fuzzyTerms(tokens)
	// KEPT FOR THE ROWS TO DRAW FROM: the emphasis a frame draws asks where the
	// search landed, and taking it from here is what keeps it the same ranking
	// these terms just produced.
	p.ft = ft
	now := timeNow()
	// AN OPEN FOLD IS THE SUBJECT OF WHAT IS TYPED NEXT, and it is read before
	// the fold is forgotten below ([picker.narrowFold]).
	open := p.unfold
	p.hits = p.hits[:0]
	p.unfold, p.lanes, p.auto = "", nil, ""
	if p.narrowFold(open, ft, now) {
		return
	}
	for i, text := range p.text {
		if len(tokens) == 0 {
			p.hits = append(p.hits, i)
			continue
		}
		total, hit := fuzzy.Score(text, ft)
		if !hit {
			continue
		}
		p.score[i] = total
		p.hits = append(p.hits, i)
	}
	if len(tokens) > 0 {
		sort.SliceStable(p.hits, func(a, b int) bool {
			left, right := p.all[p.hits[a]], p.all[p.hits[b]]
			if left.GroupOrder != right.GroupOrder {
				return left.GroupOrder < right.GroupOrder
			}
			return p.score[p.hits[a]] > p.score[p.hits[b]]
		})
	}
	// AND THE SORT COMES LAST, over whatever the name ranking left — so `deep`
	// then a press of the sort key is the deepseek rows by price, rather than the
	// cheapest rows that happen to say deep (pickersort.go).
	p.sortHits(len(tokens) > 0)
	// A changed query is a changed list, and a cursor left at row nine of the
	// old one points at nothing anybody chose.
	p.cursor, p.top = 0, 0
	p.relist()
	// AND A BOX WITH NOTHING IN IT IS THE LIST THE PICKER OPENED ON, so the
	// cursor goes back to where it opened: on the model in use. Emptying the
	// box with ctrl+u used to leave it on row zero, which made the enter that
	// followed a switch to whatever sorted first.
	//
	// THE SORT KEY MOVES IT TO THE TOP ITSELF ([picker.sortNext]) rather than this
	// being asked to tell the two cases apart: opening the list and pressing the
	// sort key both end here, and only the second of them wants row one.
	if len(tokens) == 0 {
		p.cursorToCurrent()
	}
}

// narrowFold answers the filter box AS A QUESTION ABOUT THE MACHINES ALREADY ON
// SCREEN, and false when it is not one — in which case the box filters models
// exactly as it always has.
//
// WHAT IS OPEN IS WHAT IS BEING ASKED ABOUT. Somebody who has walked into a
// model's fold and typed `morph` is looking at a list of machines and narrowing
// it; the shipped reading took the same keystrokes as a hunt for a MODEL, found
// `morph/morph-v3-large` in the catalog, and closed the fold they were standing
// in (issue #1022). So an open fold gets the tokens first, and only a query that
// matches none of its machines falls through to the models.
//
// IT IS THE SAME MATCHER AND NOT A SECOND ONE ([fuzzy.Score], every term
// ANDed), so `cloud fl` finds Cloudflare in a fold exactly as it finds a model
// in the list. It is a NAME search on both sides of that fall-through, which is
// the whole of what this box does ([picker.rank]): the only thing that changes
// with the fold is whose names are being searched.
func (p *picker) narrowFold(model string, ft []fuzzy.Term, now time.Time) bool {
	if model == "" || len(ft) == 0 {
		return false
	}
	at := -1
	for i, hit := range p.all {
		if hit.ID == model {
			at = i
			break
		}
	}
	if at < 0 {
		return false
	}
	views := laneViews(model, now)
	matched := make([]scoredLane, 0, len(views))
	for _, view := range views {
		if total, hit := fuzzy.Score(view.Name, ft); hit {
			matched = append(matched, scoredLane{view: view, score: total})
		}
	}
	if len(matched) == 0 {
		return false
	}
	// THE MACHINES ARE RANKED THE WAY THE MODELS ARE, by the same alignment: a
	// machine whose name carries the word at a boundary or as a prefix outranks
	// one that merely contains its letters somewhere. `core` matches
	// Cloudflare too — c-o-r-e sit in order inside the word — and a list that
	// left it on top would put the cursor on the machine nobody typed for.
	// Ties keep the ledger's own order, which is the order the fold draws when
	// nothing is typed.
	sort.SliceStable(matched, func(a, b int) bool { return matched[a].score > matched[b].score })
	kept := make([]laneView, 0, len(matched))
	for _, one := range matched {
		kept = append(kept, one.view)
	}
	// The fold's own model is the only hit, so the machines that matched are
	// drawn under the name they serve and nothing else is on the screen to
	// wonder about. The `auto` row's prediction is taken over ALL the views: it
	// is a claim about where the next turn goes and not about what was typed.
	// AND THE MACHINES ARE OPEN, because they are what was typed for. The
	// filter's whole answer is a shorter list of them, and a fold that answered
	// by narrowing a list it then left closed would have hidden the answer.
	p.hits = append(p.hits, at)
	p.unfold, p.lanes, p.machines = model, kept, true
	p.auto = laneAuto(p.routing, model, views, now)
	p.relist()
	p.top = 0
	// AND THE CURSOR LANDS ON THE FIRST MACHINE THAT MATCHED rather than on the
	// `auto` row above them, because the machines are what was asked for and
	// enter is what happens next.
	for i, row := range p.list {
		if row.lane >= 0 {
			p.cursor = i
			break
		}
	}
	p.follow(pickerRows)
	return true
}

// scoredLane is one machine of an open fold beside how well the filter box
// matched its name, so the two can be sorted together.
type scoredLane struct {
	view  laneView
	score int
}

// fuzzyTerms is the ranking words of one query as matcher terms, built once
// per keystroke and scored against every row. THE CALLER HAS ALREADY FOLDED
// THEM — every list on this surface searches things spelled in lowercase, so
// the words match case-insensitively exactly as they always have rather than
// under the matcher's own smart case; the join and split is the one small
// allocation a keystroke makes, in place of the per-row fold the old ladder
// precomputed.
func fuzzyTerms(tokens []string) []fuzzy.Term {
	if len(tokens) == 0 {
		return nil
	}
	return fuzzy.Terms(strings.Join(tokens, " "))
}

// move walks the list, clamping at both ends rather than wrapping: a list that
// wraps makes "hold ↓ until it stops" an infinite gesture.
func (p *picker) move(delta int) {
	if len(p.list) == 0 {
		return
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	at := p.cursor
	for n := 0; n < abs(delta); n++ {
		next := at
		for {
			next += step
			if next < 0 || next >= len(p.list) {
				next = at
				break
			}
			if !p.rowUnavailable(next) {
				break
			}
		}
		at = next
	}
	p.cursor = at
	p.follow(pickerRows)
}

func (p *picker) rowUnavailable(at int) bool {
	if at < 0 || at >= len(p.list) {
		return true
	}
	row := p.list[at]
	return p.all[p.hits[row.hit]].Unavailable
}

// follow scrolls the window by the least that keeps the cursor inside it.
func (p *picker) follow(height int) { p.top = listTop(p.cursor, p.top, len(p.list), height) }

// ── THE LANES UNDER A MODEL ─────────────────────────────────────────────────
//
// `→` on a row opens the machines behind it, in place, indented under the name
// they serve. It is a fold and not a second overlay for the reason there is one
// list on this surface at all: the question "which of these" and the question
// "which machine behind this one" are the same question at two depths, and
// answering the second somewhere else would make a person leave the list to ask
// it and come back to find their filter gone.
//
// A FOLD ALWAYS HAS ITS TWO ANSWERS. `auto` and `openrouter` are real, writable
// choices for every model whether or not a single machine behind it has been
// measured — they are the two ways of declining to name one — so a model nobody
// has measured opens onto exactly those two, with one dim line in the machines'
// place saying why there are none yet ([laneUnmeasured]). It draws no number:
// the emptiness law is about figures nobody took, and it is kept.
//
// It used to refuse to open at all, on the argument that a fold with one dim
// line in it punishes the person for trying. The gesture that punished was the
// one that did NOTHING: the row said `via together` and the key that should
// have shown that machine was dead, while the settings panel's `lane` row
// offered `auto` and `openrouter` for the very same model — two doors onto one
// list disagreeing about what it could do. Owner's report, 2026-09-10.
//
// AND OPENING ONE ASKS FOR ITS SHEET. [lane.WantSheet] hands the name to the
// beat that already runs and returns at once — the same door [Agent.SetModel]
// knocks on — so the machines are on the way while the person is still looking
// at the two answers. The picker still fetches nothing itself.

// unfoldAt unfolds the model at hit `at`. It reports whether anything opened.
func (p *picker) unfoldAt(at int, now time.Time) bool {
	if at < 0 || at >= len(p.hits) {
		return false
	}
	model := p.all[p.hits[at]]
	if model.Unavailable || model.Direct {
		return false
	}
	views := laneViews(model.ID, now)
	if len(views) == 0 {
		lane.WantSheet(model.ID)
	}
	// THE ROWS ARE DRAWN IN ALPHABETICAL ORDER and the chooser's order is left
	// where it is. [laneViews] hands them back fastest-feeling first, which is
	// the right order for a MACHINE TO BE PICKED BY — `auto` reads it, the
	// model row's `via` reads it — and the wrong order for a list a person
	// reads: it puts the same provider in a different place every time the
	// ledger learns something, so the eye has to start over on every visit. The
	// prediction is taken from `views` before the copy is sorted, so nothing
	// downstream is looking at this order.
	p.auto = laneAuto(p.routing, model.ID, views, now)
	p.unfold, p.lanes = model.ID, views
	// AND THE FOLD OPENS IN ITS OWN SORT, whose zero value is the name column
	// ascending — the alphabetical order these rows have always been drawn in, now
	// said once as a sort rather than twice as a sort and a special case. The
	// prediction above is taken from `views` BEFORE this, so nothing downstream is
	// looking at the order.
	p.sortLanes()
	return true
}

// fold closes whatever is open. It answers false when nothing was, so `←` on a
// folded row can fall through to the filter box's own left.
func (p *picker) fold() bool {
	if p.unfold == "" {
		return false
	}
	p.unfold, p.lanes, p.auto, p.machines = "", nil, "", false
	return true
}

// unfoldHere is `→` and `tab` on a model's row: open its machines and WALK IN.
// Inside an open block it does nothing and answers false, so `tab` can fall
// through to closing it.
//
// THE CURSOR MOVES INTO THE FOLD, onto the row that is true right now
// ([picker.cursorToPin]), and the window scrolls until the model and every row
// under it are in view ([picker.revealFold]). It used to stay on the model and
// append the rows below it — and since the picker opens with the model in use on
// the LAST row of its window, the first `→` anybody pressed changed nothing on
// the screen at all. A tree you press `→` on and nothing moves is a tree you
// believe is a list. This is [picker.start]'s own law one level down: the list
// opens ON what you are on, so enter with nothing typed confirms.
func (p *picker) unfoldHere() bool {
	if p.laneSlot == "" || p.cursor < 0 || p.cursor >= len(p.list) {
		return false
	}
	row := p.list[p.cursor]
	// THE SECOND LEVEL: `→` on `openrouter` opens the machines it routes to.
	// The key means the same thing at both depths — show me what is inside this
	// — which is the only way a tree is learnable from one press.
	if row.lane == laneRoutAt {
		// IT OPENS WITH NOTHING MEASURED TOO. The `default` row is always in
		// there, and the line saying why the machines are missing is in there
		// with it ([picker.lineUnder]) — which is the only place a person
		// looking for machines will go to find out.
		if p.machines {
			return false
		}
		p.machines = true
		p.relist()
		p.cursorToMachine()
		p.revealFold(pickerRows)
		return true
	}
	if row.lane != laneNone {
		return false
	}
	// A model whose block the filter box already opened — by narrowing that
	// model's own machines ([picker.narrowFold]) — is walked into rather than
	// opened twice.
	if p.all[p.hits[row.hit]].ID != p.unfold {
		if !p.unfoldAt(row.hit, timeNow()) {
			return false
		}
		p.relist()
	}
	p.cursorToPin()
	p.revealFold(pickerRows)
	return true
}

// revealFold scrolls by the least that puts the open block — the model's own
// row and every row under it, with the dim lines they carry — inside a window
// of height lines. Where the block is taller than the window the cursor wins,
// because the row enter would act on is the one that may never be off screen.
func (p *picker) revealFold(height int) {
	from, to, extra := -1, -1, 0
	for at, row := range p.list {
		if row.hit != p.list[p.cursor].hit {
			continue
		}
		if from < 0 {
			from = at
		}
		to = at
		if p.lineUnder(at) != "" {
			extra++
		}
		// THE PROVIDERS' HEADING IS A LINE OF THE BLOCK TOO, and a block scrolled
		// into view against a count that left it out is a block one line taller
		// than the room made for it ([picker.laneHeadBefore]).
		if p.machines && row.lane == laneRoutAt {
			extra++
		}
	}
	if from >= 0 {
		if to >= p.top+height-extra {
			p.top = to - (height - extra) + 1
		}
		if from < p.top {
			p.top = from
		}
	}
	p.follow(height)
}

// foldHere is `←` and `tab` on an open block: close it and put the cursor back
// on the model it belonged to, wherever inside the block it had got to.
func (p *picker) foldHere() bool {
	if p.cursor < 0 || p.cursor >= len(p.list) {
		return false
	}
	// `←` CLOSES THE INNERMOST THING THAT IS OPEN, one level at a time: from a
	// machine, from the `default` row beside them, or from the `openrouter` row
	// itself it shuts the machines and leaves the cursor on `openrouter`, and only
	// then does it shut the model's own fold. A key that collapsed both at once
	// would make the way in and the way out different lengths.
	//
	// `default` IS IN THIS LIST BECAUSE IT IS ONE OF THE ROWS INSIDE, and it was
	// left out when it stopped being a note beside `openrouter` and became a row
	// under it. Its lane number is negative like the two containers' are, so
	// `row.lane >= 0` — which is every real machine — did not cover it, and `←`
	// there fell through to closing the model's whole fold: two levels on one
	// press, from the one row `enter` on `openrouter` now lands the cursor on
	// ([picker.showChoice]). That is the press a person makes next, so the skip
	// was reachable by exactly the gesture most likely to reach it.
	if row := p.list[p.cursor]; p.machines && (row.lane >= 0 || row.lane == laneRoutAt || row.lane == laneDefaultAt) {
		p.machines = false
		p.relist()
		for at, drawn := range p.list {
			if drawn.lane == laneRoutAt && drawn.hit == row.hit {
				p.cursor = at
				break
			}
		}
		p.follow(pickerRows)
		return true
	}
	hit := p.list[p.cursor].hit
	if !p.fold() {
		return false
	}
	p.relist()
	for at, drawn := range p.list {
		if drawn.hit == hit {
			p.cursor = at
			break
		}
	}
	p.follow(pickerRows)
	return true
}

// cursorToMachine walks the cursor into the machines just opened: onto the one
// the requests are already going to, and onto the first of them otherwise.
//
// `→` WALKS IN AT BOTH DEPTHS. The key means "show me what is inside this" and
// then puts the cursor there; a second press that opened a list and left the
// cursor outside it would be the one gesture on this surface that does half of
// what it did a moment ago.
func (p *picker) cursorToMachine() {
	land := -1
	for at, row := range p.list {
		// A MACHINE, OR THE ROW THAT DECLINES TO NAME ONE. With nothing measured
		// the fold holds only `default`, and walking in has to land somewhere
		// that is inside it.
		if (row.lane < 0 && row.lane != laneDefaultAt) || p.all[p.hits[row.hit]].ID != p.unfold {
			continue
		}
		if land < 0 {
			land = at
		}
		if p.marked(at) {
			land = at
			break
		}
	}
	if land >= 0 {
		p.cursor = land
	}
	p.follow(pickerRows)
}

// cursorToPin puts the cursor, inside an open fold, on the row the pin names —
// the lane itself when one is pinned, and the `auto` or `openrouter` row when
// none is.
//
// It is [picker.start]'s rule applied one level down: a list opened AT a
// setting opens ON that setting's value, so enter with nothing typed confirms
// rather than changes. Every way into a fold comes through here — `→` and
// `tab` from either door ([picker.unfoldHere]), and the panel's `lane` row.
//
// A FOLD WITH NOTHING MARKED IN IT LANDS ON `auto`. That is every model but the
// one in use — a pin is this conversation's, and says nothing about a model it
// is not talking to (see [picker.rowText]) — so the true answer for that model
// right now is the one that chooses for you.
func (p *picker) cursorToPin() {
	// A PINNED MACHINE OPENS THE LIST IT IS IN. The pin is the true answer for
	// this model, so landing on it means the fold it lives in has to be open —
	// otherwise `→` on a model pinned to `cloudflare` walks onto `auto`, which
	// is the one row that is not what the next request would do.
	if !p.machines && p.force != "" {
		for _, view := range p.lanes {
			if strings.EqualFold(view.Name, p.force) {
				p.machines = true
				p.relist()
				break
			}
		}
	}
	land := -1
	for at, row := range p.list {
		if row.lane == laneNone || p.all[p.hits[row.hit]].ID != p.unfold {
			continue
		}
		if p.marked(at) {
			land = at
			break
		}
		if row.lane == laneAutoAt && land < 0 {
			land = at
		}
	}
	if land >= 0 {
		p.cursor = land
	}
	p.follow(pickerRows)
}

// pasteFilter puts clipboard text into the filter box, as ONE edit by somebody
// who is standing at that box.
//
// IT STAMPS [picker.typed] AND `/model <query>` DELIBERATELY DOES NOT, and the
// difference is where the person's hands are. A paste happens with the list
// already open and the caret already in the box — it is editing, arriving through
// a different door than the keyboard, and the arrows belong to the caret
// afterwards for the same reason they do after a typed character. Opening the
// list with text already in it ([app.openPickerFiltered], home's and a room's
// `/model <query>`) is the opposite: the query was finished before the list
// existed, so the arrows are the tree's from the first frame and a person who
// meant to edit that text still has 600ms of nothing to wait for.
//
// It also spares the insert-then-rank pair from being written out at each door;
// a paste that filtered nothing because one site forgot the second call is a bug
// this shape cannot have.
func (p *picker) pasteFilter(text string) {
	p.filter.insert(text)
	p.rank()
	p.typed = timeNow()
}

// pickerQuiet is how long the filter box must go untouched before `→` and `←`
// stop being the caret and go back to being the tree.
//
// IT IS A QUIET WINDOW AND NOT A DEADLINE, which is the difference between a
// mode that follows a pair of hands and one that expires mid-word: every edit
// pushes it out again, so a person typing at any speed keeps the caret keys for
// as long as they are typing, and the moment they stop is the moment the arrows
// mean the list.
//
// SIX HUNDRED MILLISECONDS IS CHOSEN AGAINST TYPING CADENCE rather than against
// what feels like a pause in the abstract. Ordinary typing puts 100–200ms between
// keys and a correction burst is faster than that, so 600ms cannot land inside a
// word; and a hand moving from the letters to an arrow key takes about that long,
// so by the time the arrow is pressed on purpose the window has usually closed.
// Longer and the list feels stuck behind text nobody is editing any more; much
// shorter and a thinking pause mid-name would take the caret away.
const pickerQuiet = 600 * time.Millisecond

// editing reports whether the caret keys still belong to the FILTER BOX rather
// than to the tree.
//
// THE TWO GESTURES COLLIDE ON ONE PAIR OF KEYS and something has to break the
// tie. `→` and `←` are what a hand reaches for at a tree, and they are also how
// a caret walks text. The tie used to be broken by POSITION alone — the arrows
// were the tree's only at the very start and the very end of what was typed —
// and that made the commonest gesture in this list cost four presses: type
// `deep`, press `→` to open the providers, then press `←` to come back out and
// watch the caret step backwards through `p`, `e`, `e`, `d` while the fold
// stayed open.
//
// So the tie is broken by TIME as well, and time is the honest signal: somebody
// still editing is still pressing keys. The position rule is kept on top of this
// one, because `→` at the end of a word has nowhere to step and was always the
// tree's — which is what makes an empty box, where this list spends most of its
// life, behave exactly as it always did.
//
// COMING BACK IS ANY EDIT AT ALL — a character, a backspace, a kill, an undo, or
// `ctrl+b`/`ctrl+f`, which are the caret's own keys and never the tree's
// ([picker.navigate] stamps them all). That last pair is the way out of this mode
// that does not change a single letter of the query, and it is why the mode
// cannot trap anybody: the arrows went to the list, so the arrows' understudies
// are still there to take the caret back.
func (p *picker) editing() bool {
	return !p.typed.IsZero() && timeNow().Sub(p.typed) < pickerQuiet
}

// foldKey is `tab`, `→` and `←` over this list — the fold's whole key map, in
// one place because the list has two doors ([app.pickerKey] and settings.go's
// [app.sheetSelectKey]) and a gesture that opened the machines from one of them
// and did nothing from the other is the exact drift [picker.navigate] exists to
// prevent. It answers whether it took the key; false falls through to the walk.
//
// THE ONE COMPROMISE THE FOLD COSTS. `tab` opens and closes outright, because
// it means nothing else in a box you type into. `→` and `←` are the keys a
// person reaches for at a tree, and they are ALSO how the caret walks the
// filter text — so they open and close only from the END and the START of what
// is typed, where there is no character left to step over. With an empty box,
// which is where this list spends most of its life, that is every press.
func (p *picker) foldKey(name string) bool {
	switch name {
	// ── THE SORT IS THE LIST'S KEY AND NOT A DOOR'S ─────────────────────────
	//
	// It is read here, with the fold's keys, for [picker.foldKey]'s own reason:
	// there are four doors onto this list and an order that could be changed from
	// one of them and not the others would be four lists again. It is read BEFORE
	// the filter box because `alt+s` is not text — the chord exists so that a bare
	// `s` stays the commonest first letter a person types into this box
	// (taskstable.go argues it for the tasks page's filter, and the argument is
	// the same one here).
	case pickerSortKeyChord:
		p.sortNext(false)
		return true
	case pickerSortBackChord:
		// AND SHIFT WALKS THE CYCLE BACKWARDS. Every column is two rungs now — its
		// own direction and the other one ([tableSort.step]) — so "the previous
		// rung" is what this chord can mean, and it retraces exactly what the
		// unshifted key visited rather than being a second way to say "reverse".
		p.sortNext(true)
		return true
	case "tab":
		if !p.unfoldHere() {
			p.foldHere()
		}
		return true
	case "right":
		return (!p.editing() || p.filter.cursor >= len(p.filter.value)) && p.unfoldHere()
	case "left":
		return (!p.editing() || p.filter.cursor <= 0) && p.foldHere()
	}
	return false
}

// showChoice opens what a row just chosen is the LID of, and does nothing for a
// row that is not one. It runs after the write, so the mark it reveals is the
// answer that was just written and not the one before it.
//
// `enter` ON `openrouter` CHOOSES `default` — the container and the row inside it
// write the same thing (lanes.go's [app.applyLaneChoice]) — AND THAT IS EXACTLY
// WHY IT HAS TO OPEN. Shut, the gesture reads as "you have chosen openrouter",
// which sounds like a destination and hides that there was a list under it at
// all. Open, it reads as the true sentence: here are the machines this routes
// between, and the answer you just gave is `default`, the one that declines to
// pick among them.
//
// THE MARK DOES THE TALKING AND IT ALREADY WORKED THIS WAY. [picker.marked] gives
// the container the mark only while it is SHUT and gives it to `default` once it
// is open, and [picker.cursorToMachine] walks onto the marked row — so opening is
// the whole of the change, and what a person sees is the cursor landing on
// `default` with the band on it.
func (p *picker) showChoice(row pickRow) {
	if row.lane == laneRoutAt {
		p.unfoldHere()
	}
}

// laneUnder is the lane row the cursor is on: the view, and which of the three
// kinds of row it is. It is what enter reads before it writes anything.
func (p *picker) laneUnder() (pickRow, bool) {
	if p.cursor < 0 || p.cursor >= len(p.list) {
		return pickRow{}, false
	}
	row := p.list[p.cursor]
	if row.lane == laneNone {
		return pickRow{}, false
	}
	return row, true
}

// ── the overlay grammar, shared by every list this surface opens ────────────
//
// There is ONE bottom-anchored list on this surface and three things open it:
// /model (picker, above), a typed "/" (the command list, commands.go) and a
// typed "@" (the file completion, files.go). They share the three functions
// below — the cursor walk, the scroll, and the row — so that they cannot drift
// into three overlays that each look almost like the others. What differs
// between them is what they LIST, which is the only thing that should.

// moveCursor walks a list of count rows by delta, clamping at both ends.
func moveCursor(cursor, delta, count int) int {
	if count == 0 {
		return 0
	}
	cursor += delta
	if cursor < 0 {
		return 0
	}
	if cursor >= count {
		return count - 1
	}
	return cursor
}

// listTop scrolls a window of `height` rows by the least that keeps the cursor
// inside it.
func listTop(cursor, top, count, height int) int {
	if height <= 0 {
		return top
	}
	if cursor < top {
		top = cursor
	}
	if cursor >= top+height {
		top = cursor - height + 1
	}
	if top > count-height {
		top = count - height
	}
	if top < 0 {
		return 0
	}
	return top
}

// overlayRow is ONE row of ONE overlay, and every list draws through it.
//
// Three tiers and no fourth. What the row is ABOUT — the model in use, and
// nothing else so far — is accent wherever it sits in the list. The row under
// the cursor is ink and bold, so it stays the brightest thing on a monochrome
// terminal too. Everything else is dim, because a list of six hundred names
// that all shout is a list nobody can read down. The note trails on the right
// and the label gives way before it does: a truncated name is still
// recognizable, and "164k" cut in half is a wrong number.
//
// THE EMPHASIZED ROW IS A GROUND, AND THE WHOLE LINE IS IN IT. Emphasis used to
// be a bold label and nothing else, which left the cursor row reading as half a
// row: the lead was accent, the name was bright, and the tail that carries the
// window, the price and the arena score stayed dim grey — the three facts a
// person is actually comparing, greyed out on the one row they were comparing
// them ON. So the emphasis now spans the line, lead to note, padded to the full
// width, and the note joins it in ink rather than staying behind in dim.
//
// WHICH STEP EACH ROW DRAWS IS THE GROUND LADDER'S ANSWER AND NOT THIS FILE'S.
// A list has up to three things going on at once and they are three different
// facts, so they take three different rungs:
//
//	the keyboard cursor   THE CURSOR STEP — where ↑/↓ has got to
//	the pointer's row     THE CURSOR STEP — the same rung, deliberately
//	the marked row        THE SELECTED STEP — the one this terminal is IN
//
// CURSOR AND HOVER ARE ONE STEP, NOT TWO. This surface used to draw them at two
// different weights, on the argument that a pointer crossing a list must not
// look like the cursor moving. The ladder refuses that argument: whether a
// person arrived at a row with the mouse or with `↓`, the row they are on is the
// row they are on, and it does not change appearance depending on which hand
// they used. What still tells the two apart is the LEAD — `›` where enter would
// act, `·` where the pointer is — which is a mark and not a rung.
//
// AND THE MARKED ROW IS THE ONE THAT WAS MISSING ITS GROUND. The conversation
// this terminal is actually in is the definition of the ladder's selected step —
// the chosen thing, persistent, still true when nobody is touching the list —
// and it used to be an accent label on the bare terminal background while the
// row a person was merely scrolling PAST wore the louder ground. That is the
// ladder upside down, and a roster where the session you are sitting in looks
// less chosen than the one under the cursor is a roster that answers "where am
// I" with the wrong row.
func overlayRow(label, note string, selected, marked, hovered bool, width int, pal palette) string {
	// The two-valued form every list but home draws: marked or not, which is
	// [markFront] or [markNone].
	return overlayRowTinted(label, note, nil, selected, markIf(marked), hovered, width, pal)
}

// markIf is the two-valued mark said in the three-valued type. Only home has a
// third state, because only home lists conversations this terminal is holding.
func markIf(marked bool) rowMark {
	if marked {
		return markFront
	}
	return markNone
}

// noteInk is how a row's trailing fact is painted, for the one list where the
// tail is not a fact but an ANSWER (connectcaps.go's capability rows: yes, ask
// first, off). Every other list wants the rule below — dim, and ink on the
// selected row — and passes nil to say so.
//
// It is a hook rather than a second row-drawing function because the row is the
// row: the lead, the band, the hover step and the two-line law at [tierPhone]
// are decided in one place for every list on this surface, and a list that drew
// its own would be a second grammar to keep in step.
type noteInk func(pal palette, note string, selected bool) string

// paintNote is the ordinary rule, and the hook where one was given.
func paintNote(tint noteInk, pal palette, note string, selected bool) string {
	if tint != nil {
		return tint(pal, note, selected)
	}
	if selected {
		return pal.ink(note)
	}
	return pal.dim(note)
}

// rowMark is how strongly a row is marked as THE ONE THIS TERMINAL IS IN. It is
// three-valued because a terminal can now hold several conversations: the one on
// screen, the ones open behind it, and everything else on the machine.
//
// IT IS A PAINT AND NOT A WORD, on purpose. Home's left column is forty-six
// cells wide and every column spent on furniture is a column taken from the name
// the row is about — which is the argument the short spelling of `another
// window` already makes one file over.
type rowMark uint8

const (
	// markNone is a row this terminal does not hold.
	markNone rowMark = iota
	// markOurs is a conversation this terminal has open behind the one on
	// screen: the same treatment as the front one, at the tier below it.
	markOurs
	// markFront is the CHOSEN ROW OF AN OVERLAY — the model in use, the
	// conversation a picker would re-open — and it keeps the ladder's selected
	// step: an overlay is a modal list with a visible cursor in it, and the
	// chosen row's band is the language those lists have always spoken.
	markFront
	// markHere is home's own conversation — the row esc drops back into — and
	// it is a SEPARATE mark because home is a dashboard, not an overlay: a
	// persistent band on a resting page read, every time, as a cursor nobody
	// had moved. It paints the label in the body ink, takes no ground at all,
	// and says what it is in a word instead (home.go's homeHereWord).
	markHere
)

// overlayNoteRoom is HOW MANY CELLS A ROW'S NOTE MAY HAVE, given the label in
// front of it and the width of the frame.
//
// It is a function rather than four lines inside [overlayRowTinted] because a
// list that RANKS ITS FACTS has to know the answer before it composes the note:
// rowfit.go drops whole facts rather than cutting one in half, and it can only
// do that if it is told the budget it is fitting into. The settings sheet's
// money rows are the first callers (settingspend.go) and the arithmetic is the
// one below, unchanged and in one place, so the budget a caller fits into is by
// construction the budget this row will hand it.
func overlayNoteRoom(label string, width int) int {
	room := width - 2
	floor := ansi.StringWidth(label)
	if half := room - room/2; floor > half {
		floor = half
	}
	return room - floor - rowGutter
}

// overlayMeasure is HOW WIDE A LABEL/TAIL PAIR IS LAID OUT, however wide the
// frame is. It is a reading measure the way [teachMeasure] is one for prose, and
// it is wider because a row carries structure a paragraph does not.
//
// A pair is read by jumping the eye from the name on the left to the value
// right-aligned against it, and past about a hundred cells that jump stops
// landing: at a hundred and sixty columns the settings row `ssh reuse` put a
// hundred and fifty blank cells in front of `300s`, and a person scanning the
// column read the wrong value against the wrong row. So the tail stops
// travelling right at the measure and the rest of the frame is simply left
// empty, which is what every other wide-frame surface here already does.
const overlayMeasure = 100

// overlayPairRoom is THE ROOM A ROW'S PAIR IS LAID OUT IN — the cells the tail
// is right-aligned inside — and it is ONE function because both of this row's
// faults were the same expression judged at two widths. `width - 2` with a
// one-cell floor under the gap ran `300s` out to the frame's edge a hundred and
// fifty cells from `ssh reuse` at a hundred and sixty, and butted `per task`
// against its own value with a single word space at eighty. The measure answers
// the first; [rowGutter] answers the second.
//
// TWO ROWS KEEP THE WHOLE FRAME, and both are law 1 (rowfit.go): a row with NO
// TAIL, because the measure is about the gap between two things and a name alone
// has nothing to be far from; and a pair that does not FIT the measure, because
// pulling the tail in on that row would cut the identity to buy a margin.
func overlayPairRoom(label, note string, width int) int {
	room := width - 2
	if note == "" {
		return room
	}
	need := ansi.StringWidth(label) + rowGutter + ansi.StringWidth(note)
	if need < overlayMeasure {
		need = overlayMeasure
	}
	if need < room {
		room = need
	}
	return room
}

// ── the search's emphasis ───────────────────────────────────────────────────
//
// A list a person is typing into answers them at a glance by saying WHICH
// LETTERS matched. The matcher (internal/fuzzy) hands back the exact bytes
// the winning alignment touched, and the row carries those bytes in bold
// over the row's own ink — the one emphasis this surface already owns, the
// user/assistant distinction's (styles.go's [palette.bold]). No new colour,
// no ground, and the rest of the row untouched: the mark is a reading aid
// for the scan, not a louder row, which is what "subtle" asked for.

// rowLabelInk is a label's own ink for its row's state — the switch every
// row already paints through, said once so the searched rows and the plain
// ones keep one grammar ([overlayRowCore] and [overlayLinesCore] below).
func rowLabelInk(pal palette, marked rowMark, lit bool) func(string) string {
	switch {
	case marked == markFront:
		return pal.accent
	case marked == markHere:
		// HOME'S OWN CONVERSATION IS A FACT, NOT A SELECTION. It wore the
		// front mark's accent-on-selected band for a wave, and on a resting
		// dashboard that band was the loudest thing in sight — read, every
		// time, as a cursor nobody had moved. Ground bands on home mean one
		// thing only: where a person's hands are. So the row says what it is
		// the way every identity on this surface is said — in words: ink for
		// the label (readable above its dim siblings, junior to nothing) and
		// `here` on the tail (homeNote), with no ground and no accent.
		return pal.ink
	case marked == markOurs:
		// Open here, and not the one being drawn. One tier under the here
		// row's ink, so a person's eye reads "this terminal has these" as one
		// group rather than as two unrelated paints.
		return pal.muted
	case lit || pal.placeRows:
		return pal.ink
	default:
		return pal.dim
	}
}

// paintHit paints a row's label in the row's own ink with the bytes the
// search matched carried in bold. hit is ascending byte indices into label;
// the walk is by rune, so an emphasis run can never cut one in half, and a
// position that is not a rune's first byte — or lands past a [fit] cut — is
// simply not drawn. lit is the row that is already bold whole, the cursor's:
// there a second bold marks nothing, so the span rides the row's bold
// quietly rather than wrapping bold inside bold and closing it early.
// An empty hit is the plain paint exactly, so a list that is not searched
// never changes what it drew.
func paintHit(pal palette, label string, hit []int, lit bool, paint func(string) string) string {
	if len(hit) == 0 || lit {
		// An empty hit is the plain paint exactly; a LIT row is bold whole
		// already and the span rides that quietly — one wrap, byte for byte
		// the row it always was, with no bold opened inside a bold that a close
		// could cut short.
		return paint(label)
	}
	var out strings.Builder
	from, hot, hi := 0, false, 0
	emit := func(to int, on bool) {
		if from == to {
			return
		}
		if on && !lit {
			out.WriteString(pal.bold(paint(label[from:to])))
		} else {
			out.WriteString(paint(label[from:to]))
		}
	}
	for at := 0; at < len(label); {
		_, size := utf8.DecodeRuneInString(label[at:])
		for hi < len(hit) && hit[hi] < at {
			hi++
		}
		on := hi < len(hit) && hit[hi] == at
		if at == 0 {
			hot = on
		} else if on != hot {
			emit(at, hot)
			from, hot = at, on
		}
		at += size
	}
	emit(len(label), hot)
	return out.String()
}

// hitUnion is where a search landed on ONE field: the byte positions of
// every term that won it, appended to dst — merged, ascending, with no
// duplicates — as the span a row draws its emphasis from. Only the appended
// part is sorted, so a caller laying rows' spans end to end in one buffer
// (the settings sheet's build) keeps each row's own bytes in order.
// Positions pointing past a field's own length, a label a fitter narrowed,
// are the caller's to keep honest; this only merges what it is given.
func hitUnion(hits []fuzzy.TermHit, field int, dst []int) []int {
	at := len(dst)
	for _, one := range hits {
		if one.Field != field {
			continue
		}
		dst = append(dst, one.Pos...)
	}
	span := dst[at:]
	sort.Ints(span)
	// The dedup, in place: two terms may land on the same bytes.
	n := 0
	for _, at := range span {
		if n == 0 || span[n-1] != at {
			span[n] = at
			n++
		}
	}
	return dst[:at+n]
}

func overlayRowTinted(label, note string, tint noteInk, oncursor bool, marked rowMark, hovered bool, width int, pal palette) string {
	return overlayRowCore(label, note, nil, tint, oncursor, marked, hovered, width, pal)
}

// overlayRowHitTinted is [overlayRowTinted] with the search's emphasis: the
// bytes hit names in the label carry the bold ([paintHit]).
func overlayRowHitTinted(label, note string, hit []int, tint noteInk, oncursor bool, marked rowMark, hovered bool, width int, pal palette) string {
	return overlayRowCore(label, note, hit, tint, oncursor, marked, hovered, width, pal)
}

// overlayRowCore is ONE row of ONE overlay, and every list draws through it —
// [overlayRowTinted] for the lists that are not searched, [overlayRowHitTinted]
// for the ones that are. hit is the search's emphasis, nil for none.
func overlayRowCore(label, note string, hit []int, tint noteInk, oncursor bool, marked rowMark, hovered bool, width int, pal palette) string {
	lead := overlayLead(oncursor, hovered, pal)
	// THE NOTE IS CUT TO THE ROW BEFORE THE ROW IS BUDGETED AROUND IT. The label
	// absorbs whatever the note leaves and the gap below clamps at one cell, so a
	// note longer than the terminal used to be appended WHOLE to an empty label —
	// the row ran past the edge by however long the note was, and no amount of
	// squeezing the label could pull it back. What it may take is everything but
	// the lead and the gutter. Settings' `tool exceptions` is the row
	// that found it: a value naming ten tools is 141 cells against a 60-cell
	// terminal, which is LAW 1 (a place takes exactly the frame) broken by a
	// value a person chose.
	room := width - 2
	if note != "" {
		// AND THE LABEL KEEPS A FLOOR UNDER IT. The label used to absorb
		// whatever the note left, which on a long note left it NOTHING: the row
		// drew a full-width value with no name in front of it, and a person
		// reading down the column could not tell which setting they were
		// looking at. So the note may take the row's second half and no more —
		// or all of it but the label's own width, when the label is the shorter
		// of the two — and the label gives way only inside what is left.
		//
		// EVERY LIST THAT RANKS ITS FACTS HANDS US A NOTE THAT ALREADY FITS
		// (rowfit.go drops whole facts rather than cutting one in half), so this
		// is the floor under the lists that pass a note they did not budget.
		note = fit(note, overlayNoteRoom(label, width))
	}
	if note != "" {
		room -= ansi.StringWidth(note) + rowGutter
	}
	label = fit(label, room)

	// lifted is whether this row wears a ground at all, which is the one thing
	// the note's ink turns on: dim grey on a raised ground is grey on grey.
	lifted := oncursor || hovered || marked == markFront
	// ON A PLACE THE POINTER'S ROW IS THE CURSOR'S ROW, word for word: the
	// same ground, the same bold subject (styles.go's [palette.placeRows]).
	lit := oncursor || (hovered && pal.placeRows)

	// THE LABEL'S OWN INK, THEN THE EMPHASIS: a searched row carries the bytes
	// the search matched in bold over that ink ([rowLabelInk] is the switch
	// every row already paints through, said once), and the rest of the label
	// keeps it — the mark is on the matched letters and nowhere else.
	ink := rowLabelInk(pal, marked, lit)
	painted := paintHit(pal, label, hit, lit, ink)
	if lit {
		painted = pal.bold(painted)
	}
	line := lead + painted
	if note != "" {
		// THE TAIL IS RIGHT-ALIGNED INSIDE THE MEASURE AND NOT INSIDE THE FRAME
		// ([overlayPairRoom]).
		gap := overlayPairRoom(label, note, width) - ansi.StringWidth(label) - ansi.StringWidth(note)
		if gap < rowGutter {
			gap = rowGutter
		}
		// THE NOTE IS INSIDE THE GROUND, so it is painted as part of it: dim ink
		// on a raised ground is grey on grey, and the tail is the half of the row
		// a person is reading when they stop on it.
		line += strings.Repeat(" ", gap) + paintNote(tint, pal, note, lifted)
	}
	// AN OVERLAY'S CHOSEN ROW OUTRANKS THE CURSOR ON THE ROW IT SHARES WITH IT
	// — both can be true of one row, and the louder step wins so the row never
	// gets quieter for being arrived at; the cursor is still said, on the lead.
	// Home's own conversation deliberately is not in this switch: on a
	// dashboard the ground is the hand's and only the hand's ([markHere]).
	switch {
	case marked == markFront:
		return pal.selected(line, width)
	case oncursor, hovered:
		return pal.cursor(line, width)
	}
	return line
}

// overlayLead is the two cells in front of every row: the cursor's mark, the
// pointer's, or nothing. It is its own function because a wrapped row draws it
// on the first line and pads to it on the second — the same two cells either
// way, so the label starts in the same column on both.
func overlayLead(selected, hovered bool, pal palette) string {
	switch {
	case pal.placeRows:
		// A PLACE'S ROW WEARS NO MARK. The ground says which row a hand is on and
		// the bold subject says it again; an accent `›` beside them was a second
		// accent on a screen whose one accent belongs to the live thing. The one
		// cell left is the place's own edge ([placeLead]), where the row's own
		// glyph stands.
		return placeLead
	case selected:
		return pal.accent("› ")
	case hovered:
		return pal.accent("· ")
	}
	return "  "
}

// ── the two-line row, at tierPhone ──────────────────────────────────────────
//
// A row is a label and a dim tail of facts, and on a wide frame they share one
// line with the label giving way first ([overlayRow]). On a phone there is no
// width to share: an id and "200k · $3/$15 per M · elo 1300" cannot both be on
// a forty-four-cell line, and the row that came out of that arithmetic was a
// truncated name beside a truncated number — the two halves of the row both
// cut, neither readable.
//
//	› anthropic/claude-sonnet-4.5
//	    200k · $3/$15 per M · elo 1300
//
// So at [tierPhone] the tail takes a line of its own, indented under the label
// it belongs to. THE PAIR IS ONE ROW and everything downstream treats it as
// one: the selection band spans both lines, the pointer over either line is
// over the row, and the window never draws the first line of a pair whose
// second would not fit (see [overlayFill]).
//
// A row with no tail — most of the file completion's paths — stays one line.
// A blank second line under every path would spend half the screen saying
// nothing.

// laneIndent is how far a provider row hangs in from the frame's own edge: the
// two cells every row pays for its cursor mark, the two that put `auto` and
// `openrouter` under the model, and two more that put the machines under
// `openrouter` — which is where they now live ([picker.relist]).
const laneIndent = 6

// overlayIndent is where a wrapped tail starts: the row's own two-cell lead,
// plus two more so the tail reads as hanging under the label rather than as a
// row of its own.
const overlayIndent = 4

// phoneList reports whether lists on a frame this wide wrap their tails.
func phoneList(width int) bool { return layoutTier(width) == tierPhone }

// overlayItemLines is how many SCREEN lines one row takes. It is the ONE
// answer, asked by the fill that draws the rows and by the height that reserves
// the frame's rows for them — two counts that must agree or the list is drawn
// into a block of the wrong size.
func overlayItemLines(width int, note string) int {
	if note != "" && phoneList(width) {
		return 2
	}
	return 1
}

// overlayLines is one row as the lines it takes: [overlayRow] everywhere, and
// the label/tail pair at [tierPhone].
func overlayLines(label, note string, selected, marked, hovered bool, width int, pal palette) []string {
	return overlayLinesTinted(label, note, nil, selected, marked, hovered, width, pal)
}

// overlayLinesHit is [overlayLines] for a list under a search: the bytes hit
// names in the label carry the emphasis and the rest of the row keeps its
// own ink.
func overlayLinesHit(label, note string, hit []int, selected, marked, hovered bool, width int, pal palette) []string {
	return overlayLinesHitTinted(label, note, hit, nil, selected, marked, hovered, width, pal)
}

func overlayLinesTinted(label, note string, tint noteInk, selected, marked, hovered bool, width int, pal palette) []string {
	return overlayLinesCore(label, note, nil, tint, selected, marked, hovered, width, pal)
}

// overlayLinesHitTinted is [overlayLinesTinted] with the search's emphasis:
// the bytes hit names in the label carry the bold ([paintHit]).
func overlayLinesHitTinted(label, note string, hit []int, tint noteInk, selected, marked, hovered bool, width int, pal palette) []string {
	return overlayLinesCore(label, note, hit, tint, selected, marked, hovered, width, pal)
}

// overlayLinesCore is one row as the lines it takes: [overlayRowCore]
// everywhere, and the label/tail pair at [tierPhone]. hit is the search's
// emphasis over the label, nil for none.
func overlayLinesCore(label, note string, hit []int, tint noteInk, selected, marked, hovered bool, width int, pal palette) []string {
	if overlayItemLines(width, note) == 1 {
		return []string{overlayRowCore(label, note, hit, tint, selected, markIf(marked), hovered, width, pal)}
	}
	head := overlayLead(selected, hovered, pal)
	lit := selected || (hovered && pal.placeRows)
	// The label keeps the row's own ink ([rowLabelInk]) and carries the
	// search's emphasis in bold over it, exactly as the one-line row does.
	painted := paintHit(pal, fit(label, width-2), hit, lit, rowLabelInk(pal, markIf(marked), lit))
	if lit {
		painted = pal.bold(painted)
	}
	head += painted

	// The tail keeps the row's own ink rule: dim, and ink on the selected row,
	// because dim grey on the selection band is grey on grey — and the tail is
	// the half of the row a person stopped on the row to read.
	tail := strings.Repeat(" ", overlayIndent) +
		paintNote(tint, pal, fit(note, width-overlayIndent), lit)

	switch {
	case lit && pal.placeRows:
		// ONE GROUND FOR BOTH HANDS ON A PLACE, the one the single-line row wears.
		return []string{pal.cursor(head, width), pal.cursor(tail, width)}
	case selected:
		return []string{pal.selected(head, width), pal.selected(tail, width)}
	case hovered:
		return []string{pal.cursor(head, width), pal.cursor(tail, width)}
	}
	return []string{head, tail}
}

// overlayWindow is how many lines the rows from top take, stopping at the
// ceiling the list was given. A row that would straddle the bottom edge is not
// counted, because it is not drawn ([overlayFill.add]).
//
// note answers what row i's tail is — the only thing the count needs, since the
// tail is what decides whether the row is one line or two.
func overlayWindow(width, top, count, ceiling int, note func(int) string) int {
	lines := 0
	for at := top; at < count && lines < ceiling; at++ {
		take := overlayItemLines(width, note(at))
		if lines+take > ceiling {
			break
		}
		lines += take
	}
	return lines
}

// overlayItems is the item-space window a list follows its cursor within, given
// the SCREEN rows the frame handed it. At [tierPhone] a row can be two lines, so
// half the rows is the count that cannot overflow — which is what makes the
// cursor's row always fit whole inside the window it is scrolled into.
func overlayItems(n, width int) int {
	if phoneList(width) {
		return n / 2
	}
	return n
}

// overlayFill accumulates one list's lines into exactly the n rows the frame
// reserved for it. Every list on this surface draws through it, so the two-line
// law, the pointer's row and the bottom edge are decided once.
type overlayFill struct {
	out   []string
	owner []int
	n     int
	width int
	pal   palette
	// hover is the pointer's row within the block, in SCREEN lines — which is
	// what the frame records (view.go's chromeOverlay) and not what the list
	// counts in. A row is hovered when the pointer is on EITHER of its lines.
	hover int
}

func newOverlayFill(width, n int, pal palette, hover int) *overlayFill {
	return &overlayFill{out: make([]string, 0, n), owner: make([]int, 0, n), n: n, width: width, pal: pal, hover: hover}
}

// room reports whether another line will fit.
func (f *overlayFill) room() bool { return len(f.out) < f.n }

// add draws one row, and reports whether it fit. A two-line row with one line of
// room left does NOT fit: half a row at the bottom of a list is a label whose
// facts are on the next screen, and a selection band with one end cut off.
//
// at is what the row belongs to — the index a pointer resolves back to — or -1
// for a line that answers to nothing.
func (f *overlayFill) add(at int, label, note string, selected, marked bool) bool {
	return f.addTinted(at, label, note, nil, selected, marked)
}

// addTinted is [overlayFill.add] with the note's own ink named ([noteInk]), for
// the lists whose tail is not a fact but an ANSWER — the capability rows, and
// the intake card's filled fields, where the value a person put in is the datum
// the row is about and steps to ink while the schema's prose beside it stays
// dim (docs/DESIGN-LANGUAGE.md's payload rule).
//
// It is the same function and not a second one, because the lead, the ground,
// the pointer's row and the two-line law at [tierPhone] are decided here for
// every list on this surface, and a list that drew its own would be a second
// grammar to keep in step.
func (f *overlayFill) addTinted(at int, label, note string, tint noteInk, selected, marked bool) bool {
	return f.addCore(at, label, note, nil, tint, selected, marked)
}

// addHit is [overlayFill.add] for a list under a search: the bytes hit names
// in the label carry the emphasis ([paintHit]) and the rest of the row keeps
// its own ink.
func (f *overlayFill) addHit(at int, label, note string, hit []int, selected, marked bool) bool {
	return f.addCore(at, label, note, hit, nil, selected, marked)
}

// addCore is the one draw behind [overlayFill.add], [addTinted] and [addHit]:
// hit is the search's emphasis, nil for none, and tint the note's own ink
// where one was given.
func (f *overlayFill) addCore(at int, label, note string, hit []int, tint noteInk, selected, marked bool) bool {
	take, flat := overlayItemLines(f.width, note), false
	if len(f.out)+take > f.n {
		// EXCEPT ON A FRAME WITH ONE ROW TO GIVE. A list that answered a one-row
		// window with a blank would be an overlay that opened onto nothing,
		// which is worse than the truncation this whole surface is about: the
		// pair is a way of READING a row, and no row at all is not a better one.
		// So the first row of a window too short for a pair falls back to the
		// one line every wider frame draws.
		if len(f.out) > 0 || f.n < 1 {
			return false
		}
		take, flat = 1, true
	}
	hovered := f.hover >= len(f.out) && f.hover < len(f.out)+take
	lines := overlayLinesHitTinted(label, note, hit, tint, selected, marked, hovered, f.width, f.pal)
	if flat {
		lines = []string{overlayRowHitTinted(label, note, hit, tint, selected, markIf(marked), hovered, f.width, f.pal)}
	}
	for _, line := range lines {
		f.out = append(f.out, line)
		f.owner = append(f.owner, at)
	}
	return true
}

// plain adds a line that is not a row — a section rule, a "nothing matches" —
// already painted by its caller.
func (f *overlayFill) plain(line string) bool {
	if !f.room() {
		return false
	}
	f.out = append(f.out, line)
	f.owner = append(f.owner, -1)
	return true
}

// done closes the block: blanks under the last row where the items ran out
// before the frame's rows did.
//
// THE BLOCK IS EXACTLY THE HEIGHT IT WAS PROMISED. The frame subtracts that
// height from the conversation before the list is drawn ([app.overlayHeight]),
// and a list that came back a line short would leave the frame a line short of
// the terminal. It can only happen at [tierPhone], where a row's height depends
// on the row; everywhere else the count and the rows agree exactly, so nothing
// is padded and the block is byte-for-byte the one this surface always drew.
func (f *overlayFill) done() ([]string, []int) {
	if phoneList(f.width) {
		for len(f.out) < f.n {
			f.out = append(f.out, "")
			f.owner = append(f.owner, -1)
		}
	}
	return f.out, f.owner
}

// choice is the model under the cursor, and false when the filter matched
// nothing — enter on an empty list must change nothing at all.
func (p *picker) choice() (Model, bool) {
	if !p.open || p.cursor < 0 || p.cursor >= len(p.list) {
		return Model{}, false
	}
	model := p.all[p.hits[p.list[p.cursor].hit]]
	return model, !model.Unavailable
}

// height is how many LIST rows the picker wants, not counting the filter box —
// the box sits in the input line's place and costs the frame nothing. One row
// is reserved for the "no model matches" line, because a filter that matches
// nothing has to say so where the list was.
//
// THE CEILING IS IN LINES AND NOT IN MODELS, which is what keeps the overlay
// the same size on every frame: [pickerRows] rows of a phone are six models
// with their facts under them rather than twelve models with their facts cut
// off, and either way the list takes the same twelve rows from the screen.
func (p *picker) height(width int) int {
	switch {
	case !p.open:
		return 0
	case len(p.list) == 0:
		return 1
	}
	// The why line rides with the row it explains, so it is counted the same
	// way: one line, inside the ceiling, and never half of a pair. The fetching
	// line is counted inside the same ceiling, so the overlay does not grow
	// while a refresh is out.
	//
	// THE TABLE'S HEAD IS THE ONE LINE COUNTED OUTSIDE IT, and the difference is
	// that it does not come and go. A fetch is a thing that is happening to the
	// list for a second or two, so an overlay that grew for it would jump under
	// somebody's hands and jump back; the heads stand over the columns for as
	// long as the list is open, and twelve rows is a promise about how many
	// MODELS you can see (the manual makes it in those words). Charging the
	// heading to the models would quietly make it eleven.
	ceiling := pickerRows + p.tableHead(width)
	lines := p.headLines(width)
	for at := p.top; at < len(p.list) && lines < ceiling; at++ {
		if p.groupBefore(at) != "" {
			lines++
		}
		// The providers' own heading is counted where it is drawn, for
		// [overlayItemLines]' reason: the count here and the lines the fill
		// actually writes must agree or the list is laid into a block of the
		// wrong size.
		if p.laneHeadBefore(at, width) != "" {
			lines++
		}
		_, note := p.entryText(at, width, nil)
		take := overlayItemLines(width, note)
		if p.lineUnder(at) != "" {
			take++
		}
		if lines+take > ceiling {
			break
		}
		lines += take
	}
	return lines
}

// rows draws exactly n list rows. n comes from [app.overlayHeight], which is
// this picker's own height clamped to what the terminal can give, so a short
// window shows fewer rows rather than a frame that does not fit.
//
// level answers what a model has been dialled to; it is passed in rather than
// looked up here because the answer lives on the agent (see [app.reasoningFor])
// and the picker is a list, not a thing that holds a session.
func (p *picker) rows(width, n int, pal palette, hover int, level func(string) string) []string {
	lines, _ := p.rowsOwned(width, n, pal, hover, level)
	return lines
}

// rowsOwned is [picker.rows] with the hit each LINE belongs to, or -1. The
// settings panel puts this list inside its own frame and resolves clicks
// against it (settings.go's [sheet.selectLines]), and "the hit is the line's
// index from the top" stopped being true the moment a row could be two lines.
func (p *picker) rowsOwned(width, n int, pal palette, hover int, level func(string) string) ([]string, []int) {
	if n <= 0 {
		return nil, nil
	}
	if len(p.list) == 0 {
		return []string{pal.dim("  " + p.emptyLine())}, []int{-1}
	}
	fill := newOverlayFill(width, n, pal, hover)
	if p.fetching {
		fill.plain(pal.dim("  " + modelsFetching))
	}
	// THE HEADS STAND OVER THE COLUMNS, and they are what lets a row carry a
	// bare figure at all: `$0.09` means nothing until `in $/M` is over it
	// (modeltable.go). It is drawn under the fetching line rather than above it,
	// because the fetching line is about the LIST and the heads are about the
	// rows, and the heads must be the last thing before the first row they
	// describe.
	if head := p.tableFit(width).header(); head != "" {
		fill.plain(pal.head(fit(head, width)))
	}
	p.follow(overlayItems(n-p.headLines(width), width))
	for at := p.top; at < len(p.list) && fill.room(); at++ {
		if group := p.groupBefore(at); group != "" {
			if !fill.plain(pal.dim(fit("  "+group, width))) {
				break
			}
		}
		// THE PROVIDERS' OWN HEADING, drawn where their block starts and nowhere
		// else. A bare `0.8s 58 $1.3` under `openrouter` is six figures with
		// nothing saying which is which, and this table is a table for the same
		// reason the model list is ([picker.laneFit]).
		if head := p.laneHeadBefore(at, width); head != "" {
			if !fill.plain(pal.head(fit(head, width))) {
				break
			}
		}
		if p.rowUnavailable(at) {
			model := p.all[p.hits[p.list[at].hit]]
			if !fill.plain(pal.dim(fit("  "+model.Notice, width))) {
				break
			}
			continue
		}
		label, note := p.entryText(at, width, level)
		if !fill.addHit(at, label, note, p.rowHit(at, label), at == p.cursor, p.marked(at)) {
			break
		}
		// THE WHY LINE IS UNDER THE CURSOR AND NOWHERE ELSE. One sentence about
		// the row a person has stopped on is an explanation; the same sentence
		// under every row is a wall, and the numbers above it stop being read.
		// It is indented to where a machine's name starts, which is also where
		// a fold with no machines says why ([picker.lineUnder]).
		if under := p.lineUnder(at); under != "" {
			if !fill.plain(pal.dim(fit(strings.Repeat(" ", overlayIndent+2)+under, width))) {
				break
			}
		}
	}
	return fill.done()
}

// laneHeadBefore is the providers' heading line when row `at` is the FIRST
// machine of an open block, and empty everywhere else — the same shape
// [picker.groupBefore] has for a service's name, and for the same reason: a
// heading belongs to the block under it and a scrolled window that starts
// mid-block draws it again at the top.
func (p *picker) laneHeadBefore(at, width int) string {
	if at < 0 || at >= len(p.list) || p.list[at].lane < 0 {
		return ""
	}
	if at > 0 && p.list[at-1].lane >= 0 && at != p.top {
		return ""
	}
	return p.laneFit(width).header()
}

// restate moves the marks after a choice has been made with the list still
// open, which is what `enter` now leaves it ([app.pickerKey]).
//
// THE MARKS ARE SNAPSHOTS AND THAT IS STILL RIGHT — nothing running underneath
// may move them ([picker.current] says why). What just happened is not
// something running underneath: it is the person pressing enter, and a list
// that went on marking the model they had just left would be the one thing on
// the row that was no longer true.
//
// A door that writes something else — a task's model, a settings row, home's
// draft — passes what IT now holds, because the mark is about that door's
// subject and not about this window's conversation.
func (p *picker) restate(current, pin, force string) {
	if !p.open {
		return
	}
	p.current, p.pin, p.force = current, pin, force
}

// restatePicker is [picker.restate] with the two lane answers read off the
// profile this window writes to — the row VERBATIM, which is what tells `auto`
// from `openrouter` ([picker.marked]), and the machine the wire would actually
// demand.
func (a *app) restatePicker(p *picker, current string) {
	pin := ""
	if p.laneSlot != "" {
		pin = config.LaneAt(a.profileDir, p.laneSlot)
	}
	p.restate(current, pin, a.pinnedNow())
}

// rowHit is where the search landed on one drawn model row: the bytes of its
// name the query matched, as offsets into the label the row is DRAWN with —
// the emphasis [overlayFill.addHit] carries in bold. The span is the optimal
// alignment's own bytes ([fuzzy.ScoreHits]), which is why it is asked for
// here rather than remembered from the ranking: the rows a frame draws are
// the few, and the span costs nothing on the ones it does not.
//
// THE LABEL IS THE ROW'S OWN READING OF THE NAME, not the string the search
// scored: the fitter drops an author that names no second row and cuts a
// name that will not fit in the middle. A whole name maps straight onto its
// row; an author dropped maps with its offset carried; a name cut in the
// middle has no honest mapping, and the row draws unemphasised rather than
// bolding bytes that are not its own.
//
// ONLY THE MODEL'S OWN ROW IS ASKED: the fold's lane rows and its `auto` and
// `openrouter` ends answer a different question (a machine the router
// knows, a way of declining to name one) and are drawn from sentences of
// their own.
func (p *picker) rowHit(at int, label string) []int {
	if len(p.ft) == 0 {
		return nil
	}
	row := p.list[at]
	if row.lane != laneNone {
		return nil
	}
	text := p.text[p.hits[row.hit]]
	off := 0
	switch {
	case strings.HasPrefix(label, text):
		// The whole name, perhaps with a level rider the search never saw.
	case strings.HasSuffix(text, label):
		// The author gave its name away and the slug names the row alone.
		off = len(text) - len(label)
	default:
		return nil
	}
	if _, ok := fuzzy.ScoreHits(text, p.ft, &p.hitTerms, &p.hitSpan); !ok {
		return nil
	}
	// THE SPAN IS THIS ROW'S OWN: the buffer is rewritten per row, so the
	// merged positions of the row above never bleed into the one being drawn.
	span := hitUnion(p.hitTerms, 0, p.hitBuf[:0])
	n := 0
	for _, pos := range span {
		if pos < off {
			// Where the dropped author stood: not the row's bytes.
			continue
		}
		span[n] = pos - off
		n++
	}
	p.hitBuf = span[:n]
	return p.hitBuf
}

// pinnedLane is the MACHINE this conversation is held to, and empty for every
// row that names none — `auto`, `openrouter`, and a pairing the wire has retired.
//
// It exists because [picker.pin] is the settings row VERBATIM, which is what
// [picker.marked] needs to tell the three rungs of the fold apart, and is
// exactly the wrong thing to hand to anything that draws a lane's name: the row
// reading `auto` drew a model whose speed came `via auto`, a machine no router
// has ever heard of. It is [picker.force] and not that row for the second half
// of the same rule — a name may be drawn only while a request would demand it
// (lanes.go's [laneInForce]).
func (p *picker) pinnedLane() string { return p.force }

// marked is the row an overlay's chosen band belongs to: the model in use, and
// — inside an open fold — the lane this conversation is actually held to, which
// is the machine the next request would demand when there is one and the `auto`
// row when there is not.
//
// THE MACHINE ROWS ARE MARKED AGAINST WHAT THE WIRE WOULD DEMAND and the two
// answers that name no machine against the ROW, because those two are the only
// thing the row says that the wire cannot: `auto` and `openrouter` both send no
// lane, and only the row knows which of them a person wrote. So a pin the wire
// has retired marks `auto` — the requests are going there — while the row itself
// still reads `pinned: morph` on the panel that owns it.
func (p *picker) marked(at int) bool {
	if p.rowUnavailable(at) {
		return false
	}
	row := p.list[at]
	model := p.all[p.hits[row.hit]]
	switch {
	case row.lane == laneNone:
		return model.ID == p.current
	case model.ID != p.current:
		return false
	case row.lane == laneAutoAt:
		return p.force == "" && !strings.EqualFold(p.pin, config.LaneOpenRouter)
	case row.lane == laneDefaultAt:
		return strings.EqualFold(p.pin, config.LaneOpenRouter)
	case row.lane == laneRoutAt:
		// THE CONTAINER WEARS THE MARK ONLY WHILE IT IS SHUT. Open, the row
		// that holds this answer is visible and wears it itself; marking both
		// would draw one answer twice.
		return !p.machines && strings.EqualFold(p.pin, config.LaneOpenRouter)
	}
	return p.force != "" && strings.EqualFold(p.force, p.lanes[row.lane].Name)
}

// groupBefore is the dim service heading before a model row. Folded lane rows
// stay under their model, and a scrolled window repeats the heading at its top
// so a service name is never left above the viewport.
func (p *picker) groupBefore(at int) string {
	if at < 0 || at >= len(p.list) || p.list[at].lane != laneNone {
		return ""
	}
	model := p.all[p.hits[p.list[at].hit]]
	if model.Group == "" {
		return ""
	}
	if at == p.top || at == 0 {
		return model.Group
	}
	previous := p.list[at-1]
	if previous.lane != laneNone {
		return ""
	}
	before := p.all[p.hits[previous.hit]]
	if before.Group != model.Group {
		return model.Group
	}
	return ""
}

// entryText is one drawn row as the row's two halves. level may be nil, which
// is what the height ask passes: the effort rides on the label and the height
// only needs the tail.
func (p *picker) entryText(at int, width int, level func(string) string) (string, string) {
	row := p.list[at]
	model := p.all[p.hits[row.hit]]
	switch row.lane {
	case laneNone:
		dial := ""
		if level != nil {
			dial = level(model.ID)
		}
		return p.rowText(model, dial, width)
	case laneAutoAt:
		// THE NAME ON THIS ROW IS THE CHOOSER'S AND NOT THE SORT'S. "auto weighs
		// speed against price each answer — coreweave now" is a claim about where
		// the NEXT REQUEST would go, and only the chooser answers that; the
		// order the rows are drawn in is this file's own reading of the same
		// beliefs and is allowed to differ.
		//
		// AND ON A NARROW FRAME THE SENTENCE BECOMES THE NAME. What the row is
		// FOR is a thing you read once; which machine it would send you to now
		// is the thing you came back to look at, so the short spelling keeps the
		// name and drops the explanation around it.
		//
		// AND WHAT IT MAY CLAIM AT ALL IS THE ROUTING ROW'S TO SAY, asked once
		// ([laneAutoSaid]) rather than read off the mode here: under `simple`
		// nothing on this side chooses, so there is no machine to name and no
		// rescue to promise.
		said := laneAutoSaid(p.routing)
		sentence := said.note
		short := ""
		if said.chooses && p.auto != "" {
			sentence += " — " + strings.ToLower(p.auto) + " now"
			short = strings.ToLower(p.auto) + " now"
		}
		fields := []rowField{rowSay(sentence, short)}
		// AND WHAT AUTO WILL NOT DO, said where the choice is made. With the
		// speed guard off, a lane that turns slow mid-answer is one you wait
		// out; that is a fact about this row and it belongs on it. Where the
		// routing runs no rescue in the first place the sentence above has
		// already said so, and a chip repeating it is the same fact twice.
		if said.chooses && !p.guard {
			fields = append(fields, rowSay("no rescue"))
		}
		return rowHalves(rowPlan{primary: "  auto", fields: fields}, width, 0)
	case laneRoutAt:
		// NO SENTENCE ON THIS ROW. It said `default routing`, which is the
		// answer the `default` row inside it now carries — and a container that
		// describes one of the things it contains reads like a third choice.
		return rowHalves(rowPlan{primary: "  openrouter"}, width, 0)
	case laneDefaultAt:
		label := strings.Repeat(" ", laneIndent-2) + laneDefaultWord
		if fit := p.laneFit(width); fit.drawn() {
			return label, fit.row(make([]string, len(laneColumns)))
		}
		return label, ""
	}
	// A MACHINE SITS UNDER `openrouter`, indented past it, because it is one of
	// the machines that row routes to ([picker.machines] says why the list
	// lives there). The two answers that name no machine stand at the block's
	// own margin above it.
	//
	// AND IT IS A TABLE, the same engine the model list is drawn with
	// ([picker.laneFit]) — the providers behind one model are read down the
	// page and compared, which is what a column is for. Where the frame cannot
	// hold one, the ranked tail it has always drawn is what it falls back to.
	view := p.lanes[row.lane]
	if fit := p.laneFit(width); fit.drawn() {
		name, _ := rowTrim(strings.ToLower(view.Name), fit.name, false)
		return strings.Repeat(" ", laneIndent-2) + name, fit.row(laneCells(view))
	}
	label, note := rowHalves(laneRowPlan(view), width, overlayIndent)
	return strings.Repeat(" ", overlayIndent) + label, note
}

// ── WHY THESE TWO ROWS WEAR NO MARK ─────────────────────────────────────────
//
// `auto` and `openrouter` used to carry a filled and a hollow bullet, and the
// mark was doing two jobs. The first — saying these two are not machines — the
// INDENT already does, and does better now they stand together above the list
// rather than at either end of it ([picker.relist]).
//
// The second was `filled for the answer that chooses for you, hollow for the one
// that declines to`, and that one was not true where most people read it. Under
// the shipped `simple` routing row NEITHER of them chooses: auto sends no lane
// either ([laneAutoSaid]), so the filled bullet claimed a difference the wire
// does not make. A mark that is wrong on the default install is worse than no
// mark, and the sentence beside each row says what it does in words.

// laneDefaultWord is the row inside the `openrouter` fold that names no machine
// — what this build did before it held an opinion, and what it still does when
// nobody has asked for anything.
//
// IT IS A WORD AND NOT A SENTENCE because it stands in a list of machines and
// is read as one of them. The row it used to be — `openrouter · default
// routing` — described the container instead of the choice.
const laneDefaultWord = "default"

// laneAutoSay is what the `auto` row may honestly claim, as the routing row in
// force decides it: the sentence saying what leaving the choosing alone DOES,
// and whether codeaf is the one doing any of the choosing.
type laneAutoSay struct {
	// note is the sentence on the row. It is a sentence and not a word because
	// it is the row a person will land on first and the one they will leave
	// alone: what it is FOR has to be on it.
	note string
	// about is the same promise as the settings sheet's `lane` row explains it
	// ([settingUI], read through [sheet.metaFor]). It is a second wording and
	// not a second decision: the sheet's row is a paragraph about what the four
	// answers to "which machine" mean and the picker's row is a label inside
	// the list, and the two would say different things about `simple` the first
	// time either was written without the other.
	about string
	// chooses is whether codeaf chooses anything under this routing row. It
	// gates the two claims that are only true when it does: the name of the
	// machine the next turn would go to — which is the CHOOSER'S answer
	// (lanes.go's [laneAuto]), and a prediction nobody makes where no chooser
	// runs — and the `no rescue` chip, which is a fact about a speed guard that
	// has nothing to guard.
	chooses bool
}

// laneAutoSaid reads the routing row and answers it once, so that what the row
// promises is decided in ONE place rather than at each thing the row draws.
//
// THE SENTENCE IS A PROMISE AND A PROMISE HAS TO BE KEPT UNDER EVERY ROW. Under
// `latency` and `price` codeaf does take over when the router's answers turn
// bad, and the row has said so since it was written. Under `simple` it does
// not: the request goes out with no preference of codeaf's own on it and
// OpenRouter's own default routing answers, which is exactly the row a person
// chose in order to be left alone — so the row that still said "codeaf takes
// over" would be the surface promising machinery the mode disconnected.
//
// An unknown word — an empty one, or a row read before this surface armed
// anything — is the shipped routing, and the sentence follows it there rather
// than keeping a favourite of its own: [config.RoutingWord] is the one place
// that says what an unwritten row is in force as, and the shipped row is
// `simple`, so a surface that fell through to the takeover sentence would be
// promising the machinery the shipped row disconnects.
func laneAutoSaid(routing string) laneAutoSay {
	// THE SENTENCE SAYS WHAT THIS ROW IS FOR AND THEN WHETHER IT IS DOING IT.
	// `codeaf tries to pick the best provider` is the whole of what auto means,
	// and under the shipped `simple` routing row it is a thing codeaf is not
	// allowed to do — so the row that would otherwise read exactly like
	// `openrouter` says which setting is holding it back, and names it. A
	// sentence true only on a setting most people have not got is a sentence
	// that lies on the default install.
	if config.RoutingWord(routing) == config.RoutingSimple {
		return laneAutoSay{
			note: laneAutoNote,
			about: "which provider answers your model. routing is simple, so auto " +
				"sends no choice of ours at all and openrouter's own routing answers; a provider " +
				"you pin is the whole request. enter opens them all with what has been " +
				"measured of each.",
		}
	}
	return laneAutoSay{
		note: laneAutoNote,
		about: "which provider answers your model. auto picks the fastest one " +
			"each answer; enter opens them all with what has been measured of each.",
		chooses: true,
	}
}

// laneAutoNote is what the `auto` row is FOR.
//
// IT POINTS AT THE SETTING RATHER THAN NAMING ITS VALUE. It said `codeaf tries
// to pick the best provider — not while routing is simple` for one wave, and
// `simple` is a word nobody meets before this row: it is one of four values on
// a `routing` setting nothing on this screen mentions. A sentence you have to
// already know the answer to is not a hint. `/settings` is a place a person can
// go, so the row names that and the setting explains itself when they get
// there.
//
// AND IT IS ONE SENTENCE UNDER EVERY ROUTING ROW, which is the other half of
// the same fix. `according to /settings` is true whichever value is set —
// that is what makes it honest without having to be rewritten per value.
const laneAutoNote = "auto-route based on /settings"

// laneUnmeasured is the one line a fold draws in the providers' place when
// nothing behind the model has been measured. It is a sentence a person would
// say, it draws no number, and it says when that changes — which is the whole
// of what somebody who pressed `→` on the model needs to know about the gap.
const laneUnmeasured = "no provider has been measured for this model yet — providers show up after its first answer"

// lineUnder is the dim line drawn under one row, and empty under all but one of
// them: [laneUnmeasured], under the `auto` row of a fold with no providers in
// it, standing exactly where the providers would.
//
// THE CURSOR'S LANE ROW USED TO CARRY A SENTENCE HERE TOO, and it is gone. It
// read `baseten: first token 0.4s, steady 64 t/s, no tail — from the sheet`
// under the row that already read `baseten   0.4s · 64 t/s · $1.2/M · out ≤ 32k
// · 69%` — the same three numbers in prose, under a name the row had just said,
// one line further from the eye. It was written when the lane row was thinner
// than it is now and it outlived the row filling in; what it added at the end
// was the provenance clause, which is one fact about the LIST rather than about
// the row the cursor happens to be on.
//
// AND IT COST A ROW OF THE FOLD, every time, on the one list where a row is a
// machine somebody is comparing against fifteen others.
func (p *picker) lineUnder(at int) string {
	if at < 0 || at >= len(p.list) {
		return ""
	}
	// IT IS UNDER `openrouter` AND INSIDE ITS OPEN FOLD, which is where somebody
	// went looking: the machines are that row's, so their absence is that row's
	// to explain, and it stands exactly where they would.
	if p.list[at].lane == laneRoutAt && p.machines && len(p.lanes) == 0 {
		return laneUnmeasured
	}
	return ""
}

// rowText is one model as the row's two halves: the id with whatever level it
// has been dialled to, and the dim tail of facts (models.go's [modelNote] —
// window, price, arena score). The model in use is the marked row — that is the
// mark, and it survives scrolling past it.
//
// The level goes with the ID and not into the tail, because it is the one thing
// on the row that is not a fact about the model: it is what THIS person asked
// for, it reads the same here as it does in the status line ("<model>:<level>"),
// and the tail stays what the catalog said.
func (p *picker) rowText(model Model, level string, width int) (string, string) {
	// THE PIN IS THIS CONVERSATION'S AND SO IT RIDES ON THIS CONVERSATION'S ROW.
	// A lane pinned while you were on one model is not a claim about the next
	// one, so every other row's `via` is what auto would do (lanes.go).
	pin := ""
	if model.ID == p.current {
		pin = p.pinnedLane()
	}
	plan := rowPlan{
		primary: model.ID,
		// THE AUTHOR IS DROPPABLE WHERE THE SLUG STILL NAMES ONE ROW. Which is
		// a question about the LIST and not about the model, so the list
		// answers it once when it opens ([picker.shared]).
		author: !p.shared[rowSlug(model.ID)],
		fields: p.rowFields(model, pin),
	}
	// THE LEVEL RIDES THE NAME AND IS NEVER CUT. It is the one thing on the row
	// that is not a fact about the model — it is what THIS person asked for, and
	// it reads the same here as it does in the status line ("<model>:<level>") —
	// so the fitter reserves it and fits the id into what is left (rowfit.go).
	if level != "" {
		plan.suffix = ":" + level
	}
	// THE TABLE IS THE ROW'S SHAPE WHEREVER THERE IS ROOM FOR ONE, and the
	// ranked tail is what it falls back to below that and at tierPhone
	// (modeltable.go argues the trade). They are the same facts in the same
	// order either way, so a frame that crosses between them loses a column and
	// never a subject.
	if fit := p.tableFit(width); fit.drawn() {
		name, _ := rowTrim(plan.primary, fit.name-ansi.StringWidth(plan.suffix), plan.author)
		// A CUT NAME KEEPS ITS FACTS HERE, WHICH IS LAW 1's ONE EXCEPTION and it
		// is the table that earns it. In a tail, the facts beside an ellipsis
		// are a second loss on a row that has already spent what it was drawn to
		// say. In a table they are not the row's, they are the COLUMN's: a blank
		// where the window should be reads as "nobody published one" on every
		// other row of this list (the emptiness law), so blanking it for want of
		// cells would make this row lie about the catalog. The name gives way
		// and the columns hold.
		return name + plan.suffix, fit.row(p.rowCells(model, pin))
	}
	return rowHalves(plan, width, 0)
}

// tableFit is this list's columns laid out at one width — the measurement taken
// once ([modelTable.add] over every row) and fitted fresh, since the measurement belongs to the
// list and the fit belongs to the frame, and only one of those two changes when
// a terminal is dragged wider.
//
// A PHONE NEVER TABLES. At [tierPhone] the tail already has a line of its own
// under the name and there is no second column to put anything in; a table
// there would be one column of figures with a heading nobody can see the other
// half of.
func (p *picker) tableFit(width int) colTableFit {
	if phoneList(width) {
		return colTableFit{}
	}
	p.measure()
	if p.fitAt != width {
		p.fitted, p.fitAt = p.columns.fit(width, p.sort.column(), p.sort.arrow()), width
		p.fitted.name0 = modelHead
	}
	return p.fitted
}

// measure builds this list's column measurement, once, and is what every question
// about the columns goes through.
//
// IT IS SEPARATE FROM THE FIT BECAUSE IT IS NOT ABOUT THE FRAME. The measurement
// is over the rows and answers "what did this catalog publish"; the fit is over a
// width and answers "what fits". They were one function, and the join meant a
// question that needs only the measurement could not be asked until a frame had
// been drawn — which is exactly what the sort cycle needs
// ([picker.sortable]): it skips the columns nobody filled, and before the first
// draw it could not tell, so a key pressed then walked onto a rung that ordered
// nothing. A phone draws no table at all and still has a list to sort.
func (p *picker) measure() {
	if p.columns != nil {
		return
	}
	table, pin := newColTable(modelColumns, 0), p.pinnedLane()
	for _, model := range p.all {
		// A NOTICE IS NOT A MODEL. An unavailable service's row carries a
		// sentence where an id would be and draws no facts at all, so measuring
		// it would widen columns for a row that uses none of them.
		if model.Unavailable {
			continue
		}
		// THE PIN IS THIS CONVERSATION'S ROW ALONE ([picker.rowText] says why),
		// and it rides into the measurement because a column measured without it
		// would be one machine's name too narrow for the one row that matters
		// most.
		held := ""
		if model.ID == p.current {
			held = pin
		}
		table.add(p.rowCells(model, held), nameAsk(model))
	}
	p.columns = &table
	p.fitAt = 0
}

// laneFit is the providers' own table, measured over the machines of the model
// whose fold is open and laid out in what the frame leaves after their indent.
//
// IT IS REBUILT WHEN THE FOLD MOVES and not on every draw: the rows are this
// model's machines, frozen when the fold opened the way everything else on this
// list is ([picker.lanes]), so the measurement can only change when the fold
// does.
func (p *picker) laneFit(width int) colTableFit {
	if phoneList(width) || len(p.lanes) == 0 {
		return colTableFit{}
	}
	if p.lanesFitAt != width || p.lanesFor != p.unfold {
		table := newColTable(laneColumns, laneIndent-2)
		for _, view := range p.lanes {
			table.add(laneCells(view), ansi.StringWidth(strings.ToLower(view.Name)))
		}
		p.lanesFitted = table.fit(width, p.laneSort.column(), p.laneSort.arrow())
		p.lanesFitted.name0 = laneHead
		p.lanesFitAt, p.lanesFor = width, p.unfold
	}
	return p.lanesFitted
}

// rowCells is one model's facts in column order, frozen the first time this
// list drew them — [picker.rowFields]' rule, for the table's shape of row and
// for the same reason: a running turn still updates the ledger, and a row may
// not rewrite itself under somebody who is reading it.
func (p *picker) rowCells(model Model, pin string) []string {
	if p.cells == nil {
		p.cells = make(map[string][]string)
	}
	if cells, ok := p.cells[model.ID]; ok {
		return cells
	}
	cells := modelFactsOf(model, pin, p.routing).cells()
	p.cells[model.ID] = cells
	return cells
}

// rowFields is the tail of one model, frozen the first time this list drew
// it. A second paint — a frame tick, a token arriving under the overlay —
// must not re-ask the chooser or re-age the ledger: that is the flicker
// `/model` used to show, `via` hopping and the speed rewriting itself.
func (p *picker) rowFields(model Model, pin string) []rowField {
	if p.held == nil {
		p.held = make(map[string][]rowField)
	}
	if fields, ok := p.held[model.ID]; ok {
		return fields
	}
	fields := modelFields(model, pin, p.routing)
	p.held[model.ID] = fields
	return fields
}

// ── reasoning strength, from the row it belongs to ──────────────────────────
//
// ctrl+t walks the model under the cursor through auto → low → medium → high →
// xhigh → max → auto. It is the SAME walk a task's own thinking control takes,
// and auto is absence: the model is handed back to whatever stands below its
// per-model pin. The level is stored ON THE AGENT, per model id
// (internal/session's agent.go), which is what makes it survive the picker
// closing, a switch away and a switch back — and what makes /new forget it,
// since /new is a new agent.
//
// IT IS ctrl+t AND NOT t. The filter box takes every printable key, and a bare
// t would mean nobody could type "sonnet", "mistral" or "gpt" into a list whose
// whole purpose is being typed into. A modifier is the price of a type-to-filter
// overlay, and it is the cheaper half of that trade by a wide margin.
//
// The key does nothing on a model whose catalog row does not accept a reasoning
// knob ([Model.Reasoning]), and nothing is exactly what it should do: the level
// would be a 400 at the next turn, and refusing to offer it is how the surface
// declines to sell something the endpoint will not honour.

// nextReasoning is the level after this one, wrapping.
func nextReasoning(level string) string {
	rung, known := effort.Parse(level)
	if !known {
		// A level from a build that knew a word this one does not resolves to
		// absence, which is the one answer that cannot surprise anybody.
		return effort.None.String()
	}
	return effortNextClearing(rung).String()
}

// The level a model is held at is read through [app.reasoningFor], which lives
// in reasoninglevel.go: the draw path asks it three times a frame and it must
// never touch the agent, because over a connection the agent is another machine.

// cycleReasoning is ctrl+t: the selected row's model moves one step round the
// cycle, or nothing happens because that model takes no reasoning knob.
//
// IT ASKS THE AGENT WHERE THE ROW STANDS WHEN NOBODY HAS ASKED YET, which is
// the one place the surface's held answer is not good enough: the cycle is
// RELATIVE, so starting it from "not told yet" would walk a model already
// dialled to high back down to low. This is a keystroke — the fourth of
// reasoninglevel.go's seeded moments, and waiting is what a keystroke may do.
//
// AND THE NEW LEVEL IS WRITTEN THROUGH, so the row under the cursor changes on
// the very next frame. The surface is the only thing that sets these, so what it
// just set is what is true.
func (a *app) cycleReasoning() {
	chosen, ok := a.pick.choice()
	if !ok || !chosen.Reasoning || a.agent == nil {
		return
	}
	if _, known := a.levels[session.ReasoningKey(chosen.ID)]; !known {
		a.learnLevel(chosen.ID)
	}
	a.setLevel(chosen.ID, nextReasoning(a.reasoningFor(chosen.ID)))
}

// slashPickerTack is the command left standing in the box while the model list is
// open ([draftBlockTacked]). It is spelled once, here, rather than at the doors
// that draw this box, and it carries its slash because that is how a person typed
// it and how the command list spells it.
//
// A test asserts this names a command the surface actually has, since a chip for a
// command nobody can type would be the box teaching a gesture that does not exist.
const slashPickerTack = "/model"

// pickerHint is the placeholder in the empty filter box, and it names the box
// and the one key that is not about walking the list.
//
// THE KEYS MOVED TO THE FOOT, and the reason they had to is that a placeholder
// is the one line on the screen that DISAPPEARS THE MOMENT SOMEBODY USES IT.
// `→ providers` and `ctrl+t effort` lived here, so they were gone by the first
// typed character — exactly when a person has found their model and wants its
// machines — and at home they were worse than gone: they named two keys that
// door did not answer at all (homedraft.go now arms the fold and holds the
// rung). The foot is a line that stays, follows the cursor, and can say what
// each key does WHERE IT DOES IT ([picker.keysHint]).
//
// WHAT IS LEFT IS WHAT THE FOOT CANNOT SAY. `filter by name` is what the box IS,
// which no foot can tell you about an empty box, and the refresh key belongs to
// the LIST rather than to the row the cursor is on — the foot is cursor-shaped
// and this key is not.
//
// AND IT SAYS `by name` BECAUSE THAT IS THE WHOLE SCOPE OF IT. The box once took
// a query language over the facts as well ([picker.rank] buries it), and while it
// did, `filter` was the honest word: it could not promise names without
// under-selling the rest. Now that names are all it answers, a person who types
// `cheap` deserves to have been told, in the one line that was on the screen
// before they typed, that this box was never going to understand them. The two
// spellings degrade to `filter` on a narrow frame (rowfit.go's second law), where
// naming the box at all beats naming its scope.
const pickerHint = "filter by name · " + refreshModelsHint

// pickerHintFields is that same line as the fields it is made of, ranked. The
// test that joins them and compares against [pickerHint] is what keeps the two
// spellings one (the one-source-of-truth law: a constant read by a person and a
// list read by the fitter would otherwise drift).
var pickerHintFields = []rowField{rowSay("filter by name", "filter"), rowSay(refreshModelsHint)}

// pickerHintFieldsBare is the line for a list that cannot be refreshed: the
// same fields with the refresh key taken out, since a key that does nothing is
// a key that must not be named.
var pickerHintFieldsBare = slices.DeleteFunc(slices.Clone(pickerHintFields),
	func(field rowField) bool { return field.full == refreshModelsHint })

// pickerHintAt is the hint in the cells the box actually has, naming the
// refresh key only when refresh says the list answers it.
func pickerHintAt(room int, refresh bool) string {
	if refresh {
		return rowTail(pickerHintFields, room)
	}
	return rowTail(pickerHintFieldsBare, room)
}

// hintAt is this list's own placeholder: the refresh key is named while the
// list answers it and not while a fetch is already out.
func (p *picker) hintAt(room int) string { return pickerHintAt(room, p.offersRefresh()) }

// sortKeyWord is how the foot names the sort, and it is one constant because the
// spelled-out rows above and the cursor-shaped foot below have to say it the same
// way (the one-source-of-truth rule; [pickerHint]'s own test compares the two).
const sortKeyWord = pickerSortKeyChord + " sort"

// effortKeyWord is the chord that walks the rung of the model under the cursor,
// named in the foot since [pickerHint] stopped naming it in the box.
const effortKeyWord = "ctrl+t effort"

// The hint slot's words while this list is open, by where the cursor is. They
// are named constants because the manual and the tests quote them, and they are
// built from the pieces [picker.keysParts] returns so the two cannot drift.
const (
	// pickerKeysModel is a model's row on a list that folds: `→` opens the
	// providers behind it and walks in, `ctrl+t` dials how hard it thinks, and
	// enter switches.
	//
	// THE EFFORT KEY IS NAMED HERE BECAUSE THE BOX STOPPED NAMING IT
	// ([pickerHint]), and this is the row it works on: inside a fold the cursor
	// is on a machine and `ctrl+t` has no model to dial.
	pickerKeysModel = "→ providers · " + sortKeyWord + " · enter switch · " + effortKeyWord + " · esc"
	// pickerKeysModelTab is the same row with the caret somewhere inside what is
	// typed, where `→` steps over a character instead ([picker.foldKey]) and
	// only `tab` opens.
	pickerKeysModelTab = "tab providers · " + sortKeyWord + " · enter switch · " + effortKeyWord + " · esc"
	// pickerKeysFold is a row inside an open fold: enter chooses that provider,
	// `←` walks back out to the model.
	pickerKeysFold = "← back · " + sortKeyWord + " · enter choose · esc"
	// pickerKeysFoldTab is the same with characters before the caret, where
	// `←` edits the box and `tab` is the way out.
	pickerKeysFoldTab = "tab back · " + sortKeyWord + " · enter choose · esc"
	// pickerKeysUnpin is the row inside the fold that the requests are ALREADY
	// going to: the same enter takes the pin off there (lanes.go's
	// [app.applyLaneChoice]), and the hint is the only place that gesture
	// announces itself.
	pickerKeysUnpin = "← back · " + sortKeyWord + " · enter unpin · esc"
	// pickerKeysUnpinTab is that row with characters before the caret.
	pickerKeysUnpinTab = "tab back · " + sortKeyWord + " · enter unpin · esc"
	// pickerKeysSwitch is a list with no fold at all and no cursor on anything:
	// the two keys every list has. pickerKeysSwitchEffort is that list where the
	// rung can still be dialled, which is every model list a door holds a level
	// for even when no machine stands behind the row.
	pickerKeysSwitch       = "enter switch · esc"
	pickerKeysSwitchEffort = sortKeyWord + " · enter switch · " + effortKeyWord + " · esc"
)

// keysHint is what the hint slot says this list's keys do RIGHT NOW, read off
// the row the cursor is on and the caret in the box — the two things
// [picker.foldKey] reads before it decides what `→`, `←` and `tab` mean.
//
// IT IS CURSOR-AWARE BECAUSE THE FOLD WAS INVISIBLE WITHOUT IT. The slot used to
// read `enter switch · esc` wherever the cursor stood, and the only mention of
// `→ lanes` was the filter box's placeholder — which vanishes on the first
// typed character, exactly when a person has found their model and wants its
// machines. The owner opened /model, pressed `←` and `→`, and asked how anybody
// changes the provider of a model (2026-09-10). The placeholder has since given
// the keys up altogether ([pickerHint]) and this line carries them.
func (p *picker) keysHint() string {
	before, enter, after := p.keysParts()
	return dotted(before, "enter "+enter, after, "esc")
}

// keysParts is that same reading in its three pieces: whatever is said before
// enter, the one word that says what enter DOES on the row the cursor is on,
// and whatever is said after it.
//
// ── THE ORDER OF THE WHOLE LINE ──────────────────────────────────────────────
//
//	↑↓ pick · ← back · → providers · alt+s sort · enter <verb> · ctrl+t effort · esc
//
// The walk and the way out are the DOOR's (home adds `↑↓ pick` and ends `esc
// back`); everything between is this list's and comes back from here.
//
// IT IS GROUPED BY WHAT THE KEY MOVES, not by how often it is pressed. The first
// two move the CURSOR — `← back` walks out of a fold exactly as `↑↓` walks the
// rows, so it belongs beside it rather than stranded after enter, which is where
// it used to sit. The next two change the LIST: `→` opens a row, `alt+s` reorders
// it. Then `enter`, which is the one key that DECIDES something, so it stands
// where the eye stops. `ctrl+t` comes after it because it is the one key here
// that is not about the list at all — it dials a setting on the row and leaves
// the list exactly as it was.
//
// IT IS SPLIT BECAUSE THE DOORS END THE SENTENCE DIFFERENTLY. /model switches a
// conversation and says `enter switch · esc`; home pins the NEXT one and says
// `enter use it · esc back` around the very same keys ([app.targetPickFoot]).
// Splicing home's ending onto the finished sentence meant cutting the other
// ending back off it, which worked on the row it was written for and left
// `enter choose · ← back · esc · enter use it · esc back` on every row inside a
// fold.
func (p *picker) keysParts() (string, string, string) {
	row, ok := pickRow{}, p.cursor >= 0 && p.cursor < len(p.list)
	if ok {
		row = p.list[p.cursor]
	}
	// AND A MACHINE THAT IS ALREADY THE ANSWER SAYS WHAT ENTER DOES THERE, which
	// is the one gesture in this fold that is not the same as its neighbours'.
	unpin := ok && row.lane >= 0 && p.marked(p.cursor)
	// AND THE FOOT NAMES WHICHEVER KEY ACTUALLY WORKS RIGHT NOW. While the box is
	// being edited the arrows are the caret's and only `tab` reaches the tree, so
	// the foot says `tab`; once the box has gone quiet ([picker.editing]) the
	// arrows are the tree's again and it says so. A foot that named `←` while `←`
	// was stepping through a word is the thing that made the fold feel broken.
	back := "← back"
	if p.editing() && p.filter.cursor > 0 {
		back = "tab back"
	}
	open := "→ providers"
	if p.editing() && p.filter.cursor < len(p.filter.value) {
		open = "tab providers"
	}
	// AND THE SORT IS NAMED WHEREVER THE TABLE IS DRAWN, because it is the LIST's
	// key rather than the cursor's — the same reason the refresh key is in the
	// placeholder and not here. It names the KEY and not the column, which is
	// taskstable.go's ruling and its measurement: `alt+s sort: out/M` cost the
	// five cells that made the foot drop this clause and the one beside it at a
	// hundred columns, and which column the list is on is already drawn, on the
	// heading, wearing the arrow.
	sorts := sortKeyWord
	switch {
	case !ok:
		return "", "switch", ""
	case p.laneSlot == "":
		// A LIST WITH NO FOLD STILL DIALS THE RUNG, because the level is the
		// door's and not the router's: a task's model list holds one, and the
		// foot is the only place the key is named.
		return sorts, "switch", effortKeyWord
	case unpin:
		return dotted(back, sorts), "unpin", ""
	case row.lane == laneRoutAt && !p.machines:
		// THE SECOND FOLD SAYS SO ON ITS OWN ROW. `openrouter` opens the
		// machines it routes to, and the key that opens them is the key that
		// opened this fold — said again, because a row that can be opened and
		// does not say so is a row nobody opens.
		//
		// IT SAYS SO WITH NOTHING MEASURED TOO. This asked for at least one
		// believed machine, which was true when an unmeasured fold refused to
		// open — and stopped being true the moment `default` became a row inside
		// it ([picker.unfoldHere]), leaving `→` working on a row that did not
		// name it. A key that does something is named where it does it.
		return dotted(back, open, sorts), "choose", ""
	case row.lane != laneNone:
		// A MACHINE'S ROW NAMES THE SORT TOO, because the sort key orders the
		// PROVIDERS from in here (pickersort.go's [picker.sortNext]) and a table a
		// person is reading down is exactly where they want to reorder it.
		return dotted(back, sorts), "choose", ""
	}
	return dotted(open, sorts), "switch", effortKeyWord
}

// ── the app's side of the overlay ───────────────────────────────────────────

// openPicker is /model with no argument. It names the chat law out loud rather
// than leaning on the list having been filtered already: /model is a slot like
// any other, and every slot says which models may answer it (settings.go's
// [filterFor]).
func (a *app) openPicker() {
	a.noticeEvent(eventModelListOpened)
	a.pick.startFor(a.modelList(), a.model, chatModel)
	// THE PIN IS A SNAPSHOT, exactly as the model in use is: it is what marks a
	// row inside an open fold, and what the row in use says `via`, and neither
	// of those can change while a modal overlay owns the keyboard.
	a.armLanes(&a.pick, laneSlotFor(a.model))
	a.armRefresh()
	a.touch()
}

// openPickerFiltered is /model with words after it that are a QUESTION rather
// than a name — `/model deepseek <1s`. It is the same list, opened with the
// query already typed, so the next keystroke narrows it further instead of
// starting again.
func (a *app) openPickerFiltered(query string) {
	a.openPicker()
	a.pick.filter.setText(query)
	a.pick.rank()
	a.touch()
}

// openTaskPicker is the same list, pointed at ONE RUNNING NODE: the model word
// in a room's status line pressed, which is the only door onto it (app.go's
// [app.statusPress]).
//
// IT OFFERS THE SAME ROWS AS THE CONVERSATION'S, filtered by the same chat law,
// and that is what keeps the engine's ambiguity out of this gesture entirely: a
// row is one concrete catalog id, so the word handed over resolves to exactly one
// model and the shortlist a typed word can raise (internal/session's
// taskmodel.go) has nothing to raise here.
//
// THE MARK OPENS ON THE NODE'S OWN MODEL, not the session's, for [picker.start]'s
// stated reason: the cursor sits on what you are on, so enter confirms rather
// than changes. In here what you are on is what the task is running.
func (a *app) openTaskPicker(id uint64) {
	current := ""
	if node := a.tasks[id]; node != nil {
		current = firstNonEmpty(node.nextModel, node.model)
	}
	a.pick.startFor(a.modelList(), current, chatModel)
	a.pick.task = id
	a.armRefresh()
	a.touch()
}

// modelList is the source order stated in models.go, applied once here: the
// door's list (the catalog, when it can answer without a fetch), then the disk
// cache, then the built-ins. Each rung is tried only if the one above it came
// back empty, and none of them can block.
//
// EVERY RUNG IS FILTERED THE SAME WAY ([chatModels]): a row on offer here is a
// model you can talk to. The filter sits at the join rather than on any one
// source because all three of them have carried a drawing model at some point —
// the door's catalog publishes them, the cache is a file the door wrote before
// this rule existed — and a rule enforced at two of three places is a rule with
// a way round it.
func (a *app) modelList() []Model { return a.modelsFor(chatModel) }

// modelsFor is that same source order, asked ONE SLOT'S question instead of the
// chat law's ([modelFilter], models.go).
//
// The filter is applied INSIDE the ladder rather than to whatever it returned,
// and that is the whole reason this exists as a function. A slot filtering
// [app.modelList]'s answer is filtering a list from which its own rows have
// already been removed — which is what the media slots were doing, and why the
// drawing row offered a picker that could not contain a drawing model. It also
// keeps the rung rule honest for every slot: a catalog that carries no speech
// model at all falls through to the cache, exactly as a catalog with no chat
// model falls through for /model.
func (a *app) modelsFor(keep modelFilter) []Model {
	services := a.sources.All()
	if len(services) < 2 {
		return a.modelsForDefault(keep)
	}
	grouped := make([]Model, 0)
	for order, service := range services {
		var models []Model
		if order == 0 {
			models = a.modelsForDefault(keep)
		} else {
			models = keepModels(a.modelsForConnectedService(service), keep)
		}
		group := strings.ToLower(strings.TrimSpace(service.Source.Written))
		if group == "" {
			group = strings.ToLower(strings.TrimSpace(service.Source.Name))
		}
		if len(models) == 0 {
			grouped = append(grouped, Model{
				Notice: noServiceModelListWord, Group: group, GroupOrder: order, Unavailable: true,
			})
			continue
		}
		for _, model := range models {
			if order > 0 {
				model.ID = service.Qualify(model.ID)
				model.Direct = true
			}
			model.Group, model.GroupOrder = group, order
			grouped = append(grouped, model)
		}
	}
	return grouped
}

// modelsForDefault is the exact pre-service ladder. Keeping it whole makes the
// one-service path and each slot's fallback byte-for-byte what they were.
func (a *app) modelsForDefault(keep modelFilter) []Model {
	if a.models != nil {
		if list := keepModels(a.models(), keep); len(list) > 0 {
			return list
		}
	}
	if list := keepModels(a.cachedModels(), keep); len(list) > 0 {
		return list
	}
	return keepModels(BuiltinModels(), keep)
}

// nonChatWarning is what `/model <slug>` says instead of switching, and it is
// empty for every slug that may be taken.
//
// THE OFFLINE LAW DECIDES WHO IS CHECKED. A slug no list this surface can reach
// carries is taken AS TYPED, exactly as it always was: the catalog may be cold,
// a person may be naming a model this build has never listed, and a surface that
// refused every unfamiliar name would be a surface that stops working the moment
// the network does. The check is only for a slug the catalog DOES carry, where
// "this one cannot hold a conversation" is a published fact and not a guess.
//
// One sentence, and it says what happens rather than what went wrong: the model
// is unchanged, which is the thing the person needs to know before they type
// their next message.
func (a *app) nonChatWarning(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	for _, model := range a.modelsFor(nil) {
		if !strings.EqualFold(model.ID, id) {
			continue
		}
		if chatModel(model) {
			return ""
		}
		line := model.ID + " cannot hold a conversation"
		if why := cannotChatBecause(model); why != "" {
			line += " — " + why
		}
		return line + ". Still on " + a.model + "."
	}
	return ""
}

// cannotChatBecause is the half-sentence that says why a slug the catalog
// carries is not something you can talk to: `it answers with speech`,
// `it reads audio, not text`.
//
// IT IS A SENTENCE AND SO IT NEEDS A VERB, which is the one place on this
// surface where the modality nouns cannot stand alone. A column says the side
// in its head and the cell carries `speech`; a sentence in the middle of a
// conversation has no head over it, so the verb is written out. Both are built
// from the same two readings ([modalityInputs] and [modalityOutputs]), so there
// is no second vocabulary to keep in step — only a second grammar.
//
// WHICH SIDE IT NAMES IS WHICHEVER SIDE IS THE REASON. A model that answers in
// speech is refused for what it gives back; a transcriber is refused for what it
// takes in, and its output is text like anything else's, so naming that would
// explain nothing. A row that published neither says nothing at all and the
// refusal is the bare sentence, because a reason nobody published is not a
// reason this surface may invent.
func cannotChatBecause(model Model) string {
	if makes := modalityOutputs(model.Output); makes != "" {
		return "it answers with " + makes
	}
	if reads := modalityInputs(model.Input); reads != "" && !readsText(model) {
		return "it reads " + reads + ", not text"
	}
	return ""
}

// windowFor is the context length this surface knows for a model id, or zero.
// It is how /model <slug> — which carries no row with it — still tells the
// session what window it just switched to.
func (a *app) windowFor(id string) int {
	id = strings.TrimSpace(id)
	for _, model := range a.modelList() {
		if strings.EqualFold(model.ID, id) {
			return model.ContextLength
		}
	}
	return 0
}

// switchModel is the ONE road a model change takes, from the picker, from
// /model <slug> and from the settings sheet's talk row alike (settings.go's
// registry wires that row straight to here): swap it, learn its window, write
// it down, say so.
//
// window is the figure the caller already has (the picker's row); zero asks the
// list. Telling the session about the window is not decoration — compaction
// fires at a fraction of it, so a session that switched to a 1M model without
// saying so would keep compacting as if it were on the 128k one it started on.
func (a *app) switchModel(id string, window int) {
	a.agent.SetModel(id)
	a.model = a.agent.Model()
	if a.model == "" {
		a.model = id
	}
	// The new model's dial is learned HERE rather than left to the frame clock,
	// so the status row names it on the very frame the switch lands on
	// (reasoninglevel.go states the three moments that are seeded and why).
	a.learnLevel(a.model)
	if window <= 0 {
		window = a.windowFor(a.model)
	}
	if window > 0 {
		a.agent.SetContextWindow(window)
		// The surface keeps the figure it just handed over: session has no
		// getter for it, and the status line's meter is a percentage of exactly
		// this number (see [app.ctxPercent]).
		a.ctxWindow = window
	}
	a.rememberModel(a.model)
	a.refreshCreditWarnings()
	if a.chatCreditWarning != "" {
		a.askCredits(credits.PaidSwitch)
	}
	if a.creditSwitching {
		return
	}
	// THE ID IS THE WHOLE OF THIS LINE (payload.go). `model ·` is a label a person
	// already knows they asked for; the id is the one thing here they cannot see
	// anywhere else at this moment, so it steps to ink and the label stays dim.
	//
	// AND WHEN WORK IS RUNNING, THE LINE SAYS WHAT THE SWITCH DID NOT TOUCH. A
	// task's model is frozen when it is admitted (task_run.go's runModel reads
	// the spec, never the live dial), so a person who switches mid-task and
	// then watches the task keep answering in the old voice has been told
	// nothing unless it is said here, at the moment they acted. New tasks
	// started after this line follow the switch (taskmodel.go's
	// defaultTaskModel reads the live dial), which is why the sentence is
	// about the running ones only.
	note := "model · " + a.model
	if a.deckRunning() > 0 {
		note += " — tasks already running keep the model they started on"
	}
	a.noteFacts(note, a.model)
	a.noticeEvent(eventModelSwitched)
}

// rememberModel writes the choice down, so the NEXT launch opens on the model
// this one ended on (the door's seam is [Options.SaveModel], and the v3 door
// reads it back in v3TalkModel).
//
// It is here rather than at the picker because this is the one road every model
// change takes; a second write site would be the drift where /model persisted
// and the sheet's row did not.
//
// NIL IS A SURFACE THAT CANNOT REMEMBER, exactly as it is for the consent
// card's "always" — and that is the whole of the --host rule, kept in wiring
// rather than in a condition here: the far machine's engine reads the far
// machine's profile, so the hosted door hands over no seam and nothing about a
// remote model choice lands on this laptop.
//
// The write's error is dropped, and that is honest rather than lazy: nothing on
// screen claims the choice was saved. The note says "model · <id>", which is
// true of the running session whatever the disk did.
func (a *app) rememberModel(id string) {
	if a.creditSwitching || a.saveModel == nil || strings.TrimSpace(id) == "" {
		return
	}
	_ = a.saveModel(id)
}

// pickerKey routes one keypress while the overlay owns the keyboard. The input
// box is suspended for the duration — its draft is untouched and comes back
// whole on esc or enter.
//
// esc means the overlay here and not the turn: a modal that cannot be dismissed
// by the dismiss key is a trap. A turn is still interruptible the moment the
// picker closes.
//
// It returns a command for the one key that has to leave the loop — the
// refresh — and nil for every other.
func (a *app) pickerKey(msg tea.KeyPressMsg) tea.Cmd {
	var cmd tea.Cmd
	switch msg.String() {
	case "esc":
		a.pick.close()

	// ── ENTER CHOOSES AND THE LIST STAYS OPEN ───────────────────────────────
	//
	// It used to close on the press, which made every choice final and every
	// comparison a round trip: pick a model, watch the list vanish, type
	// `/model` again to see what the other one cost. The list is a TABLE now —
	// a thing built to be read down and compared — and a table that shuts the
	// moment you touch a row is a table you can use once.
	//
	// So enter applies and leaves it up, and `esc` is the way out. Applying is
	// safe to repeat: switching a model twice lands on the second, and pinning
	// a provider twice writes the second row.
	case "enter":
		chosen, ok := a.pick.choice()
		task := a.pick.task
		row, onLane := a.pick.laneUnder()
		if ok && onLane && task == 0 {
			// ENTER ON A LANE IS TWO ANSWERS AT ONCE WHEN THE MODEL IS NOT THE
			// ONE IN USE: somebody who opened another model's lanes and chose
			// one of them asked for that model on that machine, and pinning a
			// lane under a model they are not talking to would be a setting
			// that took effect the next time they happened to switch.
			if chosen.ID != a.model {
				a.switchModel(chosen.ID, chosen.ContextLength)
			}
			a.applyLaneChoice(chosen.ID, row, a.pick.lanes)
			a.restatePicker(&a.pick, a.model)
			a.pick.showChoice(row)
			a.touch()
			return nil
		}
		if ok {
			// One list, two subjects, decided where the list was opened: a node when
			// the model word in its room was pressed, and the conversation every
			// other time (palette.go's [app.openTaskPicker]).
			if task != 0 {
				a.retargetTask(task, chosen.ID)
			} else {
				a.switchModel(chosen.ID, chosen.ContextLength)
			}
			a.restatePicker(&a.pick, a.model)
		}

	// The reasoning cycle sits above the filter's default branch on purpose: it
	// is the one key here that is not about the list, and it changes the row
	// rather than the query.
	case "ctrl+t":
		a.cycleReasoning()

	// Today's list, asked of the door (modelrefresh.go). Where the door has no
	// refresh, or one is already out, the key does nothing at all.
	case refreshModelsKey:
		cmd = a.fetchModels()

	// THE FOLD IS ITS OWN KEY MAP and it is the list's, not this door's
	// ([picker.foldKey]) — the settings panel's model row reads the very same
	// three keys, and a fold that opened from one door and not the other would
	// be two pickers again.
	default:
		if !a.pick.foldKey(msg.String()) {
			a.pick.navigate(msg)
		}
	}
	a.touch()
	return cmd
}

// navigate is EVERY KEY THE PICKER OWNS that is not a decision: the walk, the
// scroll and the filter box. enter, esc and ctrl+t are left to whoever opened
// the list, because what they mean is the caller's business — /model switches a
// session with them, the settings panel writes a registry row (settings.go).
//
// It exists so that the two entry points cannot drift into two pickers. There
// is one filterable model list on this surface; a slot row in the settings
// panel and /model are two doors onto it, not two lists that look alike.
func (p *picker) navigate(msg tea.KeyPressMsg) {
	// WHAT COUNTS AS EDITING IS THE BOX HAVING MOVED, asked of the box itself
	// rather than of the key's name. A list of editing keys kept here would be a
	// second copy of [listNavigate]'s own switch — and the day somebody adds a
	// kill to that switch, the copy stops agreeing and the mode starts lying
	// about a keystroke that plainly edited. The text and the caret are the whole
	// state a person can see, so a press that changed neither did not edit.
	//
	// AND THE WALK DELIBERATELY DOES NOT COUNT. `↑`/`↓` move the cursor through
	// the list and leave the box alone, so walking a filtered list never hands
	// the arrows back to the caret.
	before, at := string(p.filter.value), p.filter.cursor
	listNavigate(msg, &p.filter, p.move, p.rank, pickerRows)
	if string(p.filter.value) != before || p.filter.cursor != at {
		p.typed = timeNow()
	}
}

// listNavigate is that key map itself, held apart from the model list so the
// session picker can have exactly it (resume.go) rather than a second copy of
// it that answers ctrl+w and forgets pgdn. What a filterable overlay LISTS is
// its own; how a person walks and types into one is this surface's, once.
//
// move walks the rows and rank re-filters after an edit — a caller passes its
// own two, because the scoring is about what is being listed. page is how far
// pgup and pgdn jump, which is that list's own window.
func listNavigate(msg tea.KeyPressMsg, filter *editor, move func(int), rank func(), page int) {
	// A LIST WITH NO BOX UNDER IT STILL WALKS, and takes no text: the spend
	// place has nothing to type into (pages.go's [place.box]).
	if filter == nil {
		switch msg.String() {
		case "up", "ctrl+p":
			move(-1)
		case "down", "ctrl+n":
			move(1)
		case "pgup":
			move(-page)
		case "pgdown":
			move(page)
		}
		return
	}
	// THE WORD AND LINE JUMPS ARE THE SURFACE'S, NOT THIS LIST'S (editkeys.go).
	// They are read before the switch because they belong to every box on the
	// program and this one is only the busiest door onto them — twelve overlays
	// share this key map, and a jump added here has to be the same jump the
	// message box makes or a person learns two of them.
	if editorMotion(filter, msg.String()) {
		return
	}
	// AND ctrl+z TAKES BACK WHAT WAS TYPED, in every box on this surface and not
	// only in the message one (editundo.go).
	if editorUndo(filter, msg.String()) {
		rank()
		return
	}
	switch msg.String() {
	case "up", "ctrl+p":
		move(-1)
	case "down", "ctrl+n":
		move(1)
	case "pgup":
		move(-page)
	case "pgdown":
		move(page)

	case "backspace":
		filter.deleteBackward()
		rank()
	case "delete":
		filter.deleteForward()
		rank()
	// THE LINE AND WORD KILLS ANSWER TO EVERY NAME THEY SEND UNDER, exactly as
	// they do in the message box (input.go). A gesture that clears the filter in
	// the composer and does nothing in the model picker is a gesture a person
	// stops trusting anywhere — and `super+backspace` is what a hand on a Mac
	// keyboard does without being told, so it means kill-to-line-start here for
	// the same reason it means it there. It reaches this switch only on a terminal
	// that reports the super modifier at all, which costs nothing where none does.
	case "ctrl+u", "super+backspace":
		filter.killToStart()
		rank()
	case "ctrl+k":
		// AND THE OTHER HALF OF THE PAIR, which reached this box the day the
		// switcher gave the letter back (hop.go's [hopOpenKey]). It is here for
		// the reason directly above: a kill that works in the composer and does
		// nothing in the model picker is a kill a person stops trusting anywhere.
		filter.killToEnd()
		rank()
	case "ctrl+w", "alt+backspace", "ctrl+backspace":
		filter.deleteWord()
		rank()
	case "left", "ctrl+b":
		filter.left()
	case "right", "ctrl+f":
		filter.right()
	case "home":
		// `ctrl+a` is the same jump and is read above, with the rest of the
		// surface's line vocabulary (editkeys.go).
		filter.home()
	case "end", "ctrl+e":
		filter.end()

	default:
		if text := msg.Key().Text; text != "" {
			filter.insert(text)
			rank()
		}
	}
}

// overlayHeight is how many rows the frame gives whichever list is open. It is
// that list's own want, clamped so the status line and the box always survive:
// an overlay that could take the whole frame is an overlay that can hide where
// you are and what you typed.
//
// Only one list is ever open — [app.closeLists] and the sync in [app.edited]
// see to that — so this is a switch and not a sum.
func (a *app) overlayHeight() int {
	width, height := a.size()
	// WHICH LIST IS OPEN IS ASKED BEFORE THE ROOM IS MEASURED, and the order is a
	// performance law and not a preference. The measurement below is seven calls
	// deep and [app.inputHeight] alone composes the whole draft block to find out
	// how tall it is — twenty-five allocations of work that a frame with NO list
	// open has no use for, which is nearly every frame there is. Measuring it
	// above this switch put that cost on every scroll notch and broke the
	// one-screen scroll ceiling in PERF.md by twenty percent.
	//
	// So the command list, which is the only list that wants to be told the size
	// ([menu.height] shows as many commands as the frame can hold and says how
	// many are left over), is answered AFTER the room is known rather than inside
	// the switch. Every other list still answers with its own figure and meets the
	// same clamp at the foot of this function — the number is one number either
	// way, and it is still written once.
	var want int
	commands := false
	switch {
	case a.pick.open:
		want = a.pick.height(width)
	case a.crewPick.open:
		want = a.crewPick.height()
	case a.effPick.open:
		want = a.effPick.height()
	case a.roster.open:
		want = a.roster.height(width)
	case a.shelf.open:
		want = a.shelf.height(width)
	case a.connPanel.open:
		want = a.connPanel.height(width)
	case a.harnPanel.open:
		want = a.harnPanel.height(width)
	case a.harnPick.open:
		want = a.harnPick.height(width)
	case a.skillPick.open:
		want = a.skillPick.height(width)
	case a.permPanel.open:
		want = a.permPanel.height(width)
	case a.subPage.open:
		want = a.subPage.height(width)
	case a.menu.open:
		commands = true
	case a.comp.open:
		want = a.comp.height(width)
	default:
		return 0
	}
	// The approval question, the follow-up count and a waiting message are spoken
	// for before the list is: all of them sit between the conversation and the
	// box, and a list that claimed their rows would push the status line off the
	// frame. The two reserved rows are the status line and one row of
	// conversation — a list that left neither would be a list that took the
	// screen.
	room := height - 2 - a.inputHeight() - a.questionHeight() -
		a.followHeight() - a.landHeight() - a.parkedHeight()
	if commands {
		room = height - a.topHeight() - a.chromeBaseHeight() - 1
		want = a.menu.height(width, room, a.chords)
	}
	if want > room {
		want = room
	}
	if want < 0 {
		return 0
	}
	return want
}

// overlayRows is the tail of the frame: the open list, drawn in exactly the
// rows [app.overlayHeight] handed out.
func (a *app) overlayRows(width, n int) []string {
	// The pointer's row within whichever list is open, or -1. One number for all
	// three, because there is only ever one list (hover.go).
	hover := -1
	if a.hot.kind == hoverOverlay {
		hover = a.hot.index
	}
	switch {
	case a.pick.open:
		return a.pick.rows(width, n, a.pal, hover, a.reasoningFor)
	case a.crewPick.open:
		return a.crewPick.rows(width, n, a.pal, hover, a)
	case a.effPick.open:
		return a.effPick.rows(width, n, a.pal, hover)
	case a.roster.open:
		return a.roster.rows(width, n, a.pal, hover)
	case a.shelf.open:
		return a.shelf.rows(width, n, a.pal, hover)
	case a.connPanel.open:
		return a.connPanel.draw(width, n, a.pal, hover)
	case a.harnPanel.open:
		return a.harnPanel.draw(width, n, a.pal, hover)
	case a.harnPick.open:
		return a.harnPick.draw(width, n, a.pal, hover)
	case a.skillPick.open:
		return a.skillPick.draw(width, n, a.pal, hover)
	case a.permPanel.open:
		return a.permPanel.draw(width, n, a.pal, hover)
	case a.subPage.open:
		return a.subPage.draw(a, width, n, hover)
	case a.menu.open:
		return a.menu.rows(width, n, a.pal, hover, a.chords)
	case a.comp.open:
		head := ""
		if a.hot.kind == hoverOverlay {
			head = a.hot.key
		}
		return a.comp.rows(width, n, a.pal, hover, head)
	}
	return nil
}
