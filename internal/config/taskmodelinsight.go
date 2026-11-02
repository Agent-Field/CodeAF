package config

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// ── the page AI's roles in the task models ─────────────────────────────────
//
// The page AI ([roles.RoleInsight] and its overrides) is pinned in the role
// pins row ([KeyModelRoles]), the same row every other role's pin lives in, so
// the task models add rows and no store. Its default is the engine's own and
// is NEVER WRITTEN: a written default would be indistinguishable from a
// person's choice, would survive the provider it needs being disconnected or
// the model being unticked, and would be overwritten by nothing. Answered at
// read time instead, it is there exactly while it can run and gone the moment
// it cannot, and the first pin a person writes simply outranks it.

// InsightDefault is the model every page AI runs on when nobody pinned one,
// on the provider it is sent through.
var InsightDefault = CrewPin{Model: "deepseek/deepseek-v4.1-flash", Provider: modelsource.DefaultID}

// InsightRoles is the family first, then its overrides.
func InsightRoles() []roles.Role {
	return append([]roles.Role{roles.RoleInsight}, roles.InsightOverrides...)
}

// InsightDefaultAt is [InsightDefault] as this profile sends it, and false
// while its provider is not connected or turned off, or the allowed models do
// not admit it, when the page AI follows its tier instead.
func InsightDefaultAt(profileDir string) (string, bool) {
	route, ok := insightDefaultRoute(CrewProvidersAt(profileDir), CrewAllowedAt(profileDir))
	return route.Send, ok
}

func insightDefaultRoute(providers []CrewProvider, rule crewroute.Allowed) (crewroute.Pin, bool) {
	p, ok := crewProviderByID(providers, InsightDefault.Provider)
	if !ok || !p.On || !crewPinAdmitted(rule, InsightDefault.Model, p.ID) {
		return crewroute.Pin{}, false
	}
	return resolveCrewPin(InsightDefault, providers), true
}

// insightPinsAt is each page-AI role's pin as the role pins row holds it, with
// the provider the send goes through.
func insightPinsAt(profileDir string, providers []CrewProvider) map[roles.Role]CrewPin {
	written, _ := ParseModelRoles(ModelRolesAt(profileDir))
	pins := map[roles.Role]CrewPin{}
	for _, role := range InsightRoles() {
		pin, auto, err := ParseCrewPin(written[string(role)])
		if auto || err != nil {
			continue
		}
		if pin.Provider == "" {
			pin.Provider = resolveCrewPin(pin, providers).Provider
		}
		pins[role] = pin
	}
	return pins
}

// taskInsightRoles is the page AI's rows: each pin with its warning, the
// default on the family row, and the available models any of them could be
// pinned to.
func taskInsightRoles(profileDir string, providers []CrewProvider, rows []TaskModelRow, rule *crewroute.Allowed, health crewHealth) map[roles.Role]TaskModelRole {
	pins := insightPinsAt(profileDir, providers)
	out := map[roles.Role]TaskModelRole{}
	for _, role := range InsightRoles() {
		var pin *CrewPin
		if held, ok := pins[role]; ok {
			pin = &held
		}
		out[role] = taskErrandRole(pin, providers, rows, rule, health)
	}
	if route, ok := insightDefaultRoute(providers, *rule); ok {
		family := out[roles.RoleInsight]
		family.Default = &CrewPin{Model: InsightDefault.Model, Provider: route.Provider}
		out[roles.RoleInsight] = family
	}
	return out
}

// insightPinsRow is the role pins row with the change's page-AI pins applied:
// every other entry kept exactly as the person wrote it, each named role's
// entry taken out, and each new pin appended as the model it is sent as.
func insightPinsRow(profileDir string, rows []TaskModelRow, providers []CrewProvider, change TaskModelsChange, rule crewroute.Allowed) (any, error) {
	kept := []string{}
	for _, item := range pairItems(ModelRolesAt(profileDir)) {
		name, _, _ := strings.Cut(item, ":")
		if _, named := change.RolePins[roles.Role(roles.RoleKey(name))]; !named {
			kept = append(kept, item)
		}
	}
	for _, role := range InsightRoles() {
		ref := change.RolePins[role]
		if ref == nil {
			continue
		}
		pin, err := taskModelsPin(rows, providers, change.Served, *ref, "the "+string(role)+" pin")
		if err != nil {
			return nil, err
		}
		route := resolveCrewPin(pin, providers)
		if !crewPinAdmitted(rule, pin.Model, route.Provider) {
			return nil, refuse("%s is not allowed on %s · allow it before pinning it", pin.Model, taskProviderName(providers, route.Provider))
		}
		kept = append(kept, string(role)+":"+route.Send)
	}
	if len(kept) == 0 {
		return removeProfileKey, nil
	}
	return strings.Join(kept, ", "), nil
}
