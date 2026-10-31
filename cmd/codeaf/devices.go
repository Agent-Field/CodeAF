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
// A kind with no remote devices has no heading. Before pairing, the local
// computer's own row is followed by the pairing next step.

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/syncwords"
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
	if len(args) == 0 {
		defer terminalSyncProgress(syncsetup.Resolve(home.Dir()).URL)()
		if r := syncsetup.Resolve(home.Dir()); r.On() {
			fmt.Println(syncwords.ServiceLine(r.URL))
		}
	}
	switch {
	case len(args) > 0 && args[0] == "revoke":
		if !askedForHelp(args[1:]) {
			if err := checkRevokeRosters(kinds); err != nil {
				return err
			}
		}
		return stopDevice(kinds, args[1:])
	case len(args) > 0 && args[0] == "rename":
		return runDevicesRename(args[1:])
	case askedForHelp(args):
		return commandHelp("devices")
	case len(args) > 0:
		return fmt.Errorf("usage: codeaf devices [revoke <name> [--all] | rename <name>]")
	}
	if !cell.Available() {
		paired, err := pair.DeviceBook().Devices()
		if err != nil {
			return err
		}
		fmt.Print(localDeviceTable(time.Now(), false))
		device, err := pair.ThisDevice(pair.OpenKeeper())
		if err != nil {
			return err
		}
		fmt.Print(pair.DevicesList(device.Name(), paired, pair.OpenKeeper(), time.Now()))
		known, err := pair.Machines()
		if err != nil {
			return err
		}
		fmt.Print(pair.MachinesList(known, time.Now()))
		return nil
	}
	return listDevices(kinds, time.Now(), true)
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
// The public command opts into local facts without changing roster-only callers.
func listDevices(kinds []deviceKind, now time.Time, includeLocal ...bool) error {
	// Read each roster once before rendering so duplicate names across the two
	// permissions get selectors too, without adding another service request.
	chats := map[int][]chatsRow{}
	books := map[int][]pair.Paired{}
	counts := map[string]int{}
	for i, kind := range kinds {
		switch c := kind.(type) {
		case chatsKind:
			rows, err := c.rows()
			if err != nil {
				return err
			}
			chats[i] = rows
			for _, row := range rows {
				counts[strings.ToLower(row.baseName)]++
			}
		case bookKind:
			rows, err := c.book.Devices()
			if err != nil {
				return err
			}
			books[i] = rows
			for _, row := range rows {
				counts[strings.ToLower(row.Label)]++
			}
		}
	}
	duplicates := map[string]bool{}
	for name, count := range counts {
		duplicates[name] = count > 1
	}
	var tables []string
	if len(includeLocal) > 0 && includeLocal[0] && len(chats) == 0 {
		tables = append(tables, localDeviceTable(now, true))
	}
	for i, kind := range kinds {
		var table string
		var err error
		switch c := kind.(type) {
		case chatsKind:
			table = chatsRowsTable(chats[i], now, duplicates)
		case bookKind:
			var known []pair.Known
			known, err = pair.Machines()
			if err == nil {
				table, err = c.letIn(books[i], now, duplicates)
				table = strings.TrimLeft(table+pair.MachinesList(known, now), "\n")
			}
		default:
			table, err = kind.table(now)
		}
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
	all := flags.Bool("all", false, "remove every device of that name, computers that sync your chats included, not just the one")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: codeaf devices revoke <name> [--all] — `codeaf devices` lists the names")
	}
	name := flags.Arg(0)
	// A partial --all still says what it did stop before what it could not.
	said, err := stopByName(kinds, name, *all)
	if said != "" {
		fmt.Println(said)
	}
	return err
}

// stopByName finds the one kind that has the name and stops it there. With
// --all it stops every device of that name, in every kind that has one.
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

// stopAllByName stops every device called name. A kind that can stop several
// stops them all; any other kind stops its one, so --all never misses a device
// that `codeaf devices` lists (#2072).
func stopAllByName(kinds []deviceKind, name string) (string, error) {
	var said []string
	var failed error
	for _, kind := range kinds {
		n, err := kind.find(name)
		failed = cmp.Or(failed, err)
		if n == 0 {
			continue
		}
		stop := kind.stop
		if many, ok := kind.(allStopper); ok {
			stop = many.stopAll
		}
		line, err := stop(name)
		if err != nil {
			return strings.Join(said, "\n"), err
		}
		said = append(said, line)
	}
	if len(said) == 0 {
		return "", cmp.Or(failed, unknownDevice(name))
	}
	return strings.Join(said, "\n"), nil
}

// kindOf is the one kind that has a device called name. A name in two kinds is
// as ambiguous as two in one, and is answered the same way: say so, and point at
// the list.
func kindOf(kinds []deviceKind, name string) (deviceKind, error) {
	var hits []deviceKind
	var failed error
	for _, kind := range kinds {
		n, err := kind.find(name)
		if err != nil {
			line := "could not read the device list; nothing was removed — try again"
			switch kind.(type) {
			case chatsKind:
				line = chatsRevokeReadFailure
			case bookKind:
				line = accessRevokeReadFailure
			}
			failed = cmp.Or(failed, error(toldLine{line: line, err: err}))
		}
		if n > 0 {
			hits = append(hits, kind)
		}
	}
	// Both lists must be known before a selector can authorize a removal.
	if failed != nil {
		return nil, failed
	}
	switch {
	case len(hits) == 1:
		return hits[0], nil
	case len(hits) > 1:
		var choices []string
		for _, kind := range hits {
			if selector, ok := kind.(interface {
				selectors(string) ([]string, error)
			}); ok {
				picked, err := selector.selectors(name)
				if err != nil {
					return nil, err
				}
				choices = append(choices, picked...)
			}
		}
		if len(choices) > 0 {
			return nil, devname.Ambiguous(name, choices)
		}
		return nil, fmt.Errorf("more than one device is called %q, so the name cannot pick one — `codeaf devices` lists them", name)
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
// own name is only worth saying when there is a device it matters to.
func (bookKind) letIn(paired []pair.Paired, now time.Time, duplicates ...map[string]bool) (string, error) {
	if len(paired) == 0 {
		return "", nil
	}
	device, err := pair.ThisDevice(pair.OpenKeeper())
	if err != nil {
		return "", err
	}
	return pair.DevicesList(device.Name(), paired, pair.OpenKeeper(), now, duplicates...), nil
}

func (b bookKind) find(name string) (int, error) {
	paired, err := b.book.Devices()
	count := 0
	for _, p := range paired {
		if devname.MatchesDevice(name, p.Label, p.ID()) {
			count++
		}
	}
	return count, err
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
	return fmt.Sprintf("%d devices called %s were removed — each needs a new pairing code to come back.", count, name), nil
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
	if syncsetup.Quiet(home.Dir()) {
		return chatsKind{}, false
	}
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
	id       string
	baseName string
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
	presence, _ := c.dir.Presence(ctx)
	open := func(sealed string) (string, error) { return directory.OpenName(c.key, sealed) }
	rows := make([]chatsRow, 0, len(l.Devices))
	for id, d := range l.Devices {
		name := chatlist.DeviceName(l.Devices, id, open)
		rows = append(rows, chatsRow{
			ChatsDevice: pair.ChatsDevice{Name: name, This: id == c.self, Stopped: d.Revoked, Platform: d.Platform},
			id:          id,
			baseName:    name,
		})
	}
	for i := range rows {
		rows[i].Online = presence.Devices[rows[i].id].Online
		if seen := l.Devices[rows[i].id].LastSeen; seen > 0 {
			rows[i].LastSeen = time.UnixMilli(seen)
		}
	}
	// This computer first, removed ones last, then by name: the order the chat's list uses.
	slices.SortFunc(rows, func(a, b chatsRow) int {
		return cmp.Or(cmp.Compare(rank(a.Stopped), rank(b.Stopped)), -cmp.Compare(rank(a.This), rank(b.This)), cmp.Compare(a.Name, b.Name), cmp.Compare(a.id, b.id))
	})
	return rows, nil
}

// rank is a bool as the number a sort can compare.
func rank(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (c chatsKind) table(now time.Time) (string, error) {
	rows, err := c.rows()
	if err != nil {
		return "", err
	}
	return chatsRowsTable(rows, now, nil), nil
}

// chatsRowsTable renders the same snapshot used to decide duplicate selectors.
func chatsRowsTable(rows []chatsRow, now time.Time, duplicates map[string]bool) string {
	if len(rows) == 0 {
		return ""
	}
	// Selectors belong to presentation. Roster consumers need the original
	// name even when a removed computer once used the same name.
	if duplicates == nil {
		duplicates = chatNameDuplicates(rows)
	}
	shown := make([]pair.ChatsDevice, len(rows))
	for i, row := range rows {
		shown[i] = row.ChatsDevice
		shown[i].Name = devname.DeviceLabel(row.baseName, row.id, duplicates[strings.ToLower(row.baseName)])
	}
	slices.SortStableFunc(shown, func(a, b pair.ChatsDevice) int {
		return cmp.Or(cmp.Compare(rank(a.Stopped), rank(b.Stopped)), -cmp.Compare(rank(a.This), rank(b.This)), cmp.Compare(a.Name, b.Name))
	})
	text := pair.ChatsDevicesListAt(shown, now)
	if stdinIsTerminal(os.Stdout) {
		text = strings.Replace(text, "\nremoved\n", "\n\x1b[2mremoved\x1b[0m\n", 1)
	}
	return text
}

func (c chatsKind) called(name string) ([]chatsRow, error) {
	rows, err := c.rows()
	duplicates := chatNameDuplicates(rows)
	for i := range rows {
		rows[i].Name = devname.DeviceLabel(rows[i].baseName, rows[i].id, duplicates[strings.ToLower(rows[i].baseName)])
	}
	return slices.DeleteFunc(rows, func(r chatsRow) bool { return !devname.MatchesDevice(name, r.baseName, r.id) }), err
}

// chatNameDuplicates keeps list labels and removal receipts on the same selectors.
func chatNameDuplicates(rows []chatsRow) map[string]bool {
	duplicates := map[string]bool{}
	for i, row := range rows {
		for _, other := range rows[i+1:] {
			if strings.EqualFold(row.baseName, other.baseName) {
				duplicates[strings.ToLower(row.baseName)] = true
				duplicates[strings.ToLower(other.baseName)] = true
			}
		}
	}
	return duplicates
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
		return "", ambiguousChatDevices(name, rows)
	case len(rows) == 0:
		return "", unknownDevice(name)
	case rows[0].Stopped:
		return rows[0].Name + " is already removed.", nil
	}
	return c.revoke(rows[0])
}

func (c chatsKind) revoke(row chatsRow) (string, error) {
	if row.This {
		return "", errors.New("that is this device — remove it from another of your devices instead")
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
		return explainRemovedDevice(err)
	case errors.Is(err, wireauth.ErrSkew):
		return errors.New(chatlist.ClockOff)
	case errors.Is(err, directory.ErrUnreachable):
		return explainSyncUnreachable(err, c.relay)
	case errors.Is(err, directory.ErrNotFound):
		return errors.New("that device is not in your list any more — `codeaf devices` shows the ones there are")
	}
	return explainSyncUnreachable(err, c.relay)
}

// explainSyncUnreachable keeps network failures in the same words at every
// command door while preserving the cause for exit handling.
func explainSyncUnreachable(err error, relay string) error {
	// A failed request knows the service it used, even if the configured
	// default changed. Let syncwords read that origin before filling a default.
	var request *url.Error
	if relay == "" && !errors.As(err, &request) {
		relay = syncsetup.Resolve(home.Dir()).URL
	}
	return syncwords.Explain(err, relay)
}

// explainRemovedDevice gives every command the same removal and recovery words.
func explainRemovedDevice(err error) error {
	if errors.Is(err, directory.ErrRevoked) {
		return toldLine{line: chatlist.Removed, err: err}
	}
	return err
}

// ambiguousChatDevices lists usable selectors, so the next command can choose one.
func ambiguousChatDevices(name string, rows []chatsRow) error {
	choices := make([]string, len(rows))
	for i, row := range rows {
		choices[i] = devname.DeviceLabel(row.baseName, row.id, true)
	}
	return devname.Ambiguous(name, choices)
}

func (c chatsKind) selectors(name string) ([]string, error) {
	rows, err := c.called(name)
	choices := make([]string, len(rows))
	for i, row := range rows {
		choices[i] = devname.DeviceLabel(row.baseName, row.id, true)
	}
	return choices, err
}

func (b bookKind) selectors(name string) ([]string, error) {
	rows, err := b.book.Devices()
	var choices []string
	for _, row := range rows {
		if devname.MatchesDevice(name, row.Label, row.ID()) {
			choices = append(choices, devname.DeviceLabel(row.Label, row.ID(), true))
		}
	}
	return choices, err
}

// localDeviceTable uses only this computer's facts before a shared roster exists.
func localDeviceTable(now time.Time, canPair bool) string {
	return pair.ChatsDevicesListAt([]pair.ChatsDevice{{Name: devname.Name(home.Dir()), This: true, Platform: runtime.GOOS}}, now, canPair)
}

const chatsRevokeReadFailure = "could not read the devices that sync your chats; nothing was removed — try again"
const accessRevokeReadFailure = "could not read the devices that can use this machine; nothing was removed — try again"

// checkRevokeRosters distinguishes a home with no sync roster from one whose
// roster setup failed. Quiet display fallbacks cannot authorize a removal.
func checkRevokeRosters(kinds []deviceKind) error {
	_, err := identity.Load(home.Dir())
	if errors.Is(err, identity.ErrNone) {
		return nil
	}
	if err == nil && identity.Solo(home.Dir()) {
		return nil
	}
	if err == nil {
		for _, kind := range kinds {
			if _, ok := kind.(chatsKind); ok {
				return nil
			}
		}
		err = errors.New("sync devices are unavailable")
	}
	return toldLine{line: chatsRevokeReadFailure, err: err}
}
