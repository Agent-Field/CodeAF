package factory

import (
	"context"
	"time"
)

// ── THE FLOOR'S OWN SETTINGS ────────────────────────────────────────────────
//
// There is no settings page for the factory. Each setting lives where a person
// looks when they need it: which repositories the floor watches is `R` on the
// floor, a repository's recipe is `E`, the day's rail is `$` on the handover,
// and how this machine reaches GitHub is one row on the settings place. What
// is here is the plain values those doors trade in, so the surface never
// imports a forge package to draw a list of repositories.

// RepoInfo is one repository the connected forge account can see, as the
// picker lists it: `owner/name`, its two halves, whether it is private and
// when it was last pushed to.
type RepoInfo struct {
	Full    string
	Name    string
	Owner   string
	Private bool
	Pushed  time.Time
	// Open is how many issues and pull requests are open on it, as the forge
	// lists it (GitHub's open_issues_count counts both), and -1 when the forge
	// did not say.
	Open int
	// Dir is where this machine has it checked out, "" when it does not know;
	// the floor's stages run only where it is known.
	Dir string
}

// GitHubLink is how this machine reaches GitHub: the login the token belongs
// to and the way the token was found. Via is one of [ViaGH] (the person said
// yes to gh's own login), [ViaToken] (a token kept in the profile) and
// [ViaEnv] (GH_TOKEN or GITHUB_TOKEN in the environment). A ZERO LINK IS NO
// TOKEN AT ALL, and the floor then offers to connect rather than a list.
type GitHubLink struct {
	Login string
	Via   string
}

// The three ways a token is found, as the settings row says them.
const (
	ViaGH    = "gh"
	ViaToken = "token"
	ViaEnv   = "env"
)

// Connected says a token resolves.
func (l GitHubLink) Connected() bool { return l.Via != "" }

// RepoKeeper is what the local seam needs of a store to keep the watched
// repositories, and internal/factory/store's *Store answers it. It is asked
// of the [ItemStore] by assertion, so a store that keeps no repositories
// leaves the picker's doors nil.
type RepoKeeper interface {
	Repos() ([]string, error)
	SetRepos([]string) error
}

// RailKeeper is what the local seam needs of a store to keep the day's rail,
// asked by assertion on the same terms as [RepoKeeper].
type RailKeeper interface {
	Rail() (float64, error)
	SetRail(usd float64) error
}

// RepoLister lists every repository the connected account can see, most
// recently pushed first.
type RepoLister func(ctx context.Context) ([]RepoInfo, error)

// WithRepoLister hands the local seam the forge's list for the picker. The
// launch that opens the store passes the GitHub source's ListRepos here;
// without it the picker lists only the repositories already watched.
func WithRepoLister(list RepoLister) LocalOption {
	return func(o *localOptions) { o.lister = list }
}
