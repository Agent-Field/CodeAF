package desktopbridge

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

// CatalogModel is one model a person can pick, as the settings page reads it.
type CatalogModel struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	ContextLength int      `json:"contextLength,omitempty"`
	Efforts       []string `json:"efforts,omitempty"`
}

// Models is the desktop's model-per-role door. The choice lives in the
// profile's own config file, which the engine process reads on every call of
// a role, so a change made here reaches the next call without a restart.
type Models struct {
	// ProfileDir is the profile whose config.json holds the choices; empty is
	// the ordinary profile, as everywhere else.
	ProfileDir string
	// Catalog lists the models the provider offers. It may be slow or fail;
	// the bridge caches a good answer and never blocks a role read on it.
	Catalog func(context.Context) ([]CatalogModel, error)

	mu       sync.Mutex
	cached   []CatalogModel
	cachedAt time.Time
}

// catalogLife is how long a fetched list is served before it is fetched again.
const catalogLife = 15 * time.Minute

// RoleView is one role as the settings page and the composer read it.
type RoleView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Controls string `json:"controls"`
	Model    string `json:"model"`
	Default  string `json:"default"`
	Effort   string `json:"effort,omitempty"`
	// Chosen is false while the role is on the default.
	Chosen bool `json:"chosen"`
	// Category is the page section the role sits in.
	Category string `json:"category"`
	// Live is false while nothing in the engine makes this role's calls; the
	// page says so instead of implying the choice is doing something.
	Live bool `json:"live"`
	// Inherits names the role this one follows until it is chosen itself.
	Inherits string `json:"inherits,omitempty"`
}

// RolesView is the whole page: every role, the default they start on, and the
// sections the page groups them under, in order.
type RolesView struct {
	Default    string                   `json:"default"`
	Roles      []RoleView               `json:"roles"`
	Categories []config.DesktopCategory `json:"categories"`
}

// ModelsView is the catalog answer. Fallback says the list could not be read
// and holds only the models in use now.
type ModelsView struct {
	Models   []CatalogModel `json:"models"`
	Fallback bool           `json:"fallback,omitempty"`
}

func (m *Models) roles() RolesView {
	view := RolesView{Default: config.DesktopDefaultModel, Categories: config.DesktopRoleCategories()}
	for _, role := range config.DesktopRoles() {
		model, effort, chosen := config.DesktopRoleChoice(m.ProfileDir, role.ID)
		view.Roles = append(view.Roles, RoleView{
			ID: role.ID, Name: role.Name, Controls: role.Controls,
			Model: model, Default: config.DesktopDefaultModel, Effort: effort, Chosen: chosen,
			Category: role.Category, Live: config.DesktopRoleLive(role), Inherits: role.Inherits,
		})
	}
	return view
}

func (m *Models) role(id string) (RoleView, bool) {
	for _, view := range m.roles().Roles {
		if view.ID == id {
			return view, true
		}
	}
	return RoleView{}, false
}

// list answers from the cache while it is fresh, fetches otherwise, and falls
// back to the models now in use when the provider cannot be reached.
func (m *Models) list(ctx context.Context) ModelsView {
	m.mu.Lock()
	if len(m.cached) > 0 && time.Since(m.cachedAt) < catalogLife {
		cached := m.cached
		m.mu.Unlock()
		return ModelsView{Models: cached}
	}
	m.mu.Unlock()
	if m.Catalog != nil {
		fetch, cancel := context.WithTimeout(ctx, 20*time.Second)
		models, err := m.Catalog(fetch)
		cancel()
		if err == nil && len(models) > 0 {
			sort.SliceStable(models, func(i, j int) bool { return models[i].ID < models[j].ID })
			m.mu.Lock()
			m.cached, m.cachedAt = models, time.Now()
			m.mu.Unlock()
			return ModelsView{Models: models}
		}
	}
	return ModelsView{Models: m.inUse(), Fallback: true}
}

// inUse is each distinct model some role runs on now.
func (m *Models) inUse() []CatalogModel {
	seen := map[string]bool{}
	var models []CatalogModel
	for _, view := range m.roles().Roles {
		if !seen[view.Model] {
			seen[view.Model] = true
			models = append(models, CatalogModel{ID: view.Model, Name: view.Model})
		}
	}
	return models
}

// allows reports whether a model may be chosen: any listed model, or, with the
// list unreadable, one already in use.
func (m *Models) allows(ctx context.Context, model string) bool {
	for _, listed := range m.list(ctx).Models {
		if listed.ID == model {
			return true
		}
	}
	return false
}

// UseModels attaches the model-per-role door. Without it the routes answer 404.
func (b *Bridge) UseModels(models *Models) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.models = models
}

// modelRoutes serves /models and /models/roles[/{id}]. It reports whether the
// path was one of its own.
func (b *Bridge) modelRoutes(w http.ResponseWriter, r *http.Request, path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if parts[0] != "models" {
		return false
	}
	b.mu.Lock()
	models := b.models
	b.mu.Unlock()
	if models == nil {
		fail(w, 404, "this engine has no model settings")
		return true
	}
	switch {
	case len(parts) == 1:
		if needGet(w, r) {
			write(w, models.list(r.Context()))
		}
	case len(parts) == 2 && parts[1] == "roles":
		if needGet(w, r) {
			write(w, models.roles())
		}
	case len(parts) == 3 && parts[1] == "roles":
		b.putRole(w, r, models, parts[2])
	case len(parts) == 2 && parts[1] == "pinned":
		b.pinnedRoute(w, r, models)
	default:
		fail(w, 404, "unknown engine action")
	}
	return true
}

func (b *Bridge) putRole(w http.ResponseWriter, r *http.Request, models *Models, id string) {
	if r.Method != http.MethodPut {
		fail(w, 405, "PUT required")
		return
	}
	var ask struct {
		Model  string `json:"model"`
		Effort string `json:"effort"`
	}
	if !decode(w, r, &ask) {
		return
	}
	if _, known := models.role(id); !known {
		fail(w, 404, "unknown model role")
		return
	}
	ask.Model = strings.TrimSpace(ask.Model)
	if ask.Model != "" && !models.allows(r.Context(), ask.Model) {
		fail(w, 400, "that model is not on the list")
		return
	}
	if err := config.WriteDesktopRole(models.ProfileDir, id, ask.Model, ask.Effort); err != nil {
		fail(w, 400, err.Error())
		return
	}
	view, _ := models.role(id)
	if id == config.DesktopRoleConversation {
		b.moveConversations(view)
	}
	write(w, view)
}

// moveConversations puts every open chat on the conversation role's model, so
// the next message of each one uses it.
func (b *Bridge) moveConversations(view RoleView) {
	b.mu.Lock()
	open := make([]*conversation, 0, len(b.sessions))
	for _, s := range b.sessions {
		open = append(open, s)
	}
	b.mu.Unlock()
	for _, s := range open {
		if door, ok := s.conn.Agent.(interface{ SetModel(string) }); ok {
			door.SetModel(view.Model)
		}
		if door, ok := s.conn.Agent.(interface{ SetReasoningFor(string, string) }); ok && view.Effort != "" {
			door.SetReasoningFor(view.Model, view.Effort)
		}
		s.publishSnapshot()
	}
}
