package serve

import (
	"fmt"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func (h *Handler) waitsParentsVisible(w http.ResponseWriter, r *http.Request, ids []int, parents map[int]factory.Item) bool {
	for _, id := range ids {
		parent, ok := parents[id]
		visible := ok && factory.IsIssueJob(parent) && h.repoVisible(r, itemRepo(parent))
		if h.authFailed(w, r) {
			return false
		}
		if !visible {
			h.fail(w, http.StatusUnprocessableEntity, fmt.Sprintf("job %d does not exist in this factory", id))
			return false
		}
	}
	return true
}
