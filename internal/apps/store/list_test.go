package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestListKeepsUnreadableApps(t *testing.T) {
	for _, tt := range []struct {
		name, data string
		code       bridge.ErrorCode
		message    string
	}{
		{"mismatched-folder", `{"id":"other","name":"Untrusted name","maker":"owner","space":"personal"}`, bridge.CodeInvalid, "app metadata does not match its personal folder"},
		{"corrupt-state", `{`, bridge.CodeInternal, "app metadata could not be read"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newStore(t)
			good := create(t, s)
			dir := s.appDir("bad-app")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, metadataFile), []byte(tt.data), 0600); err != nil {
				t.Fatal(err)
			}
			apps, err := s.List(testCtx, owner)
			if err != nil {
				t.Fatalf("List must isolate per-app errors: %v", err)
			}
			if len(apps) != 2 || apps[0].ID != "bad-app" || apps[0].Name != "bad-app" || apps[1] != good {
				t.Fatalf("apps = %+v, want bad row and unchanged good app", apps)
			}
			data, err := json.Marshal(apps)
			if err != nil {
				t.Fatal(err)
			}
			var rows []struct {
				Error *bridge.Error `json:"error"`
			}
			if err := json.Unmarshal(data, &rows); err != nil {
				t.Fatal(err)
			}
			if rows[0].Error == nil || rows[0].Error.Code != tt.code || rows[0].Error.Message != tt.message || rows[1].Error != nil {
				t.Fatalf("wire rows = %s", data)
			}
			others, err := s.List(testCtx, svc.Caller{Member: "someone-else"})
			if err != nil || len(others) != 0 {
				t.Fatalf("non-owner list = %+v, %v", others, err)
			}
		})
	}
}

func TestListReturnsDirectoryReadError(t *testing.T) {
	s, _ := newStore(t)
	if err := os.WriteFile(s.root, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if apps, err := s.List(testCtx, owner); err == nil || apps != nil {
		t.Fatalf("List = %+v, %v; want directory error", apps, err)
	}
}

func TestListSkipsFolderWithoutMetadata(t *testing.T) {
	s, _ := newStore(t)
	good := create(t, s)
	if err := os.Mkdir(s.appDir("half-made"), 0700); err != nil {
		t.Fatal(err)
	}
	apps, err := s.List(testCtx, owner)
	if err != nil || len(apps) != 1 || apps[0] != good {
		t.Fatalf("List = %+v, %v; want only the good app", apps, err)
	}
}
