package api

import "github.com/Agent-Field/codeaf/internal/factory"

// ── COMMENTS ON A PASSAGE OF A PAGE, ON THE WIRE ───────────────────────────
//
//	GET  /v1/workstreams/{id}/pages/{pid}/comments[?resolved=1]  -> PageComments: open threads, or every one
//	POST /v1/workstreams/{id}/pages/{pid}/comments                PageCommentNew -> PageCommentThread (201)
//	POST /v1/workstreams/{id}/comments/{cid}/reply                PageCommentReply -> PageCommentThread (201)
//	POST /v1/workstreams/{id}/comments/{cid}/resolve              PageCommentResolve -> PageCommentThread
//	POST /v1/workstreams/{id}/comments/{cid}/seen                 -> PageCommentThread
//	GET  /v1/workstreams/{id}/comments/for_you                    -> PageComments: open threads unread by the caller
//
// A thread's own routes name only the workstream: a thread is found by its
// id, the way the desk's Personal calls name only the thread.
//
// Reading needs view on the workstream; posting, replying and resolving need
// edit, and an archived workstream takes none of them. Seen is the caller's
// own and needs only view. The server keeps the anchor exactly as posted and
// checks only that the quote is the words at offset in block_text: where the
// words are now is the desk's to find (factory/workstream_page_comment.go).
// A reply to a resolved thread opens it again. Every change but seen
// publishes workstream.changed with part "page" (the page's id) and part
// "files", because a document's facts count its open threads; seen publishes
// "page" alone, and only when something was unread.

// The thread shapes are the factory's own.
type (
	PageCommentThread = factory.PageCommentThread
	PageCommentNew    = factory.PageCommentNew
)

// PageComments is a list of threads, oldest first.
type PageComments struct {
	Threads []PageCommentThread `json:"threads"`
}

// PageCommentReply is something more said in a thread.
type PageCommentReply struct {
	Text string `json:"text"`
}

// PageCommentResolve resolves a thread, or with Reopen opens it again.
type PageCommentResolve struct {
	Reopen bool `json:"reopen,omitempty"`
}
