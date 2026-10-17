package api

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

// ReasonGitHubExpired tells the app to start GitHub sign-in again.
const ReasonGitHubExpired = "github_expired"

// AuthFailure is a refused sign-in; the reason is independent of its wording.
type AuthFailure struct {
	Factory   Factory `json:"factory"`
	Error     string  `json:"error"`
	Reason    string  `json:"reason"`
	RequestID string  `json:"request_id"`
}

// Profile is the GitHub identity checked at sign-in, never a client claim.
type Profile = tokens.Profile

// Me describes this caller. machines are that handle's own; another person's
// machine is not listed here.
type Me struct {
	Factory       Factory `json:"factory"`
	CodeafVersion string  `json:"codeaf_version"`
	Contract      int     `json:"contract"`
	Profile
	Mode      string   `json:"mode"`
	Operator  bool     `json:"operator"`
	Runners   []Runner `json:"runners"`
	SessionID string   `json:"session_id"`
	// GitHubSignedIn is asserted only by the serving factory, and only when the
	// authenticated credential carries a GitHub-verified profile. A session_ or
	// tok_ id alone proves nothing; an older server omits the field, so absent
	// means false and no other field may promote it.
	GitHubSignedIn  bool            `json:"github_signed_in"`
	Machines        []Machine       `json:"machines"`
	TakesUnassigned map[string]bool `json:"takes_unassigned"`
	Access          string          `json:"access"`
	PollingEvery    string          `json:"polling_every,omitempty"`
	ServedBy        string          `json:"served_by"`
}

// Device describes the code to enter on GitHub and the opaque code to poll here.
type Device struct {
	Factory         Factory `json:"factory"`
	DeviceCode      string  `json:"device_code"`
	UserCode        string  `json:"user_code"`
	VerificationURI string  `json:"verification_uri"`
	Interval        int     `json:"interval"`
	ExpiresIn       int     `json:"expires_in"`
}

// DevicePoll exchanges a code issued to this caller only.
type DevicePoll struct {
	DeviceCode string `json:"device_code"`
}

// SignedIn returns a factory credential, never the GitHub access token.
type SignedIn struct {
	Factory     Factory `json:"factory"`
	AccessToken string  `json:"access_token"`
	TokenType   string  `json:"token_type"`
	ExpiresIn   int     `json:"expires_in"`
	Me          Me      `json:"me"`
}

// Member is one GitHub person who has signed in. Sessions is how many of
// their sign-ins are still live, not a list of those sign-ins.
type Member struct {
	Handle    string    `json:"handle"`
	Name      string    `json:"name"`
	AvatarURL string    `json:"avatar_url"`
	JoinedAt  time.Time `json:"joined_at"`
	LastSeen  time.Time `json:"last_seen"`
	Sessions  int       `json:"sessions"`
}

// Members is `GET /v1/members`.
type Members struct {
	Factory Factory  `json:"factory"`
	Members []Member `json:"members"`
}

// Session is one sign-in of the caller. Device is the label captured then.
type Session struct {
	ID       string    `json:"id"`
	Device   string    `json:"device"`
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"last_seen"`
	Current  bool      `json:"current"`
}

// Sessions is `GET /v1/me/sessions`.
type Sessions struct {
	Factory  Factory   `json:"factory"`
	Sessions []Session `json:"sessions"`
}

// SignedOut is `POST /v1/me/sessions/signout-others`.
type SignedOut struct {
	Factory   Factory `json:"factory"`
	SignedOut int     `json:"signed_out"`
}
