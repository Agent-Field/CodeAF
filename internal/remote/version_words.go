package remote

import (
	"fmt"
	"github.com/Agent-Field/codeaf/internal/update"
	"os"
	"strings"
)

// Version mismatches leave the engine and its windows alone. The higher wire
// identifies the end that needs no update; never claim that every host is old.
func wireMismatchSentence(machine string, far, near int, build string) string {
	binary, _ := os.Executable()
	name := update.InstallName(binary)
	if far > near {
		return fmt.Sprintf("%s is on a newer build (%s); %s update to match — this connection needs matching versions", machine, build, name)
	}
	return fmt.Sprintf("%s is on an older build (%s); run %s update on %s, then finish work and close every window for this workspace before reopening — this connection needs matching versions", machine, build, name, machine)
}

// Only rewrite our version refusal. Arbitrary remote errors remain verbatim.
// The engine may have a different executable name after fallback or handoff;
// the update command displayed by this terminal belongs to this installation.
func callingProgramMismatch(reason string) string {
	const suffix = "this connection needs matching versions"
	if !strings.HasSuffix(reason, suffix) {
		return reason
	}
	start := strings.LastIndex(reason, "; ")
	if start < 0 {
		return reason
	}
	end := strings.Index(reason[start+1:], " update")
	if end < 0 {
		return reason
	}
	binary, _ := os.Executable()
	return reason[:start+2] + update.InstallName(binary) + reason[start+1+end:]
}
