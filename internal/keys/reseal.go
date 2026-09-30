package keys

import (
	"errors"
	"os"
	"path/filepath"
)

// nextFile holds a resealed vault until the identity that opens it is in place.
const nextFile = "vault.enc.next"

// StageReseal seals the vault under newKey into vault.enc.next beside the live
// file and answers the new envelope, which is what the new identity's relay
// receives. The live file is not touched, so nothing is lost if the identity
// is never replaced. A home with no vault answers nil. Repeating it is safe: a
// vault that already opens under newKey is answered as it is.
func StageReseal(home string, oldKey, newKey []byte) ([]byte, error) {
	live := filepath.Join(home, "vault.enc")
	blob, err := os.ReadFile(live)
	if errors.Is(err, os.ErrNotExist) {
		return readNext(home)
	}
	if err != nil {
		return nil, err
	}
	if _, err := open(newKey, blob); err == nil {
		return blob, nil
	}
	plain, err := open(oldKey, blob)
	if err != nil {
		return nil, err
	}
	next, err := seal(newKey, plain)
	if err != nil {
		return nil, err
	}
	return next, writeAtomic(filepath.Join(home, nextFile), next)
}

func readNext(home string) ([]byte, error) {
	blob, err := os.ReadFile(filepath.Join(home, nextFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return blob, err
}

// CommitReseal puts the staged vault in place of the live one. It is the step
// after the identity changed, and it does nothing when nothing is staged.
func CommitReseal(home string) error {
	err := os.Rename(filepath.Join(home, nextFile), filepath.Join(home, "vault.enc"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
