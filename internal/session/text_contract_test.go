package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Unix parity 2 contract items 1/2: gated bytes, shell or status expectations.
// Text contract C3: jobs output and the completion note use the same command
// text rule as a foreground result, including CRLF split across writes.
func TestTextContractBackgroundOutput(t *testing.T) {
	agent, _ := jobsAgent(t)
	id := startJob(t, agent, `printf 'version\r'; printf '\nprogress\rnext\r\n'`)
	waitExited(t, agent, id)
	got, bad := runTool(t, agent, "jobs", fmt.Sprintf(`{"action":"output","id":%d}`, id))
	want := "version\r\nprogress\rnext\r\n\n[job "
	if runtime.GOOS == "windows" {
		want = "version\nprogress\rnext\n\n[job "
	}
	if bad || !strings.HasPrefix(got, want) {
		t.Fatalf("jobs output = %q, error=%v", got, bad)
	}
}

// Unix parity 2 contract items 1/2: gated bytes, shell or status expectations.
// Text contract C2/C4: append decodes the original before combining text and
// keeps its UTF-16 BOM and CRLF; ordinary Unix append keeps supplied bytes.
// The receipt counts decoded lines.
func TestTextContractAppendEncoding(t *testing.T) {
	for _, tc := range []struct {
		name          string
		before, after string
	}{
		{"utf8", "first\r\n", "first\r\nnext\r\n"},
		{"le", "\xff\xfef\x00i\x00r\x00s\x00t\x00\r\x00\n\x00", "\xff\xfef\x00i\x00r\x00s\x00t\x00\r\x00\n\x00n\x00e\x00x\x00t\x00\r\x00\n\x00"},
		{"be", "\xfe\xff\x00f\x00i\x00r\x00s\x00t\x00\r\x00\n", "\xfe\xff\x00f\x00i\x00r\x00s\x00t\x00\r\x00\n\x00n\x00e\x00x\x00t\x00\r\x00\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args, err := json.Marshal(map[string]any{"path": "notes.txt", "append": true, "content": "next\n"})
			if err != nil {
				t.Fatal(err)
			}
			scripted := &scriptedCompleter{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return toolResponse("append", "write", string(args)), nil
				},
				func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
					if !strings.Contains(lastToolText(messages), "Appended 1 line to notes.txt; the file now has 2 lines.") {
						t.Errorf("append receipt: %q", lastToolText(messages))
					}
					return textResponse("done"), nil
				},
			}}
			agent, _ := newTestAgent(t, scripted, nil)
			path := filepath.Join(agent.config.Workspace, "notes.txt")
			if err := os.WriteFile(path, []byte(tc.before), 0o600); err != nil {
				t.Fatal(err)
			}
			collect(t, mustSubmit(t, agent, "append one line"))
			got, err := os.ReadFile(path)
			if runtime.GOOS != "windows" && tc.name == "utf8" {
				tc.after = "first\r\nnext\n"
			}
			if err != nil || string(got) != tc.after {
				t.Fatalf("appended bytes = %x, %v; want %x", got, err, tc.after)
			}
		})
	}
}
