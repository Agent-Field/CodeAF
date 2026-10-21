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
	"errors"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
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

// Join types another computer's code and takes its chats. A computer with
// chats of its own refuses, and the refusal names the in-chat way out: the
// same code typed again with --replace, which asks before it replaces
// (pair.JoinReplacing below). The terminal's own --replace wording names its
// own flag, so the door says the chat's here rather than retyping internal/pair's.
func (chatPairDoor) Join(ctx context.Context, typed string, ui pair.JoinUI) (pair.Joined, error) {
	route, err := pairMailbox("")
	if err != nil {
		return pair.Joined{}, err
	}
	joined, err := pair.Join(ctx, route, pairJoining(false), typed, ui)
	return joined, chatJoinRefusal(err)
}

// chatJoinRefusal says the plain refusal the chat's way: the terminal's own
// sentence names its --replace flag, and here the way out is the same code
// typed in the chat the person is already sitting in.
func chatJoinRefusal(err error) error {
	if errors.Is(err, pair.ErrDifferentChats) {
		return errChatReplaceRefusal
	}
	return err
}

// JoinReplacing is Join with this computer's own chats given up — the
// terminal's --replace, typed in chat. It runs only behind the surface's
// confirmation (internal/tui3/pair.go's joinWork), so a person has said an
// explicit yes before anything is touched.
func (chatPairDoor) JoinReplacing(ctx context.Context, typed string, ui pair.JoinUI) (pair.Joined, error) {
	route, err := pairMailbox("")
	if err != nil {
		return pair.Joined{}, err
	}
	return pair.Join(ctx, route, pairJoining(true), typed, ui)
}

// HasOwnChats reports whether this home holds an identity of its own — the
// fact a plain Join refuses on, and the reason a replace asks first.
func (chatPairDoor) HasOwnChats() bool {
	_, err := identity.Load(home.Dir())
	return err == nil
}

// errChatReplaceRefusal is the plain refusal, said the chat's way: the way out
// is a keystroke in the chat the person is already sitting in.
var errChatReplaceRefusal = errors.New("this computer already has chats of its own, so they stay local and nothing was changed; to take the other computer's chats instead, type /pair <code> --replace and confirm")
