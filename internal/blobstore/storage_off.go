package blobstore

import "net/http"

// pathHandoff is where a computer that is rotating its identity asks the relay to carry the storage permission over to the new
// one (directory.HTTP.HandoffStorage). The hosted relay answers it; this file is why a relay with its storage off does too.
const pathHandoff = "/v1/store/handoff"

// StorageOff makes the store wire answer the way the hosted relay does for an account it has not approved to keep chats: it says
// its storage is off and refuses every upload with `storage_off`, and nothing else about it changes. A computer that reads that
// keeps its chats where they are made, says so once, and stops asking, which is what it already does on the hosted relay.
//
// WHY A RELAY WOULD WANT THIS. An organization's relay (deploy/org) exists so its members' computers can pair, see each other and
// open each other's chats live. The organization's hosted chats are already kept by the organization's own server, so a second,
// sealed copy of anyone's chats on the relay is storage nobody asked for.
//
// IT IS ONE LAYER OVER THE REAL HANDLER, NOT A SECOND STORE. Reads (has, locate, get) still reach the store, which is empty, so a
// computer that asks for something is told there is none. Without this layer a native relay's stats omit `storage`, a computer
// reads that as "on", uploads, is refused by a proxy in front, marks the relay off, and refreshes back to "on" a minute later.
//
// THE STATS AND THE HANDOFF ARE ANSWERED WITHOUT CHECKING A SIGNATURE, because they carry nothing of anyone's: the first is a fixed
// answer and the second changes nothing, since there is no permission here to hand over.
func StorageOff(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == pathStats:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(mustJSON(statsBody{Storage: "off"}))
		case r.Method == http.MethodPost && r.URL.Path == pathFrames:
			send(w, http.StatusForbidden, "storage_off", 0)
		case r.Method == http.MethodPost && r.URL.Path == pathHandoff:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{}"))
		default:
			next.ServeHTTP(w, r)
		}
	})
}
