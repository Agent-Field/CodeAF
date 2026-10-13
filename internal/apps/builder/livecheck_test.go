//go:build live

package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// standInCheck is the live smoke's Check until lane B6 lands: schema, kit
// parts and props, labels, binding kinds, children rules, and that every
// record type, field, screen and run a file names exists.
type standInCheck struct{}

func (standInCheck) Run(_ context.Context, _ string, files spec.Files, round int) (bridge.CheckReport, error) {
	issues := spec.Validate(files)
	if len(issues) == 0 {
		issues = semanticIssues(files)
	}
	return bridge.CheckReport{Pass: len(issues) == 0, Round: round, Issues: issues}, nil
}

func semanticIssues(files spec.Files) []spec.Issue {
	bundle, err := spec.Decode(files)
	if err != nil {
		return []spec.Issue{{File: "app.json", Rule: "parse", Message: err.Error()}}
	}
	kit, _ := spec.LoadKit()
	var issues []spec.Issue
	for _, id := range bundle.ScreenIDs() {
		issues = append(issues, screenIssues(bundle, kit, id)...)
	}
	return append(issues, appIssues(bundle)...)
}

func appIssues(b *spec.Bundle) []spec.Issue {
	var issues []spec.Issue
	for typ, rt := range b.App.Records {
		if _, ok := rt.Fields[rt.Title]; !ok {
			issues = append(issues, spec.Issue{File: "app.json", Rule: "title", Message: fmt.Sprintf("%s.title %q is not a field", typ, rt.Title)})
		}
		for name, f := range rt.Fields {
			if (f.Type == spec.FieldRef || f.Type == spec.FieldRefs) && b.App.Records[f.To] == nil {
				issues = append(issues, spec.Issue{File: "app.json", Rule: "ref", Message: fmt.Sprintf("%s.%s refs unknown type %q", typ, name, f.To)})
			}
		}
		if b.App.Capabilities.Records[typ] == "" {
			issues = append(issues, spec.Issue{File: "app.json", Rule: "kit/capability", Message: "no records capability for " + typ})
		}
	}
	for _, t := range b.App.Triggers {
		if b.Runs[t.Run] == nil {
			issues = append(issues, spec.Issue{File: "app.json", Rule: "trigger", Message: "trigger runs unknown run " + t.Run})
		}
	}
	return issues
}

func screenIssues(b *spec.Bundle, kit *spec.Kit, id string) []spec.Issue {
	file := "screens/" + id + ".json"
	var issues []spec.Issue
	add := func(rule, format string, args ...any) {
		issues = append(issues, spec.Issue{File: file, Rule: rule, Message: fmt.Sprintf(format, args...)})
	}
	b.Screens[id].Walk(func(n *spec.Node) {
		part := kit.Part(n.Part)
		if part == nil {
			add("kit/parts", "unknown part %q", n.Part)
			return
		}
		if part.Interactive && n.Label == "" && part.Name != "SectionTabs" {
			add("kit/label", "%s needs a label", n.Part)
		}
		for prop := range n.Props {
			if part.Props[prop] == nil {
				add("kit/parts", "%s has no prop %q", n.Part, prop)
			}
		}
		for event, actions := range n.On {
			if !contains(part.Events, event) {
				add("kit/parts", "%s has no event %q", n.Part, event)
			}
			issues = append(issues, actionIssues(b, file, actions)...)
		}
		if n.Bind != nil {
			issues = append(issues, bindIssues(b, part, file, n)...)
		}
		if rule, _ := part.ChildRule(); rule == "none" && len(n.Children) > 0 {
			add("kit/children", "%s takes no children", n.Part)
		}
	})
	return issues
}

func bindIssues(b *spec.Bundle, part *spec.Part, file string, n *spec.Node) []spec.Issue {
	var issues []spec.Issue
	add := func(rule, format string, args ...any) {
		issues = append(issues, spec.Issue{File: file, Rule: rule, Message: fmt.Sprintf(format, args...)})
	}
	kind, typ := bindKind(n.Bind)
	if !contains(part.Bind, kind) {
		add("kit/bind", "%s cannot bind %q", n.Part, kind)
	}
	if typ != "" && b.App.Records[typ] == nil {
		add("kit/bind", "%s binds unknown record type %q", n.Part, typ)
	}
	if rt := b.App.Records[typ]; rt != nil {
		for _, field := range stringList(n.Props["columns"], n.Props["fields"]) {
			if rt.Fields[field] == nil {
				add("kit/parts", "%s lists %q, not a field of %s", n.Part, field, typ)
			}
		}
	}
	return issues
}

func bindKind(bind *spec.Binding) (kind, recordType string) {
	switch {
	case bind.Records != "":
		return "records", bind.Records
	case bind.Record != "":
		return "record", bind.Type
	case bind.Run != "":
		return "run", ""
	case bind.Runs != "":
		return "runs", ""
	case bind.Approvals != "":
		return "approvals", ""
	}
	return "selection", ""
}

func actionIssues(b *spec.Bundle, file string, actions []spec.Action) []spec.Issue {
	var issues []spec.Issue
	for _, a := range actions {
		verb := a.Verb()
		var target string
		switch verb {
		case "":
			issues = append(issues, spec.Issue{File: file, Rule: "kit/parts", Message: "action has no known verb"})
		case spec.VerbOpen:
			_ = json.Unmarshal(a[verb], &target)
			if b.Screens[target] == nil {
				issues = append(issues, spec.Issue{File: file, Rule: "open", Message: "opens unknown screen " + target})
			}
		case spec.VerbRunsStart:
			_ = json.Unmarshal(a[verb], &target)
			if b.Runs[target] == nil {
				issues = append(issues, spec.Issue{File: file, Rule: "runs.start", Message: "starts unknown run " + target})
			}
		case spec.VerbRecordsDelete:
			if _, ok := a["confirm"]; !ok {
				issues = append(issues, spec.Issue{File: file, Rule: "kit/confirm", Message: "records.delete needs confirm"})
			}
		}
	}
	return issues
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func stringList(values ...any) []string {
	var out []string
	for _, v := range values {
		if list, ok := v.([]any); ok {
			for _, item := range list {
				if s, ok := item.(string); ok {
					out = append(out, s)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}
