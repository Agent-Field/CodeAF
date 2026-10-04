package syncsetup

import (
	"os"
	"path/filepath"
)

// FirstRunLine is the one sentence a person is told, once, when their chats
// start to sync through the hosted relay without them having chosen it. It asks
// for nothing: there is no sign-in and no consent wall, only the two ways out.
const FirstRunLine = "Your chats sync end-to-end encrypted through codeaf's fabric (codeaf.agentfield.ai/fabric), a sync service that only ever sees ciphertext; turn that off with CODEAF_SYNC_URL=off, or use your own with CODEAF_SYNC_URL=<url>."

// toldFile marks that the line has been said on this computer.
const toldFile = "sync-told"

// FirstRun is the line to say now, or false when there is nothing to say: the
// relay was chosen by the person, or the line was said before. Saying it is
// recorded here, so a caller cannot forget to. A failed write costs a repeat of
// one sentence, which is better than a silent relay.
func (s *Sync) FirstRun() (string, bool) {
	if !s.Hosted {
		return "", false
	}
	marker := filepath.Join(s.Home, toldFile)
	if _, err := os.Stat(marker); err == nil {
		return "", false
	}
	_ = os.WriteFile(marker, []byte("told\n"), 0o600)
	return FirstRunLine, true
}
