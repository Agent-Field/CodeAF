package roles

import "fmt"

// InsightOverrides are the family's per-feature roles, in the order a settings
// screen lists them.
var InsightOverrides = []Role{RoleInsightNotes, RoleInsightAsk, RoleInsightThemes,
	RoleInsightPipeline, RoleInsightViews, RoleInsightFactoryRead, RoleInsightFactoryScout}

// families is each override's family role, written once by [RegisterUnder].
var families = map[Role]Role{}

// RegisterUnder registers role as an override of family: it takes the
// family's tier, and its ladder reads the family's pin and default after its
// own pin and before the tier. The family must already be registered and must
// not itself be an override, so a ladder is never more than two roles deep.
func RegisterUnder(role, family Role, description ...string) {
	registryMu.RLock()
	tier, ok := registry[family]
	_, nested := families[family]
	registryMu.RUnlock()
	if !ok || nested {
		panic(fmt.Sprintf("roles: %q cannot be registered under %q, which is not a registered family", role, family))
	}
	Register(role, tier, description...)
	registryMu.Lock()
	families[role] = family
	registryMu.Unlock()
}

// FamilyOf is the role an override falls back to, and false for a role that
// has no family.
func FamilyOf(role Role) (Role, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	family, ok := families[role]
	return family, ok
}

const defaultPrefix = "defaults."

// DefaultKey is the source key of a role's default: the model the engine
// answers for a role nobody pinned, before its tier. It is NOT A PIN and is
// never written to settings; whoever builds a [Source] answers it from what
// it knows (a provider connected, a model allowed), so a person's own pin is
// never overwritten and a default that stops applying simply stops answering.
func DefaultKey(role Role) string { return defaultPrefix + string(role) }

// lineage is the roles whose pins and defaults a role's ladder reads, most
// specific first: the role, then its family when it has one.
func lineage(role Role) []Role {
	if family, ok := FamilyOf(role); ok {
		return []Role{role, family}
	}
	return []Role{role}
}
