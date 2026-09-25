package tui3

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── THE WALL AND ITS TEAMS: THE SHAPES THE THREE HALVES AGREE ON ───────────
//
// The wall is a full-frame grid of every conversation this window has a tab
// for, each drawn as a tile holding the live tail of its transcript. A team is
// a named set of those conversations that the tab strip can be narrowed to.
//
// The wall is built in three halves that meet only through the types in this
// file: the reading (walltail.go) turns what this process holds into tiles,
// the painting (wallview.go) turns tiles into rows, and the teams
// (teams.go, teamhue.go) keep the named sets and their colours on disk. The
// wiring (wall.go, wallpop.go) is the only part that touches the app's keys,
// frame and pointer; the strip draws the wall's toggle and the shown team's
// chip itself (chattabs.go).
//
// THE FRAME LAW HOLDS HERE AS EVERYWHERE (framedisk_law_test.go): nothing a
// frame calls opens a file, crosses a wire or takes an agent's mutex. Tails
// are read on a stir or an opening and cached; the painter draws the cache.

// wallLineKind is what one tail line is, which is all that decides its ink.
type wallLineKind uint8

const (
	wallProse wallLineKind = iota // the assistant's words
	wallTool                      // one tool call, folded to one line: `▸ bash go test ./...`
	wallUser                      // what the person said
	wallNote                      // a session aside or a compaction note
)

// wallLine is one logical line of a tile's tail, NOT yet fit to any width.
// The painter wraps and cuts; the reader never knows how wide a tile is.
type wallLine struct {
	kind wallLineKind
	text string
}

// wallSparkLen is how many activity samples a tile keeps, one per second.
const wallSparkLen = 24

// wallTile is one conversation as the wall draws it.
type wallTile struct {
	// tab is the strip's own record of this conversation. The wall's doors are
	// the strip's doors: enter is [app.tabGo] and x is [app.tabDismiss].
	tab  chatTab
	name string
	here bool
	// signal is [app.tabSignalFor]'s reading, never a second one.
	signal tabSignal
	// live is false for a tile whose tail is a snapshot this window cannot
	// refresh: over a shared engine handle only the front conversation is
	// live. A frozen tile draws no spinner and no sparkline, and says `seen`.
	live bool
	// seen is when the tail was last read.
	seen time.Time
	// age is time since the conversation last moved, spelled short ("2m");
	// "" when unknown.
	age string
	// lines is the tail, oldest first, at most wallTailCap logical lines.
	lines []wallLine
	// fresh is how many of the LAST lines arrived since the previous reading;
	// the painter lifts them to ink and lets them settle.
	fresh int
	// freshAt is when those lines arrived, for the settle.
	freshAt time.Time
	// spark is activity per second, oldest first, each 0..7, len <= wallSparkLen.
	// All zeros (or empty) draws no sparkline: the emptiness law.
	spark []uint8
	// question is the one-line ask when signal is tabNeedsPerson, "" otherwise.
	question string
	// marked is picked for a new team.
	marked bool
	// manager says this is the manager of the team shown, pinned first and
	// marked before its name (teammanager.go).
	manager bool
	// rows is the tail drawn the way the conversation itself draws it
	// (wallmini.go), already painted and fit to the body's width. When it is
	// set the painter draws it in place of lines.
	rows []string
	// doing is what a working conversation is doing now, `running bash`,
	// `writing`, `waiting on you`; "" at rest (wallmini.go's [wallDoing]).
	doing string
	// moved is when the conversation last grew; zero when this window has not
	// seen it move.
	moved time.Time
	// teams is the id of every team this conversation is in. A conversation
	// may be in several.
	teams []string
}

// wallTailCap is the most logical lines a reading keeps per conversation.
const wallTailCap = 40

// wallView is everything the painter needs, and nothing it may go and get.
type wallView struct {
	// team is the active team's id, "" for All, and teams is every team in
	// the order the Teams row draws them.
	team  string
	teams []wallTeamRow
	tiles []wallTile
	focus int
	// scroll is the first tile ROW on screen.
	scroll int
	// cols forces the column count; 0 lets the frame decide.
	cols int
	// filter is what the person has typed after `/`; filtering says the box is up.
	filter    string
	filtering bool
	// naming is the new-team prompt; name is what is typed in it.
	naming bool
	name   string
	// spin is the frame's pulse step, for the working mark.
	spin int
	now  time.Time
	// reduced turns every settle and every motion off.
	reduced bool
	// hover is the target the pointer rests on, as the last frame's hits named
	// it; the zero ref is no hover. It lights a button and reveals a tile's own
	// controls, and it never moves a cell.
	hover wallHitRef
	// total is how many conversations are open in all.
	total int
	// away is how many of the shown team's members this window does not have
	// open; the title offers to resume them while it is not zero.
	away int
	// choices is the colours the new-team card offers and choice the one
	// taken.
	choices []teamHueSpec
	choice  int
	// pop is the popover that is up, if one is.
	pop wallPop
	// nameFresh says the name in the new-team card is one the wall filled in,
	// drawn selected so the first key typed replaces it; asking says a better
	// one is being asked for, and the card says `naming…`.
	nameFresh bool
	asking    bool
	// nameIn is the team the new team will be made inside, by name, "" for
	// the top level (`+ New team in harbor` on the teams page).
	nameIn string
	// made is the team just made and how many it holds, said on the chip row
	// until madeAt is wallMadeFor old.
	made   string
	madeN  int
	madeAt time.Time
	// pointerOn says pointerY holds the row the pointer is on, in the painter's
	// own rows (the head's rows taken off). Some targets share a ref on two
	// rows, a waiting tile's Answer and its row's Answer, or a picked tile's ☐
	// and its row's Select, and the row says which one to light; left unset,
	// both light.
	pointerOn bool
	pointerY  int
	// help says the help sheet is up, and helpTop is the first of its lines
	// shown when it is scrolled.
	help    bool
	helpTop int
	// doorHot says the pointer rests on the strip's own door to this view
	// (chattabs.go), which the toolbar explains like any other control.
	doorHot bool
	// org is Organize's button and card (teamorganize.go).
	org wallOrganize
	// popManager is the teams popover's manager row for its one conversation
	// in the team shown: 0 for none, and otherwise one of wallManagerMake and
	// wallManagerRemove (teammanager.go). mark is the manager's glyph.
	popManager int
	// popManagerWord is that row's words (teammanager.go's
	// [app.teamManagerMenuWord]).
	popManagerWord string
	mark           string
}

// The teams popover's manager row, as [wallView.popManager] says it.
const (
	wallManagerMake   = 1
	wallManagerRemove = 2
)

// wallTeamRow is one team as the painter draws it: its id, name and colour,
// how many of its members are open in this window, and how many members it
// has in all, open or not.
type wallTeamRow struct {
	id      string
	name    string
	hue     teamHueSpec
	count   int
	members int
}

// teamRow is where team id sits in v.teams, -1 when it is not there.
func (v wallView) teamRow(id string) int {
	for i, t := range v.teams {
		if id != "" && t.id == id {
			return i
		}
	}
	return -1
}

// wallHitKind is what a press on one hit does.
type wallHitKind uint8

const (
	wallHitNone     wallHitKind = iota
	wallHitTile                 // a tile's body: open it, or pick it in selection mode; arg is the tile
	wallHitSelect               // a tile's Select, or its ☐ in selection mode: pick it or put it back; arg is the tile
	wallHitTeams                // a tile's Teams: the teams it is in; arg is the tile
	wallHitOpen                 // a tile's Open, or its Answer; arg is the tile
	wallHitClose                // a tile's Close, which closes the view; arg is the tile
	wallHitChip                 // a Teams segment; id is the team, "" for All
	wallHitChipMenu             // a segment's dot or its ⋯: the team's settings; id is the team
	wallHitAddTeam              // the + New team segment
	wallHitAction               // a button; arg is a wallAct
	wallHitMini                 // one minimap cell; arg is the tile
	wallHitPopRow               // a popover row; id is a team (arg wallPopTeam), or arg is a wallPop row code
	wallHitSwatch               // a colour swatch; arg is its index among the choices
	wallHitHelp                 // a row of the help sheet; arg is its place in [wallHelpList]
	wallHitOrgRow               // a suggestion on the Organize card; arg is its place in the card
)

// The popover rows that are not a team.
const (
	wallPopTeam    = 0  // a team's row; the hit's id says which
	wallPopNew     = -1 // + New team…
	wallPopDelete  = -2 // Delete team
	wallPopConfirm = -3 // Delete, confirmed
	wallPopKeep    = -4 // Keep, the delete undone
	wallPopManager = -6 // Make manager, or Remove manager, in the team shown
)

// wallPopKind is which popover is up.
type wallPopKind uint8

const (
	wallPopNone     wallPopKind = iota
	wallPopMembers              // which teams the targets are in
	wallPopSettings             // one team's name, colour and deletion
)

// wallPop is a small card anchored to the control that opened it. It is the
// wiring's state and the painter's input at once: everything it draws is here
// or in the view.
type wallPop struct {
	kind wallPopKind
	// x, y0 and y1 are the anchor: the column it hangs from, and the rows of
	// the control, so it can open under it or, short of room, over it.
	x, y0, y1 int
	// targets is the tiles a members popover acts on, by key.
	targets []string
	// cursor is the row the keyboard is on.
	cursor int
	// team, name, choices and choice are the settings popover's: the team's
	// id, its name as being edited, the colours offered and the one it has.
	team    string
	name    string
	choices []teamHueSpec
	choice  int
	// confirm says the delete has been asked for and waits on its answer.
	confirm bool
}

// wallAct is what one button does. Every act that has a key does exactly
// what that key does.
type wallAct int

const (
	wallActBack        wallAct = iota // esc
	wallActOpen                       // enter
	wallActSelect                     // space
	wallActNewTeam                    // s
	wallActFilter                     // /
	wallActNext                       // n
	wallActColsLess                   // -
	wallActColsMore                   // + or =
	wallActClose                      // x
	wallActMakeTeam                   // s, from the tray
	wallActAddTo                      // the teams popover for every picked tile, from the tray
	wallActCloseViews                 // x on every marked tile
	wallActClear                      // unmark all
	wallActSave                       // enter, naming
	wallActCancel                     // esc, naming
	wallActFilterClear                // esc, filtering
	wallActShuffle                    // ctrl+r, naming
	wallActHelp                       // ?
	wallActOrganize                   // o: the Organize card, on All
	wallActOrgUndo                    // u: the last Organize undone, while it is offered
	wallActOrgApply                   // enter, organizing
	wallActOrgCancel                  // esc, organizing
	wallActResume                     // r: the shown team's members not open here, resumed behind
)

// wallHitRef names one target without its cells, which is what a hover keeps
// from one frame to the next.
//
// A TARGET ON A TEAM NAMES IT BY ID, in id, and never by its place: a hover
// or a popover that outlives a frame must still mean the same team after one
// is deleted or the row is redrawn in another order.
type wallHitRef struct {
	kind wallHitKind
	arg  int
	id   string
}

// wallHit is where one target landed, for the pointer.
type wallHit struct {
	x0, y0, x1, y1 int // inclusive-exclusive cell rectangle
	kind           wallHitKind
	arg            int    // the tile, the act or a popover row code, by kind
	id             string // the team, for a target on one; "" is All
}

func (h wallHit) ref() wallHitRef { return wallHitRef{kind: h.kind, arg: h.arg, id: h.id} }

// onTile reports whether the ref is one of tile i's own targets.
func (r wallHitRef) onTile(i int) bool {
	return r.kind >= wallHitTile && r.kind <= wallHitClose && r.arg == i
}

// team and teamMember are the store's own types (internal/teams), named
// here as the interface has always named them. The store owns the file, the
// ids, the tree, handles and the manager; the interface owns how a team looks.
type (
	team       = teamstore.Team
	teamMember = teamstore.Member
)

// wallState is the wall's whole footprint on the app: one field.
type wallState struct {
	// frontVer is the front conversation as the wall last read it for its tile
	// (walltail.go's [app.wallFrontMoved]).
	frontVer wallFrontVer
	on       bool
	focus    int
	scroll   int
	cols     int
	marked   map[string]bool // by chatTab.key
	filter   string
	filterOn bool
	naming   bool
	name     string
	openedAt time.Time
	hits     []wallHit
	// tails is the reading cache, by chatTab.key (walltail.go owns it).
	tails map[string]*wallTail
	// teams is the loaded set and activeID the id of the one the strip is
	// narrowed to, "" for none (teams.go owns both).
	teams    []team
	activeID string
	loaded   bool
	// ticking says a wallTickMsg is already on its way, so an opening never
	// starts a second clock beside the first.
	ticking bool
	// hover is what the pointer rests on, resolved against hits.
	hover wallHitRef
	// headRows is how many rows the last frame spent above the grid, so a
	// pointer can be told apart from the strip without laying the strip out.
	headRows int
	// pop is the popover that is up; choices and choice are the new-team
	// card's colours and the one taken.
	pop     wallPop
	choices []teamHueSpec
	choice  int
	// chip is where the strip drew its team chip, empty when it was not
	// drawn.
	chip hudSpan
	// nameFresh, made, madeN and madeAt are the view's fields of those names.
	// nameGen counts the suggestions asked for, so an answer to one the card
	// has moved past is dropped, and nameAsking says one is on its way.
	nameFresh  bool
	nameGen    int
	nameAsking bool
	// nameParent is the team a new team is made inside, by id: set by `+ New
	// team in harbor` and cleared by every other opening of the card.
	nameParent string
	made       string
	madeN      int
	madeAt     time.Time

	// The motion and the pointer's memory (wall.go). revealAt is when the
	// opening's row-by-row reveal began, zero once it is done; zoomAt and
	// zoomFrom are an opened tile growing into the frame.
	revealAt time.Time
	zoomAt   time.Time
	zoomFrom wallRect
	// wheelAt and wheelDir are the last wheel step taken, so a trackpad's
	// burst moves one row per settle rather than one per event.
	wheelAt  time.Time
	wheelDir int
	// ptrX, ptrY and ptrIn are where the pointer last was over the wall, so a
	// scroll can light what slid under it; rehover asks the next frame to.
	ptrX, ptrY int
	ptrIn      bool
	rehover    bool
	// stirred says something besides the hover changed since the last frame,
	// so a pointer resting on the same target may not reuse that frame.
	stirred bool
	// places is each team's focus and scroll while the wall is up, by team
	// id, "" for All.
	places map[string]wallPlace
	// card is where the popover or the new-team card was drawn, in frame
	// cells, empty when neither is up.
	card wallRect
	// spinning says the last frame drew a live working tile, whose spinner
	// needs the paint clock turning.
	spinning bool
	// help says the help sheet is up; helpTop is its scroll, and helpMax the
	// most it could scroll on the last frame, so a key clamps to what was
	// drawn.
	help    bool
	helpTop int
	helpMax int
	// door is where the strip drew its own door to this view, empty when it
	// was not drawn (chattabs.go).
	door hudSpan
	// org is Organize's card, its suggestions and its Undo (teamorganize.go).
	org wallOrganize
}

// wallTail is one conversation's cached reading (walltail.go fills it).
type wallTail struct {
	lines   []wallLine
	count   int // transcript entries seen at the last reading
	textLen int // total text length seen, so a growing last entry counts as activity
	seen    time.Time
	fresh   int
	freshAt time.Time
	// spark is a ring of per-second activity, newest at sparkAt.
	spark   [wallSparkLen]uint8
	sparkAt time.Time
	live    bool
	// recent is the last few entries as they were read, kept so a tile can be
	// drawn with the chat's own markdown and tool rows rather than flattened.
	recent []session.DisplayEntry
	// ver moves whenever the reading changed; the drawn rows are kept against
	// it and the width they were drawn at.
	ver     int
	rows    []string
	rowsW   int
	rowsVer int
}
