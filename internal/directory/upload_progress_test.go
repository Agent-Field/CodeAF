package directory_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
)

// A beat carries the upload's progress only to a relay that advertises it;
// an older relay never sees the fields, and one that does keeps them on the
// lease for other computers to read.
func TestHTTPBeatUploadProgressOnlyWhereAdvertised(t *testing.T) {
	clock := directorytest.NewFakeClock()
	next := directory.Handler(fakeAuth, newNamespaces(clock).open)
	var supported atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if !supported.Load() && bytes.Contains(raw, []byte(`"upload_`)) {
			t.Error("an older relay received upload progress")
			http.Error(w, `{"err":"bad_request"}`, http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		reply := httptest.NewRecorder()
		next.ServeHTTP(reply, r)
		for key, values := range reply.Header() {
			if supported.Load() || !strings.EqualFold(key, directory.UploadProgressHeader) {
				w.Header()[key] = values
			}
		}
		w.WriteHeader(reply.Code)
		_, _ = w.Write(reply.Body.Bytes())
	}))
	defer srv.Close()
	client := directory.NewHTTP(srv.URL, fakeSign("id_one", devA), srv.Client())
	v, err := client.Create(t.Context(), cellX, directory.CellInit{Head: head1})
	if err != nil {
		t.Fatal(err)
	}
	beat := directory.Beat{Fence: v.Cell.Lease.Fence, Pending: 1, UploadSent: 412, UploadTotal: 1433}
	got, err := client.Heartbeat(t.Context(), cellX, beat)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cell.Lease.UploadTotal != 0 {
		t.Fatalf("an older relay's lease shows progress: %+v", got.Cell.Lease)
	}
	supported.Store(true)
	if _, err := client.Cell(t.Context(), cellX); err != nil { // a read learns the capability
		t.Fatal(err)
	}
	if got, err = client.Heartbeat(t.Context(), cellX, beat); err != nil {
		t.Fatal(err)
	}
	if got.Cell.Lease.UploadSent != 412 || got.Cell.Lease.UploadTotal != 1433 {
		t.Fatalf("the lease after a beat = %+v, want 412 of 1433", got.Cell.Lease)
	}
}

// A record's sealed folder goes only to a relay that advertises it.
func TestHTTPFolderOnlyWhereAdvertised(t *testing.T) {
	clock := directorytest.NewFakeClock()
	next := directory.Handler(fakeAuth, newNamespaces(clock).open)
	var supported atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if !supported.Load() && bytes.Contains(raw, []byte(`"folder"`)) {
			t.Error("an older relay received a folder")
			http.Error(w, `{"err":"bad_request"}`, http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		reply := httptest.NewRecorder()
		next.ServeHTTP(reply, r)
		for key, values := range reply.Header() {
			if supported.Load() || !strings.EqualFold(key, directory.ChatFoldersHeader) {
				w.Header()[key] = values
			}
		}
		w.WriteHeader(reply.Code)
		_, _ = w.Write(reply.Body.Bytes())
	}))
	defer srv.Close()
	client := directory.NewHTTP(srv.URL, fakeSign("id_one", devA), srv.Client())
	v, err := client.Create(t.Context(), cellX, directory.CellInit{Head: head1, Folder: "sealed"})
	if err != nil || v.Cell.Folder != "" {
		t.Fatalf("an older relay's create = %+v, %v", v.Cell, err)
	}
	supported.Store(true)
	if _, err := client.Cell(t.Context(), cellX); err != nil { // a read learns the capability
		t.Fatal(err)
	}
	got, err := client.Publish(t.Context(), cellX, directory.Publish{Fence: v.Cell.Lease.Fence, OldHead: head1, Head: head2, Folder: "sealed"})
	if err != nil || got.Cell.Folder != "sealed" {
		t.Fatalf("a relay that advertises folders kept %+v, %v", got.Cell, err)
	}
}
