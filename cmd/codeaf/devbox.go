package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/devbox"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func runDevbox(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return devboxCommand(ctx, args, newDevboxFlow())
}

func devboxCommand(ctx context.Context, args []string, flow devbox.Flow) error {
	if len(args) == 0 {
		return wrongCall("usage: codeaf devbox up <name> | status [<name>] | down <name>")
	}
	if askedForHelp(args[:1]) {
		return commandHelp("devbox")
	}
	action := args[0]
	if action != "up" && action != "status" && action != "down" {
		return wrongCall("usage: codeaf devbox up <name> | status [<name>] | down <name>")
	}
	var o devbox.Options
	flags := devboxFlags("devbox "+action, action == "up", &o)
	if err := parseCommandFlags(flags, reorder(flags, args[1:])); err != nil {
		return err
	}
	if flags.NArg() > 1 || (action != "status" && flags.NArg() != 1) {
		return wrongCall("usage: codeaf devbox " + action + " <name>")
	}
	if flags.NArg() == 1 {
		o.Name = flags.Arg(0)
	}
	if action == "up" {
		o.Version = devboxVersion(buildinfo.Revision(), o.Channel)
	}
	err := flow.Run(ctx, action, o)
	if err != nil && (err.Error() == devbox.TokenNeeded || err.Error() == devbox.TerraformNeeded) {
		fmt.Fprintln(usageErr, err)
		return exitStatus(1)
	}
	return err
}

// The root row stays compact; this page carries the full action and flag reference.
const devboxUsage = `  codeaf devbox up <name> [--yes] [--dir D] [--region R]
              [--workspace W] [--channel C]
  codeaf devbox down <name> [--yes] [--dir D]
  codeaf devbox status [<name>] [--dir D]`

func devboxFlags(name string, up bool, o *devbox.Options) *flag.FlagSet {
	flags := commandFlags(name)
	flags.StringVar(&o.Dir, "dir", "", "folder holding this box's Terraform; default is <codeaf home>/devbox/<name>")
	flags.BoolVar(&o.Yes, "yes", false, "skip the create or destroy confirmation")
	if up {
		flags.StringVar(&o.Region, "region", "", "the region the box runs in; empty picks one for you")
		flags.StringVar(&o.Workspace, "workspace", "", "the workspace on your account, by name or id; needed when you have several")
		flags.StringVar(&o.Channel, "channel", devboxChannel(buildinfo.Revision()), "stable, staging or dev; follows this binary, with dev for a source build")
	}
	return flags
}

func devboxChannel(revision string) string {
	switch channel := buildinfo.Channel(revision); channel {
	case "stable", "staging", "dev":
		return channel
	case "rc":
		return "stable"
	default:
		return "dev"
	}
}

func devboxVersion(revision, channel string) string {
	if buildinfo.Channel(revision) == channel {
		if _, _, err := devbox.Channel(channel); err == nil {
			return revision
		}
	}
	return ""
}

func newDevboxFlow() devbox.Flow {
	door := newPairDoor(os.Stdin, os.Stdout)
	return devbox.Flow{
		Home: home.Dir(), In: os.Stdin, Out: os.Stdout, Err: os.Stderr, Interactive: stdinIsTerminal(os.Stdin), StdoutTerminal: stdinIsTerminal(os.Stdout),
		Unavailable: func() string {
			if reason := cell.UnavailableReason(); reason != "" {
				return reason
			}
			_, err := door.mailbox("")
			if err != nil {
				return err.Error()
			}
			return ""
		},
		Prepare: func(ctx context.Context, name string) (devbox.Offer, error) { return prepareDevbox(door, name) },
		Pair: func(ctx context.Context, name string, ready func(devbox.Offer)) error {
			return offerDevbox(ctx, door, name, ready)
		},
		Devices:    devboxDevices,
		Probe:      probeDevbox,
		Remove:     devboxRemoval(openChatsKind, pair.MachineBook()),
		ShareState: shareDevboxState,
	}
}

// devboxShareWithin bounds the one home-state round up runs for a new box, so
// a slow relay cannot hold the command after the box already answers.
const devboxShareWithin = 30 * time.Second

// shareDevboxState runs the round an open terminal runs: it pushes this
// computer's saved keys and settings to the identity the box just joined, and
// the box's serve pulls them on its next round.
func shareDevboxState(ctx context.Context) error {
	s, ok, err := syncsetup.Open(home.Dir())
	if err != nil || !ok {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, devboxShareWithin)
	defer cancel()
	return s.SyncState(ctx)
}

func devboxDevices(ctx context.Context) ([]devbox.Device, error) {
	known, err := pair.Machines()
	if err != nil {
		return nil, err
	}
	remembered := make([]devbox.Device, 0, len(known))
	for _, one := range known {
		name := one.Name
		if one.Alias != "" {
			name = one.Alias
		}
		remembered = append(remembered, devbox.Device{Name: name})
	}
	if syncsetup.Quiet(home.Dir()) {
		return remembered, nil
	}
	s, ok, err := syncsetup.Open(home.Dir())
	if err != nil {
		return nil, err
	}
	if !ok {
		return remembered, nil
	}
	kind := chatsKindOf(s.Dir, s.Identity.CellKey(), s.Device.ID(), s.Relay)
	ctx, cancel := context.WithTimeout(ctx, listRequestWithin)
	defer cancel()
	listing, err := kind.dir.List(ctx)
	if err != nil {
		return nil, kind.explain(err)
	}
	presence, presenceErr := kind.dir.Presence(ctx)
	result := make([]devbox.Device, 0, len(listing.Devices))
	for id, record := range listing.Devices {
		name, err := directory.OpenName(kind.key, record.Name)
		if err != nil {
			if record.Revoked {
				continue
			}
			return nil, errors.New("could not read a device name from this identity's list")
		}
		d := devbox.Device{ID: id, Name: name, Removed: record.Revoked}
		seen := record.LastSeen
		if presenceErr == nil {
			if p, ok := presence.Devices[id]; ok {
				online := p.Online
				d.Online = &online
				seen = p.LastSeen
			}
		}
		if seen > 0 {
			d.LastSeen = time.UnixMilli(seen)
		}
		result = append(result, d)
	}
	return append(result, remembered...), nil
}

// Pin before offering the first code so even a lost acknowledgement can be probed.
func prepareDevbox(door pairDoor, name string) (devbox.Offer, error) {
	route, err := door.mailbox("")
	if err != nil {
		return devbox.Offer{}, err
	}
	client, err := pair.ThisDevice(pair.OpenKeeper())
	if err != nil {
		return devbox.Offer{}, err
	}
	key, devices, known, err := pair.DevboxRemote(name, route.URL, client, pair.ThisMachineLabel())
	if err != nil {
		return devbox.Offer{}, err
	}
	if err := pair.MachineBook().Remember(known); err != nil {
		return devbox.Offer{}, err
	}
	return devbox.Offer{Via: route.URL, RemoteKey: base64.StdEncoding.EncodeToString(key), RemoteDevices: base64.StdEncoding.EncodeToString(devices)}, nil
}

// Select once, ignoring revoked names; a saved id continues to identify a renamed box.
func devboxRemoval(open func() (chatsKind, bool), book *pair.Book) func(context.Context, string, string) (bool, error) {
	return func(ctx context.Context, name, id string) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		kind, ok := open()
		if !ok {
			return false, errors.New("could not open this identity's device list")
		}
		rows, err := kind.rows()
		if err != nil {
			return false, err
		}
		var selected *chatsRow
		if id != "" {
			for _, row := range rows {
				if !row.Stopped && row.id == id {
					copy := row
					selected = &copy
					break
				}
			}
		}
		if selected == nil {
			for _, row := range rows {
				if !row.Stopped && strings.EqualFold(row.Name, name) {
					if selected != nil {
						return false, fmt.Errorf("more than one active device is called %q", name)
					}
					copy := row
					selected = &copy
				}
			}
		}
		if selected != nil {
			if _, err := kind.revoke(*selected); err != nil {
				return false, err
			}
		}
		if err := book.ForgetAlias(name); err != nil {
			return false, err
		}
		return selected != nil, nil
	}
}

func offerDevbox(ctx context.Context, door pairDoor, name string, ready func(devbox.Offer)) error {
	route, err := door.mailbox("")
	if err != nil {
		return err
	}
	grant, err := door.grant(route)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ui := devboxOfferUI{name: name, offer: devbox.Offer{Via: route.URL}, ready: ready, cancel: cancel}
	label, err := pair.Offer(ctx, route, grant, &ui)
	if err != nil {
		if errors.Is(err, pair.ErrDidNotFinish) || errors.Is(err, context.DeadlineExceeded) {
			return devbox.ErrJoinInterrupted
		}
		return err
	}
	if label != name {
		return fmt.Errorf("a different device joined: %s", label)
	}
	door.recordOwnDeviceOrSay(ctx, route)
	return nil
}

// The command authorizes this one named box to receive the full identity.
// A burned code cancels rather than silently changing the code already in a plan.
type devboxOfferUI struct {
	name   string
	offer  devbox.Offer
	ready  func(devbox.Offer)
	cancel context.CancelFunc
	shown  bool
}

func (u *devboxOfferUI) Show(code *pair.Code, _ string) {
	if !u.shown {
		u.shown = true
		u.offer.Code = code.Shown()
		u.ready(u.offer)
	}
}
func (u *devboxOfferUI) Burned() { u.cancel() }
func (u *devboxOfferUI) Ask(ctx context.Context, label, _ string) bool {
	return ctx.Err() == nil && label == u.name
}

// The Noise handshake proves the configured key answered, including on released boxes without a watch.
func probeDevbox(ctx context.Context, name string) bool {
	book := pair.MachineBook()
	known, found, err := book.Machine(name)
	if err != nil || !found || known.Alias != name {
		return false
	}
	client, err := pair.ThisDevice(pair.OpenKeeper())
	if err != nil {
		return false
	}
	tunnel, err := (pair.Reach{Name: name, Device: client, Machines: book, Label: pair.ThisMachineLabel()}).Open(ctx)
	if err != nil {
		return false
	}
	_ = tunnel.Close()
	return true
}
