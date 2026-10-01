package provider

import (
	"net/http"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
)

// ── APP ATTRIBUTION ─────────────────────────────────────────────────────────
//
// Three headers say who is calling. OpenRouter reads them to attribute traffic
// to an app rather than to an anonymous key: HTTP-Referer is the app's identity
// and the only one that creates an app page at all, X-OpenRouter-Title is the
// display name shown against it, and X-OpenRouter-Categories files the app
// under the marketplace categories its rankings are computed within.
//
// THE BINARY'S OWN RELEASE STAMP DECIDES THE APP, AND NOTHING ELSE CAN. A stable
// or release-candidate build reports as AgentField AI, exactly as every build
// did before; a staging build reports as its own app; a dev build, and any
// build that carries no release tag at all — `make build` from a checkout, a
// test binary — reports as a third. The team runs the dev and staging channels
// and builds from source all day, and that usage landing on the AgentField AI
// page made the release's own numbers unreadable. Splitting by stamp keeps the
// release's page about the release.
//
// The values were once resolved from settings and from a handful of environment
// variables, and that was the whole of the drift: a caller that forgot to copy
// three fields through its own config sent nothing, and an environment that
// named a different site split one product's usage across two app pages. So
// the environment still cannot move a binary from one app to another, and the
// identities are spelled here once and read from here everywhere.
//
// EACH IDENTITY IS ITS OWN ORIGIN. OpenRouter groups referers by origin, so a
// path under agentfield.ai (`https://agentfield.ai/codeaf`) is the AgentField AI
// app again, which its own app lookup confirms; a subdomain is a separate app.
//
// X-Title is sent alongside X-OpenRouter-Title for backwards compatibility. It
// is the older spelling, some proxies in front of the router still read only
// that one, and a duplicate header costs nothing on a router that reads the new
// one.

const (
	// DirectUserAgent is the identity codeaf gives a service it reaches itself.
	// The historical name distinguishes it from pretending to be a vendor's
	// supported client; the same product name also belongs on routed requests.
	DirectUserAgent = "codeaf"

	// AppURL is the HTTP-Referer OpenRouter groups a RELEASE binary's usage
	// under — stable and release candidates — and it is the app's identity: a
	// request without it is attributed to nobody, whatever else it carries.
	AppURL = "https://agentfield.ai"

	// AppName is the display title OpenRouter shows for the release's app. It
	// also RENAMES the app page when it changes, so it is not a string to vary
	// per caller or per rig.
	AppName = "AgentField AI"

	// AppCategories are the marketplace categories the app is filed under, in
	// the lowercase hyphenated spellings OpenRouter's published category list
	// recognizes ("cli-agent" under Coding, "programming-app" under Coding).
	// An unrecognized word is dropped by the router without an error, so these
	// are copied from that list rather than invented. Every identity carries
	// them: a channel build is the same kind of program as the release.
	AppCategories = "cli-agent,programming-app"

	// StagingAppURL and StagingAppName are the app a staging build reports as.
	StagingAppURL  = "https://staging.codeaf.agentfield.ai"
	StagingAppName = "codeaf staging"

	// DevAppURL and DevAppName are the app a dev build reports as, and so does
	// every build the release workflow did not cut.
	DevAppURL  = "https://dev.codeaf.agentfield.ai"
	DevAppName = "codeaf dev"
)

// App is one OpenRouter app identity: the values the attribution headers carry.
type App struct {
	URL        string
	Name       string
	Categories string
}

// AppFor names the app a binary stamped with revision reports as. A stable or
// release-candidate tag is the release's app and nothing else is; a staging
// tag is the staging app; everything else, recognized or not, is the dev app,
// so a build nobody tagged can never be counted as the release.
func AppFor(revision string) App {
	switch buildinfo.Channel(revision) {
	case "stable", "rc":
		return App{URL: AppURL, Name: AppName, Categories: AppCategories}
	case "staging":
		return App{URL: StagingAppURL, Name: StagingAppName, Categories: AppCategories}
	default:
		return App{URL: DevAppURL, Name: DevAppName, Categories: AppCategories}
	}
}

// runningApp is decided once, from the stamp this process was built with,
// because the stamp cannot change while it runs.
var runningApp = AppFor(buildinfo.Revision())

// RunningApp is the app this binary reports as.
func RunningApp() App { return runningApp }

// Apply writes the whole attribution set onto a request's headers.
func (app App) Apply(header http.Header) {
	if header == nil {
		return
	}
	header.Set("HTTP-Referer", app.URL)
	header.Set("X-OpenRouter-Title", app.Name)
	header.Set("X-Title", app.Name)
	header.Set("X-OpenRouter-Categories", app.Categories)
}

// ApplyAttribution writes this binary's attribution set onto a request's
// headers.
//
// IT IS EXPORTED BECAUSE THIS PACKAGE HAS NOT ALWAYS BEEN THE ONLY ONE THAT
// POSTS TO THE ROUTER. v1's microphone client reached /audio/transcriptions on
// its own — that package is gone now, and transcribe.go is the one transcription
// transport — but it spent
// a release writing out its own half of this set by hand — a referer and the
// old title spelling, with no X-OpenRouter-Title and no categories at all — so
// every word a person spoke to v1 was attributed as an unclassified app while
// every word they typed was attributed correctly.
//
// So: a package that talks to OpenRouter calls this. It does not write header
// names and it does not carry the values.
func ApplyAttribution(header http.Header) {
	RunningApp().Apply(header)
}
