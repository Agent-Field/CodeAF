//go:build !windows

package builtin

import (
	"github.com/Agent-Field/codeaf/internal/claudecode"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/praf"
	"github.com/Agent-Field/codeaf/internal/secaf"
	"github.com/Agent-Field/codeaf/internal/seniordev"
)

// carried is every program this build carries on a unix: senior-dev, whose
// engine lives in internal/seniordev; sec, sec-af's auditor, in
// internal/secaf; review, pr-af's reviewer, in internal/praf; and claude,
// which hands a task to the person's own Claude Code, in internal/claudecode.
var carried = []delegate.Delegate{seniordev.Program, secaf.Program, praf.Program, claudecode.Program}
