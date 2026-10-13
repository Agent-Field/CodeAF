package parts

import (
	"fmt"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

func validateTimeline(text string, _ *bridge.PartExtra) error {
	var body struct {
		View  string `json:"view"`
		Items []struct {
			ID        string   `json:"id"`
			Title     *string  `json:"title"`
			Start     string   `json:"start"`
			End       string   `json:"end"`
			DependsOn []string `json:"dependsOn"`
			Lane      string   `json:"lane"`
		} `json:"items"`
	}
	if err := decodeJSON(text, &body); err != nil {
		return err
	}
	if body.Items == nil || !oneOf(body.View, "gantt", "calendar", "shifts") {
		return fmt.Errorf("needs items array and view (gantt, calendar or shifts)")
	}
	seen := map[string]bool{}
	for _, item := range body.Items {
		if item.ID == "" || item.Title == nil || item.Start == "" || item.End == "" {
			return fmt.Errorf("every item needs id, title, start and end")
		}
		if seen[item.ID] {
			return fmt.Errorf("duplicate item id %q", item.ID)
		}
		seen[item.ID] = true
	}
	for _, item := range body.Items {
		for _, id := range item.DependsOn {
			if !seen[id] {
				return fmt.Errorf("unknown dependency %q", id)
			}
		}
	}
	return nil
}
