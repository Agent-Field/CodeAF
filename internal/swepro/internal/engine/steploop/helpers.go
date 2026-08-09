package steploop

import (
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/Agent-Field/swe-pro-go/internal/engine/msgmodel"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// PlanDBInfo is prompt.ts:1468-1474's parser result.
type PlanDBInfo struct {
	ProjectID  string `json:"projectID"`
	RootTaskID string `json:"rootTaskID"`
	DBPath     string `json:"dbPath"`
}

// ECMAScript \s is WhiteSpace ∪ LineTerminator. Go's RE2 \s is ASCII-only,
// so every code point is explicit (standing fidelity rule).
const jsSpaceClass = `[\t\n\v\f\r \x{00A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}]`

var (
	projectPattern  = regexp.MustCompile(`(?i)Project:` + jsSpaceClass + `+(p-[a-z0-9-]+)`)
	rootPattern     = regexp.MustCompile(`(?i)Root task:` + jsSpaceClass + `+(t-[a-z0-9-]+)`)
	databasePattern = regexp.MustCompile(
		`Database:` + jsSpaceClass + `+([^\n\r\x{2028}\x{2029}]+)`,
	)
)

// PlanDBInfoFromMessages is prompt.ts:1460-1476. It scans chronological
// messages and text parts, returning the first single part containing both
// Project and Root task. Database is case-sensitive and optional.
func PlanDBInfoFromMessages(msgs []msgmodel.WithParts) *PlanDBInfo {
	for _, msg := range msgs {
		for _, raw := range msg.Parts {
			part, ok := raw.(msgmodel.TextPart)
			if !ok {
				continue
			}
			project := projectPattern.FindStringSubmatch(part.Text)
			root := rootPattern.FindStringSubmatch(part.Text)
			if len(project) < 2 || len(root) < 2 {
				continue
			}
			dbPath := ""
			if database := databasePattern.FindStringSubmatch(part.Text); len(database) >= 2 {
				dbPath = jscompat.Trim(database[1])
			}
			return &PlanDBInfo{
				ProjectID:  project[1],
				RootTaskID: root[1],
				DBPath:     dbPath,
			}
		}
	}
	return nil
}

// BackScanResult is prompt.ts:1590-1601's reverse scan.
type BackScanResult struct {
	LastUser      *msgmodel.User
	LastAssistant *msgmodel.Assistant
	LastFinished  *msgmodel.Assistant
	Tasks         []msgmodel.Part
}

// BackScan scans chronological filtered messages from newest to oldest.
func BackScan(msgs []msgmodel.WithParts) BackScanResult {
	var out BackScanResult
	for i := len(msgs) - 1; i >= 0; i-- {
		msg := msgs[i]
		switch info := msg.Info.(type) {
		case msgmodel.User:
			if out.LastUser == nil {
				copy := info
				out.LastUser = &copy
			}
		case msgmodel.Assistant:
			if out.LastAssistant == nil {
				copy := info
				out.LastAssistant = &copy
			}
			if out.LastFinished == nil && info.Finish != nil && *info.Finish != "" {
				copy := info
				out.LastFinished = &copy
			}
		}
		if out.LastUser != nil && out.LastFinished != nil {
			break
		}
		if out.LastFinished == nil {
			for _, part := range msg.Parts {
				switch part.(type) {
				case msgmodel.CompactionPart, msgmodel.SubtaskPart:
					out.Tasks = append(out.Tasks, part)
				}
			}
		}
	}
	return out
}

// ShouldExit is prompt.ts:1612-1620, including lookup of the persisted
// last-assistant WithParts by id. Provider-executed tool parts do not count.
func ShouldExit(lastUser *msgmodel.User, lastAssistant *msgmodel.Assistant, msgs []msgmodel.WithParts) bool {
	if lastUser == nil || lastAssistant == nil || lastAssistant.Finish == nil || *lastAssistant.Finish == "" {
		return false
	}
	if *lastAssistant.Finish == orFinishToolCalls {
		return false
	}

	var persisted *msgmodel.WithParts
	for i := len(msgs) - 1; i >= 0; i-- {
		assistant, ok := msgs[i].Info.(msgmodel.Assistant)
		if ok && assistant.ID == lastAssistant.ID {
			persisted = &msgs[i]
			break
		}
	}
	if persisted != nil {
		for _, raw := range persisted.Parts {
			part, ok := raw.(msgmodel.ToolPart)
			if ok && !part.ProviderExecuted() {
				return false
			}
		}
	}
	return jsStringLess(lastUser.ID, lastAssistant.ID)
}

const orFinishToolCalls = "tool-calls"

// jsStringLess is JavaScript relational string comparison: lexicographic
// UTF-16 code units. Message IDs are ASCII, but fixtures pin the language
// operation too so this helper is safe on synthetic test IDs.
func jsStringLess(left, right string) bool {
	a := utf16.Encode([]rune(left))
	b := utf16.Encode([]rune(right))
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

// WrapLateUserText is synthetic-user site 5 (prompt.ts:1785-1801). The input
// must be a fresh store load because this mutates text-part values in place.
func WrapLateUserText(msgs []msgmodel.WithParts, lastFinished msgmodel.Assistant) {
	for mi := range msgs {
		user, ok := msgs[mi].Info.(msgmodel.User)
		if !ok || !jsStringLess(lastFinished.ID, user.ID) {
			continue
		}
		for pi, raw := range msgs[mi].Parts {
			part, ok := raw.(msgmodel.TextPart)
			if !ok || boolValue(part.Ignored) || boolValue(part.Synthetic) || jscompat.Trim(part.Text) == "" {
				continue
			}
			part.Text = strings.Join([]string{
				"<system-reminder>",
				"The user sent the following message:",
				part.Text,
				"",
				"Please address this message and continue with your tasks.",
				"</system-reminder>",
			}, "\n")
			msgs[mi].Parts[pi] = part
		}
	}
}

func boolValue(v *bool) bool { return v != nil && *v }

func newestFirst(msgs []msgmodel.WithParts) []msgmodel.WithParts {
	out := append([]msgmodel.WithParts(nil), msgs...)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func latestAssistantAgent(msgs []msgmodel.WithParts, fallback string) string {
	start := 0
	if len(msgs) > 10 {
		start = len(msgs) - 10
	}
	for i := len(msgs) - 1; i >= start; i-- {
		if assistant, ok := msgs[i].Info.(msgmodel.Assistant); ok {
			return assistant.Agent
		}
	}
	return fallback
}

func defaultLastModel(msgs []msgmodel.WithParts) *msgmodel.UserModel {
	for i := len(msgs) - 1; i >= 0; i-- {
		if user, ok := msgs[i].Info.(msgmodel.User); ok {
			model := user.Model
			return &model
		}
	}
	return nil
}
