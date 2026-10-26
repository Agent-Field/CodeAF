package syncsetup

import (
	"errors"
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
	line, first := s.PeekFirstRun()
	if first {
		_, _ = s.AcknowledgeFirstRun()
	}
	return line, first
}

// PeekFirstRun reads the disclosure without counting it as shown. App surfaces
// acknowledge it only after the person could read it.
func (s *Sync) PeekFirstRun() (string, bool) {
	if s == nil || !s.Hosted {
		return "", false
	}
	marker := filepath.Join(s.Home, toldFile)
	if _, err := os.Stat(marker); err == nil {
		return "", false
	}
	return FirstRunLine, true
}

// AcknowledgeFirstRun writes the terminal's marker and reports whether it was
// still owed. A failed write leaves the disclosure owed for a later surface.
func (s *Sync) AcknowledgeFirstRun() (bool, error) {
	if _, owed := s.PeekFirstRun(); !owed {
		return false, nil
	}
	f, err := os.OpenFile(filepath.Join(s.Home, toldFile), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, writeErr := f.WriteString("told\n")
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		_ = os.Remove(filepath.Join(s.Home, toldFile))
		return false, err
	}
	return true, nil
}
