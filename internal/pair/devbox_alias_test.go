package pair

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/relay"
)

// Contract 3: --at resolves the chosen alias to a pinned key through a real relay.
func TestDevboxAliasOpensItsPinnedRemoteComputer(t *testing.T) {
	service := devboxTestRelay(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := ThisDevice(FileKeeper{Path: filepath.Join(t.TempDir(), "client.key")})
	if err != nil {
		t.Fatal(err)
	}
	key, devices, known, err := DevboxRemote("build-box", service, client, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	dir := filepath.Join(home, "v3", "remote")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{"device.key": key, "devices.json": devices} {
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	machine, err := ThisDevice(FileKeeper{Path: filepath.Join(home, "v3", "remote", "device.key")})
	if err != nil {
		t.Fatal(err)
	}
	known.Service = service
	book := BookAt(filepath.Join(t.TempDir(), "machines.json"))
	if err := book.Remember(known); err != nil {
		t.Fatal(err)
	}
	reachable := make(chan struct{}, 1)
	desk := &Desk{}
	host := &Host{Service: service, Device: machine, Devices: BookAt(filepath.Join(home, "v3", "remote", "devices.json")), Desk: desk, Say: func(string) {
		select {
		case reachable <- struct{}{}:
		default:
		}
	}, Open: func(conn io.ReadWriteCloser) { defer conn.Close(); _, _ = io.WriteString(conn, "hello from the box") }}
	ended := make(chan error, 1)
	go func() { ended <- host.Run(ctx) }()
	defer func() {
		cancel()
		select {
		case err := <-ended:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(time.Second):
			t.Error("host did not stop")
		}
	}()
	select {
	case <-reachable:
	case <-ctx.Done():
		t.Fatal("host never registered")
	}
	tunnel, err := (Reach{Name: "build-box", Device: client, Machines: book, Label: "laptop"}).Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()
	message, err := io.ReadAll(tunnel)
	if err != nil || string(message) != "hello from the box" {
		t.Fatalf("remote message %q %v", message, err)
	}
	stranger := aDevice(t)
	// Knowing the host's key does not admit a different client key.
	if conn, err := (Reach{Name: "build-box", Device: stranger, Machines: book, Label: "other"}).Open(ctx); err == nil {
		conn.Close()
		t.Fatal("unknown client was admitted")
	}
	code, err := desk.Offer()
	if err != nil {
		t.Fatal(err)
	}
	otherBook := BookAt(filepath.Join(t.TempDir(), "machines.json"))
	if conn, err := (Reach{Name: machine.Name(), Sync: service, Device: stranger, Machines: otherBook, Label: "other", AskCode: func(string) (string, error) { return code.Shown(), nil }}).Open(ctx); err == nil {
		conn.Close()
		t.Fatal("headless host admitted a new client")
	} else if !strings.Contains(err.Error(), "was refused there") {
		t.Fatalf("headless admission: %v", err)
	}
}

func devboxTestRelay(t *testing.T, handlers ...http.Handler) string {
	t.Helper()
	var listener net.Listener
	for port := 8900; port <= 8999; port++ {
		opened, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err == nil {
			listener = opened
			break
		}
	}
	if listener == nil {
		t.Fatal("no allowed test relay port is free")
	}
	var handler http.Handler = &relay.Server{}
	if len(handlers) > 0 {
		handler = handlers[0]
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	t.Cleanup(func() { _ = server.Close() })
	go func() { _ = server.Serve(listener) }()
	return "http://" + listener.Addr().String()

}

// Round 2: the status probe's short context also bounds an established but silent handshake.
func TestDevboxAliasProbeHonorsCancellationDuringHandshake(t *testing.T) {
	service := devboxTestRelay(t)
	machine, client := aDevice(t), aDevice(t)
	registration, err := relay.Register(context.Background(), service, machine.Private())
	if err != nil {
		t.Fatal(err)
	}
	defer registration.Close()
	book := BookAt(filepath.Join(t.TempDir(), "machines.json"))
	if err := book.Remember(Known{Name: machine.Name(), Alias: "box", Service: service, Key: base64.RawURLEncoding.EncodeToString(machine.Public())}); err != nil {
		t.Fatal(err)
	}
	accepted := make(chan struct{})
	go func() {
		conn, err := registration.Accept()
		if err == nil {
			defer conn.Close()
			var intent [1]byte
			if _, err := io.ReadFull(conn, intent[:]); err != nil {
				return
			}
			if _, err := readRecord(conn); err != nil {
				return
			}
			close(accepted)
			io.Copy(io.Discard, conn)
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		tunnel, err := (Reach{Name: "box", Device: client, Machines: book}).Open(ctx)
		if tunnel != nil {
			tunnel.Close()
		}
		done <- err
	}()
	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("probe never dialed")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("silent machine answered")
		}
	case <-time.After(time.Second):
		t.Fatal("probe ignored cancellation")
	}
}

// Round 2: a legacy relay's raw HTTP upgrade must obey the same short probe deadline.
func TestDevboxAliasProbeBoundsLegacyRelayUpgrade(t *testing.T) {
	service := devboxTestRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			w.WriteHeader(http.StatusUpgradeRequired)
			return
		}
		<-r.Context().Done()
	}))
	machine, client := aDevice(t), aDevice(t)
	book := BookAt(filepath.Join(t.TempDir(), "machines.json"))
	if err := book.Remember(Known{Name: machine.Name(), Alias: "box", Service: service, Key: base64.RawURLEncoding.EncodeToString(machine.Public())}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		tunnel, err := (Reach{Name: "box", Device: client, Machines: book}).Open(ctx)
		if tunnel != nil {
			tunnel.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("silent relay answered")
		}
	case <-time.After(time.Second):
		t.Fatal("legacy relay ignored probe deadline")
	}
}
