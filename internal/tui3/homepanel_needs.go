package tui3

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// needsPanel is `needs you` (docs/design/home-mission-control/DESIGN.md §1, §3
// P1): every question on the machine that is waiting on a person, the longest
// wait first (homeattention.go's [attentionOlder]).
//
// THREE SOURCES AND ONE LIST. A conversation stopped on a question, a task the
// record marks as the person's call, and a watch that needs somebody are three
// different engines asking the same thing, and a person answering them does not
// care which engine it was. Each row is two lines: the title with its age at the
// right, and under it the question's own sentence.
//
// THE ANSWERS ARE DRAWN ON THE TOP ANSWERABLE ROW AND NOWHERE ELSE (law 7). A
// digit pressed anywhere on home answers that row ([app.homeGridAnswer]), so the
// chips beside it are the key a person is about to press. Chips on a second row
// would be a `1` on screen twice with one meaning, which is a guess; every other
// row says `enter`, and enter opens it.
type needsPanel struct{ homePanelBase }

const (
	// needsOpenWord is what a row says where its answers are not drawn: a
	// question with a paragraph, a task's call, a watch's question, any row
	// below the top one. enter opens it.
	needsOpenWord = "enter"
	// needsAnswersCap is how many of a question's answers fit on its row. A
	// question with more draws the first ones and then [needsOpenWord], because
	// every answer is still one enter away.
	needsAnswersCap = 3
)

// needsItem is one row before the panel orders it: when it was asked, the line,
// and the answers it would draw if it were the top answerable row.
type needsItem struct {
	asked   time.Time
	line    homeLine
	answers string
}

func (needsPanel) rows(in *homeGridInput) homePanelRows {
	items := append(needsAsked(in), needsCalls(in)...)
	sort.SliceStable(items, func(i, j int) bool { return attentionOlder(items[i].asked, items[j].asked) })
	lines := make([]homeLine, 0, len(items))
	drawn := false
	for _, item := range items {
		item.line.cell.subRight = needsOpenWord
		if item.answers != "" && !drawn {
			item.line.cell.subRight, drawn = item.answers, true
		}
		lines = append(lines, item.line)
	}
	out := homePanelCut(panelNeeds, lines)
	out.said = countWord(len(lines))
	return out
}

// needsAsked is the conversations and watches the switcher already ranks as
// waiting on somebody.
func needsAsked(in *homeGridInput) []needsItem {
	var items []needsItem
	for _, row := range in.rows {
		if !row.needs {
			continue
		}
		cell := &homeCell{panel: panelNeeds, mark: cellMarkNeeds, title: row.title}
		homeLiveMargin(cell, row, sinceAt(row.at, in.now))
		item := needsItem{asked: row.at}
		switch row.kind {
		case switcherConversation:
			head, whole := needsSentence(row.session)
			cell.sub = head
			if _, ok := answerable(row.session, in.now); ok && whole {
				item.answers = answersWord(row.session.Presence.Question)
			}
		case switcherStanding:
			cell.sub = switcherFirstLine(row.item.Item.NeedsPerson)
		}
		item.line = switcherRowLine(row, cell)
		items = append(items, item)
	}
	return items
}

// needsCalls is every task the project's record marks as the person's call —
// work that finished and nobody could check, a landing that turned the work
// back, a branch that would not fasten. They are read off the world's own index
// rows through the one reading of a row ([taskEntryStatus]), so this panel and
// the tasks place can never disagree about which work is waiting.
//
// ENTER OPENS THE CONVERSATION ON THE TASK'S RECORD ([app.homeLandOnTask]),
// which is where the question and its two answers are.
func needsCalls(in *homeGridInput) []needsItem {
	var items []needsItem
	for _, project := range in.world.Projects {
		for _, row := range project.Sessions {
			for i := range row.Tasks.Rows {
				entry := row.Tasks.Rows[i]
				status := taskEntryStatus(entry, row.Runs(entry))
				if !status.Attention || status.On != session.TaskWaitPerson || needsAskedInChat(row, entry) {
					continue
				}
				items = append(items, needsCall(project, row, entry, status, in.now))
			}
		}
	}
	return items
}

// needsCall is one of those rows.
func needsCall(project session.Project, row session.SessionRow, entry session.TaskIndexEntry, status session.TaskStatus, now time.Time) needsItem {
	title := strings.TrimSpace(entry.Label)
	if title == "" {
		title = strings.TrimSpace(entry.Title)
	}
	sub := switcherFirstLine(status.Reason)
	if sub == "" {
		sub = status.Word
	}
	asked := entry.EndedAt
	if asked.IsZero() {
		asked = entry.StartedAt
	}
	cell := &homeCell{panel: panelNeeds, mark: cellMarkNeeds, title: title, right: sinceAt(asked, now), sub: sub,
		key: needsCallKey + entry.ID}
	line := homeLine{kind: homeSession, row: row, project: project.Name,
		dir: homeBucketOf(row.Transcript), task: &entry, cell: cell}
	return needsItem{asked: asked, line: line}
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
