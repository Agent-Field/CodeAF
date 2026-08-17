package config

// APPROVAL MEMORY: the "always" a person presses on a consent card, written
// down.
//
// internal/session's consent.go already remembers an "always" for the life of
// one agent, and says why that memo is not a setting: a session-scoped answer
// that survived the session would be a policy change nobody made. This file is
// the other half of that sentence — the place a person CAN make it, reached by
// a deliberate press on the card rather than by a background inference — and
// everything here exists to keep the two apart. The session's memo is memory;
// these two functions are a WRITE to the person's own config.json, into the
// same rows the settings sheet edits and the same rows they can undo there.
//
// Three laws hold the seam together.
//
//   - IT WRITES THE PERSON'S ROW AND NOTHING ELSE. Merging one entry into their
//     own tools.approval map does not touch the project layer's merge law
//     (projectconfig.go, law 2), which is about the two FILES: a repository's
//     row still replaces the person's WHOLESALE at launch. The honest
//     consequence, stated here because it is surprising: pressing always inside
//     a repository that answers tools.approval writes a preference that takes
//     effect everywhere EXCEPT that repository. Nothing else would be safe —
//     silently editing the repository's file would be this surface committing to
//     somebody's repository on their behalf, and quietly promoting the person's
//     row over it would break the one law that makes a checked-in rule set
//     readable.
//   - A MALFORMED EXISTING ROW IS REPORTED, NEVER CLOBBERED. A row that does not
//     parse is a rule set somebody wrote and believes is in force; rewriting it
//     from a parse that already failed would delete rules by accident. The write
//     refuses, and the surface that asked drops the refusal (consent.go).
//   - AN ALREADY-ANSWERED CALL IS NOT WRITTEN TWICE. Pressing always on the same
//     command in two sessions leaves one rule, because a row that grew a
//     duplicate every time would be a row nobody can read back.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/approval"
)

// BashRule is one entry of the bash rules row: a glob in internal/approval's
// restricted dialect ('*' and literal text, nothing else) and the answer it
// carries.
//
// It is a config-side twin of [approval.Rule] rather than that type re-exported,
// for the reason [targetField] in the surface is a twin of session's glossField:
// this one is a line of text a person edits in a settings sheet, that one is a
// decision surface, and one type serving both would be one reason to change
// both.
type BashRule struct {
	Match  string
	Action string
}

// BashApprovalsAt resolves the bash rules row as the person wrote it. The text
// is the record; [ParseBashApprovals] is how a caller reads it.
func BashApprovalsAt(profileDir string) string {
	if value, ok := persistedString(profileDir, KeyBashApprovals); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

// ParseBashApprovals reads the row into the ordered rule list internal/approval
// matches in order.
//
// The shape is one rule per entry, entries separated by commas or newlines, each
// entry an ACTION and then the command glob it answers for:
//
//	allow git status*, allow "npm test -- --grep=a,b", deny rm -rf *
//
// The action leads because it is the short, fixed half: a person scanning the
// row is looking for the word deny, and a command line is long enough to push it
// off the end of a line if it went first. The glob may be QUOTED in Go's own
// syntax, and has to be when it carries a comma, a newline or a quote of its own
// — a shell line contains anything, and a separator that a command can also
// contain is a row that reads back as two rules nobody wrote. Unquoted is
// accepted and is what the row looks like nine times in ten.
//
// ORDER IS THE AUTHOR'S PRIORITY STATEMENT and is preserved exactly: the policy
// takes the first rule that matches, so a deny somebody put at the top of the row
// stays at the top of the row.
func ParseBashApprovals(raw string) ([]BashRule, error) {
	var rules []BashRule
	for _, entry := range splitOutsideQuotes(raw) {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		action, rest, found := strings.Cut(entry, " ")
		action = strings.ToLower(strings.TrimSpace(action))
		rest = strings.TrimSpace(rest)
		if !found || rest == "" {
			return nil, fmt.Errorf("write each rule as `allow <command>` — %q says only %q", entry, action)
		}
		if !knownToolApprovalMode(action) {
			return nil, fmt.Errorf("%q is not allow, prompt or deny", action)
		}
		match := rest
		if strings.HasPrefix(rest, `"`) {
			unquoted, err := strconv.Unquote(rest)
			if err != nil {
				return nil, fmt.Errorf("the command in %q opens a quote it does not close", entry)
			}
			match = unquoted
		}
		if strings.TrimSpace(match) == "" {
			return nil, fmt.Errorf("%q names no command", entry)
		}
		rules = append(rules, BashRule{Match: match, Action: action})
	}
	return rules, nil
}

// FormatBashApprovals writes the rules back as the row's own text, quoting only
// the globs that need it. A glob that reads plainly stays plain: the row is a
// line a person edits by hand, and quotes around every entry would be a file
// format wearing a settings row's clothes.
func FormatBashApprovals(rules []BashRule) string {
	entries := make([]string, 0, len(rules))
	for _, rule := range rules {
		entries = append(entries, rule.Action+" "+quoteMatch(rule.Match))
	}
	return strings.Join(entries, ", ")
}

// quoteMatch quotes a glob that would otherwise read back as something else: one
// carrying a separator, a quote, or edge whitespace the parser would trim off.
func quoteMatch(match string) string {
	if strings.ContainsAny(match, ",\n\"") || strings.TrimSpace(match) != match {
		return strconv.Quote(match)
	}
	return match
}

// splitOutsideQuotes breaks the row at the commas and newlines that are not
// inside a quoted glob. It is the one place the row's separators are decided, so
// the parser and the writer cannot disagree about where an entry ends.
func splitOutsideQuotes(raw string) []string {
	var (
		entries []string
		current strings.Builder
		quoted  bool
	)
	for index := 0; index < len(raw); index++ {
		char := raw[index]
		if quoted {
			current.WriteByte(char)
			switch char {
			case '\\':
				if index+1 < len(raw) {
					index++
					current.WriteByte(raw[index])
				}
			case '"':
				quoted = false
			}
			continue
		}
		switch char {
		case '"':
			quoted = true
			current.WriteByte(char)
		case ',', '\n':
			entries = append(entries, current.String())
			current.Reset()
		default:
			current.WriteByte(char)
		}
	}
	entries = append(entries, current.String())
	return entries
}

// RememberToolApproval merges one tool's answer into the person's tool
// exceptions row — their file, their row, one entry changed and everything else
// left exactly as they typed it.
//
// An entry that already says this is not written at all: the caller gets nil and
// the file is not touched, which is what makes pressing always twice a no-op
// rather than a row that grows.
//
// See this file's head for the one surprising consequence: while a REPOSITORY
// answers tools.approval, its row replaces the person's whole at launch
// (projectconfig.go, law 2), so a preference written here takes effect
// everywhere except inside that repository.
func RememberToolApproval(profileDir, tool, action string) error {
	tool = strings.TrimSpace(tool)
	action = strings.ToLower(strings.TrimSpace(action))
	if tool == "" {
		return fmt.Errorf("no tool to remember")
	}
	if !knownToolApprovalMode(action) {
		return fmt.Errorf("%q is not allow, prompt or deny", action)
	}
	raw := ToolApprovalsAt(profileDir)
	existing, err := ParseToolApprovals(raw)
	if err != nil {
		// The row is somebody's rule set and it does not parse. Rewriting it from
		// a failed parse would drop rules they believe are in force.
		return fmt.Errorf("settings row %q: %w", KeyToolApprovals, err)
	}
	if current, named := existing[tool]; named {
		if current == action {
			return nil
		}
		return writeToolApprovals(profileDir, replacePair(raw, tool, action))
	}
	entry := tool + ":" + action
	if strings.TrimSpace(raw) == "" {
		return writeToolApprovals(profileDir, entry)
	}
	// APPENDING KEEPS THE PERSON'S OWN TEXT. The row is stored as they typed it
	// (writeToolApprovals says why), and a merge that rebuilt it from the parsed
	// map would reorder and re-space a line they can read today.
	return writeToolApprovals(profileDir, strings.TrimRight(raw, " ,\n")+", "+entry)
}

// replacePair swaps one entry's value and leaves the order alone. It is the
// rewriting path — a tool that already had a DIFFERENT answer — so it is the one
// case where the row is rebuilt, and it is rebuilt in the order it was written.
func replacePair(raw, name, value string) string {
	entries := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	})
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if key, _, found := strings.Cut(entry, ":"); found && strings.TrimSpace(key) == name {
			out = append(out, name+":"+value)
			continue
		}
		out = append(out, entry)
	}
	return strings.Join(out, ", ")
}

// RememberBashApproval appends one allow rule for the SHAPE a person has just
// picked on a consent card.
//
// It takes the shape and not the line. The card banks what was CHOSEN
// (bashshapes.go derives the offer, tui3's consent.go puts it), so the argument
// here is `git status*` as readily as `git status --short`, and this function
// asks nothing about which of the two it was handed: a rule is a glob, and the
// person read the glob before they pressed the key. What it does ask is whether
// the glob is one worth writing down — [bashShapeHolds] — because a shape that
// pins nothing down is a row entry that answers for every command there is.
//
// Four refusals, and each of them is internal/approval's law rather than this
// file's caution:
//
//   - A COMPOUND SHAPE CANNOT BE REMEMBERED. An allow rule vouches only for a
//     single command it matches whole (bash.go's matching law), so a rule written
//     for `cd /tmp && rm -rf build` could never fire. Writing it anyway would put
//     a line in somebody's settings that says they approved something and does
//     nothing at all.
//   - A SHAPE THAT NAMES NO COMMAND IS NOT A RULE, and neither is one whose only
//     word is sudo (bashshapes.go states both).
//   - A SHAPE THE ROW ALREADY ALLOWS IS NOT WRITTEN AGAIN, whether the rule that
//     allows it is this exact glob or a broader one somebody wrote by hand.
//   - A SHAPE THE ROW ALREADY DENIES OR ASKS ABOUT IS LEFT ALONE, and the caller
//     is told. First match wins, so an allow appended after a standing deny is a
//     rule that never runs; the standing rule is a decision the person made in
//     the settings sheet, and a keystroke on a card does not overturn it.
//
// A LINE IS STORED AS THE GLOB IT IS. The dialect has no escape for '*', so a
// command line containing one is remembered as a pattern with a wildcard in it —
// `ls *.go` approved is `ls *.go` allowed. That is the honest reading of the
// line the person saw and approved, it stays bounded to a single non-compound
// command, and the critical-command table still asks about the shapes that
// destroy a disk whatever this row says.
func RememberBashApproval(profileDir, match string) error {
	match = strings.TrimSpace(match)
	if match == "" {
		return fmt.Errorf("no command to remember")
	}
	if !approval.Vouchable(match) {
		return fmt.Errorf("a compound command cannot be remembered: an allow answers for one whole command")
	}
	if !bashShapeHolds(match) {
		return fmt.Errorf("%q names no command to allow", match)
	}
	raw := BashApprovalsAt(profileDir)
	rules, err := ParseBashApprovals(raw)
	if err != nil {
		return fmt.Errorf("settings row %q: %w", KeyBashApprovals, err)
	}
	policy := approval.Policy{BashPatterns: asApprovalRules(rules)}
	if rule, matched := policy.MatchRule(match); matched {
		if rule.Action == approval.ActionAllow {
			return nil
		}
		return fmt.Errorf("settings row %q already answers this command with %s (%q)",
			KeyBashApprovals, rule.Action, rule.Match)
	}
	rules = append(rules, BashRule{Match: match, Action: string(approval.ActionAllow)})
	return writeBashApprovals(profileDir, FormatBashApprovals(rules))
}

// asApprovalRules is the one conversion between the row's rules and the policy's.
func asApprovalRules(rules []BashRule) []approval.Rule {
	out := make([]approval.Rule, 0, len(rules))
	for _, rule := range rules {
		out = append(out, approval.Rule{Match: rule.Match, Action: approval.Action(rule.Action)})
	}
	return out
}

// writeBashApprovals validates the row, then keeps the text. It is
// [writeToolApprovals] for the rules list and for the same reason: the text is
// the record a person reads back, and the parse is derived on every read anyway.
func writeBashApprovals(profileDir, raw string) error {
	if _, err := ParseBashApprovals(raw); err != nil {
		return err
	}
	return writeText(profileDir, KeyBashApprovals, raw)
}
