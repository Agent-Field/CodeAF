package pair

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

type testSigner struct {
	id  identity.Identity
	dev identity.Dev
}

func (s testSigner) IdentityKey() ed25519.PublicKey { return s.id.PublicKey() }
func (s testSigner) Cert() identity.Cert            { return s.dev.Cert }
func (s testSigner) Sign(msg []byte) []byte         { return s.dev.Sign(msg) }

// linkRig is a relay, a device that is in, and an empty home for a new one.
type linkRig struct {
	url      string
	old, new string
	oldDir   *directory.HTTP
	approver Approver
}

func newLinkRig(t *testing.T) *linkRig {
	t.Helper()
	svc := relayserve.New(relayserve.Config{Store: t.TempDir()})
	srv := httptest.NewServer(svc.Handler)
	t.Cleanup(func() { srv.Close(); svc.Close() })
	r := &linkRig{url: srv.URL, old: t.TempDir(), new: t.TempDir()}
	id, err := identity.Ensure(r.old)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := identity.Device(r.old)
	if err != nil {
		t.Fatal(err)
	}
	r.oldDir = directory.NewHTTP(srv.URL, reqsign.SignFor(testSigner{id, dev}, time.Now), srv.Client())
	sealed, _ := directory.SealName(directory.MetadataKey(id.CellKey()), "spark")
	if err := r.oldDir.PutDevice(context.Background(), dev.ID(), directory.Device{V: 1, Name: sealed, AddedBy: id.ID()}); err != nil {
		t.Fatal(err)
	}
	r.approver = Approver{Requests: directory.NewLinkHTTP(srv.URL, srv.Client()), Dir: r.oldDir, Identity: id}
	return r
}

func (r *linkRig) joining(replace bool) LinkJoining {
	dir := r.new
	return LinkJoining{
		Joining:  Joining{Home: dir, Label: "laptop", Replace: replace},
		Platform: "linux", Host: "sync.test",
		Pull: func(ctx context.Context) (Fleet, error) {
			id, _ := identity.Load(dir)
			dev, _ := identity.Device(dir)
			l, err := directory.NewHTTP(r.url, reqsign.SignFor(testSigner{id, dev}, time.Now), http.DefaultClient).List(ctx)
			return Fleet{Devices: len(l.Devices)}, err
		},
	}
}

type invites struct {
	got chan Invite
}

func (i *invites) Invited(in Invite) { i.got <- in }

// start runs a join by link in the background.
func (r *linkRig) start(t *testing.T, ctx context.Context) (*invites, chan result) {
	t.Helper()
	ui := &invites{got: make(chan Invite, 1)}
	done := make(chan result, 1)
	go func() {
		j, err := JoinByLink(ctx, r.approver.Requests, r.joining(false), ui)
		done <- result{j, err}
	}()
	return ui, done
}

type result struct {
	joined LinkJoined
	err    error
}

func waitInvite(t *testing.T, ui *invites) Invite {
	t.Helper()
	select {
	case in := <-ui.got:
		return in
	case <-time.After(10 * time.Second):
		t.Fatal("no link was shown")
		return Invite{}
	}
}

func waitResult(t *testing.T, done chan result) result {
	t.Helper()
	select {
	case res := <-done:
		return res
	case <-time.After(20 * time.Second):
		t.Fatal("the join did not finish")
		return result{}
	}
}

// A new device asks, the old one sees its name and approves, and the new one
// ends up holding the same identity and a device the old one's directory lists.
func TestJoinByLinkApproved(t *testing.T) {
	r := newLinkRig(t)
	ui, done := r.start(t, context.Background())
	in := waitInvite(t, ui)

	if !strings.HasPrefix(in.Ref.URL(), "https://codeaf.agentfield.ai/p/"+in.Ref.Code+"#") || len(in.Check) != 4 {
		t.Fatalf("the invite looks wrong: %+v", in)
	}
	ref, err := ReadLink(in.Ref.Token())
	if err != nil {
		t.Fatal(err)
	}
	asking, err := r.approver.Look(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if asking.Name != "laptop" || asking.Platform != "linux" || asking.Check != in.Check {
		t.Fatalf("the approver saw %+v, want laptop on linux with check %s", asking, in.Check)
	}
	if err := r.approver.Approve(context.Background(), asking); err != nil {
		t.Fatal(err)
	}

	res := waitResult(t, done)
	if res.err != nil {
		t.Fatal(res.err)
	}
	old, _ := identity.Load(r.old)
	got, err := identity.Load(r.new)
	if err != nil || got.ID() != old.ID() {
		t.Fatalf("the new device holds %v (%v), want the old identity", got.ID(), err)
	}
	if res.joined.Fleet.Devices != 2 {
		t.Fatalf("the new device sees %d devices, want 2", res.joined.Fleet.Devices)
	}
	dev, _ := identity.Device(r.new)
	listed, _ := r.oldDir.List(context.Background())
	if _, ok := listed.Devices[dev.ID()]; !ok || dev.Cert.Verify(old.PublicKey()) != nil {
		t.Fatalf("the new device %s is not a certified device of the identity", dev.ID())
	}
}

// A no ends the wait with the declined sentence and installs nothing.
func TestJoinByLinkDeclined(t *testing.T) {
	r := newLinkRig(t)
	ui, done := r.start(t, context.Background())
	ref := waitInvite(t, ui).Ref
	asking, err := r.approver.Look(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.approver.Deny(context.Background(), asking); err != nil {
		t.Fatal(err)
	}
	if res := waitResult(t, done); !errors.Is(res.err, ErrLinkDeclined) {
		t.Fatalf("a declined request ended with %v", res.err)
	}
	if _, err := identity.Load(r.new); err == nil {
		t.Fatal("a declined device was given the identity")
	}
	if err := r.approver.Approve(context.Background(), asking); !errors.Is(err, ErrLinkDecided) {
		t.Fatalf("approving after a no gave %v, want already answered", err)
	}
}

// goneRequests answers every poll as an expired request.
type goneRequests struct{ directory.Requests }

func (goneRequests) CreateRequest(_ context.Context, in directory.NewRequest) (directory.Opened, error) {
	return directory.Opened{Code: "k7m2q9xd", Check: "0000", RequestedAt: 0, ExpiresAt: 600000}, nil
}
func (goneRequests) GetRequest(context.Context, string, time.Duration) (directory.Request, error) {
	return directory.Request{}, directory.ErrRequestGone
}

// A request that ran out says to run the command again.
func TestJoinByLinkExpires(t *testing.T) {
	r := newLinkRig(t)
	ui := &invites{got: make(chan Invite, 1)}
	_, err := JoinByLink(context.Background(), goneRequests{}, r.joining(false), ui)
	if !errors.Is(err, ErrLinkExpired) || !strings.Contains(err.Error(), "codeaf pair") {
		t.Fatalf("an expired request ended with %v", err)
	}
	if len(ui.got) != 1 {
		t.Fatal("the link was never shown")
	}
}

// A device that already has an identity is not overwritten without --replace,
// and nothing is asked of the relay before that is said.
func TestJoinByLinkKeepsOwnChats(t *testing.T) {
	r := newLinkRig(t)
	if _, err := identity.Ensure(r.new); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.new, "vault.enc"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := JoinByLink(context.Background(), goneRequests{}, r.joining(false), &invites{got: make(chan Invite, 1)})
	if !errors.Is(err, ErrDifferentChats) {
		t.Fatalf("joining over own chats ended with %v", err)
	}
}

// A request whose check number does not fit its key is refused by the approver.
func TestLookRefusesAMismatchedCheck(t *testing.T) {
	r := newLinkRig(t)
	ui, _ := r.start(t, context.Background())
	in := waitInvite(t, ui)
	bad := directory.Request{Code: in.Ref.Code, State: directory.RequestPending, Check: "0000", Pubkey: b64u.EncodeToString(make([]byte, 32)), X25519: b64u.EncodeToString(make([]byte, 32))}
	if _, err := askingOf(in.Ref, bad); !errors.Is(err, ErrCheckMismatch) {
		t.Fatalf("a mismatched check gave %v", err)
	}
}

func TestLinkShapes(t *testing.T) {
	key := b64u.EncodeToString(make([]byte, 16))
	for _, text := range []string{
		"https://codeaf.agentfield.ai/p/k7m2q9xd#" + key, "codeaf://pair?code=K7M2Q9XD#" + key, "k7m2q9xd." + key, "k7m2q9xd#" + key, "K7M2Q9XD", "k7m2q9xd",
	} {
		ref, err := ReadLink(text)
		if err != nil || ref.Code != "k7m2q9xd" {
			t.Errorf("%q read as %+v (%v)", text, ref, err)
		}
		if !IsLinkText(text) {
			t.Errorf("%q is not link text", text)
		}
	}
	for _, text := range []string{"42-715-302", "715 302", "42715302", "715302", "", "k7m2q9x", "https://example.com/p/k7m2q9xd#" + key, "https://codeaf.link/p/k7m2q9xd#" + key, "k7m2q9xd.short"} {
		if IsLinkText(text) {
			t.Errorf("%q was taken for link text", text)
		}
	}
}

func TestDeviceNameSealing(t *testing.T) {
	key, _ := NewLinkKey()
	sealed, err := SealDeviceName(key, "my laptop")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := OpenDeviceName(key, sealed); err != nil || got != "my laptop" {
		t.Fatalf("opened %q (%v)", got, err)
	}
	other, _ := NewLinkKey()
	if _, err := OpenDeviceName(other, sealed); err == nil {
		t.Fatal("a wrong key opened the name")
	}
	long, _ := SealDeviceName(key, strings.Repeat("é", 60))
	if raw, _ := b64u.DecodeString(long); len(raw) > 24+16+maxNameBytes {
		t.Fatalf("a long name sealed to %d bytes", len(raw))
	}
}

// A computer that has only been started (it made its own identity and holds
// nothing else) joins without being told to replace, and ends on the old identity.
func TestJoinByLinkAdoptsAnUnusedIdentity(t *testing.T) {
	r := newLinkRig(t)
	if _, err := identity.Ensure(r.new); err != nil {
		t.Fatal(err)
	}
	ui, done := r.start(t, context.Background())
	in := waitInvite(t, ui)
	ref, _ := ReadLink(in.Ref.Token())
	asking, err := r.approver.Look(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.approver.Approve(context.Background(), asking); err != nil {
		t.Fatal(err)
	}
	if res := waitResult(t, done); res.err != nil {
		t.Fatal(res.err)
	}
	old, _ := identity.Load(r.old)
	if got, _ := identity.Load(r.new); got.ID() != old.ID() {
		t.Fatalf("the unused computer holds %v, want the old identity %v", got.ID(), old.ID())
	}
}

// Anything a person made here (a vault, a chat store) makes the computer used,
// so it keeps its identity and nothing is asked of the relay.
func TestJoinByLinkKeepsAUsedIdentity(t *testing.T) {
	for _, name := range []string{"vault.enc", "graph.db", "v3", "something-new"} {
		r := newLinkRig(t)
		own, err := identity.Ensure(r.new)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(r.new, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err = JoinByLink(context.Background(), goneRequests{}, r.joining(false), &invites{got: make(chan Invite, 1)})
		if !errors.Is(err, ErrDifferentChats) {
			t.Fatalf("%s: joining over a used computer ended with %v", name, err)
		}
		if got, _ := identity.Load(r.new); got.ID() != own.ID() {
			t.Fatalf("%s: the used computer's identity changed", name)
		}
	}
}
