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

// Running is a command that was alive at a seal: what the chat had going that
// another machine will not have. Cwd is cell-relative (L1) and "." is the root;
// Ports are the TCP ports it listened on, where the platform could say; Since is
// display only and orders nothing.
type Running struct {
	Command string `json:"command"`
	Cwd     string `json:"cwd,omitempty"`
	Ports   []int  `json:"ports,omitempty"`
	Since   int64  `json:"since,omitempty"`
}

// Withheld is a folder the seal left out because a lockfile that does travel
// rebuilds it. Lock is that lockfile. MadeBy and Cwd say which command made the
// folder, and are empty when the folder was there before the chat or came from
// another terminal.
type Withheld struct {
	Path   string `json:"path"`
	Lock   string `json:"lock"`
	MadeBy string `json:"made_by,omitempty"`
	Cwd    string `json:"cwd,omitempty"`
}

// Detached is a container or service a command started outside its own process
// tree, so no process of the chat stands for it. It is a hint that something was
// started, never a promise about what it holds.
type Detached struct {
	Command string `json:"command"`
	Cwd     string `json:"cwd,omitempty"`
}

// Inventory is .cell/env/inventory.json. V is first so readers can branch on it.
type Inventory struct {
	V        uint16    `json:"V"`
	Tools    []Tool    `json:"tools,omitempty"`
	Services []Service `json:"services,omitempty"`
	Platform Platform  `json:"platform"`
	// Workspace is the folder the seal was taken in, so a machine that receives
	// the chat can say the folder changed. It is display only: nothing is ever
	// resolved against it (L1).
	Workspace   string   `json:"workspace,omitempty"`
	Lockfiles   []string `json:"lockfiles,omitempty"`
	EnvVarNames []string `json:"env_var_names,omitempty"`
	// Running, Withheld and Detached are what the chat left behind that a seal
	// does not carry (docs/STAGE-1-CONTRACTS.md section 19). The harness writes
	// them from what it sees and nothing else does (L13); Omitted counts, per
	// list, the entries a bound cut.
	Running     []Running                 `json:"running,omitempty"`
	Withheld    []Withheld                `json:"withheld,omitempty"`
	Detached    []Detached                `json:"detached,omitempty"`
	Omitted     map[string]int            `json:"omitted,omitempty"`
	Hints       map[string]string         `json:"hints,omitempty"`
	Annotations map[string]map[string]any `json:"annotations,omitempty"`
}
