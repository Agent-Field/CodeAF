package remote

import "github.com/Agent-Field/codeaf/internal/session"

// This assertion belongs on the transport side because remote may import
// session while session cannot legally import its remote implementation.
var _ session.PlanAgent = (*Agent)(nil)
var _ session.PlanWorkAgent = (*Agent)(nil)
