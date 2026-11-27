package approval

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ProtectedDeletion checks the refusal floor when the caller has no policy to
// consult or needs to choose between this refusal and a credential refusal.
func ProtectedDeletion(tool string, args json.RawMessage) (Decision, bool) {
	if strings.TrimSpace(tool) == ToolBash {
		if command, ok := bashCommand(args); ok {
			return CheckProtectedDeletion(command)
		}
	}
	return Decision{}, false
}

// CheckProtectedDeletion applies only the refusal floor to a shell command.
// This lets another shell tool enforce it without inheriting bash's policy rows.
func CheckProtectedDeletion(command string) (Decision, bool) {
	for _, words := range commandWords(command, false) {
		words = unwrap(words, map[string]string{}, CredentialScope{})
		if len(words) == 0 {
			continue
		}
		switch commandBase(words[0]) {
		case "rm":
			recursive, noPreserveRoot, targets := removeArguments(words[1:])
			if noPreserveRoot || recursive && slices.ContainsFunc(targets, protectedDeletionTarget) {
				return protectedDeletionDecision(command), true
			}
		case "find":
			if findDeletesProtected(words[1:]) {
				return protectedDeletionDecision(command), true
			}
		}
	}
	return Decision{}, false
}

func findDeletesProtected(words []string) bool {
	if !slices.Contains(words, "-delete") {
		return false
	}
	// Find takes its options before the start points, then an expression. A
	// path operand in the expression is not a start point.
	started := false
	for i := 0; i < len(words); i++ {
		word := words[i]
		if strings.HasPrefix(word, "-") {
			if started {
				return false
			}
			switch word {
			case "-H", "-L", "-P", "--", "-O0", "-O1", "-O2", "-O3":
				continue
			case "-D":
				i++
				continue
			default:
				return false
			}
		}
		if word == "!" {
			return false
		}
		if protectedDeletionTarget(word) {
			return true
		}
		started = true
	}
	return false
}

func protectedDeletionTarget(target string) bool {
	switch target {
	case "~", "~/", "~/*", "$HOME", "$HOME/", "$HOME/*", "${HOME}", "${HOME}/", "${HOME}/*":
		return true
	}
	if !path.IsAbs(target) && !filepath.IsAbs(filepath.FromSlash(target)) {
		return false
	}
	clean := path.Clean(target)
	if clean == "/*" {
		return true
	}
	clean = strings.TrimSuffix(clean, "/*")
	switch clean {
	case "/", "/home", "/Users", "/root", "/etc", "/usr", "/var", "/bin", "/sbin", "/lib", "/lib64",
		"/opt", "/boot", "/System", "/Library", "/Applications":
		return true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	if clean == path.Clean(filepath.ToSlash(home)) {
		return true
	}
	realHome, err := filepath.EvalSymlinks(home)
	return err == nil && clean == path.Clean(filepath.ToSlash(realHome))
}

// protectedDeletionDecision ends the attempt and keeps long compound commands
// from filling the tool result with an echo of the call.
func protectedDeletionDecision(command string) Decision {
	const excerptLimit = 200
	command = strings.TrimSpace(command)
	if len(command) > excerptLimit {
		end := excerptLimit
		for end > 0 && !utf8.RuneStart(command[end]) {
			end--
		}
		command = command[:end] + "…"
	}
	message := "Refused: " + strconv.Quote(command) + ". codeaf never runs this command in any approval mode. There is no Approve option. " +
		"Do not try another route: a script, find -delete, or another spelling is treated the same way. " +
		"If this is really the work, stop and tell the person. They can run it themselves in their own terminal."
	return Decision{Action: ActionDeny, Rule: message, Never: true}
}
