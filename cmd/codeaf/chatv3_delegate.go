package main

import (
	"github.com/Agent-Field/codeaf/internal/delegate"
)

// THE DELEGATE SIDE OF ONE CONVERSATION: the outside programs a task can be
// handed to whole (internal/delegate, docs/DELEGATE-PROTOCOL.md). The registry
// is read here, at the door, for the reason every other registry on this path
// is (chatv3_subharness.go): where the manifests live is the SURFACE'S decision,
// and internal/session is handed the registry and nothing about a directory.
//
// IT IS SILENT ON FAILURE, in the same posture: a folder that cannot be read
// means DELEGATES OFF — the door lists nothing, `via` refuses every name, the
// prompt says nothing — and a registry is not worth failing a launch over. A
// manifest the loader would not admit is not a failure of the launch either; it
// is a line on `/delegate`, which is where the person who wrote it will look.
func v3Delegates() *delegate.Registry {
	registry, err := delegate.Load(delegate.Dir())
	if err != nil {
		return nil
	}
	return registry
}
