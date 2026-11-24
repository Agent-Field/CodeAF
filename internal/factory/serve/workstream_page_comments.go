package serve

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ── COMMENTS ON A PASSAGE OF A PAGE ────────────────────────────────────────
//
// The routes of api/workstream_page_comments.go, and the desk's Personal
// rules (crates/workstreams comments.rs) for an organization's workstream:
//
//   - VIEW READS, EDIT COMMENTS. Anyone who can see the workstream reads its
//     threads; anyone who can edit it starts a thread, replies and resolves.
//     Seen is each person's own, so it needs only view.
//   - THE ANCHOR IS KEPT AS POSTED. The page is a live document this server
//     never parses, so a post is checked only against itself (the quote is the
//     words at offset in block_text), never against the page; the desk finds
//     the words again on every read.
//   - A REPLY OPENS A RESOLVED THREAD AGAIN, and a person's own comment is
//     read the moment they write it.
//
// Every change publishes workstream.changed with part "page" and part "files"
// (a document's facts count its open threads).

var (
	errCommentWords   = workstreamInvalid("a comment needs words")
	errCommentLong    = workstreamInvalid("a comment is at most 16384 characters")
	errCommentQuote   = workstreamInvalid("a comment hangs on 1–4000 bytes of the page's words")
	errCommentBlock   = workstreamInvalid("block is the path of the paragraph, heading or code block the words are in")
	errCommentPassage = workstreamInvalid("the quoted words are not the words at that offset in block_text")
)

func (h *Handler) workstreamPageCommentRoutes() {
	h.route("/v1/workstreams/{id}/pages/{pid}/comments", map[string]http.HandlerFunc{http.MethodGet: h.listPageComments, http.MethodPost: h.postPageComment})
	h.route("/v1/workstreams/{id}/comments/for_you", map[string]http.HandlerFunc{http.MethodGet: h.pageCommentsForYou})
	h.route("/v1/workstreams/{id}/comments/{cid}/reply", map[string]http.HandlerFunc{http.MethodPost: h.replyPageComment})
	h.route("/v1/workstreams/{id}/comments/{cid}/resolve", map[string]http.HandlerFunc{http.MethodPost: h.resolvePageComment})
	h.route("/v1/workstreams/{id}/comments/{cid}/seen", map[string]http.HandlerFunc{http.MethodPost: h.seenPageComment})
}

// commentRefused writes the refusal a thread change ended with.
func (h *Handler) commentRefused(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, store.ErrPageCommentGone):
		h.fail(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrPageCommentsFull):
		h.fail(w, http.StatusRequestEntityTooLarge, err.Error())
	default:
		return h.workstreamRefused(w, err)
	}
	return true
}

// commentWords is a comment's text as kept: trimmed, not empty, not too long.
func commentWords(text string) (string, error) {
	text = strings.TrimSpace(text)
	switch {
	case text == "":
		return "", errCommentWords
	case len(text) > factory.PageCommentMost:
		return "", errCommentLong
	}
	return text, nil
}

// publishComments tells everyone in ws that page's threads changed, and that
// the document's facts (its open threads) did.
func (h *Handler) publishComments(by string, ws factory.OrgWorkstream, page string, files bool) {
	h.publishWorkstreamPart(ws, api.PartPage, page, by, 0, false)
	if !files {
		return
	}
	fid := ""
	if all, err := h.config.Store.WorkstreamFiles(ws.ID); err == nil {
		for id, f := range all {
			if f.Kind == factory.FileDocument && f.PageID == page {
				fid = id
			}
		}
	}
	h.publishWorkstreamPart(ws, api.PartFiles, fid, by, 0, false)
}

// listPageComments answers a page's open threads, or with ?resolved=1 every
// one, each marked unread for the caller.
func (h *Handler) listPageComments(w http.ResponseWriter, r *http.Request) {
	ws, page, v, ok := h.pageOf(w, r, false)
	if !ok {
		return
	}
	all, err := h.config.Store.PageComments(ws.ID, page.ID, v.Handle)
	if h.failed(w, err) {
		return
	}
	out := api.PageComments{Threads: []api.PageCommentThread{}}
	resolved := r.URL.Query().Get("resolved") == "1"
	for _, t := range all {
		if resolved || t.Resolved == nil {
			out.Threads = append(out.Threads, t)
		}
	}
	h.write(w, http.StatusOK, out)
}

// postPageComment starts a thread on the words the caller selected.
func (h *Handler) postPageComment(w http.ResponseWriter, r *http.Request) {
	ws, page, v, ok := h.pageOf(w, r, true)
	if !ok {
		return
	}
	var body api.PageCommentNew
	if !h.decode(w, r, &body, false) {
		return
	}
	// THE AGENT POINTS AT WORDS, IT SELECTS NOTHING: it reads the page's
	// Markdown, never the editor's blocks, so its thread has no block path and
	// its block text is the quote itself. The desk finds the words on the live
	// page the same way it finds every thread's (the one place that says them).
	_, agent := afiCallOf(r)
	text, err := commentWords(body.Text)
	switch {
	case err != nil:
	case strings.TrimSpace(body.Quote) == "" || len(body.Quote) > factory.PageCommentQuoteMost:
		err = errCommentQuote
	case (len(body.Block) == 0 && !agent) || len(body.Block) > factory.PageCommentPathMost || len(body.BlockText) > factory.PageCommentBlockMost:
		err = errCommentBlock
	case !factory.PageCommentPassage(body.BlockText, body.Offset, body.Quote):
		err = errCommentPassage
	}
	if h.commentRefused(w, err) {
		return
	}
	now := h.clock().UTC().Format(time.RFC3339Nano)
	t := factory.PageCommentThread{
		ID: h.newTaskID("pc_")(), WS: ws.ID, PageID: page.ID,
		Block: body.Block, BlockText: body.BlockText, Offset: body.Offset, Quote: body.Quote,
	}
	t.Comments = []factory.PageComment{{ID: t.ID, By: h.taskBy(r, v), Text: text, At: now}}
	err = h.config.Store.EditPageComments(func(cw store.PageCommentWrite) error {
		if err := cw.Add(t); err != nil {
			return err
		}
		return readByPerson(r, cw, t, v)
	})
	if h.commentRefused(w, err) {
		return
	}
	h.publishComments(h.orgActor(r), ws, page.ID, true)
	h.write(w, http.StatusCreated, t)
}

// editThread runs change on the thread the path names, as the caller, and
// answers it as it now stands. write is whether the change needs edit;
// change says whether it changed anything that must be told.
func (h *Handler) editThread(w http.ResponseWriter, r *http.Request, write bool, status int, change func(t *factory.PageCommentThread, v wsViewer, cw store.PageCommentWrite) (bool, error)) {
	var ws factory.OrgWorkstream
	var v wsViewer
	var ok bool
	if write {
		ws, v, ok = h.writablePart(w, r)
	} else {
		ws, v, ok = h.findWorkstream(w, r)
	}
	if !ok {
		return
	}
	var out factory.PageCommentThread
	var changed bool
	err := h.config.Store.EditPageComments(func(cw store.PageCommentWrite) error {
		t, err := cw.Thread(ws.ID, r.PathValue("cid"), v.Handle)
		if err != nil {
			return err
		}
		if !pageLive(ws, t.PageID) {
			return store.ErrPageCommentGone
		}
		if changed, err = change(&t, v, cw); err != nil || !changed {
			out = t
			return err
		}
		if err := cw.Put(t); err != nil {
			return err
		}
		out, err = cw.Thread(ws.ID, t.ID, v.Handle)
		return err
	})
	if h.commentRefused(w, err) {
		return
	}
	if changed {
		h.publishComments(h.orgActor(r), ws, out.PageID, write)
	}
	h.write(w, status, out)
}

// readByPerson marks t read for the person who wrote in it. What the
// workstream's agent writes for them is NOT their own words, so it leaves what
// they have read alone: the agent's answer, and anything before it, is still
// for them to read.
func readByPerson(r *http.Request, cw store.PageCommentWrite, t factory.PageCommentThread, v wsViewer) error {
	if _, agent := agentCallOf(r); agent {
		return nil
	}
	return cw.Read(t, v.Handle)
}

// pageLive is whether ws still has page: a deleted page's threads go with it.
func pageLive(ws factory.OrgWorkstream, page string) bool {
	for _, p := range ws.Pages {
		if p.ID == page {
			return true
		}
	}
	return false
}

// replyPageComment says something more in a thread, opening it again when it
// was resolved.
func (h *Handler) replyPageComment(w http.ResponseWriter, r *http.Request) {
	var body api.PageCommentReply
	if !h.decode(w, r, &body, false) {
		return
	}
	text, err := commentWords(body.Text)
	if h.commentRefused(w, err) {
		return
	}
	h.editThread(w, r, true, http.StatusCreated, func(t *factory.PageCommentThread, v wsViewer, cw store.PageCommentWrite) (bool, error) {
		t.Comments = append(t.Comments, factory.PageComment{ID: h.newTaskID("pc_")(), By: h.taskBy(r, v), Text: text, At: h.clock().UTC().Format(time.RFC3339Nano)})
		t.Resolved = nil
		return true, readByPerson(r, cw, *t, v)
	})
}

// resolvePageComment resolves a thread, or with reopen opens it again.
func (h *Handler) resolvePageComment(w http.ResponseWriter, r *http.Request) {
	var body api.PageCommentResolve
	if !h.decode(w, r, &body, true) {
		return
	}
	h.editThread(w, r, true, http.StatusOK, func(t *factory.PageCommentThread, v wsViewer, _ store.PageCommentWrite) (bool, error) {
		switch {
		case body.Reopen && t.Resolved != nil:
			t.Resolved = nil
		case !body.Reopen && t.Resolved == nil:
			t.Resolved = &factory.PageCommentResolved{By: h.taskBy(r, v).Name, At: h.clock().UTC().Format(time.RFC3339Nano)}
		default:
			return false, nil
		}
		return true, nil
	})
}

// seenPageComment records that the caller read a thread: what was said in it
// until now is no longer for them.
func (h *Handler) seenPageComment(w http.ResponseWriter, r *http.Request) {
	h.editThread(w, r, false, http.StatusOK, func(t *factory.PageCommentThread, v wsViewer, cw store.PageCommentWrite) (bool, error) {
		if !t.Unread {
			return false, nil
		}
		return true, cw.Read(*t, v.Handle)
	})
}

// pageCommentsForYou answers every open thread in the workstream with
// something in it the caller has not read, on pages it still has.
func (h *Handler) pageCommentsForYou(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	all, err := h.config.Store.UnreadPageComments(ws.ID, v.Handle)
	if h.failed(w, err) {
		return
	}
	out := api.PageComments{Threads: []api.PageCommentThread{}}
	for _, t := range all {
		if pageLive(ws, t.PageID) {
			out.Threads = append(out.Threads, t)
		}
	}
	h.write(w, http.StatusOK, out)
}
