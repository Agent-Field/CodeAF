package session

import "github.com/Agent-Field/codeaf/internal/roles"

// The page AI rides the errand door: an app writes a repository's notes,
// answers a question about it, groups its backlog into themes, builds a
// pipeline, draws a factory's topic views or reads what arrived in a factory by
// asking an errand of one of these roles. The engine never makes the call itself, so the
// roles are registered here, beside the one door that answers them.
//
// THE FAMILY SITS ON THE LOW TIER. A page note is short grounded prose over a
// fact sheet the app already checked, asked often and cached by its input, so
// it is the small-work model's job, and an engine with no insight pin and no
// default answers it on exactly the model the person chose for small jobs.
func init() {
	roles.Register(roles.RoleInsight, roles.TierLow, "the model every page AI writes with")
	roles.RegisterUnder(roles.RoleInsightNotes, roles.RoleInsight, "a repository's notes since you last looked")
	roles.RegisterUnder(roles.RoleInsightAsk, roles.RoleInsight, "an answer to a question about a repository")
	roles.RegisterUnder(roles.RoleInsightThemes, roles.RoleInsight, "the themes in a repository's backlog")
	roles.RegisterUnder(roles.RoleInsightPipeline, roles.RoleInsight, "a pipeline's steps written with AI")
	roles.RegisterUnder(roles.RoleInsightViews, roles.RoleInsight, "a factory's topic views")
	roles.RegisterUnder(roles.RoleInsightFactoryRead, roles.RoleInsight, "the read of what arrived in a factory")
	roles.RegisterUnder(roles.RoleInsightFactoryScout, roles.RoleInsight, "a closer look at what arrived in a factory")
}
