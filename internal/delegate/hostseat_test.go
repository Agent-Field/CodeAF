package delegate

import "github.com/Agent-Field/codeaf/internal/executor/executortest"

// These tests run tools with no session, so their calls run on the host seat
// by name; production still refuses a call that lost its session.
func init() { executortest.Host() }
