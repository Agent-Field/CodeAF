package syncsetup

// ANOTHER COMPUTER IS WHAT SHARING IS FOR. A computer that once showed a
// pairing code, or was paired with one that has since been removed, is no
// longer solo (identity.Solo), so it goes on sharing its chats. With no second
// computer to read them on, a share that fails costs its person nothing, and
// saying so in every chat reports a problem they do not have. The sentences
// that report a failed or running share are therefore said only while another
// computer is known ([Sync.OthersPaired]).
//
// KNOWN MEANS THE DIRECTORY SAID SO. Every complete roster read saves whether
// it lists a computer other than this one that was not removed, so the answer
// outlives a launch and a relay that cannot be reached. A computer that has
// never read a roster knows of none.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// othersPath binds the fact to this relay, identity and device, as removedPath
// binds a removal: a different pairing starts with none known.
func othersPath(home string, b deviceBinding) string {
	key := sha256.Sum256([]byte(b.relay + "\x00" + b.identity + "\x00" + b.self))
	return filepath.Join(home, "v3", "sync", "others", hex.EncodeToString(key[:]))
}

// rememberOthers saves what one complete roster said. It writes only when the
// answer changed, so the reads the engine makes anyway cost a stat.
func rememberOthers(home string, b deviceBinding, devices map[string]directory.Device) {
	if home == "" || b.self == "" {
		return
	}
	others := false
	for id, rec := range devices {
		if id != b.self && !rec.Revoked {
			others = true
			break
		}
	}
	path := othersPath(home, b)
	_, err := os.Stat(path)
	switch {
	case others && err != nil:
		if os.MkdirAll(filepath.Dir(path), 0o700) == nil {
			_ = os.WriteFile(path, nil, 0o600)
		}
	case !others && err == nil:
		_ = os.Remove(path)
	}
}

// OthersPaired says this person has another computer paired, as far as this
// one knows: the last complete roster it read listed one that was not removed.
func (s *Sync) OthersPaired() bool {
	_, err := os.Stat(othersPath(s.Home, deviceBindingFor(s.Relay, s.Identity, s.Device)))
	return err == nil
}
