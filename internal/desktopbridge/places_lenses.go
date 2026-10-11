package desktopbridge

import (
	"net/http"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

func getLenses(p *Places, w http.ResponseWriter, r *http.Request, id string) {
	if !needGet(w, r) {
		return
	}
	org := p.organizer()
	if org == nil || org.State == nil {
		fail(w, 404, "unknown engine action")
		return
	}
	snap, err := p.advice.snapshot()
	if err != nil {
		failPlaces(w, 500, "store", "Places could not be read.")
		return
	}
	if _, ok := snap.Place(id); !ok {
		failPlaces(w, 404, "unknown_place", "That place isn't in the list.")
		return
	}
	org.mu.Lock()
	lenses := append([]placeorg.Lens(nil), org.State.Lenses[id]...)
	org.mu.Unlock()
	out := make([]lensWire, 0, len(lenses))
	for _, lens := range lenses {
		chats := lens.Members
		if chats == nil {
			chats = []string{}
		}
		out = append(out, lensWire{Name: lens.Name, Axis: lens.Axis, Value: lens.Value, Chats: chats})
	}
	write(w, map[string]any{"lenses": out})
}

// lensWire uses chats, the word the desktop reads. The organiser stores the
// same list as members.
type lensWire struct {
	Name  string   `json:"name"`
	Axis  string   `json:"axis"`
	Value string   `json:"value"`
	Chats []string `json:"chats"`
}
