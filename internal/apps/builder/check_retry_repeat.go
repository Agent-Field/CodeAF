package builder

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// repeatValueMax caps a quoted value so a repeat note stays one line.
const repeatValueMax = 160

// roundMemory remembers last round's findings and the value each one pointed
// at, so a finding that comes back says what was tried and what still breaks.
type roundMemory struct {
	round  int
	values map[string]string // issue key -> value at its path before the fix
}

func issueKey(i spec.Issue) string { return i.File + "\x00" + i.Path + "\x00" + i.Rule }

// remember records this round's findings against the files Check read.
func (m *roundMemory) remember(r bridge.CheckReport, files spec.Files) {
	m.round, m.values = r.Round, map[string]string{}
	for _, issue := range r.Issues {
		m.values[issueKey(issue)] = valueAt(files, issue.File, issue.Path)
	}
}

// note is the repeat annotation for one issue, "" when it is new.
func (m *roundMemory) note(issue spec.Issue, files spec.Files) string {
	before, ok := m.values[issueKey(issue)]
	if !ok {
		return ""
	}
	now := valueAt(files, issue.File, issue.Path)
	change := "you left it unchanged"
	if now != before {
		change = "you changed it from " + before
	}
	return fmt.Sprintf("; REPEATED from round %d — the rule above still fails: value now %s, %s; apply the fix exactly as stated", m.round, now, change)
}

// formatReport is the check report as the FIX call reads it, one issue a
// line, with repeats from the previous round marked.
func (m *roundMemory) formatReport(r bridge.CheckReport, files spec.Files) string {
	lines := make([]string, 0, len(r.Issues))
	for _, issue := range r.Issues {
		lines = append(lines, issue.String()+lengthHint(issue)+m.note(issue, files))
	}
	sortStrings(lines)
	return strings.Join(lines, "\n")
}

// valueAt quotes the JSON value at pointer in file, or a short placeholder.
func valueAt(files spec.Files, file, pointer string) string {
	data, ok := files[file]
	if !ok {
		return "(file missing)"
	}
	if strings.HasSuffix(file, ".yaml") {
		converted, err := spec.YAMLToJSON(data)
		if err != nil {
			return "(unparsable)"
		}
		data = converted
	}
	var value any
	if !strings.HasSuffix(file, ".json") && !strings.HasSuffix(file, ".yaml") || json.Unmarshal(data, &value) != nil {
		return "(whole file)"
	}
	for _, seg := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		if seg == "" {
			continue
		}
		value = step(value, strings.ReplaceAll(strings.ReplaceAll(seg, "~1", "/"), "~0", "~"))
	}
	raw, _ := json.Marshal(value)
	text := string(raw)
	if value == nil {
		text = "(absent)"
	}
	if r := []rune(text); len(r) > repeatValueMax {
		text = string(r[:repeatValueMax]) + "…"
	}
	return text
}

func step(value any, seg string) any {
	switch v := value.(type) {
	case map[string]any:
		return v[seg]
	case []any:
		var i int
		if _, err := fmt.Sscan(seg, &i); err == nil && i >= 0 && i < len(v) {
			return v[i]
		}
	}
	return nil
}
