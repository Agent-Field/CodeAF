package syncsetup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/filesdoor"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/livedoor"
	"github.com/Agent-Field/codeaf/internal/pair"
	relaypkg "github.com/Agent-Field/codeaf/internal/relay"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// stubLiveAgent embeds remote.WrappedAgent so only the methods exercised by a
// live viewer turn are implemented and any unexpected call panics into the
// server's recovery.
type stubLiveAgent struct {
	remote.WrappedAgent
	title      string
	replyTexts []string

	mu        sync.Mutex
	submitted []string
}

func (a *stubLiveAgent) Submit(_ context.Context, text string) (<-chan session.Event, error) {
	a.mu.Lock()
	a.submitted = append(a.submitted, text)
	replies := slices.Clone(a.replyTexts)
	a.mu.Unlock()

	ch := make(chan session.Event, len(replies))
	for _, part := range replies {
		ch <- session.Event{Kind: session.EventTextDelta, Text: part}
	}
	close(ch)
	return ch, nil
}

func (a *stubLiveAgent) Interrupt()                                   {}
func (a *stubLiveAgent) InterruptFor(session.StopDoor)                {}
func (a *stubLiveAgent) Close() error                                 { return nil }
func (a *stubLiveAgent) Model() string                                { return "stub-live-model" }
func (a *stubLiveAgent) Title() string                                { return a.title }
func (a *stubLiveAgent) Usage() session.Usage                         { return session.Usage{} }
func (a *stubLiveAgent) ContextTokens() int                           { return 0 }
func (a *stubLiveAgent) ReasoningLevels() map[string]string           { return nil }
func (a *stubLiveAgent) Transcript() []session.DisplayEntry           { return nil }
func (a *stubLiveAgent) EarlierHistory() session.EarlierHistory       { return session.EarlierHistory{} }
func (a *stubLiveAgent) RewindPoints() []session.RewindPoint          { return nil }
func (a *stubLiveAgent) RewindAt(int) ([]session.DisplayEntry, error) { return nil, nil }

func (a *stubLiveAgent) submissions() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.submitted)
}

// shortHostHome symlinks r.homeA into a short /tmp/ld-* path so Unix domain
// sockets created under <home>/v3/live/<pid>.sock stay well below the 103-byte
// sun_path limit regardless of the test function name length.
func shortHostHome(t *testing.T, r *liveRig) string {
	t.Helper()
	shortDir, err := os.MkdirTemp("/tmp", "ld-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(shortDir) })
	shortHome := filepath.Join(shortDir, "a")
	if err := os.Symlink(r.homeA, shortHome); err != nil {
		t.Fatal(err)
	}
	r.homeA = shortHome
	r.a.Home = shortHome
	return shortHome
}

// hostHeldChat sets up a running chat on r.a backed by livedoor.Listen and a
// locked transcript plus presence file so livedoor.NewResolver routes live
// channels to it without starting a daemon.
func hostHeldChat(t *testing.T, r *liveRig, chatID string, agent *stubLiveAgent) (*remote.Session, string) {
	t.Helper()
	shortHostHome(t, r)
	chatDir := filepath.Join(r.a.Home, "v3", "projects", "bucket", chatID)
	if err := os.MkdirAll(chatDir, 0o700); err != nil {
		t.Fatal(err)
	}
	transcript := (session.Place{Dir: chatDir}).Transcript()
	lockFile, err := os.OpenFile(transcript, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := filelock.Lock(lockFile, true, true); err != nil {
		_ = lockFile.Close()
		t.Fatalf("locking transcript %q: %v", transcript, err)
	}
	t.Cleanup(func() {
		_ = filelock.Unlock(lockFile)
		_ = lockFile.Close()
	})

	presenceRaw, err := json.Marshal(session.SessionPresence{
		Schema:    1,
		SessionID: chatID,
		Workspace: r.work,
		PID:       os.Getpid(),
		UpdatedAt: time.Now(),
		State:     session.PresenceIdle,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chatDir, "presence.json"), presenceRaw, 0o600); err != nil {
		t.Fatal(err)
	}

	sess := remote.NewSession(&remote.Engine{
		Agent:       agent,
		Workspace:   r.work,
		SessionFile: transcript,
	}, true)
	t.Cleanup(func() { _ = sess.Close() })

	ln, err := livedoor.Listen(r.a.Home, func(target string) (*remote.Session, bool) {
		if filepath.Clean(target) == filepath.Clean(transcript) {
			return sess, true
		}
		return nil, false
	}, nil)
	if err != nil {
		t.Fatalf("livedoor.Listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	return sess, transcript
}

func allowViewerDrive(t *testing.T, r *liveRig) {
	t.Helper()
	on := true
	mustNil(t, pair.Permits().Set(r.a.Identity.ID(), r.b.Device.ID(), &on, &on, "person"))
}

// TestAMessageTypedOnTheViewerReachesTheHostAndTheReplyShowsOnBothWithStorageOff
// verifies contract V3: with storage off on both homes, a viewer connected via
// Sync.LiveLink and remote.Roam submits a message that reaches the host's held
// session stub agent once, the stub's reply events reach both the viewer and
// the host's own attached surface, zero /v1/store/* requests are made, and at
// least one dial carries header Codeaf-Door: live.
func TestAMessageTypedOnTheViewerReachesTheHostAndTheReplyShowsOnBothWithStorageOff(t *testing.T) {
	t.Setenv("CODEAF_REMOTE_CONTROL", "on")
	r := newLiveRig(t)
	allowViewerDrive(t, r)

	stub := &stubLiveAgent{
		title:      "Live Hosted Chat",
		replyTexts: []string{"reply-part-1", "reply-part-2"},
	}
	sess, transcript := hostHeldChat(t, r, r.cell.ID, stub)

	// Attach the host's own local surface to the same persistent session.
	hostSurface, err := remote.LoopbackAttach(remote.Hello{
		Workspace: r.work,
		Session:   transcript,
		Surface:   nameA,
		Join:      true,
	}, remote.AttachOptions{
		Open: func(remote.Hello) (*remote.Session, error) {
			return sess, nil
		},
	})
	if err != nil {
		t.Fatalf("LoopbackAttach on host: %v", err)
	}
	t.Cleanup(func() { _ = hostSurface.Close() })

	r.a.ensureFilesDoor()

	link := r.b.LiveLink(r.a.Device.ID(), nameA, func() bool { return true }, nil)
	t.Cleanup(link.Close)

	var viewerClient *remote.Client
	waitFor(t, "the viewer to connect to the host's live door", func() bool {
		c, dialErr := remote.Roam(nameA, remote.Hello{
			Surface: nameB,
			Join:    true,
		}, remote.Roaming{
			Dial: link.Opener(r.cell.ID),
		})
		if dialErr != nil {
			return false
		}
		viewerClient = c
		return true
	})
	t.Cleanup(func() { _ = viewerClient.Close() })

	if !viewerClient.Driver().Yours {
		if err := viewerClient.Take(); err != nil {
			t.Fatalf("viewerClient.Take: %v", err)
		}
	}

	const prompt = "hello from the viewer with storage off"
	viewerEvents, err := viewerClient.Agent().Submit(context.Background(), prompt)
	if err != nil {
		t.Fatalf("viewer Submit: %v", err)
	}

	var viewerGot []string
	for ev := range viewerEvents {
		if ev.Kind == session.EventTextDelta {
			viewerGot = append(viewerGot, ev.Text)
		}
	}
	if !slices.Equal(viewerGot, stub.replyTexts) {
		t.Fatalf("viewer received reply deltas %v, want %v", viewerGot, stub.replyTexts)
	}

	select {
	case hostTurn := <-hostSurface.Client.Follow():
		if hostTurn.Said != prompt {
			t.Fatalf("host surface saw prompt %q, want %q", hostTurn.Said, prompt)
		}
		var hostGot []string
		for ev := range hostTurn.Events {
			if ev.Kind == session.EventTextDelta {
				hostGot = append(hostGot, ev.Text)
			}
		}
		if !slices.Equal(hostGot, stub.replyTexts) {
			t.Fatalf("host surface received reply deltas %v, want %v", hostGot, stub.replyTexts)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("host surface did not receive the followed turn from the viewer")
	}

	if got := stub.submissions(); !slices.Equal(got, []string{prompt}) {
		t.Fatalf("stub agent submissions = %v, want exactly [%q]", got, prompt)
	}

	for _, e := range r.wire.snapshot() {
		if strings.HasPrefix(e.Path, "/v1/store") {
			t.Fatalf("unexpected store request during live session: %s %s", e.Method, e.Path)
		}
	}
	if !bytes.Contains(r.tap.bytes(), []byte("Codeaf-Door: live")) {
		t.Fatal("wire tap saw no dial with header Codeaf-Door: live")
	}
}

// TestALiveSessionAddsNoStoredObjectAtTheRelay verifies contract V5: across an
// entire live session, the relay request log contains no /v1/store path and no
// write to any per-chat record route.
func TestALiveSessionAddsNoStoredObjectAtTheRelay(t *testing.T) {
	t.Setenv("CODEAF_REMOTE_CONTROL", "on")
	r := newLiveRig(t)
	allowViewerDrive(t, r)

	stub := &stubLiveAgent{
		title:      "Zero Relay Storage Chat",
		replyTexts: []string{"delta-one", "delta-two"},
	}
	hostHeldChat(t, r, r.cell.ID, stub)
	r.a.ensureFilesDoor()

	before := len(r.wire.snapshot())

	link := r.b.LiveLink(r.a.Device.ID(), nameA, func() bool { return true }, nil)
	t.Cleanup(link.Close)

	var viewerClient *remote.Client
	waitFor(t, "the viewer to connect to the host's live door", func() bool {
		c, err := remote.Roam(nameA, remote.Hello{
			Surface: nameB,
			Join:    true,
		}, remote.Roaming{
			Dial: link.Opener(r.cell.ID),
		})
		if err != nil {
			return false
		}
		viewerClient = c
		return true
	})

	if !viewerClient.Driver().Yours {
		mustNil(t, viewerClient.Take())
	}
	events, err := viewerClient.Agent().Submit(context.Background(), "check relay writes")
	mustNil(t, err)
	for range events {
	}
	_ = viewerClient.Close()
	link.Close()

	for _, e := range r.wire.snapshot()[before:] {
		if strings.HasPrefix(e.Path, "/v1/store") {
			t.Fatalf("relay saw store request %s %s during live session", e.Method, e.Path)
		}
		if e.Method != http.MethodGet && strings.HasPrefix(e.Path, "/v1/dir/cells") {
			t.Fatalf("relay saw per-chat record write %s %s during live session", e.Method, e.Path)
		}
	}
}

// TestAViewerDoesNotChangeTheHostsLeaseTraffic verifies contract V5: the set of
// relay request paths and counts the host makes for lease traffic
// (heartbeat/publish) over a fixed window is identical with and without an
// attached viewer.
func TestAViewerDoesNotChangeTheHostsLeaseTraffic(t *testing.T) {
	t.Setenv("CODEAF_REMOTE_CONTROL", "on")

	measureLeaseTraffic := func(t *testing.T, withViewer bool) map[string]int {
		t.Helper()
		r := newLiveRig(t)
		allowViewerDrive(t, r)
		stub := &stubLiveAgent{
			title:      "Lease Traffic Chat",
			replyTexts: []string{"live-reply"},
		}
		hostHeldChat(t, r, r.cell.ID, stub)
		seedLiveTree(t, r.work)
		a := r.openA()
		a.mustSay("first host turn")
		r.durable(r.cell.ID, r.cell)
		r.a.ensureFilesDoor()

		if withViewer {
			link := r.b.LiveLink(r.a.Device.ID(), nameA, func() bool { return true }, nil)
			t.Cleanup(link.Close)

			var viewerClient *remote.Client
			waitFor(t, "the viewer to attach", func() bool {
				c, err := remote.Roam(nameA, remote.Hello{
					Surface: nameB,
					Join:    true,
				}, remote.Roaming{
					Dial: link.Opener(r.cell.ID),
				})
				if err != nil {
					return false
				}
				viewerClient = c
				return true
			})
			t.Cleanup(func() { _ = viewerClient.Close() })

			if !viewerClient.Driver().Yours {
				mustNil(t, viewerClient.Take())
			}
			evCh, err := viewerClient.Agent().Submit(context.Background(), "viewer turn while host holds lease")
			mustNil(t, err)
			for range evCh {
			}
		}

		// Record the host's lease traffic (publish/heartbeat) across a second
		// durable turn on the host.
		start := len(r.wire.snapshot())
		a.mustSay("second host turn")
		r.durable(r.cell.ID, r.cell)

		counts := map[string]int{}
		for _, e := range r.wire.snapshot()[start:] {
			if strings.HasPrefix(e.Path, "/v1/dir/cells/") {
				parts := strings.Split(strings.TrimPrefix(e.Path, "/v1/dir/cells/"), "/")
				route := "/v1/dir/cells/{id}"
				if len(parts) > 1 {
					route += "/" + parts[len(parts)-1]
				}
				counts[e.Method+" "+route]++
			}
		}
		return counts
	}

	withoutViewer := measureLeaseTraffic(t, false)
	withViewer := measureLeaseTraffic(t, true)
	if len(withoutViewer) == 0 {
		t.Fatal("host recorded no lease traffic in baseline window")
	}
	if !reflect.DeepEqual(withoutViewer, withViewer) {
		t.Fatalf("host lease traffic changed with an attached viewer:\nwithout viewer: %v\nwith viewer:    %v", withoutViewer, withViewer)
	}
}

// TestRemovingADeviceClosesItsLiveViewThroughTheRealDirectoryPath verifies
// contract V10: removing the viewer from the roster through the directory
// client ends the viewer's channel with ReasonRemoved within the roster TTL,
// and livedoor.NotifyRemoved closes an open live view at once.
func TestRemovingADeviceClosesItsLiveViewThroughTheRealDirectoryPath(t *testing.T) {
	t.Run("directory revocation closes within roster TTL", func(t *testing.T) {
		t.Setenv("CODEAF_REMOTE_CONTROL", "on")
		r := newLiveRig(t)
		liveDoorRosterFor = 20 * time.Millisecond

		stub := &stubLiveAgent{title: "Revoke Directory Chat"}
		hostHeldChat(t, r, r.cell.ID, stub)
		r.a.ensureFilesDoor()

		endedCh := make(chan string, 1)
		link := r.b.LiveLink(r.a.Device.ID(), nameA, func() bool { return true }, nil)
		t.Cleanup(link.Close)
		link.OnChannelEnd(func(_, reason string) {
			select {
			case endedCh <- reason:
			default:
			}
		})

		var viewerClient *remote.Client
		waitFor(t, "the viewer to open the live chat", func() bool {
			c, err := remote.Roam(nameA, remote.Hello{
				Surface: nameB,
				Join:    true,
			}, remote.Roaming{
				Dial: link.Opener(r.cell.ID),
			})
			if err != nil {
				return false
			}
			viewerClient = c
			return true
		})
		t.Cleanup(func() { _ = viewerClient.Close() })

		// Remove the viewer through the real directory client without calling
		// NotifyRemoved directly; the pull backstop must close the view with
		// ReasonRemoved within the shrunk roster TTL.
		mustNil(t, r.a.Dir.Revoke(context.Background(), r.b.Device.ID()))

		select {
		case reason := <-endedCh:
			if reason != livedoor.ReasonRemoved {
				t.Fatalf("viewer channel ended with reason %q, want %q", reason, livedoor.ReasonRemoved)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("viewer channel did not end within the roster TTL after directory revocation")
		}
		waitFor(t, "the link state to reach Revoked", func() bool {
			st := link.State()
			return st.Kind == livedoor.Revoked && st.Reason == livedoor.ReasonRemoved
		})
	})

	t.Run("NotifyRemoved closes at once without waiting for the roster TTL", func(t *testing.T) {
		t.Setenv("CODEAF_REMOTE_CONTROL", "on")
		r := newLiveRig(t)
		// Set a long roster TTL so only NotifyRemoved can close the channel now.
		liveDoorRosterFor = 10 * time.Second

		stub := &stubLiveAgent{title: "NotifyRemoved Chat"}
		hostHeldChat(t, r, r.cell.ID, stub)
		r.a.ensureFilesDoor()

		endedCh := make(chan string, 1)
		link := r.b.LiveLink(r.a.Device.ID(), nameA, func() bool { return true }, nil)
		t.Cleanup(link.Close)
		link.OnChannelEnd(func(_, reason string) {
			select {
			case endedCh <- reason:
			default:
			}
		})

		var viewerClient *remote.Client
		waitFor(t, "the viewer to open the live chat", func() bool {
			c, err := remote.Roam(nameA, remote.Hello{
				Surface: nameB,
				Join:    true,
			}, remote.Roaming{
				Dial: link.Opener(r.cell.ID),
			})
			if err != nil {
				return false
			}
			viewerClient = c
			return true
		})
		t.Cleanup(func() { _ = viewerClient.Close() })

		livedoor.NotifyRemoved(r.a.Home, r.b.Device.ID())

		select {
		case reason := <-endedCh:
			if reason != livedoor.ReasonRemoved {
				t.Fatalf("viewer channel ended with reason %q, want %q", reason, livedoor.ReasonRemoved)
			}
		case <-time.After(time.Second):
			t.Fatal("livedoor.NotifyRemoved did not close the viewer channel at once")
		}
	})
}

// TestTheDoorStartsForRemoteControlEvenWithStorageOffAndNoPassThrough verifies
// the door start condition: when storage is off and blind pass-through is
// switched off, turning remote control on still starts the door so a paired
// viewer can dial live.
func TestTheDoorStartsForRemoteControlEvenWithStorageOffAndNoPassThrough(t *testing.T) {
	r := newLiveRig(t)
	t.Setenv(LiveTransferVar, "off")
	t.Setenv("CODEAF_REMOTE_CONTROL", "on")

	if r.a.liveCarries() {
		t.Fatal("liveCarries() is true with CODEAF_PASSTHROUGH=off")
	}
	if !r.a.remoteControlWanted() {
		t.Fatal("remoteControlWanted() is false with CODEAF_REMOTE_CONTROL=on and another paired device")
	}

	r.a.ensureFilesDoor()

	waitFor(t, "the host door to register at the relay for remote control", func() bool {
		tun, err := filesdoor.DialLive(context.Background(), r.a.Relay, r.b.Identity, r.b.Device, r.a.Device.ID())
		if err != nil {
			return false
		}
		_ = tun.Close()
		return true
	})
}

// TestTheDoorDoesNotStartWhenRemoteControlIsOffAndPassThroughIsOff verifies the
// door start condition: when both blind pass-through and remote control are
// switched off, ensureFilesDoor starts no door and makes no relay registration.
func TestTheDoorDoesNotStartWhenRemoteControlIsOffAndPassThroughIsOff(t *testing.T) {
	r := newLiveRig(t)
	t.Setenv(LiveTransferVar, "off")
	t.Setenv("CODEAF_REMOTE_CONTROL", "off")

	if r.a.liveCarries() {
		t.Fatal("liveCarries() is true with CODEAF_PASSTHROUGH=off")
	}
	if r.a.remoteControlWanted() {
		t.Fatal("remoteControlWanted() is true with CODEAF_REMOTE_CONTROL=off")
	}

	beforeReg := r.wire.registrations()
	r.a.ensureFilesDoor()

	filesDoors.Lock()
	_, running := filesDoors.runs[r.a.Home]
	filesDoors.Unlock()
	if running {
		t.Fatal("ensureFilesDoor started a door run with both pass-through and remote control off")
	}
	if got := r.wire.registrations(); got != beforeReg {
		t.Fatalf("relay registrations = %d, want %d (no registration)", got, beforeReg)
	}
	if _, err := filesdoor.DialLive(context.Background(), r.a.Relay, r.b.Identity, r.b.Device, r.a.Device.ID()); !errors.Is(err, relaypkg.ErrNoMachine) {
		t.Fatalf("DialLive = %v, want relay.ErrNoMachine", err)
	}
}

// TestRemoteControlNeverChangesHowChatsSync verifies that toggling
// CODEAF_REMOTE_CONTROL on and off never alters liveCarries, LiveTransfer,
// ChatStorageOff, or the Scope returned by chatScope.
func TestRemoteControlNeverChangesHowChatsSync(t *testing.T) {
	r := newLiveRig(t)
	for _, passthrough := range []string{"on", "off"} {
		t.Setenv(LiveTransferVar, passthrough)

		t.Setenv("CODEAF_REMOTE_CONTROL", "off")
		carriesOff := r.a.liveCarries()
		transferOff := r.a.LiveTransfer()
		storageOff := r.a.ChatStorageOff()
		scopeOff := r.a.chatScope(r.cell.ID)

		t.Setenv("CODEAF_REMOTE_CONTROL", "on")
		carriesOn := r.a.liveCarries()
		transferOn := r.a.LiveTransfer()
		storageOn := r.a.ChatStorageOff()
		scopeOn := r.a.chatScope(r.cell.ID)

		if carriesOff != carriesOn || transferOff != transferOn || storageOff != storageOn {
			t.Fatalf("passthrough=%s: sync predicates changed with CODEAF_REMOTE_CONTROL: off=(%v,%v,%v) on=(%v,%v,%v)",
				passthrough, carriesOff, transferOff, storageOff, carriesOn, transferOn, storageOn)
		}
		if (scopeOff.live != nil) != (scopeOn.live != nil) || reflect.TypeOf(scopeOff.Store) != reflect.TypeOf(scopeOn.Store) {
			t.Fatalf("passthrough=%s: chatScope store changed with CODEAF_REMOTE_CONTROL: off=%T (live=%v) on=%T (live=%v)",
				passthrough, scopeOff.Store, scopeOff.live != nil, scopeOn.Store, scopeOn.live != nil)
		}
	}
}

// TestTheFirstFullRosterReadSnapshotsTheLegacyDevicesOnce verifies contract
// (legacy snapshot): the first full roster read for a non-solo identity writes
// the two existing devices into the legacy snapshot once, a third device that
// joins later resolves to ClassNew with drive on, and a second process-level
// read does not rewrite the permit book.
func TestTheFirstFullRosterReadSnapshotsTheLegacyDevicesOnce(t *testing.T) {
	r := newLiveRig(t)
	idID := r.a.Identity.ID()
	book := pair.Permits()

	for _, devID := range []string{r.a.Device.ID(), r.b.Device.ID()} {
		p, class := book.For(idID, devID)
		if !p.Watch || p.Drive || class != pair.ClassLegacy {
			t.Fatalf("For(%q) after first roster read = (%+v, %v), want Watch=true Drive=false ClassLegacy", devID, p, class)
		}
	}

	permitPath := filepath.Join(home.Dir(), "v3", "remote", "permits.json")
	beforeBytes, err := os.ReadFile(permitPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", permitPath, err)
	}

	// Add a third device to the directory after the initial snapshot.
	homeC := t.TempDir()
	if _, err := identity.Adopt(homeC, r.a.Identity, false); err != nil {
		t.Fatal(err)
	}
	syncC := openSync(t, homeC)
	ctx := context.Background()
	mustNil(t, syncC.putDevice(ctx, "tablet"))

	// A second full roster read in this process must not rewrite permits.json,
	// and the new device must be ClassNew (Watch:true, Drive:true).
	if _, err := r.a.Dir.List(ctx); err != nil {
		t.Fatal(err)
	}
	afterBytes, err := os.ReadFile(permitPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeBytes, afterBytes) {
		t.Fatalf("second roster read rewrote permits.json:\nbefore: %s\nafter:  %s", beforeBytes, afterBytes)
	}

	pNew, classNew := book.For(idID, syncC.Device.ID())
	if !pNew.Watch || !pNew.Drive || classNew != pair.ClassNew {
		t.Fatalf("For(later device %q) = (%+v, %v), want Watch=true Drive=true ClassNew", syncC.Device.ID(), pNew, classNew)
	}
}

// TestAPermitEditedByAnotherProcessReachesAnOpenViewWithinTheWatchInterval
// verifies contract V10/V11: when another process turns Drive off in
// permits.json on disk, the door's PermitBook.Watch goroutine notifies the live
// registry within the watch interval and the viewer's open channel ends with
// permit-changed.
func TestAPermitEditedByAnotherProcessReachesAnOpenViewWithinTheWatchInterval(t *testing.T) {
	oldInterval := livePermitWatchEvery
	livePermitWatchEvery = 15 * time.Millisecond
	t.Cleanup(func() { livePermitWatchEvery = oldInterval })

	t.Setenv("CODEAF_REMOTE_CONTROL", "on")
	r := newLiveRig(t)
	allowViewerDrive(t, r)

	stub := &stubLiveAgent{title: "Permit Watch Chat"}
	hostHeldChat(t, r, r.cell.ID, stub)
	r.a.ensureFilesDoor()

	endedCh := make(chan string, 1)
	link := r.b.LiveLink(r.a.Device.ID(), nameA, func() bool { return true }, nil)
	t.Cleanup(link.Close)
	link.OnChannelEnd(func(_, reason string) {
		select {
		case endedCh <- reason:
		default:
		}
	})

	openCh := link.Opener(r.cell.ID)
	var ch interface {
		Read([]byte) (int, error)
		Write([]byte) (int, error)
		Close() error
	}
	waitFor(t, "the viewer to open a drive-capable channel", func() bool {
		rwc, err := openCh()
		if err != nil {
			return false
		}
		ch = rwc
		return true
	})
	t.Cleanup(func() { _ = ch.Close() })

	helloPayload, err := json.Marshal(remote.Hello{Version: remote.Version, Join: true})
	mustNil(t, err)
	helloFrame, err := json.Marshal(remote.Frame{Kind: "hello", Payload: helloPayload})
	mustNil(t, err)
	if _, err := ch.Write(append(helloFrame, '\n')); err != nil {
		t.Fatalf("writing viewer hello: %v", err)
	}

	readDone := make(chan error, 1)
	guard.Go("syncsetup.test.permitWatchRead", func() {
		buf := make([]byte, 4096)
		// First read consumes the welcome line; subsequent read waits for the
		// channel reset when Drive is turned off on disk.
		for {
			_, rErr := ch.Read(buf)
			if rErr != nil {
				readDone <- rErr
				return
			}
		}
	})

	// Wait until the link reports Live (Drive:true) before editing permits.json.
	waitFor(t, "the link state to report Live with Drive:true", func() bool {
		st := link.State()
		return st.Kind == livedoor.Live && st.Permit.Drive
	})

	// Another PermitBook instance (simulating the CLI in another process) turns
	// Drive off on disk.
	driveOff := false
	otherProcessBook := pair.Permits()
	mustNil(t, otherProcessBook.Set(r.a.Identity.ID(), r.b.Device.ID(), nil, &driveOff, "person"))

	select {
	case reason := <-endedCh:
		if reason != livedoor.ReasonPermitChanged {
			t.Fatalf("channel ended with reason %q, want %q", reason, livedoor.ReasonPermitChanged)
		}
	case rErr := <-readDone:
		if reason, ok := livedoor.ReasonOf(rErr); !ok || reason != livedoor.ReasonPermitChanged {
			t.Fatalf("channel read ended with %v (reason=%q), want %q", rErr, reason, livedoor.ReasonPermitChanged)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("open channel was not reset with permit-changed within the watch interval")
	}
}
