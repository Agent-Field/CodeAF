package praf

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/agentsession"
	secappx "github.com/Agent-Field/codeaf/internal/agentsession/appx"
)

func TestReviewAnswerReserveStaysInsideTheRequestedSessionWall(t *testing.T) {
	for _, wall := range []time.Duration{time.Second, 45 * time.Second, 10 * time.Minute} {
		got := limitsFor(options{sessionWall: wall})(secappx.HarnessOptions{Label: "reviewer"})
		if got.Wall != wall || got.AnswerReserve != min(30*time.Second, wall/2) {
			t.Fatalf("requested %s, got %+v", wall, got)
		}
	}
}

func TestReviewFailedAgentsKeepLabelsTimesAndReasons(t *testing.T) {
	var notes bytes.Buffer
	watch := sessionFailureNotes(&notes)
	watch("successful", agentsession.SessionResult{Duration: time.Second}, nil)
	if notes.Len() != 0 {
		t.Fatal("successful sessions emitted failure evidence")
	}
	var group sync.WaitGroup
	for _, label := range []string{"anatomy", "coverage"} {
		group.Go(func() {
			watch(label, agentsession.SessionResult{Duration: 45 * time.Second, Turns: 6, Tools: 4}, context.DeadlineExceeded)
		})
	}
	group.Wait()
	for _, want := range []string{"agent anatomy failed after 45s (6 turns, 4 reads): context deadline exceeded", "agent coverage failed after 45s (6 turns, 4 reads): context deadline exceeded"} {
		if !strings.Contains(notes.String(), want) {
			t.Fatalf("missing failure evidence %q in %q", want, notes.String())
		}
	}
}
