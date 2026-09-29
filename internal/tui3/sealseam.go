package tui3

import "github.com/Agent-Field/codeaf/internal/tui2/tokens"

// ── WHEN THE CONVERSATION'S CALLS STOP BEING SEALED ─────────────────────────
//
// Every tool call is sealed as it finishes, and a seal that fails never fails
// the call. That is the right thing for the call and a silent thing for the
// person: a chat could run for hours with nothing to rewind to and say so
// nowhere. So the state is on the status line for as long as it is true, and
// the three changes worth a sentence — it started, its cause changed, it
// recovered — are said once each in the transcript.
//
//	the segment   `✗ not sealed`, drawn while the LAST seal failed and gone the
//	              moment one holds — the emptiness law: a fact that is not true
//	              draws nothing
//	the notice    each change, once, on the road every one-off sentence takes

// SealSeam is what the door that seals this conversation's calls tells the
// surface. The zero value is a surface whose calls are not sealed at all —
// cells off, a headless frame, a test — where nothing is drawn or said.
type SealSeam struct {
	// Failing reports whether the most recent seal failed. It is asked on the
	// draw path and answers from a field already held.
	Failing func() bool
	// Notice hands over the next sentence worth saying, and "" when there is
	// none. It drains, so [app.takeSealNotice] is its only caller.
	Notice func() string
}

// sealSegment is the status-line segment: a mark and two words while sealing
// is failing, and nothing otherwise.
func (a *app) sealSegment() string {
	if a.seal.Failing == nil || !a.seal.Failing() {
		return ""
	}
	return a.icon(tokens.GFailed) + " not sealed"
}

// takeSealNotice puts the sentence the seam holds, if any, in the transcript.
// Like [app.takeLinkNotice] it runs at the top of [app.Update], so a window
// sitting idle says it on the next key rather than the next turn.
func (a *app) takeSealNotice() {
	if a.seal.Notice == nil {
		return
	}
	if said := a.seal.Notice(); said != "" {
		a.note(said)
	}
}
