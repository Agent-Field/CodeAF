package tui3

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
)

type deadlineChatListing struct{}

func (deadlineChatListing) Rows(context.Context) ([]chatlist.Row, error) {
	return nil, fmt.Errorf("%w: %w", directory.ErrUnreachable, context.DeadlineExceeded)
}

// Contract J22 describes an expired ownership read as a deadline on Home.
func TestHomeOwnershipDeadlineDoesNotClaimTheNetworkIsUnreachable(t *testing.T) {
	a := machinesHome(t, deadlineChatListing{}, 200)
	frame := homeText(a)
	if !strings.Contains(frame, "other machines did not answer in time") || strings.Contains(frame, "other machines unreachable") {
		t.Fatalf("ownership read deadline was misreported:\n%s", frame)
	}
}
