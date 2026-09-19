package tui3_test

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/tui3"
)

type collabAdapter struct{}

func (collabAdapter) Mark(context.Context, string) error                { return nil }
func (collabAdapter) Unmark(context.Context, string) error              { return nil }
func (collabAdapter) Marked(context.Context) ([]tui3.CollabMark, error) { return nil, nil }
func (collabAdapter) CoordinateMarked(context.Context, string) error    { return nil }
func (collabAdapter) Activity(context.Context, string) ([]tui3.CollabActivity, error) {
	return nil, nil
}
func (collabAdapter) Participants(context.Context, string) ([]tui3.CollabParticipant, error) {
	return nil, nil
}

var _ tui3.Collab = collabAdapter{}

func TestCoordinateInterfaceIsImplementableOutsideThePackage(t *testing.T) {
	var collab tui3.Collab = collabAdapter{}
	if err := collab.Mark(context.Background(), "aaaa000000000001"); err != nil {
		t.Fatal(err)
	}
	marks, err := collab.Marked(context.Background())
	if err != nil || marks != nil {
		t.Fatalf("empty adapter Marked: %+v err=%v", marks, err)
	}
	acts, err := collab.Activity(context.Background(), "aaaa000000000001")
	if err != nil || acts != nil {
		t.Fatalf("empty adapter Activity: %+v err=%v", acts, err)
	}
	parts, err := collab.Participants(context.Background(), "aaaa000000000001")
	if err != nil || parts != nil {
		t.Fatalf("empty adapter Participants: %+v err=%v", parts, err)
	}
}
