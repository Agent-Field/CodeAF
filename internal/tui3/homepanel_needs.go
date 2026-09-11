package tui3

import (
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// needsPanel is `needs you` (docs/design/home-mission-control/DESIGN.md §1, §3
// P1): everything on the machine that is waiting on a person, in TWO GROUPS on
// one panel.
//
// BLOCKING FIRST, THEN WHAT HAS LANDED (the spec of record for #884). The top
// group is the live questions — a conversation stopped on a question or on a
// consent card, a watch that needs somebody — longest wait first
// ([attentionOlder]), each wearing the amber mark and carrying its own sentence.
// Under them the `to check` group is every task whose call is the person's,
// newest first, one line each. A live question is always above a landing however
// old the landing is, because a landing costs nothing while it waits and a
// stopped conversation costs everything.
//
// THE DIFFERENCE BETWEEN THE TWO GROUPS IS SAID ONCE, ON THE GROUP'S OWN LINE:
// `to check · 8` at the left and `finished, nobody has checked it` at the right
// (placeprose.go's [needsCheckWord] and [needsCheckClause]). It used to be said
// under every landing row, which was the same nine words nine times and pushed
// `where you were` off a forty-row frame (owner, 2026-09-11).
//
// A LANDING IS ONE LINE AT REST AND TWO UNDER THE CURSOR. The second line is the
// first sentence of what the work came to and the two answers the task itself
// offers ([session.TaskAsk]), which is the whole of what "check it" means; the
// panel reserves the line it grows into ([homeCell.grows]) so the column does
// not move as the cursor walks.
//
// THE ANSWERS ARE DRAWN ON ONE ROW OF THE FRAME AND NOWHERE ELSE (law 7): the
// row under the cursor when it can take an answer, and the top row that can
// otherwise ([app.homeAnswerAt]). Drawing and routing ask that one function, so
// a `1` on the screen and the key a person presses cannot be two different rows.
type needsPanel struct{ homePanelBase }

const (
	// needsOpenWord is what a row says where its answers are not drawn: a
	// question with a paragraph, a landing this window has nowhere to leave an
	// answer for, any row that is not the frame's answering row. enter opens it.
	needsOpenWord = "enter"
	// needsAnswersCap is how many of a question's answers fit on its row. A
	// question with more draws the first ones and then [needsOpenWord], because
	// every answer is still one enter away.
	needsAnswersCap = 3
)

// needsItem is one row before the panel orders it: when it was asked, and the
// line.
type needsItem struct {
	asked time.Time
	line  homeLine
}

func (needsPanel) rows(in *homeGridInput) homePanelRows {
	asked := needsAsked(in)
	sort.SliceStable(asked, func(i, j int) bool { return attentionOlder(asked[i].asked, asked[j].asked) })
	// AND THE LANDINGS ARE NEWEST FIRST, which is the opposite order and the
	// right one for them: `needs you` is ranked by how long something has been
	// stopped, and nothing is stopped here — the freshest landing is the work
	// still in the person's head, and the oldest ages out of the group entirely
	// ([needsFresh]).
	calls := append([]needsItem(nil), in.calls...)
	sort.SliceStable(calls, func(i, j int) bool { return attentionOlder(calls[j].asked, calls[i].asked) })
	lines := make([]homeLine, 0, len(asked)+len(calls))
	for _, item := range append(asked, calls...) {
		lines = append(lines, item.line)
	}
	out := homePanelCut(panelNeeds, lines)
	// THE HEADING COUNTS THE QUESTIONS AND THE GROUP LINE COUNTS THE LANDINGS.
	// A frame holding only landings draws `needs you` with no count at all
	// rather than `needs you · 0`, which is the emptiness law applied to a
	// heading ([countWord]).
	out.said = countWord(len(asked))
	out.older = in.callsOlder
	if len(calls) > 0 {
		out.group = &homePanelGroup{at: len(asked), word: needsCheckWord,
			said: countWord(len(calls)), right: needsCheckClause}
	}
	return out
}

// needsFresh is the task calls that are still worth checking, and how many are
// history.
//
// A YOUR-CALL OLDER THAN [homeNeedsTaskFresh] IS HISTORY, NOT A QUESTION (owner,
// 2026-09-10: a machine with twenty-four week-old landings drew `needs you · 24`
// over rows nobody was going to answer, and a live question arriving under them
// would have been the twenty-fifth). They stay one door away — the fold counts
// them into the tasks place, where every one of them still is. Only a task's
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
// has no question object to write for it, so such a session's presence says
// `waiting on you · your call on <title>` with [session.PresenceQuestion] left
// zero. Drawn as a question, that is the same piece of work twice on one panel:
// once under the CONVERSATION's name with a bare `enter`, and once under the
// TASK's name with its files and its two answers. The second row is the better
// one and this drops the first (the spec of record's "said once", applied inside
// the panel as well as across the columns).
func needsAsked(in *homeGridInput) []needsItem {
	var items []needsItem
	for _, row := range in.rows {
		if !row.needs || needsOnlyItsOwnLanding(in, row) {
			continue
		}
		cell := &homeCell{panel: panelNeeds, mark: cellMarkNeeds, title: row.title, subRight: needsOpenWord}
		homeLiveMargin(cell, row, sinceAt(row.at, in.now))
		item := needsItem{asked: row.at}
		switch row.kind {
		case switcherConversation:
			head, whole := needsSentence(row.session)
			cell.sub = head
			if _, ok := answerable(row.session, in.now); ok && whole {
				cell.answers = answersWord(row.session.Presence.Question)
			}
		case switcherStanding:
			cell.sub = switcherFirstLine(row.item.Item.NeedsPerson)
		}
		item.line = switcherRowLine(row, cell)
		items = append(items, item)
	}
	return items
}

// needsOnlyItsOwnLanding reports that this row is a conversation whose presence
// offers NO question of its own and that has a landing in the `to check` group,
// which together mean the only thing it is waiting on is that landing.
//
// BOTH HALVES MATTER. A conversation with a question on its desk always writes
// the question object, so a missing one is the engine saying "nothing is being
// asked here"; and without a landing of its own on the screen there would be
// nothing left to say it, so the row stays.
func needsOnlyItsOwnLanding(in *homeGridInput, row switcherRow) bool {
	if row.kind != switcherConversation || row.session.Presence.Question.Kind != "" {
		return false
	}
	for _, call := range in.calls {
		if call.line.task != nil && call.line.task.SessionID == row.session.ID {
			return true
		}
	}
	return false
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
// THREE READERS AND ONE ANSWER. The `to check` group draws these rows, `since
// you left` drops the landings this group is already drawing ([leftPanel.rows]),
// and the pulse counts them beside the questions ([machineCounts]). A second
// spelling of this test anywhere would be a `3 want you` over four rows, which
// is the exact failure the one-reader law exists for.
//
// A LANDING THE CONVERSATION IS ITSELF ASKING ABOUT IS NOT ONE OF THESE
// ([needsAskedInChat]): that is a stopped conversation, it is a `needs you` row,
// and it is the row that can take a digit.
func needsCallOf(row session.SessionRow, entry session.TaskIndexEntry) (session.TaskStatus, bool) {
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

// needsCall is one of those rows: the title, how many files it wrote and how
// long ago, and — under the cursor — what it came to and its two answers.
func needsCall(project session.Project, row session.SessionRow, entry session.TaskIndexEntry, status session.TaskStatus, now time.Time) needsItem {
	title := strings.TrimSpace(entry.Label)
	if title == "" {
		title = strings.TrimSpace(entry.Title)
	}
	asked := needsCallAt(entry)
	// A LANDING WEARS NO MARK (law 8). The amber `?` means a thing has stopped
	// and will not move until somebody answers it; a landing has already
	// finished, and a column of question marks over work that is DONE was the
	// screen saying the opposite of what was true.
	cell := &homeCell{panel: panelNeeds, title: title, right: needsCallFacts(entry, asked, now),
		key:   needsCallKey + entry.ID,
		grows: true, sub: needsCallSub(status), answers: needsCallAnswers(status)}
	line := homeLine{kind: homeSession, row: row, project: project.Name,
		dir: homeBucketOf(row.Transcript), task: &entry, cell: cell}
	return needsItem{asked: asked, line: line}
}

// needsCallSub is the line a landing grows under the cursor: the first sentence
// of what the work came to, and the engine's own reason for the call where the
// work left no report.
func needsCallSub(status session.TaskStatus) string {
	if sub := switcherFirstLine(status.Reason); sub != "" {
		return sub
	}
	return status.Word
}

// needsCallFacts is a landing's right margin: how many files it wrote and how
// long ago, as ONE clause — `3 files · 1d`.
//
// IT IS ONE CLAUSE AND NOT TWO FACTS SIDE BY SIDE because they are read as one
// sentence about the same piece of work, and because a row cut between them
// would leave `3 files` with no age beside a column of rows that all have one.
// A landing that wrote no files says NOTHING where the count would be, never
// `0 files` (the emptiness law).
func needsCallFacts(entry session.TaskIndexEntry, asked, now time.Time) string {
	age := sinceAt(asked, now)
	if entry.FilesChanged <= 0 {
		return age
	}
	files := itoa(entry.FilesChanged) + plural(" file", entry.FilesChanged)
	if age == "" {
		return files
	}
	return files + rowSep + age
}

// needsCallAnswers is the two answers a landing offers, IN THE TASK'S OWN WORDS
// AND ON TASK-STATES' OWN KEYS.
//
// THE WORDS ARE THE ASK'S ([session.TaskAsk.Yes] and .No, filled in by the
// engine for the row's own shape — `accept` / `not right` for work nobody could
// check, `resolve it` for a branch that would not fasten) and this file spells
// none of them. THE KEYS ARE [session.LandingYesKey] AND [session.LandingNoKey],
// letter for letter, because a landing is answered with the same two keys
// wherever it is drawn (answers.go states the law from the writing end) and a
// surface that renumbered them to `1` and `2` would be teaching a person a key
// that does not work on the record they open next.
func needsCallAnswers(status session.TaskStatus) string {
	yes, no := strings.TrimSpace(status.Ask.Yes), strings.TrimSpace(status.Ask.No)
	if yes == "" || no == "" {
		return ""
	}
	return session.LandingYesKey + " " + yes + homeCellGap + session.LandingNoKey + " " + no
}

// needsChecking is every landing the `to check` group is drawing, by the
// identity a `since you left` line carries for the same task
// (homepanel_left.go's [leftKey]) — so one piece of work is one row of home's
// column and never two.
func needsChecking(in *homeGridInput) map[string]bool {
	said := make(map[string]bool, len(in.calls))
	for _, item := range in.calls {
		if item.line.task != nil {
			said[item.line.task.SessionID+"/"+item.line.task.ID] = true
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
// [needsAnswersCap] with [needsOpenWord] after them.
func answersWord(question session.PresenceQuestion) string {
	var parts []string
	for i, chip := range answerChips(question) {
		if i == needsAnswersCap {
			parts = append(parts, needsOpenWord)
			break
		}
		parts = append(parts, chip.text)
	}
	return strings.Join(parts, homeCellGap)
}

// ── answering a landing from home ───────────────────────────────────────────

// needsLandingQuestion is the landing question a `to check` row stands for, as
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
		{Key: session.LandingYesKey, Label: yes},
		{Key: session.LandingNoKey, Label: no, Safe: true},
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
	yes = strings.TrimPrefix(parts[0], session.LandingYesKey+" ")
	no = strings.TrimPrefix(parts[1], session.LandingNoKey+" ")
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
	if label == "" {
		return nil, false
	}
	row := a.homeTrue(line.row)
	if _, sent := a.answerSent(row, question); sent {
		// ONE ANSWER PER LANDING, for [app.answerRowKey]'s reason: the first is
		// on its way and the row is still saying so.
		return nil, false
	}
	if a.answeringHere(row) {
		doors, ok := a.questionDoors()
		if !ok {
			return nil, false
		}
		answer := session.Answer{At: time.Now(), Kind: session.QuestionLanding, ID: question.ID,
			Key: key, Picked: []string{key}, DecidedBy: session.DecidedByPerson}
		if err := doors.ResolveQuestion(answer); err != nil {
			return nil, false
		}
		a.home.say(answerSentWord+label, "")
		return nil, true
	}
	dir := strings.TrimSpace(row.Dir)
	if a.leaveAnswer == nil || dir == "" {
		return nil, false
	}
	if err := a.leaveAnswer(dir, question.Kind, question.ID, key); err != nil {
		a.home.say(answerFailedWord, dir)
		return nil, true
	}
	a.rememberAnswered(dir, question, label)
	a.home.say(answerSentWord+label, "")
	return nil, true
}
