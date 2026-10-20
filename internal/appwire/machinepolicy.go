package appwire

import (
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/session"
	"strings"
	"time"
)

// MachineCache is local to the app process; it never addresses a joined host.
type MachineCache interface {
	CacheStatus() (CacheStatus, error)
	CleanCache() (CacheCleanResult, error)
}
type CacheStatus struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Human string `json:"human"`
}
type CacheCleanParams struct {
	Confirm bool `json:"confirm"`
}
type CacheCleanResult struct {
	FreedBytes int64 `json:"freed_bytes"`
}

// ProjectAutonomy uses the held conversation's own door, including over a host.
type ProjectAutonomy interface {
	GetAutonomy(string) (AutonomyResult, error)
	SetAutonomy(AutonomySetParams) (AutonomyResult, error)
}
type AutonomyRow struct {
	Kind  session.AskKind `json:"kind"`
	Rule  string          `json:"rule" enum:"ask,recommend,decide"`
	After string          `json:"after,omitempty"`
}
type AutonomyResult struct {
	Agent string        `json:"agent"`
	Kinds []AutonomyRow `json:"kinds"`
}
type AutonomySetParams struct {
	Agent string `json:"agent"`
	Kind  string `json:"kind"`
	Rule  string `json:"rule"`
}

// SavedHarnessLister reads the same registry snapshot as the terminal panel.
// Choosing a chip is client draft state and deliberately has no use method.
type SavedHarnessLister interface {
	SavedHarnesses() (HarnessListResult, error)
}
type HarnessListRow struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Version     int             `json:"version"`
	Runs        int             `json:"runs"`
	LastRun     *HarnessLastRun `json:"last_run,omitempty"`
}

// HarnessLastRun carries only the time and outcome recorded by a saved trace.
type HarnessLastRun struct {
	At       time.Time `json:"at,omitzero"`
	Finished bool      `json:"finished"`
}
type HarnessListResult struct {
	Harnesses []HarnessListRow `json:"harnesses"`
}

// CrewPolicyEditor fills only the gaps left by settings.models: dynamic rules
// and the crew daily cap, which is distinct from the machine spending limit.
type CrewPolicyEditor interface {
	CrewPolicy() (CrewPolicy, error)
	SetCrewPolicy(CrewPolicyPatch) (CrewPolicy, error)
}
type CrewPolicy struct {
	Models string  `json:"models"`
	CapUSD float64 `json:"cap_usd"`
}
type CrewPolicyPatch struct {
	Models *string `json:"models,omitempty"`
	Cap    *string `json:"cap,omitempty"`
}

// strictMachineParams refuses misspelled confirmation and policy fields before
// a producer runs, instead of silently accepting a partial request.
func strictMachineParams[T any](raw json.RawMessage) (T, error) {
	var p T
	err := decodeStrict("machine policy", raw, &p)
	return p, err
}

func init() {
	methods["cache.status"] = shaped[struct{}](func(e Engine, raw json.RawMessage) (*CacheStatus, error) {
		if _, err := strictMachineParams[struct{}](raw); err != nil {
			return nil, err
		}
		door, ok := e.(MachineCache)
		if !ok {
			return nil, missingMachineMethod("cache.status")
		}
		got, err := door.CacheStatus()
		return &got, err
	})
	methods["cache.clean"] = shaped[CacheCleanParams](func(e Engine, raw json.RawMessage) (*CacheCleanResult, error) {
		p, err := strictMachineParams[CacheCleanParams](raw)
		if err != nil {
			return nil, err
		}
		door, ok := e.(MachineCache)
		if !ok {
			return nil, missingMachineMethod("cache.clean")
		}
		if !p.Confirm {
			return nil, Errorf(CodeBadParams, "cache.clean needs confirm: true after asking before deletion")
		}
		got, err := door.CleanCache()
		return &got, err
	})
	methods["autonomy.get"] = shaped[AgentParams](func(e Engine, raw json.RawMessage) (*AutonomyResult, error) {
		p, err := strictMachineParams[AgentParams](raw)
		if err != nil {
			return nil, err
		}
		door, ok := e.(ProjectAutonomy)
		if !ok {
			return nil, missingMachineMethod("autonomy.get")
		}
		agent, err := machinePolicyAgent(p.Agent)
		if err != nil {
			return nil, err
		}
		got, err := door.GetAutonomy(agent)
		return &got, err
	})
	methods["autonomy.set"] = shaped[AutonomySetParams](func(e Engine, raw json.RawMessage) (*AutonomyResult, error) {
		p, err := strictMachineParams[AutonomySetParams](raw)
		if err != nil {
			return nil, err
		}
		door, ok := e.(ProjectAutonomy)
		if !ok {
			return nil, missingMachineMethod("autonomy.set")
		}
		p.Agent, err = machinePolicyAgent(p.Agent)
		if err != nil {
			return nil, err
		}
		if len(p.Kind) > 32 || len(p.Rule) > 64 || session.AutonomyKindNamed(p.Kind) == "" {
			return nil, Errorf(CodeBadParams, "unknown question kind")
		}
		parts := strings.Fields(p.Rule)
		if len(parts) == 0 || (parts[0] != "recommend" && len(parts) != 1) {
			return nil, Errorf(CodeBadParams, "rule is ask, recommend [duration], or decide")
		}
		_, _, refused := session.ParseAutonomyLine(p.Kind + " " + p.Rule)
		if refused != "" {
			return nil, Errorf(CodeBadParams, "%s", refused)
		}
		got, err := door.SetAutonomy(p)
		return &got, err
	})
	methods["harness.list"] = shaped[struct{}](func(e Engine, raw json.RawMessage) (*HarnessListResult, error) {
		if _, err := strictMachineParams[struct{}](raw); err != nil {
			return nil, err
		}
		door, ok := e.(SavedHarnessLister)
		if !ok {
			return nil, missingMachineMethod("harness.list")
		}
		got, err := door.SavedHarnesses()
		got.Harnesses = nonNil(got.Harnesses)
		return &got, err
	})
	methods["settings.crew.get"] = shaped[struct{}](func(e Engine, raw json.RawMessage) (*CrewPolicy, error) {
		if _, err := strictMachineParams[struct{}](raw); err != nil {
			return nil, err
		}
		door, ok := e.(CrewPolicyEditor)
		if !ok {
			return nil, missingMachineMethod("settings.crew.get")
		}
		got, err := door.CrewPolicy()
		return &got, err
	})
	methods["settings.crew.set"] = shaped[CrewPolicyPatch](func(e Engine, raw json.RawMessage) (*CrewPolicy, error) {
		p, err := strictMachineParams[CrewPolicyPatch](raw)
		if err != nil {
			return nil, err
		}
		door, ok := e.(CrewPolicyEditor)
		if !ok {
			return nil, missingMachineMethod("settings.crew.set")
		}
		// One write per call avoids leaving half a patch applied on a refusal.
		if (p.Models == nil) == (p.Cap == nil) {
			return nil, Errorf(CodeBadParams, "set exactly one of models or cap")
		}
		if p.Models != nil && (strings.TrimSpace(*p.Models) == "" || len(*p.Models) > 16384) {
			return nil, Errorf(CodeBadParams, "models needs 1 to 16384 bytes")
		}
		if p.Cap != nil && (strings.TrimSpace(*p.Cap) == "" || len(*p.Cap) > 64) {
			return nil, Errorf(CodeBadParams, "cap is at most 64 bytes")
		}
		got, err := door.SetCrewPolicy(p)
		return &got, err
	})
}

// Agent ids are opaque, bounded single-line identifiers just as on other reads.
func machinePolicyAgent(agent string) (string, error) {
	if len(agent) > 256 || strings.TrimSpace(agent) != agent || strings.ContainsAny(agent, "\x00\r\n") {
		return "", Errorf(CodeBadParams, "agent is a single-line id of at most 256 bytes")
	}
	return required("agent", agent)
}

func missingMachineMethod(name string) error {
	return Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
}

func withMachinePolicyFeatures(hello Hello, e Engine) Hello {
	if _, ok := e.(MachineCache); ok {
		hello = withFeature(withFeature(hello, "cache.status"), "cache.clean")
	}
	if _, ok := e.(ProjectAutonomy); ok {
		hello = withFeature(withFeature(hello, "autonomy.get"), "autonomy.set")
	}
	if _, ok := e.(SavedHarnessLister); ok {
		hello = withFeature(hello, "harness.list")
	}
	if _, ok := e.(CrewPolicyEditor); ok {
		hello = withFeature(withFeature(hello, "settings.crew.get"), "settings.crew.set")
	}
	return hello
}
