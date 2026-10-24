package pair

import (
	"encoding/hex"

	"github.com/Agent-Field/codeaf/internal/identity"
)

// DeviceID is the id the directory gives the asking device, so a screen can
// match it to the joined event that follows an approval.
func (a Asking) DeviceID() string {
	return identity.Cert{Device: hex.EncodeToString(a.pubkey)}.DeviceID()
}
