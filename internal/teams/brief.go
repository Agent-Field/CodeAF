package teams

// brief.go is a member's brief changed in place.
//
// A MEMBER'S BRIEF IS ITS STANDING ASSIGNMENT, and a person or its manager may
// rewrite it without a restart and without a message in its conversation. The
// change is two writes under the teams lock: a [KindBrief] line in the team's
// log, addressed to the member, which the member's session reads at the start
// of its next step like any delivered line (it wakes nobody); and a
// [BriefRevision] on the membership, which is the change's durable record —
// who, when, the words before and after — so any reader (the terminal, the
// app, a report) can say "edited by" and show the change after a restart
// without reading the log. A membership with no revisions was never edited
// here; nothing is ever inferred for one.
//
// The brief replaces the one the member was started with, never a role it
// carries: a role is told to the member on its own and stays in force.

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// BriefRevisionsKept is how many changes a membership keeps, newest last.
const BriefRevisionsKept = 16

// BriefTextMax is the most characters a brief may have: all of it is what the
// member reads (the session delivers a brief up to this length, never cut).
const BriefTextMax = 12000

// BriefRevision is one change to a member's brief.
type BriefRevision struct {
	// Entry is the [KindBrief] line in the team's log that delivered it.
	Entry string `json:"entry,omitempty"`
	// Text is the brief from this change on; Before what it replaced ("" when
	// the brief it replaced was not known: a member nobody started). On disk
	// only the oldest kept revision stores Before; every later one's is the
	// revision before it ([Member.BriefRevisions] fills them in).
	Text   string `json:"text"`
	Before string `json:"before,omitempty"`
	// By is who changed it: [FromYou], [FromManager], or a member's handle;
	// ByKey that party's conversation key when it has one.
	By    string    `json:"by"`
	ByKey string    `json:"by_key,omitempty"`
	At    time.Time `json:"at"`
}

var (
	// ErrBriefUnchanged is a revision with the words the brief already has.
	ErrBriefUnchanged = errors.New("the brief already says that")
	// ErrBriefStale is a change made from a brief that has changed since.
	ErrBriefStale = errors.New("the brief was changed since it was read; read it again")
	// ErrBriefTooLong is a brief longer than [BriefTextMax].
	ErrBriefTooLong = fmt.Errorf("a brief can be at most %d characters", BriefTextMax)
	// ErrBriefFactoryStage is a brief owned by a factory job, checked under the write lock.
	ErrBriefFactoryStage = errors.New("a factory stage brief belongs to its job")
	// ErrBriefNoMember is a change for a conversation that is not a named member.
	ErrBriefNoMember = errors.New("that conversation is not a named member of the team")
)

// BriefChange is one change asked of a member's brief.
type BriefChange struct {
	// By and ByKey are who changes it (see [BriefRevision]).
	By, ByKey string
	Text      string
	// Before is the brief as the caller read it, used only when the
	// membership has no revision yet (the start line's words); a membership
	// with revisions knows its own.
	Before string
	// Base, when set, is the revision the change was made from: the newest
	// revision's Entry, or "" for a brief never changed. A brief that changed
	// since is [ErrBriefStale] and nothing is written.
	Base *string
	At   time.Time
}

// ReviseBrief replaces the brief of member key in team and answers the
// revision recorded.
func ReviseBrief(profileDir, team, key string, c BriefChange) (BriefRevision, error) {
	text := strings.TrimSpace(c.Text)
	if text == "" {
		return BriefRevision{}, errors.New("a brief needs words")
	}
	if utf8.RuneCountInString(text) > BriefTextMax {
		return BriefRevision{}, ErrBriefTooLong
	}
	at := c.At
	if at.IsZero() {
		at = time.Now()
	}
	var rev BriefRevision
	err := withLock(profileDir, lockWait, func() error {
		f, _, _, err := read(profileDir)
		if err != nil {
			return err
		}
		if f == nil {
			return errors.New("there are no teams")
		}
		t, ok := f.Team(team)
		if !ok {
			return errors.New("there is no team " + team)
		}
		if t.Closed() {
			return ErrClosed
		}
		m, ok := t.Member(key)
		if !ok || m.Handle == "" {
			return ErrBriefNoMember
		}
		if m.FactoryStage {
			return ErrBriefFactoryStage
		}
		cur, changed := m.CurrentBrief()
		if c.Base != nil && *c.Base != cur.Entry {
			return ErrBriefStale
		}
		before := strings.TrimSpace(c.Before)
		if changed {
			before = cur.Text
		}
		if before == text {
			return ErrBriefUnchanged
		}
		id, err := appendTrafficIDLocked(profileDir, team, Entry{At: at, Kind: KindBrief, From: c.By, FromKey: c.ByKey, To: m.Handle, ToKey: m.Key, Member: m.Key, Text: text})
		if err != nil {
			return err
		}
		rev = BriefRevision{Entry: id, Text: text, Before: before, By: c.By, ByKey: c.ByKey, At: at.UTC()}
		_, err = updateLocked(profileDir, func(f *File) error {
			for i := range f.Teams {
				if f.Teams[i].ID != team {
					continue
				}
				for j := range f.Teams[i].Members {
					mm := &f.Teams[i].Members[j]
					if mm.Key != key {
						continue
					}
					stored := rev
					if len(mm.Briefs) > 0 {
						stored.Before = ""
					}
					mm.Briefs = append(mm.Briefs, stored)
					if over := len(mm.Briefs) - BriefRevisionsKept; over > 0 {
						kept := append([]BriefRevision(nil), mm.Briefs[over:]...)
						kept[0].Before = mm.Briefs[over-1].Text
						mm.Briefs = kept
					}
					return nil
				}
			}
			return ErrBriefNoMember
		})
		return err
	})
	return rev, err
}

// BriefRevisions are m's brief changes, oldest first, each with the words it
// replaced; nil when it never changed.
func (m Member) BriefRevisions() []BriefRevision {
	if len(m.Briefs) == 0 {
		return nil
	}
	out := append([]BriefRevision(nil), m.Briefs...)
	for i := 1; i < len(out); i++ {
		out[i].Before = out[i-1].Text
	}
	return out
}

// CurrentBrief is the newest revision of m's brief, and false when it was
// never changed (its brief is still its start line, [StartFor]).
func (m Member) CurrentBrief() (BriefRevision, bool) {
	n := len(m.Briefs)
	if n == 0 {
		return BriefRevision{}, false
	}
	cur := m.Briefs[n-1]
	if n > 1 {
		cur.Before = m.Briefs[n-2].Text
	}
	return cur, true
}
