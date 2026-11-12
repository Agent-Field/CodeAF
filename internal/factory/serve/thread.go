package serve

import (
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"net/http"
)

// thread reads the replicated job, never a path on the factory machine.
func (h *Handler) thread(w http.ResponseWriter, r *http.Request) {
	it, ok := h.findJob(w, r)
	if !ok {
		return
	}
	messages := append([]api.Message{}, it.Thread...)
	h.write(w, http.StatusOK, struct {
		Factory  api.Factory   `json:"factory"`
		Messages []api.Message `json:"messages"`
	}{h.factory, messages})
}
