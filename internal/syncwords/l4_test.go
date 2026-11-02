package syncwords

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"syscall"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// TestMain stands in what internal/syncsetup hands over at startup: this build's
// hosted default, and the hosts of every release channel's hosted service.
func TestMain(m *testing.M) {
	UseHosted("https://codeaf.agentfield.ai/fabric", []string{"codeaf.agentfield.ai", "fabric-staging.example.test"})
	os.Exit(m.Run())
}

// L4-M9a: another release channel's hosted service is hosted too, never "your own",
// and an unknown address names nothing.
func TestL4EveryChannelsServiceIsHosted(t *testing.T) {
	if got := ServiceLine("https://fabric-staging.example.test"); got != "sync service: fabric-staging.example.test (hosted)" {
		t.Fatal(got)
	}
	if Hosted("") || Hosted("http://127.0.0.1:8901") {
		t.Fatal("an empty or personal address was called hosted")
	}
}

// L4-H6a through L4-H6f: connection, deadline and agreement have different remedies.
func TestL4ServiceFailures(t *testing.T) {
	for _, tc := range []struct {
		cause         error
		address, want string
	}{
		{&url.Error{Op: "Get", URL: "http://127.0.0.1:8901/private", Err: syscall.ECONNREFUSED}, "http://127.0.0.1:8901", "nothing is answering at http://127.0.0.1:8901 — if that is your own sync service, start it there with codeaf relay"},
		{&net.DNSError{Err: "no such host", Name: "mine"}, HostedURL, "cannot reach codeaf.agentfield.ai — check this computer's internet connection"},
		{context.DeadlineExceeded, "http://127.0.0.1:8901", "the sync service at http://127.0.0.1:8901 did not answer in time — try again"},
		{wireauth.ErrUnavailable, "http://127.0.0.1:8901", "the sync service at 127.0.0.1:8901 is up but cannot agree on changes right now; it usually recovers within a minute"},
		{directory.ErrUnreachable, "http://127.0.0.1:8901", "the sync service at 127.0.0.1:8901 is up but cannot agree on changes right now; it usually recovers within a minute"},
	} {
		got := Explain(tc.cause, tc.address)
		if got.Error() != tc.want || !errors.Is(got, tc.cause) {
			t.Fatalf("%v", got)
		}
	}
}

// L4-M9a: terminal and future chat lists use one exported service label.
func TestL4ServiceLine(t *testing.T) {
	if got := ServiceLine(HostedURL); got != "sync service: codeaf.agentfield.ai (hosted)" {
		t.Fatal(got)
	}
	if got := ServiceLine("http://127.0.0.1:8901"); got != "sync service: 127.0.0.1:8901 (your own)" {
		t.Fatal(got)
	}
}

// L4-H4c: a failure talking to the local storage socket is still a local save failure.
func TestL4LocalStorageIsNotSyncOutage(t *testing.T) {
	err := &net.OpError{Op: "dial", Net: "unix", Addr: &net.UnixAddr{Name: "storage.sock", Net: "unix"}, Err: syscall.ECONNREFUSED}
	if NetworkFailure(err) {
		t.Fatal("local storage failure was called an upload outage")
	}
}

// L4-H4c: a bare local drain deadline does not prove network use or local durability.
func TestL4LocalDeadlineIsNotAnUpload(t *testing.T) {
	if NetworkFailure(context.DeadlineExceeded) || UploadPending(context.DeadlineExceeded) {
		t.Fatal("local deadline was called an upload")
	}
	if !UploadPending(UploadFailure(context.DeadlineExceeded)) {
		t.Fatal("explicit local receipt was lost")
	}
}

// L4-H4c: a joined local failure cannot be hidden by a successful local receipt for another part.
func TestL4JoinedLocalFailureIsNotUploadOnly(t *testing.T) {
	err := errors.Join(errors.New("local disk full"), UploadFailure(context.DeadlineExceeded))
	if UploadPending(err) {
		t.Fatal("a mixed local/upload failure was marked safe")
	}
}
