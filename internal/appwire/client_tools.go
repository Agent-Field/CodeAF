package appwire

import (
	"encoding/json"
	"regexp"

	"github.com/Agent-Field/codeaf/internal/session"
)

const FeatureClientTools = "agents.client_tools"
const FeatureClientToolsSource = "agents.client_tools.source"
const EventAgentsToolCall = "agents.tool_call"

// ClientTooler is optional because only a connected app can answer these tools.
type ClientTooler interface {
	SetClientTools(ClientToolsSetParams) (ClientToolsSetResult, error)
	ClientToolResult(ClientToolResultParams) (ClientToolResultAck, error)
}

type ClientToolsSetParams struct {
	Agent        string               `json:"agent"`
	Set          string               `json:"set"`
	Instructions string               `json:"instructions"`
	DeadlineMS   int                  `json:"deadline_ms"`
	Tools        []session.ClientTool `json:"tools"`
}

type ClientToolsSetResult struct {
	Agent string   `json:"agent"`
	Set   string   `json:"set"`
	Tools []string `json:"tools"`
}

type AgentsToolCall struct {
	Agent           string          `json:"agent"`
	Turn            uint64          `json:"turn"`
	Call            string          `json:"call"`
	TurnCall        string          `json:"turn_call"`
	SourceMessageID string          `json:"source_message_id,omitempty"`
	Set             string          `json:"set"`
	Name            string          `json:"name"`
	Args            json.RawMessage `json:"args"`
	DeadlineMS      int             `json:"deadline_ms"`
}

type ClientToolResultParams struct {
	Call string `json:"call"`
	// RawMessage distinguishes an explicit JSON null result from an omitted one.
	Result json.RawMessage `json:"result,omitempty"`
	Error  *string         `json:"error,omitempty"`
}

type ClientToolResultAck struct {
	Accepted bool `json:"accepted"`
}

func (p *ClientToolResultParams) UnmarshalJSON(data []byte) error {
	type plain ClientToolResultParams
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if _, present := fields["error"]; present && decoded.Error == nil {
		return Errorf(CodeBadParams, "error must be a nonempty string")
	}
	*p = ClientToolResultParams(decoded)
	return nil
}

var clientToolSetName = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
var clientToolName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Validate rejects a whole replacement before the backend can change any set.
func (p ClientToolsSetParams) Validate() error {
	if p.Agent == "" || !clientToolSetName.MatchString(p.Set) {
		return Errorf(CodeBadParams, "agent and a valid set id are required")
	}
	if len(p.Tools) > session.ClientToolMaxPerSet || len(p.Instructions) > session.ClientToolInstructionsMax {
		return Errorf(CodeBadParams, "a set allows at most %d tools and %d instruction bytes", session.ClientToolMaxPerSet, session.ClientToolInstructionsMax)
	}
	if p.DeadlineMS != 0 && (p.DeadlineMS < session.ClientToolMinDeadlineMS || p.DeadlineMS > session.ClientToolMaxDeadlineMS) {
		return Errorf(CodeBadParams, "deadline_ms must be 0 or %d–%d", session.ClientToolMinDeadlineMS, session.ClientToolMaxDeadlineMS)
	}
	held := make(map[string]bool, len(p.Tools))
	for _, tool := range p.Tools {
		if !clientToolName.MatchString(tool.Name) || held[tool.Name] {
			return Errorf(CodeBadParams, "invalid or duplicate tool name %q", tool.Name)
		}
		held[tool.Name] = true
		if err := validateClientTool(tool); err != nil {
			return err
		}
	}
	return nil
}

func validateClientTool(tool session.ClientTool) error {
	if len(tool.Description) == 0 || len(tool.Description) > session.ClientToolDescriptionMax {
		return Errorf(CodeBadParams, "%s description must be 1–%d bytes", tool.Name, session.ClientToolDescriptionMax)
	}
	var object map[string]json.RawMessage
	if len(tool.InputSchema) > session.ClientToolSchemaMax || json.Unmarshal(tool.InputSchema, &object) != nil || object == nil {
		return Errorf(CodeBadParams, "%s input_schema must be an object of at most %d bytes", tool.Name, session.ClientToolSchemaMax)
	}
	var kind string
	if json.Unmarshal(object["type"], &kind) != nil || kind != "object" {
		return Errorf(CodeBadParams, "%s input_schema type must be object", tool.Name)
	}
	switch tool.Category {
	case session.ActionRead, session.ActionEdit, session.ActionCreate:
		return nil
	}
	return Errorf(CodeBadParams, "%s category must be read, edit or create", tool.Name)
}

func (p ClientToolResultParams) Validate() error {
	if (p.Result != nil) == (p.Error != nil) {
		return Errorf(CodeBadParams, "exactly one of result or error is required")
	}
	if p.Error != nil && (len(*p.Error) == 0 || len(*p.Error) > session.ClientToolErrorMax) {
		return Errorf(CodeBadParams, "error must be 1–%d bytes", session.ClientToolErrorMax)
	}
	if p.Result != nil && !json.Valid(p.Result) {
		return Errorf(CodeBadParams, "result must be JSON")
	}
	return nil
}

func withClientToolsFeature(hello Hello, engine Engine) Hello {
	if _, ok := engine.(ClientTooler); ok {
		return withFeature(withFeature(hello, FeatureClientTools), FeatureClientToolsSource)
	}
	return hello
}

func setClientTools(e Engine, p ClientToolsSetParams) (*ClientToolsSetResult, error) {
	door, ok := e.(ClientTooler)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "client tools are unavailable")
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	result, err := door.SetClientTools(p)
	return &result, err
}

func answerClientTool(e Engine, p ClientToolResultParams) (*ClientToolResultAck, error) {
	door, ok := e.(ClientTooler)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "client tools are unavailable")
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	result, err := door.ClientToolResult(p)
	return &result, err
}
