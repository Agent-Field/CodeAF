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
}

func newPairDoor(in io.Reader, out io.Writer) pairDoor {
	return pairDoor{term: newTerminal(in, out), mailbox: pairMailbox, grant: pairGrant, joining: pairJoining}
}

const pairUsage = "usage: codeaf pair [<code>] [--relay url] [--replace]"

func runPair(args []string) error {
	// ctrl+c is how a shown code is taken back, so it cancels the pairing rather
	// than killing the process: Offer deletes its mailbox on the way out.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return newPairDoor(os.Stdin, os.Stdout).run(ctx, args)
}

func (d pairDoor) run(ctx context.Context, args []string) error {
	flags := commandFlags("pair")
	relay := flags.String("relay", "", "the relay to pair through; empty is the relay this computer syncs through")
	replace := flags.Bool("replace", false, "when typing a code, give up this computer's own chats for the ones being shared")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	switch {
	case flags.NArg() == 0 && !*replace:
		return d.show(ctx, *relay)
	case flags.NArg() == 1:
		return d.join(ctx, *relay, flags.Arg(0), *replace)
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
