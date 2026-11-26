package relayserve

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func relayAnswer(t *testing.T, srv *httptest.Server, method, path string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, srv.URL+path, strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(body)
}

// A relay run with --storage off keeps the device list (the directory is mounted, so a computer's roster and its chats' places
// work) and keeps no chat: it says its storage is off and refuses every upload. Without the flag nothing about the store wire
// changes, so an unsigned request is still turned away by the signature check and not answered by this layer.
func TestStorageOffKeepsTheDirectoryAndRefusesUploads(t *testing.T) {
	off := httptest.NewServer(New(Config{Store: t.TempDir(), StorageOff: true}).Handler)
	defer off.Close()
	if code, body := relayAnswer(t, off, http.MethodGet, "/v1/store/stats"); code != http.StatusOK || !strings.Contains(body, `"storage":"off"`) {
		t.Fatalf("stats of a relay with its storage off: %d %s", code, body)
	}
	if code, body := relayAnswer(t, off, http.MethodPost, "/v1/store/frames"); code != http.StatusForbidden || !strings.Contains(body, "storage_off") {
		t.Fatalf("an upload to a relay with its storage off: %d %s", code, body)
	}
	if code, _ := relayAnswer(t, off, http.MethodGet, "/v1/dir/list"); code == http.StatusNotFound {
		t.Fatal("the device list must still be served: the directory was not mounted")
	}

	on := httptest.NewServer(New(Config{Store: t.TempDir()}).Handler)
	defer on.Close()
	if code, body := relayAnswer(t, on, http.MethodGet, "/v1/store/stats"); code != http.StatusUnauthorized {
		t.Fatalf("with storage on the store wire is the real one and wants a signature: %d %s", code, body)
	}
}

// --storage means on or off, and a typo stops the relay with a sentence instead of starting it with the opposite setting.
func TestStorageFlagIsOnOrOff(t *testing.T) {
	err := Main([]string{"--storage", "maybe"})
	if err == nil || !strings.Contains(err.Error(), "on or off") {
		t.Fatalf("a typo in --storage: %v", err)
	}
}
