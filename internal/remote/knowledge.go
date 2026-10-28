package remote

import "github.com/Agent-Field/codeaf/internal/knowledge"

// Knowledge is unavailable over the remote protocol in this experiment. A
// hosted window must never fall back to a vault on the viewing machine.
func (a *Agent) Knowledge() *knowledge.Vault { return nil }

// KnowledgeCaptureError has no local capture to inspect on a hosted window.
func (a *Agent) KnowledgeCaptureError() string { return "" }
