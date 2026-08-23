// Package attribution centralizes the SWE AF / AgentField branding the
// harness stamps onto artifacts it produces: OpenRouter app-attribution
// headers (https://openrouter.ai/docs/app-attribution) and git commit
// co-author trailers.
//
// This is a deliberate Go-side addition with no counterpart in swe-pro
// 3b25a1a. The commit-attribution half still reads its environment; the
// OpenRouter half no longer reads anything, because the app a request names is
// a fact about the product rather than an operator's preference.
package attribution

import (
	"net/http"
	"os"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

const (
	// DefaultSiteURL, DefaultAppName and DefaultCategories are the OpenRouter
	// app this engine reports as. They are INTERPOLATED FROM internal/provider
	// rather than re-spelled here: the engine is a second process spending the
	// same key against the same app, and two copies of an app's identity is how
	// one product's usage ends up split across two dashboard pages. The names
	// keep the "Default" prefix the rest of this package uses, but nothing
	// overrides them — see [OpenRouterHeaderPairs].
	DefaultSiteURL    = provider.AppURL
	DefaultAppName    = provider.AppName
	DefaultCategories = provider.AppCategories

	// CommitCoAuthorTrailer is the git trailer identifying SWE AF as a
	// co-author of harness-produced commits.
	CommitCoAuthorTrailer = "Co-Authored-By: SWE AF <noreply@agentfield.ai>"

	commitGeneratedLine = "🤖 Generated with SWE AF (https://agentfield.ai)"

	// DefaultCommitterName / DefaultCommitterEmail are the identity every
	// commit the harness authors is attributed to when the environment says
	// nothing. They match the SWE-AF node's own default
	// (swe_af/app.py, go/internal/orch/resolve.go) so a build attributes
	// consistently whether the classic loop or this engine produced it.
	DefaultCommitterName  = "SWE-AF"
	DefaultCommitterEmail = "swe-af@users.noreply.github.com"

	// EnvCommitterName / EnvCommitterEmail override the identity above. The
	// names are the SWE-AF node's existing surface, so one pair of variables
	// configures the whole stack.
	EnvCommitterName  = "SWE_AF_GIT_NAME"
	EnvCommitterEmail = "SWE_AF_GIT_EMAIL"
)

// falseValues matches the SDK: anything else (including unset) is enabled.
var falseValues = map[string]struct{}{
	"0": {}, "false": {}, "no": {}, "off": {},
}

func enabled(name string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	_, disabled := falseValues[value]
	return !disabled
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// ── OpenRouter app attribution ────────────────────────────────────────────

// OpenRouterHeaderPairs returns the ordered attribution {name, value} pairs to
// include on OpenRouter requests.
//
// NOTHING CAN CHANGE THEM AND NOTHING CAN SWITCH THEM OFF. The engine used to
// resolve all three from the environment — AGENTFIELD_OPENROUTER_SITE_URL,
// OR_APP_NAME, a kill switch — and an engine that runs as a second process
// inheriting somebody's shell is exactly where an app quietly becomes two apps,
// or none. The app this binary reports as is a fact about the product.
//
// X-OpenRouter-Categories is the fourth pair and it was missing for a release:
// the engine named the app and its URL, so its tokens landed on the app page,
// but it named no category — and categories are what the marketplace boards are
// ranked within. Every other outbound path in this binary sends the whole set
// (provider.ApplyAttribution), and this one now does too.
func OpenRouterHeaderPairs() [][2]string {
	return [][2]string{
		{"HTTP-Referer", DefaultSiteURL},
		{"X-OpenRouter-Title", DefaultAppName},
		{"X-Title", DefaultAppName},
		{"X-OpenRouter-Categories", DefaultCategories},
	}
}

// ApplyOpenRouterHeaders adds the attribution headers to an http.Header,
// leaving any header the caller already set untouched.
func ApplyOpenRouterHeaders(header http.Header) {
	for _, pair := range OpenRouterHeaderPairs() {
		if header.Get(pair[0]) == "" {
			header.Set(pair[0], pair[1])
		}
	}
}

// ── git commit attribution ────────────────────────────────────────────────

// CommitAttributionEnabled reports whether commit messages get the SWE AF
// trailer. Disable with AGENTFIELD_COMMIT_ATTRIBUTION=0|false|no|off.
func CommitAttributionEnabled() bool {
	return enabled("AGENTFIELD_COMMIT_ATTRIBUTION")
}

// CommitTrailer is the footer appended to harness-produced commit messages.
func CommitTrailer() string {
	return commitGeneratedLine + "\n\n" + CommitCoAuthorTrailer
}

// AppendCommitTrailer appends the SWE AF footer to a commit message. It is
// idempotent and a no-op when commit attribution is disabled.
func AppendCommitTrailer(message string) string {
	if !CommitAttributionEnabled() || strings.Contains(message, "Co-Authored-By: SWE AF") {
		return message
	}
	return strings.TrimRight(message, "\n") + "\n\n" + CommitTrailer()
}

// AppendCommitTrailerToArgv rewrites a git argv so the message argument of a
// `git commit` or `git merge` carries the SWE AF footer. Both the detached
// (`-m <msg>`, `--message <msg>`) and attached (`--message=<msg>`) spellings
// are handled; other argv shapes come back unchanged.
func AppendCommitTrailerToArgv(argv []string) []string {
	if !CommitAttributionEnabled() || len(argv) < 2 || argv[0] != "git" {
		return argv
	}
	subcommand := argv[1]
	if subcommand != "commit" && subcommand != "merge" {
		return argv
	}
	out := append([]string(nil), argv...)
	for i := 2; i < len(out); i++ {
		switch {
		case (out[i] == "-m" || out[i] == "--message") && i+1 < len(out):
			out[i+1] = AppendCommitTrailer(out[i+1])
			i++
		case strings.HasPrefix(out[i], "--message="):
			out[i] = "--message=" + AppendCommitTrailer(strings.TrimPrefix(out[i], "--message="))
		}
	}
	return out
}

// ── git committer identity ────────────────────────────────────────────────

// CommitterName resolves the git author/committer name for commits the
// harness makes, from EnvCommitterName or DefaultCommitterName. Blank and
// whitespace-only values count as unset.
func CommitterName() string {
	return firstNonEmpty(os.Getenv(EnvCommitterName), DefaultCommitterName)
}

// CommitterEmail resolves the git author/committer email, from
// EnvCommitterEmail or DefaultCommitterEmail.
func CommitterEmail() string {
	return firstNonEmpty(os.Getenv(EnvCommitterEmail), DefaultCommitterEmail)
}

// CommitterFlags returns the `-c user.name=… -c user.email=…` argv fragment
// that pins the identity of a git invocation. It is resolved per call, not
// at init, so a process whose environment changes picks the new value up.
//
// These are `-c` config overrides, which git ranks *below* the standard
// GIT_AUTHOR_* / GIT_COMMITTER_* environment variables. A deployment that
// sets those (the SWE-AF container images do) therefore still wins, exactly
// as it did before this identity became configurable.
func CommitterFlags() []string {
	return []string{
		"-c", "user.name=" + CommitterName(),
		"-c", "user.email=" + CommitterEmail(),
	}
}

// GitArgv builds a full git argv with the committer identity pinned:
// `git -c user.name=… -c user.email=… <args…>`.
func GitArgv(args ...string) []string {
	argv := append([]string{"git"}, CommitterFlags()...)
	return append(argv, args...)
}

// CommitPromptInstruction is the system-prompt rule telling an LLM agent to
// attribute the commits it writes. Empty when commit attribution is disabled.
func CommitPromptInstruction() string {
	if !CommitAttributionEnabled() {
		return ""
	}
	return "Every git commit message you write must end with this exact footer, " +
		"separated from the body by a blank line:\n\n" + CommitTrailer()
}
