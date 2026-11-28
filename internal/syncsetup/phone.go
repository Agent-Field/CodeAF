package syncsetup

// A SOLO COMPUTER MAY SERVE AN APPROVED PHONE WITHOUT STARTING TO SHARE CHATS.
// Approving a phone does not share the identity key or the cell key with
// another computer, so the computer's cell-sealed chat list, cluster
// membership, Teams state and transcript store still have no reader. Leaving
// identity.Solo intact keeps MayTalk false and all computer-to-computer sync
// quiet, while [PhoneMayTalk] and [phoneWire] let only the phone runner's own
// directory and client-fabric requests out as long as at least one approved,
// unrevoked phone is recorded on disk (contract P13).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

var phonesMu sync.Mutex

// phonesPath is where this home keeps the device ids of approved, unrevoked
// phones beside the other sync markers.
func phonesPath(home string) string {
	return filepath.Join(home, "v3", "sync", "phones.json")
}

func readPhonesLocked(home string) ([]string, error) {
	raw, err := os.ReadFile(phonesPath(home))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out, nil
}

func writePhonesLocked(home string, ids []string) error {
	if ids == nil {
		ids = []string{}
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	path := phonesPath(home)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "phones.json.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	// THE LIST SURVIVES A CRASH. Approving or removing a phone is a promise to
	// the person, so the bytes reach the disk before the rename and the rename
	// reaches the disk before this returns.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := fsreplace.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return fsreplace.SyncDir(dir)
}

// NotePhoneApproved records that device is an approved, unrevoked phone for
// home. Recording the same phone twice keeps one entry and does not notify
// listeners a second time.
func NotePhoneApproved(home, device string) error {
	device = strings.TrimSpace(device)
	if home == "" || device == "" {
		return nil
	}
	phonesMu.Lock()
	ids, err := readPhonesLocked(home)
	if err != nil {
		phonesMu.Unlock()
		return err
	}
	if slices.Contains(ids, device) {
		phonesMu.Unlock()
		return nil
	}
	wasApproved := len(ids) > 0
	ids = append(ids, device)
	if err := writePhonesLocked(home, ids); err != nil {
		phonesMu.Unlock()
		return err
	}
	changed := !wasApproved && len(ids) > 0
	phonesMu.Unlock()
	if changed {
		phonesChanged(home)
	}
	return nil
}

// ForgetPhone removes device from home's approved phone list. Forgetting an id
// that is not present is not an error.
func ForgetPhone(home, device string) error {
	device = strings.TrimSpace(device)
	if home == "" || device == "" {
		return nil
	}
	phonesMu.Lock()
	ids, err := readPhonesLocked(home)
	if err != nil {
		phonesMu.Unlock()
		return err
	}
	if !slices.Contains(ids, device) {
		phonesMu.Unlock()
		return nil
	}
	wasApproved := len(ids) > 0
	ids = slices.DeleteFunc(ids, func(id string) bool { return id == device })
	if err := writePhonesLocked(home, ids); err != nil {
		phonesMu.Unlock()
		return err
	}
	changed := wasApproved && len(ids) == 0
	phonesMu.Unlock()
	if changed {
		phonesChanged(home)
	}
	return nil
}

// phoneRecorded reports whether device is one of the approved phones recorded for home.
func phoneRecorded(home, device string) bool {
	phonesMu.Lock()
	ids, err := readPhonesLocked(home)
	phonesMu.Unlock()
	return err == nil && slices.Contains(ids, device)
}

// PhonesApproved reports whether home has at least one approved, unrevoked
// phone recorded on disk.
func PhonesApproved(home string) bool {
	if home == "" {
		return false
	}
	phonesMu.Lock()
	ids, err := readPhonesLocked(home)
	phonesMu.Unlock()
	return err == nil && len(ids) > 0
}

// phonesListeners are told, in this process, whenever PhonesApproved changes in
// either direction for a home.
var phonesListeners = struct {
	sync.Mutex
	next int
	by   map[int]func(home string)
}{by: map[int]func(string){}}

// OnPhonesChange registers f to hear, with the home concerned, whenever
// PhonesApproved changes in either direction: the first phone was approved, or
// the last one was forgotten. A no-op write notifies nobody.
func OnPhonesChange(f func(home string)) (stop func()) {
	phonesListeners.Lock()
	defer phonesListeners.Unlock()
	phonesListeners.next++
	id := phonesListeners.next
	phonesListeners.by[id] = f
	return func() {
		phonesListeners.Lock()
		defer phonesListeners.Unlock()
		delete(phonesListeners.by, id)
	}
}

func phonesChanged(home string) {
	phonesListeners.Lock()
	var tell []func(string)
	for _, f := range phonesListeners.by {
		tell = append(tell, f)
	}
	phonesListeners.Unlock()
	for _, f := range tell {
		f(home)
	}
}

// PhoneMayTalk is the narrower gate for the phone runner: sync is on, the
// identity loads, and either the identity is not solo or at least one approved
// phone is recorded for home. MayTalk and Quiet are unchanged.
func PhoneMayTalk(home string) bool {
	if !Resolve(home).On() {
		return false
	}
	if _, err := identity.Load(home); err != nil {
		return false
	}
	return !identity.Solo(home) || PhonesApproved(home)
}

func (s *Sync) phoneBaseTransport() http.RoundTripper {
	if s != nil && s.phoneBase != nil {
		return s.phoneBase
	}
	return http.DefaultTransport
}

// PhoneHTTP returns an HTTP client gated by phoneWire for the phone runner's
// client-fabric requests.
func (s *Sync) PhoneHTTP() *http.Client {
	return &http.Client{
		Timeout: requestTimeout,
		Transport: phoneWire{
			home:  s.Home,
			self:  s.Device.ID(),
			next:  relayCountingWire{s: s, next: s.phoneBaseTransport()},
			owner: s,
		},
	}
}

// PhoneDirectory returns a directory client signed by this computer's device
// signer and gated by phoneWire, satisfying directory.ServingWatcher with a
// stream that closes as soon as PhoneMayTalk stops holding.
func (s *Sync) PhoneDirectory() directory.Client {
	sign := reqsign.SignFor(deviceSigner{s.Identity, s.Device}, s.now)
	return &phoneDirectory{
		HTTP:  directory.NewHTTP(s.Relay, sign, s.PhoneHTTP()),
		home:  s.Home,
		owner: s,
	}
}

type phoneDirectory struct {
	*directory.HTTP
	home  string
	owner *Sync
}

var _ directory.ServingWatcher = (*phoneDirectory)(nil)

func (d *phoneDirectory) Watch(ctx context.Context) (dirwatch.Stream, error) {
	stream, err := d.HTTP.Watch(ctx)
	if err != nil {
		return nil, preservePhoneQuiet(err)
	}
	return phoneStream{Stream: stream, home: d.home, owner: d.owner}, nil
}

func (d *phoneDirectory) WatchHolding(ctx context.Context, holds []directory.Hold) (dirwatch.Stream, error) {
	stream, err := d.HTTP.WatchHolding(ctx, holds)
	if err != nil {
		return nil, preservePhoneQuiet(err)
	}
	return phoneStream{Stream: stream, home: d.home, owner: d.owner}, nil
}

func (d *phoneDirectory) WatchServing(ctx context.Context) (dirwatch.Stream, error) {
	stream, err := d.HTTP.WatchServing(ctx)
	if err != nil {
		return nil, preservePhoneQuiet(err)
	}
	return phoneStream{Stream: stream, home: d.home, owner: d.owner}, nil
}

func preservePhoneQuiet(err error) error {
	if err != nil && !errors.Is(err, ErrQuiet) && strings.Contains(err.Error(), ErrQuiet.Error()) {
		return fmt.Errorf("%w: %w", directory.ErrUnreachable, ErrQuiet)
	}
	return err
}

// phoneWire lets all traffic through once MayTalk holds; while the computer is
// still solo, it lets only the phone runner's routes through when PhoneMayTalk
// holds and refuses everything else with ErrQuiet before reaching the network.
type phoneWire struct {
	home  string
	self  string
	next  http.RoundTripper
	owner *Sync
}

func (w phoneWire) RoundTrip(r *http.Request) (*http.Response, error) {
	if w.owner != nil && w.owner.isRemoved() {
		return removedReply(r), nil
	}
	if !MayTalk(w.home) {
		if !PhoneMayTalk(w.home) || !phoneAllowedRequest(w.home, r, w.self) {
			return nil, ErrQuiet
		}
	}
	return w.next.RoundTrip(r)
}

func phoneAllowedRequest(home string, r *http.Request, self string) bool {
	if r == nil || r.URL == nil {
		return false
	}
	p := r.URL.Path
	if strings.Contains(p, "..") || strings.Contains(r.URL.EscapedPath(), "..") {
		return false
	}
	switch {
	case r.Method == http.MethodGet && p == "/v1/dir/list":
		return true
	case r.Method == http.MethodGet && p == "/v1/dir/watch":
		return r.URL.Query().Get(clientproto.ServingParam) == "1"
	case r.Method == http.MethodPut && self != "" && !strings.Contains(self, "/") && p == "/v1/dir/devices/"+self:
		return true
	case r.Method == http.MethodGet && p == "/v1/clients":
		return true
	case r.Method == http.MethodPost && strings.HasPrefix(p, "/v1/dir/devices/"):
		// REMOVING A PHONE THIS COMPUTER APPROVED is the runner's own traffic: it unpairs
		// the phone and the removal has to reach the sync service. Only an id recorded
		// as an approved phone here may be revoked through this gate, never another device.
		id, ok := strings.CutSuffix(strings.TrimPrefix(p, "/v1/dir/devices/"), "/revoke")
		return ok && id != "" && !strings.Contains(id, "/") && phoneRecorded(home, id)
	}
	rest, ok := strings.CutPrefix(p, "/v1/clients/")
	if !ok {
		return false
	}
	id, tail, ok := strings.Cut(rest, "/")
	if !ok || id == "" {
		return false
	}
	if tail == "queue" {
		return r.Method == http.MethodGet
	}
	if afterQueue, ok := strings.CutPrefix(tail, "queue/"); ok {
		if num, hasClaim := strings.CutSuffix(afterQueue, "/claim"); hasClaim {
			return r.Method == http.MethodPost && positiveQueueNumber(num)
		}
		return r.Method == http.MethodDelete && positiveQueueNumber(afterQueue)
	}
	if slotName, ok := strings.CutPrefix(tail, "slots/"); ok {
		return slotName != "" && !strings.Contains(slotName, "/") && (r.Method == http.MethodPut || r.Method == http.MethodGet)
	}
	return false
}

func positiveQueueNumber(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return err == nil && n > 0
}

// phoneStream rechecks PhoneMayTalk and the removed state at every Next and
// Ping, closing the socket and answering ErrQuiet when the last phone of a solo
// computer is forgotten or sync is turned off.
type phoneStream struct {
	dirwatch.Stream
	home  string
	owner *Sync
}

func (s phoneStream) allowed() bool {
	return PhoneMayTalk(s.home)
}

func (s phoneStream) Next(ctx context.Context) (dirwatch.Frame, error) {
	if s.owner != nil && s.owner.isRemoved() {
		s.Stream.Close()
		return dirwatch.Frame{}, dirwatch.ErrRevoked
	}
	if !s.allowed() {
		s.Stream.Close()
		return dirwatch.Frame{}, ErrQuiet
	}
	frame, err := s.Stream.Next(ctx)
	if s.owner != nil && (errors.Is(err, dirwatch.ErrRevoked) || errors.Is(err, directory.ErrRevoked)) {
		s.owner.rememberRemoved()
	}
	return frame, err
}

func (s phoneStream) Ping(ctx context.Context) error {
	if s.owner != nil && s.owner.isRemoved() {
		s.Stream.Close()
		return dirwatch.ErrRevoked
	}
	if !s.allowed() {
		s.Stream.Close()
		return ErrQuiet
	}
	return s.Stream.Ping(ctx)
}

func (s phoneStream) Vouching() bool {
	return (s.owner == nil || !s.owner.isRemoved()) && s.allowed() && s.Stream.Vouching()
}
