// Package inventory records what a cell's tool calls revealed about its
// environment: which tools ran, which services were left alive, the platform,
// and the names (never the values) of environment variables
// (docs/ARCHITECTURE.md section 8.4, SCHEMAS.md section 7).
//
// Only this package writes .cell/env/inventory.json (law L13). The executor
// tells an [Observer] about each call; the model can add lower-trust notes
// through [Store.Annotate], which touch nothing but the annotations field.
package inventory

import (
	"github.com/Agent-Field/codeaf/internal/cell"
)

// Path is the cell-relative location of the inventory file.
const Path = cell.EnvPath + "/inventory.json"

const schemaV = 1

// Tool is one executable a call resolved. BinaryHash identifies the bytes, so
// it means the same on another machine; a machine path would not.
type Tool struct {
	Name          string `json:"name"`
	BinaryHash    string `json:"binary_hash"`
	VersionString string `json:"version_string,omitempty"`
}

// Service is a process a call left alive. Whether it is stateful is derived:
// only a data directory inside the cell travels with the chat.
type Service struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Ports   []int  `json:"ports"`
	DataDir string `json:"data_dir,omitempty"`
}

// Stateful reports whether the service keeps data in the cell.
func (s Service) Stateful() bool { return s.DataDir != "" }

// Platform is the machine that observed the tools.
type Platform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

// Inventory is .cell/env/inventory.json. V is first so readers can branch on it.
type Inventory struct {
	V           uint16                    `json:"V"`
	Tools       []Tool                    `json:"tools,omitempty"`
	Services    []Service                 `json:"services,omitempty"`
	Platform    Platform                  `json:"platform"`
	Lockfiles   []string                  `json:"lockfiles,omitempty"`
	EnvVarNames []string                  `json:"env_var_names,omitempty"`
	Hints       map[string]string         `json:"hints,omitempty"`
	Annotations map[string]map[string]any `json:"annotations,omitempty"`
}
