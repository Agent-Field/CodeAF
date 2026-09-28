//go:build windows

package builtin

import "github.com/Agent-Field/codeaf/internal/delegate"

// carried is empty on Windows. senior-dev's engine uses process groups, file
// locks and a bash shell, none of which it has ever had a Windows form of, so
// on Windows it is ABSENT — no row, no verb, no paragraph in the prompt —
// rather than present and failing every time it is asked.
var carried []delegate.Delegate
