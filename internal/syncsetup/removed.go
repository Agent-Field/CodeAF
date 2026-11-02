package syncsetup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// removedPath binds the refusal to this relay, identity and device key. A
// replacement device installed by pairing has a different path; old handles
// retain their refusal even after another window pairs this home again.
func (s *Sync) removedPath() string {
	key := sha256.Sum256([]byte(s.Ledger + "/" + s.Device.ID()))
	return filepath.Join(s.Home, "v3", "sync", "removed", hex.EncodeToString(key[:]))
}

func (s *Sync) isRemoved() bool {
	if s.removed.Load() {
		return true
	}
	_, err := os.Stat(s.removedPath())
	if err == nil {
		s.removed.Store(true)
	}
	return err == nil
}

// Removed follows the current pairing so an open Home can offer moves again
// after this computer has been added back.
func (s *Sync) Removed() bool {
	current, err := s.currentDisplaySync()
	return err != nil || current.isRemoved()
}

// rememberRemoved stops this handle immediately and shares that knowledge
// with other windows and later launches. Failure to save the marker cannot
// make a live handle resume sending requests.
func (s *Sync) rememberRemoved() {
	if !s.removed.Swap(true) {
		if member := s.displayMember(); member != nil {
			go member.owner.stopMember()
		}
	}
	path := s.removedPath()
	if os.MkdirAll(filepath.Dir(path), 0700) == nil {
		_ = os.WriteFile(path, nil, 0600)
	}
}

// removedReply preserves the directory's refusal through clients that wrap
// transport errors as unreachable. It is answered locally without a request.
func removedReply(r *http.Request) *http.Response {
	return &http.Response{StatusCode: http.StatusUnauthorized, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{"err":"revoked"}`)), Request: r}
}

func removedBody(raw []byte) bool {
	var body struct {
		Err string `json:"err"`
	}
	return json.Unmarshal(raw, &body) == nil && body.Err == "revoked"
}
