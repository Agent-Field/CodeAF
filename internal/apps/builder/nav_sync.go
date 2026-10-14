package builder

import (
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/builder/prompt"
)

// syncNav gives every screen the same SectionTabs items: each screen's own
// title, in plan order. Screens are written by separate calls side by side, so
// each guesses its neighbours' titles; deriving the row here keeps navigation
// from drifting without spending a repair round on it. A screen whose nav is
// missing or another part is left for Check to report.
func syncNav(files map[string][]byte, plan prompt.Plan) {
	items := navItems(files, plan)
	for _, l := range plan.Screens {
		path := screenPath(l.ID)
		var screen map[string]any
		if json.Unmarshal(files[path], &screen) != nil {
			continue
		}
		nav, _ := screen["nav"].(map[string]any)
		if nav == nil || nav["part"] != "SectionTabs" {
			continue
		}
		props, _ := nav["props"].(map[string]any)
		if props == nil {
			props = map[string]any{}
			nav["props"] = props
		}
		props["items"] = items
		if raw, err := json.MarshalIndent(screen, "", "  "); err == nil {
			files[path] = append(raw, '\n')
		}
	}
}

func screenPath(id string) string { return "screens/" + id + ".json" }

// navItems is each plan screen's written title, falling back to the plan
// line's own title (the text before " · ") when the file has none.
func navItems(files map[string][]byte, plan prompt.Plan) []string {
	items := make([]string, 0, len(plan.Screens))
	for _, l := range plan.Screens {
		var screen struct {
			Title string `json:"title"`
		}
		_ = json.Unmarshal(files[screenPath(l.ID)], &screen)
		title := strings.TrimSpace(screen.Title)
		if title == "" {
			title, _, _ = strings.Cut(l.Line, " · ")
			title = strings.TrimSpace(title)
		}
		items = append(items, title)
	}
	return items
}
