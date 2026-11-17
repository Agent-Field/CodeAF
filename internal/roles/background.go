package roles

// ── BACKGROUND SEATS FOLLOW THE MODEL THE PERSON PICKED ─────────────────────
//
// A BACKGROUND SEAT is a call nobody typed and no task asked for: the title a
// conversation names itself, the reflex that reads every finished turn, the
// router and its confirm, the suggested reply, the page AI, the Library's
// compiler. THE OWNER'S RULE (2026-11-16, F-20 of the workstreams-v5 live
// run): every one of them answers on the model the person — or, on an
// organization's server, the organization — chose. A person who picks one model
// is billed for one model.
//
// The rule used to be broken in two ways, and this file is where the second
// one is closed. The first was build-shipped models under the reflex and
// small-work rows (internal/config deleted them). The second was a background
// role registered on a CREW tier: the confirm the router asks before work
// starts itself sits on the mastermind tier, and an unpinned mastermind row
// reads the crew router's standing pick, so a person on one model saw a
// confirm billed to a model they had never heard of
// (`google/gemini-4-hydrogen` in that run).
//
// So a background role reads the tier rung ONLY on a tier that is not a crew
// seat — reflex and small work, the two rows a person writes precisely to make
// the cheap calls cheaper — and the crew tiers' rows are left to the work they
// govern. A role's own pin is always honoured: that is a person's choice for
// that exact call.

// crewWork are the roles whose calls are a TASK'S OWN WORK — the seats the
// Task Models settings govern (Auto, one model, the pins). Everything else a
// registry knows is background, except media, which is chosen by what a model
// can do ([IsMedia]).
//
// THE LIST IS EXHAUSTIVE BY LAW: TestEveryRoleIsCrewWorkBackgroundOrMedia
// names every role this build has and the side it sits on, so a new role
// lands with a decision rather than silently becoming background.
var crewWork = map[Role]bool{
	RolePlanner:    true,
	RoleDesigner:   true,
	RoleWorker:     true,
	RoleAuditor:    true,
	RoleRepair:     true,
	RoleCareful:    true,
	RoleDivision:   true,
	RoleShaper:     true,
	RoleMarkReader: true,
	RoleHandoff:    true,
}

// CrewWork reports whether a role's calls are a task's own work.
func CrewWork(role Role) bool { return crewWork[role] }

// Background reports whether a role is a background seat: neither a task's own
// work nor capability-chosen media. A background seat's model is its pin, then
// a reflex or small-work row the person wrote, then the model the person chose.
func Background(role Role) bool { return !crewWork[role] && !IsMedia(role) }

// crewTier reports whether a tier is one of the three crew seats' rows. Those
// rows are routed per task (an unpinned one reads the router's standing pick),
// which is exactly why a background role must not read them.
func crewTier(tier Tier) bool {
	return tier == TierWorker || tier == TierHigh || tier == TierMastermind
}

// readsTier reports whether role's ladder consults its tier row at all.
func readsTier(role Role, tier Tier) bool {
	return !(Background(role) && crewTier(tier))
}
