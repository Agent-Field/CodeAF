package keys

import (
	"encoding/json"
	"fmt"
)

// Export returns the vault.enc envelope bytes, the form a vault takes on the
// wire. A vault never written yet exports as an empty, sealed one, so the
// receiver can tell "nothing yet" from "damaged".
func (v *Vault) Export() ([]byte, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	d, err := v.read()
	if err != nil {
		return nil, err
	}
	plain, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	return seal(v.key, plain)
}

// Merge folds another copy of this identity's vault into the local one: the
// union of secrets by id, and for an id both hold with different content the
// slot with the newer stamp, where a delete is a slot like any other. Device clocks are trusted, which
// Stage 1 accepts because the devices are one person's own. A copy that cannot
// be opened under this key changes nothing. The merged document keeps the newer
// stamp instead of taking the time of the merge, so merging never makes an old
// edit look new.
func (v *Vault) Merge(enc []byte) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	remote, err := openDoc(v.key, enc)
	if err != nil {
		return fmt.Errorf("keys: cannot merge vault: %w", err)
	}
	local, err := v.read()
	if err != nil {
		return err
	}
	if !fold(local, remote) {
		return nil
	}
	return v.write(local)
}

// fold merges remote into local and reports whether local changed. Each secret
// id is decided alone by the newer slot stamp, live secrets and tombstones
// alike; on a tie a tombstone wins, since a key someone chose to delete should
// not survive a coin flip.
func fold(local, remote *vaultDoc) bool {
	changed := false
	for id, theirs := range remote.Secrets {
		if ours, ok := local.Secrets[id]; !ok || supersedes(theirs, remote, ours, local) {
			local.Secrets[id], changed = theirs, true
		}
	}
	if remote.Updated > local.Updated {
		local.Updated, changed = remote.Updated, true
	}
	return changed
}

// supersedes reports whether slot a (in document ad) replaces slot b.
func supersedes(a record, ad *vaultDoc, b record, bd *vaultDoc) bool {
	if a == b {
		return false
	}
	sa, sb := a.stampOf(ad), b.stampOf(bd)
	return sa > sb || (sa == sb && a.Deleted && b.live())
}
