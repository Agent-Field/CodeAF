//go:build !windows

package builtin

import (
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/secaf"
	"github.com/Agent-Field/codeaf/internal/seniordev"
)

// carried is every program this build carries on a unix: senior-dev, whose
// engine lives in internal/seniordev, and sec, sec-af's auditor,
// in internal/secaf.
var carried = []delegate.Delegate{seniordev.Program, secaf.Program}
