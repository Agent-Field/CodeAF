package knowledge

import (
	"errors"
	"time"
)

// Resolution is a human choice kept with its edit, so the replaced account is
// still explainable after the compiler publishes another version of the guide.
type Resolution struct {
	Claims   []string `json:"claims"`
	Conflict string   `json:"conflict"`
	Keep     string   `json:"keep"`
	Chosen   string   `json:"chosen"`
	Replaced []string `json:"replaced"`
	Note     string   `json:"note,omitempty"`
	At       string   `json:"at"`
}

func (v *Vault) SettlePage(id, revision, conflict, keep, note string) error {
	p, err := v.ReadDetail(id)
	if err != nil {
		return err
	}
	if p.Revision != revision {
		return ErrPageChanged
	}
	var selected *Conflict
	for i := range p.Conflicts {
		if p.Conflicts[i].ID == conflict && p.Conflicts[i].State == "unresolved" {
			selected = &p.Conflicts[i]
			break
		}
	}
	if selected == nil || !contains(selected.Claims, keep) {
		return errors.New("choose an account in the current disagreement")
	}
	claims := map[string]ClaimDetail{}
	for _, c := range p.ClaimDetails {
		claims[c.ID] = c
	}
	chosen, ok := claims[keep]
	if !ok {
		return errors.New("the chosen account is unavailable")
	}
	replaced := []string{}
	for _, id := range selected.Claims {
		if id != keep {
			replaced = append(replaced, claims[id].Text)
		}
	}
	resolutions := append(append([]Resolution{}, p.Resolutions...), Resolution{Claims: selected.Claims, Conflict: conflict, Keep: keep, Chosen: chosen.Text, Replaced: replaced, Note: note, At: time.Now().UTC().Format(time.RFC3339Nano)})
	return v.changePage(id, revision, pageOverride{ID: id, ChoicesOnly: true, Resolutions: resolutions})
}
