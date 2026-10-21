package main

import (
	"encoding/json"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/verification"
)

// ExecutionNotes carries exact command context and observed outcomes to later stages.
// Journal messages are complete; display events are capped and cannot prove results.
func (c *stageConversation) ExecutionNotes() ([]string, *factory.TestCounts) {
	f, err := os.Open(c.file)
	if err != nil {
		return []string{verification.FailurePrefix + " command journal could not be read: " + err.Error()}, nil
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	commands := map[string]string{}
	var notes []string
	var tests *factory.TestCounts
	lastOutcome := map[string]string{}
	setupFailures := map[string]string{}
	var journalErr error
	checks, checkErr := verification.Read(c.file)
	observed := map[string]bool{}
	latest := map[string]verification.Check{}
	for _, check := range checks {
		observed[check.Command] = true
		latest[check.Key()] = check
	}
	corrected := map[string]bool{}
	for _, check := range latest {
		if !verification.PresentationOnly(check.Command) {
			continue
		}
		identity := verificationCommand(check.Command)
		if _, seen := corrected[identity]; !seen {
			corrected[identity] = true
		}
		if check.Exit == nil || *check.Exit != 0 {
			corrected[identity] = false
		}
	}
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
			journalErr = err
			break
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
		// Setup refusal happens before the shell runs and has no exit receipt.
		// Keep it even for commands outside the legacy check-name vocabulary.
		if strings.HasPrefix(row.Content, verification.SetupFailurePrefix) {
			setupFailures[cmd] = row.Content
			continue
		}
		delete(setupFailures, cmd)
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
		if observed[cmd] && verification.PresentationOnly(cmd) {
			// The pipeline may have failed while its check passed. Keep the
			// shell receipt honest; only the observed check decides the guard.
			receipt["scope"] = "shell presentation; check exits recorded separately"
		} else {
			lastOutcome[cmd] = receipt["outcome"].(string)
		}
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
	// Exact argv, directory and target context identify a check. A later
	// corrected presentation of THAT check can clear an earlier failure; a
	// passing subset, other directory or target cannot.
	var pending []string
	for command, failure := range setupFailures {
		pending = append(pending, command+" · "+failure)
	}
	if journalErr != nil {
		pending = append(pending, "command journal could not be decoded: "+journalErr.Error())
	}
	for command, outcome := range lastOutcome {
		if outcome == "failed" && !(verification.PresentationOnly(command) && corrected[verificationCommand(command)]) {
			pending = append(pending, command)
		}
	}
	keys := make([]string, 0, len(latest))
	for key := range latest {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		check := latest[key]
		outcome := "unknown"
		if check.Exit != nil {
			if *check.Exit == 0 {
				outcome = "passed"
			} else {
				outcome = "failed"
			}
		}
		raw, _ := json.Marshal(map[string]any{"command": check.Command, "check": check.Args, "cwd": check.Cwd, "exit": check.Exit, "outcome": outcome})
		notes = append(notes, "Check receipt: "+string(raw))
		if outcome != "passed" {
			pending = append(pending, check.Label()+" · "+outcome)
		}
	}
	if len(notes) > 12 {
		notes = notes[len(notes)-12:]
	}
	if checkErr != nil {
		pending = append(pending, "check receipts could not be read: "+checkErr.Error())
	}
	sort.Strings(pending)
	for _, command := range pending {
		notes = append(notes, verification.FailurePrefix+" "+command)
	}
	return notes, tests
}

// Old transcripts have only a pipeline's exit. Match a newly observed rerun
// conservatively to the identical command before its presentation pipe. Shell
// substitutions and compound alternatives stay exact rather than guessing at
// what they executed. Changed flags, environment and cd prefixes stay distinct.
func verificationCommand(command string) string {
	quote := byte(0)
	escaped := false
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		if ch == '`' || ch == '(' || ch == ';' || ch == '\n' {
			return command
		}
		if ch == '|' {
			if i+1 < len(command) && command[i+1] == '|' {
				return command
			}
			return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(command[:i]), "2>&1"))
		}
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(command), "2>&1"))
}
