package syncsetup

// ANOTHER COMPUTER IS WHAT SHARING IS FOR. A computer that once showed a
// pairing code, or was paired with one that has since been removed, is no
// longer solo (identity.Solo), so it goes on sharing its chats. With no second
// computer to read them on, a share that fails costs its person nothing, and
// saying so in every chat reports a problem they do not have. The sentences
// that report a failed or running share are therefore said only while another
// computer is known ([Sync.OthersPaired]).
//
// KNOWN MEANS THE DIRECTORY SAID SO, OR A PAIRING JUST FINISHED. Every complete
// roster read saves whether it lists a computer other than this one that was
// not removed, so the answer outlives a launch and a relay that cannot be
// reached, and a finished pairing saves that there is one ([NotePaired]) until
// the next roster says otherwise. A computer that has done neither knows of none.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"

	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
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
	others := false
	for id, rec := range devices {
		// A PHONE IS NOT ANOTHER COMPUTER of this identity: there is no chat to carry to
		// it, and counting it would make a computer that only serves a phone behave as
		// if it shared its chats (contract P13).
		if id != b.self && !rec.Revoked && rec.Kind != clientproto.ClientLinkClass {
			others = true
			break
		}
	}
	setOthers(home, b, others)
}

// NotePaired is what a pairing that just finished tells this home: another
// computer of this identity is in the fleet. THE FIRST LAUNCH AFTER A PAIRING
// HAS READ NO ROSTER YET, and without this it knew of nobody to send a chat to
// on exactly the launch where a person looks for blind pass-through, and said
// the sentence for a computer on its own. The pairing is only the first word:
// the next complete roster replaces it through [rememberOthers], so a computer
// whose other was removed in the meantime stops carrying chats. relayURL is the
// service the pairing went through; it counts only on the relay this home
// syncs with, as every roster does.
func NotePaired(home, relayURL string) error {
	id, err := identity.Load(home)
	if err != nil {
		return err
	}
	dev, err := identity.Device(home)
	if err != nil {
		return err
	}
	setOthers(home, deviceBindingFor(relayURL, id, dev), true)
	return nil
}

// setOthers keeps the answer on disk and tells the loops and listeners of this
// process when it changed, in either direction.
func setOthers(home string, b deviceBinding, others bool) {
	if home == "" || b.self == "" {
		return
	}
	path := othersPath(home, b)
	_, err := os.Stat(path)
	switch {
	case others && err != nil:
		if os.MkdirAll(filepath.Dir(path), 0o700) != nil || os.WriteFile(path, nil, 0o600) != nil {
			return
		}
		// A computer that just learned of another may now share live.
		notifyLive(home)
		othersChanged(home)
	case !others && err == nil:
		if os.Remove(path) == nil {
			othersChanged(home)
		}
	}
}

// othersListeners are told, in this process, that the answer to "is another
// computer paired" changed for a home. The sentence a person was shown about
// closed storage depends on it ([chatlist.StorageOffSentence]), so a surface
// that showed one asks here when to say it again.
var othersListeners = struct {
	sync.Mutex
	next int
	by   map[int]func(home string)
}{by: map[int]func(string){}}

// OnOthersChange registers f to hear, with the home concerned, every change of
// whether another computer is paired: one became known, or the last one was
// removed. It is not told of a read that changes nothing. f runs on the
// goroutine that read the roster, so it must not block.
func OnOthersChange(f func(home string)) (stop func()) {
	othersListeners.Lock()
	defer othersListeners.Unlock()
	othersListeners.next++
	id := othersListeners.next
	othersListeners.by[id] = f
	return func() {
		othersListeners.Lock()
		defer othersListeners.Unlock()
		delete(othersListeners.by, id)
	}
}

func othersChanged(home string) {
	othersListeners.Lock()
	var tell []func(string)
	for _, f := range othersListeners.by {
		tell = append(tell, f)
	}
	othersListeners.Unlock()
	for _, f := range tell {
		f(home)
	}
}

// OthersPaired says this person has another computer paired, as far as this
// one knows: the last complete roster it read listed one that was not removed.
func (s *Sync) OthersPaired() bool {
	_, err := os.Stat(othersPath(s.Home, deviceBindingFor(s.Relay, s.Identity, s.Device)))
	return err == nil
}
