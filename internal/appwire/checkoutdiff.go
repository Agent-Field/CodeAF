package appwire

import "strings"

// CHECKOUT.DIFF and CHECKOUT.LOCATE: Review and Open/Reveal of one row of the
// checkout status.
//
// THE AUTHORITY IS A ROW OF THE ENGINE'S OWN CURRENT STATUS, never a path the
// client made. checkout.status spells paths from the repository root whatever
// folder the workspace is, so a client that joined one to the workspace named
// another file (workspace repo/sub, row root.txt: repo/sub/root.txt). These
// methods take the row's path byte for byte, read ONE status, find that row,
// and only then touch the file. A path that is not a row is not_found.
//
// THE REVISION IS OPTIONAL ON THE WIRE AND MANDATORY FOR A CLIENT THAT DISPLAYS
// A ROW: send the revision the row was shown under and a checkout that has since
// moved is refused (conflict, stale_revision) instead of showing different
// content than the list. Omitted means an explicit fresh lookup, and the reply
// says which revision was used.
//
// A RESPONSE HERE IS FACTS, NOT A GRANT. `open` is an absolute path the engine
// proved reachable through real directories inside `repo` at that moment. A
// native host must not accept it back from a renderer as authority; it receives
// agent, row path and displayed revision, asks checkout.locate again, validates
// the answer against the trusted repository, and only then acts.

// CheckoutPathQuery names one status row.
type CheckoutPathQuery struct {
	Agent string `json:"agent"`
	// Path is exactly a current status entry's path: repository-root-relative,
	// "/"-spelled.
	Path string `json:"path"`
	// Revision is the status revision the row was displayed under.
	Revision string `json:"revision,omitempty"`
}

// CheckoutLocation is checkout.locate's answer.
type CheckoutLocation struct {
	Entry    CheckoutEntry `json:"entry"`
	Revision string        `json:"revision"`
	// Repo is the canonical repository root the path is relative to.
	Repo string `json:"repo"`
	// WorkspacePath is the row's path relative to the workspace folder, absent
	// when the file is not inside that folder.
	WorkspacePath string `json:"workspace_path,omitempty"`
	// Open is the absolute path of an existing regular file, not a link, reached
	// through real directories inside Repo; absent otherwise (deleted, a link, a
	// path through a link, not UTF-8, or a platform that cannot prove it). It is
	// a fact and not a grant: see the comment above.
	Open string `json:"open,omitempty"`
}

// CheckoutDiffResult is checkout.diff's answer.
type CheckoutDiffResult struct {
	Diff          FileDiff      `json:"diff"`
	Entry         CheckoutEntry `json:"entry"`
	Revision      string        `json:"revision"`
	Repo          string        `json:"repo"`
	WorkspacePath string        `json:"workspace_path,omitempty"`
	Open          string        `json:"open,omitempty"`
}

// More reasons a diff is unavailable, for rows this producer does not diff.
const (
	DiffReasonUnsupportedRow = "unsupported_row" // a copied, conflicted or submodule row: M1 does not diff it
	DiffReasonUnreadable     = "unreadable"      // the file could not be opened safely
)

// ConflictStaleRevision is the conflict detail for a revision that has moved.
const ConflictStaleRevision = "stale_revision"

// CheckoutDiffer is the optional engine half of checkout.diff and checkout.locate.
type CheckoutDiffer interface {
	CheckoutDiff(CheckoutPathQuery) (CheckoutDiffResult, error)
	CheckoutLocate(CheckoutPathQuery) (CheckoutLocation, error)
}

// CheckoutPathMax is the longest row path a request may name.
const CheckoutPathMax = 4096

// ValidCheckoutPath says whether path could be a row's: non-empty, relative,
// "/"-spelled with no empty, "." or ".." segment, no NUL, valid UTF-8.
func ValidCheckoutPath(path string) bool {
	if path == "" || len(path) > CheckoutPathMax || strings.ContainsRune(path, 0) || strings.HasPrefix(path, "/") || strings.Contains(path, "\\") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return strings.ToValidUTF8(path, "") == path
}

func checkoutPathParams(p CheckoutPathQuery, method string) error {
	if strings.TrimSpace(p.Agent) == "" || !ValidCheckoutPath(p.Path) {
		return Errorf(CodeBadParams, "%s needs an agent and a status row's path (repository-relative, no . or .. parts)", method)
	}
	if len(p.Revision) > 128 {
		return Errorf(CodeBadParams, "%s revision is not one checkout.status issued", method)
	}
	return nil
}

func init() {
	methods["checkout.diff"] = typed(func(e Engine, p CheckoutPathQuery) (*CheckoutDiffResult, error) {
		d, ok := e.(CheckoutDiffer)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve checkout.diff")
		}
		if err := checkoutPathParams(p, "checkout.diff"); err != nil {
			return nil, err
		}
		got, err := d.CheckoutDiff(p)
		if err != nil {
			return nil, err
		}
		return &got, nil
	})
	methods["checkout.locate"] = typed(func(e Engine, p CheckoutPathQuery) (*CheckoutLocation, error) {
		d, ok := e.(CheckoutDiffer)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve checkout.locate")
		}
		if err := checkoutPathParams(p, "checkout.locate"); err != nil {
			return nil, err
		}
		got, err := d.CheckoutLocate(p)
		if err != nil {
			return nil, err
		}
		return &got, nil
	})
}
