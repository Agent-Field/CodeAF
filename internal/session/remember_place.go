package session

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// rememberPlace writes the chat's fact to a direct place, never to an inherited
// ancestor the person did not file it under. Explicit non-place scopes retain
// the ordinary memory path.
func (a *Agent) rememberPlace(text, scope string) (string, bool, error) {
	if scope != "" && scope != "place" {
		return "", false, nil
	}
	a.mu.Lock()
	door, chat := a.config.PlaceGraph, a.id
	eligible := a.file != nil && !a.closed && !a.config.bindingOnlyMemory && !a.config.promptProfile().lean()
	a.mu.Unlock()
	if !eligible || door == nil || door.Path == "" || chat == "" {
		if scope == "place" {
			return "", true, errors.New("this conversation has no place")
		}
		return "", false, nil
	}
	snap, err := placegraph.ReadSnapshot(door.Path)
	if err != nil {
		return "", true, err
	}
	target, count := rememberPlaceTarget(snap, chat)
	if count == 0 {
		if scope == "place" {
			return "", true, errors.New("this conversation has no place")
		}
		return "", false, nil
	}
	text = strings.TrimSpace(text)
	graph, err := placegraph.Open(placegraph.Options{Path: door.Path})
	if err != nil {
		return "", true, err
	}
	line, _, err := graph.AddLine(placegraph.Line{PlaceID: target.ID, Text: text, Source: placegraph.LineSource{Kind: placegraph.LineSaidInChat, ChatID: chat, At: time.Now().UTC()}})
	if err != nil {
		return "", true, err
	}
	saved := fmt.Sprintf("Saved to %s: %s", target.Name, line.Text)
	if count > 1 {
		saved += " · first parent-most place"
	}
	a.mu.Lock()
	a.queuePlaceNoteWithUndoLocked(saved, []string{placegraph.RememberUndoToken(line)})
	a.mu.Unlock()
	return saved, true, nil
}

// rememberPlaceTarget prefers the shallowest direct membership. Equal depths
// preserve filing order so an unrelated place never wins by its spelling.
func rememberPlaceTarget(snap *placegraph.Snapshot, chat string) (placegraph.Place, int) {
	var chosen placegraph.Place
	best, count := len(snap.Places)+1, 0
	for _, membership := range snap.PlacesOf(chat) {
		p, ok := snap.Place(membership.PlaceID)
		if !ok || p.Archived {
			continue
		}
		count++
		depth := rememberPlaceDepth(snap, p)
		if depth < best {
			chosen, best = p, depth
		}
	}
	return chosen, count
}

func rememberPlaceDepth(snap *placegraph.Snapshot, p placegraph.Place) int {
	frontier := []placegraph.Place{p}
	seen := map[string]bool{p.ID: true}
	for depth := 0; len(frontier) > 0; depth++ {
		var next []placegraph.Place
		for _, current := range frontier {
			if len(current.Parents) == 0 {
				return depth
			}
			for _, id := range current.Parents {
				if seen[id] {
					continue
				}
				seen[id] = true
				if parent, ok := snap.Place(id); ok {
					next = append(next, parent)
				}
			}
		}
		frontier = next
	}
	return len(snap.Places)
}

func (a *Agent) canRememberPlace() bool {
	if a.config.bindingOnlyMemory || a.config.promptProfile().lean() || a.config.PlaceGraph == nil || a.config.PlaceGraph.Path == "" || a.file == nil || a.id == "" {
		return false
	}
	snap, err := placegraph.ReadSnapshot(a.config.PlaceGraph.Path)
	if err != nil {
		return false
	}
	_, count := rememberPlaceTarget(snap, a.id)
	return count > 0
}

// Place membership can change after the chat opens. The turn that first reads
// that change must also gain or lose the verb, before it asks the model to act.
func (a *Agent) refreshRememberPlaceToolLocked() {
	if a.memoryWritable() {
		return
	}
	if a.canRememberPlace() {
		_, _ = a.armFamily(a.memoryTools())
		return
	}
	a.armMu.Lock()
	defer a.armMu.Unlock()
	retire := func(tools []bare.Tool) []bare.Tool {
		kept := make([]bare.Tool, 0, len(tools))
		for _, tool := range tools {
			if tool.Name != "remember" {
				kept = append(kept, tool)
			}
		}
		return kept
	}
	a.tools = retire(a.tools)
	a.profileArmed = retire(a.profileArmed)
	a.definitions, _ = toolDefinitions(a.tools)
}
