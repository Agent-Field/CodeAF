//go:build !windows

package netpolicy

import (
	"bufio"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestBlackholeAnswersWithPolicy403(t *testing.T) {
	address, err := BlackholeAddr()
	if err != nil {
		t.Skipf("sandbox blocks loopback listeners: %v", err)
	}
	connection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatalf("dial blackhole: %v", err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := connection.Write([]byte("GET http://example.com/ HTTP/1.1\r\nHost: example.com\r\n\r\n")); err != nil {
		t.Fatalf("write request: %v", err)
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.StatusCode)
	}
	body := make([]byte, 512)
	n, _ := response.Body.Read(body)
	if !strings.Contains(string(body[:n]), "[network-policy]") {
		t.Fatalf("body %q missing policy marker", body[:n])
	}
}

func TestBlackholeAddrIsStableWhileAlive(t *testing.T) {
	first, err := BlackholeAddr()
	if err != nil {
		t.Skipf("sandbox blocks loopback listeners: %v", err)
	}
	second, err := BlackholeAddr()
	if err != nil {
		t.Fatalf("second BlackholeAddr: %v", err)
	}
	if first != second {
		t.Fatalf("address changed while alive: %q then %q", first, second)
	}
}

func TestShellProxyEnv(t *testing.T) {
	if entries := ShellProxyEnv(Policy{Mode: ModeAllow}); entries != nil {
		t.Fatalf("allow mode should inject nothing, got %v", entries)
	}

	entries := ShellProxyEnv(Policy{Mode: ModeOff})
	if len(entries) != 6 {
		t.Fatalf("off mode entries = %v, want 6", entries)
	}
	for _, want := range []string{"HTTP_PROXY=http://127.0.0.1:", "HTTPS_PROXY=http://127.0.0.1:", "http_proxy=", "https_proxy=", "NO_PROXY=localhost,127.0.0.1,::1", "no_proxy="} {
		found := false
		for _, entry := range entries {
			if strings.HasPrefix(entry, strings.SplitN(want, "=", 2)[0]+"=") && strings.Contains(entry, strings.SplitN(want, "=", 2)[1]) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("entries %v missing %q", entries, want)
		}
	}

}
