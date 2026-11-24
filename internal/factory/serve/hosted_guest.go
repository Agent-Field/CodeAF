package serve

import "net/http"

// RUN B fills this stub with the guest wire pump. Until then guest attaches receive the usual missing-chat reply.
func (h *Handler) hostedGuestWire(w http.ResponseWriter, _ *http.Request, _ Item, _ string, _ Viewer) {
	hostedNoSuchChatWrite(w)
}
