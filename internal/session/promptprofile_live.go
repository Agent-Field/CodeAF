package session

import (
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// preparePromptProfile changes a conversation's prefix at a request boundary.
// Nothing already in flight is rewritten. Caller prompts and deliberately
// narrowed workers keep the shape their owner chose; an explicit lean or full
// choice keeps its word, and gives way only to a model that cannot use tools
// (chatpage.go), coming back when the conversation leaves that model.
func (a *Agent) preparePromptProfile(model string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	state := a.config.liveProfile
	if state == nil || !a.systemOwn || a.config.InTask {
		return nil
	}
	next := state.launch
	if state.auto {
		window := a.window()
		if a.config.ContextWindowFor != nil {
			if known := a.config.ContextWindowFor(model); known > 0 {
				window = known
			}
		}
		next = profileFull
		if window < leanWindowThreshold {
			next = profileLean
		}
	}
	if a.config.takesNoTools(model) {
		next = profileChat
	}
	previous := a.config.promptProfile()
	if previous == next {
		return nil
	}
	a.armMu.Lock()
	if a.withdrawn != nil {
		a.armMu.Unlock()
		return nil
	}
	oldShelf, oldOrder, oldPrearm := a.shelf, a.shelfOrder, a.prearm
	a.armMu.Unlock()

	// The pointer is stable for this engine's lifetime; background memory
	// readers can observe its value without racing a write to the Config.
	state.current.Store(next)
	tools := a.belt()
	a.armMu.Lock()
	// Explicitly loaded capabilities and connected services remain usable.
	// Only the default belt is repartitioned when the window changes.
	held := make(map[string]bool, len(tools))
	for _, tool := range tools {
		held[tool.Name] = true
	}
	// A MODEL WITH NO TOOLS TAKES NONE BACK. What was loaded stays in
	// profileArmed and returns with the next model that can use it.
	if next.chat() {
		tools = nil
	}
	for _, tool := range a.profileArmed {
		if next.chat() {
			break
		}
		if !held[tool.Name] {
			tools = append(tools, tool)
			held[tool.Name] = true
		}
	}
	// Pre-armed groups join this same atomic publication, so no request can
	// see the new page without the tools that page promises.
	for _, tool := range a.prearm {
		if !held[tool.Name] {
			tools = append(tools, tool)
			held[tool.Name] = true
		}
	}
	definitions, err := toolDefinitions(tools)
	if err != nil {
		a.shelf, a.shelfOrder, a.prearm = oldShelf, oldOrder, oldPrearm
		state.current.Store(previous)
		a.armMu.Unlock()
		return fmt.Errorf("change prompt profile: %w", err)
	}
	// Fresh slices leave snapshots held by an earlier request untouched.
	a.tools = append([]bare.Tool(nil), tools...)
	a.definitions = definitions
	a.armMu.Unlock()
	if next.lean() {
		a.memoryText = ""
	}
	a.rerenderSystemLocked(time.Now())
	// The last usage figure counted the old prefix. The new request is weighed
	// again by the provider after the new prompt and schemas are encoded.
	a.contextTokens = 0
	return nil
}
