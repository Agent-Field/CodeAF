package parts

import (
	"fmt"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

func validateGraph(text string, _ *bridge.PartExtra) error {
	var body struct {
		Layout string `json:"layout"`
		Nodes  []struct {
			ID    string   `json:"id"`
			Label *string  `json:"label"`
			X     *float64 `json:"x"`
			Y     *float64 `json:"y"`
		} `json:"nodes"`
		Links []struct {
			From  string `json:"from"`
			To    string `json:"to"`
			Label string `json:"label"`
		} `json:"links"`
	}
	if err := decodeJSON(text, &body); err != nil {
		return err
	}
	if body.Nodes == nil || body.Links == nil || !oneOf(body.Layout, "tree", "flow", "free") {
		return fmt.Errorf("needs nodes and links arrays and layout (tree, flow or free)")
	}
	seen := map[string]bool{}
	for _, node := range body.Nodes {
		if node.ID == "" || node.Label == nil || seen[node.ID] {
			return fmt.Errorf("nodes need unique ids and labels")
		}
		seen[node.ID] = true
	}
	for _, link := range body.Links {
		if !seen[link.From] || !seen[link.To] {
			return fmt.Errorf("link endpoints must name existing nodes")
		}
	}
	return nil
}
