package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
)

// ── THE CREDENTIAL A REQUEST HEADER WAS BUILT FROM ──────────────────────────
//
// A refusal of a key belongs to the key the refused request carried, and the
// one place that knows it for certain is the line that writes the request's
// Authorization header ([Client.newHTTPRequestAt]). The bearer is snapshotted
// there, once per attempt, under the adapter's own lock, and that snapshot is
// the value the service receives. Anything read before it — the session's
// account for the adapter, the profile — can be overtaken by a key handed to
// the live adapter between the two ([Client.SetAPIKey]), and a refusal blamed
// on the new key would hold a good key back for as long as the task runs. So
// the observation is taken from the snapshot itself, and an attempt the
// dispatcher makes again takes its own.
//
// NOTHING BUT A ONE-WAY DIGEST LEAVES THIS PACKAGE. The observer names a
// domain, the adapter hashes the bearer under it, and only the digest is
// reported; the bearer is never copied into the report, logged or kept.

// RequestCredential is what one request this adapter built was sent with.
type RequestCredential struct {
	// Digest is [CredentialDigest] of the observer's domain and the bearer the
	// request's Authorization header was written from, or "" when the request
	// carried no bearer or the observer named no domain.
	Digest string
	// Override is true when that bearer was the request's own
	// [ai.Request.APIKeyOverride] rather than the adapter's key.
	Override bool
}

type requestCredentialKey struct{}

// requestCredentialWatch is one observer, bound to the adapter it watches: a
// request another adapter builds under the same context carries another key
// and is not reported to it.
type requestCredentialWatch struct {
	client  *Client
	domain  string
	observe func(RequestCredential)
}

// ObserveRequestCredential asks c to tell observe, on the goroutine building
// it, what every request it builds under ctx carries — each attempt's own, in
// the order they are built, so the last one told is the last one sent.
func (c *Client) ObserveRequestCredential(ctx context.Context, domain string, observe func(RequestCredential)) context.Context {
	if c == nil || observe == nil {
		return ctx
	}
	return context.WithValue(ctx, requestCredentialKey{}, requestCredentialWatch{client: c, domain: domain, observe: observe})
}

// CredentialDigest is sixteen hex digits of a hash of secret under domain, ""
// for no secret or no domain. It is the print config's crew credentials are
// spelled in, so a digest taken here and a print taken from a connection with
// the same key and domain are the same string. The secret is hashed as given:
// both callers write the bearer already trimmed.
func CredentialDigest(domain, secret string) string {
	if domain == "" || secret == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(domain + "\x00" + secret))
	return hex.EncodeToString(sum[:8])
}

// noteRequestCredential reports bearer, the value just written into one
// request's header, to the observer watching c under ctx.
func (c *Client) noteRequestCredential(ctx context.Context, bearer string, override bool) {
	watch, ok := ctx.Value(requestCredentialKey{}).(requestCredentialWatch)
	if !ok || watch.client != c {
		return
	}
	watch.observe(RequestCredential{Digest: CredentialDigest(watch.domain, bearer), Override: override})
}
