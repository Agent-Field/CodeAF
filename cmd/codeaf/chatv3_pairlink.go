package main

// The chat's add-another-machine card asks to be let in by link through this
// door: the same JoinByLink that `codeaf pair` runs, with this computer's home.
// It never replaces chats a computer already has; that stays a flag of the
// terminal command.

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

var _ tui3.LivePairLinker = chatPairDoor{}

// PairByLink shows a link through ui and waits for a device that is in to say yes.
func (chatPairDoor) PairByLink(ctx context.Context, ui pair.LinkUI) (pair.LinkJoined, error) {
	route, err := pairMailbox("")
	if err != nil {
		return pair.LinkJoined{}, err
	}
	return pair.JoinByLink(ctx, linkRequests(route), linkJoining(home.Dir())(route, false), ui)
}
