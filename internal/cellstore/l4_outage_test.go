package cellstore

import (
	"errors"
	"net/url"
	"syscall"
	"testing"
)

// L4-H3a, L4-H3b, L4-H3c: a local seal cannot announce a recovered sync service.
func TestL4SyncOutageNeedsNetworkRecovery(t *testing.T) {
	var w SealWatch
	cause := &url.Error{Op: "Put", URL: "http://127.0.0.1:8901/v1/dir/devices/private", Err: syscall.ECONNREFUSED}
	w.Report(cause)
	want := "the sync service at 127.0.0.1:8901 is not answering; this chat keeps working here and catches up when it is back"
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
