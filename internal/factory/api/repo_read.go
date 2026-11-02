package api

// RepoRead is GET /v1/repos/{owner}/{repo}/read: one bounded, read-only look
// at the factory's own checkout of a watched repository, for the desk's Ask
// loop. The query names the look:
//
//   - op=tree&path=P lists the folder P (the root when absent): Entries.
//   - op=grep&pattern=RE&path=P finds lines matching the RE2 pattern under P:
//     Hits, at most 50.
//   - op=read&path=P&from=A&to=B is lines A..B of the file P: Text, at most
//     400 lines and 32 KiB.
//   - job=N reads at that job's head commit instead of the default branch,
//     when the job's commit is in this checkout.
//
// Everything is read from git's object store at one commit, never from the
// working tree, so uncommitted files and symbolic links cannot reach outside
// the repository. Untrusted is always true: the text is the repository's own,
// and a reader hands it to a model as data, never as instructions.
type RepoRead struct {
	Factory Factory `json:"factory"`
	Repo    string  `json:"repo"`
	Op      string  `json:"op"`
	// At is "main" (the default branch tip as the checkout knows it) or "job".
	At  string `json:"at"`
	Job int    `json:"job,omitempty"`
	// Ref is the ref read for main (origin/main, main or master); Commit is
	// the 40-hex commit every answer of this read came from.
	Ref       string `json:"ref,omitempty"`
	Commit    string `json:"commit"`
	Path      string `json:"path"`
	Untrusted bool   `json:"untrusted"`
	// Truncated says a bound stopped the answer before all of it: entries
	// past the cap, hits past 50 or past the scan budget, lines past 400 or
	// 32 KiB.
	Truncated bool `json:"truncated"`

	Entries []RepoEntry `json:"entries,omitempty"`

	Pattern string    `json:"pattern,omitempty"`
	Hits    []RepoHit `json:"hits,omitempty"`

	From int    `json:"from,omitempty"`
	To   int    `json:"to,omitempty"`
	Size int64  `json:"size,omitempty"`
	EOF  bool   `json:"eof,omitempty"`
	Text string `json:"text,omitempty"`
}

// RepoEntry is one child of a folder: Type is file, dir, link or submodule;
// Size is a file's bytes.
type RepoEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size,omitempty"`
}

// RepoHit is one matching line, its text cut to a bounded length.
type RepoHit struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// The reasons the read route refuses with, beside the ordinary not_found and
// bad_input.
const (
	// ReasonNotCloned (409): the factory's machine has no git checkout of the
	// repository, or one with no default branch to read.
	ReasonNotCloned = "not_cloned"
	// ReasonDenied (403): the path is on the deny list (reporead.DenyList).
	// The error is the fixed word "denied".
	ReasonDenied = "denied"
	// ReasonBinary (422): the file holds a NUL byte in its first 8 KiB. The
	// error is the fixed word "binary".
	ReasonBinary = "binary"
)
