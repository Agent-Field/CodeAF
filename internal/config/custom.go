package config

import (
<<<<<<< HEAD
	"fmt"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// Custom connections: named, multiple, switchable.
//
// A custom connection is one instance of the vendored custom service. The
// first keeps the vendored id so profiles written before instances existed
// read back unchanged; every later one mints modelsource.CustomID-<slug> and
// is told apart by modelsource.IsCustomID. Routing still keys on the Written
// prefix of a model id, so the id is persistence vocabulary and the Written
// name is what a person's model ids carry. Nothing here may conflate them.

// PrepareCustomSource is the one door that mints a custom connection's
// persisted row, and both doors onto a connection (/connect and the Providers
// tab) call it before ConnectService, which stays the one validate, probe and
// persist path. written is the name the person gave or empty for the default
// the host slug answers; the id is minted from the name that will actually be
// used, first instance keeping the vendored id and later ones taking a numeric
// tiebreak when the slug is already taken.
func PrepareCustomSource(profileDir, address, written string) PersistedSource {
	address = strings.TrimRight(strings.TrimSpace(address), "/")
	if written = strings.TrimSpace(written); written == "" {
		// The host-to-name rule is modelsource's, next to the slug it feeds:
		// one spelling for config, the chat surface's draft and its address
		// step ([modelsource.AddressHost]).
		written = modelsource.SourceSlug(modelsource.AddressHost(address))
	}
	taken := make(map[string]bool)
	order := 0
	for _, row := range PersistedSources(profileDir) {
		taken[strings.ToLower(strings.TrimSpace(row.ID))] = true
		if row.Order > order {
			order = row.Order
		}
	}
	id := modelsource.CustomID
	if taken[strings.ToLower(id)] {
		base := modelsource.CustomID + "-" + modelsource.IDWord(written)
		id = base
		for n := 2; taken[strings.ToLower(id)]; n++ {
			id = base + "-" + strconv.Itoa(n)
		}
	}
	return PersistedSource{
		ID: id, Written: written, Address: address, Order: order + 1,
	}
}

// ActiveConnectionFor answers the service a conversation's live model answers on.
// THE ACTIVE CONNECTION IS DERIVED FROM THE CONVERSATION'S MODEL, NOT THE
// SHARED PROFILE: a caller holding the model this conversation actually runs
// ([app.model] in the talk surface, the deferred target while a move waits
// out a working turn) must not be routed through [ChatModelAt], which is the
// LAST model any conversation settled on and is written asynchronously by the
// engine host — two tabs on different connections would read each other's.
// The blank-model and empty-set guards are the only logic here; everything
// else is [Set.For], which answers with the default service when no connected
// Written prefix matches — so the switcher that hands this set around reaches
// the default service the same way the conversation itself does. False only
// when there are no services at all, or the conversation has settled on no
// model yet.
func ActiveConnectionFor(model string, sources modelsource.Set) (modelsource.Connected, bool) {
	if sources.Empty() || strings.TrimSpace(model) == "" {
		return modelsource.Connected{}, false
	}
	service, _ := sources.For(model)
	return service, true
}

// RenameConnectionModels carries a connection rename across every STORED model
// id the profile holds under the old Written name: the crew's five tier rows,
// the fallback chain, the role pins and the capability slots. The conversation
// slot's id is not stored vocabulary here — routing keys on the Written prefix
// of a model id, so a connection renamed from homelab to lab leaves every
// homelab/... id answering on a service that no longer exists, resolving to no
// service and handing itself to the default one: a silent misroute that only
// fails at send.
//
// THE NAME IS THE ONLY THING THAT MOVES, IN ONE WRITE. The id is re-spelled
// under the new prefix — the SAME model, spelled the new way — and nothing else
// about a row is touched; every re-prefixed value passes the tier gate
// ([ValidateTierValue]) and the whole decision lands through one
// [writeProfileValues], so a failure leaves the profile byte-identical. A tier
// row that was never held stays never held: writing one would convert an
// inherited tier into a pinned one. Only a row whose value actually changes is
// written.
//
// Node pins (the plan and work model recorded on already-created tasks) and
// journaled role bindings are historical records of what ran, and are not
// rewritten; store.SetRoleBinding has no caller outside the store.
func RenameConnectionModels(profileDir, oldWritten, newWritten string) (changed []string, err error) {
	oldWritten, newWritten = strings.TrimSpace(oldWritten), strings.TrimSpace(newWritten)
	if oldWritten == "" || newWritten == "" || strings.EqualFold(oldWritten, newWritten) {
		return nil, nil
	}
	updates := make(map[string]any)
	for _, tier := range ModelTiers {
		key := tierKeyFor(tier)
		value, held := persistedString(profileDir, key)
		if !held {
			continue
		}
		trimmed := strings.TrimSpace(value)
		next := ReprefixModelID(trimmed, oldWritten, newWritten)
		if next == trimmed {
			continue
		}
		if err := ValidateTierValue(next); err != nil {
			return nil, fmt.Errorf("move tier row %s to %s: %w", key, newWritten, err)
		}
		updates[key] = next
	}
	// THE FALLBACK CHAIN is a comma list of slugs, and a slug may carry a colon
	// of its own, so the split is commas and nothing else (config's own parse
	// rule for this row).
	if value, held := persistedString(profileDir, KeyModelFallbacks); held {
		if next := reprefixSlugList(strings.TrimSpace(value), oldWritten, newWritten); next != strings.TrimSpace(value) {
			updates[KeyModelFallbacks] = next
		}
	}
	// THE ROLE PINS are role:model pairs, the role cut at its first colon.
	if value, held := persistedString(profileDir, KeyModelRoles); held {
		if next := reprefixRolePins(strings.TrimSpace(value), oldWritten, newWritten); next != strings.TrimSpace(value) {
			updates[KeyModelRoles] = next
		}
	}
	// THE CAPABILITY SLOTS write into the profile through the same rows the
	// settings sheet writes; the role slots are the tier rows above.
	for _, slot := range ModelSlots() {
		if slot.Role != "" {
			continue
		}
		key := ModelSettingKey(slot.Slot)
		value, held := persistedString(profileDir, key)
		if !held || ReprefixModelID(strings.TrimSpace(value), oldWritten, newWritten) == strings.TrimSpace(value) {
			continue
		}
		updates[key] = ReprefixModelID(strings.TrimSpace(value), oldWritten, newWritten)
	}
	// THE REPORT IS THE ROWS THAT MOVED, in [ModelTiers] order for the tier
	// rows and then the fallback chain, the role pins and the capability slots
	// in the order [ModelSlots] renders them. A map has no order, and a caller
	// refreshing what it shows should not learn one from the map.
	for _, tier := range ModelTiers {
		key := tierKeyFor(tier)
		if _, ok := updates[key]; ok {
			changed = append(changed, key)
		}
	}
	for _, key := range []string{KeyModelFallbacks, KeyModelRoles} {
		if _, ok := updates[key]; ok {
			changed = append(changed, key)
		}
	}
	for _, slot := range ModelSlots() {
		if slot.Role != "" {
			continue
		}
		key := ModelSettingKey(slot.Slot)
		if _, ok := updates[key]; ok {
			changed = append(changed, key)
		}
	}
	if err := writeProfileValues(profileDir, updates); err != nil {
		return nil, fmt.Errorf("move stored models to %s: %w", newWritten, err)
	}
	return changed, nil
}

// ReprefixModelID rewrites one model id whose connection segment is the old
// Written name. Ids on other services, and bare ids, come back unchanged.
func ReprefixModelID(id, oldWritten, newWritten string) string {
	id = strings.TrimSpace(id)
	at := strings.Index(id, "/")
	if at <= 0 || !strings.EqualFold(id[:at], oldWritten) {
		return id
	}
	return newWritten + id[at:]
}

// reprefixSlugList rewrites every slug of a comma-separated model row, keeping
// the row's own shape when nothing in it carried the old name.
func reprefixSlugList(raw, oldWritten, newWritten string) string {
	out := make([]string, 0, strings.Count(raw, ",")+1)
	changed := false
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		next := ReprefixModelID(item, oldWritten, newWritten)
		changed = changed || next != item
		out = append(out, next)
	}
	if !changed {
		return strings.TrimSpace(raw)
	}
	return strings.Join(out, ", ")
}

// reprefixRolePins rewrites the model half of every role:model pair. The role
// separator is the FIRST colon, which is how config's own parse cuts the pair,
// and a model's own colons stay with the model.
func reprefixRolePins(raw, oldWritten, newWritten string) string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	})
	out := make([]string, 0, len(fields))
	changed := false
	for _, field := range fields {
		item := strings.TrimSpace(field)
		if item == "" {
			continue
		}
		role, model, hadModel := strings.Cut(item, ":")
		if !hadModel {
			out = append(out, item)
			continue
		}
		next := ReprefixModelID(model, oldWritten, newWritten)
		changed = changed || next != model
		out = append(out, role+":"+next)
	}
	if !changed {
		return strings.TrimSpace(raw)
	}
	return strings.Join(out, ", ")
||||||| a38492026
=======
	"net/url"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// Custom connections: named, multiple, switchable.
//
// A custom connection is one instance of the vendored custom service. The
// first keeps the vendored id so profiles written before instances existed
// read back unchanged; every later one mints modelsource.CustomID-<slug> and
// is told apart by modelsource.IsCustomID. Routing still keys on the Written
// prefix of a model id, so the id is persistence vocabulary and the Written
// name is what a person's model ids carry. Nothing here may conflate them.

// customAddressHost is the host a base URL is at, for the slug a new
// connection defaults its name from. An address the surface already validated
// is the normal case; an unparseable one falls back to the raw text so the
// slug still says something about what was given.
func customAddressHost(address string) string {
	address = strings.TrimSpace(address)
	parsed, err := url.Parse(address)
	if err != nil || strings.TrimSpace(parsed.Hostname()) == "" {
		return address
	}
	return parsed.Hostname()
}

// PrepareCustomSource is the one door that mints a custom connection's
// persisted row, and both doors onto a connection (/connect and the Providers
// tab) call it before ConnectService, which stays the one validate, probe and
// persist path. written is the name the person gave or empty for the default
// the host slug answers; the id is minted from the name that will actually be
// used, first instance keeping the vendored id and later ones taking a numeric
// tiebreak when the slug is already taken.
func PrepareCustomSource(profileDir, address, written string) PersistedSource {
	address = strings.TrimRight(strings.TrimSpace(address), "/")
	if written = strings.TrimSpace(written); written == "" {
		written = modelsource.SourceSlug(customAddressHost(address))
	}
	taken := make(map[string]bool)
	order := 0
	for _, row := range PersistedSources(profileDir) {
		taken[strings.ToLower(strings.TrimSpace(row.ID))] = true
		if row.Order > order {
			order = row.Order
		}
	}
	id := modelsource.CustomID
	if taken[strings.ToLower(id)] {
		base := modelsource.CustomID + "-" + strings.ToLower(written)
		id = base
		for n := 2; taken[strings.ToLower(id)]; n++ {
			id = base + "-" + strconv.Itoa(n)
		}
	}
	return PersistedSource{
		ID: id, Written: written, Address: address, Order: order + 1,
	}
}

// ActiveCustomSource reads which custom connection the conversation is on, so
// the Providers tab can show it and switch away from it. THE ACTIVE
// CONNECTION IS DERIVED, NEVER STORED: the conversation slot's model already
// carries the answer in its Written prefix, and a second key would be a
// second source of truth that can disagree with the model actually in use.
// Empty when the conversation is on the default service, on a vendored
// service, or on no service at all.
func ActiveCustomSource(profileDir string, sources modelsource.Set) (modelsource.Connected, bool) {
	segment, _ := modelsource.Split(ChatModelAt(profileDir), sources.Written())
	if segment == "" {
		return modelsource.Connected{}, false
	}
	for _, service := range sources.All() {
		if modelsource.IsCustomID(service.Source.ID) && strings.EqualFold(strings.TrimSpace(service.Source.Written), segment) {
			return service, true
		}
	}
	return modelsource.Connected{}, false
>>>>>>> feat/1089-custom-connections
}
