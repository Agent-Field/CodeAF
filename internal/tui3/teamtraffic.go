package tui3

import (
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── TRAFFIC: WHAT PASSES BETWEEN A TEAM'S MANAGER AND ITS MEMBERS ───────────
//
// The Traffic log (internal/teams' traffic.go) is the only channel between this
// interface and the team tools a model calls (internal/session): the manager's
// notes and directives, the members' posts, and the two acts the manager asks
// for, stop and start. This file is the interface's side of it, and it has three
// halves.
//
// THE READ IS OFF THE LOOP. A clock of its own ([trafficEvery]) runs while this
// window holds a conversation in any team, and on each turn it pages each such
// team's log forward from a cursor, in a command beside the door line
// ([app.besideLine]): nobody pressed for it, and nothing a person does waits on
// it. What came back is folded into a cache on the loop, and THE FRAME DRAWS THE
// CACHE AND NOTHING ELSE (framedisk_law_test.go). The same read picks up the
// teams file when another process changed it, and gives a member its title
// when it joined before it had one ([teamstore.DeriveHandle] through the
// store's tidy), so the strip names the manager another window made.
//
// THE ACTS ARE DONE ONCE, AND ONLY NEW ONES. A stop addressed to a
// conversation this window holds ends that conversation's current turn, as the
// person's own stop does; a start is done by the window that holds the team's
// manager, which opens a conversation in the team's folder, puts it in the team
// under the handle the manager chose, and sends it the brief marked as the
// manager's. Each is done once per entry id, remembered in memory, and a window
// opening reads the log from its TAIL, so what was asked before it opened is
// history on its rail and is never done again.
//
// THE RAIL IS WHERE A PERSON READS IT. While the manager is the conversation in
// front, the frame gives the right of the body to a column of the team's
// traffic, newest at the bottom: `@parser → @web  the lexer is in`, `◆ → @web`,
// and the events, a member stopped, started or joined. Each row is a door to
// the member it is about. On a frame too narrow to lend the column, the column
// is a two-cell edge with the manager's mark in it, and pressing it lays the
// traffic over the body instead, until a row is chosen or the edge is pressed
// again.

const (
	// trafficEvery is how often the log is read while this window holds a
	// conversation in a team.
	trafficEvery = time.Second
	// trafficKeep is how many entries a team's cache holds, and how many a
	// first look reads from the tail.
	trafficKeep = 200
	// trafficPage is the most one read pages forward; a longer backlog is read
	// on the turns after.
	trafficPage = 200
	// trafficFromStart is the cursor for a log that was empty at the first
	// look: every entry after it is new.
	trafficFromStart = "000000000000"
	// The column: it stands on a frame at least trafficFloor wide after the
	// task column, takes three tenths of that between trafficColsMin and
	// trafficColsMax, and never leaves the conversation under trafficBodyFloor.
	trafficFloor     = 110
	trafficColsMin   = 28
	trafficColsMax   = 44
	trafficBodyFloor = 56
	// trafficGripCols is the narrow frame's edge, the task column's own
	// width for its closed edge (task.go's [railGripCols]).
	trafficGripCols = 2
)

// trafficState is the cache and the clock.
type trafficState struct {
	// ticking says a trafficTickMsg is on its way, and reading that a read is
	// out, so a turn of the clock never starts a second one beside it.
	ticking bool
	reading bool
	// cursor is each team's last entry id read, by team id; a team with none
	// has not been looked at yet, and its first read is the tail.
	cursor map[string]string
	// rows is each team's cached entries, oldest first, at most trafficKeep.
	rows map[string][]teamstore.Entry
	// done is every stop and start acted on, by team id and entry id.
	done map[string]bool
	// stamp is the teams file's time when it was last read here, and edits
	// counts this window's own writes, so a read that crossed one does not
	// put the list from before it back.
	stamp time.Time
	edits int
	// over says the traffic is laid over the body on a narrow frame.
	over bool
	// lines is the last frame's rows, top to bottom, as the member a press on
	// each goes to ("" for none); left and width are the columns they were
	// drawn in, and grip says the frame drew the narrow edge instead.
	lines []string
	left  int
	width int
	grip  bool
}

// trafficTickMsg is the Traffic clock.
type trafficTickMsg struct{}

// trafficJob is one team's read: after the cursor, or the tail on a first look.
type trafficJob struct {
	id    string
	after string
	first bool
}

// trafficGot is what one team's read found.
type trafficGot struct {
	trafficJob
	entries []teamstore.Entry
	err     error
}

// trafficTitle is a member that joined with no title and has one now.
type trafficTitle struct{ team, key, word string }

// ── THE CLOCK AND THE READ ──────────────────────────────────────────────────

// trafficHeld reports whether this window holds key: in front or behind.
func (a *app) trafficHeld(key string) bool {
	return key != "" && (key == a.frontTabKey() || a.behind[key] != nil)
}

// trafficWanted reports whether any team holds a conversation this window
// holds, which is when the log is worth reading. It allocates nothing: it is
// asked after every message.
func (a *app) trafficWanted() bool {
	if !a.wall.loaded || len(a.wall.teams) == 0 {
		return false
	}
	for _, t := range a.wall.teams {
		for _, m := range t.Members {
			if a.trafficHeld(m.Key) {
				return true
			}
		}
	}
	return false
}

// trafficArm starts the clock when there is something to read and it is not
// already turning. It is asked after every message ([app.Update]), so a team
// made, a member joined or a manager opened starts it without each of those
// doors having to know.
func (a *app) trafficArm() tea.Cmd {
	if a.traffic.ticking || !a.trafficWanted() {
		return nil
	}
	a.traffic.ticking = true
	return tea.Batch(a.trafficRead(), a.trafficNext())
}

// trafficNext is the clock's next turn.
func (a *app) trafficNext() tea.Cmd {
	return surfaceTick(trafficEvery, func(time.Time) tea.Msg { return trafficTickMsg{} })
}

// trafficTick is a turn of the clock: read, and come round again, while there
// is anything to read. It stops itself when there is not, and [app.trafficArm]
// starts it again.
func (a *app) trafficTick() tea.Cmd {
	if !a.trafficWanted() {
		a.traffic.ticking = false
		return nil
	}
	return tea.Batch(a.trafficRead(), a.trafficNext())
}

// trafficRead reads every team this window holds a conversation in, off the
// loop, and folds what it found in ([app.trafficTake]).
func (a *app) trafficRead() tea.Cmd {
	if a.traffic.reading || !a.wall.loaded {
		return nil
	}
	var jobs []trafficJob
	var titles []trafficTitle
	for _, t := range a.wall.teams {
		held := false
		for _, m := range t.Members {
			if a.trafficHeld(m.Key) {
				held = true
			}
			if strings.TrimSpace(m.Word) != "" {
				continue
			}
			for _, tab := range a.chatTabs {
				if tab.key == m.Key && !tab.start && !tab.work && strings.TrimSpace(tab.word) != "" {
					titles = append(titles, trafficTitle{team: t.ID, key: m.Key, word: tab.word})
					break
				}
			}
		}
		if !held {
			continue
		}
		after, seen := a.traffic.cursor[t.ID]
		jobs = append(jobs, trafficJob{id: t.ID, after: after, first: !seen})
	}
	if len(jobs) == 0 && len(titles) == 0 {
		return nil
	}
	a.traffic.reading = true
	dir, reserved, stamp, edits := a.profileDir, teamReservedHues(a.pal), a.traffic.stamp, a.traffic.edits
	return a.besideLine(func() func(bool) tea.Cmd {
		got := make([]trafficGot, len(jobs))
		for i, j := range jobs {
			after, limit := j.after, trafficPage
			if j.first {
				after, limit = "", trafficKeep
			}
			entries, err := teamstore.ReadTraffic(dir, j.id, after, limit)
			got[i] = trafficGot{trafficJob: j, entries: entries, err: err}
		}
		titled := false
		if len(titles) > 0 {
			err := teamstore.Update(dir, func(f *teamstore.File) error {
				for _, want := range titles {
					i := teamstore.Index(f.Teams, want.team)
					if i < 0 {
						continue
					}
					for j := range f.Teams[i].Members {
						if m := &f.Teams[i].Members[j]; m.Key == want.key && strings.TrimSpace(m.Word) == "" {
							m.Word = want.word
						}
					}
				}
				return nil
			})
			titled = err == nil
		}
		var fresh []team
		var at time.Time
		if info, err := os.Stat(teamstore.Path(dir)); err == nil {
			at = info.ModTime()
		}
		if titled || !at.Equal(stamp) {
			if f, err := teamstore.LoadHued(dir, reserved); err == nil {
				fresh = f.Teams
			}
		}
		return func(bool) tea.Cmd { return a.trafficTake(got, fresh, at, edits) }
	})
}

// trafficTake folds one read in, on the loop: the teams file when it changed
// and nothing was written here since the read began, each team's new entries
// onto its cache, and the stops and starts among them done.
func (a *app) trafficTake(got []trafficGot, fresh []team, at time.Time, edits int) tea.Cmd {
	a.traffic.reading = false
	if edits == a.traffic.edits {
		if fresh != nil {
			a.teamAdopt(teamsClone(fresh))
			a.touch()
		}
		a.traffic.stamp = at
	}
	if a.traffic.cursor == nil {
		a.traffic.cursor, a.traffic.rows, a.traffic.done = map[string]string{}, map[string][]teamstore.Entry{}, map[string]bool{}
	}
	var acts []tea.Cmd
	for _, g := range got {
		if g.err != nil {
			continue
		}
		if cur, seen := a.traffic.cursor[g.id]; g.first == seen || (!g.first && cur != g.after) {
			// Another read got here first; this one's view of the cursor is
			// stale, and taking it would add its entries twice.
			continue
		}
		if len(g.entries) == 0 {
			if g.first {
				a.traffic.cursor[g.id] = trafficFromStart
			}
			continue
		}
		a.traffic.cursor[g.id] = g.entries[len(g.entries)-1].ID
		rows := append(a.traffic.rows[g.id], g.entries...)
		if len(rows) > trafficKeep {
			rows = append([]teamstore.Entry(nil), rows[len(rows)-trafficKeep:]...)
		}
		a.traffic.rows[g.id] = rows
		a.touch()
		if g.first {
			// A first look is history: what was asked before this window
			// opened was asked of another window, or of none, and doing it now
			// would do it twice or late.
			continue
		}
		for _, e := range g.entries {
			if cmd := a.trafficAct(g.id, e); cmd != nil {
				acts = append(acts, cmd)
			}
		}
	}
	return tea.Batch(acts...)
}

// ── THE ACTS ────────────────────────────────────────────────────────────────

// trafficAct does what one new entry asks of this window, once per entry id: a
// stop for a conversation it holds, a start when it holds the team's manager.
func (a *app) trafficAct(teamID string, e teamstore.Entry) tea.Cmd {
	if e.Kind != teamstore.KindStop && e.Kind != teamstore.KindStart {
		return nil
	}
	done := teamID + "/" + e.ID
	if a.traffic.done[done] {
		return nil
	}
	t, ok := a.teamByID(teamID)
	if !ok {
		return nil
	}
	switch e.Kind {
	case teamstore.KindStop:
		key := trafficMemberKey(t, e.To, e.Member)
		if !a.trafficHeld(key) {
			return nil
		}
		a.traffic.done[done] = true
		a.trafficStop(key)
		return nil
	case teamstore.KindStart:
		if t.Manager == "" || !a.trafficHeld(t.Manager) {
			return nil
		}
		a.traffic.done[done] = true
		return a.trafficStart(t, e)
	}
	return nil
}

// trafficMemberKey is the member an entry addresses: by the conversation key
// it carries when it carries one the team holds, and otherwise by its handle.
func trafficMemberKey(t team, handle, key string) string {
	if key != "" && t.Holds(key) {
		return key
	}
	if m, ok := t.ByHandle(handle); ok {
		return m.Key
	}
	return ""
}

// trafficStop is the manager's stop for a conversation this window holds. It
// is the person's own stop and nothing more: the current turn ends, and what
// is queued behind it is left alone.
func (a *app) trafficStop(key string) {
	if key == a.frontTabKey() {
		if a.state != stateWorking {
			return
		}
		a.interruptTurn()
		a.note(a.teamManagerMark() + " the manager stopped this turn")
		return
	}
	if held := a.behind[key]; held != nil && held.conv.Agent != nil {
		held.conv.Agent.Interrupt()
	}
}

// trafficStart is the manager's start: a new conversation in the team's
// folder, in the team under the handle the manager chose, sent the brief as
// its first message, marked as the manager's rather than the person's. It comes
// to the front, which is where a person who approved a start looks for it.
func (a *app) trafficStart(t team, e teamstore.Entry) tea.Cmd {
	// A conversation started while a team is shown joins that team
	// ([app.teamJoinFront]); this one belongs to t whatever is shown, so no
	// other team is shown while it starts.
	shown := a.wall.activeID
	if shown != t.ID {
		a.wall.activeID = ""
	}
	cmd, refusal := a.teamStartIn(t)
	a.wall.activeID = shown
	if refusal != "" {
		a.note(a.teamManagerMark() + " could not start @" + e.To + ": " + refusal)
		return nil
	}
	m := teamMember{Key: a.convKey(a.file), File: a.file, Where: a.workspace, Handle: e.To}
	if err := a.teamEdit(func(f *teamstore.File) error {
		if err := f.AddMember(t.ID, m); err != nil {
			return err
		}
		if got, _ := f.Teams[teamIndex(f.Teams, t.ID)].Member(m.Key); got.Handle != e.To && teamstore.ValidHandle(e.To) == nil {
			// Joined before this edit without the handle (the team was
			// shown); the manager's name for it is the one the log uses.
			_ = f.SetHandle(t.ID, m.Key, e.To)
		}
		return nil
	}); err != nil {
		a.note("the new member is in " + t.Name + " for this window, but " + err.Error())
	}
	brief := strings.TrimSpace(e.Text)
	if brief == "" {
		return cmd
	}
	return tea.Batch(cmd, a.submit(a.teamManagerMark()+" from manager: "+brief))
}

// ── THE RAIL ────────────────────────────────────────────────────────────────

// trafficColsFor is the column the rail takes from room columns of frame, 0
// when it cannot stand there.
func trafficColsFor(room int) int {
	if room < trafficFloor {
		return 0
	}
	cols := min(max(room*3/10, trafficColsMin), trafficColsMax)
	if room-cols < trafficBodyFloor {
		return 0
	}
	return cols
}

// trafficWidth is what the rail costs the conversation, in columns: its column
// while the manager is in front on a frame wide enough, the narrow edge on one
// that is not, and nothing otherwise. It is asked wherever [app.bodyWidth] is,
// so it reads memory and allocates nothing.
func (a *app) trafficWidth() int {
	if _, ok := a.teamFrontManaged(); !ok {
		return 0
	}
	width, _ := a.size()
	if cols := trafficColsFor(width - a.railWidth()); cols > 0 {
		return cols
	}
	return trafficGripCols
}

// trafficOverShowing reports whether the traffic is laid over the body: asked
// for, on a frame that has only the edge to give it.
func (a *app) trafficOverShowing() bool {
	return a.traffic.over && a.trafficWidth() == trafficGripCols
}

// trafficAddr is one address of an entry as the rail spells it: `@handle`, the
// manager's mark, or the word for someone who is not a member.
func (a *app) trafficAddr(s string) string {
	switch s {
	case teamstore.FromManager:
		return a.teamManagerMark()
	case teamstore.FromYou:
		return "you"
	case teamstore.FromSystem:
		return "·"
	case teamstore.ToEveryone:
		return "all"
	case teamstore.ToRoom:
		return "room"
	case "":
		return ""
	}
	return "@" + s
}

// trafficLine is one entry as one painted row width cells wide, and the
// member a press on it goes to.
func (a *app) trafficLine(t team, e teamstore.Entry, width int) (string, string) {
	pal := a.pal
	arrow := a.linearMark("→", "->")
	if pal.ascii {
		arrow = "->"
	}
	text := strings.Join(strings.Fields(e.Text), " ")
	var head string
	target := ""
	switch e.Kind {
	case teamstore.KindStop, teamstore.KindStart:
		word := "stopped"
		if e.Kind == teamstore.KindStart {
			word = "started"
		}
		head = pal.dim(a.trafficAddr(e.From) + " " + word + " " + a.trafficAddr(e.To))
		target = trafficMemberKey(t, e.To, e.Member)
		text = ""
	case teamstore.KindEvent:
		head = pal.muted(a.trafficAddr(e.From))
		target = trafficMemberKey(t, e.From, e.Member)
		text = pal.dim(text)
	default:
		head = pal.ink(a.trafficAddr(e.From)) + pal.dim(" "+arrow+" ") + pal.ink(a.trafficAddr(e.To))
		target = trafficMemberKey(t, e.From, "")
		if target == "" {
			target = trafficMemberKey(t, e.To, e.Member)
		}
		text = pal.muted(text)
	}
	line := head
	if text != "" {
		line += "  " + text
	}
	line = fit(line, width)
	return line + strings.Repeat(" ", max(width-ansi.StringWidth(line), 0)), target
}

// trafficBody is the rail's rows for team t, height tall and width wide: the
// team's name at the top, and the newest entries at the bottom, as many as fit.
// It records where each row goes on a press. Frame-safe: the cache only.
func (a *app) trafficBody(t team, height, width int) []string {
	out := make([]string, height)
	a.traffic.lines = make([]string, height)
	if height <= 0 || width <= 0 {
		return out
	}
	blank := strings.Repeat(" ", width)
	for i := range out {
		out[i] = blank
	}
	title := fit(a.pal.accent(a.teamManagerMark())+" "+a.pal.ink(t.Name)+a.pal.dim("  traffic"), width)
	out[0] = title + strings.Repeat(" ", max(width-ansi.StringWidth(title), 0))
	rows := a.traffic.rows[t.ID]
	room := height - 1
	if len(rows) > room {
		rows = rows[len(rows)-room:]
	}
	if len(rows) == 0 && height > 2 {
		quiet := fit(a.pal.dim("nothing yet"), width)
		out[height-1] = quiet + strings.Repeat(" ", max(width-ansi.StringWidth(quiet), 0))
		return out
	}
	at := height - len(rows)
	for i, e := range rows {
		line, target := a.trafficLine(t, e, width)
		if target != "" && a.hot.kind == hoverTraffic && a.hot.index == at+i {
			line = a.pal.cursor(line, width)
		}
		out[at+i], a.traffic.lines[at+i] = line, target
	}
	return out
}

// trafficRows is the rail's column beside the body, height rows exactly
// [app.trafficWidth] wide: the seam and the rows, or the narrow edge with the
// manager's mark at its middle. nil when the manager is not in front.
func (a *app) trafficRows(height int) []string {
	t, ok := a.teamFrontManaged()
	cols := a.trafficWidth()
	a.traffic.grip = false
	if !ok || cols == 0 || height <= 0 {
		a.traffic.lines, a.traffic.width = nil, 0
		return nil
	}
	width, _ := a.size()
	if cols == trafficGripCols {
		// The rows laid over the body, when they are, were placed by
		// [app.trafficOverRows] earlier in this frame and are left as they are.
		a.traffic.grip = true
		if !a.traffic.over {
			a.traffic.lines, a.traffic.width = nil, 0
		}
		blank := strings.Repeat(" ", cols)
		out := make([]string, height)
		for i := range out {
			out[i] = blank
		}
		mark := " " + a.pal.ink(a.teamManagerMark())
		if a.hot.kind == hoverTrafficGrip || a.traffic.over {
			mark = a.pal.cursor(" "+a.pal.accent(a.teamManagerMark()), cols)
		}
		out[height/2] = mark
		return out
	}
	seamW := ansi.StringWidth(railSeam)
	body := a.trafficBody(t, height, cols-seamW)
	a.traffic.left, a.traffic.width = width-cols+seamW, cols-seamW
	seam := a.pal.dim(railSeam)
	for i := range body {
		body[i] = seam + body[i]
	}
	return body
}

// trafficBeside joins the rail's column onto the task column's rows for the
// body region, so [app.railJoin] lays both beside the conversation: the task
// column's row padded to its own width, then the traffic's. With no traffic
// column it hands the task column back as it was.
func (a *app) trafficBeside(rail []string, height int) []string {
	traffic := a.trafficRows(height)
	if traffic == nil {
		return rail
	}
	cols := a.railWidth()
	out := make([]string, height)
	for i := range out {
		task := ""
		if i < len(rail) {
			task = rail[i]
		}
		if cols > 0 {
			if w := ansi.StringWidth(task); w < cols {
				task += strings.Repeat(" ", cols-w)
			}
		}
		out[i] = task + traffic[i]
	}
	return out
}

// trafficOverRows is the traffic laid over the body on a narrow frame, height
// rows at the body's width.
func (a *app) trafficOverRows(height int) []string {
	t, ok := a.teamFrontManaged()
	if !ok {
		return nil
	}
	width := a.bodyWidth()
	pad := ansi.StringWidth(railSeam)
	body := a.trafficBody(t, height, width-pad)
	a.traffic.left, a.traffic.width = pad, width-pad
	for i := range body {
		body[i] = strings.Repeat(" ", pad) + body[i]
	}
	return body
}

// ── THE POINTER ─────────────────────────────────────────────────────────────

// trafficRowAt is the rail's row under the pointer on the last frame, -1 for
// none, and whether the pointer is over the rail's cells at all.
func (a *app) trafficRowAt(x, y int) (int, bool) {
	if a.trafficWidth() == 0 || a.traffic.width <= 0 {
		return -1, false
	}
	top := a.bodyTop()
	if top < 0 || y < top || y >= top+a.viewHeight() {
		return -1, false
	}
	if a.traffic.grip && !a.trafficOverShowing() {
		return -1, false
	}
	left := a.traffic.left - ansi.StringWidth(railSeam)
	if x < left || x >= a.traffic.left+a.traffic.width {
		return -1, false
	}
	if i := y - top; i < len(a.traffic.lines) && a.traffic.lines[i] != "" {
		return i, true
	}
	return -1, true
}

// trafficGripAt reports whether the pointer is on the narrow frame's edge.
func (a *app) trafficGripAt(x, y int) bool {
	if a.trafficWidth() != trafficGripCols {
		return false
	}
	width, _ := a.size()
	top := a.bodyTop()
	return top >= 0 && y >= top && y < top+a.viewHeight() && x >= width-trafficGripCols && x < width
}

// trafficHoverAt is the hover the rail answers with.
func (a *app) trafficHoverAt(x, y int) (hoverAt, bool) {
	if a.trafficGripAt(x, y) {
		return hoverAt{kind: hoverTrafficGrip}, true
	}
	if row, ok := a.trafficRowAt(x, y); ok {
		if row < 0 {
			return hoverAt{}, true
		}
		return hoverAt{kind: hoverTraffic, index: row}, true
	}
	return hoverAt{}, false
}

// trafficPress answers a press on the rail and reports whether it took it. A
// row goes to its member; the edge on a narrow frame lays the traffic over the
// body or takes it away; the rail's other cells are furniture and take the press
// to do nothing.
func (a *app) trafficPress(x, y int) (tea.Cmd, bool) {
	if a.trafficGripAt(x, y) {
		a.traffic.over = !a.traffic.over
		a.touch()
		return nil, true
	}
	row, ok := a.trafficRowAt(x, y)
	if !ok {
		return nil, false
	}
	if row < 0 {
		return nil, true
	}
	a.traffic.over = false
	return a.trafficGo(a.traffic.lines[row]), true
}

// trafficGo switches to member key: its tab when the strip has one, and
// otherwise what the team kept of it, which is enough to open it again.
func (a *app) trafficGo(key string) tea.Cmd {
	if key == "" || key == a.frontTabKey() {
		return nil
	}
	for _, tab := range a.tabList() {
		if tab.key == key {
			return a.tabGo(tab)
		}
	}
	for _, t := range a.wall.teams {
		if m, ok := t.Member(key); ok && m.File != "" {
			return a.tabGo(chatTab{key: m.Key, file: m.File, where: m.Where, word: m.Word, full: m.Word})
		}
	}
	return nil
}

// trafficHint is the composer's placeholder while the manager is in front: the
// person's words go to the manager, and the box says so.
func (a *app) trafficHint() string {
	if _, ok := a.teamFrontManaged(); !ok {
		return ""
	}
	return "to " + a.teamManagerMark() + " manager"
}
