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

// Attribution is who is calling, in the three values OpenRouter reads.
//
// IT IS EXPORTED BECAUSE THIS PACKAGE IS NOT THE ONLY ONE THAT POSTS TO THE
// ROUTER. internal/voice reaches /audio/transcriptions on its own, and it spent
// a release writing out its own half of this set by hand — a referer and the
// old title spelling, with no X-OpenRouter-Title and no categories at all — so
// every word a person spoke to v1 was attributed as an unclassified app while
// every word they typed was attributed correctly. That is precisely the drift
// the paragraph above says a helper exists to prevent, and it happened anyway
// because the helper could not be reached from outside this package.
//
// So: a package that talks to OpenRouter takes one of these and calls Apply. It
// does not write header names.
type Attribution struct {
	SiteURL        string
	SiteName       string
	SiteCategories string
}

// Apply writes the attribution onto a request's headers.
func (a Attribution) Apply(header http.Header) {
	if header == nil {
		return
	}
	if site := strings.TrimSpace(a.SiteURL); site != "" {
		header.Set("HTTP-Referer", site)
	}
	if name := strings.TrimSpace(a.SiteName); name != "" {
		header.Set("X-OpenRouter-Title", name)
		header.Set("X-Title", name)
	}
	if categories := strings.TrimSpace(a.SiteCategories); categories != "" {
		header.Set("X-OpenRouter-Categories", categories)
	}
}

func applyAttribution(header http.Header, config Config) {
	Attribution{
		SiteURL:        config.SiteURL,
		SiteName:       config.SiteName,
		SiteCategories: config.SiteCategories,
	}.Apply(header)
}
