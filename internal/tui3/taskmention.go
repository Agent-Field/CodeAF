package tui3

// TASK MENTIONS: "@" ALSO MEANS WORK SOMEBODY DID.
//
// files.go says what "@" was — a way to put a path in a sentence without typing
// it — and the rule it holds to is that the surface inserts TEXT and resolves
// nothing. This file adds the second kind of thing a person points at with the
// same key, and it holds to the same rule for a harder reason.
//
// A TASK IS NOT A FILE, AND POINTING AT ONE CANNOT BE INSERTING ITS CONTENTS.
// The work is a worktree, a branch and a whole session journal — tens of
// thousands of tokens if anybody were foolish enough to inline it. So what a
// chosen task becomes in the sentence is a POINTER BLOCK: six facts and two
// URIs, written where the person can read them, with the model's own hands
// (read, bash) left to follow either URI if the answer needs more than the
// facts. That is the same trade files.go makes, one size up.
//
// ── THE THREE PARTS ──
//
//   - THE LIST. The project's task index (internal/session's task_index.go),
//     ranked against whatever follows the "@", drawn ABOVE the files because a
//     person who half-remembers a piece of work has fewer other ways to find it
//     than a person who half-remembers a filename. Sectioned by WHEN, because
//     "the one that is running" and "the one from last week" are two different
//     questions and a single recency-sorted list answers neither well.
//   - THE INSERTION. Choosing a row types "@<slug>" — the task's title,
//     kebab-cased. It is derived from the title rather than minted, so it is a
//     name a person can type from memory without having opened the list at all
//     (session.TaskSlug says why).
//   - THE EXPANSION. At submit, every "@<slug>" that names a task in the index
//     grows a pointer block, appended after the sentence. The token STAYS where
//     it was typed: the sentence is what the person wrote, and the block is the
//     footnote under it.
//
// ── WHAT THE MODEL SEES IS WHAT THE PERSON SEES ──
//
// The expansion happens before the message is submitted AND before it lands in
// the transcript, so the block is on screen exactly as it went on the wire.
// This surface has one law about the box (files.go: "what is submitted is the
// text as typed") and appending something invisible to a person's own message
// would be the first breach of it. Nothing here is silent.

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// The bounds of the task half of the list.
const (
	// taskMentionRows is the cap on DRAWN task rows. Eight is the file list's
	// own screenful (completeRows), and the tasks do not get more than the
	// files: this is a completion, not a task browser.
	taskMentionRows = 8
	// taskMentionPool is how deep the search goes before the sections cut it
	// down. It is wider than the cap so that a match three sections down is
	// found and counted even when there is no room to draw it.
	taskMentionPool = 40
	// taskRecentWindow is where "recent" ends. A day is the span inside which a
	// person says "earlier" and means it.
	taskRecentWindow = 24 * time.Hour
)

// The section rules. They are words and not decoration: the list has two kinds
// of thing on it now, and a person scanning it needs to know which kind their
// eye is in.
const (
	compRunningRule = "running"
	compRecentRule  = "recent"
	compOlderRule   = "older"
	compFilesRule   = "files"
)

// The three sections, in the order they are drawn.
const (
	sectionRunning = iota
	sectionRecent
	sectionOlder
)

// taskMentionAgent is the slice of *session.Agent this file needs, asserted
// rather than added to [Agent].
//
// It is optional for [taskAgent]'s reason (task.go): a surface can be driven by
// a scripted agent that has never heard of a task, and every other test in this
// package is. An agent that cannot answer this simply has no task rows under
// its "@", which is also what a brand-new project has.
type taskMentionAgent interface {
	// TaskIndex is the project's task history, newest first, with this
	// session's live graph merged over it.
	TaskIndex() []session.TaskIndexEntry
}

// ── the snapshot ────────────────────────────────────────────────────────────

// tasksLoadedMsg carries the index read back to the loop.
type tasksLoadedMsg struct{ rows []session.TaskIndexEntry }

// loadTasks reads the project's index off the loop, ONCE per surface — the same
// discipline [app.loadFiles] follows, for a smaller reason: the file is small,
// but it is a file, and a list that touched the disk on every keystroke would be
// a list that stutters on a slow home directory.
//
// It is re-armed by [app.refreshTasks] whenever a node lands, which is the only
// moment the answer can change under a person who is looking at it.
func (a *app) loadTasks() tea.Cmd {
	if a.comp.tasksLoaded || a.comp.tasksHeld {
		return nil
	}
	agent, ok := a.agent.(taskMentionAgent)
	if !ok {
		// Nothing behind the seam to ask. Marked loaded so the question is asked
		// once and not on every keystroke of every "@" for the rest of the
		// session.
		a.comp.tasksLoaded = true
		return nil
	}
	a.comp.tasksHeld, a.comp.tasksStale = true, false
	return func() tea.Msg { return tasksLoadedMsg{rows: agent.TaskIndex()} }
}

// tasksLoaded folds one read in and re-ranks whatever list is open over it.
//
// A READ THAT WAS OVERTAKEN DOES NOT COUNT AS FRESH. If a node changed state
// while this read was in flight, the rows coming back are one event out of date;
// marking the snapshot loaded on them would leave the list stale for the rest of
// the session, because nothing would ever ask again. So the rows are kept — they
// are better than nothing to draw — and the read is immediately re-armed.
func (a *app) tasksLoaded(rows []session.TaskIndexEntry) tea.Cmd {
	a.comp.tasks, a.comp.tasksHeld = rows, false
	a.comp.tasksLoaded = !a.comp.tasksStale
	if a.comp.open {
		a.comp.rank()
	}
	a.touch()
	if a.comp.tasksLoaded {
		return nil
	}
	return a.loadTasks()
}

// refreshTasks says the snapshot is stale and reads it again if anybody is
// looking. It is called where a node changes state (task.go): a task that has
// just started belongs under "running" the next time the list is opened, and a
// task that has just landed belongs in the file it was written to.
func (a *app) refreshTasks() tea.Cmd {
	a.comp.tasksLoaded = false
	if a.comp.tasksHeld {
		// A read is already on its way and it left before this event: see
		// [app.tasksLoaded] for what happens to it.
		a.comp.tasksStale = true
		return nil
	}
	if !a.comp.open && !a.taskSheet.open && !a.railShowing() {
		// Nobody is looking. The next "@" — or the next time the task page is
		// opened — pays for the read, which is the same deal the first one made.
		// THE PAGE IS ON THIS LIST BECAUSE IT IS THE ONE READER THAT STAYS OPEN
		// ACROSS A LANDING: the "@" list is dismissed by the keystroke after it,
		// while somebody can sit on the task page watching a node finish, and a page
		// that kept drawing it under "running" after it landed would be the record
		// disagreeing with the column beside it (taskview.go).
		//
		// AND THE COLUMN IS ON IT FOR THE SAME REASON, ONE STEP FURTHER: it carries
		// the project's record under its own rows now ([app.railRecord]) and it is
		// on screen the whole time, so it is the reader that never looks away. A
		// node that lands in THIS session moves from the column's forest into that
		// record, and a snapshot nobody re-read would draw it in both.
		return nil
	}
	return a.loadTasks()
}

// dropTaskMentions forgets the snapshot. It runs where the agent underneath is
// REPLACED (/new): the index is the project's and survives, but the live rows
// merged into it belong to a conversation that no longer exists.
func (a *app) dropTaskMentions() {
	a.comp.tasks, a.comp.tasksLoaded = nil, false
	a.comp.tasksHeld, a.comp.tasksStale = false, false
	a.comp.taskHits, a.comp.taskSection, a.comp.older = nil, nil, 0
}

// ── the ranking, and the three sections ─────────────────────────────────────

// rankTasks matches the query against the index and lays the survivors out in
// section order, capped.
//
// THE ARGUMENT LIST GETS NONE. "/image <path>" takes a path, and a task offered
// where a file is required is a row that cannot be chosen for what the line is
// asking (files.go's arg mode).
func (c *completion) rankTasks() {
	c.taskHits, c.taskSection, c.older = c.taskHits[:0], c.taskSection[:0], 0
	if c.arg || len(c.tasks) == 0 {
		return
	}
	hits := session.SearchTaskIndex(c.tasks, c.query, taskMentionPool)
	if len(hits) == 0 {
		return
	}
	now := time.Now()
	// Bucketed first and then drawn, because the sections are not the search's
	// order: a running task is the top row whatever it scored, and the ranking
	// decides the order INSIDE a section rather than between them.
	buckets := [3][]session.TaskIndexEntry{}
	for _, entry := range hits {
		section := taskSectionOf(entry, now)
		buckets[section] = append(buckets[section], entry)
	}
	for section, bucket := range buckets {
		for _, entry := range bucket {
			if len(c.taskHits) >= taskMentionRows {
				// THE COUNT IS KEPT RATHER THAN THE ROW. Everything the cap cut is
				// counted into [completion.older] and said once on the "older"
				// rule — a list that silently dropped four matches would be a list
				// that told a person their task is not there.
				c.older++
				continue
			}
			c.taskHits = append(c.taskHits, entry)
			c.taskSection = append(c.taskSection, section)
		}
	}
}

// taskSectionOf files one row: running now, ended inside the day, or older.
func taskSectionOf(entry session.TaskIndexEntry, now time.Time) int {
	switch {
	case entry.Live():
		return sectionRunning
	case entry.EndedAt.IsZero(), now.Sub(entry.EndedAt) > taskRecentWindow:
		return sectionOlder
	default:
		return sectionRecent
	}
}

// layoutTasks appends the task half of the list: one rule per section that has
// anything in it, and the rows under it.
//
// The OLDER rule carries a count when the cap cut something, which is the
// "collapsed" half of this section: on a crowded list the older work is a number
// rather than eight more rows, and the number is what says to type another
// letter.
func (c *completion) layoutTasks(lines []compLine) []compLine {
	section := -1
	for at := range c.taskHits {
		if c.taskSection[at] != section {
			section = c.taskSection[at]
			lines = append(lines, compLine{header: c.sectionRule(section), task: -1, file: -1})
		}
		lines = append(lines, compLine{task: at, file: -1})
	}
	if c.older > 0 && section != sectionOlder {
		// Everything the cap cut is older than everything drawn, so its rule goes
		// last — and with nothing under it, which is exactly what a collapsed
		// section is.
		lines = append(lines, compLine{header: c.sectionRule(sectionOlder), task: -1, file: -1})
	}
	return lines
}

// sectionRule is one section's words, with the count on the one that has one.
func (c *completion) sectionRule(section int) string {
	switch section {
	case sectionRunning:
		return compRunningRule
	case sectionRecent:
		return compRecentRule
	default:
		if c.older > 0 {
			return compOlderRule + " · " + itoa(c.older) + " more"
		}
		return compOlderRule
	}
}

// ── the row ─────────────────────────────────────────────────────────────────

// The status glyphs, and the mark that says a row is a task at all.
//
// THE STATUS IS A SHAPE AND NOT A COLOUR, which is the opposite of what the rail
// does (task.go paints a failed node red). The reason is the row it sits on:
// [overlayRow] paints a whole row by what it IS — under the cursor, hovered,
// dim — and a label carrying colour of its own would fight the row's own state
// for the eye and lose on a monochrome terminal anyway. On the rail a node is
// the row; here it is one word in a list of twelve.
//
// glyphMention is the identity mark: the one cell that says this row is work
// that ran somewhere else rather than a path in this directory. Without it the
// two halves of the list are told apart only by the rule scrolled above them.
const (
	glyphRunning      = "▸"
	glyphRunningASCII = ">"
	glyphMention      = "⧉"
	glyphMentionASCII = "#"
)

// taskRowLabel is one task's half of a row, in the overlay's own grammar: the
// state, the mention mark and the words. The dim note beside it — or under it on
// a phone — is the AGE and nothing else ([taskNoteWord]): a row here is chosen
// by recognition, the person already knows what the task was, so the one fact
// worth the tail is which of the two similarly-named ones this is.
//
//	› ✓ ⧉ Fix the nil-map crash                                    3h
//	  ▸ ⧉ Sweep the deprecated call sites                          4m
func taskRowLabel(entry session.TaskIndexEntry, ascii bool) string {
	// Label is the title already cut to a row's width (session.taskLabel), and
	// the uncut title stands in for a row written before that field existed.
	words := entry.Label
	if words == "" {
		words = entry.Title
	}
	return taskStatusGlyph(entry, ascii) + " " + mentionMark(ascii) + " " + words
}

func mentionMark(ascii bool) string {
	if ascii {
		return glyphMentionASCII
	}
	return glyphMention
}

// taskStatusGlyph is the node's state in one cell, from the vocabulary the rail
// already spends (task.go) so that a person who has watched a task run
// recognizes it here.
func taskStatusGlyph(entry session.TaskIndexEntry, ascii bool) string {
	switch entry.Status {
	case string(session.TaskRunning):
		if ascii {
			return glyphRunningASCII
		}
		return glyphRunning
	case string(session.TaskQueued):
		if ascii {
			return glyphQueuedASCII
		}
		return glyphQueued
	case string(session.TaskFailed):
		if ascii {
			return glyphBadASCII
		}
		return glyphBad
	case string(session.TaskUnverified):
		// THE TICK IS NOT THE DEFAULT ANSWER TO "WHAT ELSE IS THERE". An
		// unverified row would otherwise fall through below and wear the one
		// success glyph this surface has, which is the single place a person
		// picking a task by recognition could be told that work nobody could
		// judge came home (task.go's [glyphUnverified]).
		return glyphUnverified
	default:
		if ascii {
			return glyphDoneASCII
		}
		return glyphDone
	}
}

// taskNoteWord is the age on the right: how long a live task has been going,
// how long ago a landed one landed.
func taskNoteWord(entry session.TaskIndexEntry) string {
	if entry.Live() {
		return countUpWord(entry.Duration())
	}
	if entry.EndedAt.IsZero() {
		return ""
	}
	return since(entry.EndedAt)
}

// ── choosing one ────────────────────────────────────────────────────────────

// completeMention is enter on the list: a task becomes its slug, a file becomes
// its path (files.go). It is the one door, so that enter means one thing.
func (a *app) completeMention() {
	if entry, ok := a.comp.taskChoice(); ok {
		a.completeTask(entry)
		return
	}
	a.completeFile()
}

// completeTask types the task's slug where the half-typed query was, keeping the
// "@" exactly as a chosen path does.
//
// WHAT GOES IN THE LINE IS THE NAME, NOT THE BLOCK. The block is minted at
// submit ([app.expandTaskMentions]) rather than here, for two reasons that point
// the same way: a person who changes their mind and deletes the token should not
// have to delete six lines of footnote with it, and a block minted now would be
// a snapshot of a task that may still be running when the message is finally
// sent.
func (a *app) completeTask(entry session.TaskIndexEntry) {
	name := strings.TrimSpace(entry.Name)
	if name == "" {
		name = strings.TrimSpace(entry.ID)
	}
	if name == "" {
		a.comp.close()
		return
	}
	e := &a.input
	head := append([]rune(nil), e.value[:a.comp.at+1]...)
	tail := append([]rune(nil), e.value[e.cursor:]...)
	e.value = append(append(head, []rune(name)...), tail...)
	e.cursor = a.comp.at + 1 + len([]rune(name))
	a.comp.done = name
	a.comp.close()
	a.touch()
}

// ── the pointer block ───────────────────────────────────────────────────────

// expandTaskMentions returns the sentence with a pointer block appended for
// every "@<slug>" in it that names a task, and returns it unchanged when none
// do.
//
// UNKNOWN TOKENS ARE LEFT ALONE, in silence. "@internal/session/task.go" is a
// path, "@santosh" is a person, and neither is a failed lookup worth telling
// anybody about — the sentence a person wrote is not a query this surface gets
// to reject.
//
// One block per task, in the order the tokens appear, and a task mentioned twice
// gets one: the block is a citation, and a citation repeated is noise in a
// context window somebody is paying for.
//
// IT RESOLVES AGAINST THE SNAPSHOT AND NEVER TOUCHES THE DISK. Enter runs on the
// program loop, and a loop that read a file to decide what a message says would
// stutter on exactly the keystroke a person is least willing to wait on. The
// snapshot is there because typing "@" and two characters loads it
// ([app.loadTasks]), which is every way a mention gets into the box except one:
// a slug pasted whole and submitted in the same beat resolves to nothing and
// stays the plain "@word" the person typed. That is the same thing an unknown
// token does, and it is the right failure — the sentence still says what they
// meant.
func (a *app) expandTaskMentions(text string) string {
	if len(a.comp.tasks) == 0 || !strings.Contains(text, "@") {
		return text
	}
	var blocks []string
	seen := make(map[string]bool)
	for _, token := range mentionTokens(text) {
		entry, ok := session.LookupTask(a.comp.tasks, token)
		if !ok {
			continue
		}
		key := entry.SessionID + "/" + entry.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		blocks = append(blocks, taskPointerBlock(entry))
	}
	if len(blocks) == 0 {
		return text
	}
	return strings.TrimRight(text, "\n") + "\n\n" + strings.Join(blocks, "\n")
}

// mentionTokens pulls every "@word" out of a sentence, in order.
//
// It is [atToken]'s rule applied to the whole line rather than to the caret: a
// run that STARTS with "@" and ends at whitespace. An "@" in the middle of a
// word — an email address, a Go doc link — is not one, for the reason the
// completion does not open on one either.
func mentionTokens(text string) []string {
	var out []string
	for _, field := range strings.Fields(text) {
		if !strings.HasPrefix(field, "@") || len(field) == 1 {
			continue
		}
		// Trailing punctuation belongs to the sentence, not to the name: a slug
		// is letters, digits and hyphens, so "@fix-the-crash," is that task
		// followed by a comma.
		token := strings.ToLower(strings.TrimRight(field[1:], ".,;:!?)]}\"'"))
		if token != "" {
			out = append(out, token)
		}
	}
	return out
}

// taskPointerBlock is the footnote one mention becomes.
//
//	[Task reference: Fix the nil-map crash — id 7 · done · ended 3h ago
//	 Outcome: "Added the guard and the regression test; the parser suite passes."
//	 Output: git:task/fix-the-nil-map-crash-9c1a2f · Transcript: file:///…/7.jsonl]
//
// Three lines, and every clause that has nothing behind it is DROPPED rather
// than written empty. A block reading `Outcome: "" · Files: 0` is three facts
// this build does not have, stated as though it did — and stated to a model,
// which will reason from them.
//
// A RUNNING TASK SAYS SO, and only a running task carries the steer clause. The
// design this implements prints "ended <age>" and a steer handle on every block;
// on a task that finished last Tuesday the first is right and the second is an
// offer nobody can take, because there is nothing left running to send words to.
func taskPointerBlock(entry session.TaskIndexEntry) string {
	head := "[Task reference: " + entry.Title + " — id " + entry.ID + " · " + entry.Status
	if when := mentionWhenWord(entry); when != "" {
		head += " · " + when
	}

	var facts []string
	if entry.Outcome != "" {
		facts = append(facts, `Outcome: "`+entry.Outcome+`"`)
	}
	if entry.FilesChanged > 0 {
		facts = append(facts, "Files: "+itoa(entry.FilesChanged))
	}
	// A RUNNING TASK SAYS WHAT IT IS DOING, in the place a landed one says what
	// came of it. The index fills this only for live rows and never writes it to
	// disk (session.TaskIndexEntry.Activity); it is stale by however long the
	// snapshot has sat, which is the same staleness the status beside it has.
	if entry.Activity != "" {
		facts = append(facts, "Live: "+entry.Activity)
	}

	var where []string
	if entry.ArtifactURI != "" {
		where = append(where, "Output: "+entry.ArtifactURI)
	}
	if entry.TranscriptURI != "" {
		where = append(where, "Transcript: "+entry.TranscriptURI)
	}
	if entry.Live() {
		// TWO DOORS ON ONE RUNNING NODE, and the block is read by the model, so
		// it names the model's first: `tasks id N say "…"` reaches the node's
		// loop exactly as the person's own line does (session.SteerTask). The
		// room on the rail is the other half of the same door — the person walks
		// into the node and types — and it is named second because nobody
		// reading this block can press it.
		where = append(where, `Steer: tasks id `+entry.ID+` say "…", or its room on the rail`)
	}

	block := head
	if len(facts) > 0 {
		block += "\n " + strings.Join(facts, " · ")
	}
	if len(where) > 0 {
		block += "\n " + strings.Join(where, " · ")
	}
	return block + "]"
}

// mentionWhenWord is the block's time clause, in the tense the task is in.
func mentionWhenWord(entry session.TaskIndexEntry) string {
	if entry.Live() {
		if word := countUpWord(entry.Duration()); word != "" {
			return "running " + word
		}
		return ""
	}
	if entry.EndedAt.IsZero() {
		return ""
	}
	return "ended " + session.TaskAgeWord(time.Since(entry.EndedAt)) + " ago"
}
