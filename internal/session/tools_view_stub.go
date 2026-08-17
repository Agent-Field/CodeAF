package session

// STUB(media/sight): deleted at merge — the sight lane owns the real viewTools.
//
// The belt calls a.viewTools() beside the three generation verbs (tools.go), so
// this lane's tree needs the symbol to compile. The sight lane's tools_view.go
// carries the real one — view_image, looking through the LOOKING slot's model —
// and the merge keeps that file and removes this one.

import "github.com/Agent-Field/aforge-v2/internal/exec/bare"

func (a *Agent) viewTools() []bare.Tool { return nil }
