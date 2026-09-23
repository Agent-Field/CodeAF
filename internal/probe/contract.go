// Package probe is the machine contract for codeaf-probe: the companion
// binary a coding agent drives to test CodeAF in real terminal sessions.
//
// Every command answers the same envelope: a compact JSON Response whose
// data payload is one of the types below. The wire shape is the contract;
// keep it exactly.
package probe

// Response is the envelope for every probe command.
//   - ok=true  -> data carries the command's result.
//   - ok=false -> error carries one of the codes below and a message.
type Response struct {
	OK    bool       `json:"ok"`
	Data  any        `json:"data,omitempty"`
	Error *ErrorBody `json:"error,omitempty"`
}

// Error codes.
const (
	CodeStaleRevision = "STALE_REVISION" // act carried expect_revision that no longer matches
	CodeNoSession     = "NO_SESSION"     // unknown or dead probe session id
	CodeTimeout       = "TIMEOUT"        // bounded wait did not reach its condition
	CodeNotOwned      = "NOT_OWNED"      // cleanup refused to touch a foreign resource
	CodeBadRequest    = "BAD_REQUEST"    // malformed act/start/observe request
)

// ErrorBody is the error half of the envelope.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ObserveData is the data payload of observe: a full rendered snapshot of
// the tmux pane (real screen state with cursor, not stripped ANSI), the
// revision counter, and live process status.
type ObserveData struct {
	Revision  int      `json:"revision"`
	Snapshot  string   `json:"snapshot"`
	Cursor    Cursor   `json:"cursor"`
	Processes []string `json:"processes"`
	TS        string   `json:"ts"` // RFC3339

	// Diff is set only by observe --diff: line-oriented +/- changes since the
	// previous observation of this session.
	Diff string `json:"diff,omitempty"`
}

// Cursor is the pane's cursor position, 0-based.
type Cursor struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// ActWait is the bounded wait half of an act request. quietMs asks for a
// screen that has not changed for that long; timeoutMs bounds the whole
// wait. Screen quiet does NOT mean the work is complete — it means no
// pixels moved.
type ActWait struct {
	QuietMs   int `json:"quietMs"`
	TimeoutMs int `json:"timeoutMs"`
}

// ActRequest mutates a session. Exactly one mutation may be given:
// text (typed literally), keys (tmux key names like Enter, C-c, BSpace),
// resize (new width/height), or none (wait-only). An act may additionally
// carry wait (bounded quiet wait — a modifier, not a mutation, so
// act+wait is atomic) and expect_revision: if the session's revision does
// not match, the act is rejected with STALE_REVISION and nothing is sent.
type ActRequest struct {
	Text           string   `json:"text,omitempty"`
	Keys           string   `json:"keys,omitempty"`
	Resize         *Resize  `json:"resize,omitempty"`
	Wait           *ActWait `json:"wait,omitempty"`
	ExpectRevision *int     `json:"expect_revision,omitempty"`
}

// Resize is a pane resize request.
type Resize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// ActData is the data payload of act.
type ActData struct {
	Accepted       bool `json:"accepted"`
	RevisionBefore int  `json:"revision_before"`
	RevisionAfter  int  `json:"revision_after"`
	Stale          bool `json:"stale"`
}

// StartData is the data payload of start: a new persistent tmux-backed
// session under the probe root.
type StartData struct {
	SessionID string `json:"session_id"`
	Socket    string `json:"socket"`
	Profile   string `json:"profile"`
	Dims      Dims   `json:"dims"`
}

// Dims is a pane size in cells.
type Dims struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// BuildIdentity is the immutable identity of a prepared binary: recorded
// once, reused while it matches, never silently swapped mid-session.
type BuildIdentity struct {
	SHA       string   `json:"sha"`
	Dirty     bool     `json:"dirty"`
	GoVersion string   `json:"go_version"`
	Flags     []string `json:"flags"`
	Binary    string   `json:"binary"`
}

// PrepareData is the data payload of prepare.
type PrepareData struct {
	Build  BuildIdentity `json:"build"`
	Reused bool          `json:"reused"`
}

// FixtureData is the data payload of a fixture operation.
type FixtureData struct {
	Scenario string `json:"scenario"`
	Home     string `json:"home"`
	Seeded   bool   `json:"seeded"`
}
