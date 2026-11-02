package appwire

import "time"

// health.check: plain facts about this engine an assistant can cite when the
// person asks whether something is wrong. Every fact is read, never inferred,
// and the call is cheap: no network and no child process.

// The roles a disk reading is taken for.
const (
	DiskWorkFolder = "work_folder"
	DiskCodeafHome = "codeaf_home"
)

// Health is the engine as it stands. Resume is the replies and tasks this
// start set to carry on after the last quit, absent when there were none;
// Factory is this machine's local factory as factory.local_status shapes it,
// absent from an engine that does not say.
type Health struct {
	Version   string              `json:"version"`
	Build     string              `json:"build,omitempty"`
	StartedAt time.Time           `json:"started_at"`
	UptimeS   int64               `json:"uptime_s"`
	Resume    *HealthResume       `json:"resume,omitempty"`
	Disks     []HealthDisk        `json:"disks"`
	Factory   *FactoryLocalStatus `json:"factory,omitempty"`
}

// HealthResume is what a start carried on after a quit: when, and how many
// replies and tasks. RepliesResumed says at least one reply carried on.
type HealthResume struct {
	At             time.Time `json:"at"`
	RepliesResumed bool      `json:"replies_resumed"`
	Replies        int       `json:"replies"`
	Tasks          int       `json:"tasks"`
}

// HealthDisk is one filesystem reading: free and total bytes for the
// filesystem Path is on, or Error when it could not be read.
type HealthDisk struct {
	Role       string `json:"role" enum:"work_folder,codeaf_home"`
	Path       string `json:"path"`
	FreeBytes  uint64 `json:"free_bytes"`
	TotalBytes uint64 `json:"total_bytes"`
	Error      string `json:"error,omitempty"`
}

// HealthResult is health.check's answer.
type HealthResult struct {
	Health Health `json:"health"`
}

// HealthChecker is the engine's half of health.check.
type HealthChecker interface {
	HealthCheck() (Health, error)
}

// FeatureHealthCheck is hello's word for an engine that is a [HealthChecker].
const FeatureHealthCheck = "health.check"

func init() {
	methods["health.check"] = typed(func(e Engine, _ struct{}) (*HealthResult, error) {
		checker, ok := e.(HealthChecker)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve health.check")
		}
		health, err := checker.HealthCheck()
		if err != nil {
			return nil, err
		}
		health.Disks = nonNil(health.Disks)
		return &HealthResult{Health: health}, nil
	})
}

func withHealthFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(HealthChecker); !ok {
		return hello
	}
	return withFeature(hello, FeatureHealthCheck)
}
