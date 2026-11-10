package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// UsageLedgerLockSuffix keeps remote append and reconstruction on one process-shared lock.
const UsageLedgerLockSuffix = ".remote-lock"

// UsageCallID identifies the original worker row independently of the computer,
// folder or parent that later carries it. Older worker rows need no rewrite:
// the task transport computes the same identity from their original facts.
func UsageCallID(line UsageLine) string {
	if line.CallID != "" {
		return line.CallID
	}
	if line.At.IsZero() || line.Session == "" {
		return ""
	}
	raw, _ := json.Marshal([]any{line.At.UTC().Format(time.RFC3339Nano), line.Session, line.Model, line.Role, line.Calls, line.Input, line.Output, line.USD})
	sum := sha256.Sum256(raw)
	return "call:" + hex.EncodeToString(sum[:])
}
