package desktopbridge

import (
	"net/http"

	"github.com/Agent-Field/codeaf/internal/config"
)

// PinnedModel is one pinned model: its id and the short word the composer's
// segmented control shows for it.
type PinnedModel struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// PinnedView is the three pinned models in segment order, and whether the
// person chose them.
type PinnedView struct {
	Pinned []PinnedModel `json:"pinned"`
	Chosen bool          `json:"chosen"`
}

func (m *Models) pinned() PinnedView {
	ids, chosen := config.DesktopPinned(m.ProfileDir)
	view := PinnedView{Chosen: chosen}
	for _, id := range ids {
		view.Pinned = append(view.Pinned, PinnedModel{ID: id, Label: config.DesktopPinnedLabel(id)})
	}
	return view
}

// pinnedRoute serves GET and PUT /models/pinned. A PUT carries {"models":[...]},
// three catalog models, or an empty list to go back to the default three.
func (b *Bridge) pinnedRoute(w http.ResponseWriter, r *http.Request, models *Models) {
	if r.Method == http.MethodGet {
		write(w, models.pinned())
		return
	}
	if r.Method != http.MethodPut {
		fail(w, 405, "GET or PUT required")
		return
	}
	var ask struct {
		Models []string `json:"models"`
	}
	if !decode(w, r, &ask) {
		return
	}
	for _, id := range ask.Models {
		if !models.allows(r.Context(), id) {
			fail(w, 400, "that model is not on the list")
			return
		}
	}
	if err := config.WriteDesktopPinned(models.ProfileDir, ask.Models); err != nil {
		fail(w, 400, err.Error())
		return
	}
	write(w, models.pinned())
}
