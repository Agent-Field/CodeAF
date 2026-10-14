package check

import (
	"fmt"
	"regexp"
	"strings"
)

var quotedName = regexp.MustCompile(`'([^']+)'`)

const (
	missingPrefix = "missing properties:"
	extraPrefix   = "additionalProperties"
)

// shape gathers what a schema with alternatives said about one object: each
// alternative reports the keys it misses and the keys it does not allow.
type shape struct {
	first  int        // index of the group's first finding
	needs  []string   // one entry per alternative: the keys it requires
	extras [][]string // per alternative: the keys it rejects
}

// collapseShapes replaces the many findings one wrong object draws from a
// schema with alternatives (a command, an action, a step) with ONE finding
// that names the allowed shapes and the keys to remove. A repair that reads
// forty lines about one object fixes none of them.
func collapseShapes(issues []Issue) []Issue {
	groups := map[string]*shape{}
	for i, issue := range issues {
		if issue.Rule != "schema" || !shapeMessage(issue.Message) {
			continue
		}
		key := issue.File + "\x00" + issue.Path
		g := groups[key]
		if g == nil {
			g = &shape{first: i}
			groups[key] = g
		}
		g.add(issue.Message)
	}
	out := make([]Issue, 0, len(issues))
	for i, issue := range issues {
		g := groups[issue.File+"\x00"+issue.Path]
		collapsed := g != nil && len(g.needs) > 1 && issue.Rule == "schema" && shapeMessage(issue.Message)
		switch {
		case !collapsed:
			out = append(out, issue)
		case i == g.first:
			out = append(out, g.finding(issue))
		}
	}
	return out
}

func shapeMessage(message string) bool {
	return strings.HasPrefix(message, missingPrefix) || strings.HasPrefix(message, extraPrefix)
}

func (g *shape) add(message string) {
	names := []string{}
	for _, m := range quotedName.FindAllStringSubmatch(message, -1) {
		names = append(names, m[1])
	}
	if strings.HasPrefix(message, missingPrefix) {
		g.needs = append(g.needs, strings.Join(names, " + "))
		return
	}
	g.extras = append(g.extras, names)
}

// rejected is the keys every alternative refused, or all refused keys when no
// key was refused by every one.
func (g *shape) rejected() []string {
	count := map[string]int{}
	var order []string
	for _, extras := range g.extras {
		for _, name := range extras {
			if count[name] == 0 {
				order = append(order, name)
			}
			count[name]++
		}
	}
	var all []string
	for _, name := range order {
		if count[name] == len(g.extras) {
			all = append(all, name)
		}
	}
	if len(all) == 0 {
		return order
	}
	return all
}

func (g *shape) finding(first Issue) Issue {
	where := first.File + ":" + first.Path
	message := fmt.Sprintf("%s fits none of its %d allowed shapes; each shape needs one of: %s", where, len(g.needs), strings.Join(g.needs, " | "))
	hint := "rewrite " + where + " as exactly ONE of those shapes: that key holds the arguments, e.g. {\"" + firstKey(g.needs[0]) + "\": ...}"
	if rejected := g.rejected(); len(rejected) > 0 {
		message += "; keys no shape allows here: " + strings.Join(rejected, ", ")
		hint += "; remove " + strings.Join(rejected, ", ") + " from this object (move them inside the shape's key when it takes them)"
	}
	first.Message = message
	first.FixHint = hint
	return first
}

func firstKey(need string) string {
	key, _, _ := strings.Cut(need, " + ")
	return key
}
