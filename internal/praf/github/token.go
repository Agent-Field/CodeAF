package github

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/env"
)

// LookPath finds the gh program. It is a variable so a test can say gh is
// absent without touching the machine's PATH. The default tries the inherited
// PATH first, then the standard Homebrew install locations: an app launched from
// the Dock or Finder inherits a minimal PATH that omits them, so a gh that is
// installed would read as missing. No login shell is spawned and the process
// PATH is never changed.
var LookPath = lookGH

// ghFallbackDirs are where Homebrew puts gh, in the order tried. A variable so
// a test can stand a temporary folder in for them.
var ghFallbackDirs = platformGHDirs(runtime.GOOS)

func platformGHDirs(goos string) []string {
	switch goos {
	case "darwin":
		return []string{"/opt/homebrew/bin", "/usr/local/bin"}
	case "linux":
		return []string{"/home/linuxbrew/.linuxbrew/bin", "/usr/local/bin"}
	}
	return nil
}

// systemLookPath is the inherited-PATH lookup, a variable so a test can say it
// fails without emptying the real PATH.
var systemLookPath = exec.LookPath

func lookGH(name string) (string, error) {
	path, err := systemLookPath(name)
	if err == nil {
		return path, nil
	}
	if name != "gh" {
		return "", err
	}
	for _, dir := range ghFallbackDirs {
		candidate := filepath.Join(dir, name)
		if info, statErr := os.Stat(candidate); statErr == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", err
}

// Token is the GitHub token codeaf reads with: GH_TOKEN, then GITHUB_TOKEN,
// then what `gh auth token` answers; "" for none, which reads a public
// repository and writes nothing. The review pipeline and the factory's GitHub
// source both ask here, so the order is said once.
//
// THE TOKEN NEVER REACHES A LOG. It is handed to [NewClient] and nowhere else;
// a caller that wants to say a token was found says "a token", never the bytes
// (internal/redact exists because a secret on a screen is a secret in a
// transcript).
func Token(ctx context.Context) string { return TokenWith(ctx, LookPath) }

// TokenWith is [Token] with the gh lookup handed in, so a caller that already
// keeps its own seam for "is gh installed" (internal/praf's ghLookPath) keeps
// its tests exactly as they were.
func TokenWith(ctx context.Context, look func(string) (string, error)) string {
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if token := strings.TrimSpace(env.Value(name)); token != "" {
			return token
		}
	}
	if look == nil {
		return ""
	}
	gh, err := look("gh")
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, gh, "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
