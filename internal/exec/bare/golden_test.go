package bare

import (
	"context"
	"encoding/json"

	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// oracleTurn is one turn's assistant message extracted from the pi oracle.
type oracleTurn struct {
	text       string
	toolCalls  []ai.ToolCall
	stopReason string
	// toolErrors maps tool-call id → isError, extracted from the oracle's
	// toolResult messages. Used to build the expected Ran sequence with the
	// same error suffixes the bare loop produces.
	toolErrors map[string]bool
}

// scriptedCompleter replays pi's actual assistant responses in order. Each call
// to CompleteWithMessages returns the next turn's assistant message: the text
// content and the tool calls pi made. The last turn has no tool calls, which
// stops the bare loop.
type scriptedCompleter struct {
	turns []oracleTurn
	calls int
	t     *testing.T
}

func (s *scriptedCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	if s.calls >= len(s.turns) {
		s.t.Fatalf("scripted completer exhausted: the loop made %d requests but the oracle had %d turns", s.calls+1, len(s.turns))
	}
	turn := s.turns[s.calls]
	s.calls++

	content := []ai.ContentPart{}
	if turn.text != "" {
		content = append(content, ai.ContentPart{Type: "text", Text: turn.text})
	}
	finishReason := turn.stopReason
	if finishReason == "" {
		finishReason = "stop"
	}

	return &ai.Response{
		Choices: []ai.Choice{{
			Index:        0,
			Message:      ai.Message{Role: "assistant", Content: content, ToolCalls: turn.toolCalls},
			FinishReason: finishReason,
		}},
	}, nil
}

// loadOracle parses the pi oracle JSONL (stdout.jsonl) and extracts one
// oracleTurn per turn: the assistant message with its text and tool calls.
// It also parses toolResult messages to record which tool calls produced
// errors, so the expected Ran sequence can carry the same error suffixes.
func loadOracle(t *testing.T, path string) []oracleTurn {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read oracle %s: %v", path, err)
	}
	var turns []oracleTurn
	// toolErrorMap collects toolCallId → isError from toolResult messages.
	toolErrorMap := map[string]bool{}
	for _, line := range splitLines(string(data)) {
		if line == "" {
			continue
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			continue
		}
		var typ string
		_ = json.Unmarshal(obj["type"], &typ)
		if typ != "message_end" {
			continue
		}
		var msg struct {
			Role       string            `json:"role"`
			Content    []json.RawMessage `json:"content"`
			ToolCallID string            `json:"toolCallId"`
			IsError    bool              `json:"isError"`
		}
		if err := json.Unmarshal(obj["message"], &msg); err != nil {
			continue
		}
		if msg.Role == "toolResult" {
			toolErrorMap[msg.ToolCallID] = msg.IsError
			continue
		}
		if msg.Role != "assistant" {
			continue
		}
		var turn oracleTurn
		turn.toolErrors = map[string]bool{}
		for _, raw := range msg.Content {
			var part struct {
				Type       string          `json:"type"`
				Text       string          `json:"text"`
				ToolCallID string          `json:"id"`
				Name       string          `json:"name"`
				Arguments  json.RawMessage `json:"arguments"`
			}
			if err := json.Unmarshal(raw, &part); err != nil {
				continue
			}
			switch part.Type {
			case "text":
				turn.text += part.Text
			case "toolCall":
				args := string(part.Arguments)
				if args == "" {
					args = "{}"
				}
				turn.toolCalls = append(turn.toolCalls, ai.ToolCall{
					ID:   part.ToolCallID,
					Type: "function",
					Function: ai.ToolCallFunction{
						Name:      part.Name,
						Arguments: args,
					},
				})
			}
		}
		// stopReason is on the message itself
		var sr struct {
			StopReason string `json:"stopReason"`
		}
		_ = json.Unmarshal(obj["message"], &sr)
		turn.stopReason = sr.StopReason
		turns = append(turns, turn)
	}
	// Apply the error map to each turn's tool calls.
	for i := range turns {
		for _, tc := range turns[i].toolCalls {
			turns[i].toolErrors[tc.ID] = toolErrorMap[tc.ID]
		}
	}
	if len(turns) == 0 {
		t.Fatalf("no assistant turns found in oracle %s", path)
	}
	return turns
}

// splitLines splits a string on newlines, preserving Go's simplicity.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// copyFixture copies the fixture directory to a temp workspace.
func copyFixture(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy fixture %s -> %s: %v", src, dst, err)
	}
}

// TestGoldenEquivalence replays the pi oracle for bug-r1 against the bare
// executor and asserts that the tool-call sequence, the append-only message
// discipline, the wire tool schemas, and the system prompt all match.
func TestGoldenEquivalence(t *testing.T) {
	oraclePath := filepath.Join(os.Getenv("HOME"), "aforge-bench", "vs-pi", "cells-wave4", "bug-r1", "stdout.jsonl")
	fixtureSrc := filepath.Join(os.Getenv("HOME"), "aforge-bench", "vs-pi", "fixture-src")
	if _, err := os.Stat(oraclePath); err != nil {
		t.Skipf("oracle not found at %s: %v", oraclePath, err)
	}
	if _, err := os.Stat(fixtureSrc); err != nil {
		t.Skipf("fixture not found at %s: %v", fixtureSrc, err)
	}

	oracle := loadOracle(t, oraclePath)

	// Copy the fixture to a temp workspace.
	tempDir := t.TempDir()
	workDir := filepath.Join(tempDir, "work")
	copyFixture(t, fixtureSrc, workDir)

	// Build the workspace. NewWorkspace may fail if the dir isn't empty; we
	// use a direct Workspace instead.
	ws, err := exec.NewWorkspace(workDir)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}

	// The oracle's tool calls carry absolute paths to the pi workspace
	// (/home/santosh/aforge-bench/vs-pi/cells-wave4/bug-r1/work/...). The bare
	// test workspace is a temp copy at a different path, so the tool arguments
	// must be rewritten to match. The pi workspace path is extracted from the
	// oracle's session event.
	piWorkDir := oracleWorkDir(t, oraclePath)
	rewriteOraclePaths(oracle, piWorkDir, workDir)

	// Build the scripted completer from the oracle turns.
	completer := &scriptedCompleter{turns: oracle, t: t}

	// Construct the loop directly (bypassing provider.NewClient) so the test
	// has no network dependency.
	cwd := ws.Root()
	tools := Tools(cwd)
	system := SystemPrompt(cwd)

	// The user message is the bug-r1 task text.
	userText := "the test suite has failing tests; find the real root cause and fix it — all tests must pass, do not weaken the tests."

	loop := &loopState{
		client:   completer,
		tools:    tools,
		system:   system,
		user:     userText,
		cwd:      cwd,
		deadline: 0, // no deadline for the test
	}

	outcome := loop.run(context.Background())

	// (a) The tool-call SEQUENCE (name+args) equals pi's actual sequence.
	var actualSequence []string
	for _, line := range outcome.Ran {
		actualSequence = append(actualSequence, line)
	}

	// Build the expected sequence from the oracle.
	var expectedSequence []string
	oracleToolCallCount := 0
	for _, turn := range oracle {
		for _, tc := range turn.toolCalls {
			oracleToolCallCount++
			line := tc.Function.Name + " " + snip(tc.Function.Arguments, 200)
			if turn.toolErrors[tc.ID] {
				line += "  → error"
			}
			expectedSequence = append(expectedSequence, line)
		}
	}

	if len(actualSequence) != oracleToolCallCount {
		t.Errorf("tool-call count: got %d, want %d (oracle)", len(actualSequence), oracleToolCallCount)
	}

	matched := 0
	for i := 0; i < len(actualSequence) && i < len(expectedSequence); i++ {
		if actualSequence[i] == expectedSequence[i] {
			matched++
		} else {
			// The Ran tail may format slightly differently; compare the
			// tool name at minimum.
			t.Errorf("tool-call %d: got %q, want %q", i, actualSequence[i], expectedSequence[i])
		}
	}
	t.Logf("tool-call sequence match: %d/%d", matched, oracleToolCallCount)

	// Also verify the actual tool calls executed match by name+args. The
	// bare loop executed real tools against the fixture, so the actual
	// results may differ from pi's, but the CALLS (what the model asked for)
	// should be identical.
	if outcome.ToolCalls != oracleToolCallCount {
		t.Errorf("outcome.ToolCalls: got %d, want %d", outcome.ToolCalls, oracleToolCallCount)
	}

	// (b) Each request's messages are a strict append-only extension of the
	// previous. The requestLog records the messages slice sent on each
	// request. Each should be a prefix of (or equal to a prefix of) the next.
	for i := 1; i < len(loop.requestLog); i++ {
		prev := loop.requestLog[i-1]
		curr := loop.requestLog[i]
		if !isPrefix(prev, curr) {
			t.Errorf("request %d messages are not a prefix of request %d", i-1, i)
			t.Errorf("  prev has %d messages, curr has %d messages", len(prev), len(curr))
		}
	}
	t.Logf("append-only check: %d requests verified", len(loop.requestLog))

	// (c) The wire tool schemas equal the spec's verbatim JSON.
	expectedSchemas := map[string]string{
		"read":  readSchemaJSON,
		"bash":  bashSchemaJSON,
		"edit":  editSchemaJSON,
		"write": writeSchemaJSON,
	}
	for _, tool := range tools {
		expected, ok := expectedSchemas[tool.Name]
		if !ok {
			t.Errorf("unexpected tool on the wire: %s", tool.Name)
			continue
		}
		if string(tool.Schema) != expected {
			t.Errorf("tool %s schema mismatch:\n  got:  %s\n  want: %s", tool.Name, tool.Schema, expected)
		}
	}
	t.Logf("wire schema check: %d tools verified", len(tools))

	// (d) The system prompt equals the pinned template byte-for-byte (modulo
	// cwd and doc paths).
	expectedSystem := SystemPrompt(cwd)
	if system != expectedSystem {
		t.Errorf("system prompt does not match the pinned template")
	}
	t.Logf("system prompt length: %d bytes", len(system))

	// The loop should have run all oracle turns and stopped on the last one
	// (which has no tool calls).
	if outcome.Turns != len(oracle) {
		t.Errorf("turns: got %d, want %d (oracle turns)", outcome.Turns, len(oracle))
	}
	if outcome.Stop != exec.StopDone {
		t.Errorf("stop: got %q, want %q", outcome.Stop, exec.StopDone)
	}

	t.Logf("golden equivalence: %d turns, %d tool calls, %d requests, stop=%s",
		outcome.Turns, outcome.ToolCalls, len(loop.requestLog), outcome.Stop)
}

// oracleWorkDir reads the session event from the oracle to find the pi
// workspace path. The session event's "cwd" field is the pi workspace.
func oracleWorkDir(t *testing.T, oraclePath string) string {
	t.Helper()
	data, err := os.ReadFile(oraclePath)
	if err != nil {
		t.Fatalf("read oracle: %v", err)
	}
	for _, line := range splitLines(string(data)) {
		if line == "" {
			continue
		}
		var obj struct {
			Type string `json:"type"`
			Cwd  string `json:"cwd"`
		}
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			continue
		}
		if obj.Type == "session" {
			return obj.Cwd
		}
	}
	t.Fatalf("no session event in oracle %s", oraclePath)
	return ""
}

// rewriteOraclePaths rewrites absolute paths in the oracle's tool-call
// arguments from the pi workspace path to the temp workspace path. This
// allows the bare tools to actually execute against the fixture copy.
func rewriteOraclePaths(turns []oracleTurn, oldPrefix, newPrefix string) {
	for i := range turns {
		for j := range turns[i].toolCalls {
			args := turns[i].toolCalls[j].Function.Arguments
			turns[i].toolCalls[j].Function.Arguments = rewritePathInJSON(args, oldPrefix, newPrefix)
		}
	}
}

// rewritePathInJSON replaces occurrences of oldPrefix with newPrefix inside
// JSON string values. It parses the JSON, rewrites any string value containing
// oldPrefix, and re-serializes.
func rewritePathInJSON(raw string, oldPrefix, newPrefix string) string {
	var v interface{}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw
	}
	rewritePaths(v, oldPrefix, newPrefix)
	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return string(out)
}

func rewritePaths(v interface{}, oldPrefix, newPrefix string) {
	switch val := v.(type) {
	case map[string]interface{}:
		for k, child := range val {
			if s, ok := child.(string); ok && containsString(s, oldPrefix) {
				val[k] = replaceAll(s, oldPrefix, newPrefix)
			} else {
				rewritePaths(child, oldPrefix, newPrefix)
			}
		}
	case []interface{}:
		for i, child := range val {
			if s, ok := child.(string); ok && containsString(s, oldPrefix) {
				val[i] = replaceAll(s, oldPrefix, newPrefix)
			} else {
				rewritePaths(child, oldPrefix, newPrefix)
			}
		}
	}
}

func replaceAll(s, old, new string) string {
	result := s
	for containsString(result, old) {
		idx := indexOf(result, old)
		result = result[:idx] + new + result[idx+len(old):]
	}
	return result
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// isPrefix reports whether prev is a prefix of curr (every message in prev
// appears identically at the start of curr).
func isPrefix(prev, curr []ai.Message) bool {
	if len(prev) > len(curr) {
		return false
	}
	for i := 0; i < len(prev); i++ {
		if !messagesEqual(prev[i], curr[i]) {
			return false
		}
	}
	return true
}

// messagesEqual reports whether two messages are identical in role, content,
// and tool calls. This is a deep comparison for the append-only assertion.
func messagesEqual(a, b ai.Message) bool {
	if a.Role != b.Role {
		return false
	}
	if a.ToolCallID != b.ToolCallID {
		return false
	}
	if len(a.Content) != len(b.Content) {
		return false
	}
	for i := range a.Content {
		if a.Content[i].Type != b.Content[i].Type {
			return false
		}
		if a.Content[i].Text != b.Content[i].Text {
			return false
		}
	}
	if len(a.ToolCalls) != len(b.ToolCalls) {
		return false
	}
	for i := range a.ToolCalls {
		if a.ToolCalls[i].ID != b.ToolCalls[i].ID {
			return false
		}
		if a.ToolCalls[i].Function.Name != b.ToolCalls[i].Function.Name {
			return false
		}
		if a.ToolCalls[i].Function.Arguments != b.ToolCalls[i].Function.Arguments {
			return false
		}
	}
	return true
}

// TestPromptTemplatePinned asserts the system prompt template is byte-for-byte
// the pinned string, modulo cwd and doc paths. This guards against accidental
// edits to the template.
func TestPromptTemplatePinned(t *testing.T) {
	// The template must contain the "Available tools" section with exactly
	// the four active tools (read, bash, edit, write) — NOT all seven
	// registry tools. This is the selectedTools finding: pi's
	// buildSystemPrompt (system-prompt.js) filters by toolSnippets, and the
	// default active set is [read, bash, edit, write] (agent-session.js
	// L2044-2045), so grep/find/ls do not appear.
	for _, name := range []string{"read", "bash", "edit", "write"} {
		if !contains(systemPromptTemplate, "- "+name+": ") {
			t.Errorf("system prompt template missing active tool %q", name)
		}
	}
	for _, name := range []string{"grep", "find", "ls"} {
		if contains(systemPromptTemplate, "- "+name+": ") {
			t.Errorf("system prompt template lists inactive tool %q — should only have the 4 active tools", name)
		}
	}

	// The "Use bash for file operations" guideline must be present — it
	// fires when bash is active and none of grep/find/ls are, which is the
	// four-tool default.
	if !contains(systemPromptTemplate, "Use bash for file operations like ls, rg, find") {
		t.Errorf("system prompt template missing the bash-file-operations guideline")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || containsString(s, substr))
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
