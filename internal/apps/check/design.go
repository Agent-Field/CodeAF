package check

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// Design warnings never block Keep: they ride along in a FIX call that a real
// failure already triggered, so they cannot cost reach.

// designLint is one screen-level design rule.
type designLint func(file string, screen *spec.Screen) []Issue

var designLints = []designLint{onePrimary, listEmptyState, placeholderText, actionLabels}

var (
	placeholder   = regexp.MustCompile(`\b(?:(?i:lorem|ipsum|placeholder text|sample text)|TODO|TBD|XXX+|(?i:test) \d+)\b`)
	questionStart = regexp.MustCompile(`(?i)^(what|how|why|when|which|who|whose|is|are|should|can|do|does)\b`)
)

// shownProps are the props whose text the owner reads as written.
var shownProps = []string{"text", "title", "subtitle", "description"}

// DesignWarnings lists design-law findings that do not fail Check.
func DesignWarnings(b *spec.Bundle) []Issue {
	var issues []Issue
	for _, id := range b.ScreenIDs() {
		for _, lint := range designLints {
			issues = append(issues, lint("screens/"+id+".json", b.Screens[id])...)
		}
	}
	for i, command := range b.App.Commands {
		if hint := actionLabelHint(command.Label); hint != "" {
			issues = append(issues, finding(spec.AppFile, fmt.Sprintf("/commands/%d/label", i), "design/action-label", fmt.Sprintf("command label %q", command.Label), hint))
		}
	}
	return issues
}

// walkPaths visits every node of a screen with its JSON pointer.
func walkPaths(screen *spec.Screen, visit func(n *spec.Node, path string)) {
	var walk func(*spec.Node, string)
	walk = func(n *spec.Node, path string) {
		if n == nil {
			return
		}
		visit(n, path)
		nodeChildren(n, path, walk)
	}
	walk(screen.Nav, "/nav")
	walk(screen.Body, "/body")
}

func onePrimary(file string, screen *spec.Screen) []Issue {
	var paths []string
	walkPaths(screen, func(n *spec.Node, path string) {
		if n.Part == "Button" && n.Props["kind"] == "primary" {
			paths = append(paths, path)
		}
	})
	if len(paths) <= 1 {
		return nil
	}
	return []Issue{finding(file, paths[1]+"/props/kind", "design/primary",
		fmt.Sprintf("%d primary buttons on one screen (%s)", len(paths), strings.Join(paths, ", ")),
		"keep kind primary on the one main action; set the others to secondary or quiet")}
}

func listEmptyState(file string, screen *spec.Screen) []Issue {
	var issues []Issue
	walkPaths(screen, func(n *spec.Node, path string) {
		if n.Part == "RecordList" && n.Slots["empty"] == nil {
			issues = append(issues, finding(file, path+"/slots", "design/empty", "RecordList has no empty state",
				`add slots.empty: {"part":"Empty","label":"<what appears here and how to add the first one>"}`))
		}
	})
	return issues
}

func placeholderText(file string, screen *spec.Screen) []Issue {
	var issues []Issue
	check := func(path, text string) {
		if m := placeholder.FindString(text); m != "" {
			issues = append(issues, finding(file, path, "design/placeholder", fmt.Sprintf("placeholder text %q", text),
				"replace "+fmt.Sprintf("%q", m)+" with the words the owner would use for this app"))
		}
	}
	check("/title", screen.Title)
	walkPaths(screen, func(n *spec.Node, path string) {
		check(path+"/label", n.Label)
		for _, key := range shownProps {
			if text, ok := n.Props[key].(string); ok {
				check(ptr(path+"/props", key), text)
			}
		}
	})
	return issues
}

func actionLabels(file string, screen *spec.Screen) []Issue {
	var issues []Issue
	walkPaths(screen, func(n *spec.Node, path string) {
		if n.Part != "Button" || n.Label == "" {
			return
		}
		if hint := actionLabelHint(n.Label); hint != "" {
			issues = append(issues, finding(file, path+"/label", "design/action-label", fmt.Sprintf("button label %q", n.Label), hint))
		}
	})
	return issues
}

// actionLabelHint says how to fix an action label that is not a command.
func actionLabelHint(label string) string {
	text := strings.TrimSpace(strings.TrimSuffix(label, " · AI"))
	if strings.HasSuffix(text, "?") || questionStart.MatchString(text) {
		return "start the label with the verb the action does (e.g. Summarise month, Plan week), not a question; keep a trailing · AI"
	}
	return ""
}
