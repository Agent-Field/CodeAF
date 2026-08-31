package furrowbin

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
)

// pinJSON is the committed pin, and it is THE one source of truth for which
// furrow this aforge is. The build reads it to know what to fetch and what
// sha256 to demand; the running binary reads the same bytes to know what to
// call the file it extracts. A version written down twice is a version that
// drifts, and the shape it drifts into is the worst one — a binary extracting
// itself to a path stamped with a release it is not.
//
//go:embed pin.json
var pinJSON []byte

// Pin is the whole of the pin file: which furrow, from where, and what each
// platform's artifact must hash to.
type Pin struct {
	// Repository is the GitHub owner/name the release hangs off, and Tag is
	// the release's tag. They are carried rather than hardcoded so that a
	// furrow that moves house is a one-line diff in pin.json.
	Repository string `json:"repository"`
	Tag        string `json:"tag"`

	// Version is furrow's own version without the tag's leading v, and it is
	// what stamps the extracted file's name. It is separate from Tag because
	// a tag is a git label and a version is what `furrow --version` prints;
	// they agree today and there is no reason to make one imply the other.
	Version string `json:"version"`

	// Artifacts maps "<goos>-<goarch>" to the release asset for it. A platform
	// with no entry is a platform this aforge cannot embed furrow for — which
	// the build says out loud rather than quietly skipping.
	Artifacts map[string]Artifact `json:"artifacts"`
}

// Artifact is one platform's release asset and the hash it must have.
type Artifact struct {
	// Asset is the file's name on the release page, which is also the last
	// element of its download URL.
	Asset string `json:"asset"`

	// SHA256 is the hex digest published in the release's own SHA256SUMS. IT
	// IS THE ONLY THING THAT MAKES THE FETCH TRUSTWORTHY: a build that takes
	// whatever the network hands it and embeds it in the product has moved the
	// supply chain from "a release somebody signed for" to "whatever answered".
	SHA256 string `json:"sha256"`
}

var (
	pinOnce   sync.Once
	pinParsed Pin
)

// ReadPin returns the compiled-in pin.
//
// A malformed pin panics rather than becoming an error every caller carries.
// The file is embedded at compile time and a test in this package parses every
// byte of it, so a pin that does not decode is a pin that could not have got
// past `make test` — it is not a runtime mode, and the same reasoning
// internal/packed states for its archives holds here.
func ReadPin() Pin {
	pinOnce.Do(func() {
		if err := json.Unmarshal(pinJSON, &pinParsed); err != nil {
			panic("furrowbin: read pin.json: " + err.Error())
		}
	})
	return pinParsed
}

// Version is the furrow this aforge is pinned to — "0.1.0" — and is what
// stamps the extracted binary's name.
func Version() string { return ReadPin().Version }

// Platform is the key an artifact is filed under, and the only place the
// spelling of that key is decided. It is a function rather than a fmt.Sprintf
// at four call sites because the build stages a file whose NAME is this string
// and the running binary looks that name up: the two have to agree exactly or
// the binary quietly carries a furrow it will never find.
func Platform(goos, goarch string) string { return goos + "-" + goarch }

// ArtifactFor returns the pinned asset for one platform.
func (p Pin) ArtifactFor(platform string) (Artifact, error) {
	artifact, ok := p.Artifacts[platform]
	if !ok {
		return Artifact{}, fmt.Errorf("furrow %s has no release artifact for %s", p.Version, platform)
	}
	return artifact, nil
}

// DownloadURL is where one platform's artifact lives. GitHub's release
// download path is stable and public, so no token and no gh CLI is needed —
// which matters because this runs inside `make build` on machines that have
// neither.
func (p Pin) DownloadURL(artifact Artifact) string {
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", p.Repository, p.Tag, artifact.Asset)
}
