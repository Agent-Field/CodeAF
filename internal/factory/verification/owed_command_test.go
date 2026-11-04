package verification

import (
	"strings"
	"testing"
)

func TestOwedCommandMissingQuotedPath(t *testing.T) {
	for _, command := range []string{"'/missing/toolchain/go' test ./...", `"/missing/custom tool" test`, "CHECK_EXIT=7 '/missing/custom tool' test"} {
		got := OwedCommand(command)
		if !strings.HasPrefix(got, command) || !strings.Contains(got, "missing on this machine: /missing/") {
			t.Errorf("unsafe literal must retain command and explain missing path: %s", got)
		}
	}
	for _, command := range []string{"", "go test ./...", `"/missing/$TOOL" test`, "'/missing/go'other test"} {
		if got := OwedCommand(command); got != command {
			t.Errorf("nonliteral or ordinary command changed: %q", got)
		}
	}
}
