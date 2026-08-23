package provider

import (
	"net/http"
)

// ── APP ATTRIBUTION ─────────────────────────────────────────────────────────
//
// Three headers say who is calling. OpenRouter reads them to attribute traffic
// to an app rather than to an anonymous key: HTTP-Referer is the app's identity
// and the only one that creates an app page at all, X-OpenRouter-Title is the
// display name shown against it, and X-OpenRouter-Categories files the app
// under the marketplace categories its rankings are computed within.
//
// THE VALUES ARE CONSTANTS AND NOTHING CAN OVERRIDE THEM. They were resolved
// from settings and from a handful of environment variables once, and that was
// the whole of the drift: a caller that forgot to copy three
// fields through its own config sent nothing, and an environment that named a
// different site split one product's usage across two app pages. The app this
// binary reports as is a fact about the product, not a preference, so it is
// spelled here once and read from here everywhere.
//
// X-Title is sent alongside X-OpenRouter-Title for backwards compatibility. It
// is the older spelling, some proxies in front of the router still read only
// that one, and a duplicate header costs nothing on a router that reads the new
// one.

const (
	// AppURL is the HTTP-Referer OpenRouter groups this binary's usage under,
	// and it is the app's identity: a request without it is attributed to
	// nobody, whatever else it carries.
	AppURL = "https://agentfield.ai"

	// AppName is the display title OpenRouter shows for the app. It also
	// RENAMES the app page when it changes, so it is not a string to vary per
	// caller or per rig.
	AppName = "AgentField AI"

	// AppCategories are the marketplace categories the app is filed under, in
	// the lowercase hyphenated spellings OpenRouter's published category list
	// recognizes ("cli-agent" under Coding, "programming-app" under Coding).
	// An unrecognized word is dropped by the router without an error, so these
	// are copied from that list rather than invented.
	AppCategories = "cli-agent,programming-app"
)

// ApplyAttribution writes the whole attribution set onto a request's headers.
//
// IT IS EXPORTED BECAUSE THIS PACKAGE IS NOT THE ONLY ONE THAT POSTS TO THE
// ROUTER. internal/voice reaches /audio/transcriptions on its own, and it spent
// a release writing out its own half of this set by hand — a referer and the
// old title spelling, with no X-OpenRouter-Title and no categories at all — so
// every word a person spoke to v1 was attributed as an unclassified app while
// every word they typed was attributed correctly.
//
// So: a package that talks to OpenRouter calls this. It does not write header
// names and it does not carry the values.
func ApplyAttribution(header http.Header) {
	if header == nil {
		return
	}
	header.Set("HTTP-Referer", AppURL)
	header.Set("X-OpenRouter-Title", AppName)
	header.Set("X-Title", AppName)
	header.Set("X-OpenRouter-Categories", AppCategories)
}
