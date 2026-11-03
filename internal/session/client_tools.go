package session

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// Client tool limits are shared by the wire validator and the conversation door.
const (
	ClientToolMaxSets           = 4
	ClientToolMaxPerSet         = 32
	ClientToolMaxPerAgent       = 64
	ClientToolDescriptionMax    = 2048
	ClientToolSchemaMax         = 16 << 10
	ClientToolInstructionsMax   = 4096
	ClientToolErrorMax          = 4096
	ClientToolOutputMax         = 64 << 10
	ClientToolDefaultDeadlineMS = 30000
	ClientToolMinDeadlineMS     = 1000
	ClientToolMaxDeadlineMS     = 120000
)

// ClientToolDoor belongs to one conversation and one connected app.
type ClientToolDoor interface {
	// Snapshots are immutable, so an unchanged version costs no copying.
	ClientToolSets() (version uint64, sets []ClientToolSet)
	CallClientTool(context.Context, ClientToolCall) (ClientToolAnswer, error)
}

type ClientTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	Category    ActionCategory  `json:"category"`
}

type ClientToolSet struct {
	ID           string
	Instructions string
	DeadlineMS   int
	Tools        []ClientTool
}

type ClientToolCall struct {
	Set, Name, TurnCall string
	Args                json.RawMessage
	DeadlineMS          int
}

type ClientToolAnswer struct {
	Text    string
	IsError bool
}

// clientToolState is protected by armMu, along with the belt it describes.
type clientToolState struct {
	version uint64
	seen    bool
	sets    []ClientToolSet
	names   map[string]ActionCategory
	retired map[string]bool
	section string
}

func (c Config) clientToolSets() (uint64, []ClientToolSet) {
	if c.ClientTools == nil || c.InTask || c.Errand || c.promptProfile().chat() {
		return 0, nil
	}
	return c.ClientTools.ClientToolSets()
}

const clientToolHeading = "\n\n## Tools from the app: "
const clientToolSectionEnd = "\n<!-- End tools from the app. -->"

func clientToolsSection(config Config) string {
	_, sets := config.clientToolSets()
	return clientToolsSectionOf(sets)
}

func clientToolsSectionOf(sets []ClientToolSet) string {
	var out strings.Builder
	for _, set := range sets {
		out.WriteString(clientToolHeading)
		out.WriteString(set.ID)
		out.WriteByte('\n')
		out.WriteString(set.Instructions)
		out.WriteString(clientToolSectionEnd)
	}
	return out.String()
}

// Child pages lose the app's instructions as well as the execution door.
func clientToolsWithoutSections(page string) string {
	start := strings.Index(page, clientToolHeading)
	if start < 0 {
		return page
	}
	// All app sections form one tail. The last renderer-owned end also covers
	// delimiter text that an app may have included inside its instructions.
	end := strings.LastIndex(page, clientToolSectionEnd)
	if end < start {
		return page[:start]
	}
	return page[:start] + page[end+len(clientToolSectionEnd):]
}

// appendClientTools runs during construction and complete belt rebuilds.
func (a *Agent) appendClientTools(builtins []bare.Tool) []bare.Tool {
	a.armMu.Lock()
	defer a.armMu.Unlock()
	version, sets := a.config.clientToolSets()
	a.clientTools.version, a.clientTools.seen = version, true
	a.clientTools.sets = sets
	a.clientTools.section = clientToolsSectionOf(sets)
	return a.clientToolTailLocked(builtins, sets)
}

func (a *Agent) clientToolTailLocked(builtins []bare.Tool, sets []ClientToolSet) []bare.Tool {
	a.noteClientToolBuiltinsLocked(builtins)
	for name := range a.clientTools.names {
		if a.clientTools.retired == nil {
			a.clientTools.retired = make(map[string]bool)
		}
		a.clientTools.retired[name] = true
	}
	a.clientTools.names = nil
	if len(sets) == 0 {
		return builtins
	}
	tools := make([]bare.Tool, len(builtins), len(builtins)+ClientToolMaxPerAgent)
	copy(tools, builtins)
	held := make(map[string]bool, len(builtins))
	for _, tool := range builtins {
		held[tool.Name] = true
	}
	a.clientTools.names = make(map[string]ActionCategory)
	for _, set := range sets {
		for _, spec := range set.Tools {
			if held[spec.Name] {
				continue
			}
			held[spec.Name] = true
			a.clientTools.names[spec.Name] = spec.Category
			delete(a.clientTools.retired, spec.Name)
			tools = append(tools, a.clientTool(set, spec))
		}
	}
	return tools
}

func (a *Agent) noteClientToolBuiltinsLocked(builtins []bare.Tool) {
	observer, ok := a.config.ClientTools.(interface{ ClientToolBuiltins([]string) })
	if !ok {
		return
	}
	names := make([]string, 0, len(builtins))
	for _, tool := range builtins {
		names = append(names, tool.Name)
	}
	for _, tools := range a.shelf {
		for _, tool := range tools {
			names = append(names, tool.Name)
		}
	}
	observer.ClientToolBuiltins(names)
}

func (a *Agent) clientToolRetiredNotice(name string) (string, bool) {
	a.armMu.Lock()
	defer a.armMu.Unlock()
	if !a.clientTools.retired[name] {
		return "", false
	}
	return ClientToolUnavailable(name).Text, true
}

func ClientToolUnavailable(name string) ClientToolAnswer {
	return ClientToolAnswer{Text: fmt.Sprintf("client_unavailable: the app that provides %s is not connected; nothing was done.", name), IsError: true}
}

// BoundClientToolOutput reuses the engine's rune-safe cut and spells the loss plainly.
func BoundClientToolOutput(text string) string {
	bounded := capBytes(text, ClientToolOutputMax)
	if len(text) <= ClientToolOutputMax {
		return bounded
	}
	cut := strings.LastIndex(bounded, "… (")
	return bounded[:cut] + fmt.Sprintf("\n[cut: %d bytes dropped]", len(text)-cut)
}

func (a *Agent) clientTool(set ClientToolSet, spec ClientTool) bare.Tool {
	return bare.Tool{Name: spec.Name, Description: spec.Description, Schema: spec.InputSchema,
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var object map[string]json.RawMessage
			data := bytes.TrimSpace(args)
			if len(data) == 0 || data[0] != '{' || !json.Valid(data) || decodeToolArguments(data, &object) != nil || object == nil {
				return rejectToolArguments(ctx, invalidArgumentsPrefix+"expected a JSON object"), true, nil
			}
			answer, err := a.config.ClientTools.CallClientTool(ctx, ClientToolCall{
				Set: set.ID, Name: spec.Name, Args: args, TurnCall: callIDFrom(ctx), DeadlineMS: set.DeadlineMS,
			})
			if err != nil {
				return err.Error(), true, nil
			}
			return answer.Text, answer.IsError, nil
		},
	}
}

// A boundary swaps fresh arrays; existing requests can keep walking their snapshot.
func (a *Agent) clientToolsBoundary() {
	if a.config.ClientTools == nil {
		return
	}
	version, sets := a.config.clientToolSets()
	a.armMu.Lock()
	if a.clientTools.seen && version == a.clientTools.version {
		a.armMu.Unlock()
		return
	}
	section := clientToolsSectionOf(sets)
	changed := section != a.clientTools.section
	builtins, definitions, err := a.clientToolBuiltinsLocked(0)
	if err != nil {
		a.armMu.Unlock()
		return
	}
	a.tools = a.clientToolTailLocked(builtins, sets)
	tail, _ := toolDefinitions(a.tools[len(builtins):])
	a.definitions = append(definitions, tail...)
	a.clientTools.version, a.clientTools.seen = version, true
	a.clientTools.sets, a.clientTools.section = sets, section
	a.armMu.Unlock()
	if changed {
		a.mu.Lock()
		a.rerenderSystemLocked(time.Now())
		a.mu.Unlock()
	}
}

func (a *Agent) clientToolBuiltinsLocked(room int) ([]bare.Tool, []ai.ToolDefinition, error) {
	cache := a.definitions
	if len(cache) < len(a.tools) {
		// Lightweight callers may have no cache, or only a previously armed family's definitions.
		var err error
		cache, err = toolDefinitions(a.tools)
		if err != nil {
			return nil, nil, err
		}
	}
	tools := make([]bare.Tool, 0, len(a.tools)+room)
	definitions := make([]ai.ToolDefinition, 0, len(a.tools)+room)
	for i, tool := range a.tools {
		if _, client := a.clientTools.names[tool.Name]; client {
			continue
		}
		tools = append(tools, tool)
		definitions = append(definitions, cache[i])
	}
	return tools, definitions, nil
}

func (a *Agent) withoutClientToolsLocked(tools []bare.Tool) []bare.Tool {
	if len(a.clientTools.names) == 0 {
		return tools
	}
	builtins := make([]bare.Tool, 0, len(tools))
	for _, tool := range tools {
		if _, client := a.clientTools.names[tool.Name]; !client {
			builtins = append(builtins, tool)
		}
	}
	return builtins
}

// ActionCategoryForTool resolves this conversation's declaration before the static floor.
func (a *Agent) ActionCategoryForTool(name string) ActionCategory {
	a.armMu.Lock()
	category, ok := a.clientTools.names[name]
	a.armMu.Unlock()
	if ok {
		return category
	}
	return ActionCategoryForTool(name)
}

func (a *Agent) clientToolCategory(name string) (ActionCategory, bool) {
	a.armMu.Lock()
	defer a.armMu.Unlock()
	category, ok := a.clientTools.names[name]
	return category, ok
}

func (a *Agent) clientToolEvent(event Event) Event {
	if category, ok := a.clientToolCategory(event.Tool); ok {
		event.Category = category
	}
	return event
}

func toolEventCategory(event Event) ActionCategory {
	if event.Category != "" {
		return event.Category
	}
	return ActionCategoryForTool(event.Tool)
}

// ClientToolBuiltinNames includes the live belt and deferred tools for collision checks.
func (a *Agent) ClientToolBuiltinNames() []string {
	a.armMu.Lock()
	defer a.armMu.Unlock()
	var names []string
	for _, tool := range a.tools {
		if _, client := a.clientTools.names[tool.Name]; !client {
			names = append(names, tool.Name)
		}
	}
	for _, tools := range a.shelf {
		for _, tool := range tools {
			names = append(names, tool.Name)
		}
	}
	return names
}
