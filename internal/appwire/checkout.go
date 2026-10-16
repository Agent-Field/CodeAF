package appwire

import "strings"

// CHECKOUT.STATUS: what the agent's own workspace checkout looks like to git
// right now, as typed facts, read-only and bounded.
//
// The caller names an agent and never a path: the checkout is the one the
// engine already trusts for files.diff and branches.list. Unknown stays
// unknown: an upstream that is not configured, a count git did not give, and a
// listing that was cut short are each ABSENT or marked, never zero and never a
// short list called whole.
//
// There is no landing here. The result says so in [CheckoutLand] rather than
// leaving a client to guess from a missing method: the only producer that
// merges work into a checkout lands a standing copy of a referred folder, binds
// no plan or revision, and is not the producer of the branches branches.list
// names, so offering a "land" over this wire would be a git merge wrapper.

// The bounds of checkout.status.
const (
	// CheckoutPageDefault and CheckoutPageMax bound the entries of one answer.
	CheckoutPageDefault = 100
	CheckoutPageMax     = 200
	// CheckoutJobs is how many statuses are read at once; one more is busy.
	CheckoutJobs = 2
	// CheckoutSeconds is the deadline for every git process of one call together.
	CheckoutSeconds = 3
	// CheckoutOutMax is the most status text one read keeps; past it the process
	// is killed and the answer says incomplete.
	CheckoutOutMax = 4 << 20
	// CheckoutTotalMax is the most git output one call reads, every command and
	// both status reads together. A status over about half of it cannot be read
	// twice, so its counts are too_large.
	CheckoutTotalMax = 6 << 20
)

// What an entry is.
const (
	CheckoutChanged    = "changed"    // tracked, differs in the index, the work tree or both
	CheckoutRenamed    = "renamed"    // git paired a removal with an addition; OldPath says from where
	CheckoutCopied     = "copied"     // git paired a copy; OldPath says from where
	CheckoutUntracked  = "untracked"  // in the work tree, not tracked, not ignored
	CheckoutConflicted = "conflicted" // unmerged; Conflict says how
)

// The one operation a checkout may be in the middle of, read from the files git
// itself leaves while it is.
const (
	CheckoutOpMerge      = "merge"
	CheckoutOpRebase     = "rebase"
	CheckoutOpCherryPick = "cherry_pick"
	CheckoutOpRevert     = "revert"
	CheckoutOpBisect     = "bisect"
)

// Why a reply carries no line counts.
const (
	LinesIncomplete = "incomplete"          // the status was cut, so no revision covers the counts
	LinesChanged    = "changed_during_read" // the status read after the counts differs from the one before
	LinesTimeout    = "timeout"             // the call's deadline ended during the counts
	LinesTooLarge   = "too_large"           // the counts' own output passed its bound
	LinesGitFailed  = "git_failed"          // git refused or answered something unreadable
)

// Why there is no land.
const CheckoutLandNoProducer = "no_producer"

// CheckoutEntry is one path of the status. Index is the change staged for the
// next commit, Worktree the change on disk not yet staged; either may be absent
// and a changed path has at least one. For a renamed or copied path the side
// that is the rename carries the same word.
type CheckoutEntry struct {
	// Path is the repository-relative path as git spells it. A path whose bytes
	// are not valid UTF-8 has none and says PathUnreadable: it is counted and
	// never converted lossily.
	Path           string `json:"path,omitempty"`
	PathUnreadable bool   `json:"path_unreadable,omitempty"`
	OldPath        string `json:"old_path,omitempty"`
	Kind           string `json:"kind" enum:"changed,renamed,copied,untracked,conflicted"`
	Index          string `json:"index,omitempty" enum:"modified,added,deleted,typechange,renamed,copied"`
	Worktree       string `json:"worktree,omitempty" enum:"modified,added,deleted,typechange,renamed,copied"`
	// Conflict says how an unmerged path is unmerged.
	Conflict string `json:"conflict,omitempty" enum:"both_deleted,added_by_us,deleted_by_them,added_by_them,deleted_by_us,both_added,both_modified"`
	// Submodule says the path is a submodule: only its recorded commit is
	// compared, its own work tree is never entered.
	Submodule bool `json:"submodule,omitempty"`
	// Added and Removed are the entry's line counts, ABSENT when unknown and
	// never zero for unknown. A tracked entry's are the NET change of the work
	// tree (staged and unstaged together) against the status's base: the HEAD
	// commit it names, or the empty tree when unborn; the index is not the base,
	// so edits staged and unstaged that cancel read 0 and 0. A rename counts
	// the edits since its original path. Untracked, conflicted, copied,
	// submodule and filtered entries have none: an untracked file is never
	// opened. Binary says git found the content binary, with the counts absent.
	Added   *int `json:"added,omitempty"`
	Removed *int `json:"removed,omitempty"`
	Binary  bool `json:"binary,omitempty"`
}

// CheckoutCounts is the whole status counted. A path changed both in the index
// and in the work tree is in Staged and in Unstaged. It is present only when
// every entry was read.
type CheckoutCounts struct {
	Staged     int `json:"staged"`
	Unstaged   int `json:"unstaged"`
	Untracked  int `json:"untracked"`
	Conflicted int `json:"conflicted"`
}

// CheckoutLand says whether this engine can land work into the checkout through
// the wire. It never can today; Reason says why in a word a client can switch on.
type CheckoutLand struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty" enum:"no_producer"`
}

// CheckoutStatus is checkout.status's answer. Repo false means the agent has no
// workspace or git said it is not a checkout (git that did not answer in time is
// busy, never repo false).
type CheckoutStatus struct {
	Repo bool `json:"repo"`
	// Head is the full commit HEAD names, absent on an unborn branch. Branch is
	// absent when HEAD is detached.
	Head     string `json:"head,omitempty"`
	Branch   string `json:"branch,omitempty"`
	Detached bool   `json:"detached,omitempty"`
	Unborn   bool   `json:"unborn,omitempty"`
	Upstream string `json:"upstream,omitempty"`
	// Ahead and Behind are against Upstream and absent when git did not give
	// them, never zero for unknown.
	Ahead     *int   `json:"ahead,omitempty"`
	Behind    *int   `json:"behind,omitempty"`
	Operation string `json:"operation,omitempty" enum:"merge,rebase,cherry_pick,revert,bisect"`
	// Entries is this page, in git's own order.
	Entries []CheckoutEntry `json:"entries"`
	Counts  *CheckoutCounts `json:"counts,omitempty"`
	// Revision names the exact status Next was issued for; a cursor is refused
	// once the status is different.
	Revision string `json:"revision,omitempty"`
	// Next resumes at the first entry not sent. Incomplete says the status was
	// longer than the engine reads: what is here is the part read, there is no
	// Next past it, and Counts is absent.
	Next       string       `json:"next,omitempty"`
	Incomplete bool         `json:"incomplete,omitempty"`
	Land       CheckoutLand `json:"land"`
	// LinesUnavailable, when set, says no entry on this reply carries counts and
	// why: the status was cut (incomplete), changed between the two reads that
	// must agree (changed_during_read), or the counts' own reads ran out of time
	// (timeout), output (too_large) or git (git_failed). Absent means counts were
	// attempted and each entry's own fields are the whole account.
	LinesUnavailable string `json:"lines_unavailable,omitempty" enum:"incomplete,changed_during_read,timeout,too_large,git_failed"`
}

// CheckoutQuery is checkout.status's request.
type CheckoutQuery struct {
	Agent  string `json:"agent"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// CheckoutReader is the optional engine half of checkout.status. One that is
// not a CheckoutReader refuses it as unknown.
type CheckoutReader interface {
	CheckoutStatus(CheckoutQuery) (CheckoutStatus, error)
}

func init() {
	methods["checkout.status"] = typed(func(e Engine, p CheckoutQuery) (*CheckoutStatus, error) {
		r, ok := e.(CheckoutReader)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve checkout.status")
		}
		if strings.TrimSpace(p.Agent) == "" {
			return nil, Errorf(CodeBadParams, "checkout.status needs an agent")
		}
		if p.Limit < 0 {
			return nil, Errorf(CodeBadParams, "checkout.status needs a limit of zero or more")
		}
		if p.Limit == 0 {
			p.Limit = CheckoutPageDefault
		}
		p.Limit = min(p.Limit, CheckoutPageMax)
		got, err := r.CheckoutStatus(p)
		if err != nil {
			return nil, err
		}
		if got.Entries == nil {
			got.Entries = []CheckoutEntry{}
		}
		// THE LAND FACT IS THE WIRE'S, NOT THE ENGINE'S: no engine may claim a
		// landing this protocol has no door for.
		got.Land = CheckoutLand{Reason: CheckoutLandNoProducer}
		return &got, nil
	})
}
