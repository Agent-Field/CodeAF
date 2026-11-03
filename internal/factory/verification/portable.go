package verification

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/approval"
)

// portableCheckArgs removes installation locations only for recognized check
// tools. Raw receipt arguments remain available for audit; flags, directories
// and target context still distinguish measured requirements.
func portableCheckArgs(args []string) []string {
	if len(args) == 0 || !filepath.IsAbs(args[0]) || !checkWords(args) {
		return args
	}
	out := slices.Clone(args)
	out[0] = filepath.Base(out[0])
	return out
}

func missingCheckPath(args []string) string {
	if len(args) > 0 && filepath.IsAbs(args[0]) {
		if _, err := os.Stat(args[0]); os.IsNotExist(err) {
			return " · missing on this machine: " + args[0]
		}
	}
	return ""
}

// OwedCommand presents a portable literal check without changing execution or
// granting evidence to an unmeasured command. Shell programs outside this small
// grammar retain their text and identify missing executable paths explicitly.
func OwedCommand(command string) string {
	words := strings.Fields(command)
	if len(words) > 0 && !strings.ContainsAny(command, "\"'`$;&|<>\\()\n\r") {
		args := portableCheckArgs(words)
		if args[0] != words[0] {
			return strings.Join(args, " ")
		}
	}
	for _, part := range approval.SplitBashCommand(command) {
		args := strings.Fields(part.Command)
		for len(args) > 0 && assignment.MatchString(args[0]) {
			args = args[1:]
		}
		if why := missingCheckPath(args); why != "" {
			return command + why
		}
	}
	return command
}
