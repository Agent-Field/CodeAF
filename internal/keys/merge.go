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
// copy whose document `updated` is newer. Device clocks are trusted, which
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

// fold merges remote into local and reports whether local changed.
func fold(local, remote *vaultDoc) bool {
	changed := false
	for id, theirs := range remote.Secrets {
		if ours, ok := local.Secrets[id]; !ok || (ours != theirs && remote.Updated > local.Updated) {
			local.Secrets[id], changed = theirs, true
		}
	}
	if remote.Updated > local.Updated {
		local.Updated, changed = remote.Updated, true
	}
	return changed
}
