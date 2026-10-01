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
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/pair"
)

// pairDoor is one run of `codeaf pair`: the terminal it talks on and the three
// things that say where a pairing goes, which are seams so a test can point two
// doors at two homes.
type pairDoor struct {
	term    *terminal
	mailbox func(relayFlag string) (pair.Mailbox, error)
	grant   func(pair.Mailbox) (pair.Grant, error)
	joining func(replace bool) pair.Joining

	// The three seams of pairing by link.
	requests func(pair.Mailbox) directory.Requests
	linking  func(route pair.Mailbox, replace bool) pair.LinkJoining
	approver func(pair.Mailbox) (pair.Approver, error)
}

func newPairDoor(in io.Reader, out io.Writer) pairDoor {
	return pairDoor{term: newTerminal(in, out), mailbox: pairMailbox, grant: pairGrant, joining: pairJoining,
		requests: linkRequests, linking: linkJoining(home.Dir()), approver: linkApprover(home.Dir())}
}

const pairUsage = "usage: codeaf pair [--replace] | codeaf pair approve <link-or-code> | codeaf pair <code> [--replace] | codeaf pair --code"

func runPair(args []string) error {
	// ctrl+c is how a shown code is taken back, so it cancels the pairing rather
	// than killing the process: Offer deletes its mailbox on the way out.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return newPairDoor(os.Stdin, os.Stdout).run(ctx, args)
}

func (d pairDoor) run(ctx context.Context, args []string) error {
	flags := commandFlags("pair")
	relay := flags.String("relay", "", "the sync address to pair through; empty is the one this computer syncs through")
	via := flags.String("via", "", "the same as --relay")
	code := flags.Bool("code", false, "show a six-digit code to share this computer's chats, instead of asking to join")
	replace := flags.Bool("replace", false, "when typing a code, give up this computer's own chats for the ones being shared")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	if *relay == "" {
		*relay = *via
	}
	return d.dispatch(ctx, flags.Args(), *relay, *replace, *code)
}

// dispatch picks the door by what was typed: nothing asks to join, `approve`
// and anything shaped like a link approve, and a six-digit code joins as before.
func (d pairDoor) dispatch(ctx context.Context, args []string, relay string, replace, code bool) error {
	switch {
	case code && len(args) == 0 && !replace:
		return d.show(ctx, relay)
	case len(args) == 0 && !code:
		return d.askToJoin(ctx, relay, replace)
	case len(args) == 2 && args[0] == "approve" && !code && !replace:
		return d.approve(ctx, relay, args[1])
	case len(args) == 1 && !code && pair.IsLinkText(args[0]):
		return d.approve(ctx, relay, args[0])
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
		return endedByPerson(ctx, err)
	}
	d.term.say(pair.PairedChatsLine(label))
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
	joined, err := pair.Join(ctx, route, d.joining(replace), typed, joinScreen{d.term})
	if err != nil {
		return endedByPerson(ctx, err)
	}
	d.term.say(joinedSentence(joined))
	return nil
}

// joinedSentence is how a finished join reads.
func joinedSentence(joined pair.Joined) string { return joined.Sentence() }

// endedByPerson turns the end a person chose with ctrl+c into a clean exit, and
// leaves every other failure as the sentence it already is.
func endedByPerson(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return nil
	}
	return err
}
