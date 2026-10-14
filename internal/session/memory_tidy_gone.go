package session

import "github.com/Agent-Field/codeaf/internal/automation"

// NewMemoryTidy is what remains of the idle tidy, which is gone: nothing changes
// a remembered line without the person seeing it happen (memory.go), and a pass
// that merged and retired lines while nobody was looking was exactly that.
//
// IT IS A SHIM WITH ONE CALLER AND A SHORT LIFE. The automations clock
// (cmd/codeaf's chatv3_clock.go) still asks for a tidy, and that file belongs to
// the automations work, which is removing the call. Until it lands this answers
// nil, which the clock reads as "there is no memory pass" ([automation.Clock]'s
// tidy returns on a nil Tidy), and then this file is deleted.
func NewMemoryTidy(Config, string, string, automation.Idle) automation.Tidy { return nil }
