//go:build e2e

package provider

import "time"

// ── THE THREE DOORS THE LIVE PROBE NEEDS ────────────────────────────────────
//
// e2e_ignore_real_test.go drives a real router and so must resolve a key the
// way a session does — internal/config's APIKeyAt — which package `provider`
// may not import, because config imports provider. So the probe is an EXTERNAL
// test package and reaches these three internals through here, which is the
// ordinary shape of an export_test file: it is compiled only under the `e2e`
// tag and only into the test binary, so nothing it names reaches a build.

// E2EStrikeLane refuses one endpoint for one model, exactly as a 429 naming its
// pool does ([velocityLedger.pace]).
func (c *Client) E2EStrikeLane(model, lane string, wait time.Duration) bool {
	return c.velocity.pace(model, lane, wait)
}

// E2EServedBy is the endpoint the last answer for a model came from, "" when
// nothing has answered yet.
func (c *Client) E2EServedBy(model string) string {
	served, ok := c.velocity.lastServed(model)
	if !ok {
		return ""
	}
	return served.Provider
}

// E2EVetoes is what the ledger would put in `provider.ignore` for the next
// request.
func (c *Client) E2EVetoes(model string) []string {
	_, ignore := c.velocity.preferences(model)
	return ignore
}

// E2ESaysTheSetWasEmptied reports whether a refusal body is the router saying an
// ignore list removed every endpoint ([ignoredEverything]).
func E2ESaysTheSetWasEmptied(body []byte) bool { return ignoredEverything(body) }
