//go:build !windows

package exec

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/executor"
)

// pipelineStatusCases are the pipeline outcomes every bash door must agree
// on: a reader that stops early is not a failure, and every other failing
// stage still is, pipefail's whole point.
var pipelineStatusCases = []struct {
	command string
	want    int
}{
	{"yes x | head -1", 0},
	{"yes x | head -c 30000", 0},
	{"false | tail -20", 1},
	{"false | yes | head -1", 1},
	{"yes x | head -1 | false", 1},
	{"set -e; yes x | head -1; echo after", 141},
	{"(yes x | head -1)", 141},
	{"yes x | head -1 && false", 141},
	{"exit 141", 141},
}

func TestForegroundShellForgivesOnlyAnEarlyReader(t *testing.T) {
	toolbox := NewToolbox(workspace(t), "1", nil)
	for _, tc := range pipelineStatusCases {
		if got := toolbox.runShell(t.Context(), tc.command, 10, "").exitCode; got != tc.want {
			t.Errorf("%q exited %d, want %d", tc.command, got, tc.want)
		}
	}
}

func TestBackgroundJobForgivesOnlyAnEarlyReader(t *testing.T) {
	for _, tc := range pipelineStatusCases {
		tools, _ := backgroundToolbox(t)
		cmd := fmt.Sprintf(`{"cmd":%q,"bg":true}`, tc.command+" >/dev/null")
		if result := tools.Execute(context.Background(), "sh", cmd); result.IsError {
			t.Fatal(result.Content)
		}
		ceiling := int(slack(10 * time.Second).Seconds())
		result := tools.Execute(context.Background(), "job", fmt.Sprintf(`{"id":1,"wait":%d}`, ceiling))
		if want := fmt.Sprintf("exited %d", tc.want); !bytes.Contains([]byte(result.Content), []byte(want)) {
			t.Errorf("%q: job said %q, want %q", tc.command, result.Content, want)
		}
	}
}

// Services and jobs share detachedCommand; its status is what a job reads.
func TestDetachedCommandForgivesOnlyAnEarlyReader(t *testing.T) {
	for _, tc := range pipelineStatusCases {
		var output bytes.Buffer
		cmd, err := detachedCommand(executor.Host, t.Context(), t.TempDir(), tc.command, nil, &output, 0)
		if err != nil {
			t.Fatal(err)
		}
		_ = cmd.Run()
		if got := cmd.ProcessState.ExitCode(); got != tc.want {
			t.Errorf("%q exited %d, want %d (%s)", tc.command, got, tc.want, output.String())
		}
	}
}
