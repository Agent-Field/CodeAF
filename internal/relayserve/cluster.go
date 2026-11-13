package relayserve

import "net/http"

// mountCluster keeps peer transport separate from pairing mailboxes, public
// command admission and the directory writer. A host with no enrolled members
// has no peer capability to advertise. The handler boundary avoids starting or
// linking a consensus runtime before the persistent host is assembled.
func mountCluster(mux *http.ServeMux, handler http.Handler) {
	if handler == nil {
		return
	}
	mux.Handle("/v2/cluster/", handler)
}
