package standing

import (
	"errors"
	"strings"
	"time"
)

// Scope is an explicit governing binding, never a discovered association.
// Descendants is opt-in; direct placements are the default.
type Scope struct {
	CollectionIDs []string `json:"collection_ids"`
	Descendants   bool     `json:"descendants,omitempty"`
}

// Adoption is a runtime-stamped answer receipt. It proves an answer to this
// proposal, not that model-composed wording was an exact quote from the person.
type Adoption struct {
	Actor      string    `json:"actor"`
	ProposalID uint64    `json:"proposal_id"`
	At         time.Time `json:"at"`
	// Via names the door the yes came through: [DoorTerminal] for an item the
	// person wrote whole with `aforge standing add`, where there was no
	// proposal to answer and ProposalID stays zero, and [DoorChat] for a card
	// answered in a conversation. Empty is an item made before doors were
	// named, and it stays unknown rather than being guessed at.
	Via string `json:"via,omitempty"`
}

// The doors an item can be set up through, as [Adoption.Via] records them and
// `aforge standing show` says them back ("set up by: person, through the
// chat"). ONE SPELLING EACH: the two doors make the same item, and the receipt
// is the one place that says which of them the yes came through.
const (
	DoorTerminal = "terminal"
	DoorChat     = "chat"
)

func (it Item) validateScope() error {
	if it.Scope == nil {
		return nil
	}
	if it.When.Kind != WhenHold {
		return errors.New("folder scope is only available for a rule that does not wake")
	}
	if it.Altitude != "" {
		return errors.New("choose folder scope or altitude, not both")
	}
	if len(it.Scope.CollectionIDs) == 0 || len(it.Scope.CollectionIDs) > 32 {
		return errors.New("folder scope needs between 1 and 32 folders")
	}
	seen := make(map[string]bool)
	for _, id := range it.Scope.CollectionIDs {
		if strings.TrimSpace(id) != id || id == "" || len(id) > 4096 || strings.ContainsAny(id, "\r\n\x00") || seen[id] {
			return errors.New("folder scope needs distinct valid folder ids")
		}
		seen[id] = true
	}
	return nil
}

// AppliesToScope extends the existing owner resolver with authoritative folder
// depths supplied by the organization owner. References never enter this map.
func (it Item) AppliesToScope(workspace, sessionID string, collections map[string]int) bool {
	if it.Scope == nil {
		return it.AppliesTo(workspace, sessionID)
	}
	if it.validateScope() != nil || it.ExceptedFrom(workspace, sessionID) {
		return false
	}
	for _, id := range it.Scope.CollectionIDs {
		if depth, ok := collections[id]; ok && (depth == 0 || depth > 0 && it.Scope.Descendants) {
			return true
		}
	}
	return false
}
