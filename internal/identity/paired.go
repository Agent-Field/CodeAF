package identity

import (
	"os"
	"path/filepath"
)

// pairedFile is the mark that this computer has been in a pairing: it took
// another computer's identity, or it showed a code to let one in.
const pairedFile = "paired"

// MarkPaired records that this home belongs to a fleet of more than one
// computer, or was just offered to one.
func MarkPaired(home string) error {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(home, pairedFile), []byte("paired\n"), 0o600)
}

// Paired says whether this home has been in a pairing. A home that was never
// in one has no reason to talk to the relay about its standing.
func Paired(home string) bool {
	_, err := os.Stat(filepath.Join(home, pairedFile))
	return err == nil
}
