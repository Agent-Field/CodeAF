package main

// The chat's door onto answering new devices and keeping the device list: what
// the approve screen and `/devices` reach through tui3.Approvals. It is the
// same Approver and signed directory `codeaf pair approve` uses, pointed at
// this computer's home, so the terminal and the app cannot disagree.

import (
	"context"
	"errors"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/livedoor"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// activePhone holds the in-process phone runner when one is running, so
// removing a phone from /devices or `codeaf devices revoke` notifies it. The
// runner's start and stop write it while screen goroutines read it, so it is
// held behind a mutex: a plain package variable read there was a data race.
var activePhone struct {
	sync.Mutex
	u tui3.PhoneUnpairer
}

// activePhoneUnpairer is the running phone runner, or nil when none is.
func activePhoneUnpairer() tui3.PhoneUnpairer {
	activePhone.Lock()
	defer activePhone.Unlock()
	return activePhone.u
}

// setActivePhoneUnpairer records the phone runner that has just started.
func setActivePhoneUnpairer(u tui3.PhoneUnpairer) {
	activePhone.Lock()
	defer activePhone.Unlock()
	activePhone.u = u
}

// clearActivePhoneUnpairer forgets u only while it is still the active one, so
// a runner that stops late can never clear the runner that replaced it.
func clearActivePhoneUnpairer(u tui3.PhoneUnpairer) {
	activePhone.Lock()
	defer activePhone.Unlock()
	if activePhone.u == u {
		activePhone.u = nil
	}
}

// approvalsDoor is the surface's tui3.Approvals.
type approvalsDoor struct {
	dir      string
	mailbox  func(relayFlag string) (pair.Mailbox, error)
	approver func(pair.Mailbox) (pair.Approver, error)
	// permits is this computer's book of what each paired computer may do here.
	// Nil reads the book in the default state folder.
	permits func() *pair.PermitBook
	// unpair notifies an in-process phone runner when a phone is removed.
	unpair tui3.PhoneUnpairer

	mu     sync.Mutex
	asking map[string]pair.Asking // by code: what Pending read, for the answer
}

var _ tui3.Approvals = (*approvalsDoor)(nil)

func newApprovalsDoor(dir string) *approvalsDoor {
	return &approvalsDoor{dir: dir, mailbox: pairMailbox, approver: linkApprover(dir), asking: map[string]pair.Asking{}}
}

// toldLine is a refusal said in the sentence every sync surface uses for it, and
// it still is the refusal for errors.Is.
type toldLine struct {
	line string
	err  error
}

func (e toldLine) Error() string { return e.line }
func (e toldLine) Unwrap() error { return e.err }

// told puts the relay's refusals (a stopped computer, a clock that is off) in
// the words a person reads, so a screen that lists devices says why it cannot,
// and the sentence is the one the chat list and the sync notice already say.
// Anything that is not a refusal is returned as it came.
func told(err error) error {
	if refusal, ok := cellsync.RefusalOf(err); ok {
		line := refusal.Say(err)
		if line == chatlist.Removed {
			line = chatlist.RemovedChat
		}
		return toldLine{line: line, err: err}
	}
	return err
}

func (d *approvalsDoor) who() (pair.Approver, error) {
	route, err := d.mailbox("")
	if err != nil {
		return pair.Approver{}, err
	}
	who, err := d.approver(route)
	if err == nil {
		// The record gets its own wait, so a slow first write cannot spend the approve's.
		who.BeforeApprove = func(ctx context.Context) error {
			ctx, cancel := context.WithTimeout(ctx, approveStep)
			defer cancel()
			return ownDeviceRecord(ctx, d.dir, route)
		}
	}
	return who, err
}

func (d *approvalsDoor) Pending(ctx context.Context, typed string) (tui3.PendingDevice, error) {
	ref, err := pair.ReadLink(typed)
	if err != nil {
		return tui3.PendingDevice{}, err
	}
	route, err := d.mailbox("")
	if err != nil {
		return tui3.PendingDevice{}, err
	}
	// Refuse before a code or its key can reach another sync service.
	if err := pair.CheckLinkService(typed, route.URL); err != nil {
		return tui3.PendingDevice{}, err
	}

	who, err := d.approver(route)
	if err != nil {
		return tui3.PendingDevice{}, err
	}
	as, err := who.Look(ctx, ref)
	if errors.Is(err, pair.ErrLinkDecided) {
		if outcome, devID, isClient := approvedDeviceHere(who, ref.Code, ""); outcome == requestApprovedHere {
			// An approval whose reply was lost: a computer shares the identity,
			// while a phone opens only the phone gate (contract P13).
			if isClient {
				if err := syncsetup.NotePhoneApproved(d.dir, devID); err != nil {
					return tui3.PendingDevice{}, err
				}
			} else if err := pair.EndSoloAt(d.dir, d.book()); err != nil {
				return tui3.PendingDevice{}, err
			}
			if !isClient {
				notePaired(d.dir, route)
			}
			return tui3.PendingDevice{}, tui3.ErrAlreadyPaired
		}
	}
	if err != nil {
		return tui3.PendingDevice{}, told(err)
	}
	d.remember(as)
	return pendingOf(as), nil
}

func pendingOf(as pair.Asking) tui3.PendingDevice {
	return tui3.PendingDevice{
		Code: as.Ref.Code, Device: as.DeviceID(), Name: as.Name, Platform: as.Platform,
		Check: as.Check, Client: as.Client, RequestedAt: as.RequestedAt, ExpiresAt: as.ExpiresAt,
	}
}

func (d *approvalsDoor) remember(as pair.Asking) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.asking[as.Ref.Code] = as
}

func (d *approvalsDoor) recall(p tui3.PendingDevice) (pair.Asking, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	as, ok := d.asking[p.Code]
	if !ok {
		return pair.Asking{}, errors.New("open that pairing link again before approving")
	}
	return as, nil
}

func (d *approvalsDoor) Approve(ctx context.Context, p tui3.PendingDevice) error {
	return d.answer(p, func(who pair.Approver, as pair.Asking) error {
		if err := who.Approve(ctx, as); err != nil {
			if err = settleApproval(who, as, err); err != nil {
				return err
			}
		}
		if as.Client {
			if err := syncsetup.NotePhoneApproved(d.dir, as.DeviceID()); err != nil {
				return err
			}
		} else if err := pair.EndSoloAt(d.dir, d.book()); err != nil {
			return err
		}
		// A phone is not another computer of this identity, so it does not tell the home
		// that one is paired (contract P13).
		if route, err := d.mailbox(""); err == nil && !as.Client {
			notePaired(d.dir, route)
		}
		return nil
	})
}

// approveStep bounds each wait of an approval on its own: the device record,
// and the lookup after an answer whose reply was lost.
const approveStep = 8 * time.Second

// settleApproval reads the request again after an approve that ran out of time
// or was refused as already answered. The relay may have let the device in
// while the reply was lost, or an earlier attempt may have, so an approved
// request for this device is success. A request still waiting says plainly that
// nothing arrived; anything else keeps the error it came with.
func settleApproval(who pair.Approver, as pair.Asking, err error) error {
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, pair.ErrLinkDecided) {
		return err
	}
	switch approvedHere(who, as.Ref.Code, as.DeviceID()) {
	case requestApprovedHere:
		return nil
	case requestWaiting:
		if errors.Is(err, context.DeadlineExceeded) {
			return tui3.ErrAnswerNotArrived
		}
	}
	return err
}

type requestOutcome int

const (
	requestUnknown requestOutcome = iota
	requestWaiting
	requestApprovedHere
)

// approvedHere reads where a request stands. Approved here means the relay
// approved it for this very device AND that device is in this computer's own
// fleet, so a link some other fleet answered never counts as this one sharing.
// An empty device is whichever device the request names.
func approvedHere(who pair.Approver, code, device string) requestOutcome {
	outcome, _, _ := approvedDeviceHere(who, code, device)
	return outcome
}

func approvedDeviceHere(who pair.Approver, code, device string) (requestOutcome, string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), approveStep)
	defer cancel()
	r, err := who.Requests.GetRequest(ctx, code, 0)
	switch {
	case err != nil:
		return requestUnknown, "", false
	case r.State == directory.RequestPending:
		return requestWaiting, "", false
	case r.State != directory.RequestApproved || device != "" && r.Device != device:
		return requestUnknown, "", false
	}
	listing, err := who.Dir.List(ctx)
	dev, ok := listing.Devices[r.Device]
	if err != nil || !ok || dev.Revoked {
		return requestUnknown, "", false
	}
	isClient := r.Class == clientproto.ClientLinkClass || dev.Kind == clientproto.ClientLinkClass
	return requestApprovedHere, r.Device, isClient
}

func (d *approvalsDoor) Deny(ctx context.Context, p tui3.PendingDevice) error {
	return d.answer(p, func(who pair.Approver, as pair.Asking) error { return who.Deny(ctx, as) })
}

// answer runs one decision on the request Pending read, and forgets it after.
func (d *approvalsDoor) answer(p tui3.PendingDevice, decide func(pair.Approver, pair.Asking) error) error {
	as, err := d.recall(p)
	if err != nil {
		return err
	}
	who, err := d.who()
	if err != nil {
		return err
	}
	if err := decide(who, as); err != nil {
		return told(err)
	}
	d.forget(p.Code)
	return nil
}

func (d *approvalsDoor) forget(code string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.asking, code)
}

// home is this computer's signed directory, its identity and its device id.
func (d *approvalsDoor) home() (directory.Client, identity.Identity, string, error) {
	route, err := d.mailbox("")
	if err != nil {
		return nil, identity.Identity{}, "", err
	}
	client, id, err := homeDirectory(d.dir, route)
	if err != nil {
		return nil, id, "", err
	}
	dev, err := identity.Device(d.dir)
	return client, id, dev.ID(), err
}

func (d *approvalsDoor) Devices(ctx context.Context) ([]tui3.DeviceRow, error) {
	// A COMPUTER THAT HAS ONLY EVER SERVED A PHONE IS STILL QUIET toward every other
	// computer, but the person must be able to see the phone and remove it, so a
	// solo computer with an approved phone reads its own device list: that list
	// holds this computer and its phones and nothing else (contract P13).
	if syncsetup.Quiet(d.dir) && !syncsetup.PhonesApproved(d.dir) {
		return []tui3.DeviceRow{d.ownRow()}, nil
	}
	client, id, self, err := d.home()
	if err != nil {
		return nil, err
	}
	listing, err := client.List(ctx)
	if err != nil {
		return nil, told(err)
	}
	rows := rowsOf(listing, directory.MetadataKey(id.CellKey()), self)
	foundSelf := false
	for _, row := range rows {
		foundSelf = foundSelf || row.Self
	}
	if !foundSelf {
		rows = append(rows, d.ownRow())
	}
	if own, named := devname.Chosen(d.dir); named {
		rows = withOwnName(rows, own)
	}
	return d.withPermits(id.ID(), rows), nil
}

// book is the permit book this door reads and writes.
func (d *approvalsDoor) book() *pair.PermitBook {
	if d.permits != nil {
		return d.permits()
	}
	return pair.Permits()
}

// withPermits puts what each other computer may do here on its row, so /devices
// can show and change it. WITH REMOTE CONTROL OFF IT PUTS NOTHING: the rows,
// and every line the device list draws from them, are then what they were
// before the setting existed, and the switches are absent rather than inert.
// Your own computer and a removed one have no permit to show.
func (d *approvalsDoor) withPermits(identityID string, rows []tui3.DeviceRow) []tui3.DeviceRow {
	if identityID == "" || !config.RemoteControlOn() {
		return rows
	}
	book := d.book()
	for i := range rows {
		if rows[i].Self || rows[i].Revoked || rows[i].Client || rows[i].Kind == clientproto.ClientLinkClass {
			continue
		}
		permit, _ := book.For(identityID, rows[i].ID)
		rows[i].Permit = &tui3.DevicePermit{Watch: permit.Watch, Drive: permit.Drive}
	}
	return rows
}

// SetPermit changes what one paired computer may do with chats on this one. A nil
// half is left as it is. It is the same write `codeaf devices permit` makes, so
// the terminal's keys and the command cannot disagree.
func (d *approvalsDoor) SetPermit(id string, watch, drive *bool) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("choose a computer first")
	}
	who, err := identity.Load(d.dir)
	if err != nil {
		return err
	}
	return d.book().Set(who.ID(), id, watch, drive, "person")
}

var _ tui3.PermitSetter = (*approvalsDoor)(nil)

// withOwnName puts this computer's own name on its own row. The directory holds
// the name this computer last managed to tell it, which is the old one for a
// person who renamed offline; the name kept here is the one they typed.
func withOwnName(rows []tui3.DeviceRow, own string) []tui3.DeviceRow {
	for i := range rows {
		if rows[i].Self {
			rows[i].Name = own
		}
	}
	return rows
}

func rowsOf(l directory.Listing, key []byte, self string) []tui3.DeviceRow {
	rows := make([]tui3.DeviceRow, 0, len(l.Devices))
	for id, dev := range l.Devices {
		rows = append(rows, rowOf(id, dev, key, self))
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Name < rows[j].Name || (rows[i].Name == rows[j].Name && rows[i].ID < rows[j].ID)
	})
	return rows
}

func rowOf(id string, dev directory.Device, key []byte, self string) tui3.DeviceRow {
	isClient := dev.Kind == clientproto.ClientLinkClass
	row := tui3.DeviceRow{
		ID: id, Name: openName(key, dev.Name), Platform: dev.Platform,
		Kind: dev.Kind, Client: isClient, Self: id == self, Revoked: dev.Revoked,
	}
	if dev.LastSeen > 0 {
		row.LastSeen = time.UnixMilli(dev.LastSeen)
	}
	return row
}

// openName is a sealed name in words, and "a device" when it cannot be opened.
func openName(key []byte, sealed string) string {
	if name, err := directory.OpenName(key, sealed); err == nil && strings.TrimSpace(name) != "" {
		return devname.Shown(name)
	}
	return "a device"
}

func (d *approvalsDoor) phoneUnpairer() tui3.PhoneUnpairer {
	if d.unpair != nil {
		return d.unpair
	}
	return activePhoneUnpairer()
}

func (d *approvalsDoor) Revoke(ctx context.Context, id string) error {
	client, _, _, err := d.home()
	if err != nil {
		return err
	}
	if err := client.Revoke(ctx, id); err != nil {
		return told(err)
	}
	if u := d.phoneUnpairer(); u != nil {
		_ = u.Unpair(ctx, id)
	}
	// Forgotten after the runner has unpaired it, so that a solo computer's runner may
	// still send the revoke while the phone is recorded (contract P13).
	if err := syncsetup.ForgetPhone(d.dir, id); err != nil {
		return err
	}
	// A REMOVED COMPUTER STOPS BEING ABLE TO WATCH AT ONCE: its open live views
	// here close, its permit goes, and the last-seen copies kept of its chats go,
	// the same three things `codeaf devices remove` and the app do.
	livedoor.NotifyRemoved(d.dir, id)
	_ = d.book().Forget(id)
	forgetViewed(d.dir, id)
	return nil
}

func (d *approvalsDoor) DeviceName(sealed string) string {
	id, err := identity.Load(d.dir)
	if err != nil {
		return "a new device"
	}
	return openName(directory.MetadataKey(id.CellKey()), sealed)
}

// Rename names this computer. It is the door's one write of its own record: the
// same function `codeaf devices rename` runs, so the two cannot say different
// things.
func (d *approvalsDoor) Rename(ctx context.Context, typed string) (string, error) {
	return renameThisDevice(ctx, d.dir, typed)
}

// ownRow keeps an unpaired computer nameable without waking the sync service.
func (d *approvalsDoor) ownRow() tui3.DeviceRow {
	dev, err := identity.LoadDevice(d.dir)
	id := ""
	if err == nil {
		id = dev.ID()
	}
	return tui3.DeviceRow{ID: id, Name: devname.Name(d.dir), Platform: runtime.GOOS, Self: true}
}

// SyncService names the local pairing service even when the chat runs elsewhere.
func (d *approvalsDoor) SyncService() string { return syncsetup.Resolve(d.dir).URL }

// RelayStorage reads this computer's account without creating keys or asking the network.
func (d *approvalsDoor) RelayStorage() (code, state string) {
	relay := syncsetup.Resolve(d.dir)
	if !relay.On() || identity.Solo(d.dir) {
		return "", ""
	}
	id, err := identity.Load(d.dir)
	if err != nil {
		return "", ""
	}
	code = reqsign.IDOf(id.PublicKey())
	state = syncsetup.ReportedStorage(d.dir, relay.URL, code)
	if state == "" {
		return "", ""
	}
	return code, state
}
