package api

import "github.com/Agent-Field/codeaf/internal/factory/store"

// BotList shows metadata so people can manage tokens without seeing their secrets.
type BotList struct {
	Bots []store.Bot `json:"bots"`
}

type BotNew struct {
	Name  string `json:"name"`
	Scope string `json:"scope,omitempty"`
}

type BotCreated struct {
	Bot   store.Bot `json:"bot"`
	Token string    `json:"token"`
}

type BotTokenNew struct {
	Label     string `json:"label,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

type BotTokenCreated struct {
	ID        string  `json:"token_id"`
	Token     string  `json:"token"`
	Hint      string  `json:"hint"`
	Label     string  `json:"label"`
	CreatedAt string  `json:"created_at"`
	ExpiresAt *string `json:"expires_at,omitempty"`
}
