package main

// The chat's door onto pairing: what `/pair` and `/pair <code>` reach through
// tui3.Pairing. It pairs THIS computer, the one at the keyboard, on every door
// the chat opens through, because that is the computer a person means by "share
// my chats" whether the conversation on screen runs here or over --at or --host.
// The relay, the identity and the joining side's home come from the same three
// helpers `codeaf pair` uses, so the two doors cannot disagree about where a
// code goes.

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// chatPairDoor is the surface's tui3.Pairing.
type chatPairDoor struct{}

var _ tui3.Pairing = chatPairDoor{}

// Offer shows a code and shares this computer's chats with the device that types
// it. Every error it returns is already the sentence a person reads.
func (chatPairDoor) Offer(ctx context.Context, ui pair.OfferUI) (string, error) {
	route, err := pairMailbox("")
	if err != nil {
		return "", err
	}
	grant, err := pairGrant(route)
	if err != nil {
		return "", err
	}
	return pair.Offer(ctx, route, grant, ui)
}

// Join types another computer's code and takes its chats, unless this computer
// already has chats of its own: replacing them is a flag of the terminal command
// and never a keystroke in a chat.
func (chatPairDoor) Join(ctx context.Context, typed string, ui pair.JoinUI) (pair.Joined, error) {
	route, err := pairMailbox("")
	if err != nil {
		return pair.Joined{}, err
	}
	return pair.Join(ctx, route, pairJoining(false), typed, ui)
}
