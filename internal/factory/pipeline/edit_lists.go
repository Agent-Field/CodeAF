package pipeline

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── REPLACING THE FILE'S OWN RULES, ASK-FIRST AND NEVER LINES ───────────────
//
// set_list replaces one of the file's three lists of lines whole: rules (what
// every step is told), start.ask_first (protected paths, as `touch <path>`,
// and sentences a person is asked about) and start.never (kept for files that
// still write it: each line is a protected path, exactly as `touch <path>`). The edit is the list as it
// should read, every line, so an editor never has to say which line moved or
// went: what it shows is what is sent, and what is sent is what is committed.
//
// THESE ARE THE REPOSITORY'S LINES ONLY. The factory's own house rules are
// added to a read elsewhere ([WithHouseRules]) and are never in the file, so
// they are never in a list here, never shown as the file's and never written
// into it. A repository that wants a house rule as its own writes it here.
//
// A LINE IS CHECKED AS THE PARSER WILL READ IT BACK. The parser trims each
// line, skips a blank one, keeps the first of two rules or two ask-first lines
// that differ only in case, and keeps the first of two never paths that are the
// same. Each of those would make the committed file read as another list than
// the one previewed, so each is refused here instead: a blank line, a repeat
// (by case for rules and ask_first, exactly for never), a line break or a
// control character. A line under ask_first (a sentence, or a path rule such as
// touch auth/) and a path under never (auth/, infra/**) are written as given:
// the runner matches a path as a glob ([touches], [PlanHold]) and the parser
// takes any one line there, so no other shape is asked of them. The bounds are
// the factory's own for its house rules, so a line that fits one fits the other.

// The lists set_list replaces, in the order a preview reports them.
const (
	ListRules    = "rules"
	ListAskFirst = "ask_first"
	ListNever    = "never"
)

// EditListMost is how many lines one list may hold after a set_list, and
// EditLineMax how long one line may be, in characters.
const (
	EditListMost = factory.HouseRulesMost
	EditLineMax  = factory.HouseRuleLongest
)

// listNames is every list set_list replaces, in the order a preview reports
// them. labels is start.labels (edit_settings.go): read as names, a repeat by
// case, as GitHub reads a label.
var listNames = []string{ListRules, ListAskFirst, ListNever, ListLabels}

// ListChange is one list whose lines an edit changed, every line before and
// every line after, in order.
type ListChange struct {
	List   string
	Before []string
	After  []string
}

// checkSetList bounds a set_list without the file. It names a list and its
// lines and nothing else; each line is trimmed as it will be written.
func checkSetList(i int, op EditOp) (EditOp, *EditError) {
	if op.Name != "" || op.Pipeline != "" || op.After != nil || op.Kind != nil || op.Value != nil || stepFieldsNamed(op) || settingFieldsNamed(op) {
		return op, refuseEdit(EditBadOp, i, "set_list takes only a list and its lines")
	}
	if !slices.Contains(listNames, op.List) {
		return op, refuseEdit(EditBadValue, i, "the list is rules, ask_first, never or labels")
	}
	if op.Lines == nil {
		return op, refuseEdit(EditBadValue, i, "set_list needs its lines; send an empty list to clear it")
	}
	if len(op.Lines) > EditListMost {
		return op, refuseEdit(EditBadValue, i, "a list holds at most %d lines", EditListMost)
	}
	lines := make([]string, 0, len(op.Lines))
	seen := map[string]bool{}
	for _, raw := range op.Lines {
		line, why := cleanListLine(raw)
		if why != "" {
			return op, refuseEdit(EditBadValue, i, "%s", why)
		}
		// never is matched as paths, exactly; rules and ask_first are read
		// as sentences, and the parser keeps one of two that differ in case.
		key := line
		if op.List != ListNever {
			key = strings.ToLower(line)
		}
		if seen[key] {
			return op, refuseEdit(EditBadValue, i, "a line is in a list once")
		}
		seen[key] = true
		lines = append(lines, line)
	}
	op.Lines = lines
	return op, nil
}

// cleanListLine is one line trimmed, or why it cannot be written as one.
func cleanListLine(s string) (string, string) {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return "", "a line cannot be empty"
	case !utf8.ValidString(s):
		return "", "a line must be valid UTF-8"
	case utf8.RuneCountInString(s) > EditLineMax:
		return "", fmt.Sprintf("a line is at most %d characters", EditLineMax)
	}
	for _, r := range s {
		if unicode.IsControl(r) || r == 0x2028 || r == 0x2029 || r == 0xfeff {
			return "", "a line is one line of plain text"
		}
	}
	return s, ""
}

// listOf is the list name names in f, as the file holds it.
func listOf(f File, name string) []string {
	switch name {
	case ListRules:
		return f.Rules
	case ListAskFirst:
		return f.Start.AskFirst
	case ListLabels:
		return f.Start.Labels
	}
	return f.Start.Never
}

// setList applies one set_list. A list is the subject of one set_list per
// preview, so a second one is refused rather than silently winning.
func (st *editState) setList(i int, op EditOp) *EditError {
	if st.listSeen[op.List] {
		return refuseEdit(EditBadOp, i, "the %s list is set once", op.List)
	}
	st.listSeen[op.List] = true
	st.listed = true
	lines := append([]string{}, op.Lines...)
	switch op.List {
	case ListRules:
		st.out.Rules = lines
	case ListAskFirst:
		st.out.Start.AskFirst = lines
	case ListLabels:
		st.out.Start.Labels = lines
	default:
		st.out.Start.Never = lines
	}
	return nil
}

// listFacts fills the list half of a preview: every list whose lines changed,
// in the order rules, ask_first, never, labels.
func (st *editState) listFacts(res *Edited) {
	res.Listed = true
	res.Lists = []ListChange{}
	for _, name := range listNames {
		before, after := listOf(st.base, name), listOf(st.out, name)
		if slices.Equal(before, after) {
			continue
		}
		res.Lists = append(res.Lists, ListChange{List: name, Before: append([]string{}, before...), After: append([]string{}, after...)})
	}
}

// listsDiffer is why a list did not read back as the lines it was given, or "".
func listsDiffer(back, want File) string {
	for _, name := range listNames {
		if !slices.Equal(listOf(back, name), listOf(want, name)) {
			return fmt.Sprintf("the file format cannot hold that %s list", name)
		}
	}
	return ""
}
