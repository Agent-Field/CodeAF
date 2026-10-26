package furrow

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// Peek is the last answer about one folder, read from memory and NEVER from a
// subprocess. It is the door for a caller that must not wait on furrow — the
// engine building a session for the first frame — and it is the other half of
// [Detect], which is the door that asks.
//
// known is false when nobody has asked about this folder yet. fresh is false
// when the answer is older than [detectTTL]; the stale answer is still returned
// because it is the best one there is, and a caller that wants a newer one asks
// [Detect] from somewhere it is allowed to wait.
func Peek(root string) (presence Presence, fresh, known bool) {
	root = absoluteRoot(root)
	presenceMu.Lock()
	defer presenceMu.Unlock()
	cached, ok := presenceCache[root]
	if !ok {
		return Presence{}, false, false
	}
	return cached.presence, time.Since(cached.asked) < detectTTL, true
}

// PeekTools is [Tools] for a caller that may not wait: the four verbs when the
// remembered answer says this folder is attached, and nothing when it says it
// is not or when there is no answer yet. The absence law holds either way — a
// verb is only ever handed over on the strength of an answer furrow gave.
func PeekTools(root string) (tools []bare.Tool, fresh, known bool) {
	presence, fresh, known := Peek(root)
	if !known || !presence.Available() {
		return nil, fresh, known
	}
	workspace := &Workspace{root: absoluteRoot(root), binary: presence.Path}
	return workspace.Tools(), fresh, known
}
