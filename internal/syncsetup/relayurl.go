package syncsetup

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/env"
)

// savedURLFile is where a computer keeps the relay another device told it to
// sync through when they were paired. It is a file beside the identity and not a
// settings row, because CODEAF_SYNC_URL is deliberately never a row: a persisted
// value would point a machine at a relay it was never told about, and this one
// was told, once, by the device the person had just approved.
const savedURLFile = "sync-url"

// RelayURL is the relay this computer syncs through: the variable when it is
// set, else the address a pairing saved, else empty.
func RelayURL(home string) string {
	if set := env.Get(URLVar); set != "" {
		return set
	}
	raw, err := os.ReadFile(filepath.Join(home, savedURLFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// SaveRelayURL remembers the relay a pairing named, for every later run that has
// no variable of its own.
func SaveRelayURL(home, url string) error {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(home, savedURLFile), []byte(url+"\n"), 0o600)
}
