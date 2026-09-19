package tui3_test

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/tui3"
)

type execAdapter struct{}

func (execAdapter) LaunchState(context.Context, string) ([]tui3.ExecWork, error) {
	return nil, nil
}
func (execAdapter) PauseCoordination(context.Context, string) error { return nil }
func (execAdapter) StopWork(context.Context, string) error          { return nil }

var _ tui3.Exec = execAdapter{}

func TestLaunchInterfaceIsImplementableOutsideThePackage(t *testing.T) {
	var door tui3.Exec = execAdapter{}
	works, err := door.LaunchState(context.Background(), "aaaa000000000001")
	if err != nil || works != nil {
		t.Fatalf("empty adapter LaunchState: %+v err=%v", works, err)
	}
	if err := door.PauseCoordination(context.Background(), "aaaa000000000001"); err != nil {
		t.Fatal(err)
	}
	if err := door.StopWork(context.Background(), "w-readme"); err != nil {
		t.Fatal(err)
	}
}
