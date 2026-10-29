package main

// `codeaf pair`: share this computer's chats with another one, from a terminal.
//
//	one$ codeaf pair                      shows a code and waits
//	two$ codeaf pair 42-715-302           types it
//
// Both doors run internal/pair's Offer and Join and add nothing to them but a
// terminal to read and print on; the chat's `/pair` runs the same two functions
// on a screen. Every sentence a person reads is pair's own.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// pairDoor is one run of `codeaf pair`: the terminal it talks on and the three
// things that say where a pairing goes, which are seams so a test can point two
// doors at two homes.
type pairDoor struct {
	term    *terminal
	mailbox func(relayFlag string) (pair.Mailbox, error)
	grant   func(pair.Mailbox) (pair.Grant, error)
	joining func(replace bool) pair.Joining

	// asksName is true when a person is at the terminal and so can be asked what
	// this computer should be called while it joins.
	asksName bool
	name     string // A join keeps the requested name only after approval succeeds.

	// The three seams of pairing by link.
	requests func(pair.Mailbox) directory.Requests
	linking  func(route pair.Mailbox, replace bool) pair.LinkJoining
	approver func(pair.Mailbox) (pair.Approver, error)

	// ownRecord is the seam a finished pairing's own directory record goes
	// through — the same upsert every chat start makes. A seam like the rest,
	// so a test can point two doors of one process at two homes.
	ownRecord func(ctx context.Context, route pair.Mailbox) error
}

func newPairDoor(in io.Reader, out io.Writer) pairDoor {
	return pairDoor{term: newTerminal(in, out), asksName: in == io.Reader(os.Stdin) && stdinIsTerminal(os.Stdin), mailbox: pairMailbox, grant: pairGrant, joining: pairJoining,
		requests: linkRequests, linking: linkJoining(home.Dir()), approver: linkApprover(home.Dir()),
		ownRecord: func(ctx context.Context, route pair.Mailbox) error { return ownDeviceRecord(ctx, home.Dir(), route) }}
}

const pairUsage = "usage: codeaf pair [--name <name>] | codeaf pair approve <link-or-code> | codeaf pair <code> [--replace] [--name <name>] | codeaf pair --code"

// approveUsage is what `codeaf pair approve` with nothing after it is told.
const approveUsage = "usage: codeaf pair approve <link-or-code> \u2014 the link or code the new device shows"

// ownDeviceRecord writes dir's own directory record at the relay a pairing
// just went through — the same upsert every chat start makes
// (syncsetup.PutOwnDevice), so a device that finished a pairing is on its own
// list at once and not on the next chat start.
func ownDeviceRecord(ctx context.Context, dir string, route pair.Mailbox) error {
	client, id, err := homeDirectory(dir, route)
	if err != nil {
		return err
	}
	dev, err := identity.Device(dir)
	if err != nil {
		return err
	}
	return syncsetup.PutOwnDevice(ctx, client, id, dev, devname.Name(dir))
}

// recordOwnDeviceOrSay is what a finished pairing does about its record: the
// pairing itself succeeded, so a record that did not land is one line on the
// screen and a clean exit — the next chat start writes the record anyway.
func (d pairDoor) recordOwnDeviceOrSay(ctx context.Context, route pair.Mailbox) {
	if d.ownRecord == nil {
		return
	}
	if err := d.ownRecord(ctx, route); err != nil {
		d.term.say(pair.RecordUnsaidLine(err))
	}
}

func runPair(args []string) error {
	// ctrl+c is how a shown code is taken back, so it cancels the pairing rather
	// than killing the process: Offer deletes its mailbox on the way out.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return newPairDoor(os.Stdin, os.Stdout).run(ctx, args)
}

func (d pairDoor) run(ctx context.Context, args []string) error {
	// ASKING FOR HELP IS ANSWERED WHERE PAIRING CANNOT RUN. `pair` is on the help
	// page, so a build with no compatible engine,
	// still explains the command; only the pairing itself is refused.
	if reason := cell.UnavailableReason(); reason != "" && !asksForHelp(args) {
		return errors.New(reason)
	}
	if reason := cell.UnavailableReason(); reason != "" && asksForHelp(args) {
		fmt.Fprintln(usageOut, reason)
	}
	flags := commandFlags("pair")
	via := flags.String("via", "", "the sync address to pair through; empty is the one this device already uses")
	renamedFlag(flags, "relay", "via")
	code := flags.Bool("code", false, "show a six-digit code that shares this device's chats, instead of asking to join")
	replace := flags.Bool("replace", false, "when typing a code, replace this device's own chats with the shared ones")
	name := flags.String("name", "", "what your devices call this one; empty keeps its current name, which starts as the host name")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	if err := d.naming(ctx, *name, flags.Args(), *code); err != nil {
		return err
	}
	return explainSyncUnreachable(d.dispatch(ctx, flags.Args(), *via, *replace, *code), *via)
}

// naming settles the label shown during approval. A join keeps the choice in
// memory until it succeeds, so refusal changes no files. A bad --name stops
// pairing before the relay is looked up. Without one,
// a person at a terminal who has never named this computer is shown the host name
// and may keep it with Enter or type another. The device that shows a code or
// approves already has a name, so only a join asks.
func (d *pairDoor) naming(ctx context.Context, typed string, args []string, showing bool) error {
	if typed != "" {
		name, err := devname.Clean(typed)
		if err != nil {
			return err
		}
		if !showing && joins(args) {
			d.name = name
			return nil
		}
		_, err = devname.Set(home.Dir(), name)
		return err
	}
	if !d.asksName || showing || !joins(args) {
		return nil
	}
	if _, named := devname.Chosen(home.Dir()); named {
		return nil
	}
	return d.askName(ctx)
}

// joins says the arguments are a join: a bare `codeaf pair` or a typed code,
// and not `approve`, a link or a shown code.
func joins(args []string) bool {
	return len(args) == 0 || (len(args) == 1 && !pair.IsLinkText(args[0]))
}

// askName asks until a name is kept or the person leaves it as it is.
func (d *pairDoor) askName(ctx context.Context) error {
	for {
		fmt.Fprint(d.term.out, pair.NamePrompt(devname.Name(home.Dir()))+" ")
		typed, ok := d.term.lines.next(ctx)
		if !ok || strings.TrimSpace(typed) == "" {
			fmt.Fprintln(d.term.out)
			return nil
		}
		name, err := devname.Clean(typed)
		if err == nil {
			d.name = name
			return nil
		}
		d.term.say(pair.NameTried(err))
	}
}

// dispatch picks the door by what was typed: nothing asks to join, `approve`
// and anything shaped like a link approve, and a six-digit code joins as before.
func (d pairDoor) dispatch(ctx context.Context, args []string, relay string, replace, code bool) error {
	switch {
	case code && len(args) == 0 && !replace:
		return d.show(ctx, relay)
	case len(args) == 0 && !code:
		return d.askToJoin(ctx, relay, replace)
	case len(args) == 1 && args[0] == "approve":
		return wrongCall(approveUsage)
	case len(args) == 2 && args[0] == "approve" && !code && !replace:
		return d.withApprovalRecord().approve(ctx, relay, args[1])
	case len(args) == 1 && !code && pair.IsLinkText(args[0]):
		return d.withApprovalRecord().approve(ctx, relay, args[0])
	case len(args) == 1 && !code:
		return d.join(ctx, relay, args[0], replace)
	}
	return wrongCall(pairUsage)
}

// show is the device that has the chats: a code on screen until one device has
// typed it and a person has said yes.
func (d pairDoor) show(ctx context.Context, relay string) error {
	route, err := d.mailbox(relay)
	if err != nil {
		return err
	}
	grant, err := d.grant(route)
	if err != nil {
		return err
	}
	label, err := pair.Offer(ctx, route, grant, offerScreen{d.term})
	if err != nil {
		return d.endedByPerson(ctx, err)
	}
	d.term.say(pair.PairedChatsLine(label))
	d.recordOwnDeviceOrSay(ctx, route)
	return nil
}

// join is the device that gets them. A code that cannot be a code is refused
// before the relay is even looked up, so a typo costs no request.
func (d pairDoor) join(ctx context.Context, relay, typed string, replace bool) error {
	if _, err := pair.ReadJoinCode(typed); err != nil {
		return err
	}
	route, err := d.mailbox(relay)
	if err != nil {
		return err
	}
	joining := d.joining(replace)
	if d.name != "" {
		joining.Label = d.name
	}
	joined, err := pair.Join(ctx, route, joining, typed, joinScreen{d.term})
	if err != nil {
		return d.endedByPerson(ctx, err)
	}
	if !joined.Already && d.name != "" {
		if _, err := devname.Set(joining.Home, d.name); err != nil {
			return err
		}
	}
	d.term.say(joinedSentence(joined))
	if !joined.Already {
		d.recordOwnDeviceOrSay(ctx, route)
	}
	return nil
}

// joinedSentence is how a finished join reads.
func joinedSentence(joined pair.Joined) string { return joined.Sentence() }

// endedByPerson turns the end a person chose with ctrl+c into a clean exit that
// says nothing was paired, and leaves every other failure as the sentence it already is.
func (d pairDoor) endedByPerson(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		// The terminal echoes ^C on the prompt's own line, so start a fresh one.
		fmt.Fprintln(d.term.out)
		d.term.say(pair.CancelledLine)
		return nil
	}
	return err
}

// asksForHelp reports whether args ask a command for its help page, in any of
// the spellings the flag package answers with its usage.
func asksForHelp(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "-h", "-help", "--help":
			return true
		}
	}
	return false
}
