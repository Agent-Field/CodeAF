package appapi

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/standing"
)

// The app's numbered rules live in the binary. Resource lanes consume these
// defaults when they add their routes, so UI code never supplies a fallback.
const (
	DefaultTaskProposalSeconds = config.DefaultTaskAutoApprove
	DefaultTaskCapUSD          = config.CrewTaskCapDefault
	DefaultDayCapUSD           = config.AppDefaultDayCap
	DefaultStandingRunUSD      = standing.DefaultPerRunUSD
	QuestionClock              = config.AppDefaultQuestionClockMinutes * time.Minute
	ProcessOutputRetention     = session.AppProcessOutputRetention
	MaxProcessOutputBytes      = session.AppProcessOutputBytes
	QuickTaskFileLimit         = session.QuickTaskFileLimit
	IdleAgentAfter             = config.AppDefaultIdleDays * 24 * time.Hour
)

// StandingScope names where an order may speak, rather than where it looks.
// The order resource lane persists this field and enforces crew membership.
type StandingScope string

const (
	ScopeThisChat   StandingScope = "this_chat"
	ScopeThisCrew   StandingScope = "this_crew"
	ScopeEverywhere StandingScope = "everywhere"
	ScopeNotHere    StandingScope = "not_here"
)

// AgentIsIdle derives selection eligibility without changing the agent. An
// unknown message time stays unknown; background writes never reset the clock.
// The running flag covers the agent's turn, tasks and processes together.
func AgentIsIdle(lastMessageAt time.Time, running bool, now time.Time) bool {
	return !running && !lastMessageAt.IsZero() && !lastMessageAt.After(now.Add(-IdleAgentAfter))
}

// QuickTaskNeedsProposal decides whether work must be proposed as a full task.
// The task resource lane owns checking it before admission and as files grow;
// running a test always requires a full task.
func QuickTaskNeedsProposal(files []string, runsTests bool) bool {
	if runsTests {
		return true
	}
	unique := make(map[string]struct{}, len(files))
	for _, file := range files {
		unique[file] = struct{}{}
		if len(unique) > QuickTaskFileLimit {
			return true
		}
	}
	return false
}
