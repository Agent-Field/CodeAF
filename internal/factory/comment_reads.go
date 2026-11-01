package factory

import (
	"strings"
	"time"
)

// The outcomes a [CommentRead] can carry. They are wire words: the app keys
// the quiet line under a comment on them, so they never change once shipped.
//
// THE FIRST VERSION NEVER ADOPTS. Whether a comment narrows or widens the
// job's scope is a judgement this package cannot make deterministically, so a
// comment from someone with write access on a job with a run under way is put
// to the person as a question instead ([CommentAsked]). [CommentAdopted] is
// reserved for the manager's own reading once it is built and tested.
const (
	CommentAdopted = "adopted"
	CommentAsked   = "asked"
	CommentContext = "context"
	CommentNothing = "nothing"
)

// CommentReadsMost bounds the reads kept on a job, and commentsSeenMost the
// ids of comments considered. The floor keeps only the last few comments, so
// these bounds are generous; they exist so a busy issue cannot grow its job.
const (
	CommentReadsMost = 20
	commentsSeenMost = 50
	// commentQuoteMost is how much of a comment the manager's ask quotes.
	commentQuoteMost = 200
)

// IssueCommentMost is the most characters a person may post as one comment
// on a job's issue from the app. GitHub takes more; the bound keeps a paste
// from becoming an issue's whole history, and the floor keeps less of each
// comment it reads back anyway.
const IssueCommentMost = 8000

// CommentRead is one new comment on the job's issue and what the manager did
// with it: the outcome and the line the app shows under the comment
// (`Manager read this · <line>`).
type CommentRead struct {
	CommentID string    `json:"comment_id"`
	At        time.Time `json:"at"`
	Outcome   string    `json:"outcome"`
	Line      string    `json:"line"`
}

// commentRule is one row of the table [ReadComments] decides by: the first
// row whose test matches gives the outcome and the line.
type commentRule struct {
	matches func(Item, Comment) bool
	outcome string
	line    string
}

// commentRules is read top to bottom. A person without write access only
// informs the work; a job with no run under way has no manager reading; the
// rest is asked, never adopted silently (see [CommentAsked]).
var commentRules = []commentRule{
	{func(_ Item, c Comment) bool { return c.Tier == TierStranger }, CommentContext, "read as context · no write access to the repo"},
	{func(it Item, _ Comment) bool { return JobState(it) == "done" }, CommentNothing, "the job is finished · nothing to change"},
	{func(it Item, _ Comment) bool { return it.Stream == nil }, CommentNothing, "the job hasn't started · its plan reads the issue's comments"},
	{func(Item, Comment) bool { return true }, CommentAsked, "asked you in the manager chat whether the plan should change"},
}

// ReadComments keeps list as the item's comments and handles every comment
// that is new since the last read: one [CommentRead] each, and for an ask a
// manager line in the job's conversation. A nil list is not read yet and
// changes nothing. The factory's own comments are considered and given no
// read; a comment whose author's standing is not known yet waits.
func ReadComments(it *Item, list []Comment, now time.Time) {
	if list == nil {
		return
	}
	it.Comments = list
	if it.CommentsSeen == nil {
		it.CommentsSeen = []string{}
		for _, c := range list {
			if c.ID != "" {
				it.CommentsSeen = append(it.CommentsSeen, c.ID)
			}
		}
		return
	}
	for _, c := range list {
		if c.ID == "" || (c.Tier == "" && !c.ByFactory) || seenComment(*it, c.ID) {
			continue
		}
		it.CommentsSeen = append(it.CommentsSeen, c.ID)
		if !c.ByFactory {
			readComment(it, c, now)
		}
	}
	it.CommentsSeen = newest(it.CommentsSeen, commentsSeenMost)
	it.CommentReads = newest(it.CommentReads, CommentReadsMost)
}

func readComment(it *Item, c Comment, now time.Time) {
	for _, rule := range commentRules {
		if !rule.matches(*it, c) {
			continue
		}
		it.CommentReads = append(it.CommentReads, CommentRead{CommentID: c.ID, At: now, Outcome: rule.outcome, Line: rule.line})
		if rule.outcome == CommentAsked {
			// The ask is a manager line, never the job's needs-you question:
			// that question belongs to the run's own state machine, which a
			// GitHub read must not change under a running step.
			_ = MergeThread(it, []Message{{ID: "comment_" + c.ID, At: now, From: "manager", Text: commentAsk(c)}}, nil)
		}
		return
	}
}

// commentAsk is the manager's question about one comment.
func commentAsk(c Comment) string {
	quote := strings.Join(strings.Fields(c.Body), " ")
	if r := []rune(quote); len(r) > commentQuoteMost {
		quote = strings.TrimSpace(string(r[:commentQuoteMost])) + " …"
	}
	return "@" + c.Author + " commented on the issue: “" + quote + "”. I don't change the plan from a comment on my own. Should the plan change for it? Reply here with what to do."
}

func seenComment(it Item, id string) bool {
	for _, s := range it.CommentsSeen {
		if s == id {
			return true
		}
	}
	return false
}

func newest[T any](list []T, most int) []T {
	if len(list) > most {
		return append([]T(nil), list[len(list)-most:]...)
	}
	return list
}
