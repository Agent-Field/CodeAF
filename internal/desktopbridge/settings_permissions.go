package desktopbridge

import (
	"net/http"
	"slices"

	"github.com/Agent-Field/codeaf/internal/config"
)

// PermissionsStatus uses the config registry's choices so the desktop cannot
// offer a mode the engine's tool gate does not understand.
type PermissionsStatus struct {
	Mode  string   `json:"mode"`
	Modes []string `json:"modes"`
}

func init() {
	registerSettingsRoute(http.MethodGet, "/settings/permissions", (*Bridge).servePermissions)
	registerSettingsRoute(http.MethodPut, "/settings/permissions", (*Bridge).servePermissions)
}

func (b *Bridge) servePermissions(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	models := b.models
	b.mu.Unlock()
	// An unattached profile must not fall through to the machine's own settings.
	if models == nil {
		fail(w, http.StatusNotFound, "this engine has no model settings")
		return
	}
	if r.Method == http.MethodPut {
		var body struct {
			Mode string `json:"mode"`
		}
		if !decode(w, r, &body) {
			return
		}
		if !slices.Contains(config.ToolApprovalModes, body.Mode) {
			fail(w, http.StatusBadRequest, "unknown tool approval mode")
			return
		}
		// The registry owns validation and profile writes for every settings door.
		row, ok := config.NewSettings(config.SettingsOptions{ProfileDir: models.ProfileDir}).Row(config.KeyToolApprovalMode)
		if !ok {
			fail(w, http.StatusInternalServerError, "tool approval setting unavailable")
			return
		}
		if err := row.Apply(body.Mode); err != nil {
			fail(w, http.StatusInternalServerError, "could not save tool approval mode")
			return
		}
	}
	write(w, PermissionsStatus{
		Mode:  config.ToolApprovalModeAt(models.ProfileDir),
		Modes: slices.Clone(config.ToolApprovalModes),
	})
}
