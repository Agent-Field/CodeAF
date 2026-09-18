package tui3

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

type questionReplacer interface {
	ReplaceQuestion(context.Context, session.Answer) (<-chan session.Event, error)
}

// replaceQuestion enters the ordinary submission path after the engine has
// stopped the old turn. The transcript retains its stopped work and the new ask.
func (a *app) replaceQuestion(q questionShown, words string) tea.Cmd {
	door, ok := a.agent.(questionReplacer)
	if !ok {
		a.note("this connection cannot replace a pending request")
		return nil
	}
	answer := a.dressAnswer(q, session.Answer{Change: words})
	ctx := a.ctx
	return a.offLoop(func() func(bool) tea.Cmd {
		events, err := door.ReplaceQuestion(ctx, answer)
		return func(here bool) tea.Cmd {
			if !here {
				return nil
			}
			if err != nil {
				a.note(err.Error())
				return nil
			}
			a.resolveUnfinished()
			a.settleTurn()
			a.stream = nil
			a.turn++
			a.said(entry{kind: entryUser, text: words, turn: a.turn, began: a.now()})
			a.withdrawQuestion(q.question, "you gave an updated request")
			if a.qroom != nil && a.qroom.head.token() == q.token() {
				a.closeQuestionRoom()
			}
			return a.takeStream(events)
		}
	})
}

// discussionFeed keeps clarification deltas away from the original turn's live
// reply and tool identifiers. Entries are appended in arrival order and updated
// in place, so either conversation can continue while the other is waiting.
type discussionFeed struct {
	seq uint64

	feed      feed
	positions []int
}

func (a *app) discussionEvent(update *session.QuestionDiscussion) tea.Cmd {
	if update == nil {
		return nil
	}
	if a.discussionFeeds == nil {
		a.discussionFeeds = make(map[string]*discussionFeed)
	}
	d := a.discussionFeeds[update.ID]
	if d == nil {
		d = &discussionFeed{feed: newFeed(feedHooks{now: a.now})}
		d.feed.turn = a.turn
		a.discussionFeeds[update.ID] = d
	}
	if update.Seq > 0 && update.Seq <= d.seq {
		return nil
	}
	d.seq = update.Seq
	if update.Words != "" {
		d.feed.said(entry{kind: entryUser, text: "clarify: " + update.Words, turn: a.turn, began: a.now()})
	}
	if update.Event != nil {
		ev := *update.Event
		if update.Error != "" {
			ev.Err = errors.New(update.Error)
		}
		d.feed.ingest(ev)
		switch ev.Kind {
		case session.EventConsentRequest:
			for i := range d.feed.entries {
				if d.feed.entries[i].callID == ev.CallID {
					d.feed.entries[i].status = toolConsent
				}
			}
		case session.EventError:
			d.feed.note(errText(ev.Err))
			d.feed.closeLive()
			d.feed.resolveUnfinished()
		case session.EventTurnDone:
			d.feed.closeLive()
			d.feed.resolveUnfinished()
		}
	}
	for i, e := range d.feed.entries {
		e.discussionID, e.discussionIndex = update.ID, i
		if e.callID != "" {
			e.callID = update.ID + "/" + e.callID
		}
		if i < len(d.positions) && d.positions[i] < len(a.entries) && a.entries[d.positions[i]].discussionID == update.ID && a.entries[d.positions[i]].discussionIndex == i {
			a.entries[d.positions[i]] = e
		} else {
			if i < len(d.positions) {
				d.positions[i] = len(a.entries)
			} else {
				d.positions = append(d.positions, len(a.entries))
			}
			a.entries = append(a.entries, e)
		}
	}
	a.follow()
	a.touch()
	return nil
}
