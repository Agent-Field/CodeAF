package runstate

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/runner"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/status"
)

func TestServiceWiresRunnerStatus(t *testing.T) {
	statuses := status.New(nil)
	service := New[string](context.Background(), statuses)
	value, err := service.StartShell(
		context.Background(), "s1",
		func() (string, error) { return "", errors.New("unexpected") },
		func(context.Context) (string, error) {
			if got := statuses.Get("s1").Type; got != "busy" {
				t.Fatalf("status during shell=%q", got)
			}
			return "done", nil
		},
		nil,
	)
	if err != nil || value != "done" || statuses.Get("s1").Type != "idle" {
		t.Fatalf("value=%q err=%v status=%#v", value, err, statuses.Get("s1"))
	}
}

func TestEnsureRunningAndBusyErrorDelegate(t *testing.T) {
	service := New[string](context.Background(), nil)
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = service.EnsureRunning(
			context.Background(), "s1",
			func() (string, error) { return "interrupted", nil },
			func(context.Context) (string, error) {
				close(started)
				<-release
				return "done", nil
			},
		)
	}()
	<-started
	err := service.AssertNotBusy("s1")
	var busy *runner.BusyError
	if !errors.As(err, &busy) || busy.SessionID != "s1" {
		t.Fatalf("busy error=%#v", err)
	}
	close(release)
	<-done
	if err := service.AssertNotBusy("s1"); err != nil {
		t.Fatalf("runner was not removed on idle: %v", err)
	}
}
