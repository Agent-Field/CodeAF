package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// ChatStart is a request to the attached terminal, which owns conversation doors.
// It travels with the live turn rather than making the engine import a surface.
type ChatStart struct {
	ID        string
	Workspace string
	Model     string
	Brief     string
	Approval  string
}

func (a *Agent) startChatTools() []bare.Tool {
	if !a.config.Interactive || a.config.InTask || strings.TrimSpace(a.config.Place.Dir) == "" {
		return nil
	}
	return []bare.Tool{{Name: "start_chat", Description: "Request another independent chat in the attached terminal, keeping this chat in front. Give its complete first message and, when the person names one, its model. The terminal reports whether it opened; this receipt only confirms the request. Use team_start for a teammate, quick_task for a result returned here, or propose_task for checked work in its own copy.", Schema: json.RawMessage(`{"type":"object","properties":{"brief":{"type":"string","description":"Complete first message for the new chat."},"model":{"type":"string","description":"Catalog ID or unique model name requested for this chat. Omit to use this chat's model."}},"required":["brief"],"additionalProperties":false}`), Execute: a.startChatTool}}
}

func (a *Agent) startChatTool(ctx context.Context, args json.RawMessage) (string, bool, error) {
	var parsed struct {
		Brief string `json:"brief"`
		Model string `json:"model"`
	}
	if err := decodeToolArguments(args, &parsed); err != nil {
		return invalidArgumentsPrefix + err.Error(), true, nil
	}
	brief := strings.TrimSpace(parsed.Brief)
	if brief == "" {
		return invalidArgumentsPrefix + "brief is empty", true, nil
	}
	model := a.Model()
	if word := strings.TrimSpace(parsed.Model); word != "" {
		choice := a.resolveTaskModel(word)
		if choice.problem != "" {
			return choice.problem, true, nil
		}
		if len(choice.options) > 0 {
			return "Name one exact model: " + strings.Join(choice.options, ", "), true, nil
		}
		model = choice.model
	}
	a.mu.Lock()
	hub := a.hub
	a.mu.Unlock()
	if hub == nil {
		return "No attached turn can deliver a new-chat request.", true, nil
	}
	request := &ChatStart{ID: a.journalID() + ":" + strconv.FormatInt(time.Now().UnixNano(), 10), Workspace: a.config.Workspace, Model: model, Brief: brief, Approval: a.ResolvedApprovalPosture()}
	hub.send(Event{Kind: EventChatStart, ChatStart: request})
	return fmt.Sprintf("Requested a new chat on %s. The attached terminal will report whether it opened.", model), false, nil
}
