package relayserve

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestA4UnavailableLogsEachCauseWithBoundedRate(t *testing.T) {
	now := time.Unix(100, 0)
	n := newNamespaces(t.TempDir(), func() time.Time { return now }, directory.GraceBounds{}, 0)
	var lines []string
	n.logf = func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }
	for range 20 {
		n.unavailable("activate", errors.New("sync raft/1.db: access denied"))
	}
	n.unavailable("activate", errors.New("unreadable provision"))
	if len(lines) != 2 || !strings.Contains(lines[0], "access denied") || !strings.Contains(lines[1], "unreadable provision") {
		t.Fatalf("A4: repeated or missing causes: %q", lines)
	}
	now = now.Add(time.Minute)
	n.unavailable("activate", errors.New("sync raft/1.db: access denied"))
	if len(lines) != 3 {
		t.Fatalf("A4: rate limit did not expire: %q", lines)
	}
}
