package approval

import (
	"path/filepath"
	"strings"
)

// accountCLIs are command-line tools that run signed in to one of the person's
// own accounts. Whatever they do, read or write, they do as the person, on a
// service outside this machine, so a bash line that starts one asks first
// unless the person saved a rule for it. Public data has an anonymous road
// (fetch), which is the one an agent should take without asking.
var accountCLIs = map[string]bool{
	"gh": true, "glab": true, "hub": true,
	"aws": true, "gcloud": true, "gsutil": true, "bq": true, "az": true, "doctl": true,
	"vercel": true, "netlify": true, "railway": true, "fly": true, "flyctl": true, "heroku": true,
	"stripe": true, "firebase": true, "supabase": true, "wrangler": true,
	"slack": true, "op": true,
}

// accountCLI returns the first account CLI any segment of the line starts.
func accountCLI(segments []string) (string, bool) {
	for _, segment := range segments {
		if name := leadingProgram(segment); accountCLIs[name] {
			return name, true
		}
	}
	return "", false
}

// leadingProgram is the program a segment runs: past sudo, environment
// assignments and the `command`/`exec` builtins, without its directory.
func leadingProgram(segment string) string {
	for _, word := range strings.Fields(normalizeCritical(segment)) {
		if strings.Contains(word, "=") || word == "command" || word == "exec" {
			continue
		}
		return filepath.Base(word)
	}
	return ""
}
