package factory

import "time"

// ── AN ORGANIZATION'S LIBRARY ──────────────────────────────────────────────
//
// An organization keeps one Library on its server (org-space DESIGN
// §2.1): pages that everyone in the organization reads, and that every
// chat the server runs can search. The rules:
//
//   - NOTHING REACHES IT ON ITS OWN (OWNER-DECISIONS §5). A page arrives
//     because a person chose Add to Library on a file, or wrote one there.
//     The organization's Library learns nothing from chats.
//   - IT IS THE ORGANIZATION'S. Once added, a page is everyone's to read,
//     whatever the file it came from; the person who added it and the
//     operator may change or remove it.
//   - My space has no Library of its own: a person's pages are already theirs.

// OrgLibraryPage is one page of the organization's Library.
type OrgLibraryPage struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Body is the page's words, as Markdown.
	Body string `json:"body"`
	// By is the handle of the person who added it.
	By string `json:"by"`
	// From is the file it was added from, when it was; it names a place, and
	// the page stays whatever happens to the file.
	From      *OrgLibraryFrom `json:"from,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// OrgLibraryFrom is where a Library page was added from.
type OrgLibraryFrom struct {
	Workstream string `json:"workstream"`
	File       string `json:"file,omitempty"`
}

// OrgLibrary is every page, by id.
type OrgLibrary map[string]OrgLibraryPage
