package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// summarize says in plain words what a tool call did: "Looked up 6 deals".
// The person sees this line, so it never carries the call's arguments.
func summarize(b *spec.Bundle, tool string, args map[string]any, out outcome, err error) string {
	typeName, _ := args["type"].(string)
	if err != nil {
		return "Could not use " + humanTool(tool, typeName)
	}
	switch res := out.result.(type) {
	case bridge.QueryResult:
		return "Looked up " + count(b, typeName, len(res.Rows))
	case bridge.Record:
		return verbFor(tool) + " " + titleOf(b, res)
	case bridge.UpdateRecordResult:
		return "Updated " + count(b, typeName, len(res.Rows))
	case bridge.RunState:
		return "Started the " + res.Run + " run"
	}
	if out.approval != nil {
		return "Asked for your approval to " + humanTool(tool, "")
	}
	return "Used " + humanTool(tool, "")
}

func verbFor(tool string) string {
	if tool == bridge.MethodRecordsCreate {
		return "Created"
	}
	return "Opened"
}

func humanTool(tool, recordType string) string {
	name := strings.NewReplacer(".", " ", "_", " ").Replace(tool)
	if recordType != "" {
		return name + " on " + recordType
	}
	return name
}

// count is "1 deal" or "6 deals", with the record type's own plural.
func count(b *spec.Bundle, recordType string, n int) string {
	noun, plural := recordType, recordType+"s"
	if rt := b.App.Records[recordType]; rt != nil {
		if rt.Label != "" {
			noun, plural = strings.ToLower(rt.Label), strings.ToLower(rt.Label)+"s"
		}
		if rt.Plural != "" {
			plural = strings.ToLower(rt.Plural)
		}
	}
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// titleOf is the record's title field, the name a person knows it by.
func titleOf(b *spec.Bundle, r bridge.Record) string {
	if rt := b.App.Records[r.Type]; rt != nil {
		if title, ok := r.Fields[rt.Title].(string); ok && title != "" {
			return title
		}
	}
	return r.ID
}

// describeArgs turns a call's simple arguments into "key: value" lines for an
// approval the person reads.
func describeArgs(args map[string]any) string {
	var lines []string
	for k, v := range args {
		if s, ok := v.(string); ok && s != "" {
			lines = append(lines, k+": "+s)
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
