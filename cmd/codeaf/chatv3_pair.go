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

	"github.com/Agent-Field/codeaf/internal/config"
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
	record := func(ctx context.Context) error { return ownDeviceRecord(ctx, home.Dir(), route) }
	if grant, err = recordWithGrant(ctx, home.Dir(), grant, record); err != nil {
		return "", explainSyncUnreachable(err, route.URL)
	}
	label, err := pair.Offer(ctx, route, grant, chatOfferUI{OfferUI: ui, service: route.Shown})
	return label, explainSyncUnreachable(err, route.URL)
}

// Join pairs this computer; submitted chats require explicit replacement.
func (chatPairDoor) Join(ctx context.Context, typed string, ui pair.JoinUI) (pair.Joined, error) {
	route, err := pairMailbox("")
	if err != nil {
		return pair.Joined{}, err
	}
	joining, err := chatJoining(ctx, route, typed, false)
	if err != nil {
		return pair.Joined{}, err
	}
	joined, err := pair.Join(ctx, route, joining, typed, ui)
	if err == nil {
		if f, e := pullFleet(home.Dir(), route)(ctx); e == nil {
			joined.With = f.With
		}
	}
	return joined, explainSyncUnreachable(err, route.URL)
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
	joining, err := chatJoining(ctx, route, typed, true)
	if err != nil {
		return pair.Joined{}, err
	}
	joined, err := pair.Join(ctx, route, joining, typed, ui)
	if err == nil {
		err = ownDeviceRecord(ctx, home.Dir(), route)
	}
	return joined, explainSyncUnreachable(err, route.URL)
}

// chatJoining is this computer as the chat's joining side, after the check the
// terminal makes before a code is used: a computer whose chats already sync
// with others is refused, even when replacing was confirmed, and one whose
// identity no other device holds refuses its own chats before the other
// computer is asked. A typo is refused first and costs no request. A plain join
// whose directory cannot be read leaves the refusals to after the approval; a
// replacing one stops, as the terminal does, because nothing after the
// approval would refuse it.
func chatJoining(ctx context.Context, route pair.Mailbox, typed string, replace bool) (pair.Joining, error) {
	j := pairJoining(replace)
	if _, err := pair.ReadJoinCode(typed); err != nil {
		return j, err
	}
	standing, err := joinStanding(ctx, j.Home, func() (pair.Mailbox, error) { return route, nil })
	if err != nil && replace {
		return j, explainSyncUnreachable(err, route.URL)
	}
	if err := standing.refusal(); err != nil {
		return j, err
	}
	j.Alone = standing.alone()
	return j, nil
}

// BeforeJoin refuses, before the panel asks anything, a code that cannot be
// one or a computer that is paired with others: replacing would set aside chats
// that sync elsewhere, so such a computer is never asked to confirm it.
func (chatPairDoor) BeforeJoin(ctx context.Context, typed string, replace bool) error {
	route, err := pairMailbox("")
	if err != nil {
		return err
	}
	_, err = chatJoining(ctx, route, typed, replace)
	return err
}

// HasOwnChats reports whether this home holds an identity of its own — the
// reason an explicit replace asks first.
func (chatPairDoor) HasOwnChats() bool {
	_, err := identity.Load(home.Dir())
	return err == nil
}

// PairCommand is read when instructions are drawn, so a saved --via is reflected immediately.
func (chatPairDoor) PairCommand() string { return pair.Command(pairingRelay(home.Dir()).URL, "") }

// chatOfferUI gives the chat's link field its own directions while preserving
// every approval and cancellation operation of the ordinary offer.
type chatOfferUI struct {
	pair.OfferUI
	service string
}

func (u chatOfferUI) Show(code *pair.Code, _ string) {
	u.OfferUI.Show(code, pair.ChatCardLines(code, u.service))
}

// HasTeams checks this computer's profile even while a hosted chat is open.
func (chatPairDoor) HasTeams() bool { return pair.HasTeams(config.ProfileDir()) }
