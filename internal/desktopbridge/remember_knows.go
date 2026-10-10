package desktopbridge

import "net/http"

func init() {
	registerPlacesRoute("DELETE /places/knows", deleteRememberedLine)
}

// The chat supplies a content-bound token, so unrelated place writes cannot prevent Undo.
func deleteRememberedLine(p *Places, w http.ResponseWriter, r *http.Request, _ string) {
	var ask struct {
		Token string `json:"token"`
	}
	if !readBody(w, r, &ask) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	revision, err := p.Store.UndoRemember(ask.Token)
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	write(w, map[string]any{"revision": revision, "removed": true})
	p.publishPlaces(nil)
}
