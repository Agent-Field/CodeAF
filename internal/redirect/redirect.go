// Package redirect is the one redirect rule shared by every client that
// carries a configured provider key. It keeps the key on the origin it was
// given for and nowhere else.
package redirect

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// RefusedError is the error [Policy] returns when a redirect would leave the
// configured origin. It is typed so a caller that must not retry — the
// provider dispatcher, whose loop would otherwise treat the refusal as a
// generic transport fault and re-ask the same origin until the deadline — can
// recognise it and stop at once: the same origin answers the same redirect on
// every attempt.
type RefusedError struct {
	Target string
}

func (e *RefusedError) Error() string {
	return fmt.Sprintf("redirect to %q refused: a provider request may only follow same-origin redirects", e.Target)
}

// Policy is the shared redirect rule. Go's default policy forwards sensitive
// headers when a redirect stays on the same hostname — including a different
// port — or moves to a subdomain of it, and the configured key belongs to the
// exact origin it was given for, so that default is too broad. This policy
// follows a redirect only when the target keeps the ORIGINAL request's
// (via[0]) scheme, hostname and effective port; any other redirect is refused
// with an error naming the target host, and the key is never sent anywhere
// off-origin — nor is it stripped and sent onward, because a silently stripped
// header would surface as a confusing 401 at a host the person never
// configured. Same-origin redirects keep working.
//
// Like the stdlib default, the policy stops after 10 requests so a same-origin
// loop terminates.
func Policy(request *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if len(via) == 0 || sameOrigin(request.URL, via[0].URL) {
		return nil
	}
	return &RefusedError{Target: request.URL.Host}
}

// Guard wraps a caller-supplied client with [Policy], keeping the caller's
// transport, timeout and cookie jar and composing any CheckRedirect the caller
// already set, so a client handed in by a caller carries the product rule too.
// A nil base yields a fresh client carrying only the policy, so a caller may
// pass its possibly-nil client straight through.
func Guard(base *http.Client) *http.Client {
	if base == nil {
		return &http.Client{CheckRedirect: Policy}
	}
	client := *base
	checkRedirect := base.CheckRedirect
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if checkRedirect != nil {
			if err := checkRedirect(request, via); err != nil {
				return err
			}
		}
		return Policy(request, via)
	}
	return &client
}

// sameOrigin reports whether a redirect target keeps the original request's
// scheme, hostname and effective port.
func sameOrigin(target, original *url.URL) bool {
	if target == nil || original == nil {
		return false
	}
	if !strings.EqualFold(target.Scheme, original.Scheme) {
		return false
	}
	if !strings.EqualFold(target.Hostname(), original.Hostname()) {
		return false
	}
	return effectivePort(target) == effectivePort(original)
}

// effectivePort resolves a URL's port, defaulting to the scheme's well-known
// port so an explicit default port and a bare host are the same origin.
func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	return "80"
}
