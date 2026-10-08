package github

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/env"
)

// LookPath finds the gh program. It is a variable so a test can say gh is
// absent without touching the machine's PATH.
var LookPath = exec.LookPath

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
