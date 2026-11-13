package serve

import (
	"net/http"
	"strings"
)

// ── A STAND-IN FOR THE ACCESS CORE, UNTIL IT LANDS ─────────────────────────
//
// THIS FILE IS DELETED WHEN THE ACCESS CORE (item_access.go) LANDS: that file
// exports workstreamCan with this exact signature, so no caller changes. Until
// then it answers from the workstream rules this server already keeps, where
// every share is an edit share: a person who can see a workstream they own or
// that is shared with their team can also edit it, and the operator can view
// every one but edits none through a role.

// workstreamCan is whether the caller holds verb ("view", "comment", "edit" or
// "full") on the organization workstream id names. known is false, with no
// error, when id names no organization workstream.
func (h *Handler) workstreamCan(r *http.Request, wsID string, verb string) (known, ok bool, err error) {
	if h.config.Store == nil || !strings.HasPrefix(wsID, workstreamIDPrefix) {
		return false, false, nil
	}
	ws, found, err := h.config.Store.Workstream(wsID)
	if err != nil || !found {
		return false, false, err
	}
	v, err := h.wsViewer(r)
	if err != nil {
		return true, false, err
	}
	switch verb {
	case "view":
		return true, v.canSee(ws), nil
	case "comment", "edit":
		return true, v.canWrite(ws), nil
	case "full":
		return true, v.canGovern(ws), nil
	}
	return true, false, nil
}
