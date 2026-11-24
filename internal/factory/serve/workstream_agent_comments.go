package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── THE AGENT AND THE COMMENTS ON A DOCUMENT'S PASSAGES (F2 slice 4) ───────
//
// ws_file_read hands the agent every thread on a document, and ws_file_comment
// lets it start one on quoted words or answer one, through the same routes a
// person's desk uses (workstream_page_comments.go), as Afi for the person. A
// COMMENT IS ANSWERED IN ITS THREAD, never by changing the document without a
// word: the agent changes words only by suggestion.

// agentCommentsMost is the most threads one ws_file_read hands the agent.
const agentCommentsMost = 100

// agentThread is one thread as the model reads it: its id (to answer it), the
// words it was posted on, who said what, and whether it is resolved.
type agentThread struct {
	Thread   string         `json:"thread"`
	Quote    string         `json:"quote"`
	Comments []agentComment `json:"comments"`
	Resolved string         `json:"resolved,omitempty"`
}

type agentComment struct {
	By   string `json:"by"`
	Text string `json:"text"`
	At   string `json:"at"`
}

// passageComments are page's threads for the model, open ones first.
func (d *agentDoor) passageComments(ws, page string) ([]agentThread, error) {
	all, err := d.h.config.Store.PageComments(ws, page, d.asker)
	if err != nil {
		return nil, err
	}
	out := []agentThread{}
	for _, open := range []bool{true, false} {
		for _, t := range all {
			if (t.Resolved == nil) != open || len(out) >= agentCommentsMost {
				continue
			}
			a := agentThread{Thread: t.ID, Quote: t.Quote, Comments: []agentComment{}}
			for _, c := range t.Comments {
				a.Comments = append(a.Comments, agentComment{By: c.By.Name, Text: c.Text, At: c.At})
			}
			if t.Resolved != nil {
				a.Resolved = "by " + t.Resolved.By
			}
			out = append(out, a)
		}
	}
	return out, nil
}

func (d *agentDoor) wsFileComment(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		File   string `json:"file"`
		Quote  string `json:"quote"`
		Thread string `json:"thread"`
		Text   string `json:"text"`
	}
	if err := agentArgs(args, &a); err != nil {
		return nil, err
	}
	text, err := agentWords(a.Text, "text", 4000)
	if err != nil {
		return nil, err
	}
	f, ws, err := d.fileOf(ctx, strings.TrimSpace(a.File), false)
	if err != nil {
		return nil, err
	}
	if f.Kind != factory.FileDocument || f.PageID == "" {
		return nil, errors.New("only a document takes comments on its passages")
	}
	page, ok := pageOf(ws, f.PageID)
	if !ok {
		return nil, errPageGone
	}
	var t api.PageCommentThread
	if thread := strings.TrimSpace(a.Thread); thread != "" {
		threads, err := d.h.config.Store.PageComments(ws.ID, page.ID, d.asker)
		if err != nil {
			return nil, err
		}
		found := false
		for _, x := range threads {
			found = found || x.ID == thread
		}
		if !found {
			return nil, fmt.Errorf("%q has no comment thread %q; ws_file_read lists them", f.Name, thread)
		}
		raw, err := d.call(ctx, d.h.replyPageComment, http.MethodPost, "/v1/workstreams/"+d.id+"/comments/"+thread+"/reply",
			map[string]string{"cid": thread}, api.PageCommentReply{Text: text}, -1)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, err
		}
		return map[string]any{"said": "Answered the comment on " + strconv.Quote(cutRunes(t.Quote, 80)) + " in " + strconv.Quote(page.Title) + "; the document did not change.", "thread": t.ID}, nil
	}
	quote := a.Quote
	if strings.TrimSpace(quote) == "" || len(quote) > factory.PageCommentQuoteMost {
		return nil, errors.New("invalid arguments: quote the words the comment is on, or name the thread it answers")
	}
	switch n := strings.Count(page.BodyMD, quote); {
	case n == 0:
		return nil, errors.New("the document does not say those words in one paragraph, heading or code block; quote them exactly as it says them")
	case n > 1:
		return nil, errors.New("the document says those words more than once; quote a few more words so they name one place")
	}
	raw, err := d.call(ctx, d.h.postPageComment, http.MethodPost, "/v1/workstreams/"+d.id+"/pages/"+page.ID+"/comments",
		map[string]string{"pid": page.ID}, api.PageCommentNew{Block: []uint32{}, BlockText: quote, Quote: quote, Text: text}, -1)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, err
	}
	return map[string]any{"said": "Commented on " + strconv.Quote(cutRunes(quote, 80)) + " in " + strconv.Quote(page.Title) + "; the document did not change.", "thread": t.ID}, nil
}
