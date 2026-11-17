package api

// ── An organization's Library (lane L3, org-space DESIGN §2.1) ──
//
//	GET    /v1/library            every page's title, newest first (no words)
//	GET    /v1/library/{id}       one page with its words
//	POST   /v1/library            a person adds a page: words they wrote, or a file's words (Add to Library)
//	PATCH  /v1/library/{id}       the person who added it, or the operator, changes its title or words
//	DELETE /v1/library/{id}       the person who added it, or the operator, removes it
//
// Everyone in the organization reads every page. Nothing is added except by a
// person's deliberate act.

// The Library's limits, each spelled once and quoted by the refusals.
const (
	OrgLibraryTitleMost = 200
	// OrgLibraryBodyMost is a page's words, in bytes: the desk's Add to
	// Library sends at most this much of a file.
	OrgLibraryBodyMost  = 64000
	OrgLibraryPagesMost = 500
)

// OrgLibraryPage is one page as the wire carries it. Body is empty in a list.
type OrgLibraryPage struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	By    string `json:"by"`
	// ByName is the person's display name, when it is not their handle.
	ByName string `json:"by_name,omitempty"`
	// FromWorkstream and FromName say where it was added from, only to a
	// person who may see that workstream.
	FromWorkstream string `json:"from_workstream,omitempty"`
	FromName       string `json:"from_name,omitempty"`
	// Mine is whether the caller added it; Changeable whether they may change
	// or remove it (theirs, or the operator's).
	Mine       bool   `json:"mine,omitempty"`
	Changeable bool   `json:"changeable,omitempty"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// OrgLibraryList is GET /v1/library.
type OrgLibraryList struct {
	Pages []OrgLibraryPage `json:"pages"`
}

// OrgLibraryNew is POST /v1/library. With FromWorkstream and FromFile, the
// words are that file's as the person's app read them.
type OrgLibraryNew struct {
	Title          string `json:"title"`
	Body           string `json:"body"`
	FromWorkstream string `json:"from_workstream,omitempty"`
	FromFile       string `json:"from_file,omitempty"`
}

// OrgLibraryPatch is PATCH /v1/library/{id}; an absent field is unchanged.
type OrgLibraryPatch struct {
	Title *string `json:"title,omitempty"`
	Body  *string `json:"body,omitempty"`
}
