//go:build !windows

// Package netpolicy makes senior-dev aware of runs where agent-initiated network
// access is unavailable, so agents stop wasting cycles attempting it. It
// governs the builtin web tools (webfetch, websearch) and the environment
// handed to bash children. The model plane (the model API codeaf serves the
// run) is deliberately outside its scope: that traffic is senior-dev's own
// road to a model, not agent-initiated, and a run cannot function without it.
//
// The policy is read from the environment, following the pipeline's existing
// SENIOR_DEV_* precedent:
//
//	SENIOR_DEV_NET=allow  current behavior (default when unset)
//	SENIOR_DEV_NET=off    agent-initiated egress is unavailable for this run
//
// A value of SENIOR_DEV_NET that parses to neither fails CLOSED to off: a typo in
// a flag that exists to forbid network access must not silently grant it. The
// parse problem is preserved on the Policy so callers can surface it; a run
// refuses to start on it, so a run is never silently degraded by a typo either.
//
// Containment is not this package's job - that belongs to the environment the
// run executes in (for example a sandbox that only lets the model API
// through). What this package delivers under off is legibility
// and economy:
// the web tools disappear from the model's tool list, in-process HTTP fails
// instantly with an explicit no-retry policy error instead of a sandbox
// timeout, and proxy-honoring bash clients (curl, wget, pip, npm,
// git-over-HTTPS) get a millisecond 403 from a local black-hole listener
// rather than a DNS or connect stall. Clients that ignore proxy variables
// simply fail against the outer sandbox instead - slower, but still contained.
package netpolicy

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
)

// Mode is the egress posture.
type Mode string

const (
	// ModeAllow leaves every egress path open.
	ModeAllow Mode = "allow"
	// ModeOff declares agent-initiated egress unavailable.
	ModeOff Mode = "off"
)

// EnvMode is the environment variable the policy is read from.
const EnvMode = "SENIOR_DEV_NET"

// Policy is an immutable snapshot of the egress policy.
type Policy struct {
	Mode Mode
	// Warning is non-empty when the environment held an unrecognized value
	// and the policy failed closed because of it.
	Warning string
}

// Current reads the policy from the process environment.
func Current() Policy {
	return FromLookup(os.Getenv)
}

// FromLookup parses a policy from an environment accessor, for tests and
// embedders that do not own the process environment.
func FromLookup(getenv func(string) string) Policy {
	raw := strings.TrimSpace(strings.ToLower(getenv(EnvMode)))
	switch raw {
	case "", string(ModeAllow):
		return Policy{Mode: ModeAllow}
	case string(ModeOff):
		return Policy{Mode: ModeOff}
	default:
		return Policy{
			Mode: ModeOff,
			Warning: fmt.Sprintf(
				"%s=%q is not one of allow/off",
				EnvMode, getenv(EnvMode),
			),
		}
	}
}

// Restricted reports whether the policy restricts egress at all. Callers on
// hot paths use it to skip wrapping entirely under the default policy.
func (p Policy) Restricted() bool {
	return p.Mode == ModeOff
}

func hostOnly(host string) string {
	if trimmed, _, err := net.SplitHostPort(host); err == nil {
		return trimmed
	}
	// A bare IPv6 literal without a port fails SplitHostPort; unwrap brackets.
	return strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
}

// BlockedError is the model-facing refusal for one blocked host. It is a
// distinct type so callers can recover it with errors.As after net/http wraps
// it (a refusal on a redirect hop comes back from Client.Do inside a
// *url.Error) and surface the policy text instead of a generic transport
// failure.
type BlockedError struct {
	Host    string
	message string
}

func (err *BlockedError) Error() string { return err.message }

// HostError builds the refusal for one blocked host. The [network-policy]
// prefix and the no-retry framing follow the [environment-signal] convention
// in internal/tool/shell_env_signal.go: the point is to stop an agent from
// burning turns retrying a request that policy, not transient failure,
// rejected.
func (p Policy) HostError(host string) error {
	name := hostOnly(host)
	return &BlockedError{Host: name, message: fmt.Sprintf(
		"[network-policy] request to %q blocked: this run has network access disabled (%s=off). "+
			"This is policy, not a transient failure - do not retry and do not attempt "+
			"the same access through bash or other tools; "+
			"work from local repository content instead",
		name, EnvMode,
	)}
}

// EnvironmentNotice is the per-turn system-prompt paragraph that tells agents
// up front that the network is unavailable, so the first fetch attempt never
// happens instead of merely failing fast. Empty under the default policy.
func (p Policy) EnvironmentNotice() string {
	if !p.Restricted() {
		return ""
	}
	return "Network access is disabled for this run: external fetches, package installs, " +
		"and any other network commands will fail. Do not attempt them or retry them; " +
		"work only from content already available in the repository and this environment."
}

// Transport wraps base so every request is refused before it dials while the
// policy is restricted. Redirect hops re-enter the transport, so each hop is
// covered. A nil base means http.DefaultTransport, mirroring net/http.
func (p Policy) Transport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	if !p.Restricted() {
		return base
	}
	return policyTransport{policy: p, base: base}
}

type policyTransport struct {
	policy Policy
	base   http.RoundTripper
}

func (t policyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	// The RoundTripper contract makes the transport responsible for closing
	// the body once it has been handed the request.
	if request.Body != nil {
		_ = request.Body.Close()
	}
	return nil, t.policy.HostError(request.URL.Host)
}
