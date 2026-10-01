package main

// `codeaf pair` by link: a new device asks to join and shows a link; a device
// that is already in approves it with `codeaf pair approve <link>`.
//
//	new$   codeaf pair                       shows a link and waits
//	old$   codeaf pair approve k7m2q9xd.Qm9v  shows who is asking, asks y or n
//
// Both doors run internal/pair's JoinByLink and Approver. The app's approve
// screen calls the same Approver, so the two cannot disagree.

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net/http"
	"runtime"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// linkTimeout outlasts one held poll so the answer, not the client, ends a wait.
const linkTimeout = directory.MaxWait + 10*time.Second

// linkRequests is the open side of the directory at a route.
func linkRequests(route pair.Mailbox) directory.Requests {
	return directory.NewLinkHTTP(route.URL, &http.Client{Timeout: linkTimeout})
}

// homeDirectory is this computer's signed directory at a route, and the identity
// it signs for.
func homeDirectory(dir string, route pair.Mailbox) (directory.Client, identity.Identity, error) {
	id, err := identity.Load(dir)
	if errors.Is(err, identity.ErrNone) {
		return nil, id, pair.ErrNotJoined
	}
	if err != nil {
		return nil, id, err
	}
	dev, err := identity.Device(dir)
	if err != nil {
		return nil, id, err
	}
	sign := reqsign.SignFor(homeSigner{id, dev}, time.Now)
	return directory.NewHTTP(route.URL, sign, &http.Client{Timeout: linkTimeout}), id, nil
}

type homeSigner struct {
	id  identity.Identity
	dev identity.Dev
}

func (s homeSigner) IdentityKey() ed25519.PublicKey { return s.id.PublicKey() }
func (s homeSigner) Cert() identity.Cert            { return s.dev.Cert }
func (s homeSigner) Sign(msg []byte) []byte         { return s.dev.Sign(msg) }

// pullFleet counts what the identity holds, as this computer now sees it.
func pullFleet(dir string, route pair.Mailbox) func(context.Context) (pair.Fleet, error) {
	return func(ctx context.Context) (pair.Fleet, error) {
		client, _, err := homeDirectory(dir, route)
		if err != nil {
			return pair.Fleet{}, err
		}
		listing, err := client.List(ctx)
		return fleetOf(listing), err
	}
}

// fleetOf counts the devices that are still in and the workspaces that are still open.
func fleetOf(l directory.Listing) pair.Fleet {
	var f pair.Fleet
	for _, d := range l.Devices {
		if !d.Revoked {
			f.Devices++
		}
	}
	for _, c := range l.Cells {
		if !c.Archived {
			f.Workspaces++
		}
	}
	return f
}

// linkJoining is this computer as the device that asks to join.
func linkJoining(dir string) func(pair.Mailbox, bool) pair.LinkJoining {
	return func(route pair.Mailbox, replace bool) pair.LinkJoining {
		return pair.LinkJoining{
			Joining: pairJoiningAt(dir)(replace), Platform: runtime.GOOS, Host: route.Host, Via: route.Shown,
			Pull: pullFleet(dir, route),
		}
	}
}

// inviteScreen is the new device's terminal.
type inviteScreen struct{ *terminal }

func (s inviteScreen) Invited(in pair.Invite) { s.say(pair.InviteLines(in)) }

// askToJoin is the new device: a link on screen until a device that is in says yes.
func (d pairDoor) askToJoin(ctx context.Context, relay string, replace bool) error {
	route, err := d.mailbox(relay)
	if err != nil {
		return err
	}
	joined, err := pair.JoinByLink(ctx, d.requests(route), d.linking(route, replace), inviteScreen{d.term})
	if err != nil && !errors.Is(err, pair.ErrPairedNotRead) {
		return endedByPerson(ctx, err)
	}
	if err != nil {
		d.term.say(err.Error())
		return nil
	}
	d.term.say(pair.JoinedFleetLine(joined.Fleet.Workspaces))
	return nil
}

// approve is a device that is in: it reads the request, shows who is asking and
// the check number, and lets them in on a yes. Anything but a yes turns them down.
func (d pairDoor) approve(ctx context.Context, relay, typed string) error {
	ref, err := pair.ReadLink(typed)
	if err != nil {
		return err
	}
	route, err := d.mailbox(relay)
	if err != nil {
		return err
	}
	who, err := d.approver(route)
	if err != nil {
		return err
	}
	asking, err := who.Look(ctx, ref)
	if err != nil {
		return err
	}
	d.term.say(pair.WantsToJoinLine(asking.Name, asking.Platform))
	if !d.term.confirm(ctx, pair.CheckQuestion(asking.Check)) {
		return d.decline(ctx, who, asking)
	}
	if err := who.Approve(ctx, asking); err != nil {
		return err
	}
	d.term.say(pair.ApprovedLine(asking.Name))
	return nil
}

func (d pairDoor) decline(ctx context.Context, who pair.Approver, asking pair.Asking) error {
	if ctx.Err() != nil {
		return nil
	}
	if err := who.Deny(ctx, asking); err != nil {
		return err
	}
	d.term.say(pair.DeclinedLine)
	return nil
}

// linkApprover is this computer as the device that approves.
func linkApprover(dir string) func(pair.Mailbox) (pair.Approver, error) {
	return func(route pair.Mailbox) (pair.Approver, error) {
		client, id, err := homeDirectory(dir, route)
		if err != nil {
			return pair.Approver{}, err
		}
		return pair.Approver{
			Requests: linkRequests(route), Dir: client, Identity: id,
			SyncURL: syncsetup.Named(route.URL), Replaces: identity.Predecessors(dir),
		}, nil
	}
}
