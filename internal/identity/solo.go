package identity

// AN IDENTITY MADE ON THIS COMPUTER AND NEVER SHARED IS SOLO. It is a fleet of
// one, so nothing about it is the sync service's business: a marker beside the
// identity says so, and everything that talks to the service reads it. The
// marker ends the moment the identity is shared or taken ([EndSolo]): this
// computer took another's identity, or a person here approved another device
// and the identity was sent to it, whether that device typed this computer's
// code or asked by link. Showing a code shares nothing, so a code taken back,
// run out, declined or used up by a wrong code leaves the marker, and neither
// does reading pairing instructions.
//
// An identity that has no marker is not solo: it is every identity that was
// made before the marker existed, and it keeps syncing as it did.

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

const soloFile = "identity-solo"

// Solo says the identity under home was made here and has not been shared.
func Solo(home string) bool {
	_, err := os.Stat(filepath.Join(home, soloFile))
	return err == nil
}

// markSolo is written before the identity, so a crash between the two leaves a
// marker and no identity, which asks nothing of anyone.
func markSolo(home string) error {
	return os.WriteFile(filepath.Join(home, soloFile), nil, 0o600)
}

// EndSolo says the identity is shared now. Ending what is not begun is no error.
func EndSolo(home string) error {
	err := os.Remove(filepath.Join(home, soloFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
