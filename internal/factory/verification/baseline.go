package verification

import (
	"crypto/sha256"
	"encoding/json"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/approval"
)

// Snapshot hashes the tracked diff and untracked files as a Git blob. A clean
// checkout hashes an empty blob, in the repository's object format.
const (
	emptyGitBlobSHA1   = "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"
	emptyGitBlobSHA256 = "473a0f4c3be8a93681a267e3b1e9a7dcda1185436fe141f7749120a303721813"
)

// BaselineMatches accounts for fresh required Go test failures reproduced in a
// separate, clean worktree of the same repository at the job's pinned base.
// Outputs contains complete foreground tool output keyed by native call ID.
// The returned evidence does not change either measured check or its exit.
func BaselineMatches(checks []Check, latest map[string]Check, at Snapshot, base string, outputs map[string]string) map[string]Check {
	if len(outputs) < 2 || base == "" || at.Head == "" {
		return nil
	}
	var matched map[string]Check
	for key, check := range latest {
		relative, inWorktree := checkRelativeDir(check)
		if !inWorktree || check.Diagnostic || check.GitRoot != check.OwnerRoot || !check.Fresh(at) {
			continue
		}
		failed := baselineFailure(check, outputs)
		if len(failed) == 0 {
			continue
		}
		old, found := newestBaseline(checks, check, relative, base)
		if !found || old.CallID == check.CallID || !old.CleanDetachedBase ||
			old.Before.Head != base || old.Before != old.After || !cleanBase(old.Before) {
			continue
		}
		if !includesFailures(baselineFailure(old, outputs), failed) {
			continue
		}
		if matched == nil {
			matched = make(map[string]Check)
		}
		matched[key] = old
	}
	return matched
}

func includesFailures(base, candidate map[testIdentity][sha256.Size]byte) bool {
	for id, failure := range candidate {
		if old, ok := base[id]; !ok || old != failure {
			return false
		}
	}
	return len(candidate) != 0
}

func checkRelativeDir(check Check) (string, bool) {
	if !filepath.IsAbs(check.Cwd) || !filepath.IsAbs(check.GitRoot) || !filepath.IsAbs(check.GitCommonDir) {
		return "", false
	}
	relative, err := filepath.Rel(check.GitRoot, check.Cwd)
	return relative, err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func cleanBase(at Snapshot) bool {
	return len(at.Head) == 40 && at.Fingerprint == emptyGitBlobSHA1 ||
		len(at.Head) == 64 && at.Fingerprint == emptyGitBlobSHA256
}

func newestBaseline(checks []Check, candidate Check, relative, base string) (Check, bool) {
	var latest Check
	found, ambiguous := false, false
	for _, check := range checks {
		if !check.Diagnostic || check.Inherited || check.Source != candidate.Source ||
			check.OwnerRoot != candidate.OwnerRoot || check.GitRoot == candidate.GitRoot ||
			check.GitCommonDir != candidate.GitCommonDir ||
			(check.Before.Head != base && check.After.Head != base) ||
			!slices.Equal(check.Args, candidate.Args) || !slices.Equal(check.Context, candidate.Context) {
			continue
		}
		if dir, ok := checkRelativeDir(check); !ok || dir != relative {
			continue
		}
		if !found || check.order > latest.order {
			latest, found, ambiguous = check, true, false
		} else if check.order == latest.order {
			ambiguous = true
		}
	}
	return latest, found && !ambiguous
}

func baselineFailure(check Check, outputs map[string]string) map[testIdentity][sha256.Size]byte {
	if check.Inherited || check.Source == "" || check.CallID == "" || check.Problem != "" ||
		check.Exit == nil || *check.Exit != 1 || !directBaselineGoTest(check) {
		return nil
	}
	return goJSONFailures(outputs[check.CallID], check.GitRoot)
}

func directBaselineGoTest(check Check) bool {
	if !baselineGoArgs(check.Args) || len(check.Context) != len(checkContextNames) {
		return false
	}
	for i, name := range checkContextNames {
		value := check.Context[i]
		// The fence's cache permission flag changes file modes, not selected
		// code. Other global flags or an external workspace can select code
		// outside the snapshot even when a check's direct argv looks local.
		if name == "GOFLAGS" && value != "" && value != "-modcacherw" || name == "GOWORK" && value != "" && value != "off" {
			return false
		}
	}
	if strings.ContainsAny(check.Command, "\r\n") {
		return false
	}
	parts := approval.SplitBashCommand(check.Command)
	if len(parts) == 2 && parts[0].Separator == "&&" {
		words := baselineShellWords(parts[0].Command)
		if len(words) != 2 || words[0] != "cd" || words[1] == "" || strings.HasPrefix(words[1], "-") {
			return false
		}
		parts = parts[1:]
	}
	return len(parts) == 1 && parts[0].Separator == "" && slices.Equal(baselineShellWords(parts[0].Command), check.Args)
}

// Admit literal words only, including quoted regular expressions and paths.
// An exact argv comparison then confirms the literal command is what ran.
var baselineShellFragment = regexp.MustCompile("^(?:'([^']*)'|\"([^\"\\\\$`!]*)\"|([^ \\t\\x00-\\x1f\"'\\\\$`;|&<>(){}*?\\[\\]~#!]+))")

func baselineShellWords(command string) []string {
	var words []string
	for command = strings.TrimLeft(command, " \t"); command != ""; command = strings.TrimLeft(command, " \t") {
		var word strings.Builder
		for command != "" && command[0] != ' ' && command[0] != '\t' {
			parts := baselineShellFragment.FindStringSubmatch(command)
			if parts == nil {
				return nil
			}
			word.WriteString(parts[1] + parts[2] + parts[3])
			command = command[len(parts[0]):]
		}
		words = append(words, word.String())
	}
	return words
}

func baselineGoArgs(args []string) bool {
	if len(args) < 4 || args[0] != "go" || args[1] != "test" {
		return false
	}
	seen := map[string]bool{}
	for i := 2; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			if arg != "." && (!strings.HasPrefix(arg, "./") || slices.Contains(strings.Split(arg, "/"), "..") || strings.HasSuffix(arg, ".go")) {
				return false
			}
			continue
		}
		flag, value, hasValue := strings.Cut(arg, "=")
		if seen[flag] {
			return false
		}
		seen[flag] = true
		switch flag {
		case "-json", "-race", "-short", "-v":
			if hasValue && value != "true" && (flag == "-json" || value != "false") {
				return false
			}
		case "-count", "-run", "-timeout", "-p", "-parallel", "-tags":
			if !hasValue {
				i++
				if i == len(args) {
					return false
				}
				value = args[i]
			}
			if value == "" || flag == "-count" && value != "1" {
				return false
			}
		default:
			return false
		}
	}
	return seen["-json"] && seen["-count"]
}

type testIdentity struct {
	pkg, name string
}

type goTestEvent struct {
	Action, Package, Test, Output, OutputType string
}

type goJSONTest struct {
	outcome   string
	output    []string
	assertion bool
}

type goJSONPackage struct {
	outcome    string
	tests      map[string]*goJSONTest
	classified bool
}

// Go's package result may include a build, collection, panic or timeout
// failure. Require a complete stream and a named assertion for every red
// package. Compare full package/subtest identities and their diagnostic text.
func goJSONFailures(output, root string) map[testIdentity][sha256.Size]byte {
	packages := map[string]*goJSONPackage{}
	for line := range strings.Lines(strings.TrimSpace(output)) {
		var event goTestEvent
		if json.Unmarshal([]byte(line), &event) != nil || event.Package == "" {
			return nil
		}
		pkg := packages[event.Package]
		if event.Action == "start" && event.Test == "" && pkg == nil {
			packages[event.Package] = &goJSONPackage{tests: map[string]*goJSONTest{}}
			continue
		}
		if pkg == nil || pkg.outcome != "" || !pkg.accept(event, root) {
			return nil
		}
	}
	failed := map[testIdentity][sha256.Size]byte{}
	for name, pkg := range packages {
		if !pkg.failures(name, failed) {
			return nil
		}
	}
	return failed
}

func (pkg *goJSONPackage) accept(event goTestEvent, root string) bool {
	if event.Test == "" {
		switch event.Action {
		case "output":
			return packageFrame(event.Package, strings.TrimSpace(event.Output))
		case "pass", "skip", "fail":
			pkg.outcome = event.Action
			return true
		default:
			return false
		}
	}
	test := pkg.tests[event.Test]
	if event.Action == "run" && test == nil {
		pkg.tests[event.Test] = &goJSONTest{}
		return true
	}
	if test == nil || test.outcome != "" {
		return false
	}
	switch event.Action {
	case "output":
		pkg.classified = pkg.classified || event.OutputType != ""
		for line := range strings.Lines(event.Output) {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "panic:") || strings.HasPrefix(line, "fatal error:") || strings.HasPrefix(line, "WARNING: DATA RACE") {
				return false
			}
			if line != "" && !testFrame(event.Test, line) {
				test.output = append(test.output, strings.ReplaceAll(line, root+string(filepath.Separator), ""))
				test.assertion = test.assertion || event.OutputType == "error"
			}
		}
	case "pause", "cont":
	case "pass", "skip", "fail":
		test.outcome = event.Action
	default:
		return false
	}
	return true
}

func packageFrame(pkg, line string) bool {
	return line == "PASS" || line == "FAIL" || line == "testing: warning: no tests to run" ||
		strings.HasPrefix(line, "ok  \t"+pkg+"\t") ||
		strings.HasPrefix(line, "FAIL\t"+pkg+"\t") || strings.HasPrefix(line, "?   \t"+pkg+"\t")
}

var testResultFrame = regexp.MustCompile(`^--- (?:PASS|FAIL|SKIP): (.*) \([0-9.]+s\)$`)

func testFrame(name, line string) bool {
	for _, prefix := range []string{"=== RUN", "=== NAME", "=== PAUSE", "=== CONT"} {
		if rest, ok := strings.CutPrefix(line, prefix+" "); ok && strings.TrimSpace(rest) == name {
			return true
		}
	}
	result := testResultFrame.FindStringSubmatch(line)
	return len(result) != 0 && result[1] == name
}

func (pkg *goJSONPackage) failures(name string, failed map[testIdentity][sha256.Size]byte) bool {
	if pkg.outcome == "" {
		return false
	}
	failedTests := map[string]bool{}
	for test, result := range pkg.tests {
		if result.outcome == "" {
			return false
		}
		if result.outcome == "fail" {
			failedTests[test] = true
		}
	}
	if (len(failedTests) > 0) != (pkg.outcome == "fail") {
		return false
	}
	for test := range failedTests {
		result := pkg.tests[test]
		lines := result.output
		if pkg.classified && !result.assertion || !pkg.classified && !hasLegacyAssertion(lines) {
			// A failed child also marks its parent failed without an assertion.
			// The child's own diagnostics remain a required part of the match.
			if !hasFailedChild(test, failedTests) {
				return false
			}
			continue
		}
		failed[testIdentity{pkg: name, name: test}] = sha256.Sum256([]byte(strings.Join(lines, "\n")))
	}
	return true
}

var goAssertionLocation = regexp.MustCompile(`^[^:]+[.]go:[0-9]+: [^[:space:]]`)

func hasLegacyAssertion(lines []string) bool {
	for _, line := range lines {
		if goAssertionLocation.MatchString(line) {
			return true
		}
	}
	return false
}

func hasFailedChild(parent string, failed map[string]bool) bool {
	for test := range failed {
		if strings.HasPrefix(test, parent+"/") {
			return true
		}
	}
	return false
}
