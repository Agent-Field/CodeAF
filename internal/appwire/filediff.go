package appwire

import "strings"

// FILES.DIFF: one file compared with a commit, read-only and bounded. The
// working tree is read as it stands, so staged and unstaged edits are both in
// the answer. The base is HEAD unless the caller names a commit; a named one
// that does not resolve to a commit falls back to HEAD and says so
// (RequestedUnavailable), so a caller can tell the difference and word it.
// Nothing here knows who asked or why: the caller owns what a base means.
// At most [FileDiffMax] bytes and [FileDiffLines] lines of hunks are answered,
// with Truncated saying more was there.

// The bounds of files.diff.
const (
	// FileDiffMax is the most hunk text one answer carries.
	FileDiffMax = 256 << 10
	// FileDiffLines is the most hunk lines one answer carries.
	FileDiffLines = 2000
	// FileDiffJobs is how many diffs are read at once; one more is busy.
	FileDiffJobs = 2
	// FileDiffSeconds is the deadline for every git process of one diff together.
	FileDiffSeconds = 3
)

// What a diff found.
const (
	DiffChanged     = "changed"     // differs from the base; Text holds hunks unless Binary
	DiffUnchanged   = "unchanged"   // tracked and the same as the base
	DiffUntracked   = "untracked"   // in a repository, never added; Text is the whole file as added
	DiffIgnored     = "ignored"     // in a repository, ignored by it
	DiffNotGit      = "not_git"     // not inside a git work tree; Folder says where it is
	DiffNoCommits   = "no_commits"  // the repository has no commit yet, so nothing to compare with
	DiffMissing     = "missing"     // not on disk and not known to git
	DiffUnavailable = "unavailable" // git could not answer; Reason says why
)

// What the diff is against.
const (
	DiffBaseHead      = "head"      // HEAD, the latest commit
	DiffBaseRequested = "requested" // the commit the caller named
	DiffBaseNone      = "none"      // nothing: an untracked file is all new
)

// Why a requested base was not used.
const (
	DiffRequestedUnknown = "unknown_commit" // it does not name a commit in this repository
)

// Why a diff is unavailable.
const (
	DiffReasonTimeout   = "timeout"    // git took longer than FileDiffSeconds
	DiffReasonNoGit     = "no_git"     // there is no git program here
	DiffReasonFilter    = "filter"     // the file passes through a git filter, which is never run
	DiffReasonUnsafe    = "unsafe"     // git refuses the repository's ownership
	DiffReasonTooLarge  = "too_large"  // an untracked file over FileReadMax
	DiffReasonGitFailed = "git_failed" // any other refusal; Detail carries git's first line
)

// FileDiff is one path compared with its base. Resolved is the canonical
// absolute path, every link followed; Outside says it lies outside the agent's
// workspace once resolved. Add and Del are absent when git cannot count
// (binary, not in git). Text is unified hunks from the first "@@", with no file
// headers.
type FileDiff struct {
	Path      string `json:"path"`
	Resolved  string `json:"resolved,omitempty"`
	Outside   bool   `json:"outside,omitempty"`
	State     string `json:"state"`
	Folder    string `json:"folder,omitempty"`
	Repo      string `json:"repo,omitempty"`
	Base      string `json:"base,omitempty"`
	Commit    string `json:"commit,omitempty"` // the full hash compared with
	Requested string `json:"requested,omitempty"`
	// RequestedUnavailable says why the requested base was not used; Base is
	// then head.
	RequestedUnavailable string `json:"requested_unavailable,omitempty"`
	// InHistory says whether a requested base is an ancestor of HEAD.
	InHistory *bool  `json:"in_history,omitempty"`
	Change    string `json:"change,omitempty"` // modified · added · deleted · renamed
	OldPath   string `json:"old_path,omitempty"`
	Add       *int   `json:"add,omitempty"`
	Del       *int   `json:"del,omitempty"`
	Binary    bool   `json:"binary,omitempty"`
	Text      string `json:"text"`
	Lines     int    `json:"lines"`
	Truncated bool   `json:"truncated,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// FileDiffer is the optional engine half of files.diff. One that is not a
// FileDiffer refuses files.diff as unknown.
type FileDiffer interface {
	// DiffFile answers path (relative to the agent's workspace, or absolute)
	// compared with base (a commit or ref; empty is HEAD), read-only and
	// bounded.
	DiffFile(agent, path, base string) (FileDiff, error)
}

// FileDiffResult is files.diff's answer.
type FileDiffResult struct {
	Diff FileDiff `json:"diff"`
}

func init() {
	methods["files.diff"] = typed(func(e Engine, p struct {
		Agent string `json:"agent"`
		Path  string `json:"path"`
		Base  string `json:"base"`
	}) (*FileDiffResult, error) {
		d, ok := e.(FileDiffer)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve files.diff")
		}
		if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.Path) == "" {
			return nil, Errorf(CodeBadParams, "files.diff needs an agent and a path")
		}
		if !ValidDiffBase(p.Base) {
			return nil, Errorf(CodeBadParams, "files.diff base must be a commit or ref name")
		}
		diff, err := d.DiffFile(p.Agent, p.Path, p.Base)
		if err != nil {
			return nil, err
		}
		return &FileDiffResult{diff}, nil
	})
}

// ValidDiffBase says whether base may be handed to git as a revision: empty,
// or a name of at most 256 bytes that cannot be read as an option, a range, a
// path or a revision expression.
func ValidDiffBase(base string) bool {
	if base == "" {
		return true
	}
	if len(base) > 256 || strings.HasPrefix(base, "-") || strings.Contains(base, "..") || strings.Contains(base, "@{") {
		return false
	}
	for _, r := range base {
		if r <= ' ' || r == 0x7f || strings.ContainsRune(":^~?*[\\{}", r) {
			return false
		}
	}
	return true
}
