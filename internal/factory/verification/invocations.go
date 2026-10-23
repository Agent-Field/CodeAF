package verification

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/approval"
)

// One vocabulary drives both exit observers and journal/reporting recognition.
// The final * admits named verification scripts, not arbitrary npm run commands.
var checkInvocations = [][]string{
	{"go", "test"}, {"go", "vet"}, {"pytest"}, {"pip-audit"}, {"govulncheck"}, {"trivy"},
	{"python", "-m", "pytest"}, {"python3", "-m", "pytest"},
	{"python", "-m", "pip_audit"}, {"python3", "-m", "pip_audit"},
	{"cargo", "test"}, {"cargo", "check"}, {"cargo", "clippy"}, {"golangci-lint", "run"},
}

func init() {
	for _, p := range []string{"npm", "pnpm", "yarn"} {
		for _, s := range []string{"test*", "audit", "lint*", "typecheck*", "check*", "build*"} {
			checkInvocations = append(checkInvocations, []string{p, s}, []string{p, "run", s})
		}
	}
}
func checkWords(words []string) bool {
	words = checkProgramWords(words)
	if len(words) == 0 {
		return false
	}
	for _, rule := range checkInvocations {
		if len(words) < len(rule) {
			continue
		}
		matches := true
		for i, want := range rule {
			got := words[i]
			if i == 0 {
				got = filepath.Base(got)
			}
			if strings.HasSuffix(want, "*") {
				matches = matches && strings.HasPrefix(got, strings.TrimSuffix(want, "*"))
			} else {
				matches = matches && got == want
			}
		}
		if matches {
			return true
		}
	}
	return false
}

// Package-manager selection flags precede the verb in ordinary workspace
// commands. They remain in the receipt's exact argv and identity; this helper
// only finds the verb, never equates different workspaces or configurations.
func checkProgramWords(words []string) []string {
	if len(words) == 0 {
		return words
	}
	switch filepath.Base(words[0]) {
	case "npm", "pnpm", "yarn":
	default:
		return words
	}
	program := words[0]
	i := 1
	for i < len(words) {
		s := words[i]
		switch s {
		case "--filter", "--dir", "-C", "--prefix", "--cwd", "--workspace", "-w":
			if i+1 >= len(words) {
				return nil
			}
			i += 2
			continue
		case "--recursive", "-r", "--silent", "--offline", "--frozen-lockfile":
			i++
			continue
		}
		if strings.HasPrefix(s, "--filter=") || strings.HasPrefix(s, "--dir=") || strings.HasPrefix(s, "--prefix=") || strings.HasPrefix(s, "--cwd=") || strings.HasPrefix(s, "--workspace=") || (strings.HasPrefix(s, "--config.") && strings.Contains(s, "=")) {
			i++
			continue
		}
		break
	}
	return append([]string{program}, words[i:]...)
}

// CheckCommand recognizes executable checks, never quoted mentions in prose.
func CheckCommand(command string) bool {
	for _, part := range approval.SplitBashCommand(command) {
		words := strings.Fields(part.Command)
		for len(words) > 0 && assignment.MatchString(words[0]) {
			words = words[1:]
		}
		if checkWords(words) {
			return true
		}
	}
	return false
}
func observerFunctions() string {
	var patterns []string
	programs := map[string]bool{}
	for _, rule := range checkInvocations {
		programs[rule[0]] = true
		pattern := strings.Join(rule, ":")
		if len(rule) < 3 {
			pattern += ":*"
		}
		patterns = append(patterns, pattern)
	}
	script := `_codeaf_is_check() {
 local _codeaf_program="$1"
 shift
 case "$_codeaf_program" in npm|pnpm|yarn)
  while [ "$#" -gt 0 ]; do
   case "$1" in
    --filter|--dir|-C|--prefix|--cwd|--workspace|-w) [ "$#" -gt 1 ] || return 1; shift 2;;
    --recursive|-r|--silent|--offline|--frozen-lockfile|--filter=*|--dir=*|--prefix=*|--cwd=*|--workspace=*|--config.*=*) shift;;
    *) break;;
   esac
  done;;
 esac
 case "$_codeaf_program:${1-}:${2-}" in ` + strings.Join(patterns, "|") + ") return 0;; *) return 1;; esac; }\n"
	names := make([]string, 0, len(programs))
	for p := range programs {
		names = append(names, p)
	}
	sort.Strings(names)
	for _, p := range names {
		script += fmt.Sprintf("%s() { if _codeaf_is_check %s \"$@\"; then _codeaf_check %s \"$@\"; else command %s \"$@\"; fi; }\n", p, p, p, p)
	}
	return script + "export -f _codeaf_is_check _codeaf_check _codeaf_worktree_state " + strings.Join(names, " ") + "\n"
}

func observerNames() string {
	names := []string{"_codeaf_is_check", "_codeaf_check", "_codeaf_worktree_state"}
	seen := map[string]bool{}
	for _, rule := range checkInvocations {
		if !seen[rule[0]] {
			names = append(names, rule[0])
			seen[rule[0]] = true
		}
	}
	return strings.Join(names, " ")
}
