package config

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// ── the page AI's roles in the task models ─────────────────────────────────
//
// The page AI ([roles.RoleInsight] and its overrides) is pinned in the role
// pins row ([KeyModelRoles]), the same row every other role's pin lives in, so
// the task models add rows and no store. AN UNPINNED PAGE AI HAS NO MODEL OF
// ITS OWN: it is a background seat, and follows its small-work row when the
// person wrote one, then the model the person chose (internal/roles'
// background.go). Until 2026-11-16 the engine answered `deepseek/deepseek-v4.1-flash`
// here for everybody, which a person who chose another model never agreed to.

// InsightRoles is the family first, then its overrides.
func InsightRoles() []roles.Role {
	return append([]roles.Role{roles.RoleInsight}, roles.InsightOverrides...)
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
