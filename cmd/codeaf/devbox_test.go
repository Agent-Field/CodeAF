package main

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/devbox"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/pairbox"
)

// R6.1: only a release on the chosen stable, staging or dev channel pins the first install.
func TestDevboxR61FirstInstallVersionMatchesChannel(t *testing.T) {
	for _, row := range []struct{ name, revision, channel, want string }{
		{"stable", "v1.2.3", "stable", "v1.2.3"},
		{"staging", "staging-20261030-aaaaaaaaaaaa", "staging", "staging-20261030-aaaaaaaaaaaa"},
		{"dev", "dev-20261030-aaaaaaaaaaaa", "dev", "dev-20261030-aaaaaaaaaaaa"},
		{"stable-to-staging", "v1.2.3", "staging", ""},
		{"stable-to-dev", "v1.2.3", "dev", ""},
		{"staging-to-stable", "staging-20261030-aaaaaaaaaaaa", "stable", ""},
		{"staging-to-dev", "staging-20261030-aaaaaaaaaaaa", "dev", ""},
		{"dev-to-stable", "dev-20261030-aaaaaaaaaaaa", "stable", ""},
		{"dev-to-staging", "dev-20261030-aaaaaaaaaaaa", "staging", ""},
		{"release-candidate", "v1.2.3-rc.1", "stable", ""},
		{"rc-is-not-offered", "v1.2.3-rc.1", "rc", ""},
		{"source-commit", "abcdef12", "dev", ""},
		{"source-unstamped", "", "dev", ""},
		{"source-word", "dev", "dev", ""},
		{"other", "custom-build", "other", ""},
	} {
		t.Run(row.name, func(t *testing.T) {
			if got := devboxVersion(row.revision, row.channel); got != row.want {
				t.Fatalf("R6.1 version = %q, want %q", got, row.want)
			}
		})
	}
}

// Contracts 1 and 7: each command exposes its actual flags without touching a provider.
func TestDevboxHelpAndChannelDefaults(t *testing.T) {
	for _, action := range []string{"up", "status", "down"} {
		t.Run(action, func(t *testing.T) {
			out, errs := captureUsage(t)
			err := devboxCommand(context.Background(), []string{action, "--help"}, devbox.Flow{})
			if exitCodeOf(err) != 0 || errs.Len() != 0 {
				t.Fatalf("help: %v %s", err, errs)
			}
			for _, flag := range []string{"codeaf devbox " + action, "--dir", "--yes"} {
				if !strings.Contains(out.String(), flag) {
					t.Fatalf("missing %s: %s", flag, out)
				}
			}
			if (action == "up") != strings.Contains(out.String(), "--channel") {
				t.Fatalf("wrong flags: %s", out)
			}
		})
	}
	for _, row := range []struct{ revision, want string }{{"source", "dev"}, {"dev-20261001-aaaaaaaaaaaa", "dev"}, {"staging-20261001-aaaaaaaaaaaa", "staging"}, {"v1.2.3", "stable"}, {"v1.2.3-rc.1", "stable"}} {
		if got := devboxChannel(row.revision); got != row.want {
			t.Errorf("%s: %s != %s", row.revision, got, row.want)
		}
	}
}

// Contract 3: only the requested name may receive the unattended identity offer.
func TestDevboxOfferDoesNotPrintItsCodeOrAdmitAnotherName(t *testing.T) {
	var code, via string
	cancelled := false
	ui := devboxOfferUI{name: "build-box", offer: devbox.Offer{Via: "https://relay.example"}, ready: func(o devbox.Offer) { code, via = o.Code, o.Via }, cancel: func() { cancelled = true }}
	fresh, err := pair.NewCode(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ui.Show(fresh.WithPlate("42"), "")
	if code != "42-"+strings.ReplaceAll(fresh.Shown(), " ", "-") || via != "https://relay.example" {
		t.Fatalf("ready %q %q", code, via)
	}
	if !ui.Ask(context.Background(), "build-box", "") || ui.Ask(context.Background(), "stranger", "") {
		t.Fatal("wrong admission")
	}
	ui.Burned()
	if !cancelled {
		t.Fatal("burned code was left live")
	}
}

// Contract 6: an idle serving computer refreshes its own directory record until stopped.
func TestDevboxServeRefreshesItsOwnRecord(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	calls := make(chan struct{}, 4)
	done := make(chan struct{})
	go func() {
		defer close(done)
		refreshServeRecord(ctx, ticks, func(context.Context) error { calls <- struct{}{}; return nil })
	}()
	await := func() {
		t.Helper()
		select {
		case <-calls:
		case <-time.After(time.Second):
			t.Fatal("no refresh")
		}
	}
	await()
	ticks <- time.Now()
	await()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("refresh survived cancellation")
	}
	if len(calls) != 0 {
		t.Fatal("refresh after stop")
	}
}

// Contract 1: the command prints exactly one prerequisite sentence and exits nonzero.
func TestDevboxPrerequisiteSentenceIsPrintedOnce(t *testing.T) {
	for _, action := range []string{"up", "down"} {
		out, errs := captureUsage(t)
		f := devbox.Flow{Getenv: func(string) string { return "" }, LookPath: func(string) (string, error) { t.Fatal("terraform before token"); return "", nil }}
		err := devboxCommand(context.Background(), []string{action, "box"}, f)
		if exitCodeOf(err) == 0 || out.Len() != 0 || errs.String() != devbox.TokenNeeded+"\n" {
			t.Fatalf("exit %v, stdout %q, stderr %q", err, out.String(), errs.String())
		}
	}
}

// R5.2: serving shares one gated follower across refreshes and closes it synchronously.
func TestDevboxR52ServePresenceWatchLifetimeAndBoundedRefresh(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time, 32)
	refreshes := make(chan struct{}, 32)
	stream := &devboxPresenceStream{closed: make(chan struct{}), readerDone: make(chan struct{}), started: make(chan struct{})}
	var dials atomic.Int32
	dial := func(context.Context) (dirwatch.Stream, error) { dials.Add(1); return stream, nil }
	done := make(chan struct{})
	go func() {
		defer close(done)
		servePresence(ctx, dirwatch.Follow(t.Name(), dial), ticks, func(context.Context) error { refreshes <- struct{}{}; return nil })
	}()
	await := func(ch <-chan struct{}) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatal("presence did not advance")
		}
	}
	await(refreshes)
	await(stream.started)
	// A second follower must share this process's socket, including while records refresh.
	other := dirwatch.Follow(t.Name(), dial)
	defer other.Close()
	for range 20 {
		ticks <- time.Now()
		await(refreshes)
	}
	select {
	case <-refreshes:
		t.Fatal("refresh spins without a tick")
	default:
	}
	cancel()
	await(done)
	select {
	case <-stream.closed:
		t.Fatal("closed a shared watch")
	default:
	}
	other.Close()
	await(stream.closed)
	await(stream.readerDone)
	if dials.Load() != 1 {
		t.Fatalf("watch opened %d times", dials.Load())
	}

	// Buffered ticks cannot create workers while the current refresh is blocked.
	blockedCtx, stop := context.WithCancel(context.Background())
	defer stop()
	var calls atomic.Int32
	entered := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		refreshServeRecord(blockedCtx, ticks, func(ctx context.Context) error {
			if calls.Add(1) == 1 {
				close(entered)
			}
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	await(entered)
	for range 20 {
		ticks <- time.Now()
	}
	stop()
	await(stopped)
	if calls.Load() != 1 {
		t.Fatalf("refresh workers survived cancellation: %d", calls.Load())
	}
	// A closed ticker also ends the loop instead of spinning.
	for len(ticks) > 0 {
		<-ticks
	}
	close(ticks)
	refreshServeRecord(context.Background(), ticks, func(context.Context) error {
		if calls.Add(1) > 3 {
			t.Fatal("closed ticker spins")
		}
		return nil
	})
}

type devboxPresenceStream struct{ closed, readerDone, started chan struct{} }

func (s *devboxPresenceStream) Next(ctx context.Context) (dirwatch.Frame, error) {
	defer close(s.readerDone)
	close(s.started)
	<-ctx.Done()
	return dirwatch.Frame{}, ctx.Err()
}
func (*devboxPresenceStream) Ping(context.Context) error { return nil }
func (*devboxPresenceStream) Vouching() bool             { return false }
func (s *devboxPresenceStream) Close()                   { close(s.closed) }

// R5.3: remote configuration and its alias are prepared before offering an unchanged identity grant.
func TestDevboxR53OfferKeepsRemoteConfigurationOutsideIdentityGrant(t *testing.T) {
	for _, mode := range []string{"joined", "other-name", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			laptopHome := t.TempDir()
			t.Setenv("CODEAF_HOME", laptopHome)
			id, err := identity.Ensure(laptopHome)
			if err != nil {
				t.Fatal(err)
			}
			route := pair.Mailbox{URL: "https://relay.example", Host: "relay.example", Box: pairbox.NewMemory(pairbox.DefaultLimits, nil)}
			door := pairDoor{term: newTerminal(strings.NewReader(""), io.Discard), mailbox: func(string) (pair.Mailbox, error) { return route, nil }, grant: func(pair.Mailbox) (pair.Grant, error) { return pair.Grant{Identity: id, SyncURL: route.URL}, nil }}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			remote, err := prepareDevbox(door, "build-box")
			if err != nil {
				t.Fatal(err)
			}
			ready := make(chan devbox.Offer, 1)
			done := make(chan error, 1)
			go func() { done <- offerDevbox(ctx, door, "build-box", func(o devbox.Offer) { ready <- o }) }()
			var offer devbox.Offer
			select {
			case offer = <-ready:
			case err := <-done:
				t.Fatalf("offer ended: %v", err)
			case <-ctx.Done():
				t.Fatal("no offer")
			}
			if offer.Code == "" || offer.Via != route.URL {
				t.Fatal("missing identity offer")
			}
			for _, blob := range []string{remote.RemoteKey, remote.RemoteDevices} {
				if raw, err := base64.StdEncoding.DecodeString(blob); err != nil || len(raw) == 0 {
					t.Fatal("missing remote configuration")
				}
			}
			if _, found, err := pair.MachineBook().Machine("build-box"); err != nil || !found {
				t.Fatal("R5.3 alias not pinned before join")
			}
			if mode == "cancelled" {
				cancel()
			} else {
				name := "build-box"
				if mode == "other-name" {
					name = "other"
				}
				boxHome := t.TempDir()
				_, err := pair.Join(ctx, route, pair.Joining{Home: boxHome, Label: name}, offer.Code, devboxJoinScreen{})
				if (err == nil) != (mode == "joined") {
					t.Fatalf("join: %v", err)
				}
				if _, err := os.Stat(filepath.Join(boxHome, "v3", "remote", "device.key")); !os.IsNotExist(err) {
					t.Fatal("identity join installed remote access")
				}
			}
			select {
			case err := <-done:
				if (err == nil) != (mode == "joined") {
					t.Fatalf("offer: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("offer did not finish")
			}
			known, found, err := pair.MachineBook().Machine("build-box")
			if err != nil || !found {
				t.Fatalf("alias after offer: %v %v", found, err)
			}
			if found && (known.Alias != "build-box" || known.Name == known.Alias || known.Service != route.URL) {
				t.Fatal("alias does not pin the box")
			}
		})
	}
}

type devboxJoinScreen struct{}

func (devboxJoinScreen) Waiting(string) {}

// R5.8: compact root usage and full command usage expose their applicable flags.
func TestDevboxR58UsageShowsYesAndDir(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}} {
		out, errs := captureUsage(t)
		var err error
		if args == nil {
			err = usage(nil)
		} else {
			err = devboxCommand(context.Background(), args, devbox.Flow{})
		}
		if err != nil && exitCodeOf(err) != 0 || errs.Len() != 0 {
			t.Fatalf("help: %v %s", err, errs)
		}
		var rows []string
		for _, row := range strings.Split(out.String(), "\n") {
			if strings.Contains(row, "codeaf devbox") {
				rows = append(rows, row)
			}
		}
		if args == nil {
			if len(rows) != 1 || !strings.Contains(rows[0], "up | down <name> · status [<name>]") || !strings.Contains(rows[0], "--yes") || !strings.Contains(rows[0], "--dir") {
				t.Fatalf("R5.8 root usage: %s", out)
			}
			continue
		}
		for _, flag := range []string{"--region", "--workspace", "--channel"} {
			if !strings.Contains(out.String(), flag) {
				t.Errorf("R5.8 full devbox help omits %s", flag)
			}
		}
		for _, action := range []string{"up", "status", "down"} {
			var shape string
			for _, row := range rows {
				if strings.Contains(row, "codeaf devbox "+action) {
					shape = row
				}
			}
			if !strings.Contains(shape, "--dir") || action != "status" && !strings.Contains(shape, "--yes") {
				t.Errorf("devbox %s usage omits its flags: %s", action, out)
			}
		}
	}
}

// R3.3: expired or vanished identity joins map to the retry sentinel without saving an alias.
func TestDevboxR33OfferMapsInterruptedJoin(t *testing.T) {
	for _, cause := range []error{pairbox.ErrGone, context.DeadlineExceeded, pairbox.ErrForbidden} {
		t.Run(cause.Error(), func(t *testing.T) {
			laptopHome := t.TempDir()
			t.Setenv("CODEAF_HOME", laptopHome)
			id, err := identity.Ensure(laptopHome)
			if err != nil {
				t.Fatal(err)
			}
			box := devboxInterruptedMailbox{Box: pairbox.NewMemory(pairbox.DefaultLimits, nil), err: cause}
			route := pair.Mailbox{URL: "https://relay.example", Host: "relay.example", Box: box}
			door := pairDoor{term: newTerminal(strings.NewReader(""), io.Discard), mailbox: func(string) (pair.Mailbox, error) { return route, nil }, grant: func(pair.Mailbox) (pair.Grant, error) { return pair.Grant{Identity: id, SyncURL: route.URL}, nil }}
			err = offerDevbox(context.Background(), door, "build-box", func(devbox.Offer) {})
			if errors.Is(err, devbox.ErrJoinInterrupted) != (cause != pairbox.ErrForbidden) {
				t.Fatalf("mapped result: %v", err)
			}
			if _, found, err := pair.MachineBook().Machine("build-box"); err != nil || found {
				t.Fatal("failed offer remembered alias")
			}
		})
	}
}

type devboxInterruptedMailbox struct {
	pairbox.Box
	err error
}

func (b devboxInterruptedMailbox) Poll(context.Context, string, pairbox.Side, int, time.Duration) (pairbox.Batch, error) {
	return pairbox.Batch{}, b.err
}

// R5.8: root help preserves section separators and the two-line command shape within existing caps.
func TestDevboxR58RootHelpKeepsSeparatorsAndDescription(t *testing.T) {
	out, _ := captureUsage(t)
	if err := usage(nil); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"\n\nLook at what happened", "\n\nExamples:", "\n  codeaf devbox up | down <name> · status [<name>]  [--yes] [--dir D]\n      an always-on computer of yours on your own Railway account\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("R5.8 help lacks %q", want)
		}
	}
}
