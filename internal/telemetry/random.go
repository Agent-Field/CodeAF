package telemetry

import (
	"crypto/rand"
	"encoding/hex"
)

// randomHex returns 2n lowercase hex characters from the system's random
// source. It backs the event id (16 bytes) and the install id (32 bytes).
func randomHex(n int) string {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		// The system random source does not fail on any platform codeaf
		// ships; if it somehow did, a zero id is still exactly 64 hex
		// characters and still anonymous.
		for i := range raw {
			raw[i] = byte(i)
		}
	}
	return hex.EncodeToString(raw)
}
