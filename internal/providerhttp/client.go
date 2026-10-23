// Package providerhttp confines provider credentials to the request's origin.
package providerhttp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
)

// SameOrigin includes scheme and effective port, not just DNS ancestry.
func SameOrigin(a, b *url.URL) bool {
	if a == nil || b == nil || a.Hostname() == "" || b.Hostname() == "" {
		return false
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && port(a) == port(b)
}
func port(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	}
	return ""
}

// Clone preserves a caller's transport, timeout and cookies without mutating it.
// A caller may impose stricter redirect rules, never loosen this boundary.
func Clone(base *http.Client) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	clone := *base
	prior := base.CheckRedirect
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := CheckRedirect(req, via); err != nil {
			return err
		}
		// Preserve the original origin even if a caller's hook edits its
		// request arguments. Hooks can restrict redirects, never change trust.
		origin := *via[0].URL
		if prior != nil {
			if err := prior(req, via); err != nil {
				return err
			}
		}
		guarded := append([]*http.Request(nil), via...)
		first := *via[0]
		first.URL = &origin
		guarded[0] = &first
		return CheckRedirect(req, guarded)
	}
	return &clone
}

// CheckRedirect permits at most ten hops, each on the original origin.
func CheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return errors.New("provider redirect has no original request")
	}
	if !SameOrigin(via[0].URL, req.URL) || req.URL.User != nil {
		return &boundaryError{fmt.Sprintf("provider redirect to %s refused; check the configured service address", Host(req.URL))}
	}
	if len(via) >= 10 {
		return &boundaryError{fmt.Sprintf("provider redirect limit reached at %s; check the configured service address", Host(req.URL))}
	}
	return nil
}

// Host deliberately excludes userinfo, paths, fragments and query values.
func Host(u *url.URL) string {
	if u == nil || u.Host == "" {
		return "unknown host"
	}
	return u.Host
}

// Do keeps net/http's raw Location URL out of errors returned to the UI or logs.
// The original cause remains available for errors.Is/As, behind a safe Error().
func Do(client *http.Client, req *http.Request) (*http.Response, error) {
	resp, err := Clone(client).Do(req)
	if err != nil {
		return resp, &safeError{cause: err, host: Host(req.URL)}
	}
	return resp, nil
}

type safeError struct {
	cause error
	host  string
}

func (e *safeError) Error() string {
	var ue *url.Error
	if errors.As(e.cause, &ue) {
		return fmt.Sprintf("provider request at %s failed: %s", e.host, safeCause(ue.Err))
	}
	return fmt.Sprintf("provider request at %s failed: %s", e.host, safeCause(e.cause))
}
func (e *safeError) Unwrap() error { return e.cause }
func safeCause(err error) string {
	// Only our own redirect diagnostics are allowed to carry human-readable text;
	// transport and caller errors may contain complete credential-bearing URLs.
	var boundary *boundaryError
	if errors.As(err, &boundary) {
		return boundary.Error()
	}
	if errors.Is(err, context.Canceled) {
		return "request canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "context deadline exceeded"
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return "request timed out"
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "DNS lookup failed; check the network and service address"
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return "connection failed; check the network and service address"
	}
	if errors.Is(err, syscall.ECONNRESET) {
		return "connection reset"
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "connection refused"
	}
	if errors.Is(err, http.ErrUseLastResponse) {
		return "redirect refused"
	}
	return "request failed"
}

type boundaryError struct{ message string }

func (e *boundaryError) Error() string { return e.message }

// A refused origin cannot be repaired by retrying the same provider request.
func (*boundaryError) TerminalTransportFailure() bool { return true }
