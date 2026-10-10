package desktopbridge

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/remote"
)

func callList(t *testing.T, method, rawURL string, listDir func(string) (remote.DirListing, error)) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, rawURL, nil)
	w := httptest.NewRecorder()
	ListFiles(w, r, listDir)
	return w
}

func decodeListing(t *testing.T, w *httptest.ResponseRecorder) folderListing {
	t.Helper()
	var got folderListing
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatalf("listing: %d %s", w.Code, w.Body.String())
	}
	return got
}

func TestListFilesRelaysTheEngine(t *testing.T) {
	var asked []string
	listing := remote.DirListing{
		Path: "/engine/project/src",
		Entries: []remote.DirEntry{
			{Name: "pkg", Dir: true, ModTime: 1700000000, MIME: "inode/directory"},
			{Name: "a.go", Size: 12, ModTime: 1700000000, MIME: "text/x-go"},
			{Name: "empty", Size: 0},
		},
	}
	door := func(path string) (remote.DirListing, error) {
		asked = append(asked, path)
		return listing, nil
	}
	w := callList(t, http.MethodGet, "/files/list?path=src", door)
	got := decodeListing(t, w)
	if len(asked) != 1 || asked[0] != "src" {
		t.Fatalf("path handed to the engine: %v", asked)
	}
	if got.Path != "/engine/project/src" || got.Truncated {
		t.Fatalf("engine path and cut flag: %+v", got)
	}
	if !strings.Contains(w.Body.String(), `"truncated":false`) {
		t.Fatalf("a full listing still says it was not cut: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "mime") || strings.Contains(w.Body.String(), "inode") {
		t.Fatalf("the window row does not carry the engine's mime: %s", w.Body.String())
	}
	if len(got.Entries) != 3 || got.Entries[0].Name != "pkg" || !got.Entries[0].Dir || got.Entries[0].Size != 0 || got.Entries[0].ModTime != 1700000000 {
		t.Fatalf("directory row: %+v", got.Entries)
	}
	if got.Entries[1].Name != "a.go" || got.Entries[1].Dir || got.Entries[1].Size != 12 || got.Entries[1].ModTime != 1700000000 {
		t.Fatalf("file row: %+v", got.Entries[1])
	}
	if strings.Contains(w.Body.String(), `"name":"empty"`) && strings.Contains(w.Body.String(), `"modTime":0`) {
		t.Fatalf("a missing time is omitted, not zero: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"dir":false`) || !strings.Contains(w.Body.String(), `"size":0`) {
		t.Fatalf("dir and size stay on the row: %s", w.Body.String())
	}

	asked = nil
	if w = callList(t, http.MethodGet, "/files/list", door); decodeListing(t, w).Path != listing.Path || len(asked) != 1 || asked[0] != "." {
		t.Fatalf("a missing path lists the workspace root, asked %v", asked)
	}
	asked = nil
	if w = callList(t, http.MethodGet, "/files/list?path=", door); len(asked) != 1 || asked[0] != "." {
		t.Fatalf("a blank path lists the workspace root, asked %v", asked)
	}
	asked = nil
	callList(t, http.MethodGet, "/files/list?path=../up", door)
	if len(asked) != 1 || asked[0] != "../up" {
		t.Fatalf("a path is not rewritten before the engine sees it: %v", asked)
	}

	w = callList(t, http.MethodGet, "/files/list?path=/etc", func(string) (remote.DirListing, error) {
		return remote.DirListing{}, errors.New("engine: /etc is outside this conversation's workspace and its own folder")
	})
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "outside this conversation's workspace") {
		t.Fatalf("outside: %d %s", w.Code, w.Body.String())
	}
	w = callList(t, http.MethodGet, "/files/list?path=missing", func(string) (remote.DirListing, error) {
		return remote.DirListing{}, errors.New("engine: no such file: missing")
	})
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "no such file: missing") {
		t.Fatalf("missing: %d %s", w.Code, w.Body.String())
	}
	w = callList(t, http.MethodGet, "/files/list?path=report.md", func(string) (remote.DirListing, error) {
		return remote.DirListing{}, errors.New("engine: report.md is not a directory")
	})
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "is not a directory") {
		t.Fatalf("not a directory: %d %s", w.Code, w.Body.String())
	}

	many := make([]remote.DirEntry, listFilesCap+1)
	for i := range many {
		many[i] = remote.DirEntry{Name: "f" + strconv.Itoa(i), Size: 1}
	}
	w = callList(t, http.MethodGet, "/files/list?path=big", func(string) (remote.DirListing, error) {
		return remote.DirListing{Path: "/engine/big", Entries: many}, nil
	})
	got = decodeListing(t, w)
	if !got.Truncated || len(got.Entries) != listFilesCap || got.Entries[0].Name != "f0" || got.Entries[listFilesCap-1].Name != "f"+strconv.Itoa(listFilesCap-1) {
		t.Fatalf("the tail past %d is cut: truncated=%v len=%d first=%s last=%s", listFilesCap, got.Truncated, len(got.Entries), nameAt(got, 0), nameAt(got, len(got.Entries)-1))
	}

	w = callList(t, http.MethodGet, "/files/list?path=cut", func(string) (remote.DirListing, error) {
		return remote.DirListing{Path: "/engine/cut", Entries: many[:2], Truncated: true}, nil
	})
	got = decodeListing(t, w)
	if !got.Truncated || len(got.Entries) != 2 || got.Entries[1].Name != "f1" {
		t.Fatalf("an engine that already cut stays cut: %+v", got)
	}

	w = callList(t, http.MethodGet, "/files/list?path=empty", func(string) (remote.DirListing, error) {
		return remote.DirListing{Path: "/engine/empty"}, nil
	})
	if !strings.Contains(w.Body.String(), `"entries":[]`) || decodeListing(t, w).Path != "/engine/empty" {
		t.Fatalf("an empty folder is an empty list: %s", w.Body.String())
	}

	var routed []string
	b, _, path := richFixture(t, func(c *Connection) {
		c.ListDir = func(p string) (remote.DirListing, error) {
			routed = append(routed, p)
			return listing, nil
		}
	})
	w = request(b, http.MethodGet, path+"/files/list?path=src", "")
	got = decodeListing(t, w)
	if len(routed) != 1 || routed[0] != "src" || got.Path != listing.Path || len(got.Entries) != 3 {
		t.Fatalf("route relay: asked %v body %+v", routed, got)
	}
}

func nameAt(got folderListing, i int) string {
	if i < 0 || i >= len(got.Entries) {
		return ""
	}
	return got.Entries[i].Name
}

func TestListFilesRefusesWhenEngineCannot(t *testing.T) {
	w := callList(t, http.MethodGet, "/files/list?path=src", nil)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "this engine cannot list folders") {
		t.Fatalf("nil door: %d %s", w.Code, w.Body.String())
	}
	called := false
	w = callList(t, http.MethodPost, "/files/list", func(string) (remote.DirListing, error) {
		called = true
		return remote.DirListing{}, nil
	})
	if w.Code != http.StatusMethodNotAllowed || called {
		t.Fatalf("POST: %d called=%v %s", w.Code, called, w.Body.String())
	}

	b, _, path := richFixture(t, nil)
	w = request(b, http.MethodGet, path+"/files/list", "")
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "this engine cannot list folders") {
		t.Fatalf("connection without ListDir: %d %s", w.Code, w.Body.String())
	}
}

func TestListFilesNeedsTheToken(t *testing.T) {
	var calls atomic.Int32
	b, _, path := richFixture(t, func(c *Connection) {
		c.ListDir = func(string) (remote.DirListing, error) {
			calls.Add(1)
			return remote.DirListing{Path: "/project"}, nil
		}
	})
	for _, header := range []string{"", "Bearer nope"} {
		r := httptest.NewRequest(http.MethodGet, path+"/files/list?path=src", nil)
		if header != "" {
			r.Header.Set("Authorization", header)
		}
		w := httptest.NewRecorder()
		b.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized || calls.Load() != 0 {
			t.Fatalf("header %q: %d calls=%d %s", header, w.Code, calls.Load(), w.Body.String())
		}
	}
	w := request(b, http.MethodGet, path+"/files/list?path=src", "")
	if w.Code != http.StatusOK || calls.Load() != 1 {
		t.Fatalf("the same route with the token lists: %d calls=%d %s", w.Code, calls.Load(), w.Body.String())
	}
}
