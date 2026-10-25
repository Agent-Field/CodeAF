package remote

import (
	"fmt"
	"github.com/Agent-Field/codeaf/internal/update"
	"os"
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
