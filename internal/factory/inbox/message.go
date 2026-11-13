package inbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ErrNoJob is a (source, id) the inbox never filed.
var ErrNoJob = errors.New("no job was filed from that source with that id")

// Refused is a message turned away for its own sake (empty, too long, a bad
// author), in the words the sender acts on.
type Refused struct{ Words string }

func (r Refused) Error() string { return r.Words }

// MessagesKept is how many of a job's last comments the floor keeps when a
// message arrives. IT IS THE GITHUB POLL'S OWN FIGURE (github.CommentsMost,
// pinned equal by a test), so a job filed from outside carries exactly as much
// of what was said as an issue does.
const MessagesKept = 3

// Find is the job the inbox filed from source with id.
func Find(st *store.Store, source, id string) (factory.Item, error) {
	if st == nil {
		return factory.Item{}, errors.New("the factory keeps no store here")
	}
	ref := Ref(source, id)
	have, err := st.ListSaved()
	if err != nil {
		return factory.Item{}, err
	}
	for _, it := range have {
		if it.SourceRef == ref {
			return it, nil
		}
	}
	return factory.Item{}, ErrNoJob
}

// Said is one message from outside about a job the inbox filed.
type Said struct {
	Source string
	ID     string
	// Author is who said it on the source's side; empty is the source itself.
	Author string
	Text   string
	// Trust is the most the sender vouches for; the message is read at the
	// LOWER of it and the job's own tier, never above either.
	Trust factory.Tier
}

// Heard is what [Message] did: the job it reached and what the job's manager
// made of the words ([factory.CommentRead]'s outcome and line).
type Heard struct {
	Job     int
	Outcome string
	Line    string
}

// Message gives s to the job (s.Source, s.ID) filed AS A COMMENT ON IT IS
// GIVEN: kept among the job's last comments, so the next step's brief and the
// next manager read it, and handled by [factory.ReadComments], the one path
// every comment takes. A stranger's words are read as context only; anyone
// else's on a job with a run under way are put to the person in the manager
// chat. The source is then told what the manager made of it ([EventMessage]).
//
// It refuses an empty or over-long text in [factory.IssueCommentBounds]'s
// words ([Refused]), and a pair the inbox never filed with [ErrNoJob].
func Message(ctx context.Context, st *store.Store, s Said, now time.Time) (Heard, error) {
	text := strings.TrimSpace(s.Text)
	if text == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > factory.IssueCommentMost {
		return Heard{}, Refused{factory.IssueCommentBounds}
	}
	author, err := line("author", s.Author, MaxAuthor, false)
	if err != nil {
		return Heard{}, Refused{err.Error()}
	}
	if author == "" {
		author = s.Source
	}
	if err := ctx.Err(); err != nil {
		return Heard{}, err
	}
	it, err := Find(st, s.Source, s.ID)
	if err != nil {
		return Heard{}, err
	}
	id, err := messageID()
	if err != nil {
		return Heard{}, err
	}
	var heard Heard
	err = st.Update(it.ID, func(cur *factory.Item) error {
		tier := cur.Tier
		if tier == "" || (s.Trust != "" && lowers(tier, s.Trust)) {
			tier = s.Trust
		}
		if tier == "" {
			tier = factory.TierStranger
		}
		// A job the inbox filed never had a GitHub read, so nothing before
		// this message is "already seen": an empty list (not nil) tells
		// ReadComments to handle what is new rather than take it all as read.
		if cur.CommentsSeen == nil {
			cur.CommentsSeen = []string{}
		}
		c := factory.Comment{ID: id, Author: author, Body: text, At: now, Tier: tier}
		list := append(append([]factory.Comment{}, cur.Comments...), c)
		if len(list) > MessagesKept {
			list = list[len(list)-MessagesKept:]
		}
		factory.ReadComments(cur, list, now)
		cur.Note(now, "message from "+s.Source)
		heard = Heard{Job: cur.ID}
		for _, r := range cur.CommentReads {
			if r.CommentID == id {
				heard.Outcome, heard.Line = r.Outcome, r.Line
			}
		}
		return nil
	})
	if err != nil {
		return Heard{}, err
	}
	Notify(ctx, st, heard.Job, Event{Kind: EventMessage, Text: "codeaf read your message · " + heard.Line})
	return heard, nil
}

// messageID names one message the way a comment is named: unique on the job,
// and never a GitHub comment's number.
func messageID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("could not name the message: %w", err)
	}
	return "msg_" + hex.EncodeToString(b[:]), nil
}
