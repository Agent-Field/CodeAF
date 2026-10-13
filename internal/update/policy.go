package update

import "strings"

// ── WHO IS UPDATED WITHOUT BEING ASKED ──────────────────────────────────────
//
// The automatic road replaces the file a person runs and restarts them onto the
// new one, so it is for exactly one kind of launch: somebody running a released
// codeaf the way the install line put it there. Everybody who works ON codeaf
// runs something else, and the build already says which:
//
//   - `make build`, `go build`, `go run` and `go install` stamp a commit, not a
//     release tag (internal/buildinfo), so their channel is "other" and the
//     launch check never even makes its request.
//   - `devaf` and `stageaf` are dev and staging builds under their own file
//     names, and a dev build installed as `codeaf` still carries a `dev-*` tag.
//   - A release candidate is somebody trying the next stable early.
//
// So the rule reads the two tags and the file name and NOTHING ELSE — no guess
// about whose machine this is, no repository sniffing, no environment flag a
// developer has to remember to export. Every launch it refuses still hears that
// a release is out; it just is not installed until somebody types `/update` or
// `codeaf update`.

// autoInstallName is the one file name the automatic road replaces.
const autoInstallName = "codeaf"

// AutomaticFor reports whether a launch of the build `running`, from the file
// `executable`, may install `latest` on its own. ALL THREE must hold: this
// build is a stable release, `latest` is a newer stable release, and the file
// is installed as `codeaf`. Dev, staging and release-candidate builds, source
// builds, and any file under another name (`devaf`, `stageaf`, a `--name`
// install) are told and never replaced unasked.
//
// It is the build's half of the decision. The person's half — the `update.auto`
// row, a pinned version, a release they skipped — is asked separately by each
// launch road, because only the road knows whether anybody is there to ask.
func AutomaticFor(running, latest, executable string) bool {
	running, latest = strings.TrimSpace(running), strings.TrimSpace(latest)
	if Kind(running) != "stable" || Kind(latest) != "stable" {
		return false
	}
	if strings.TrimSpace(executable) == "" || InstallName(executable) != autoInstallName {
		return false
	}
	return Available{Latest: latest, Running: running}.Newer()
}
