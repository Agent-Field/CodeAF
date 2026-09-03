package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── A JOB'S PAGE IS NOT A ROOM ──────────────────────────────────────────────
//
// THE LAW THIS FILE EXISTS FOR: a background job has no agent inside it and no
// transcript, so a page that wears a chat header and a composer is the wrong
// shape — every word of the refusal that used to sit in that composer
// (`this log grows as the job works — say it to main`) was true, and the layout
// should have been doing that work, not a sentence.
//
// SO THIS IS A CARD, not a room. The task record card is the host: a full-frame
// page over a list, a head and a foot of its own, and nothing typed into
// (taskrecord.go, placeTasks.ownFrame). A room is a chat pointed at a node. A
// job has nobody to talk to. Matching the card rather than inventing a third
// frame means esc, scroll and the foot's grammar are already a language a
// person knows, and there is no composer underneath to apologise for.
//
// The column's section (jobsection.go) is where a person presses enter;
// [app.showJobPage] records the id; [app.jobPageArm] starts the log's reading;
// this file draws whatever that id names.

// The words the page says. Each is quoted in the manual exactly as it is
// spelled here.
const (
	// jobPageBackWord is the way out, in the head's right corner. It is the
	// card's own spelling ([taskCardBackWord]): the list is underneath, and a
	// page that promised to close would be lying about the next keystroke.
	jobPageBackWord = taskCardBackWord
	// jobPageKeysOver is the foot on a job that has ended: the scroll, the path,
	// the mention, and the way out. It matches [taskCardKeys]'s spelling, its
	// separator and its ORDER exactly — the way out last, because [hintFit]
	// keeps the final clause to the last cell there is and spends the ones in
	// front of it first — and adds the one verb a job has that a record card
	// does not, copying the log's path.
	jobPageKeysOver = "↑↓ scroll · c copy path · m puts it in your message · " + jobPageBackWord
	// jobPageKeysRun is that foot while the process is still going. `x stop it`
	// is [stopRaiseKey] and [stopActWord] in this surface's legend grammar; a
	// job that has already ended draws [jobPageKeysOver] instead, rather than a
	// key that would refuse. It is the FIRST clause of the four because stopping
	// something that is still running is the one thing on this page a person
	// cannot recover by any other route — and it is the last of them to be
	// dropped before the way out.
	jobPageKeysRun = "x stop it · ↑↓ scroll · c copy path · m puts it in your message · " + jobPageBackWord
	// jobPageHostedWord is what the body says when the log is on the ENGINE's
	// machine. An empty body would read as a job that has not written yet; the
	// file is sitting perfectly well on the other disk.
	jobPageHostedWord = "its log is on "
	// jobPageCopiedWord is the confirmation [app.homeCopyPath] already shows,
	// reused so a path copied from here and a path copied from home are the
	// same sentence.
	jobPageCopiedWord = "copied "
	// jobPageHandleLead is the handle a person says out loud — `job 3` — the
	// same spelling session's jobRowLead uses and the same number `jobs kill`
	// takes. It is drawn beside the name, never as it ([session.JobNotice.Label]
	// says why).
	jobPageHandleLead = "job "
	// jobPageMentionLines is how many of the tail `m` drops into the draft. A
	// person reaching for "this build failed, why?" wants the ending, not the
	// whole file; the page already scrolls for the rest.
	jobPageMentionLines = 8
)

// jobPageFoot is what the page spends on its own foot: a rule, the path, and
// the keys. The path is here and not in the body because the body is the log
// and the path is how to leave with it.
const jobPageFoot = 3

// jobPageHit is what one row of the page answers to a click. It is the card's
// own two-edge bargain ([taskCardHit]): the head and the foot are the way
// back, and everything between them is read.
type jobPageHit uint8

const (
	jobPageHitNone jobPageHit = iota
	jobPageHitHead
	jobPageHitFoot
)

func (h jobPageHit) back() bool { return h == jobPageHitHead || h == jobPageHitFoot }

// ── the page's own reading ──────────────────────────────────────────────────
//
// WHICH JOB IS OPEN lives on the window (jobstate.go), and so does this: the
// reading, the scroll and the beat are one page's state, and one window has at
// most one page open at a time.
//
// IT IS A FIELD AND NOT A MAP KEYED ON THE WINDOW. It was a map while this file
// and [app]'s own fields belonged to different lanes, and that shape has a leak
// written into it — an entry is dropped only by a later call that finds the page
// closed, so a window torn down while its page was open stays reachable from a
// package-level map for the life of the process. A field goes when the window
// does, which is what it was always describing.

type jobDraw struct {
	id   int
	gen  int
	log  []string
	last bool
	top  int
	path string
}

// jobView is this window's page state while a job's page is open, or nil.
//
// IT MINTS THE STATE ON FIRST SIGHT AND REPLACES IT WHEN THE PAGE MOVES to
// another job, so a scroll position from the page somebody just left cannot be
// read as a scroll position into the log they just opened.
func (a *app) jobView() *jobDraw {
	if !a.jobPageOpen() {
		a.jobDraw = nil
		return nil
	}
	view := a.jobDraw
	if view == nil || view.id != a.jobPage {
		view = &jobDraw{id: a.jobPage}
		a.jobDraw = view
	}
	if view.gen == 0 {
		// THE COLUMN'S ENTER ONLY RECORDS THE ID (jobsection.go, margin.go). The
		// first paint has to have the tail or the page is a header over a blank,
		// which is the emptiness law broken the same way the room used to break
		// it. The beat that follows still wants [app.jobPageArm] returned from
		// that enter; this is the one reading the layout cannot wait for.
		a.jobPageSeed(view)
	}
	return view
}

// jobPageSeed is the first bounded reading, taken once, so a page opened
// without [app.jobPageArm] is still a page with its log on it.
func (a *app) jobPageSeed(view *jobDraw) {
	if view.gen != 0 {
		return
	}
	job := a.jobPageJob()
	if job == nil {
		return
	}
	view.gen = 1
	view.path = a.jobPagePath(job)
	if view.path != "" {
		view.log = readJobLogTail(view.path)
	}
}

// openJobPage is the door the column takes: record the id and start the log's
// first reading. [app.showJobPage] is the other lane's and returns only whether
// the id was known.
func (a *app) openJobPage(id int) tea.Cmd {
	if !a.showJobPage(id) {
		return nil
	}
	a.touch()
	return a.jobPageArm()
}

// ── the keyboard ────────────────────────────────────────────────────────────

// jobPageKeyPress is this page's claim on the keyboard, asked from
// [app.roomKey] because that is the rung the update loop already uses for a
// full-frame overlay a person is standing in. It reports whether it took the
// key. ctrl+c and the questions above it stay the door.
func (a *app) jobPageKeyPress(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.jobPageOpen() {
		return nil, false
	}
	switch key := msg.String(); {
	case key == "ctrl+c", a.asking(), a.awaitingTask(),
		a.at(pageSettings), a.at(pageTasks), a.at(pageHome), a.deckShowing(), a.pick.open,
		a.roster.open, a.copy.on, a.welcome.open, a.menu.open, a.comp.open:
		return nil, false
	}
	return a.jobPageKey(msg.String()), true
}

// jobPageKey routes one keypress while the page is up. It is the card's own
// map ([app.taskCardKey]): esc is one layer, the letters that are verbs on this
// page, and the scroll the card already taught.
func (a *app) jobPageKey(key string) tea.Cmd {
	job := a.jobPageJob()
	if job == nil {
		a.closeJobPage()
		return nil
	}
	switch key {
	case "esc", "left":
		a.closeJobPage()
	case "m":
		a.jobPagePutInMessage(*job)
		a.closeJobPage()
	case "c":
		return a.jobPageCopyPath(*job)
	case "x":
		// THE DRAFT UNDERNEATH MAY STILL HOLD A SENTENCE, so [app.stopKey] will
		// not take `x` ([app.chordsStandDown]). This page has no box; the key
		// is the verb the foot names, and only while the job is still going.
		if !job.Over() {
			a.raiseStop(stopJobTarget(job))
		}
	case "up", "k", "ctrl+p":
		a.jobPageScroll(-1)
	case "down", "j", "ctrl+n":
		a.jobPageScroll(1)
	case "pgup", "ctrl+b":
		a.jobPageScroll(-a.jobPagePage())
	case "pgdown", "ctrl+f", " ", "space":
		a.jobPageScroll(a.jobPagePage())
	case "home", "g":
		if view := a.jobView(); view != nil {
			view.top = 0
		}
	case "end", "G":
		if view := a.jobView(); view != nil {
			view.top = 1 << 20
		}
	}
	return nil
}

// jobPagePage is a screenful of the page, one row shy so a page turn keeps a
// line of context — the same courtesy [app.taskCardPage] pays the record card.
func (a *app) jobPagePage() int {
	_, height := a.size()
	if page := height - jobPageFoot - 4; page > 1 {
		return page
	}
	return 1
}

func (a *app) jobPageScroll(delta int) {
	view := a.jobView()
	if view == nil {
		return
	}
	view.top += delta
	if view.top < 0 {
		view.top = 0
	}
	a.touch()
}

// jobPagePress resolves a click on the page. Its edges are the way back and
// its body is read, which is [app.taskCardPress]'s own shape.
func (a *app) jobPagePress(x, y int) {
	width, height := a.size()
	_, hits, _, _ := a.jobPageFrame(width, height)
	if y < 0 || y >= len(hits) {
		return
	}
	if !hits[y].back() {
		return
	}
	a.closeJobPage()
}

// ── the three verbs ─────────────────────────────────────────────────────────

// jobPageCopyPath writes the log's path to the clipboard the way every other
// path on this surface does (copymode.go's OSC 52), and says so with the same
// confirmation home already shows ([app.homeCopyPath]).
func (a *app) jobPageCopyPath(job session.JobNotice) tea.Cmd {
	path := strings.TrimSpace(job.LogPath)
	if path == "" {
		return nil
	}
	shown := a.hostedPath(path)
	a.note(jobPageCopiedWord + shown)
	return tea.Raw(osc52(shown, a.tmux))
}

// jobPagePutInMessage drops the job into the draft the way the record card
// drops a mention ([app.mentionTask]): append, never replace, because the box
// is where a person was part-way through saying something. What it composes is
// the name, the handle, the ending, and the log's last few lines — so "this
// build failed, why?" is one keystroke instead of a copy-paste.
func (a *app) jobPagePutInMessage(job session.JobNotice) {
	log := []string(nil)
	if view := a.jobView(); view != nil {
		log = view.log
	}
	composed := jobPageCompose(job, a.jobPageClock(job), log)
	text := strings.TrimRight(string(a.input.value), " ")
	if text != "" {
		text += "\n\n"
	}
	a.input.setText(text + composed)
	a.touch()
}

// jobPageCompose is the block `m` drops in, so a test can read it without
// standing the page up. The clock is passed in because a running job's age is
// a fact of the moment it was asked, not of the notice.
func jobPageCompose(job session.JobNotice, clock string, log []string) string {
	var b strings.Builder
	if name := strings.TrimSpace(job.Label()); name != "" {
		b.WriteString(name)
		b.WriteByte('\n')
	}
	b.WriteString(jobPageHandle(job.ID))
	if ending := jobPageEnding(job, clock); ending != "" {
		b.WriteString(railSep)
		b.WriteString(ending)
	}
	start := 0
	if n := len(log); n > jobPageMentionLines {
		start = n - jobPageMentionLines
	}
	for _, line := range log[start:] {
		b.WriteByte('\n')
		b.WriteString(line)
	}
	return b.String()
}

func jobPageHandle(id int) string { return jobPageHandleLead + itoa(id) }

// ── the frame ───────────────────────────────────────────────────────────────

// jobPageFrame is the whole screen while the page is up: exactly height rows,
// what each of them answers to the pointer, and where the caret sits.
//
// It is ONE function for [app.taskCardFrame]'s reason: the frame draws these
// rows and the pointer resolves against them, and two answers to "which row is
// the foot" is a click that closes a page somebody meant to scroll.
//
// The caret is reported as (0, 0) and never moves, because nothing on this
// page is typed into.
func (a *app) jobPageFrame(width, height int) ([]string, []jobPageHit, int, int) {
	pal := a.pal
	lines := make([]string, 0, height)
	hits := make([]jobPageHit, 0, height)
	add := func(text string, hit jobPageHit) {
		lines = append(lines, text)
		hits = append(hits, hit)
	}

	job := a.jobPageJob()
	if job == nil {
		return lines, hits, 0, 0
	}

	add(a.jobPageTitle(width, *job), jobPageHitHead)
	add("", jobPageHitHead)
	add(pal.dim(rule(width)), jobPageHitNone)

	head := len(lines)
	foot := jobPageFoot
	if strings.TrimSpace(job.LogPath) == "" {
		foot = 2
	}
	room := height - head - foot
	if room < 1 {
		room = 1
	}

	body := a.jobPageBody(*job, width-2)
	view := a.jobView()
	top := 0
	if view != nil {
		view.top = clampTop(view.top, len(body), room)
		top = view.top
	} else {
		top = clampTop(0, len(body), room)
	}
	for i := 0; i < room; i++ {
		at := top + i
		if at >= len(body) {
			add("", jobPageHitNone)
			continue
		}
		add(" "+body[at], jobPageHitNone)
	}

	add(pal.dim(rule(width)), jobPageHitNone)
	if path := strings.TrimSpace(job.LogPath); path != "" {
		add(" "+pal.dim(fit(a.hostedPath(path), width-2)), jobPageHitFoot)
	}
	keys := jobPageKeysOver
	if !job.Over() {
		keys = jobPageKeysRun
	}
	add(" "+paintHint(hintFit(keys, width-2), pal, pal.dim), jobPageHitFoot)

	if len(lines) > height && height > 1 {
		lines = append(lines[:1], lines[len(lines)-(height-1):]...)
		hits = append(hits[:1], hits[len(hits)-(height-1):]...)
	}
	return lines, hits, 0, 0
}

// jobPageTitle is the head: what this job is called on the left, and how to
// get back on the right. It is [app.taskCardTitle] pointed at a job.
//
// THE TITLE IS THE NAME, OR THE HANDLE WHEN THERE IS NO NAME YET. It is never
// the raw command: the body already draws the command in full, and putting the
// same string in the head made a page that said the command twice and hid the
// number a person came looking for (`job 8`). Until the namer answers, `job N`
// is the honest title — short, stable, and the same handle the foot and the
// column already use.
func (a *app) jobPageTitle(width int, job session.JobNotice) string {
	words := strings.TrimSpace(job.Name)
	if words == "" {
		words = jobPageHandle(job.ID)
	}
	right := jobPageBackWord + " "
	room := width - ansi.StringWidth(right) - 1
	if room < 1 {
		return fit(" "+a.pal.bold(a.pal.ink(words)), width)
	}
	words = fit(words, room)
	left := " " + a.pal.bold(a.pal.ink(words))
	gap := width - ansi.StringWidth(" "+words) - ansi.StringWidth(right)
	if gap < 1 {
		return fit(left, width)
	}
	return left + strings.Repeat(" ", gap) + a.pal.dim(right)
}

// jobPageBody is everything under the rule: the handle and the clock or the
// ending, the command in full (wrapped — a command is prose a person typed),
// and the log tail, dim, cut rather than wrapped, newest at the bottom.
func (a *app) jobPageBody(job session.JobNotice, width int) []string {
	if width < 1 {
		width = 1
	}
	pal := a.pal
	var bands [][]string

	if line := a.jobPageWhenLine(job); line != "" {
		bands = append(bands, []string{pal.dim(fit(line, width))})
	}
	if cmd := strings.TrimSpace(job.Command); cmd != "" {
		var said []string
		for _, wrapped := range wrap(cmd, width) {
			said = append(said, pal.ink(wrapped))
		}
		bands = append(bands, said)
	}
	if detail := strings.TrimSpace(job.Detail); detail != "" {
		bands = append(bands, []string{pal.dim(fit(detail, width))})
	}

	var log []string
	switch {
	case a.hosted():
		// SAY WHERE THE FILE IS rather than drawing the miss. A blank tail on a
		// hosted page would read as a job that has not written yet; the file is
		// on the other machine and this disk must not be asked.
		log = []string{pal.dim(fit(jobPageHostedWord+a.host, width))}
	default:
		var lines []string
		if view := a.jobView(); view != nil {
			lines = view.log
		}
		for _, line := range lines {
			log = append(log, pal.dim(fit(line, width)))
		}
	}
	bands = append(bands, log)

	var out []string
	for _, band := range bands {
		if len(band) == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, band...)
	}
	return out
}

// jobPageWhenLine is the handle and either the running clock or the ending —
// one line, with every clause that has nothing behind it dropped.
func (a *app) jobPageWhenLine(job session.JobNotice) string {
	segs := []string{jobPageHandle(job.ID)}
	if ending := jobPageEnding(job, a.jobPageClock(job)); ending != "" {
		segs = append(segs, ending)
	}
	return strings.Join(segs, railSep)
}

// jobPageClock is the job's age: counting up from Started while it runs, so
// the page does not have to re-ask the engine four times a second, and frozen
// at Elapsed once it has ended ([session.JobNotice] says why both are there).
func (a *app) jobPageClock(job session.JobNotice) string {
	if !job.Over() && !job.Started.IsZero() {
		return countUpWord(a.now().Sub(job.Started))
	}
	return countUpWord(job.Elapsed)
}

// jobPageEnding is the clock on a running job, and the state (and, when it
// failed, the exit) on a settled one. Zero and unknown drop out — the
// emptiness law over a process.
func jobPageEnding(job session.JobNotice, clock string) string {
	var segs []string
	if job.Over() {
		if word := strings.TrimSpace(string(job.State)); word != "" {
			segs = append(segs, word)
		}
		if job.State == session.JobFailed && job.ExitCode != 0 {
			segs = append(segs, "exit "+itoa(job.ExitCode))
		}
		if clock != "" {
			segs = append(segs, "ran "+clock)
		}
	} else if clock != "" {
		segs = append(segs, clock)
	}
	return strings.Join(segs, railSep)
}

// jobPageKeys is the foot's legend for a test that does not want to stand the
// whole page up.
func jobPageKeys(running bool) string {
	if running {
		return jobPageKeysRun
	}
	return jobPageKeysOver
}
