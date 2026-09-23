//go:build !windows

package builtin

import "github.com/Agent-Field/codeaf/internal/delegate"

// carried is every program this build carries on a unix. senior-dev joins it
// when its engine lands in internal/seniordev.
var carried = []delegate.Delegate{}
