package cellstore

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"syscall"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/syncwords"
)

// L4-H3a, L4-H3b, L4-H3c: a local seal cannot announce a recovered sync service.
func TestL4SyncOutageNeedsNetworkRecovery(t *testing.T) {
	var w SealWatch
	cause := &url.Error{Op: "Put", URL: "http://127.0.0.1:8901/v1/dir/devices/private", Err: syscall.ECONNREFUSED}
	w.Report(cause)
	want := "the relay at 127.0.0.1:8901 is not answering; this chat keeps working here and catches up when it is back"
	if got := w.Take(); got != want {
		t.Fatal(got)
	}
	w.Report(errors.Join(cause, errors.New("another retry")))
	w.Report(nil)
	if got := w.Take(); got != "" {
		t.Fatalf("false recovery or repeated outage: %s", got)
	}
	w.SyncReport(nil)
	if got := w.Take(); got != "syncing again" {
		t.Fatal(got)
	}
	if got := w.Take(); got != "" {
		t.Fatal(got)
	}
}

func TestQAHTTPStoreFailuresReachOutageNotices(t *testing.T) {
	for _, status := range []int{500, 502, 503, 504} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "service unavailable", status)
			}))
			defer server.Close()
			store := blobstore.NewHTTP(server.URL, func(*http.Request, []byte) {}, server.Client())
			_, err := store.Stats(t.Context())
			if err == nil {
				t.Fatal("a failed stats call succeeded")
			}
			w := SealWatch{SyncAddress: server.URL}
			w.SyncReport(err)
			if got, want := w.Take(), syncwords.Outage(server.URL); got != want {
				t.Fatalf("outage notice = %q, want %q", got, want)
			}
			w.SyncReport(err)
			w.Report(nil)
			if got := w.Take(); got != "" {
				t.Fatalf("repeated failure or local save produced %q", got)
			}
			w.SyncReport(nil)
			if got := w.Take(); got != syncwords.Recovered {
				t.Fatalf("recovery notice = %q", got)
			}
		})
	}
}
