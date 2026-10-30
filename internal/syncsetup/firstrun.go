package syncsetup

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// FirstRunLineFor formats the one sentence a person is told, once, when their
// chats start to sync through a hosted relay without them having chosen it. It
// asks for nothing: there is no sign-in and no consent wall, only the two ways
// out.
func FirstRunLineFor(relayURL string) string {
	shown := strings.TrimSpace(relayURL)
	if u, err := url.Parse(shown); err == nil && u.Host != "" {
		shown = u.Host + strings.TrimRight(u.Path, "/")
	}
	return "Your chats sync end-to-end encrypted through codeaf's fabric (" + shown + "), a sync service that only ever sees ciphertext; turn that off with CODEAF_SYNC_URL=off, or use your own with CODEAF_SYNC_URL=<url>."
}

// FirstRunLine is the first-run sentence for this binary's hosted sync service.
var FirstRunLine = FirstRunLineFor(HostedRelayURL)

// toldFile marks that the line has been said on this computer.
const toldFile = "sync-told"

// FirstRun is the line to say now, or false when there is nothing to say: the
// relay was chosen by the person, or the line was said before. Saying it is
// recorded here, so a caller cannot forget to. A failed write costs a repeat of
// one sentence, which is better than a silent relay.
func (s *Sync) FirstRun() (string, bool) {
	if s == nil || !s.Hosted {
		return "", false
	}
	marker := filepath.Join(s.Home, toldFile)
	if _, err := os.Stat(marker); err == nil {
		return "", false
	}
	_ = os.WriteFile(marker, []byte("told\n"), 0o600)
	return FirstRunLine, true
}
