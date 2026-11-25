package serve

import (
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── THE ORGANIZATION'S REPOSITORIES ────────────────────────────────────────
//
// On an organization's server a person used to read exactly the repositories
// their OWN GitHub token reached, so a person who signed in with a password or
// a passkey read none and could never make a job (film D10). The server's own
// GitHub grant (the one its poll reads with) only fed the poll.
//
// Now the organization has repositories of its own, and the rules are:
//
//   - THE ORGANIZATION'S REPOSITORIES ARE THE ONES TURNED ON IN REPOS: the
//     server's watched list. Only the operator turns one on or off
//     (repos_v2.go), and only from what the server's grant can reach, so
//     every repository here is the operator's explicit choice.
//   - EVERY SIGNED-IN MEMBER READS THEM, with or without a GitHub account of
//     their own. A member whose token reads more keeps reading that too: this
//     path only ADDS, and a person's own token is asked exactly as before.
//   - THE OPERATOR ALSO READS what the server's grant can reach and is not on
//     yet, because Repos is where they choose from it.
//   - A JOB ON A REPOSITORY A PERSON READS ONLY THROUGH THE ORGANIZATION is
//     made in a workstream that person can edit (POST /v1/jobs with
//     workstream, verbs.go), so a view-only member reads jobs and makes none.
//
// A solo factory has no organization: its owner reads everything already, and
// nothing here applies.

// orgRepos is the set of owner/name keys, lower case, the organization lets
// handle read. Empty, never nil, when the factory is not an organization's or
// a read fails: a store that cannot answer opens nothing.
func (h *Handler) orgRepos(r *http.Request, handle string) map[string]bool {
	out := map[string]bool{}
	if h.mode() != api.FactoryTeam || h.config.Store == nil || handle == "" {
		return out
	}
	watched, err := h.config.Store.Repos()
	if err != nil {
		return out
	}
	for _, name := range watched {
		addRepoKey(out, name)
	}
	if isBotCall(r) || handle != h.operatorLogin(r) {
		return out
	}
	// The operator chooses from what the server's grant reached at its last
	// read; a read nobody has made yet adds nothing.
	if available, err := h.config.Store.AvailableRepos(); err == nil {
		for _, repo := range available.Repos {
			addRepoKey(out, repo.Full)
		}
	}
	return out
}

func addRepoKey(set map[string]bool, name string) {
	key := strings.ToLower(strings.TrimSpace(name))
	if strings.Count(key, "/") == 1 {
		set[key] = true
	}
}

// repoOnlyThroughOrg is whether the caller reads repo only because the
// organization turned it on: not as the owner, not as the operator, and not
// through a GitHub token of their own.
func (h *Handler) repoOnlyThroughOrg(r *http.Request, repo string) bool {
	if h.isOperator(r) {
		return false
	}
	key := strings.ToLower(strings.TrimSpace(repo))
	sight, filter := h.repoSight(r, []string{key})
	return filter && sight.viaOrg[key]
}

// markNeedsWorkstream marks the repositories the caller reads only through the
// organization: a job on one is refused without a workstream they can edit
// (newJobWorkstream), so the New job sheet asks for one up front. The same
// sight repoOnlyThroughOrg reads, taken once for the whole list.
func (h *Handler) markNeedsWorkstream(r *http.Request, repos []api.Repo) {
	keys := make([]string, len(repos))
	for i, repo := range repos {
		keys[i] = strings.ToLower(strings.TrimSpace(repo.Name))
	}
	sight, filter := h.repoSight(r, keys)
	if !filter {
		return
	}
	for i := range repos {
		repos[i].NeedsWorkstream = sight.viaOrg[keys[i]]
	}
}
