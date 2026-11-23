package api

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"time"
)

// Person is GET /v1/people/{handle}; repository facts belong to this viewer.
type Person struct {
	Factory Factory `json:"factory"`
	Handle  string  `json:"handle"`
	factory.GitHubPerson
	Login       string      `json:"login"`
	You         bool        `json:"you"`
	Operator    bool        `json:"operator"`
	Status      string      `json:"status"`
	Runners     []Runner    `json:"runners"`
	Sessions    []Session   `json:"sessions,omitempty"`
	Role        string      `json:"role"`
	LastSeen    *time.Time  `json:"last_seen,omitempty"`
	Owns        []Ownership `json:"owns"`
	AssignedNow int         `json:"assigned_now,omitempty"`
	WaitingOn   int         `json:"waiting_on,omitempty"`
	Questions   int         `json:"questions,omitempty"`
	Machines    []Machine   `json:"machines"`
}

// Ownership is a pattern in one watched repository's default branch.
type Ownership struct {
	Repo string `json:"repo"`
	Path string `json:"path"`
}
