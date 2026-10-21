package main

import (
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ExecutionNotes carries exact command context and observed outcomes to later stages.
// Journal messages are complete; display events are capped and cannot prove results.
func (c *stageConversation) ExecutionNotes() ([]string, *factory.TestCounts) {
	f, err := os.Open(c.file)
	if err != nil {
		return nil, nil
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	commands := map[string]string{}
	var notes []string
	var tests *factory.TestCounts
	lastOutcome := map[string]string{}
	for {
		var row struct {
			Role       string `json:"role"`
			Content    string `json:"content"`
			ToolCallID string `json:"toolCallId"`
			Calls      []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"toolCalls"`
		}
		if err := dec.Decode(&row); err != nil {
			if err == io.EOF {
				break
			}
			return notes, tests
		}
		for _, call := range row.Calls {
			if call.Function.Name != "bash" {
				continue
			}
			var args struct {
				Command string `json:"command"`
			}
			if json.Unmarshal([]byte(call.Function.Arguments), &args) == nil && len(args.Command) <= 4000 {
				commands[call.ID] = args.Command
			}
		}
		cmd := commands[row.ToolCallID]
		if row.Role != "tool" || cmd == "" {
			continue
		}
		if !strings.Contains(cmd, "go test") && !strings.Contains(cmd, "go vet") && !strings.Contains(cmd, "pytest") && !strings.Contains(cmd, "npm test") {
			continue
		}
		passed := (strings.Contains(row.Content, "\nPASS\n") || strings.HasPrefix(row.Content, "PASS\n") || strings.Contains(row.Content, "ok  ")) && !strings.Contains(row.Content, "\nFAIL") && !strings.Contains(row.Content, "Command exited with code")
		receipt := map[string]any{"command": cmd, "cwd": c.dir, "outcome": "completed"}
		if strings.Contains(row.Content, "Command exited with code") {
			receipt["outcome"] = "failed"
		} else if passed {
			receipt["outcome"] = "passed"
		}
		lastOutcome[cmd] = receipt["outcome"].(string)
		if strings.Contains(cmd, "go test") {
			counts := &factory.TestCounts{}
			for _, line := range strings.Split(row.Content, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "--- PASS:") {
					counts.Passed++
				}
				if strings.HasPrefix(line, "--- FAIL:") {
					counts.Failed++
				}
				if strings.HasPrefix(line, "--- SKIP:") {
					counts.Skipped++
				}
			}
			if counts.Passed+counts.Failed+counts.Skipped > 0 {
				counts.Command = cmd
				if tests == nil || counts.Passed+counts.Failed+counts.Skipped >= tests.Passed+tests.Failed+tests.Skipped {
					tests = counts
				}
				receipt["tests"] = counts
				receipt["count_scope"] = "named Go test outcomes including subtests; standalone PASS excluded"
			}
		}
		raw, _ := json.Marshal(receipt)
		notes = append(notes, "Command receipt: "+string(raw))
		if len(notes) > 12 {
			notes = notes[len(notes)-12:]
		}
	}
	for command, outcome := range lastOutcome {
		if outcome == "failed" {
			notes = append(notes, "Unresolved verification command failed: "+command)
		}
	}
	return notes, tests
}
