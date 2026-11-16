package api

import "time"

// ── PASSKEYS (WebAuthn) ON AN ORGANIZATION SERVER ───────────────────────────
//
// A passkey ceremony runs in the person's own browser on a page the server
// serves, because only a page whose origin is the server's may use a passkey
// for it. The client that wants the session (the engine) never sees the
// ceremony: it starts a flow with the SHA-256 of a secret verifier it keeps,
// opens the flow's url in the browser, and polls with the verifier until the
// browser has finished. The shape is OAuth's PKCE: a stolen url alone cannot
// mint a session, because the poll needs the verifier, which never left the
// engine.

// Passkey flow purposes.
const (
	PasskeySignIn = "sign_in"
	PasskeyJoin   = "join"
	PasskeyAdd    = "add"
)

// PasskeyStart is POST /v1/auth/passkey/start. CodeChallenge is base64url
// (no padding) of SHA-256 of a 32-byte verifier. Link is an invitation, an
// operator claim or a reset link, for join. Username is the name the person
// already chose on a join (optional; the page asks for one otherwise).
type PasskeyStart struct {
	Purpose       string `json:"purpose"`
	Link          string `json:"link,omitempty"`
	Username      string `json:"username,omitempty"`
	Name          string `json:"name,omitempty"`
	CodeChallenge string `json:"code_challenge"`
}

// PasskeyStarted answers a start: open URL in the person's browser.
type PasskeyStarted struct {
	Flow      string    `json:"flow"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// PasskeyPoll is POST /v1/auth/passkey/poll and /v1/auth/passkey/cancel.
// Verifier is base64url (no padding) of the 32 bytes whose SHA-256 started
// the flow.
type PasskeyPoll struct {
	Flow     string `json:"flow"`
	Verifier string `json:"verifier"`
}

// Passkey flow states.
const (
	PasskeyPending = "pending"
	PasskeyDone    = "done"
	PasskeyExpired = "expired"
	PasskeyRefused = "refused"
)

// PasskeyPolled answers a poll. A done sign-in, join or reset carries the
// session exactly once; a done add carries none (the caller is signed in).
type PasskeyPolled struct {
	State       string          `json:"state"`
	Message     string          `json:"message,omitempty"`
	Purpose     string          `json:"purpose,omitempty"`
	Factory     *Factory        `json:"factory,omitempty"`
	AccessToken string          `json:"access_token,omitempty"`
	TokenType   string          `json:"token_type,omitempty"`
	ExpiresIn   int             `json:"expires_in,omitempty"`
	ExpiresAt   *time.Time      `json:"expires_at,omitempty"`
	Person      *PasswordPerson `json:"person,omitempty"`
	Me          *Me             `json:"me,omitempty"`
}

// PasskeyRow is one of the signed-in person's passkeys. ID is the WebAuthn
// credential id, base64url; it is public.
type PasskeyRow struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	Synced     bool       `json:"synced"`
}

// PasskeyList answers GET /v1/me/passkeys. Password says whether the person
// also has a password, which decides whether their last passkey may go.
type PasskeyList struct {
	Passkeys []PasskeyRow `json:"passkeys"`
	Password bool         `json:"password"`
}

// PasskeyRemove is POST /v1/me/passkeys/remove.
type PasskeyRemove struct {
	ID string `json:"id"`
}
