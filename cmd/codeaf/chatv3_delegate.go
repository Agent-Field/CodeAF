package main

import (
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/delegate/builtin"
)

// THE PROGRAMS ONE CONVERSATION CAN HAND A WHOLE TASK TO: the ones this build
// carries (internal/delegate/builtin). The list is read here, at the door, and
// handed to internal/session, so the session package never imports a
// program's whole engine and a test of it never carries one.
func v3Delegates() []delegate.Delegate {
	return builtin.All()
}
