package session

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

const workstreamLineMax = 200

const workstreamRecordMax = 2000

const workstreamDraftTextMax = 4000

const workstreamJobsMax = 20

const workstreamGoAheadCapMax = workspace.WorkstreamGoAheadCapMax

// Ending at the UTC day's last second keeps the proposal within the grant limit.
const workstreamGoAheadDaysMax = int(workspace.WorkstreamGoAheadDurationMax/(24*time.Hour)) - 1

const workstreamGoAheadWhyMax = 1000
