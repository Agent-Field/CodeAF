package desktopbridge

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
)

func permissionsCall(b *Bridge, method, body, auth string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/api/engine/settings/permissions", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	b.ServeSettings(w, r)
	return w
}

func TestPermissionsModeRoundTrips(t *testing.T) {
	profile := t.TempDir()
	b := settingsBridge(t, profile)
	read := func() PermissionsStatus {
		return decodeStatus[PermissionsStatus](t, permissionsCall(b, http.MethodGet, "", "Bearer "+testToken))
	}
	if got := read(); got.Mode != config.ToolApprovalModeAt(profile) || !slices.Equal(got.Modes, config.ToolApprovalModes) {
		t.Fatalf("initial permissions = %+v", got)
	}
	// Unrelated profile fields survive the same writer used by the settings sheet.
	path := filepath.Join(profile, "config.json")
	if err := os.WriteFile(path, []byte(`{"unrelated":"keep"}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range config.ToolApprovalModes {
		t.Run(mode, func(t *testing.T) {
			got := decodeStatus[PermissionsStatus](t, permissionsCall(b, http.MethodPut, `{"mode":"`+mode+`"}`, "Bearer "+testToken))
			if got.Mode != mode || !slices.Equal(got.Modes, config.ToolApprovalModes) || read().Mode != mode || config.ToolApprovalModeAt(profile) != mode {
				t.Fatalf("mode %q did not round trip: %+v", mode, got)
			}
			data, err := os.ReadFile(path)
			if err != nil || !strings.Contains(string(data), `"unrelated": "keep"`) {
				t.Fatalf("unrelated setting lost: %s, %v", data, err)
			}
		})
	}
}

func TestAnUnknownModeIs400(t *testing.T) {
	profile := t.TempDir()
	b := settingsBridge(t, profile)
	before := config.ToolApprovalModeAt(profile)
	for _, body := range []string{`{"mode":"sometimes"}`, `{"mode":""}`, `{}`, `{"mode":"ALLOW"}`, `{"mode":" allow "}`, `{"mode":3}`, `{"mode":"allow","extra":true}`, `broken`} {
		w := permissionsCall(b, http.MethodPut, body, "Bearer "+testToken)
		if w.Code != http.StatusBadRequest || config.ToolApprovalModeAt(profile) != before {
			t.Fatalf("body %s: status %d, mode %q", body, w.Code, config.ToolApprovalModeAt(profile))
		}
	}
}

func TestPermissionsNeedTheToken(t *testing.T) {
	profile := t.TempDir()
	b := settingsBridge(t, profile)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		for _, auth := range []string{"", "Bearer wrong"} {
			w := permissionsCall(b, method, `{"mode":"deny"}`, auth)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("%s without token: status %d", method, w.Code)
			}
		}
	}
	if got := config.ToolApprovalModeAt(profile); got != config.DefaultToolApprovalMode {
		t.Fatalf("unauthorized write changed mode to %q", got)
	}
}

func TestPermissionsWithoutProfileIs404(t *testing.T) {
	b := settingsBridge(t, "")
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		if w := permissionsCall(b, method, `{"mode":"allow"}`, "Bearer "+testToken); w.Code != http.StatusNotFound {
			t.Fatalf("%s without profile: status %d", method, w.Code)
		}
	}
}
