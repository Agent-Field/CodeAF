package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// TestC5MovedChatWorksInItsArrivedWorkspace proves C5 through a scripted
// model's real session/tools: old absolute paths write only into the new copy,
// and the model sees where the chat moved before it chooses its next call.
func TestC5MovedChatWorksInItsArrivedWorkspace(t *testing.T) {
	for _, tool := range []string{"write", "edit", "bash", "read", "grep", "find", "ls"} {
		t.Run(tool, func(t *testing.T) {
			origin, workspace, chat := t.TempDir(), t.TempDir(), t.TempDir()
			old := filepath.Join(origin, "x.txt")
			for _, root := range []string{origin, workspace} {
				if err := os.WriteFile(filepath.Join(root, "x.txt"), []byte("before"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := SaveMeta(chat, Meta{ID: "moved", Workspace: workspace, Origin: origin, FromDevice: "desk"}); err != nil {
				t.Fatal(err)
			}
			fields := map[string]any{"path": old}
			switch tool {
			case "write":
				fields["content"] = "after"
			case "edit":
				fields["edits"] = []map[string]string{{"oldText": "before", "newText": "after"}}
			case "bash":
				fields = map[string]any{"command": fmt.Sprintf("printf after > %q", old)}
			case "grep":
				fields = map[string]any{"path": origin, "pattern": "before"}
			case "find":
				fields = map[string]any{"path": origin, "pattern": "*.txt"}
			case "ls":
				fields = map[string]any{"path": origin}
			}
			args, _ := json.Marshal(fields)
			script := &scriptedCompleter{steps: []step{
				func(_ context.Context, m []ai.Message) (*ai.Response, error) {
					seen := userTextIn(m)
					for _, want := range []string{movedCopyNoteOpening, "desk", origin, workspace, "same relative paths"} {
						if !strings.Contains(seen, want) {
							t.Errorf("model missed %q in %s", want, seen)
						}
					}
					return toolResponse("move-call", tool, string(args)), nil
				},
				func(_ context.Context, m []ai.Message) (*ai.Response, error) {
					var result string
					for _, msg := range m {
						if msg.Role == "tool" {
							result += messageContentText(msg)
						}
					}
					if !strings.Contains(result, "were mapped into the workspace here") {
						t.Errorf("tool did not report path mapping: %s", result)
					}
					if strings.Contains(result, "Invalid arguments") {
						t.Errorf("tool did not run: %s", result)
					}
					return textResponse("done"), nil
				},
			}}
			agent, _ := newTestAgent(t, script, func(c *Config) {
				c.Workspace = workspace
				c.Place = Place{Dir: chat, Workspace: workspace}
			})
			collect(t, mustSubmit(t, agent, "carry on here"))
			if data, _ := os.ReadFile(old); string(data) != "before" {
				t.Fatalf("old project changed: %q", data)
			}
			if tool == "write" || tool == "edit" || tool == "bash" {
				if data, _ := os.ReadFile(filepath.Join(workspace, "x.txt")); string(data) != "after" {
					t.Fatalf("arrived copy not changed: %q", data)
				}
			}
			if !isVolatileNote(movedCopyNoteOpening + "\n\nfacts") {
				t.Fatal("move note is treated as the person's words")
			}
		})
	}
}

// TestC5MovedFileCannotFollowALinkBackToTheOrigin proves C5 when the old
// absolute path would map onto a link leading back to the original project.
func TestC5MovedFileCannotFollowALinkBackToTheOrigin(t *testing.T) {
	for _, tool := range []string{"write", "bash"} {
		t.Run(tool, func(t *testing.T) {
			origin, workspace, chat := t.TempDir(), t.TempDir(), t.TempDir()
			if err := os.Symlink(origin, filepath.Join(workspace, "link")); err != nil {
				t.Fatal(err)
			}
			if err := SaveMeta(chat, Meta{ID: "moved", Origin: origin, Workspace: workspace}); err != nil {
				t.Fatal(err)
			}
			script := &scriptedCompleter{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					args := fmt.Sprintf(`{"path":%q,"content":"wrong"}`, filepath.Join(origin, "link", "x"))
					if tool == "bash" {
						args = fmt.Sprintf(`{"command":%q}`, fmt.Sprintf("printf wrong > %q", filepath.Join(origin, "link", "x")))
					}
					return toolResponse("write-link", tool, args), nil
				},
				func(_ context.Context, m []ai.Message) (*ai.Response, error) {
					var results string
					for _, msg := range m {
						if msg.Role == "tool" {
							results += messageContentText(msg)
						}
					}
					if !strings.Contains(results, "resolves outside its workspace") {
						t.Errorf("link not refused: %s", results)
					}
					return textResponse("stopped"), nil
				},
			}}
			agent, _ := newTestAgent(t, script, func(c *Config) {
				c.Workspace = workspace
				c.Place = Place{Dir: chat, Workspace: workspace}
			})
			collect(t, mustSubmit(t, agent, "write"))
			if _, err := os.Stat(filepath.Join(origin, "x")); !os.IsNotExist(err) {
				t.Fatal("write followed link into origin")
			}
		})
	}
}

// TestC6TakenBackChatUsesTheOriginalProject proves C6 through the real session
// and tool door: absolute paths from the other device's copy follow the chat
// back, even though the original project's metadata no longer has an Origin.
func TestC6TakenBackChatUsesTheOriginalProject(t *testing.T) {
	copyRoot, project, chat := t.TempDir(), t.TempDir(), t.TempDir()
	old := filepath.Join(copyRoot, "x")
	if err := os.WriteFile(old, []byte("copy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveMeta(chat, Meta{ID: "back", Workspace: project, FromWorkspace: copyRoot, FromDevice: "laptop"}); err != nil {
		t.Fatal(err)
	}
	script := &scriptedCompleter{steps: []step{
		func(_ context.Context, m []ai.Message) (*ai.Response, error) {
			note := userTextIn(m)
			for _, want := range []string{"laptop", copyRoot, project, movedCopyNoteOpening} {
				if !strings.Contains(note, want) {
					t.Errorf("missing %q in %s", want, note)
				}
			}
			return toolResponse("back", "write", fmt.Sprintf(`{"path":%q,"content":"home"}`, old)), nil
		}, finalText("done"),
	}}
	agent, _ := newTestAgent(t, script, func(c *Config) { c.Workspace = project; c.Place = Place{Dir: chat, Workspace: project} })
	collect(t, mustSubmit(t, agent, "continue back here"))
	if data, _ := os.ReadFile(old); string(data) != "copy" {
		t.Fatalf("other device's copy changed: %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(project, "x")); string(data) != "home" {
		t.Fatalf("original project not changed: %q", data)
	}
}
