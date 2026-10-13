package appapi

import (
	"encoding/json"
)

// These wire types reserve the app vocabulary before this resource has behaviour.
type AIMessage struct {
	Role    string `json:"role" jsonschema:"enum=system,enum=user,enum=assistant"`
	Content string `json:"content"`
}
type AICallRequest struct {
	Role       string          `json:"role"`
	Tier       string          `json:"tier,omitempty" jsonschema:"enum=reflex,enum=low,enum=worker"`
	Messages   []AIMessage     `json:"messages"`
	MaxTokens  int             `json:"max_tokens"`
	JSONSchema json.RawMessage `json:"json_schema,omitempty"`
	Stream     bool            `json:"stream"`
	Budget     Money           `json:"budget"`
	Expect     *AgentExpect    `json:"expect,omitempty"`
}
type AIUsage struct {
	In  int `json:"in"`
	Out int `json:"out"`
}
type AICallResponse struct {
	Text     string          `json:"text"`
	JSON     json.RawMessage `json:"json,omitempty"`
	Model    string          `json:"model"`
	Usage    AIUsage         `json:"usage"`
	Cost     Money           `json:"cost"`
	LedgerID string          `json:"ledger_id"`
}
type AIDeltaEvent struct {
	Text string `json:"text"`
}

func aiContract() resourceContract {
	return resourceContract{Name: "ai", Types: []any{AIMessage{}, AICallRequest{}, AIUsage{}, AICallResponse{}, AIDeltaEvent{}}, Routes: aiContractRoutes, Events: []eventContract{{"ai.delta", AIDeltaEvent{}}, {"ai.done", AICallResponse{}}}}
}
