package builder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func runFailures(files spec.Files, report bridge.CheckReport) []LeftOutAutomation {
	if report.Pass || len(report.Issues) == 0 {
		return nil
	}
	reasons := map[string][]string{}
	for _, issue := range report.Issues {
		dir, name := path.Split(issue.File)
		if dir != "runs/" || !strings.HasSuffix(name, ".yaml") || files[issue.File] == nil {
			return nil
		}
		reasons[issue.File] = append(reasons[issue.File], issue.String())
	}
	var out []LeftOutAutomation
	for file, messages := range reasons {
		sort.Strings(messages)
		out = append(out, LeftOutAutomation{File: file, Name: automationName(file, files[file]), Reason: strings.Join(messages, "\n")})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out
}

func (s *Service) runFallback(ctx context.Context, b *build, files spec.Files, report bridge.CheckReport) error {
	omitted := runFailures(files, report)
	if len(omitted) == 0 {
		return s.giveUp(ctx, b, report)
	}
	next, err := omitRuns(files, omitted)
	if err != nil {
		return err
	}
	if err := s.all.Store.WriteFiles(ctx, b.app, next); err != nil {
		return err
	}
	reduced, err := s.checkRound(ctx, b, next, report.Round, nil)
	if err != nil || !reduced.Pass {
		restoreErr := s.all.Store.WriteFiles(context.WithoutCancel(ctx), b.app, files)
		if err != nil || restoreErr != nil {
			return errors.Join(err, restoreErr)
		}
		return s.giveUp(ctx, b, reduced)
	}
	result := b.result()
	if result.Original == nil {
		result.Original = files
	}
	for _, omission := range omitted {
		found := false
		for i, previous := range result.LeftOut {
			if previous.File == omission.File {
				result.LeftOut[i] = omission
				found = true
			}
		}
		if !found {
			result.LeftOut = append(result.LeftOut, omission)
		}
	}
	if err := s.saveResult(b, result); err != nil {
		return err
	}
	s.step(b, bridge.StepWrite, bridge.BuildDone, "Left out automations that did not pass Check")
	return s.preview(ctx, b, next)
}

func omitRuns(files spec.Files, omitted []LeftOutAutomation) (spec.Files, error) {
	runs := omittedRunIDs(omitted)
	next := spec.Files{}
	merge(next, files)
	for _, item := range omitted {
		delete(next, item.File)
	}
	if !containsRun(next) {
		data, err := omitAgentRunTool(next[spec.AgentFile])
		if err != nil {
			return nil, err
		}
		next[spec.AgentFile] = data
	}
	commands, err := omittedCommands(files[spec.AppFile], runs)
	if err != nil {
		return nil, err
	}
	for _, file := range files.Paths() {
		if file != spec.AppFile && file != spec.PlanFile && !strings.HasPrefix(file, "screens/") {
			continue
		}
		raw, err := rewriteWithoutRuns(file, files[file], runs, commands)
		if err != nil {
			return nil, err
		}
		next[file] = raw
	}
	return next, nil
}

func omittedRunIDs(items []LeftOutAutomation) map[string]bool {
	runs := map[string]bool{}
	for _, item := range items {
		runs[strings.TrimSuffix(path.Base(item.File), ".yaml")] = true
	}
	return runs
}

func containsRun(files spec.Files) bool {
	for file := range files {
		if strings.HasPrefix(file, "runs/") {
			return true
		}
	}
	return false
}

func omittedCommands(raw []byte, runs map[string]bool) (map[string]bool, error) {
	var app struct {
		Commands []map[string]any `json:"commands"`
	}
	if err := json.Unmarshal(raw, &app); err != nil {
		return nil, err
	}
	commands := map[string]bool{}
	for _, command := range app.Commands {
		if stripRunReferences(command, runs, nil) == nil {
			id, _ := command["id"].(string)
			commands[id] = true
		}
	}
	return commands, nil
}

func rewriteWithoutRuns(file string, data []byte, runs, commands map[string]bool) ([]byte, error) {
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	if file == spec.PlanFile {
		value["automation"] = retainedPlanLines(value["automation"], runs)
	} else {
		clean := stripRunReferences(value, runs, commands)
		if obj, ok := clean.(map[string]any); ok {
			value = obj
		}
		if strings.HasPrefix(file, "screens/") && value["body"] == nil {
			value["body"] = map[string]any{"part": "Text", "props": map[string]any{"text": "Automation left out"}}
		}
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	return append(raw, '\n'), err
}

func retainedPlanLines(value any, runs map[string]bool) []any {
	lines, _ := value.([]any)
	kept := []any{}
	for _, raw := range lines {
		line, _ := raw.(map[string]any)
		id, _ := line["id"].(string)
		if !runs[id] {
			kept = append(kept, raw)
		}
	}
	return kept
}

func namesRun(obj map[string]any, runs map[string]bool) bool {
	for _, key := range []string{"run", "runs", "runs.start"} {
		id, _ := obj[key].(string)
		if runs[id] {
			return true
		}
	}
	return false
}

// Removing a run also removes controls whose sole action or binding was that
// automation. Leaving a clickable control behind would promise unavailable work.
func stripRunReferences(value any, runs, commands map[string]bool) any {
	switch v := value.(type) {
	case map[string]any:
		return stripRunObject(v, runs, commands)
	case []any:
		out := []any{}
		for _, raw := range v {
			if clean := stripRunReferences(raw, runs, commands); clean != nil {
				out = append(out, clean)
			}
		}
		return out
	default:
		return value
	}
}

func stripRunObject(obj map[string]any, runs, commands map[string]bool) any {
	if namesRun(obj, runs) {
		return nil
	}
	if bind, ok := obj["bind"].(map[string]any); ok && namesRun(bind, runs) {
		return nil
	}
	out := map[string]any{}
	for key, raw := range obj {
		if key == "commands" || key == "actions" {
			raw = retainedCommandIDs(raw, commands)
		}
		if clean := stripRunReferences(raw, runs, commands); clean != nil {
			out[key] = clean
		}
	}
	if on, ok := obj["on"].(map[string]any); ok && len(on) > 0 && !hasScreenActions(out["on"]) {
		return nil
	}
	if do, ok := obj["do"].([]any); ok && len(do) > 0 {
		if kept, _ := out["do"].([]any); len(kept) == 0 {
			return nil
		}
	}
	return out
}

func retainedCommandIDs(value any, commands map[string]bool) any {
	list, ok := value.([]any)
	if !ok {
		return value
	}
	kept := []any{}
	for _, raw := range list {
		id, _ := raw.(string)
		if !commands[id] {
			kept = append(kept, raw)
		}
	}
	return kept
}

func hasScreenActions(value any) bool {
	on, _ := value.(map[string]any)
	for _, raw := range on {
		if list, ok := raw.([]any); ok && len(list) > 0 {
			return true
		}
	}
	return false
}

func automationName(file string, data []byte) string {
	name := strings.TrimSuffix(path.Base(file), ".yaml")
	if raw, err := spec.YAMLToJSON(data); err == nil {
		var run spec.Run
		if json.Unmarshal(raw, &run) == nil && run.Title != "" {
			name = run.Title
		}
	}
	return name
}
