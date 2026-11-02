package roles

import "fmt"

// The page-AI roles: the calls an app makes on an agent's errand door to write
// about what is on a page (a repository's notes, an answer to a question about
// it, the themes in its backlog, a pipeline built with AI, a factory's topic
// views, and a factory's arrival read and closer look). They are one FAMILY: `insight` is the model
// every page AI runs on, and each override is one page feature a person may
// point somewhere else. An override nobody pinned is the family's choice, so a
// person who sets `insight` once has set every page.
const (
	RoleInsight       Role = "insight"
	RoleInsightNotes  Role = "insight_notes"
	RoleInsightAsk    Role = "insight_ask"
	RoleInsightThemes Role = "insight_themes"
	// RoleInsightPipeline is a pipeline's steps written with AI.
	RoleInsightPipeline Role = "insight_pipeline"
	// RoleInsightViews is a factory's topic views.
	RoleInsightViews Role = "insight_views"
	// RoleInsightFactoryRead is the read of what arrived in a factory, and
	// RoleInsightFactoryScout is the closer look a person asks for after it.
	RoleInsightFactoryRead  Role = "insight_factory_read"
	RoleInsightFactoryScout Role = "insight_factory_scout"
)

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
