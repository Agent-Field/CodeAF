package tui3

// THE TWO ZONES: WHAT NEEDS YOU, AND WHAT IS MOVING.
//
// Home's left column is a DIRECTORY — every project, its conversations under
// it, the things keeping an eye on it. That shape answers "where is my work",
// and it answers it well. What it cannot answer without being read end to end
// is the two questions a person actually opens this screen with:
//
//	what needs me, and what is running everywhere?
//
// Both facts were already on the screen and both were SCATTERED through it. A
// consent card stopped in another terminal wore `▲` inside whichever project it
// belonged to; a watch that needs a look sat in that project's standing band; an
// errand holding a card sat wherever it was asked. Three rows, one claim on a
// person's attention, in three different parts of the page — and any of them
// could be under a folded project entirely, which is a question nobody is going
// to answer today.
//
// So the two questions get two strips above the list (docs/HOME-BRIDGE.md):
//
//	 needs you
//	 ▲ pricing research            hax-sdk · 2h
//	 ▲ every Monday, the update     aforge · 20m
//	 moving
//	 ● port the picker               wisp · 8m
//	 ▸ …2 more
//
// ── THE LAWS ────────────────────────────────────────────────────────────────
//
//   - A ROW MAY LIVE IN TWO ZONES, AND NEITHER IS A COPY. A conversation
//     stopped on a question is a `needs you` row and a row under its own project
//     at the same moment, because the two answer different questions. Both are
//     drawn from the ONE live object the reading holds — nothing is lifted out
//     of the list below and nothing has to be kept in step. This is where the
//     phone's inbox and this surface part company: at [tierPhone] a row appears
//     once because twelve rows of screen cannot afford to say a thing twice
//     (homephone.go's second law), and a wide frame can, because the eye takes
//     both in at once.
//
//   - NEEDS-YOU ORDER IS WAITED-LONGEST FIRST. It is the only order that cannot
//     be argued with: the thing that has been stopped longest has cost the most
//     already. THE DESTRUCTIVE PIN IS NOT BUILT — see [attentionWaitedSince].
//
//   - MOVING IS BUSIEST FIRST, FIVE ROWS AND A DOOR. The cap is the same fold
//     grammar every other list here folds with (`▸ …2 more`), because a machine
//     with eleven things in flight would otherwise spend the whole column
//     saying so, and the eleven are all in the list below anyway.
//
//   - STABLE GEOGRAPHY BEATS EMPTINESS, ON HOME ONLY. The one-word labels draw
//     at the wide tier even over nothing, because a map that redraws itself is
//     not a map — an empty `needs you` is the good news, said with space. Below
//     [homeMinDetail] the frame has no room for that argument and an empty zone
//     vanishes whole, which is what the emptiness law asks for everywhere else.
//
//   - EVERY FACT IS ONE HOME ALREADY READ. The world, the standing bands and
//     the errands are the same three readings the list below is built from, on
//     the same three-second beat ([homeEvery]). This file opens nothing, stats
//     nothing and asks the engine for nothing.
//
// AND THE ROWS ARE DOORS OF THE KINDS THEY ALREADY WERE. A zone row keeps the
// row kind of the thing it stands for — [homeSession], [homeItem],
// [homeExchangeRow] — so enter goes exactly where enter went for that kind
// before this file existed, the card beside it is that thing's card
// ([app.homeSubject]), and a digit over an answerable row answers it through
// the road the answer band already rides (homeband_answer.go). What this file
// adds is the arrangement and the paint.

import (
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// The row kinds the zones add, declared HERE and given values far above the
// iota block in home.go for [homePhoneSection]'s reason: that block is being
// edited by other lanes in the same wave, and a constant appended to it would
// be a conflict over a line that says nothing.
const (
	// homeAttentionZone is one strip's label. It is NOT a cursor stop — a label
	// names a zone rather than a thing, exactly as [homeHeading] does.
	homeAttentionZone homeRowKind = 220
	// homeAttentionMore is a strip's own fold line — `▸ …2 more` — and it is a
	// door exactly as [homeQuiet] is, with the same mark and the same gestures.
	// It carries the key of the strip it belongs to ([attentionFoldKey]), so
	// there is one kind however many strips there are.
	homeAttentionMore homeRowKind = 221
)

// The two labels, in the order they are drawn. Each is one dim lowercase
// word-group and each is quoted in the manual exactly as it is spelled here.
const (
	attentionNeedsWord  = "needs you"
	attentionMovingWord = "moving"
)

// attentionMovingShown is how many things in flight the moving zone draws
// before the rest fold. Five is what a strip can hold without becoming the
// screen — the zones are a summary standing over a list, and a summary as long
// as its list is not one.
//
// NEEDS-YOU HAS NO SUCH CAP, and that asymmetry is the point. Something moving
// is something you glance at; something stopped is something that does not go
// away until a person deals with it, and a zone that folded the eleventh of
// those away would be this screen hiding the one row it exists for.
const attentionMovingShown = 5

// homeZone is ONE STRIP, described whole: what it is called, what it gathers,
// how it orders what it found, the mark its rows wear and the hue that mark is
// said in, and how many rows it draws before the rest fold.
//
// THE ZONES ARE A TABLE AND NOT TWO SPECIAL CASES, and that is deliberate rather
// than tidy. Everything below this declaration — the build, the fold and its
// key, the cursor's memory of where it was standing, the paint — walks
// [homeZones] and cannot name `needs you` or `moving`: a third strip is a row in
// the table and no new branch anywhere, and the difference between two strips is
// exactly the six fields here and nothing hidden in an `if`.
type homeZone struct {
	// word is the label, dim and lowercase, and it is the zone's identity
	// everywhere else: the fold's key, the row's [homeAttention.word], the
	// manual's spelling.
	word string
	// gather is everything this strip stands for, in any order, and order is how
	// that becomes a reading order.
	gather func(h *homeView) []homeLine
	order  func(a, b *homeAttention) bool
	// mark is the cell every row of the zone wears, and ink is the hue it is
	// said in — the one coloured cell on the row ([app.attentionRow]).
	mark func(ascii bool) string
	ink  func(pal palette, s string) string
	// shown is how many rows it draws before the rest go behind one door. Zero is
	// no cap at all.
	shown int
}

// homeZones is the strips, in the order they are drawn. What needs a person
// stands over what is merely happening, for the reason the glyphs are ordered
// that way and the phone's sections are: something stopped is something you have
// to do, and something moving is something you check on.
var homeZones = []homeZone{{
	word:   attentionNeedsWord,
	gather: (*homeView).attentionNeeds,
	order:  func(a, b *homeAttention) bool { return attentionOlder(a.at, b.at) },
	mark:   func(ascii bool) string { return attentionPick(ascii, homeAskGlyph, homeAskASCII) },
	ink:    func(pal palette, s string) string { return pal.askBold(s) },
}, {
	word:   attentionMovingWord,
	gather: (*homeView).attentionMoving,
	// BUSIEST FIRST, AND OLDEST WITHIN A TIER. The count is what makes one row
	// worth more of a five-row strip than another; the age settles ties in the
	// only direction that is ever interesting, which is toward the thing that has
	// been going longest.
	order: func(a, b *homeAttention) bool {
		if a.busy != b.busy {
			return a.busy > b.busy
		}
		return attentionOlder(a.at, b.at)
	},
	mark:  func(ascii bool) string { return attentionPick(ascii, homeLiveGlyph, homeLiveASCII) },
	ink:   func(pal palette, s string) string { return pal.muted(s) },
	shown: attentionMovingShown,
}}

// attentionPick is the glyph tier, asked once rather than in every zone.
func attentionPick(ascii bool, glyph, plain string) string {
	if ascii {
		return plain
	}
	return glyph
}

// attentionZoneOf is one strip by its label, for the two readers that hold a row
// rather than the zone it came from: the paint, and the fold.
func attentionZoneOf(word string) (homeZone, bool) {
	for _, zone := range homeZones {
		if zone.word == word {
			return zone, true
		}
	}
	return homeZone{}, false
}

// attentionFoldKey is where one zone's fold is remembered, in the map every
// other fold on this column is remembered in ([homeView.expanded]). The null
// byte is [homeElsewhereKey]'s device: no project directory can collide with it,
// and the label after it is what keeps two strips' folds apart.
func attentionFoldKey(word string) string { return "\x00zone\x00" + word }

// homeAttention is what a zone row SAYS, resolved when the row is built.
//
// It is carried on the line rather than derived at the draw for
// [homePhoneNote]'s reason: a row and the order it was sorted into must never
// disagree about how long something has been waiting, and half of these facts —
// which task inside a conversation, when its node started — cannot be recovered
// from the line alone once it has been built.
type homeAttention struct {
	// word is which zone this row is in, spelled as that zone's own label.
	word string
	// name is what the thing is called, and it leads the line: the conversation,
	// the task, the person's own sentence for a watch or an errand. NAME FIRST,
	// because a strip gathered from every project on the machine is read by
	// looking for a thing you recognise, and the project is the answer to
	// "where", which is the second question.
	name string
	// place is the project, drawn dim behind the name.
	place string
	// at is WHEN THE WAIT BEGAN in `needs you`, and when the work began in
	// `moving`. Zero is a thing whose start nobody recorded, which draws no age
	// at all (the emptiness law) and sorts last among its equals.
	at time.Time
	// busy is how many nodes this row has running, and it is the moving zone's
	// order. It is zero for a conversation that is merely mid-turn, which is
	// live without having commissioned anything.
	busy int
	// lead is ONE CLAUSE IN FRONT OF THE PLACE AND THE AGE, said in a hue of its
	// own, and "" on a row that has nothing of the sort to say.
	//
	// It is a clause and a paint rather than a flag for a kind, because the thing
	// it does is general: a strip gathers rows of several kinds under one mark,
	// and now and then one of them has a fact about itself the mark cannot carry.
	// Today the only one is `landed` — work that finished and is waiting to be
	// read, which wears `▲` exactly as a card somebody is standing on does and
	// wants something completely different from a person.
	lead    string
	leadInk func(pal palette, s string) string
}

// attentionLanded is the one lead clause there is: work that finished and is
// waiting to be read. Green is what this surface says "it finished" in
// everywhere else ([palette.add]).
func attentionLanded(pal palette, s string) string { return pal.add(s) }

// ── the build ───────────────────────────────────────────────────────────────

// buildAttention puts the two zones above the list.
//
// A SEARCH HAS NO ZONES. With anything typed the column is the drop-up every
// width draws — matches, `ask here`, `start a new conversation` against the box
// — and two strips of triage standing over a filter would be answering a
// question the person had stopped asking (the standing bands stand down for the
// same reason, [homeView.buildWorld]).
func (h *homeView) buildAttention() {
	if h.searching() {
		return
	}
	// AN EMPTY MACHINE HAS NO GEOGRAPHY TO KEEP STABLE. A machine that has never
	// held a conversation draws ONE sentence — `nothing here yet` ([homeList]) —
	// and two labels over the top of it would be a screen laying out a map of a
	// place that does not exist yet. The stable-geography law is about a working
	// machine, where the zones are where you look; this is the emptiness law
	// applied to the whole surface, which is [app.landHome]'s third condition
	// said again one floor down.
	if len(h.world.Projects) == 0 && len(h.bare) == 0 && len(h.exchanges) == 0 {
		return
	}
	for _, zone := range homeZones {
		h.attentionZone(zone)
	}
}

// attentionZone draws one strip: its label, what it gathered in the order it
// gave, and one door where there is more than it draws.
//
// AN EMPTY ZONE KEEPS ITS LABEL AT THE WIDE TIER and vanishes whole below it —
// this file's fourth law. [homeView.wide] is read off the tier the frame settled
// before the column was built, exactly as [homeView.phone] is (home.go's
// [app.homeFrame] settles both, homebridge.go holds the ladder), so the shape of
// the list is decided once per width rather than argued about per row. It holds
// over the zones' own column at [homeTierColumns] as well: a label with nothing
// under it is where a person LOOKS for the thing that is not there.
func (h *homeView) attentionZone(zone homeZone) {
	rows := zone.gather(h)
	sort.SliceStable(rows, func(i, j int) bool {
		return zone.order(rows[i].zone, rows[j].zone)
	})
	if len(rows) == 0 && !h.wide() {
		return
	}
	h.lines = append(h.lines, homeLine{kind: homeAttentionZone, project: zone.word})
	if zone.shown <= 0 || len(rows) <= zone.shown {
		h.lines = append(h.lines, rows...)
		return
	}
	// A FOLD LINE IS A DOOR IN BOTH DIRECTIONS. Folded it says how many rows it
	// is standing for; open it is the way back, and it says the same number the
	// other way round ([homeMoreProjectsWord] spells both). It carries its own
	// zone's key, so two strips fold independently and neither has to know the
	// other exists.
	key := attentionFoldKey(zone.word)
	folded := !h.expanded[key]
	if folded {
		h.lines = append(h.lines, rows[:zone.shown]...)
	} else {
		h.lines = append(h.lines, rows...)
	}
	h.lines = append(h.lines, homeLine{
		kind: homeAttentionMore, project: zone.word, dir: key,
		quiet: len(rows) - zone.shown, folded: folded,
	})
}

// attentionNeeds is every blocked thing on the machine, in one order, whatever
// kind of thing it is and whichever project it is in: another window's card, a
// task that finished and needs somebody's eyes, a watch that stopped on a
// question, an errand holding a card.
//
// THEY ARE ONE ZONE BECAUSE THEY ARE ONE CLAIM. A person with four things
// stopped on them does not have a consent problem and a watch problem; they
// have four minutes of work in front of them, and the only thing worth sorting
// by is which has been standing still longest.
func (h *homeView) attentionNeeds() []homeLine {
	var rows []homeLine
	for _, ex := range h.exchanges {
		if !ex.waiting() {
			continue
		}
		rows = append(rows, h.attentionErrand(ex, &homeAttention{
			word: attentionNeedsWord, at: attentionErrandSince(ex),
		}))
	}
	for _, project := range h.everyProject() {
		for _, view := range h.items[project.Dir] {
			if strings.TrimSpace(view.Item.NeedsPerson) == "" {
				continue
			}
			rows = append(rows, h.attentionItem(project, view, &homeAttention{
				word: attentionNeedsWord, at: view.Item.Updated,
			}))
		}
		for _, row := range project.Sessions {
			if row.NeedsPerson() {
				rows = append(rows, attentionChat(project, row, &homeAttention{
					word: attentionNeedsWord, name: homeName(row),
					at: attentionWaitedSince(row),
				}))
			}
			// AND WORK THAT LANDED AND CANNOT SAY WHETHER IT HOLDS. `needs your
			// look` is a settled state of a task ([session.TaskUnverified], and
			// [taskStateWord] owns the words) and it is as stopped on a person as
			// any card: nothing else will happen to that work until somebody reads
			// it. The row is named by the TASK and its door is the conversation
			// that ran it, which is where looking happens.
			for _, entry := range row.Tasks.Rows {
				if entry.Status != string(session.TaskUnverified) {
					continue
				}
				rows = append(rows, attentionChat(project, row, &homeAttention{
					word: attentionNeedsWord, name: homeTaskText(entry),
					at:   entry.EndedAt,
					lead: homeLandedWord, leadInk: attentionLanded,
				}))
			}
		}
	}
	return rows
}

// attentionMoving is every live thing on the machine: a conversation with nodes
// out, a conversation mid-turn, a watch firing, an errand thinking.
//
// A CONVERSATION MID-TURN COUNTS WITH NOTHING COMMISSIONED. The model thinking
// and a tool out are work in flight even when no task node was ever made, and
// [session.PresenceWorking] is exactly that fact — the presence file says
// `working` off the turn and not off the graph (session's taskpresence.go).
//
// WHICH IS ALSO HOW A CONVERSATION IN THE KEEPER IS SEEN. A conversation this
// window holds behind the one on screen is fully alive, presence file included
// (keeper.go's header), so its turn shows up here through the same file every
// other window's does, believed for the same fifteen seconds. There is no
// cheaper door: the agent's own "a turn is running" is reachable only by
// attaching to its stream, and a draw that subscribed to eight event streams to
// paint a strip would be this screen paying for a fact it can read.
func (h *homeView) attentionMoving() []homeLine {
	var rows []homeLine
	for _, ex := range h.exchanges {
		if !ex.working || ex.waiting() {
			continue
		}
		rows = append(rows, h.attentionErrand(ex, &homeAttention{
			word: attentionMovingWord, at: ex.turnBegan, busy: 1,
		}))
	}
	for _, project := range h.everyProject() {
		for _, view := range h.items[project.Dir] {
			if !view.Running || strings.TrimSpace(view.Item.NeedsPerson) != "" {
				continue
			}
			rows = append(rows, h.attentionItem(project, view, &homeAttention{
				word: attentionMovingWord, at: view.Mark.Since, busy: 1,
			}))
		}
		for _, row := range project.Sessions {
			// WAITING OUTRANKS WORKING, which is the presence file's own law said
			// once more on this surface: a turn stopped on a question has a process
			// behind it and is standing still in every sense a person cares about,
			// and it is already in the zone above.
			if row.NeedsPerson() {
				continue
			}
			live := row.Live && row.Presence.State == session.PresenceWorking
			if row.Tasks.Running == 0 && !live {
				continue
			}
			rows = append(rows, attentionChat(project, row, &homeAttention{
				word: attentionMovingWord, name: homeName(row),
				at: attentionMovingSince(row), busy: row.Tasks.Running,
			}))
		}
	}
	return rows
}

// ── one live thing, as a row in a strip ─────────────────────────────────────
//
// THE THREE CONSTRUCTORS ARE WHERE A KIND IS TURNED INTO A ROW, and the zones
// above only ever SELECT. That separation is what keeps two strips from drifting
// apart: an errand's row is the same row in `needs you` and in `moving` down to
// its door, its name and the project it cites, and the only thing either zone
// says about it is which strip it is in and since when.

// attentionChat is one conversation as a zone row, named by whatever the zone
// calls it — the conversation itself, or one task inside it. The line is the
// LIST'S OWN session line ([homeView.projectBlock] builds the identical thing),
// which is what makes enter, the card and the digits work unchanged.
func attentionChat(project session.Project, row session.SessionRow, zone *homeAttention) homeLine {
	zone.place = project.Name
	return homeLine{
		kind: homeSession, project: project.Name, dir: project.Dir, row: row, zone: zone,
	}
}

// attentionItem is one standing item as a zone row, on the item row's own line
// ([homeView.itemLine]) so that enter, p and s mean there what they mean below.
func (h *homeView) attentionItem(project session.Project, view StandingItemView, zone *homeAttention) homeLine {
	zone.name, zone.place = strings.TrimSpace(view.Item.Words), project.Name
	line := h.itemLine(project, view)
	line.zone = zone
	return line
}

// attentionErrand is one `ask here` errand as a zone row. Its place is the
// bucket's own name, which is the only address an errand has
// ([homeView.projectNameOf]).
func (h *homeView) attentionErrand(ex *homeExchange, zone *homeAttention) homeLine {
	zone.name, zone.place = exchangeTitle(ex.spoke), h.projectNameOf(ex.bucket)
	return homeLine{
		kind: homeExchangeRow, project: zone.place, dir: ex.bucket, ex: ex, zone: zone,
	}
}

// attentionOlder is the age comparison both zones sort with. A stamp nobody
// recorded is not the oldest thing on the machine — it is an unknown, and it
// goes last rather than to the top of a list ordered by how long something has
// been standing still.
func attentionOlder(a, b time.Time) bool {
	if a.IsZero() != b.IsZero() {
		return b.IsZero()
	}
	return a.Before(b)
}

// attentionWaitedSince is when a conversation's question was put.
//
// [session.PresenceQuestion.Asked] is the stamp the session that is waiting
// wrote, which is exactly the fact this zone sorts on. A window on an older
// build, or a lane whose question describes no card, records none — and the
// fallback is when the PERSON LAST SPOKE, which is not the wait but is a bound
// on it: the question came after they spoke, so the row can never claim to have
// waited longer than it has.
//
// ── AND THE DESTRUCTIVE PIN IS NOT BUILT ────────────────────────────────────
//
// The design asks for one exception to waited-longest-first: a consent card for
// something DESTRUCTIVE pins to the top for as long as it waits. Nothing on this
// machine can say which one that is. The classification exists — internal/
// approval's critical-command table, [approval.AlwaysAsks] — but it never
// reaches a question: [session.PresenceQuestion] carries a kind, an id, the
// options and the line `needs your ok to run bash`, which is the TOOL's name and
// not the command's text (session's consent.go). So this surface could only
// guess, and a pin that guessed would put a `rm -rf` row and a `git status` row
// in the same place at the top of the one zone a person trusts.
//
// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN. The pin lands the day the
// engine writes the fact down, and not before.
func attentionWaitedSince(row session.SessionRow) time.Time {
	if asked := row.Presence.Question.Asked; !asked.IsZero() {
		return asked
	}
	return row.At
}

// attentionMovingSince is when a live conversation's work began: the oldest node
// it has out, or — for one that is merely mid-turn — when the person last spoke,
// which is when a turn begins ([session.SessionRow.At]).
//
// A TURN A WAKE STARTED IS OLDER THAN ITS AGE SAYS. Nothing records when such a
// turn began, and the last thing said is the nearest stamp there is; it is never
// NEWER than the truth, which is the direction an approximation on this screen
// is allowed to be wrong in.
func attentionMovingSince(row session.SessionRow) time.Time {
	var oldest time.Time
	for _, out := range row.Presence.RunningTasks {
		if out.StartedAt.IsZero() {
			continue
		}
		if oldest.IsZero() || out.StartedAt.Before(oldest) {
			oldest = out.StartedAt
		}
	}
	if !oldest.IsZero() {
		return oldest
	}
	if row.Tasks.Running > 0 {
		// Nodes are out and none of them recorded a start — a session under a
		// build older than the stamp. It draws no age rather than borrowing the
		// conversation's, which is [app.homeTaskTail]'s own choice about the same
		// missing fact.
		return time.Time{}
	}
	return row.At
}

// attentionErrandSince is when an errand's card arrived, as nearly as an errand
// records it: the turn that produced the card is the one that was in flight when
// it appeared, and an errand is one short turn (homeexchange.go). It is the same
// stamp the row's own working clock counts from, so the two never disagree.
func attentionErrandSince(ex *homeExchange) time.Time {
	if !ex.turnBegan.IsZero() {
		return ex.turnBegan
	}
	if !ex.said.IsZero() {
		return ex.said
	}
	return ex.began
}

// ── the cursor ──────────────────────────────────────────────────────────────

// pointAt puts the cursor on the first line a test accepts, PREFERRING one
// outside the zones, and leaves the cursor where it is when no line matches at
// all.
//
// THE LIST'S ROW WINS. A zone row and a row under a project stand for one live
// object and the zone's is drawn first, so the three restores that find a thing
// by its identity ([homeView.point], [homeView.pointItem],
// [homeView.pointExchange]) would otherwise lift the cursor out of the list
// every time they ran — on every keystroke, and on every three-second rescan.
// A cursor that was genuinely IN a zone is put back into it afterwards and on
// purpose ([homeView.pointZone]). A thing whose only row is a zone row — a
// waiting conversation inside a folded project — is still pointed at, because
// the alternative is a door the cursor cannot reach.
func (h *homeView) pointAt(is func(homeLine) bool) {
	zoned := -1
	for at, line := range h.lines {
		if !is(line) {
			continue
		}
		if line.zone == nil {
			h.cursor = at
			return
		}
		if zoned < 0 {
			zoned = at
		}
	}
	if zoned >= 0 {
		h.cursor = zoned
	}
}

// cursorZone is which zone the cursor is standing in, and "" for the list below.
func (h *homeView) cursorZone() string {
	if h.cursor < 0 || h.cursor >= len(h.lines) {
		return ""
	}
	return attentionWordOf(h.lines[h.cursor])
}

// attentionWordOf is one line's zone, and "" for every line that is not in one.
func attentionWordOf(line homeLine) string {
	if line.zone == nil {
		return ""
	}
	return line.zone.word
}

// pointZone keeps the cursor in the zone it was standing in.
//
// IT IS THE PRICE OF THE FIRST LAW. A row in a zone and the same row under its
// project are two views of one object, so [homeView.point] — which finds a
// conversation by its transcript — cannot tell them apart, and takes the first,
// which is always the zone's. Without this, a person reading down the list would
// be lifted into `needs you` every three seconds by the rescan, and a person
// standing in a zone would be dropped into the list. So the zone is remembered
// across a build and the cursor is put back into it, exactly as the conversation
// itself is ([homeView.build]).
func (h *homeView) pointZone(want string) {
	if h.cursor < 0 || h.cursor >= len(h.lines) {
		return
	}
	here := h.lines[h.cursor]
	if attentionWordOf(here) == want {
		return
	}
	for at, line := range h.lines {
		if attentionWordOf(line) == want && attentionSame(line, here) {
			h.cursor = at
			return
		}
	}
}

// attentionSame reports that two lines are two views of ONE live object. Two
// zone rows for one conversation — a card it is stopped on and a task of its
// own that needs a look — are told apart by what they are called, which is the
// only thing about them that differs.
func attentionSame(a, b homeLine) bool {
	if a.kind != b.kind {
		return false
	}
	if a.zone != nil && b.zone != nil && a.zone.name != b.zone.name {
		return false
	}
	switch a.kind {
	case homeSession:
		return a.row.Transcript != "" && a.row.Transcript == b.row.Transcript
	case homeItem:
		return a.item.ID != "" && a.item.ID == b.item.ID
	case homeExchangeRow:
		return a.ex != nil && a.ex == b.ex
	}
	return false
}

// foldZone opens or folds one strip's tail, and leaves the cursor on the line
// that did it so the gesture can be reversed without moving —
// [homeView.foldElsewhere]'s shape over a keyed fold.
func (h *homeView) foldZone(key string, open bool) {
	h.setFold(key, open)
	held := h.cursor
	h.rebuild()
	for at, line := range h.lines {
		if line.kind == homeAttentionMore && line.dir == key {
			h.cursor = at
			h.picked = true
			return
		}
	}
	h.cursor = h.clamp(held)
}

// ── the paint ───────────────────────────────────────────────────────────────

// attentionLine draws the zones' own lines, and reports false for every line
// that is not one — which is how [app.homeLine] hands this file its three rows
// without learning anything about them.
func (a *app) attentionLine(line homeLine, at, width int, pal palette) (string, bool) {
	h := &a.home
	switch {
	case line.kind == homeAttentionZone:
		// A LABEL IS ONE DIM WORD, in the heading's own indent and with nothing
		// beside it: no count, no rule, no age. The rows below carry their own
		// facts, and a label that spent a second column on decoration would cost
		// more than the zone it names.
		return "  " + pal.dim(fit(line.project, width-2)), true
	case line.kind == homeAttentionMore:
		return overlayRow(homeFoldMark(line.folded, pal)+" "+homeMoreProjectsWord(line), "",
			at == h.cursor, false, at == h.hover, width, pal), true
	case line.zone != nil:
		return a.attentionRow(line, at, width, pal), true
	}
	return "", false
}

// attentionNameFloor is how many cells a zone row keeps for the thing's own
// name whatever the tail wants — [standWordsFloor]'s law over these rows, and
// for the same defect: [overlayRowTinted] gives the tail whatever it asks for
// and cuts the label with what is left, which on a thirty-cell strip beside a
// long project name draws a row that is all place and no name. The name is what
// tells two rows apart; the place is where it lives.
const attentionNameFloor = 16

// attentionRoomy reports that a tail may stand beside a name in a row with this
// much room: either the WHOLE NAME still fits next to it, or the name still has
// its floor.
//
// THE FLOOR IS A FALLBACK AND NOT A TOLL. Asked as the floor alone it is a
// question about the longest name a row could have rather than about the name
// this row HAS — which on the zones' own narrow column at [homeTierColumns]
// takes the place and the age off a row of ten cells with fourteen to spare.
func attentionRoomy(left, name int) bool {
	return left >= name || left >= attentionNameFloor
}

// attentionRow is one zone row: the mark, the name, and the clauses against the
// right edge.
//
// ── WHY IT COMPOSES THE ROW ITSELF ──────────────────────────────────────────
//
// Every other row on this column is laid out by [overlayRowTinted], which
// paints the whole label in ONE hue. These rows paint one cell of their own, and
// the two cannot be combined: every foreground sequence on this surface closes
// with SGR 39 (styles.go's [palette.cursor] states it), so a glyph painted before
// the label was handed over would end its own colour and leave everything after
// it in the terminal's default ink. So the arithmetic and the two backgrounds
// are borrowed — the same two-cell lead, the same right edge, the same band and
// hover — and only the ink is decided here.
//
// ── WHAT EACH PART IS SAID IN ───────────────────────────────────────────────
//
// THE GLYPH CARRIES THE MEANING AND THE TEXT STAYS CALM, which is the rail's own
// division ([app.taskStateInk] paints the mark and leaves the name alone). The
// mark is the one cell a person's eye lands on first, so it is the one cell
// worth a hue:
//
//   - `▲` takes the QUESTION HUE ([palette.askBold]). A row in `needs you` is
//     the state that hue exists for — a person being waited on — and this is the
//     only place outside a question card that has any business wearing it.
//   - `●` takes [palette.muted], the still blue one rung under the accent. It is
//     the machine working rather than the person being asked, and it stays under
//     the accent the cursor's own lead is drawn in so a strip of live rows can
//     never look like a column of cursors.
//   - The name is INK, because these two strips are a summary a person reads
//     first and the list below is the index they read after — the calm-dim law
//     is the LIST's ([app.homeLine]) and the zones are what stands above it.
//   - The place and the age are DIM. They are furniture on a row whose subject
//     is the name, and an age is grey wherever it appears on this screen.
//   - A LEAD CLAUSE takes whatever hue it was built with, which today is
//     [palette.add] for `landed` and nothing else ([homeAttention.lead]).
func (a *app) attentionRow(line homeLine, at, width int, pal palette) string {
	h := &a.home
	zone := line.zone
	selected, hovered := at == h.cursor, at == h.hover

	mark := a.attentionMark(pal, zone.word, at)
	markWidth := ansi.StringWidth(mark)
	room := width - 2 - markWidth - 1
	tail, painted := a.attentionTail(zone, pal, selected, room)
	if tail != "" {
		room -= ansi.StringWidth(tail) + 1
	}
	name := fit(zone.name, room)
	inked := pal.ink(name)
	if selected {
		inked = pal.bold(inked)
	}
	text := overlayLead(selected, hovered, pal) + mark + " " + inked
	if tail != "" {
		gap := width - 2 - markWidth - 1 - ansi.StringWidth(name) - ansi.StringWidth(tail)
		if gap < 1 {
			gap = 1
		}
		text += strings.Repeat(" ", gap) + painted
	}
	// NOTHING IN A ZONE STRIP IS OPEN, so nothing in one wears the selected step.
	// A zone row is a conversation somebody has not walked into yet, and both the
	// keyboard cursor and the pointer are the same fact reached by two hands —
	// THE GROUND LADDER gives that fact ONE rung. What still tells the two apart
	// is the lead, `›` against `·`, which [overlayLead] draws above.
	if selected || hovered {
		return pal.cursor(text, width)
	}
	return text
}

// attentionTail is the clauses against the right edge of a zone row: as plain
// text, and painted clause by clause. The two are built together because the
// arithmetic measures the first and the frame draws the second.
//
// THE NAME OUTRANKS THE TAIL, and the tail gives way ONE CLAUSE AT A TIME rather
// than all at once — the place goes, then the lead, and THE AGE IS THE LAST
// THING TO GO. Which order that is says what these rows are for: the place is
// the answer to "where", and the card beside the row has it in full; the age is
// the answer to "how long has this been standing still", which is the very thing
// `needs you` is ordered by, and a column ordered by a wait it never draws is a
// column asking to be taken on trust.
func (a *app) attentionTail(zone *homeAttention, pal palette, selected bool, room int) (string, string) {
	quiet := attentionQuiet(pal, selected)
	lead := ""
	if zone.leadInk != nil {
		lead = zone.lead
	}
	age := sinceAt(zone.at, a.home.world.Read)
	for _, text := range []string{
		joinDot(lead, joinDot(zone.place, age)),
		joinDot(lead, age),
		age,
	} {
		if text == "" {
			return "", ""
		}
		if !attentionRoomy(room-ansi.StringWidth(text)-1, ansi.StringWidth(zone.name)) {
			continue
		}
		if lead != "" && strings.HasPrefix(text, lead) {
			return text, zone.leadInk(pal, lead) + quiet(strings.TrimPrefix(text, lead))
		}
		return text, quiet(text)
	}
	return "", ""
}

// attentionQuiet is how a zone row's tail is painted: dim, and ink on the row
// the cursor is on — [paintNote]'s ordinary rule, said here because these rows
// lay themselves out.
func attentionQuiet(pal palette, selected bool) func(string) string {
	paint := pal.dim
	if selected {
		paint = pal.ink
	}
	return func(s string) string {
		if s == "" {
			return ""
		}
		return paint(s)
	}
}

// attentionMark is one row's cell, painted: the zone's own glyph in the zone's
// own hue, both read off [homeZones] rather than decided here.
//
// THE TWO MARKS ARE THE COLUMN'S OWN and not a third vocabulary: `▲` is the only
// shape on this screen that points at anything and it means "asking for a hand"
// wherever it appears (home.go's glyph block); `●` is the still tier of
// "something is happening".
//
// AND EXACTLY ONE ROW IN THE MOVING ZONE TURNS: the spinner stands in for that
// zone's own still mark on the one line the page gave it and nowhere else, in
// the same hue, so a strip of live rows reads as one thing moving among several
// rather than as a column of weather (homespinner.go holds the whole law, and
// docs/HOME-BRIDGE.md the reason).
func (a *app) attentionMark(pal palette, word string, at int) string {
	zone, ok := attentionZoneOf(word)
	if !ok {
		return ""
	}
	mark := zone.mark(pal.ascii)
	if word == attentionMovingWord && a.homeSpins(at) {
		mark = a.homeSpinGlyph()
	}
	return zone.ink(pal, mark)
}
