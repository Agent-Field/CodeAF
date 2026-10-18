package factory

import (
	"reflect"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// PlanFilesMost bounds a declared list, and PlanPathMost one path in it. A
// list past either is not a plan's file list, it is something else shaped
// like one, so it is rejected whole.
const (
	PlanFilesMost = 200
	PlanPathMost  = 512
)

// filesHead is a plan's file declaration: "files:", "## Files",
// "files to touch:". Anything after the colon on that line is the list
// itself. NOTHING ELSE IS A DECLARATION: not a claim, not a bullet in prose,
// not a path that happens to appear in a sentence.
var filesHead = regexp.MustCompile(`(?i)^(?:#{1,6}\s+)?(?:planned\s+)?files(?:\s+to\s+(?:touch|change|edit))?\s*(?::\s*(.*))?$`)

// listMark is a markdown bullet or a numbered item.
var listMark = regexp.MustCompile(`^(?:[-*+]|\d+[.)])\s+`)

// PlanFilesOf is the paths the item's current plan declared, in the order it
// declared them, and nil when they are unknown. It is derived from the saved
// plan step's own result every time it is asked, never from a cache, so an
// item saved before this rule reads correctly without a migration.
//
// UNKNOWN IS NOT EMPTY AND NOT THE PREVIOUS LIST. A plan that declared no
// list, an ambiguous one, a plan sent back to run again or asking mid-round,
// out of attempts or a round behind, or an item with two
// steps called plan all answer nil, and the brief omits the count.
func PlanFilesOf(it Item) []string {
	if it.Stream == nil {
		return nil
	}
	var plan *Phase
	k := -1
	for i := range it.Stream.Phases {
		if strings.EqualFold(strings.TrimSpace(it.Stream.Phases[i].Name), "plan") {
			if plan != nil {
				return nil
			}
			plan, k = &it.Stream.Phases[i], i
		}
	}
	if plan == nil || !planFinished(it, k) {
		return nil
	}
	return ParsePlanFiles(plan.Result.Output)
}

// planFinished says the plan step's saved result is a finished plan of record,
// so its declaration may speak for the brief. A FINISHED ROUND IS THE ONE THE
// STEP IS ON: a step sent back keeps its old result and moves to a new round
// (rewindToStep), so a result whose round is behind is the rejected plan, even
// though it finished. A done step speaks. A waiting step speaks only in the
// shape older workers stored for an approve question, the finished plan waiting
// with its gate pending, read the one way [ApproveBackTarget] reads it. A step
// asking mid-round or out of attempts is not a plan anybody approved.
//
// Everything here except the legacy shape's finished flag crosses the wire, so
// an item read back from a job answers the same, or unknown, never a list the
// server did not send.
func planFinished(it Item, k int) bool {
	p := it.Stream.Phases[k]
	if p.Result == nil || !resultIsThisRound(p) {
		return false
	}
	switch p.State {
	case PhaseDone:
		return true
	case PhaseWaiting:
		gate, prev := ApproveBackTarget(it.Stream)
		return it.QKind == QKindApprove && gate == k+1 && prev == k
	}
	return false
}

// resultIsThisRound says the step's newest round line is its current round. A
// step with no round lines was saved before they were kept and is read as it
// was.
func resultIsThisRound(p Phase) bool {
	n := len(p.Rounds)
	return n == 0 || p.Rounds[n-1].Round == max(p.Round, 1)
}

// ParsePlanFiles reads the file list a plan's output declares: a "Files"
// heading or "files:" line OUTSIDE any code fence, with its paths either inline
// and comma separated or one per bullet beneath it. A path with spaces is
// backticked or quoted. A fenced block is an example, never a declaration. When
// the declaration is anything else, any one entry in it is not a
// repository-relative path, the declaration is a placeholder ("none"), or two
// declarations disagree, the WHOLE list is unknown (nil): a guess at which was
// meant is a wrong count on a person's screen.
func ParsePlanFiles(output string) []string {
	lines := strings.Split(output, "\n")
	var found []string
	declared := false
	var fence string
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if fence != "" {
			if closesFence(trim, fence) {
				fence = ""
			}
			continue
		}
		if f := opensFence(trim); f != "" {
			fence = f
			continue
		}
		m := filesHead.FindStringSubmatch(trim)
		if m == nil {
			continue
		}
		got := declaration(m[1], lines[i+1:])
		if got == nil || (declared && !reflect.DeepEqual(got, found)) {
			return nil
		}
		found, declared = got, true
	}
	return found
}

// opensFence returns the fence marker ("```" or "~~~", at its full length) a
// line opens, or "".
func opensFence(trim string) string {
	for _, c := range []string{"`", "~"} {
		n := len(trim) - len(strings.TrimLeft(trim, c))
		if n >= 3 && !(c == "`" && strings.Contains(trim[n:], "`")) {
			return trim[:n]
		}
	}
	return ""
}

// closesFence is a line of only the opening character, at least as long.
func closesFence(trim, fence string) bool {
	return len(trim) >= len(fence) && strings.Trim(trim, fence[:1]) == ""
}

// declaration is one declared list: the inline text after the head, then the
// bullets that follow it. nil is unknown, including a lone placeholder.
func declaration(inline string, rest []string) []string {
	var out []string
	if inline = strings.TrimSpace(inline); inline != "" {
		if out = splitInline(inline); out == nil {
			return nil
		}
	}
	for _, next := range rest {
		trim := strings.TrimSpace(next)
		if trim == "" {
			continue
		}
		if !listMark.MatchString(trim) {
			break
		}
		p, ok := wrapped(strings.TrimSpace(listMark.ReplaceAllString(trim, "")))
		if !ok {
			return nil
		}
		out = append(out, p)
	}
	if len(out) == 1 {
		switch strings.ToLower(out[0]) {
		case "none", "n/a", "tbd":
			return nil
		}
	}
	return finishPaths(out)
}

// splitInline splits a comma separated list, honouring quoted and backticked
// paths. nil is ambiguity.
func splitInline(s string) []string {
	var out []string
	rest := strings.TrimSpace(s)
	for {
		var tok string
		if rest != "" && strings.ContainsRune("`\"'", rune(rest[0])) {
			end := strings.IndexByte(rest[1:], rest[0])
			if end < 0 {
				return nil
			}
			tok, rest = rest[:end+2], strings.TrimSpace(rest[end+2:])
			if rest != "" && rest[0] != ',' {
				return nil
			}
		} else if comma := strings.IndexByte(rest, ','); comma >= 0 {
			tok, rest = strings.TrimSpace(rest[:comma]), rest[comma:]
		} else {
			tok, rest = rest, ""
		}
		p, ok := wrapped(tok)
		if !ok {
			return nil
		}
		out = append(out, p)
		if rest == "" {
			return out
		}
		rest = strings.TrimSpace(rest[1:])
	}
}

// wrapped is one entry with its quotes or backticks taken off. Only a quoted
// entry may hold a space.
func wrapped(tok string) (string, bool) {
	if len(tok) >= 2 && strings.ContainsRune("`\"'", rune(tok[0])) && tok[len(tok)-1] == tok[0] {
		tok = strings.TrimSpace(tok[1 : len(tok)-1])
	} else if strings.IndexFunc(tok, unicode.IsSpace) >= 0 {
		return "", false
	}
	tok = strings.TrimPrefix(tok, "./")
	return tok, ValidPlanPath(tok)
}

func finishPaths(in []string) []string {
	if len(in) == 0 || len(in) > PlanFilesMost {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range in {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// ValidPlanPath says whether s is a repository-relative path that reads the
// same on every machine: no absolute or drive path, no URL or other colon, no
// "." or ".." segment, no empty segment (so no directory), no glob (* and ?; brackets and braces are
// legal literal names such as app/[id]/page.tsx), no backslash, no control, format or bidirectional-override character. A file
// need not exist yet, and no extension is demanded: Makefile and .gitignore
// are paths.
func ValidPlanPath(s string) bool {
	if s == "" || len(s) > PlanPathMost || !utf8.ValidString(s) || strings.ContainsAny(s, `\:*?<>|`) || s[0] == '/' || s[0] == '~' {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == ' ' || r == ' ' {
			return false
		}
	}
	for _, seg := range strings.Split(s, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.HasSuffix(seg, ".") || seg != strings.TrimSpace(seg) {
			return false
		}
	}
	return true
}
