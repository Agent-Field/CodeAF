package builder

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"unicode"
)

func (s *Jobs) checkedProposal(j *job, intent proposalIntent) *Proposal {
	live, err := s.opts.Store.Get(j.App)
	if err == nil && maps.Equal(live.Files(), j.Working.Files) {
		// A question can reach Check without editing the app. Keep the
		// conversation and its checkpoint without asking for an Apply.
		j.Dirty = false
		return nil
	}
	card := &Proposal{
		App: j.App, Draft: j.Draft, ID: intent.ID, Kind: "big", Source: "assistant",
		BaseVersion: j.Base, Target: intent.Target, Screen: s.proposalScreen(j),
	}
	if card.Screen != "" {
		card.Kind = "small"
	}
	checkpoint := j.Checkpoints[len(j.Checkpoints)-1]
	card.Checkpoint = checkpoint.ID
	if target := intent.Target; target != nil {
		card.Source = "marked"
		path := "screens/" + target.Screen + ".json"
		var before string
		if snap, ok := j.Saved[target.Checkpoint]; ok {
			before = snap.Files[path]
		} else if err == nil {
			before = live.Files()[path]
		}
		name, previous := describeMarkedPart(before, target)
		_, next := describeMarkedPart(j.Working.Files[path], target)
		card.SummaryLines = []string{name, "Before: " + previous, "After: " + next}
	}
	card.SummaryLines = append(card.SummaryLines, checkpoint.Summary)
	return card
}

// Describe the saved screen at the mark. Prefer the stable node ID when a
// layout change has moved the part to a different path.
func describeMarkedPart(raw string, target *Target) (name, description string) {
	name = proposalLabel(target.Screen)
	var screen map[string]any
	if json.Unmarshal([]byte(raw), &screen) != nil {
		return name, "Screen absent"
	}
	if title, _ := screen["title"].(string); title != "" {
		name = title
	}
	node := findMarkedNode(screen, target.Node)
	if node == nil {
		var value any = screen
		for _, step := range target.Path {
			switch item := value.(type) {
			case map[string]any:
				key, _ := step.(string)
				value = item[key]
			case []any:
				index := -1
				switch n := step.(type) {
				case int:
					index = n
				case float64:
					index = int(n)
				}
				value = nil
				if index >= 0 && index < len(item) {
					value = item[index]
				}
			default:
				value = nil
			}
			if value == nil {
				break
			}
			if part, ok := value.(map[string]any); ok && part["part"] != nil {
				node = part
			}
		}
	}
	if node == nil {
		return name, name + " screen"
	}
	if label, _ := node["label"].(string); label != "" && !strings.EqualFold(label, name) {
		name += " · " + label
	}
	part, _ := node["part"].(string)
	props, _ := node["props"].(map[string]any)
	kind := part
	if part == "RecordList" {
		var views []string
		for _, view := range proposalWords(props["views"]) {
			if view == "List" {
				view = "Table"
			}
			views = append(views, view)
		}
		if len(views) != 0 {
			kind = strings.Join(views, " / ")
		}
	}
	if columns := proposalWords(props["columns"]); len(columns) != 0 {
		return name, fmt.Sprintf("%s · Columns: %s", kind, strings.Join(columns, ", "))
	}
	if text, _ := props["text"].(string); text != "" {
		return name, kind + " · " + briefText(text, 120)
	}
	return name, kind
}

func findMarkedNode(value any, id string) map[string]any {
	if id == "" {
		return nil
	}
	switch v := value.(type) {
	case map[string]any:
		if v["id"] == id && v["part"] != nil {
			return v
		}
		for _, child := range v {
			if node := findMarkedNode(child, id); node != nil {
				return node
			}
		}
	case []any:
		for _, child := range v {
			if node := findMarkedNode(child, id); node != nil {
				return node
			}
		}
	}
	return nil
}

func proposalWords(value any) []string {
	items, _ := value.([]any)
	var words []string
	for _, item := range items {
		if word, ok := item.(string); ok {
			words = append(words, proposalLabel(word))
		}
	}
	return words
}

func proposalLabel(id string) string {
	words := strings.FieldsFunc(id, func(r rune) bool { return r == '_' || r == '-' || unicode.IsSpace(r) })
	for i, word := range words {
		runes := []rune(word)
		runes[0] = unicode.ToUpper(runes[0])
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}
