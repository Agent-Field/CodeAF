package provider

import (
	"net/http"
	"strings"
)

// ── APP ATTRIBUTION ─────────────────────────────────────────────────────────
//
// Three headers say who is calling. OpenRouter reads them to attribute traffic
// to an app rather than to an anonymous key, and the third of them is the whole
// reason this package owns its own transport: the SDK sets a referer and a
// title and has no field for CATEGORIES at all, so every request that went out
// through it was attributed as an unclassified app.
//
// The set lives here, once, rather than at each call site. It was written out
// twice before — in the chat client's request builder and in the media
// client's — and the categories header only ever reached one of them; a helper
// is what makes "every aforge-owned request carries all of it" a fact about the
// code instead of a thing somebody has to remember at each new endpoint.
//
// X-Title is sent alongside X-OpenRouter-Title for backwards compatibility. It
// is the older spelling, some proxies in front of the router still read only
// that one, and a duplicate header costs nothing on a router that reads the new
// one.
//
// Empty values send nothing. An operator who cleared a field asked not to be
// attributed by it, and an empty header is a claim about the app rather than
// the absence of one.
func applyAttribution(header http.Header, config Config) {
	if header == nil {
		return
	}
	if site := strings.TrimSpace(config.SiteURL); site != "" {
		header.Set("HTTP-Referer", site)
	}
	if name := strings.TrimSpace(config.SiteName); name != "" {
		header.Set("X-OpenRouter-Title", name)
		header.Set("X-Title", name)
	}
	if categories := strings.TrimSpace(config.SiteCategories); categories != "" {
		header.Set("X-OpenRouter-Categories", categories)
	}
}
