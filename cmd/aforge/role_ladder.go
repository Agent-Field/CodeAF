package main

import (
	"log"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The role ladder (5.23) is storage in the graph and a chip on the surface, and
// this is the one seam between the two where the knobs that came before it are
// folded in. Two things happen here and nothing else does:
//
// The compiled-in floor is installed on the handle — what each of the five
// roles resolves to when nothing is bound anywhere. It is this build's
// configuration rather than this brain file's history, so it is never
// journaled. Verify and scribe take the cheap model when configuration names
// one; nothing names one today, so they take the work model, which is the
// honest floor: steering nowhere must not cost more than not steering.
//
// AFORGE_PLAN_MODEL and --plan-model become the initializer of the global plan
// binding. They initialize and never override: the store writes only when the
// role is unbound or when a previous initializer said something else, so a boot
// with an unchanged environment journals nothing and an environment variable
// never quietly undoes what somebody chose in the palette.
//
// Nothing reads the bindings at dispatch yet. With no plan knob set, this
// function installs a floor and writes not one event, which is why existing
// behaviour is identical to the byte.
func installRoleLadder(graph *store.Store, talkModel, planModel, workModel, planKnob, planFlag string) {
	if graph == nil {
		return
	}
	// No cheap slot exists in configuration yet, so the fourth argument is
	// empty and NewRoleDefaults puts verify and scribe on the work model.
	graph.InstallRoleDefaults(store.NewRoleDefaults(talkModel, planModel, workModel, ""))
	value := strings.TrimSpace(planKnob)
	if value == "" {
		return
	}
	origin := store.RoleSeedOriginPrefix + "AFORGE_PLAN_MODEL"
	if strings.TrimSpace(planFlag) != "" {
		origin = store.RoleSeedOriginPrefix + "--plan-model"
	}
	// A journal write that fails is not a reason to fail the launch it is
	// describing — the plan client is already built from the same value — so
	// this is best-effort, and loud enough in the log to be findable.
	if _, err := graph.SeedRoleBinding(store.RolePlan, store.ScopeGlobal, value, origin); err != nil {
		log.Printf("note: could not seed the plan role binding: %v", err)
	}
}
