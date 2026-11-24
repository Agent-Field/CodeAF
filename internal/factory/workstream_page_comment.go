package factory

// ── A COMMENT ON A PASSAGE OF A DOCUMENT (Workstreams v5 final spec) ───────
//
// "Comments attach to passages and go to people." A thread hangs on the words
// someone selected on an organization workstream's page: the block's path
// through the editor's content arrays, that block's whole text when the
// comment was made, the code-point offset of the words in it, and the words
// themselves. These are the desk's Personal shapes (crates/workstreams
// comments.rs) field for field, so its screens never ask where a thread lives.
//
// THE SERVER KEEPS THE ANCHOR EXACTLY AS IT WAS POSTED AND NEVER CHECKS IT
// AGAINST THE PAGE. An organization's page is a live document the server
// never parses, and its Markdown mirror lags the words people are typing, so
// the server cannot know where the words are now. The desk, bound to the live
// document, finds them again on every read and decides whether the thread is
// still anchored; a thread whose words are gone is still listed, so nothing
// anyone said is lost with the passage.
//
// A COMMENT NEVER CHANGES THE PAGE. It is said to people, never an
// instruction to an agent.

// The bounds a thread is kept within, the same as the desk's Personal store.
const (
	// PageCommentMost is the longest one comment may be, in bytes: the same
	// bound a task's comment has.
	PageCommentMost = 16 << 10
	// PageCommentQuoteMost is the longest quoted passage, in bytes.
	PageCommentQuoteMost = 4000
	// PageCommentBlockMost is the longest block text kept with a thread, the
	// most a page's body may hold.
	PageCommentBlockMost = 256 << 10
	// PageCommentThreadsMost is how many threads one page holds.
	PageCommentThreadsMost = 2000
	// PageCommentPathMost is the deepest block path a thread may name.
	PageCommentPathMost = 32
)

// PageComment is one thing said in a thread.
type PageComment struct {
	ID   string      `json:"id"`
	By   TaskEventBy `json:"by"`
	Text string      `json:"text"`
	At   string      `json:"at"`
}

// PageCommentResolved is who closed a thread, and when.
type PageCommentResolved struct {
	By string `json:"by"`
	At string `json:"at"`
}

// PageCommentThread is a thread on a passage: where it was posted, and
// everything said in it, oldest first.
//
// It carries no "anchored": the desk decides that against the live document.
// Unread is filled for the one person reading, never kept: someone else said
// something in it after that person last read it.
type PageCommentThread struct {
	ID     string `json:"id"`
	WS     string `json:"ws"`
	PageID string `json:"page_id"`
	// Block is the block's path through the page's content arrays.
	Block []uint32 `json:"block"`
	// BlockText is that block's whole text when the thread was posted.
	BlockText string `json:"block_text"`
	// Offset is the code points before the quoted words in BlockText.
	Offset uint64 `json:"offset"`
	// Quote is the words the thread is about.
	Quote    string               `json:"quote"`
	Comments []PageComment        `json:"comments"`
	Resolved *PageCommentResolved `json:"resolved,omitempty"`
	Unread   bool                 `json:"unread,omitempty"`
}

// PageCommentNew is a new thread on the words a person selected.
type PageCommentNew struct {
	Block     []uint32 `json:"block"`
	BlockText string   `json:"block_text"`
	Offset    uint64   `json:"offset"`
	Quote     string   `json:"quote"`
	Text      string   `json:"text"`
}

// PageCommentPassage is the passage's own check, which needs no page: the
// quoted words are exactly the code points at Offset in BlockText. That is
// all the server can know, and it refuses a post that contradicts itself.
func PageCommentPassage(blockText string, offset uint64, quote string) bool {
	text := []rune(blockText)
	n := uint64(len([]rune(quote)))
	if offset > uint64(len(text)) || n > uint64(len(text))-offset {
		return false
	}
	return string(text[offset:offset+n]) == quote
}

// UnreadFor reports whether t holds a comment by someone other than person
// after the first seen comments: what they have not read.
func (t PageCommentThread) UnreadFor(person string, seen int) bool {
	person = NormalizeHandle(person)
	for i := max(seen, 0); i < len(t.Comments); i++ {
		c := t.Comments[i]
		if c.By.Kind == TaskByPerson && NormalizeHandle(c.By.ID) == person {
			continue
		}
		return true
	}
	return false
}
