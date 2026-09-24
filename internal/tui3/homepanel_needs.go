package tui3

import (
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// needsPanel keeps questions whose conversation or task has no visible row.
// It has no heading: normal conversation and task rows carry their own questions,
// and these fallback rows keep off-list questions reachable. Live questions sort
// before task decisions, with older questions first and newer decisions first.
// Answers still use the shared homeAnswerAt routing so the painted action and
// the row receiving it cannot disagree.
type needsPanel struct{ homePanelBase }

// needsAnswersCap is how many of a question's answers fit on its row. A
// question with more draws the first ones and stops; every answer is still one
// enter away, and the row does not say so.
//
// NO ROW OF THIS PANEL NAMES THE ENTER KEY. A question row used to say `enter`
// at the right of its sentence when its answers were not drawn, and a watch
// with no conversation behind it `enter on standing`; the owner ruled
// (2026-09-17) that the word beside every description was noise — the foot
// under the grid already says `enter open` once for every row, and a row that
// can be stood on is a row enter does something with.
const needsAnswersCap = 3

// needsItem is one row before the panel orders it: when it was asked, and the
// line.
type needsItem struct {
	asked time.Time
	line  homeLine
}

func (needsPanel) rows(in *homeGridInput) homePanelRows {
	// Questions already carried by a conversation or task do not get a second row.
	shown := make(map[string]bool)
	for _, panel := range []homePanel{sessionsPanel{homePanelBase{panelSessions}}} {
		for _, line := range panel.rows(in).lines {
			shown[homeQuestionRowKey(line)] = true
		}
	}
	asked := needsAsked(in)
	kept := asked[:0]
	for _, item := range asked {
		if !shown[homeQuestionTargetKey(item.line)] {
			kept = append(kept, item)
		}
	}
	asked = kept
	sort.SliceStable(asked, func(i, j int) bool { return attentionOlder(asked[i].asked, asked[j].asked) })
	// AND THE LANDINGS ARE NEWEST FIRST, which is the opposite order and the
	// right one for them: `needs you` is ranked by how long something has been
	// stopped, and nothing is stopped here — the freshest landing is the work
	// still in the person's head, and the oldest ages out of the group entirely
	// ([needsFresh]).
	var calls []needsItem
	for _, item := range in.calls {
		if !shown[homeQuestionTargetKey(item.line)] {
			calls = append(calls, item)
		}
	}
	sort.SliceStable(calls, func(i, j int) bool { return attentionOlder(calls[j].asked, calls[i].asked) })
	lines := make([]homeLine, 0, len(asked)+len(calls))
	for _, item := range append(asked, calls...) {
		lines = append(lines, item.line)
	}
	out := homePanelCut(in, panelNeeds, lines)
	// NEITHER THE HEADING NOR THE GROUP LINE CARRIES A COUNT. The heading used
	// to count the live questions (`needs you · 2`) and the group line the
	// landings (`to check · 8`); the owner cut both on 2026-09-15, because the
	// rows are right there under the words and a figure beside them is a second
	// thing to read that says nothing the rows do not. The fold still counts what
	// it hides (`3 unread`, `N more`), which is the one place a count stands for
	// rows that are NOT on the screen. The aged-out landings are counted on the
	// same fold ([needsFresh]).
	out.older = in.callsOlder
	if len(calls) > 0 {
		out.group = &homePanelGroup{at: len(asked), word: needsCheckWord}
	}
	return out
}

// needsFresh is the task calls that are still worth checking, and how many are
// history.
//
// A YOUR-CALL OLDER THAN [homeNeedsTaskFresh] IS HISTORY, NOT A QUESTION (owner,
// 2026-09-10: a machine with twenty-four week-old landings drew twenty-four rows
// over rows nobody was going to answer, and a live question arriving under them
// would have been the twenty-fifth). They stay one press away — the fold counts
// them and opening the panel shows them. The tasks place also keeps every
// record. Only a task's
// call ages: a conversation stopped on a question and a watch that needs
// somebody are live, and are never aged out. A landing with no time on it is
// not known to be old, and stays.
func needsFresh(items []needsItem, now time.Time) (fresh []needsItem, older int) {
	for _, item := range items {
		if needsAged(item.asked, now) {
			older++
			continue
		}
		fresh = append(fresh, item)
	}
	return fresh, older
}

// needsAged is that rule as one predicate, so the panel's rows and the pulse's
// count age a landing out at the same instant ([machineCounts]).
func needsAged(asked, now time.Time) bool {
	return !asked.IsZero() && now.Sub(asked) > homeNeedsTaskFresh
}

// needsAsked is the conversations and watches the switcher already ranks as
// waiting on somebody.
//
// A CONVERSATION WAITING ONLY ON ITS OWN LANDING IS NOT ONE OF THEM — THE
// LANDING IS. [session.Agent.waitingOnPerson] reads a pending decision LAST and
// writes the sentence `your call on <title>` into the presence with no question
// object, so such a session drew as a question ABOVE the `unread` row for the
// very same piece of work: once under the CONVERSATION's name with a bare
// `enter`, once under the TASK's name with its files and its two answers. The
// second row is the better one and this drops the first (the spec of record's
// "said once", applied inside the panel as well as across the columns).
func needsAsked(in *homeGridInput) []needsItem {
	var items []needsItem
	for _, row := range in.rows {
		if !row.needs || (row.kind == switcherConversation &&
			needsLandingsSpeakFor(row.session, needsCallTitlesOn(in, row.session.ID))) {
			continue
		}
		// THE MARK ALWAYS SHOWS AND THE SENTENCE SHOWS UNDER THE POINTER OR THE
		// CURSOR (owner, 2026-09-17): a row is its title and its wait at rest,
		// like every row of the field, and grows the question when it is read.
		cell := &homeCell{panel: panelNeeds, mark: cellMarkNeeds, title: row.title, grows: true}
		homeLiveMargin(cell, row, sinceAt(row.at, in.now))
		item := needsItem{asked: row.at}
		switch row.kind {
		case switcherConversation:
			// THE DESCRIPTION IS HEADED BY THE THREAD (owner, 2026-09-17), which
			// for a stopped conversation is the row's own title — the label
			// `threads` draws for it — said once more as the description's
			// title line so every row of the panel reads the same way.
			cell.thread = row.title
			head, whole := needsSentence(row.session)
			cell.sub = head
			// A CONVERSATION ALWAYS HAS A DOOR: it is the conversation the
			// question was asked in, and enter goes to it — the row does not say
			// so ([needsAnswersCap] states the rule).
			if _, ok := answerable(row.session, in.now); ok && whole {
				cell.answers = answersWord(row.session.Presence.Question)
			}
		case switcherStanding:
			// THE THREAD IT BELONGS TO HEADS THE DESCRIPTION, spelled as
			// `threads` spells that conversation, for a watch asked for in one;
			// a watch made from home's own box belongs to no thread and has no
			// title line.
			cell.thread = needsThreadOf(in, row.item.Item.Origin.Transcript)
			cell.sub = switcherFirstLine(row.item.Item.NeedsPerson)
			// A watch asked for in a conversation opens that conversation; one
			// made from home's own box has none — its exchange is kept under the
			// item's folder rather than as a session ([standing.Origin.Exchange])
			// — and `enter` opens the item where it does live, on standing
			// ([app.homeItemEnter]). The row names neither door
			// ([needsAnswersCap] states the rule).
		}
		item.line = switcherRowLine(row, cell)
		items = append(items, item)
	}
	return items
}

// needsYourCallLead opens the sentence a session writes into its presence while
// nothing but a landed task is waiting on somebody — internal/session's
// `yourCallLine`, which is the tier's own word and a preposition
// ([tierYourCallWord]; taskpresence.go builds it the same way from the same
// word). It is rebuilt here rather than imported because the engine keeps it
// unexported; the seam is noted in the change entry.
const needsYourCallLead = tierYourCallWord + " on "

// needsLandingsSpeakFor reports that the WHOLE of what a conversation is waiting
// on is one of its own landings, which the `unread` group is already drawing
// under the work's own name.
//
// IT IS THE ENGINE'S SENTENCE AND NOT AN ABSENCE. The first cut of this test was
// "the presence carries no question object", and that is wrong in two lanes that
// bank no card at the desk and say so: a sub-harness offer (`wants to run …`)
// and an adaptive run at its fuel gate (`out of fuel · …`) both write a reason
// with no question, and a conversation stopped at either of those would have
// VANISHED from home the moment it happened to own a landing
// (internal/session's taskpresence.go says which lanes bank nothing). So the
// test is the sentence the pending-decision arm writes, opened by
// [needsYourCallLead] and closed by the title of one of this conversation's own
// landings — a question of any other kind keeps its row.
func needsLandingsSpeakFor(row session.SessionRow, titles []string) bool {
	if len(titles) == 0 || row.Presence.Question.Kind != "" {
		return false
	}
	reason := strings.TrimSpace(row.Reason())
	if !strings.HasPrefix(reason, needsYourCallLead) {
		return false
	}
	for _, title := range titles {
		if title != "" && strings.HasSuffix(reason, title) {
			return true
		}
	}
	return false
}

// needsCallTitlesOn is the titles the `unread` group is drawing for one
// conversation, read out of the reading the beat already took.
func needsCallTitlesOn(in *homeGridInput, id string) []string {
	var titles []string
	for _, call := range in.calls {
		if call.line.task != nil && call.line.task.SessionID == id {
			titles = append(titles, strings.TrimSpace(call.line.task.Title))
		}
	}
	return titles
}

// needsWants is how many rows of `needs you` ONE conversation is: one per
// landing of its own that the `unread` group draws, and its own question where
// the panel keeps that row.
//
// THE PANEL BUILDS ITS ROWS FROM THESE READINGS AND THE PULSE COUNTS THEM FROM
// THIS ONE, so `N want you` over a home and the rows under it are the same
// arithmetic ([machineCounts]).
func needsWants(row session.SessionRow, now time.Time) int {
	titles := needsCallTitles(row, now)
	n := len(titles)
	if row.NeedsPerson() && !needsLandingsSpeakFor(row, titles) {
		n++
	}
	return n
}

// needsCallTitles is one conversation's landings as the `unread` group would
// draw them, by title, taken straight off the index row.
func needsCallTitles(row session.SessionRow, now time.Time) []string {
	if row.Archived {
		return nil
	}
	var titles []string
	for i := range row.Tasks.Rows {
		entry := row.Tasks.Rows[i]
		if _, ok := needsCallOf(row, entry); !ok || needsAged(needsCallAt(entry), now) {
			continue
		}
		titles = append(titles, strings.TrimSpace(entry.Title))
	}
	return titles
}

// needsCalls is every task the project's record marks as the person's call —
// work that finished and nobody could check, a landing that turned the work
// back, a branch that would not fasten. They are read off the world's own index
// rows through the one reading of a row ([taskEntryStatus]), so this panel and
// the tasks place can never disagree about which work is waiting.
//
// ENTER OPENS THE CONVERSATION ON THE TASK'S RECORD ([app.homeLandOnTask]),
// which is where the question and the whole of its evidence are.
func needsCalls(world session.World, now time.Time) []needsItem {
	var items []needsItem
	for _, project := range world.Projects {
		for _, row := range project.Sessions {
			for i := range row.Tasks.Rows {
				entry := row.Tasks.Rows[i]
				status, ok := needsCallOf(row, entry)
				if !ok {
					continue
				}
				items = append(items, needsCall(project, row, entry, status, now))
			}
		}
	}
	return items
}

// needsCallOf is THE ONE READING OF "THIS LANDING IS WAITING ON THE PERSON",
// and its status.
//
// THREE READERS AND ONE ANSWER. The `unread` group draws these rows, `since
// you left` drops the landings this group is already drawing ([leftPanel.rows]),
// and the pulse counts them beside the questions ([machineCounts]). A second
// spelling of this test anywhere would be a `3 want you` over four rows, which
// is the exact failure the one-reader law exists for.
//
// A LANDING THE CONVERSATION IS ITSELF ASKING ABOUT IS NOT ONE OF THESE
// ([needsAskedInChat]): that is a stopped conversation, it is a `needs you` row,
// and it is the row that can take a digit.
func needsCallOf(row session.SessionRow, entry session.TaskIndexEntry) (session.TaskStatus, bool) {
	// WORK IN A CONVERSATION SOMEBODY PUT AWAY IS NOT WAITING ON THEM. Archiving
	// is the decision to stop being asked about it, and every other panel already
	// reads it that way ([machineCounts] skips an archived row outright).
	if row.Archived || row.ArchivedTasks[entry.ID] {
		return session.TaskStatus{}, false
	}
	status := taskEntryStatus(entry, row.Runs(entry))
	if !status.Attention || status.On != session.TaskWaitPerson || needsAskedInChat(row, entry) {
		return session.TaskStatus{}, false
	}
	return status, true
}

// needsCallAt is when a landing asked — when the work ended, or when it started
// for a row that never recorded an ending.
func needsCallAt(entry session.TaskIndexEntry) time.Time {
	if !entry.EndedAt.IsZero() {
		return entry.EndedAt
	}
	return entry.StartedAt
}

// needsCall is one of those rows: the title, how long ago it landed, and —
// under the cursor — how many files it wrote, what it came to and its two
// answers.
func needsCall(project session.Project, row session.SessionRow, entry session.TaskIndexEntry, status session.TaskStatus, now time.Time) needsItem {
	title := strings.TrimSpace(entry.Label)
	if title == "" {
		title = strings.TrimSpace(entry.Title)
	}
	// A PROGRAM'S WORK SAYS WHOSE IT IS, with its badge after its name
	// (programbadge.go) — the brackets alone, because the cell measures and
	// paints its title whole.
	title = programText(title, entry.Program)
	asked := needsCallAt(entry)
	// A LANDING WEARS NO MARK (law 8). The amber `?` means a thing has stopped
	// and will not move until somebody answers it; a landing has already
	// finished, and a column of question marks over work that is DONE was the
	// screen saying the opposite of what was true.
	// THE THREAD IT BELONGS TO HEADS THE DESCRIPTION (owner, 2026-09-17),
	// spelled as `threads` spells the same conversation ([homeName]); under
	// that title line come the files and what the work came to.
	cell := &homeCell{panel: panelNeeds, mark: cellMarkNeeds, title: title, right: sinceAt(asked, now),
		key: needsCallKey + entry.ID, thread: homeName(row),
		grows: true, sub: rowClauses(needsCallFiles(entry), needsCallSub(entry, status)), answers: needsCallAnswers(status)}
	line := homeLine{kind: homeSession, row: row, project: project.Name,
		dir: homeBucketOf(row.Transcript), task: &entry, cell: cell}
	return needsItem{asked: asked, line: line}
}

// needsCallSub is the line a landing grows under the cursor: THE FIRST SENTENCE
// OF WHAT THE WORK CAME TO, which is what a person needs in order to say whether
// it holds. The engine's own reason for the call stands in where the work left no
// report, and the tier's word where there is neither.
func needsCallSub(entry session.TaskIndexEntry, status session.TaskStatus) string {
	if sub := switcherFirstLine(entry.Outcome); sub != "" {
		return sub
	}
	if sub := switcherFirstLine(status.Reason); sub != "" {
		return sub
	}
	return status.Word
}

// needsCallFiles is how many files a landing wrote, for the head of its
// description — `3 files · <what it came to>` — and "" for one that wrote none,
// never `0 files` (the emptiness law).
//
// IT USED TO BE THE RIGHT MARGIN, as one clause with the age (`3 files · 1d`),
// so a landing that wrote nothing read `10h` beside one that read `1 file · 1d`
// — two shapes in one column for one kind of row. THE RIGHT MARGIN OF EVERY ROW
// OF THE FIELD IS A TIME (owner, 2026-09-15), and everything that is not a time
// is in the description column ([homeDescLines]).
func needsCallFiles(entry session.TaskIndexEntry) string {
	if entry.FilesChanged <= 0 {
		return ""
	}
	return itoa(entry.FilesChanged) + plural(" file", entry.FilesChanged)
}

// The two keys a landing is answered with ON HOME, and the landing keys they
// stand for.
//
// ── WHY THEY ARE DIGITS HERE AND LETTERS EVERYWHERE ELSE ────────────────────
//
// A landing is `[a] accept · [n] not right` on its card, in its room and on its
// record, and [session.LandingYesKey] is that letter (answers.go: the landing
// keys are task-states' own, letter for letter). HOME CANNOT OFFER A LETTER. Its
// box says "type to search or start something new" and the promise has no
// asterisk — a bare letter on this screen always types, whatever the cursor is
// resting on (home.go's [app.homeKey] states it), and the ONE printable
// exception it allows is a digit drawn on the row itself. An `a` that sometimes
// accepted a landing and sometimes began "a site for my café" is the mode that
// law exists to forbid.
//
// So the KEY is home's and the WORDS are the task's: `1 accept   2 not right`,
// read off [session.TaskAsk], mapped back to the landing's own keys before the
// answer leaves this surface ([needsLandingKey]). Nothing about the answer is
// renumbered — only the key a person presses on THIS screen, which is the same
// bargain the drawn digit has always been.
const (
	needsYesKey = "1"
	needsNoKey  = "2"
)

// needsCallAnswers is the two answers a landing offers, in the task's own words
// on home's own two digits.
//
// THE WORDS ARE THE ASK'S ([session.TaskAsk.Yes] and .No) and this file spells
// none of them. A row whose ask has no words offers nothing, which is the
// absence law: there is no default pair to fall back on.
//
// AND EVERY ASK THAT REACHES HERE IS [session.TaskAskCheck]. The status comes
// from [session.TaskIndexEntry.StatusFacts], which fills the state, the ending
// and the kind and nothing else — the merge, the ground hold and the phase are
// the NODE's facts and the index does not carry them — so `taskAskOf` can only
// take its `check` arm for an index row. A conflict is a different question with
// different words, it is banked by the engine as `conflict:<id>`
// ([session.Agent.landingQuestion]), and it reaches a person through the task's
// own card and not through this panel. So [needsLandingQuestion] names one kind
// rather than guessing between two.
func needsCallAnswers(status session.TaskStatus) string {
	yes, no := strings.TrimSpace(status.Ask.Yes), strings.TrimSpace(status.Ask.No)
	if yes == "" || no == "" {
		return ""
	}
	return needsYesKey + " " + yes + homeCellGap + needsNoKey + " " + no
}

// needsLandingKey is home's digit as the landing key the engine answers to.
func needsLandingKey(key string) (string, bool) {
	switch key {
	case needsYesKey:
		return session.LandingYesKey, true
	case needsNoKey:
		return session.LandingNoKey, true
	}
	return "", false
}

// needsChecking is every landing the `unread` group is drawing, by the
// identity a `since you left` line carries for the same task
// (homepanel_left.go's [leftKey]) — so one piece of work is one row of home's
// column and never two.
func needsChecking(in *homeGridInput) map[string]bool {
	said := make(map[string]bool, len(in.calls))
	for _, item := range in.calls {
		if item.line.task != nil {
			said[taskLedgerKey(*item.line.task)] = true
		}
	}
	return said
}

// needsCallKey prefixes a task row's [homeCell.key], so a conversation's own
// question and one of its tasks' calls are two rows and never one.
const needsCallKey = "call:"

// needsAskedInChat reports that a live conversation is already asking about
// this task on its own row — a landing's call raised as a question the session
// is stopped on — so the panel says it once, on the row that can take a digit.
func needsAskedInChat(row session.SessionRow, entry session.TaskIndexEntry) bool {
	if !row.NeedsPerson() {
		return false
	}
	q := row.Presence.Question
	switch q.Kind {
	case session.QuestionLanding, session.QuestionConflict:
		return strconv.FormatUint(q.ID, 10) == entry.ID
	}
	return false
}

// needsSentence is the line a question row carries under its title, and whether
// that line is the WHOLE question.
//
// THE CONSENT LINE IS THE ENGINE'S SENTENCE, VERBATIM AND UNPREFIXED
// (docs/changes/unreleased/773-home-switcher-findings.md): `needs your ok to run
// bash` is written once, by the lane that knows the tool, as the line another
// window answers from. Every other lane that describes its whole question says
// it in [session.Question.Head]; a lane that does not has only the line.
//
// A QUESTION WITH A PARAGRAPH SHOWS ITS HEAD ONLY, and its answers are not drawn
// beside half of it — enter opens the whole thing.
func needsSentence(row session.SessionRow) (head string, whole bool) {
	q := row.Presence.Question
	text := q.Text
	if q.Kind != session.QuestionConsent && q.Full != nil && strings.TrimSpace(q.Full.Head) != "" {
		text = q.Full.Head
	}
	if strings.TrimSpace(text) == "" {
		text = row.Reason()
	}
	head = switcherFirstLine(text)
	return head, head == strings.TrimSpace(text)
}

// answersWord is a question's answers as one clause, each option's key beside
// its own word — the same chips the answer strip draws ([answerChips]) — cut at
// [needsAnswersCap], with nothing after them.
func answersWord(question session.PresenceQuestion) string {
	var parts []string
	for i, chip := range answerChips(question) {
		if i == needsAnswersCap {
			break
		}
		parts = append(parts, chip.text)
	}
	return strings.Join(parts, homeCellGap)
}

// ── answering a landing from home ───────────────────────────────────────────

// needsLandingQuestion is the landing question a `unread` row stands for, as
// the answer doors want it: the kind, the node's id, and the two options in the
// task's own words. It is false for a row that is not a landing or whose id the
// index never recorded.
//
// IT IS BUILT FROM THE ROW AND NEVER FROM A LIVE SESSION'S PRESENCE. That is the
// whole difference between this group and the one above it: nothing is asking,
// so there is no question object to read — the task index says the call is the
// person's and [session.TaskAsk] says what the two answers are called.
func needsLandingQuestion(line homeLine) (session.PresenceQuestion, bool) {
	if line.task == nil || line.cell == nil || line.cell.answers == "" {
		return session.PresenceQuestion{}, false
	}
	id, err := strconv.ParseUint(strings.TrimSpace(line.task.ID), 10, 64)
	if err != nil || id == 0 {
		return session.PresenceQuestion{}, false
	}
	yes, no, ok := needsAnswerWords(line.cell.answers)
	if !ok {
		return session.PresenceQuestion{}, false
	}
	return session.PresenceQuestion{Kind: session.QuestionLanding, ID: id, Options: []session.AnswerOption{
		{Key: needsYesKey, Label: yes},
		{Key: needsNoKey, Label: no, Safe: true},
	}}, true
}

// needsAnswerWords reads the two words back out of the clause the row draws, so
// the chip a person sees and the label the receipt says are the SAME STRING and
// not two readings of one status.
func needsAnswerWords(clause string) (yes, no string, ok bool) {
	parts := strings.SplitN(clause, homeCellGap, 2)
	if len(parts) != 2 {
		return "", "", false
	}
	yes = strings.TrimPrefix(parts[0], needsYesKey+" ")
	no = strings.TrimPrefix(parts[1], needsNoKey+" ")
	return yes, no, yes != "" && no != ""
}

// homeAnswerLanding takes one of a landed task's own answers from home, and it
// is THE ONE DOOR on whichever side of the window the conversation is.
//
// In this window the answer goes straight to [session.Agent.ResolveQuestion],
// which is where the record card's `a` and `n` end up too ([app.taskCardKey] →
// [app.questionOptionKey] → the block). In any other window it is left on that
// conversation's doorstep ([app.leaveAnswer] → [session.WriteAnswer]), and that
// session's own heartbeat hands it to the SAME ResolveQuestion
// ([session.Agent.drainAnswers] → [session.Agent.applyLanding] →
// [session.Agent.ResolveUnverified]). There is no third path, and nothing here
// settles a task itself.
//
// A CONVERSATION THAT IS NOT RUNNING APPLIES IT WHEN IT NEXT RUNS, which is what
// this surface has always promised for another window's question and is why the
// row says `answered · waiting for it to pick that up` until it is picked up.
func (a *app) homeAnswerLanding(line homeLine, key string) (tea.Cmd, bool) {
	question, ok := needsLandingQuestion(line)
	if !ok {
		return nil, false
	}
	label := question.Label(key)
	answer, ok := needsLandingKey(key)
	if label == "" || !ok {
		return nil, false
	}
	row := a.homeTrue(line.row)
	if _, sent := a.answerSent(row, question); sent {
		// ONE ANSWER PER LANDING, for [app.answerRowKey]'s reason: the first is
		// on its way and the row is still saying so.
		return nil, false
	}
	if a.answeringHere(row) {
		if _, ok := a.questionDoors(); !ok {
			return nil, false
		}
		// THROUGH THE ONE ANSWERING DOOR, OFF THE LOOP (question.go's
		// [app.answerQuestion], offloop.go). This row used to ask the engine
		// from here and wait for it, which is the ten-second freeze on home's
		// own band: the window could not draw, could not take a key, and could
		// not read the news its own answer caused. The row says what it sent on
		// the keystroke, exactly as it did; what changed is that it no longer
		// waits to be told.
		sent := session.Answer{
			Kind: session.QuestionLanding, ID: question.ID,
			Key: answer, Picked: []string{answer},
		}
		// AND THE WHOLE QUESTION WHERE THE LANE LEFT ONE. Home's row carries it
		// ([session.PresenceQuestion.Full]) and it is what the receipt is
		// written from; the bare shape below is the honest fallback for a row
		// whose lane described nothing.
		asked := session.Question{
			Kind: session.QuestionLanding, ID: question.ID, Ask: session.AskLanding,
			Head: strings.TrimSpace(question.Text),
		}
		if question.Full != nil {
			asked = *question.Full
		}
		cmd := a.answerQuestion(questionShown{question: asked}, sent)
		a.home.say(answerSentWord+label, "")
		return cmd, true
	}
	dir := strings.TrimSpace(row.Dir)
	if a.leaveAnswer == nil || dir == "" {
		return nil, false
	}
	if err := a.leaveAnswer(dir, question.Kind, question.ID, answer); err != nil {
		a.home.say(answerFailedWord, dir)
		return nil, true
	}
	a.rememberAnswered(dir, question, label)
	a.home.say(answerSentWord+label, "")
	return nil, true
}

// needsThreadOf is the label `threads` draws for the conversation a transcript
// belongs to — the switcher's own title for that row — and nothing for a
// transcript the reading does not hold, or none at all.
func needsThreadOf(in *homeGridInput, transcript string) string {
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		return ""
	}
	for _, row := range in.rows {
		if row.kind == switcherConversation && strings.TrimSpace(row.session.Transcript) == transcript {
			return row.title
		}
	}
	return ""
}
