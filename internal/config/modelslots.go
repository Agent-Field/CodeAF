package config

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The models group, derived rather than written down.
//
// There used to be a slice here — nine slot words in a var, kept "identical" to
// the palette's by hand — and it drifted the way a copied list always does: it
// carried a row for a boost chord that is a second door onto the work binding
// (8.2.16 forbids exactly that), and it carried NO row for two of the five
// roles the router actually routes. A person opening the sheet was told about a
// slot the product does not treat as a slot, and was not told about the model
// two whole classes of call run on.
//
// So the list is gone. The roles come from [store.ModelRoles], which is the
// router's own table and the only place the five are declared; the capability
// slots come from [mediaModalities], which is the table [ModelCandidates]
// already answers candidacy from. Both are asserted in settings_test.go: a
// sixth role, or a sixth modality, becomes a row here without anyone editing
// this file, and a role whose plain word nobody wrote fails the build rather
// than reaching a reader as machinery.

// ModelSlot is one row of the models group: what it binds, what it is called in
// the product's language (14), and what it falls back to when it is unset.
type ModelSlot struct {
	// Slot is the word the models door and [SettingsOptions.SetModel] take. For
	// the three roles the engine still holds a client for it is the legacy
	// engine word; for the other two it is the role itself.
	Slot string

	// Label is the plain word. Never the machinery name: nobody has an
	// "orchestrate model", they have a model that does the conversation.
	Label string

	// Role is the router role this row binds, empty for a capability slot.
	Role store.ModelRole

	// Follows names the row whose model this one uses while it is unset. It is
	// read as the row's empty reading, so an unset row says what will actually
	// run instead of showing a blank a reader would have to guess at.
	Follows string

	// Held says the running engine holds a client for this slot and can be
	// asked what it is on right now. The two roles nothing resolves yet are
	// not held, and asking the engine for them would get the conversation
	// model back — a wrong answer rendered as a confident one.
	Held bool
}

// roleWords is the plain word for every role the router has. It is a total
// function over [store.ModelRoles] and settings_test.go fails if it stops being
// one: a role with no word would otherwise reach the sheet spelled the way the
// journal spells it, which 14 bans on every surface.
var roleWords = map[store.ModelRole]string{
	store.RoleOrchestrate: "conversation",
	store.RolePlan:        "planning",
	store.RoleWork:        "execution",
	store.RoleVerify:      "verification",
	store.RoleScribe:      "naming",
}

// roleSlots maps a role onto the engine slot that still holds its client. Only
// three have one; the other two are bound in the roles table and resolved
// nowhere yet, so their row names the role itself and reads as what it follows.
var roleSlots = map[store.ModelRole]string{
	store.RoleOrchestrate: "talk",
	store.RolePlan:        "plan",
	store.RoleWork:        "work",
}

// mediaSlotWords is the plain word for each modality in [mediaModalities]. Same
// contract as roleWords: total over that table, asserted by a test.
var mediaSlotWords = map[string]string{
	"image":  "drawing",
	"speech": "speaking",
	"music":  "composing",
	"video":  "filming",
	"voice":  "voice",
}

// ModelSlots is the models group, in the order a person reaches for it: the
// five roles in the router's own ladder order first, because that is the order
// the ladder is described in and the order the first three are touched in, then
// the capability models.
func ModelSlots() []ModelSlot {
	roles := store.ModelRoles()
	slots := make([]ModelSlot, 0, len(roles)+len(mediaModalities))
	work := roleWords[store.RoleWork]
	for _, role := range roles {
		slot := ModelSlot{Role: role, Label: roleWord(role)}
		if engine, held := roleSlots[role]; held {
			slot.Slot, slot.Held = engine, true
		} else {
			slot.Slot = string(role)
		}
		// The roles table gives an unbound role the work model
		// (store.NewRoleDefaults), and the plan client is repointed live when
		// the work client moves. Both facts say the same sentence to a reader.
		if role != store.RoleOrchestrate && role != store.RoleWork {
			slot.Follows = work
		}
		slots = append(slots, slot)
	}
	for _, modality := range mediaModalities {
		slots = append(slots, ModelSlot{
			Slot:  modality,
			Label: mediaWord(modality),
			Held:  true,
		})
	}
	return slots
}

// ModelSlotFor finds one row of the models group by its slot word.
func ModelSlotFor(slot string) (ModelSlot, bool) {
	slot = strings.TrimSpace(slot)
	for _, candidate := range ModelSlots() {
		if candidate.Slot == slot {
			return candidate, true
		}
	}
	return ModelSlot{}, false
}

// roleWord degrades to the roles table's own word rather than to nothing: a
// role added without a plain word here fails the test, and until somebody fixes
// it the row is still there and still bindable.
func roleWord(role store.ModelRole) string {
	if word := roleWords[role]; word != "" {
		return word
	}
	if word := role.Word(); word != "" {
		return word
	}
	return string(role)
}

func mediaWord(modality string) string {
	if word := mediaSlotWords[modality]; word != "" {
		return word
	}
	return modality
}
