package desktopbridge

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/Agent-Field/codeaf/internal/workspacestore"
)

// A favicon request names a saved pane, never an arbitrary URL. The canonical
// web target authorizes this public-only, bounded, cached image read.
func (b *Bridge) workspaceFavicon(w http.ResponseWriter, r *http.Request, store *workspacestore.Store, key string) {
	if !needGet(w, r) {
		return
	}
	record, err := store.Get(key)
	if err != nil {
		workspaceError(w, err)
		return
	}
	type pane struct {
		ID     string `json:"id"`
		Kind   string `json:"kind"`
		Target struct {
			URL string `json:"url"`
		} `json:"target"`
	}
	var doc struct {
		Tabs []struct {
			pane
			Split *struct {
				Panes []pane `json:"panes"`
			} `json:"split"`
		} `json:"tabs"`
	}
	if json.Unmarshal(record.Workspace, &doc) != nil {
		write(w, map[string]string{})
		return
	}
	id := r.URL.Query().Get("pane")
	for _, tab := range doc.Tabs {
		panes := []pane{tab.pane}
		if tab.Split != nil {
			panes = tab.Split.Panes
		}
		for _, p := range panes {
			if p.ID != id || p.Kind != "web" {
				continue
			}
			u, err := url.Parse(p.Target.URL)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
				break
			}
			domain := strings.ToLower(u.Hostname())
			if !plainDomain(domain) {
				break
			}
			write(w, map[string]string{"domain": domain, "dataUrl": b.icons.lookup(domain)})
			return
		}
	}
	write(w, map[string]string{})
}
