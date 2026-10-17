package api

import "time"

// FactorySetup carries first-run progress from the engine, including unfinished reads after restart.
type FactorySetup struct {
	ID     string      `json:"id"`
	State  string      `json:"state" jsonschema:"enum=github,enum=repos,enum=reading,enum=checkouts,enum=done"`
	Mode   string      `json:"mode" jsonschema:"enum=solo,enum=team"`
	Server string      `json:"server,omitempty"`
	GitHub SetupGitHub `json:"github"`
	Repos  []SetupRepo `json:"repos"`
}
type SetupGitHub struct {
	// The connected credential's verified GitHub profile, distinct from the optional gh CLI account.
	Handle       string   `json:"handle,omitempty"`
	Name         string   `json:"name,omitempty"`
	AvatarURL    string   `json:"avatar_url,omitempty"`
	GH           *SetupGH `json:"gh"`
	Organization string   `json:"organization"`
	VisibleRepos int      `json:"visible_repos"`
	Connected    bool     `json:"connected"`
	Error        *string  `json:"error"`
}
type SetupGH struct {
	Login string `json:"login"`
	Path  string `json:"path"`
}
type SetupRepo struct {
	ID                string     `json:"id"`
	FullName          string     `json:"full_name"`
	OpenIssues        int        `json:"open_issues"`
	Pipeline          *string    `json:"pipeline" jsonschema:"enum=file,enum=defaults"`
	Archived          bool       `json:"archived"`
	Selected          bool       `json:"selected"`
	Checkout          *string    `json:"checkout"`
	SuggestedCheckout string     `json:"suggested_checkout"`
	CloneMB           int        `json:"clone_mb"`
	Read              string     `json:"read" jsonschema:"enum=next,enum=reading,enum=done,enum=empty,enum=waiting,enum=rate_limited"`
	ReadCount         int        `json:"read_count"`
	WaitingS          int        `json:"waiting_s"`
	ResumesAt         *time.Time `json:"resumes_at"`
	Clone             string     `json:"clone" jsonschema:"enum=missing,enum=cloning,enum=found,enum=skipped,enum=failed"`
	CloneError        *string    `json:"clone_error"`
}
type FactorySetupRequest struct {
	ID      string   `json:"id,omitempty"`
	Verb    string   `json:"verb" jsonschema:"enum=discover,enum=scope,enum=ingest,enum=retry,enum=connect,enum=select_repos,enum=retry_read,enum=clone,enum=checkout,enum=skip,enum=back,enum=done"`
	Source  string   `json:"source,omitempty" jsonschema:"enum=gh,enum=token"`
	Token   string   `json:"token,omitempty"`
	RepoIDs []string `json:"repo_ids,omitempty"`
	RepoID  string   `json:"repo_id,omitempty"`
	Path    string   `json:"path,omitempty"`
}

// FactoryAccess reports a link's validity separately from the caller's GitHub sign-in.
type FactoryAccess struct {
	// AccessToken is a completed native session grant; the app host consumes it before replying.
	AccessToken string        `json:"access_token,omitempty"`
	State       string        `json:"state" jsonschema:"enum=ready,enum=waiting,enum=joined,enum=expired,enum=revoked,enum=used_up,enum=no_access,enum=unreachable,enum=already_joined"`
	Factory     AccessFactory `json:"factory"`
	Link        string        `json:"link"`
	Detail      string        `json:"detail"`
	Message     string        `json:"message,omitempty"`
	Help        string        `json:"help,omitempty"`
	SignInURL   string        `json:"sign_in_url,omitempty"`
	UserCode    string        `json:"user_code,omitempty"`
	// MaxUses, Used and Remaining count the link's people; ExpiresAt is when
	// it stops admitting them, and EndedAt when a revoked, expired or used-up
	// link did. All stay empty for a link the factory did not recognise.
	MaxUses   int        `json:"max_uses,omitempty"`
	Used      int        `json:"used,omitempty"`
	Remaining int        `json:"remaining,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}
type AccessFactory struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Operator string `json:"operator"`
	// OperatorName and OperatorAvatarURL are that handle's GitHub profile as
	// recorded when they signed in; absent when no sign-in has recorded one.
	OperatorName      string   `json:"operator_name,omitempty"`
	OperatorAvatarURL string   `json:"operator_avatar_url,omitempty"`
	Repos             []string `json:"repos"`
}
type FactoryAccessRequest struct {
	Link string `json:"link"`
	Verb string `json:"verb" jsonschema:"enum=sign_in,enum=cancel"`
}

// FactoryDay counts visible jobs once per calendar day; unknown waiting time is null.
type FactoryDay struct {
	ID                   string   `json:"id"`
	Host                 string   `json:"host,omitempty"`
	Started              int      `json:"started"`
	Posted               int      `json:"posted"`
	Merged               int      `json:"merged"`
	WaitingMedianSeconds *float64 `json:"waiting_median_seconds"`
}

// JobDisplay is a binary-owned presentation rule, evaluated against the host clock.
type JobDisplay struct {
	RunningVisible bool `json:"running_visible"`
}
