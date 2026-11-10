package jira

import (
	"context"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory/inbox"
)

// Replier answers on the ticket a job came from: a comment with the pull
// request's link when it opens and when it merges, and the job's own words
// when it stops and needs a person, with or without a question. It is an
// [inbox.Replier]; every other moment posts nothing.
type Replier struct {
	Client *Client
}

var _ inbox.Replier = Replier{}

// Reply comments ev on ticket key. A moment with nothing to say (a pull
// request event with no link, a kind it does not know) posts nothing.
func (r Replier) Reply(ctx context.Context, key string, ev inbox.Event) error {
	text := ReplyText(ev)
	if text == "" {
		return nil
	}
	return r.Client.Comment(ctx, key, Doc(text))
}

// ReplyText is the comment for ev, or "" for none.
func ReplyText(ev inbox.Event) string {
	url := strings.TrimSpace(ev.URL)
	switch ev.Kind {
	case inbox.EventPROpened:
		if url == "" {
			return ""
		}
		return "codeaf opened a pull request: " + url
	case inbox.EventMerged:
		if url == "" {
			return ""
		}
		return "Merged: " + url
	case inbox.EventStuck, inbox.EventQuestion:
		return strings.TrimSpace(ev.Text)
	}
	return ""
}
