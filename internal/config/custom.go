package config

import (
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
}
