// This file ports src/session/review-synthesizer.ts:1-161.
package reviewgate

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/agentjson"
)

type SynthesizerBlocker struct {
	File     *string  `json:"file"`
	Line     *float64 `json:"line"`
	Severity string   `json:"severity"`
	Source   string   `json:"source"`
	Detail   string   `json:"detail"`
}

type SynthesizerDecision struct {
	Verdict     string               `json:"verdict"`
	Reason      string               `json:"reason"`
	Blockers    []SynthesizerBlocker `json:"blockers"`
	RepairHints []string             `json:"repair_hints"`
	Confidence  string               `json:"confidence"`
}

var SynthFallback = SynthesizerDecision{
	Verdict: "fail",
	Reason: "Review Synthesizer produced no parseable JSON after retries. " +
		"Defaulting to fail per the high-risk gate's stance: default-pass is forbidden.",
	Blockers: []SynthesizerBlocker{{
		File: nil, Line: nil, Severity: "blocker", Source: "reviewer",
		Detail: "synthesizer-fallback: upstream verdicts could not be merged; treating as fail",
	}},
	RepairHints: []string{
		"Review synthesizer fallback — check agent logs for parse failure details",
	},
	Confidence: "low",
}

type SynthesizerSchema struct{}

func (SynthesizerSchema) SafeParse(raw json.RawMessage) agentjson.Validation[SynthesizerDecision] {
	decision, issues := ParseSynthesizerDecision(raw)
	return agentjson.Validation[SynthesizerDecision]{Data: decision, Issues: issues}
}

func ParseSynthesizerDecision(raw json.RawMessage) (SynthesizerDecision, []agentjson.Issue) {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return SynthesizerDecision{}, []agentjson.Issue{{Message: "Expected object"}}
	}
	required := []string{"verdict", "reason", "blockers", "repair_hints", "confidence"}
	issues := exactKeys(object, required, nil)
	readString := func(key string) string {
		value, ok := object[key]
		if !ok {
			return ""
		}
		var text string
		if json.Unmarshal(value, &text) != nil {
			issues = append(issues, agentjson.Issue{Path: []string{key}, Message: "Expected string"})
		}
		return text
	}
	out := SynthesizerDecision{
		Verdict: readString("verdict"), Reason: readString("reason"),
		Confidence: readString("confidence"),
	}
	if out.Verdict != "pass" && out.Verdict != "fail" {
		issues = append(issues, agentjson.Issue{Path: []string{"verdict"}, Message: "Invalid enum value"})
	}
	if utf16Length(out.Reason) < 1 || utf16Length(out.Reason) > 4000 {
		issues = append(issues, agentjson.Issue{Path: []string{"reason"}, Message: "String length out of range"})
	}
	if out.Confidence != "high" && out.Confidence != "medium" && out.Confidence != "low" {
		issues = append(issues, agentjson.Issue{Path: []string{"confidence"}, Message: "Invalid enum value"})
	}

	if value, ok := object["blockers"]; ok && !bytes.Equal(value, []byte("null")) {
		var values []json.RawMessage
		if json.Unmarshal(value, &values) != nil {
			issues = append(issues, agentjson.Issue{Path: []string{"blockers"}, Message: "Expected array or null"})
		} else {
			out.Blockers = make([]SynthesizerBlocker, 0, len(values))
			for index, item := range values {
				blocker, nested := parseSynthBlocker(item, index)
				issues = append(issues, nested...)
				out.Blockers = append(out.Blockers, blocker)
			}
		}
	}
	if value, ok := object["repair_hints"]; ok && !bytes.Equal(value, []byte("null")) {
		var values []json.RawMessage
		if json.Unmarshal(value, &values) != nil {
			issues = append(issues, agentjson.Issue{Path: []string{"repair_hints"}, Message: "Expected array or null"})
		} else {
			out.RepairHints = make([]string, 0, len(values))
			for index, item := range values {
				var text string
				if json.Unmarshal(item, &text) != nil {
					issues = append(issues, agentjson.Issue{
						Path:    []string{"repair_hints", jscompat.FormatNumber(float64(index))},
						Message: "Expected string",
					})
					continue
				}
				out.RepairHints = append(out.RepairHints, text)
			}
		}
	}
	return out, issues
}

func exactKeys(
	object map[string]json.RawMessage,
	required []string,
	base []string,
) []agentjson.Issue {
	issues := []agentjson.Issue{}
	allowed := map[string]struct{}{}
	for _, key := range required {
		allowed[key] = struct{}{}
		if _, ok := object[key]; !ok {
			issues = append(issues, agentjson.Issue{Path: appendPath(base, key), Message: "Required"})
		}
	}
	for key := range object {
		if _, ok := allowed[key]; !ok {
			issues = append(issues, agentjson.Issue{
				Path: appendPath(base, key), Message: "Unrecognized key(s) in object",
			})
		}
	}
	return issues
}

func parseSynthBlocker(raw json.RawMessage, index int) (SynthesizerBlocker, []agentjson.Issue) {
	base := []string{"blockers", jscompat.FormatNumber(float64(index))}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return SynthesizerBlocker{}, []agentjson.Issue{{Path: base, Message: "Expected object"}}
	}
	keys := []string{"file", "line", "severity", "source", "detail"}
	issues := exactKeys(object, keys, base)
	readString := func(key string) string {
		var text string
		if value, ok := object[key]; ok && json.Unmarshal(value, &text) != nil {
			issues = append(issues, agentjson.Issue{Path: appendPath(base, key), Message: "Expected string"})
		}
		return text
	}
	out := SynthesizerBlocker{
		Severity: readString("severity"), Source: readString("source"), Detail: readString("detail"),
	}
	if value, ok := object["file"]; ok && !bytes.Equal(value, []byte("null")) {
		var text string
		if json.Unmarshal(value, &text) != nil {
			issues = append(issues, agentjson.Issue{Path: appendPath(base, "file"), Message: "Expected string or null"})
		} else {
			out.File = &text
		}
	}
	if value, ok := object["line"]; ok && !bytes.Equal(value, []byte("null")) {
		var line float64
		if json.Unmarshal(value, &line) != nil {
			issues = append(issues, agentjson.Issue{Path: appendPath(base, "line"), Message: "Expected number or null"})
		} else {
			out.Line = &line
		}
	}
	if out.Severity != "blocker" && out.Severity != "major" && out.Severity != "minor" {
		issues = append(issues, agentjson.Issue{Path: appendPath(base, "severity"), Message: "Invalid enum value"})
	}
	if out.Source != "reviewer" && out.Source != "auditor" {
		issues = append(issues, agentjson.Issue{Path: appendPath(base, "source"), Message: "Invalid enum value"})
	}
	if utf16Length(out.Detail) < 1 {
		issues = append(issues, agentjson.Issue{Path: appendPath(base, "detail"), Message: "String must contain at least 1 character(s)"})
	}
	return out, issues
}

type SynthesizerPromptInput struct {
	Workspace       string
	ParentSessionID string
	ReviewerVerdict string
	AuditorVerdict  string
	TaskID          string
}

func BuildSynthesizerPrompt(input SynthesizerPromptInput, outputPath string) string {
	return strings.Join([]string{
		"# Review Synthesizer — merge two parallel verdicts",
		"",
		"Task ID: " + input.TaskID,
		"",
		"## Reviewer verdict (code-quality)",
		"",
		"```json",
		input.ReviewerVerdict,
		"```",
		"",
		"## Auditor verdict (spec-satisfaction)",
		"",
		"```json",
		input.AuditorVerdict,
		"```",
		"",
		"## Decision rules (apply in order)",
		"",
		"1. If EITHER agent returns verdict=fail → synthesis is fail.",
		"2. If BOTH return verdict=pass → synthesis is pass.",
		"3. Confidence = MIN of upstream confidences (high < medium < low).",
		"",
		"## Your output",
		"",
		"Write a single JSON SynthesizerDecision object to " + outputPath + " per the contract in your role.",
		"For pass verdicts, blockers may be null. For fail verdicts, blockers must be non-empty.",
		"Preserve `source` (reviewer | auditor) on each blocker so the repair task knows the provenance.",
	}, "\n")
}

func IsHighRisk(task any) bool {
	var tags []string
	description := ""
	switch value := task.(type) {
	case *plandb.Task:
		if value == nil {
			return false
		}
		tags = value.Tags
		if value.Description != nil {
			description = *value.Description
		}
	case plandb.Task:
		return IsHighRisk(&value)
	case map[string]any:
		if raw, ok := value["tags"].([]string); ok {
			tags = raw
		} else if raw, ok := value["tags"].([]any); ok {
			for _, item := range raw {
				if text, ok := item.(string); ok {
					tags = append(tags, text)
				}
			}
		}
		description, _ = value["description"].(string)
	default:
		return false
	}
	for _, tag := range tags {
		if tag == "risk:high" {
			return true
		}
	}
	return strings.Contains(description, "RISK: high") ||
		strings.Contains(description, "RISK:high")
}

func SynthesizedReviewVerdict(decision SynthesizerDecision) ReviewVerdict {
	bugs := make([]ReviewBug, 0, len(decision.Blockers))
	for _, blocker := range decision.Blockers {
		file := ""
		if blocker.File != nil {
			file = *blocker.File
		}
		bugs = append(bugs, ReviewBug{
			File: file, Line: blocker.Line, Severity: blocker.Severity,
			Detail: "[" + blocker.Source + "] " + blocker.Detail,
		})
	}
	hints := decision.RepairHints
	if hints == nil {
		hints = []string{}
	}
	return ReviewVerdict{
		Verdict: decision.Verdict, Confidence: decision.Confidence,
		Done:         decision.Verdict == "pass",
		SpecCoverage: utf16Slice(decision.Reason, 0, 1000),
		Bugs:         bugs, RepairHints: hints,
		Evidence: "synthesized from reviewer+auditor (high-risk flagged path); reason: " +
			utf16Slice(decision.Reason, 0, 400),
	}
}
