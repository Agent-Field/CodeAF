// This file ports the narrow runtime seams used by
// src/session/review-gate.ts:518-605. Effect services outside this bundle are
// represented by agentjson's resolver/client and the in-process PlanDB bridge.
package reviewgate

import (
	"fmt"
	"sync/atomic"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/agentjson"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/scheduler"
)

type PlanDBRunner interface {
	Run(argv []string) plandb.RunResult
}

type PlanDBRunnerFunc func(argv []string) plandb.RunResult

func (fn PlanDBRunnerFunc) Run(argv []string) plandb.RunResult { return fn(argv) }

type nativePlanDBRunner struct{}

func (nativePlanDBRunner) Run(argv []string) plandb.RunResult {
	return plandb.RunPlanDB(argv)
}

type Dependencies struct {
	Config    *Config
	PlanDB    PlanDBRunner
	AgentJSON agentjson.Dependencies
	Command   CommandFunc
	NewID     func(prefix string) string
}

type Service struct {
	config Config
	planDB PlanDBRunner
	agent  agentjson.Dependencies
	run    CommandFunc
	newID  func(prefix string) string
}

func New(deps Dependencies) *Service {
	config := LoadConfig()
	if deps.Config != nil {
		config = *deps.Config
		config.SkipGlobs = append([]string(nil), deps.Config.SkipGlobs...)
	}
	planDB := deps.PlanDB
	if planDB == nil {
		planDB = nativePlanDBRunner{}
	}
	run := deps.Command
	if run == nil {
		run = runCommand
	}
	newID := deps.NewID
	if newID == nil {
		newID = nextGateID
	}
	if deps.AgentJSON.NewID == nil {
		deps.AgentJSON.NewID = newID
	}
	return &Service{
		config: config, planDB: planDB, agent: deps.AgentJSON,
		run: run, newID: newID,
	}
}

func (s *Service) runPlanDB(args ...string) plandb.RunResult {
	return s.planDB.Run(append([]string{"plandb"}, args...))
}

var gateIDCounter atomic.Uint64

func nextGateID(prefix string) string {
	return fmt.Sprintf("%s_%016x", prefix, gateIDCounter.Add(1))
}

var _ scheduler.ReviewGate = (*Service)(nil)
