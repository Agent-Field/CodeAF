package main

// ── the door: codeaf devices ────────────────────────────────────────────────
//
// TWO KINDS OF DEVICE, ONE DOOR. A computer can hold the person's chats (it was
// paired with `codeaf pair`) and a device can be let in to use this machine (it
// was paired with `codeaf serve`). They are different powers with different
// undo, so they are listed under two headings, but a person who wants one gone
// types one verb, `codeaf devices revoke <name>`, and never asks which kind it
// was. Each kind is a [deviceKind]; the door only knows the interface, so a
// third kind is one more type and not one more branch.
//
// THE EMPTINESS LAW: a kind with nothing in it has no heading, and a machine
// with nothing of either kind says one sentence ([pair.NoDevices]).

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// deviceKind is one kind of device the door lists and stops.
type deviceKind interface {
	// table is the kind's heading and rows, or "" when it has none.
	table(now time.Time) (string, error)
	// find counts the devices of this kind that answer to name.
	find(name string) (int, error)
	// stop stops the one device of this kind that answers to name and returns
	// the sentence that says so.
	stop(name string) (string, error)
}

// allStopper is a kind that can stop every device answering to one name. Only
// the remote-access kind can: two laptops with one host name are common there,
// and each is a key the person can tell apart no other way.
type allStopper interface {
	stopAll(name string) (string, error)
}

// runDevices lists the devices of both kinds, or stops one.
//
// BOTH THE LIST AND THE STOPPING ANSWER ABOUT THE MACHINE THEY ARE TYPED ON.
// A remote device cannot list itself out of somebody's machine and cannot stop
// another device; a computer that holds the chats may stop another of the same
// person's, because the person owns both.
func runDevices(args []string) error {
	kinds := openDeviceKinds()
	switch {
	case len(args) > 0 && args[0] == "revoke":
		return stopDevice(kinds, args[1:])
	case askedForHelp(args):
		return commandHelp("devices")
	case len(args) > 0:
		return fmt.Errorf("usage: codeaf devices [revoke <name> [--all]]")
	}
	return listDevices(kinds, time.Now())
}

// revokeDevice stops one remote-access device, or every one answering to one
// name. It is the door's verb over the book alone, kept for callers that hold
// only a book.
func revokeDevice(book *pair.Book, args []string) error {
	return stopDevice([]deviceKind{bookKind{book}}, args)
}

// openDeviceKinds is the kinds this machine has: the computers that hold the
// chats only when sync is on and there is an identity, and always the devices
// let in to use this machine.
func openDeviceKinds() []deviceKind {
	kinds := []deviceKind{}
	if chats, ok := openChatsKind(); ok {
		kinds = append(kinds, chats)
	}
	return append(kinds, bookKind{pair.DeviceBook()})
}

// listDevices prints every kind's table, or the one sentence when none has rows.
func listDevices(kinds []deviceKind, now time.Time) error {
	var tables []string
	for _, kind := range kinds {
		table, err := kind.table(now)
		if err != nil {
			return err
		}
		if table != "" {
			tables = append(tables, table)
		}
	}
	if len(tables) == 0 {
		fmt.Println(pair.NoDevices)
		return nil
	}
	fmt.Print(strings.Join(tables, "\n"))
	return nil
}

// stopDevice reads `revoke <name> [--all]` and stops the device that answers to
// the name.
//
// --ALL IS A FLAG LIKE EVERY OTHER FLAG IN THIS BINARY. It used to be read by
// hand, and only when it was the FIRST word after `revoke`, so `codeaf devices
// revoke laptop --all` was refused — with a usage line that did not mention
// `--all` at all. A person taking back access to their own machine was told the
// wrong grammar for the gesture they had just typed correctly. Through
// [commandFlags] and [reorder] it is accepted in either position, printed by
// `codeaf devices revoke --help`, and named in the one usage table.
func stopDevice(kinds []deviceKind, args []string) error {
	flags := commandFlags("devices revoke")
	all := flags.Bool("all", false, "stop every device of that name that can use this machine, not just the one")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: codeaf devices revoke <name> [--all] — `codeaf devices` lists the names")
	}
	name := flags.Arg(0)
	said, err := stopByName(kinds, name, *all)
	if err != nil {
		return err
	}
	fmt.Println(said)
	return nil
}

// stopByName finds the one kind that has the name and stops it there. With
// --all the name is taken to mean the kind that can stop several at once.
func stopByName(kinds []deviceKind, name string, all bool) (string, error) {
	if all {
		return stopAllByName(kinds, name)
	}
	kind, err := kindOf(kinds, name)
	if err != nil {
		return "", err
	}
	return kind.stop(name)
}

func stopAllByName(kinds []deviceKind, name string) (string, error) {
	for _, kind := range kinds {
		if many, ok := kind.(allStopper); ok {
			return many.stopAll(name)
		}
	}
	return "", unknownDevice(name)
}

// kindOf is the one kind that has a device called name. A name in two kinds is
// as ambiguous as two in one, and is answered the same way: say so, and point at
// the list.
func kindOf(kinds []deviceKind, name string) (deviceKind, error) {
	var hits []deviceKind
	var failed error
	for _, kind := range kinds {
		n, err := kind.find(name)
		failed = cmp.Or(failed, err)
		if n > 0 {
			hits = append(hits, kind)
		}
	}
	switch {
	case len(hits) == 1:
		return hits[0], nil
	case len(hits) > 1:
		return nil, fmt.Errorf("more than one device is called %q, so that name cannot pick one — `codeaf devices` lists them", name)
	case failed != nil:
		return nil, failed
	}
	return nil, unknownDevice(name)
}

func unknownDevice(name string) error {
	return fmt.Errorf("no device is called %q — `codeaf devices` lists the ones there are", name)
}

// ── devices that can use this machine ───────────────────────────────────────

// bookKind is the devices this machine has let in (`codeaf serve`), and the
// machines this device can reach, which are listed with them because a person
// asking "what am I paired with" means both directions.
type bookKind struct{ book *pair.Book }

func (b bookKind) table(now time.Time) (string, error) {
	paired, err := b.book.Devices()
	if err != nil {
		return "", err
	}
	known, err := pair.Machines()
	if err != nil {
		return "", err
	}
	letIn, err := b.letIn(paired, now)
	return strings.TrimLeft(letIn+pair.MachinesList(known, now), "\n"), err
}

// letIn is the table of devices let in, or "" when there are none. The machine's
// own name and where its key is kept are only worth saying when there is a
// device they matter to.
func (bookKind) letIn(paired []pair.Paired, now time.Time) (string, error) {
	if len(paired) == 0 {
		return "", nil
	}
	device, err := pair.ThisDevice(pair.OpenKeeper())
	if err != nil {
		return "", err
	}
	return pair.DevicesList(device.Name(), paired, pair.OpenKeeper(), now), nil
}

func (b bookKind) find(name string) (int, error) {
	paired, err := b.book.Devices()
	return countCalled(paired, name, func(p pair.Paired) string { return p.Label }), err
}

func (b bookKind) stop(name string) (string, error) {
	gone, err := b.book.Revoke(name)
	if err != nil {
		return "", err
	}
	return pair.RevokedLine(gone.Label), nil
}

// stopAll answers a single device in the plain form's own sentence, because one
// device stopped is one device stopped whichever flag was typed.
func (b bookKind) stopAll(name string) (string, error) {
	count, err := b.book.RevokeAll(name)
	switch {
	case err != nil:
		return "", err
	case count == 1:
		return pair.RevokedLine(name), nil
	}
	return fmt.Sprintf("%d devices called %s have been stopped — each needs a new pairing code to come back.", count, name), nil
}

// countCalled counts the items whose label is name, ignoring case as the book does.
func countCalled[T any](items []T, name string, label func(T) string) int {
	n := 0
	for _, item := range items {
		if strings.EqualFold(label(item), name) {
			n++
		}
	}
	return n
}

// ── devices with your chats ─────────────────────────────────────────────────

// chatsKind is the computers that hold the person's chats: the Device records of
// their identity in the relay's directory. Nothing is cached, so a listing and a
// stop each see the directory as it is now.
type chatsKind struct {
	dir   directory.Client
	key   []byte // opens the sealed names
	self  string // this computer's device id
	relay string // named when the relay cannot be reached
}

// openChatsKind is the kind for this machine, and false when it has none: sync
// off, or no identity yet. Both are the emptiness law, not an error, so the
// heading is simply absent.
func openChatsKind() (chatsKind, bool) {
	s, ok, err := syncsetup.Open(home.Dir())
	if err != nil || !ok {
		return chatsKind{}, false
	}
	return chatsKindOf(s.Dir, s.Identity.CellKey(), s.Device.ID(), s.Relay), true
}

func chatsKindOf(dir directory.Client, cellKey []byte, self, relay string) chatsKind {
	return chatsKind{dir: dir, key: directory.MetadataKey(cellKey), self: self, relay: relay}
}

// chatsRow is one computer: what the table shows, and the id the directory knows
// it by.
type chatsRow struct {
	pair.ChatsDevice
	id string
}

// listRequestWithin bounds one request to the relay so a silent one cannot hang
// the command.
const listRequestWithin = 20 * time.Second

func (c chatsKind) rows() ([]chatsRow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), listRequestWithin)
	defer cancel()
	l, err := c.dir.List(ctx)
	if err != nil {
		return nil, c.explain(err)
	}
	open := func(sealed string) (string, error) { return directory.OpenName(c.key, sealed) }
	rows := make([]chatsRow, 0, len(l.Devices))
	for id, d := range l.Devices {
		rows = append(rows, chatsRow{
			ChatsDevice: pair.ChatsDevice{Name: chatlist.DeviceName(l.Devices, id, open), This: id == c.self, Stopped: d.Revoked},
			id:          id,
		})
	}
	slices.SortFunc(rows, func(a, b chatsRow) int { return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.id, b.id)) })
	return rows, nil
}

func (c chatsKind) table(time.Time) (string, error) {
	rows, err := c.rows()
	if err != nil {
		// The other kind is still worth showing when the relay is not answering.
		fmt.Fprintln(os.Stderr, "the devices with your chats cannot be listed now:", err)
		return "", nil
	}
	if len(rows) == 0 {
		return "", nil
	}
	shown := make([]pair.ChatsDevice, len(rows))
	for i, row := range rows {
		shown[i] = row.ChatsDevice
	}
	return pair.ChatsDevicesList(shown), nil
}

func (c chatsKind) called(name string) ([]chatsRow, error) {
	rows, err := c.rows()
	return slices.DeleteFunc(rows, func(r chatsRow) bool { return !strings.EqualFold(r.Name, name) }), err
}

func (c chatsKind) find(name string) (int, error) {
	rows, err := c.called(name)
	return len(rows), err
}

func (c chatsKind) stop(name string) (string, error) {
	rows, err := c.called(name)
	switch {
	case err != nil:
		return "", err
	case len(rows) > 1:
		return "", fmt.Errorf("more than one of your computers is called %q, so that name cannot pick one — `codeaf devices` lists them", name)
	case len(rows) == 0:
		return "", unknownDevice(name)
	case rows[0].Stopped:
		return rows[0].Name + " is already stopped.", nil
	}
	return c.revoke(rows[0])
}

func (c chatsKind) revoke(row chatsRow) (string, error) {
	if row.This {
		return "", errors.New("that is this computer — stop it from another of your computers, so it is not the one cutting itself off")
	}
	ctx, cancel := context.WithTimeout(context.Background(), listRequestWithin)
	defer cancel()
	if err := c.dir.Revoke(ctx, row.id); err != nil {
		return "", c.explain(err)
	}
	return pair.ChatsRevokedLine(row.Name), nil
}

// explain turns the directory's errors into the sentence a person reads.
func (c chatsKind) explain(err error) error {
	switch {
	case errors.Is(err, directory.ErrRevoked):
		return errors.New("this computer was stopped by another of your computers, so it can no longer reach your chats — run `codeaf pair` to bring it back")
	case errors.Is(err, wireauth.ErrSkew):
		return errors.New(chatlist.ClockOff)
	case errors.Is(err, directory.ErrUnreachable):
		return fmt.Errorf("the relay at %s cannot be reached from here — check this computer's network", c.relay)
	case errors.Is(err, directory.ErrNotFound):
		return errors.New("that device is not in your list any more — `codeaf devices` shows the ones there are")
	}
	return err
}
