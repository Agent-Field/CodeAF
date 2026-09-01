package tui3

// HOME ON A PHONE IS AN INBOX, NOT A DIRECTORY.
//
// Under sixty columns ([tierPhone]) home had no shape of its own. The detail
// column went with the width ([homeCardMin]) and nothing else changed, so a
// forty-four-cell frame drew the left column and only the left column: no card,
// no answer chips — you could not approve another window's command from your
// phone — no news, no outcome, no deliverables, no project card, and no `m` to
// reach any of it. One thing on this screen had a narrow shape and it was
// `ask here`, which stacks ([app.homeStacked]).
//
// The fix is not a squeezed dashboard. A dashboard is a thing you SCAN, and
// scanning wants width; a phone-shaped frame is a thing you WALK, one row at a
// time, top to bottom. So at this tier the column stops being a directory of
// projects and becomes an INBOX across all of them:
//
//	 home                        esc close
//	 ─────────────────────────────────────
//	 waiting on you
//	 › ▲ fix the nil-map crash
//	     waiting on you · 12m
//	   ▲ every Monday, the weekly update
//	     needs your look · Mondays 9am
//	   ▸ …2 more
//
//	 running
//	   ● port the picker
//	     2 running · 4m
//
//	 since you left
//	   ◆ the deploy went green
//	     12m · aforge-v2
//
//	 aforge-v2
//	   ○ tidy the roster
//	     3 tasks · 2h
//	 ▸ wisp                      6 · 2d
//	 ─────────────────────────────────────
//	 type to search or start something new
//	  open      new       ask here
//
// FOUR LAWS, AND EVERY ONE OF THEM IS ABOUT A THUMB.
//
//   - TRIAGE FIRST AND ACROSS EVERYTHING. `waiting on you`, `running` and
//     `since you left` are the three questions somebody opens their phone to
//     ask, and none of them is a question about one project — so the sections
//     gather conversations, standing items and errands from every project on the
//     machine. The projects come after them, and only this window's is drawn
//     open; the rest are one line each, exactly as the `elsewhere` block already
//     draws them, without the rule line that costs a row nobody can spare.
//
//   - A ROW APPEARS ONCE. A conversation lifted into `waiting on you` is not
//     drawn again under its project. The desktop can afford to say a thing twice
//     because the eye takes both in at once; twelve rows of screen cannot.
//
//   - THREE, THEN A DOOR. Every section shows three rows and folds the rest into
//     `▸ …N more`, which is the same fold mark, the same word and the same
//     gesture as every other fold on this surface ([bandFoldWord]). A section
//     with nothing in it is not drawn at all — the emptiness law applied to a
//     whole heading.
//
//   - A ROW IS TWO LINES. The label on one, the dim tail under it, which is the
//     two-line law this tier already keeps for every list ([overlayLines]).
//
// AND SEARCH IS UNTOUCHED. Typing filters exactly as it does at every other
// width, with no tiers and no sections ([homeView.buildWorld] states why), and
// the two action rows stay where they have always been — against the box, at
// the foot.

import (
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// The row kinds the inbox adds, declared HERE and given values far above the
// iota block in home.go for [homeAskHere]'s reason: that block is being edited
// by other lanes in the same wave, and a constant appended to it would be a
// conflict over a line that says nothing.
const (
	// homePhoneSection is one of the three triage headings. It is NOT a cursor
	// stop — a heading names a section rather than a thing, exactly as
	// [homeHeading] does — but a TAP on it folds the section away, because on a
	// screen this short "I have dealt with those" is a gesture worth having.
	homePhoneSection homeRowKind = 210
	// homePhoneNews is one thing that happened while you were away: a note left
	// in a conversation's inbox or in a project's, or a task that landed since
	// the last time you spoke. It is a cursor stop and its door is the thing it
	// is about.
	homePhoneNews homeRowKind = 211
	// homePhoneMore is a section's own fold line — `▸ …4 more` — and it is a
	// door exactly as [homeQuiet] is, with the same mark and the same gestures.
	homePhoneMore homeRowKind = 212
)

// The three headings, in the order they are drawn. Each is one dim word-group
// and each is quoted in the manual exactly as it is spelled here.
const (
	homePhoneWaitingWord = "waiting on you"
	homePhoneRunningWord = "running"
	homePhoneNewsWord    = "since you left"
)

// homePhoneShown is how many rows a section draws before the rest fold. Three,
// because a row is two lines at this tier — three rows and a fold line is seven
// screen lines, and three sections of that is already more than a short phone
// frame holds.
const homePhoneShown = 3

// homePhoneBarFloor is the narrowest frame the action bar is drawn on. Under it
// three targets is seven cells each, which is a target that misses, and the
// plain hint line — which degrades by truncating rather than by missing — is the
// better shape.
const homePhoneBarFloor = 24

// The section fold keys. They live in [homeView.sections] rather than in
// [homeView.expanded] because they fold a different kind of thing: somebody who
// put the `since you left` section away has not thereby folded a project.
const (
	homePhoneWaitingKey = "waiting"
	homePhoneRunningKey = "running"
	homePhoneNewsKey    = "news"
)

// homePhoneNote is one line of `since you left`.
//
// IT IS RESOLVED WHEN THE SECTION IS BUILT and carries the subject it belongs
// to, so the row and the sheet it opens can never disagree about what the news
// was about.
type homePhoneNote struct {
	words   string
	at      time.Time
	project string
	dir     string
	// row is the conversation the news belongs to, and hasRow says there is
	// one — a note left in a PROJECT's inbox has no conversation behind it
	// (homeband_news.go says why that inbox exists).
	row    session.SessionRow
	hasRow bool
	proj   session.Project
}

// homePhone reports whether home is being drawn at [tierPhone] right now.
//
// It asks the TERMINAL's width and not the body's, unlike [app.phoneFrame]:
// home takes the whole frame, so there is no body region to be narrower than
// it.
func (a *app) homePhone() bool {
	if !a.at(pageHome) {
		return false
	}
	width, _ := a.size()
	return layoutTier(width) == tierPhone
}

// ── the build ───────────────────────────────────────────────────────────────

// buildPhone is [homeView.buildWorld] at this tier: three triage sections, then
// this window's project open and every other one folded to a line.
//
// A SEARCH IS NOT AN INBOX. With anything typed the list is the drop-up every
// other width draws — the matches, `ask here` and `start a new conversation`
// against the box — because a filter that kept its sections would be hiding
// matches behind headings a person did not ask for.
func (h *homeView) buildPhone() {
	if h.searching() {
		h.buildWorld()
		return
	}
	lifted := phoneLifted{rows: map[string]bool{}, errands: map[*homeExchange]bool{}}
	h.phoneSection(homePhoneWaitingWord, homePhoneWaitingKey, h.phoneWaiting(lifted))
	h.phoneSection(homePhoneRunningWord, homePhoneRunningKey, h.phoneRunning(lifted))
	h.phoneSection(homePhoneNewsWord, homePhoneNewsKey, h.phoneNews())
	h.phoneProjects(lifted)
}

// phoneSection draws one heading and the rows under it, three of them and then
// a door. A section with nothing in it draws NOTHING — not the heading, not the
// blank above it — which is the emptiness law at the scale of a whole block.
func (h *homeView) phoneSection(word, key string, rows []homeLine) {
	if len(rows) == 0 {
		return
	}
	h.blank()
	h.lines = append(h.lines, homeLine{kind: homePhoneSection, project: word, dir: key})
	shown, hidden := rows, 0
	if !h.sections[key] && len(rows) > homePhoneShown {
		shown, hidden = rows[:homePhoneShown], len(rows)-homePhoneShown
	}
	h.lines = append(h.lines, shown...)
	if hidden > 0 || h.sections[key] && len(rows) > homePhoneShown {
		h.lines = append(h.lines, homeLine{
			kind: homePhoneMore, project: word, dir: key,
			quiet: len(rows) - homePhoneShown, folded: !h.sections[key],
		})
	}
}

// phoneLifted is what the triage sections took, so the projects under them do
// not say it twice (this file's second law).
type phoneLifted struct {
	rows    map[string]bool
	errands map[*homeExchange]bool
}

// phoneWaiting is everything on this machine that has stopped and is asking for
// a hand: a conversation on a question, a standing item that needs a look, an
// errand holding a card. They are one section because they are one claim on a
// person's attention, and the section is the reason this screen exists.
func (h *homeView) phoneWaiting(lifted phoneLifted) []homeLine {
	var out []homeLine
	for _, ex := range h.exchanges {
		if ex.waiting() {
			lifted.errands[ex] = true
			out = append(out, homeLine{kind: homeExchangeRow, project: h.projectNameOf(ex.bucket), dir: ex.bucket, ex: ex})
		}
	}
	for _, project := range h.everyProject() {
		for _, view := range h.items[project.Dir] {
			if strings.TrimSpace(view.Item.NeedsPerson) != "" {
				out = append(out, h.itemLine(project, view))
			}
		}
		for _, row := range project.Sessions {
			if !row.NeedsPerson() {
				continue
			}
			lifted.rows[row.Transcript] = true
			out = append(out, homeLine{
				kind: homeSession, project: project.Name, dir: project.Dir, row: row,
			})
		}
	}
	return out
}

// everyProject is every project on the screen, the ones home knows only through
// the things keeping an eye on them included ([homeBare]). The sections gather
// from all of them, because a watch on a workspace nobody has spoken in is
// exactly as able to need somebody as one on a busy project.
func (h *homeView) everyProject() []session.Project {
	out := make([]session.Project, 0, len(h.world.Projects)+len(h.bare))
	out = append(out, h.world.Projects...)
	for _, bare := range h.bare {
		out = append(out, bare.project)
	}
	return out
}

// phoneRunning is everything with work in flight. It is the second section for
// the reason it is the second glyph: something moving is something you check
// on, and something stopped is something you unblock.
func (h *homeView) phoneRunning(lifted phoneLifted) []homeLine {
	var out []homeLine
	for _, ex := range h.exchanges {
		if ex.working && !lifted.errands[ex] {
			lifted.errands[ex] = true
			out = append(out, homeLine{kind: homeExchangeRow, project: h.projectNameOf(ex.bucket), dir: ex.bucket, ex: ex})
		}
	}
	for _, project := range h.everyProject() {
		for _, view := range h.items[project.Dir] {
			if view.Running && strings.TrimSpace(view.Item.NeedsPerson) == "" {
				out = append(out, h.itemLine(project, view))
			}
		}
		for _, row := range project.Sessions {
			if row.NeedsPerson() || row.Tasks.Running == 0 {
				continue
			}
			lifted.rows[row.Transcript] = true
			out = append(out, homeLine{
				kind: homeSession, project: project.Name, dir: project.Dir, row: row,
			})
		}
	}
	return out
}

// phoneNews is `since you left`, newest first.
func (h *homeView) phoneNews() []homeLine {
	notes := h.phoneNotes()
	out := make([]homeLine, 0, len(notes))
	for at := range notes {
		note := notes[at]
		out = append(out, homeLine{
			kind: homePhoneNews, project: note.project, dir: note.dir,
			row: note.row, proj: note.proj, note: &note,
		})
	}
	return out
}

// projectNameOf is what a bucket directory is CALLED, for the rows the sections
// lift out of their projects. An errand asked in a project home does not know
// about answers with the bucket's own name, which is the only address it has.
func (h *homeView) projectNameOf(dir string) string {
	for _, project := range h.world.Projects {
		if project.Dir == dir {
			return project.Name
		}
	}
	return dir
}

// phoneProjects is the bottom half: this window's own project drawn open, and
// every other one folded to a line.
//
// THERE IS NO `elsewhere` RULE HERE. The rule earns its row on a wide frame,
// where it is the lid over a block of folded projects — at this tier the folded
// lines follow the one open project directly, and a rule between them would be
// a row spent on punctuation that the shape already says.
func (h *homeView) phoneProjects(lifted phoneLifted) {
	var found []homeHit
	for _, project := range h.world.Projects {
		hit := homeHit{project: project, at: project.At()}
		for _, row := range project.Sessions {
			// A PUT-AWAY ROW IS NOT IN THE INBOX. The phone tier has no room
			// for the archive's own fold; searching still finds the row, and
			// the wide frame is where it is brought back (home.go's
			// the ranked reading leaves them out, and typing a name is the way
			// back to one).
			if row.Archived {
				continue
			}
			// A ROW APPEARS ONCE (this file's second law). What the sections
			// lifted out is not drawn again down here.
			if !lifted.rows[row.Transcript] {
				hit.rows = append(hit.rows, row)
			}
		}
		found = append(found, hit)
	}
	for _, bare := range h.bare {
		if len(h.items[bare.project.Dir]) > 0 {
			found = append(found, homeHit{project: bare.project, at: bare.at, bare: true})
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].at.After(found[j].at) })
	// ONE PROJECT OPEN AND IT IS THIS WINDOW'S, which is [homeTiers]'s own law
	// with the count taken down to one: this is the project this window can open
	// a conversation in, and it is the only one whose rows are worth two lines
	// each on a frame this short.
	open, folded := homeTiers(found, h.bucket)
	if len(open) > 1 {
		folded = append(open[1:], folded...)
		open = open[:1]
	}
	h.placeExchanges(open)
	// AND THE ERRANDS THE SECTIONS LIFTED ARE NOT DRAWN AGAIN either. The map
	// [homeView.exchangeLines] reads is the one place that decides where a row
	// goes, so an errand taken out of it has no second row anywhere.
	for ex := range lifted.errands {
		delete(h.exchangeIn, ex)
	}
	for _, hit := range open {
		h.blank()
		h.lines = append(h.lines, homeLine{
			kind: homeHeading, project: hit.project.Name, dir: hit.project.Dir, bare: hit.bare,
		})
		h.projectBlock(hit, "")
	}
	if len(folded) == 0 {
		return
	}
	sort.SliceStable(folded, func(i, j int) bool {
		return h.projectHot(folded[i].project) && !h.projectHot(folded[j].project)
	})
	h.blank()
	for _, hit := range folded {
		open := h.expanded[homeProjectKey(hit.project.Dir)]
		h.lines = append(h.lines, homeLine{
			kind: homeProject, project: hit.project.Name, dir: hit.project.Dir,
			proj: hit.project, folded: !open,
		})
		if open {
			h.projectBlock(hit, "")
			h.blank()
		}
	}
}

// ── what happened while you were away ───────────────────────────────────────

// phoneNotes reads the machine's news, and reads it on HOME'S OWN CLOCK and not
// on the paint clock.
//
// The inboxes are files on disk and this list is rebuilt on every keystroke, so
// the walk is taken once per reading of the world ([homeEvery]) and answered
// from the cache in between — the same bargain [app.homeHeld] makes about the
// lock and [drawNewsBand] makes about the same files.
func (h *homeView) phoneNotes() []homePhoneNote {
	now := h.world.Read
	if !h.inboxAt.IsZero() && !now.IsZero() && now.Sub(h.inboxAt) < homeEvery {
		return h.inbox
	}
	var out []homePhoneNote
	for _, project := range h.everyProject() {
		for _, note := range standing.PeekProjectInbox(h.standRoot, homeProjectPath(project)) {
			out = append(out, homePhoneNote{
				words: standNoteWords(note), at: note.At,
				project: project.Name, dir: project.Dir, proj: project,
			})
		}
		for _, row := range project.Sessions {
			for _, note := range readHomeNews(row.Dir) {
				out = append(out, homePhoneNote{
					words: standNoteWords(note), at: note.At,
					project: project.Name, dir: project.Dir,
					row: row, hasRow: true, proj: project,
				})
			}
			// AND WORK THAT LANDED WHILE YOU WERE NOT IN THE ROOM. It is the
			// same derivation the `◆` on a standing row makes ([standNews]):
			// [session.SessionRow.At] is when the PERSON last spoke, so a task
			// that finished after it is a thing they have not seen. Nothing is
			// asserted about "last looked" — this surface has no such record and
			// will not invent one.
			for _, entry := range row.Tasks.Rows {
				if entry.EndedAt.IsZero() || !entry.EndedAt.After(row.At) {
					continue
				}
				out = append(out, homePhoneNote{
					words: homeTaskText(entry), at: entry.EndedAt,
					project: project.Name, dir: project.Dir,
					row: row, hasRow: true, proj: project,
				})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].at.After(out[j].at) })
	h.inbox, h.inboxAt = out, now
	return out
}

// standNoteWords is what one note SAYS, on the news band's own terms: the words
// the thing was asked in, and the line it came to under them.
func standNoteWords(note standing.Note) string {
	return joinDot(strings.TrimSpace(note.Words), strings.TrimSpace(note.Text))
}

// ── the frame ───────────────────────────────────────────────────────────────

// homePhoneFrame is the whole screen while the inbox is up: exactly height
// rows, which line of the column each of them answers to the pointer, and where
// the caret sits. It is [app.homeFrame]'s answer at this tier and it keeps that
// function's contract, because the press and the hover index what it returned.
//
// It spends FIVE rows on chrome where the wide frame spends eight: a head, one
// rule, the box, and the action bar. A phone frame has twelve rows of list in
// it on a good day, and three of them cannot go on air.
func (a *app) homePhoneFrame(width, height int) ([]string, []int, int, int) {
	if a.homeSheetShowing() {
		return a.homeSheetFrame(width, height)
	}
	pal := a.pal
	var lines []string
	var hits []int
	var panes []int
	add := func(text string, hit int) {
		lines = append(lines, text)
		hits = append(hits, hit)
		panes = append(panes, -1)
	}

	add(a.homePhoneHead(width, pal), -1)
	add(pal.dim(rule(width)), -1)

	const foot = 3 // the rule, the box, the bar
	room := height - len(lines) - foot
	if room < 1 {
		room = 1
	}
	a.home.top = listTop(a.home.cursor, a.home.top, len(a.home.lines), overlayItems(room, width))
	body := a.homePhoneList(width, room, pal)
	for i := 0; i < room; i++ {
		// THE REGION IS FILLED WHETHER OR NOT THE LIST FILLS IT. The foot is
		// drawn after this loop and the frame is padded at the END, so a body
		// short of its region would push the box and the bar up the screen and
		// leave the padding under them — which is the bar in a different place on
		// every machine.
		if i < len(body) {
			add(body[i].text, body[i].hit)
			continue
		}
		add("", -1)
	}

	add(pal.dim(rule(width)), -1)
	caretX, caretY := 0, 0
	if a.home.box.empty() {
		add(" "+pal.dim(fit(homeFootWord, width-2)), -1)
		// Same as the wide frame: at rest there is nothing to type into, so the
		// caret is hidden rather than blinking over the heading.
		a.caret = false
	} else {
		text := a.home.box.String()
		// THE PERSON'S OWN GLYPH IS STRUCTURE ON A PLACE, NOT AN ACCENT. It is
		// the same cell on every frame home has ever drawn, and the design spends
		// colour on the two live states alone (styles.go's THE ONE-ACCENT LAW),
		// so the prompt takes the second tier and the words keep the first.
		add(" "+pal.muted("› ")+pal.ink(fit(text, width-4)), -1)
		caretX, caretY = 3+ansi.StringWidth(text), len(lines)-1
		if caretX > width-1 {
			caretX = width - 1
		}
	}
	add(a.homeBar(width, a.homeInboxBar(), pal), -1)
	a.home.barRow = len(lines) - 1

	if len(lines) > height {
		removed := len(lines) - height
		cut := len(lines) - (height - 1)
		lines = append(lines[:1], lines[cut:]...)
		hits = append(hits[:1], hits[cut:]...)
		panes = append(panes[:1], panes[cut:]...)
		a.home.barRow = len(lines) - 1
		// THE CARET RIDES THE CLAMP, as on the wide frame: rows removed above
		// the box shift it up, and a caret computed before the cut would blink
		// below it. One whose row was cut away is hidden rather than guessed.
		switch {
		case caretY >= 1+removed:
			caretY -= removed
		case caretY > 0:
			a.caret = false
		}
	}
	for len(lines) < height {
		add("", -1)
	}
	a.home.pane = panes
	return lines, hits, caretX, caretY
}

// homePhoneHead is the one row at the top: what this is, and the way out.
func (a *app) homePhoneHead(width int, pal palette) string {
	head := " " + pal.bold(pal.ink("home"))
	escape := pal.dim("esc close")
	if ansi.StringWidth(head)+ansi.StringWidth(escape)+2 <= width {
		gap := width - ansi.StringWidth(head) - ansi.StringWidth(escape) - 1
		head += strings.Repeat(" ", gap) + escape
	}
	return head
}

// homePhoneList is the window of rows the cursor is inside, drawn two lines to
// a row.
//
// A ROW THAT WOULD STRADDLE THE BOTTOM EDGE IS NOT DRAWN, which is
// [overlayFill]'s law said again here: half a row is a label with its facts cut
// off, and the facts are what the row is for.
func (a *app) homePhoneList(width, room int, pal palette) []homeDrawn {
	h := &a.home
	drawn := make([]homeDrawn, 0, room)
	if len(h.lines) == 0 {
		word := homeEmptyWord
		switch {
		case h.searching():
			word = homeNoMatchWord
		case !h.known:
			// A WORLD THAT HAS NOT ANSWERED IS NOT AN EMPTY MACHINE. Over --host
			// the rows come from the other machine and the first frames are drawn
			// before they have arrived; `nothing here yet` over a server full of
			// work is the one wrong sentence this screen can say about somebody
			// else's disk. Unknown renders as nothing ([homeView.known]).
			return nil
		}
		return []homeDrawn{{text: "  " + pal.dim(fit(word, width-2)), hit: -1, pane: -1}}
	}
	for at := h.top; at < len(h.lines) && len(drawn) < room; at++ {
		rows := a.homePhoneRow(h.lines[at], at, width, pal)
		if len(drawn)+len(rows) > room {
			break
		}
		// A HEADING IS NEVER THE LAST ROW OF THE REGION. It names the rows under
		// it, and a name with nothing under it is a row spent promising something
		// the screen did not deliver — the emptiness law, applied to the bottom
		// edge rather than to a value.
		if headingKind(h.lines[at].kind) && len(drawn)+len(rows) == room {
			break
		}
		for _, text := range rows {
			drawn = append(drawn, homeDrawn{text: text, hit: at, pane: -1})
		}
	}
	return drawn
}

// headingKind reports whether a line NAMES rows rather than being one.
//
// The switcher's own heading is one of them, which is why it is in a list that
// started as the phone's: the same question is asked by
// [homeView.markedSection], where the answer has to hold for every section on
// the column and not only for the ones a phone draws (homesection.go). The
// claim over the ranked list, the `since you left` heading and a project's name
// under `alt+g` all name the rows below them exactly as a project heading does.
func headingKind(kind homeRowKind) bool {
	return kind == homePhoneSection || kind == homeHeading || kind == homeSwitchHead
}

// homePhoneRow is one line of the column as the lines it takes.
//
// EVERY ROW GOES THROUGH [overlayLinesTinted], which is what makes the two-line
// law one law rather than five: the label, the dim tail under it, the selection
// band across both, and the pointer's lead are decided in one place for every
// list on this surface (palette.go).
func (a *app) homePhoneRow(line homeLine, at, width int, pal palette) []string {
	h := &a.home
	switch line.kind {
	case homeBlank:
		return []string{""}
	case homePhoneSection:
		// A HEADING IS ONE DIM WORD. No count beside it and no rule under it —
		// the rows are directly below and they carry their own facts, and a
		// section that spent a second row on decoration would be a section
		// costing more than the thing it names.
		return []string{" " + pal.dim(fit(line.project, width-1))}
	case homeHeading:
		// THE HEADING IS THE PROJECT'S NAME AND NOTHING ELSE, exactly as it is on
		// a wide frame (home.go's [app.homeLine]): a tap opens any project's
		// conversation now, so there is no door to mark as shut.
		return []string{" " + pal.dim(fit(line.project, width-1))}
	}
	label, note, tint := a.homePhoneWords(line, pal)
	// THE CONVERSATION THIS TERMINAL IS IN KEEPS ITS MARK ON A PHONE TOO. It is
	// the chosen thing, it wears THE GROUND LADDER's selected step at both wider
	// tiers (home.go's [app.homeLine]), and a tier that dropped it would be the
	// one screen where "which of these am I in" had no answer at all. The flag
	// was hard `false` here from the day this tier was written, back when the
	// mark was an accent label rather than a rung.
	//
	// The pointer's flag stays `false` and that is the tier's own law rather than
	// an oversight: a phone has no pointer to shadow a row with.
	//
	// The wide tiers retired the band for home's own conversation ([markHere] —
	// a dashboard's grounds belong to the hand), and this tier deliberately did
	// not follow: a phone's inbox is one cramped stacked column whose `here`
	// tail is the first thing truncation takes, so the band is the one mark
	// that always survives the width.
	marked := line.kind == homeSession && a.homeMark(line.row) == markHere
	return overlayLinesTinted(label, note, tint, at == h.cursor, marked, false, width, pal)
}

// homePhoneWords is what one row SAYS: its label, its dim tail, and how that
// tail is painted.
//
// It is a function of its own because the wide column draws a row and this tier
// draws a PAIR of them ([app.homeLine] returns one string), and the two must
// never disagree about what the row is called — so the words are named once,
// here, and the two shapes both ask for them.
func (a *app) homePhoneWords(line homeLine, pal palette) (string, string, noteInk) {
	h := &a.home
	switch line.kind {
	case homeSession:
		return homeGlyph(line.row, pal.ascii) + " " + homeName(line.row),
			// AND THE PHONE KEEPS THE SHORT WORD, though it is the one tier with
			// no card to say the door on. Its tail is a SECOND LINE under the
			// name ([overlayLinesTinted]), so a note that grew on the selected
			// row would push that row from one line to two and shove the whole
			// column under it down a row on every press of `↓` — a list that
			// moves under the cursor is the one thing a stacked column may not
			// do. The foot is where this tier tells the door (takeover.go).
			homeNote(line.row, a.homeHeld(line.row), "", a.takeoverRowWord(line.row), a.homeMark(line.row),
				a.homeRowGone(line.row), a.homeFresh(line.row), h.world.Read),
			homeNoteInk(line.row, a.homeHeld(line.row) || a.homeRowGone(line.row))
	case homeItem:
		return standGlyph(line.view.Item, line.view.Running, line.view.News, pal.ascii) +
				" " + strings.TrimSpace(line.view.Item.Words),
			standRollup(line.view, h.world.Read), standRowInk(line.view)
	case homeExchangeRow:
		return homeAskHereGlyph + " " + exchangeTitle(line.ex.spoke),
			// bridge lane: the one-spinner law is the wide frame's (homespinner.go).
			// THE INBOX IS ITS OWN SURFACE and keeps the tail it already drew — its
			// rows are twelve on a phone, not a column of live things beside two
			// others, and the law it does keep is homephone.go's own second one.
			a.exchangeTail(line.ex, true), exchangeTailInk(line.ex)
	case homePhoneNews:
		note := line.note
		if note == nil {
			return "", "", nil
		}
		glyph := standNewsGlyph
		if pal.ascii {
			glyph = standNewsASCII
		}
		return glyph + " " + note.words, joinDot(sinceAt(note.at, h.world.Read), note.project), nil
	case homePhoneMore, homeQuiet, homeItemFold:
		return homeFoldMark(line.folded, pal) + " " + homePhoneFoldWord(line, h.world.Read), "", nil
	case homeProject:
		return homeFoldMark(line.folded, pal) + " " + line.project,
			h.projectNote(line.proj, h.world.Read, pal.ascii), h.projectInk(line.proj)
	case homeAskHere:
		label := homeAskHereWord
		if text := strings.TrimSpace(h.box.String()); text != "" {
			label += ": " + text
		}
		return homeAskHereGlyph + " " + label, "", nil
	case homeAction:
		label := homeStartWord
		if text := strings.TrimSpace(h.box.String()); text != "" {
			// A COMMAND IS SAID THE SAME WAY IN BOTH COLUMNS. Enter dispatches a
			// "/" line here exactly as it does on a wide frame ([app.homeEnter] is
			// the one router), so the clause comes from the one place it is
			// spelled ([homeView.runLabel]) rather than being written again narrower.
			if word := h.runLabel(text); word != "" {
				return homeStartGlyph + " " + word, "", nil
			}
			label += ": " + text
		}
		return homeStartGlyph + " " + label, "", nil
	}
	return "", "", nil
}

// homePhoneFoldWord is what a fold line of any of the four kinds says. They
// share one spelling because they are one gesture over one kind of thing — a
// line standing for rows you cannot see ([bandFoldWord] carries the original).
func homePhoneFoldWord(line homeLine, now time.Time) string {
	switch line.kind {
	case homeQuiet:
		return homeQuietWord(line, now)
	case homeItemFold:
		return standFoldWord(line.quiet, line.folded)
	}
	if !line.folded {
		return "…" + itoa(line.quiet) + " fewer"
	}
	return "…" + itoa(line.quiet) + " more"
}

// ── the action bar ──────────────────────────────────────────────────────────

// homeBarTarget is one of the bar's targets: what it says, and what pressing it
// does.
type homeBarTarget struct {
	word string
	do   func(a *app) tea.Cmd
}

// homeBar draws the bar and records where its targets landed.
//
// IT IS THE LEGEND, TURNED INTO SOMETHING YOU CAN HIT. The line under the box
// has always been a sentence naming keys — `↑↓ move · enter open · esc close` —
// which on a phone names three things a person does not have and offers nothing
// they do. So at this tier it becomes at most three wide targets, drawn in the
// answer bands' shape one rung down: dim rather than in the question hue,
// because a bar is always there and an answer is a thing being asked.
//
// UNDER [homePhoneBarFloor] IT IS THE PLAIN SENTENCE AGAIN. Three targets at
// seven cells each is three targets that miss, and a hint that truncates says
// more than a row of stubs.
func (a *app) homeBar(width int, targets []homeBarTarget, pal palette) string {
	a.home.bar = nil
	if width < homePhoneBarFloor || len(targets) == 0 {
		word := a.homeHint()
		if a.home.msg != "" {
			// A refusal or a report is a sentence, not a hint, and it is not
			// painted like one.
			return " " + pal.dim(fit(a.home.msg, width-2))
		}
		return " " + paintHint(fit(word, width-2), pal, pal.dim)
	}
	// A REFUSAL OUTRANKS THE BAR. Every refusal on this screen is a fact about
	// a door somebody just tried, and a row of targets drawn over the top of it
	// would be the screen answering a question nobody asked instead of the one
	// they did.
	if a.home.msg != "" {
		return " " + pal.dim(a.pathLink(a.home.msgPath, fit(a.home.msg, width-2)))
	}
	words := make([]string, 0, len(targets))
	for _, target := range targets {
		words = append(words, target.word)
	}
	line, spans := phoneBar(width, words, pal)
	a.home.bar = spans
	return line
}

// phoneBar draws the bar itself and answers where its targets landed.
//
// IT IS A FUNCTION AND NOT A METHOD because two surfaces draw one: home's foot
// and the task record's (taskphone.go). A second implementation of "three wide
// targets across a phone frame" would be a second set of edges for a thumb to
// find, and the whole point of a bar is that it is in the same place every time.
//
// The targets ABUT. There is no gap between two of them, because a gap is a
// place a press can land and mean nothing — and on a screen a person is holding
// in one hand, "nothing happened" is indistinguishable from "it is broken".
func phoneBar(width int, words []string, pal palette) (string, []hudSpan) {
	// EACH TARGET KEEPS ITS OWN WORD WHOLE and the room left over is shared out
	// evenly. An even split is what a bar of equal words wants and it is wrong
	// the moment they are not equal — `‹ back` and `m puts it in your message`
	// on one row would give the short word half the frame and cut the long one
	// mid-sentence, which is the one thing a target may not do: a person cannot
	// press a verb they cannot read.
	want := make([]int, len(words))
	total := 0
	for i, word := range words {
		want[i] = ansi.StringWidth(word) + 2
		total += want[i]
	}
	spare, share := 0, 0
	if total < width {
		spare = width - total
		share = spare / len(words)
	}
	var out strings.Builder
	spans := make([]hudSpan, 0, len(words))
	at := 0
	for i, word := range words {
		room := want[i] + share
		if i == len(words)-1 {
			room = width - at
		}
		if room < 1 {
			room = 1
		}
		spans = append(spans, hudSpan{from: at, to: at + room})
		shown := fit(word, room-1)
		pad := room - 1 - ansi.StringWidth(shown)
		if pad < 0 {
			pad = 0
		}
		out.WriteString(pal.dim(" " + shown + strings.Repeat(" ", pad)))
		at += room
	}
	return out.String(), spans
}

// homeInboxBar is the bar over the list: open what the cursor is on, start
// something new, ask the box here.
func (a *app) homeInboxBar() []homeBarTarget {
	return []homeBarTarget{
		{word: "open", do: func(a *app) tea.Cmd { return a.homeEnter() }},
		{word: "new", do: func(a *app) tea.Cmd { return a.homeStart(strings.TrimSpace(a.home.box.String())) }},
		{word: homeAskHereWord, do: func(a *app) tea.Cmd { return a.askHere(strings.TrimSpace(a.home.box.String())) }},
	}
}

// homeBarPress resolves a press on the bar, and reports whether it took it.
func (a *app) homeBarPress(x, y int, targets []homeBarTarget) (tea.Cmd, bool) {
	if y != a.home.barRow || len(a.home.bar) == 0 {
		return nil, false
	}
	for i, span := range a.home.bar {
		if span.holds(x) && i < len(targets) {
			a.touch()
			return targets[i].do(a), true
		}
	}
	return nil, false
}

// ── the gestures ────────────────────────────────────────────────────────────

// homePhonePress resolves a tap on the inbox.
//
// THERE IS NO HOVER ON GLASS, so there is no two-step either: a press SELECTS
// AND OPENS in one gesture. The wide screen's two-step exists because a card
// beside the list previews what a second press would open — there is no second
// column here to preview into, and a person who has to tap a row twice to see
// it has been charged for a preview they never got.
func (a *app) homePhonePress(x, y int) tea.Cmd {
	defer a.sweepExchanges()
	if a.homeSheetShowing() {
		return a.homeSheetPress(x, y)
	}
	// THE FRAME IS LAID OUT BEFORE IT IS READ, which is [app.consentPress]'s law
	// and load-bearing for the same reason: drawing the bar is what writes its
	// spans, and reading them first would be reading where the targets were on
	// the frame before this one.
	width, height := a.size()
	_, hits, _, _ := a.homePhoneFrame(width, height)
	if cmd, took := a.homeBarPress(x, y, a.homeInboxBar()); took {
		return cmd
	}
	if y < 0 || y >= len(hits) {
		return nil
	}
	at := hits[y]
	if at < 0 || at >= len(a.home.lines) {
		return nil
	}
	line := a.home.lines[at]
	a.homeTakeList()
	a.home.say("", "")
	// A TAP ON A HEADING FOLDS THE SECTION. It is not a cursor stop, so this is
	// resolved before the stop test below rather than after it — the pointer can
	// reach a row the keyboard has no reason to walk onto.
	if line.kind == homePhoneSection {
		a.home.foldSection(line.dir)
		a.touch()
		return nil
	}
	if !line.stop() {
		return nil
	}
	a.home.cursor = at
	if line.kind != homeAction {
		a.home.picked = true
	}
	a.touch()
	return a.homeEnter()
}

// foldSection opens or folds one of the three triage sections, and leaves the
// cursor where it was so the gesture can be reversed without moving.
func (h *homeView) foldSection(key string) {
	if h.sections == nil {
		h.sections = map[string]bool{}
	}
	if h.sections[key] {
		delete(h.sections, key)
	} else {
		h.sections[key] = true
	}
	held := h.cursor
	h.rebuild()
	h.cursor = h.clamp(held)
}

// homePhoneEnter is what enter and a tap MEAN at this tier, and it reports
// whether it took the keystroke.
//
// A ROW OPENS ITS CARD AS A SHEET, and that is the whole of the change: there
// is no second column for a card to be drawn in, so the card takes the frame
// (homesheet.go). Everything that was a FOLD stays a fold — a tap on `▸ …4 more`
// opens it in place exactly as enter always did — because a fold is not a thing
// with a card, and the two action rows keep their own meanings for the same
// reason.
func (a *app) homePhoneEnter() (tea.Cmd, bool) {
	if !a.homePhone() || a.homeSheetShowing() {
		return nil, false
	}
	line, ok := a.home.focusedLine()
	if !ok {
		return nil, false
	}
	switch line.kind {
	case homePhoneMore:
		a.home.foldSection(line.dir)
		a.touch()
		return nil, true
	case homeSession, homeItem, homePhoneNews, homeExchangeRow:
		a.openHomeSheet()
		return nil, true
	}
	return nil, false
}
