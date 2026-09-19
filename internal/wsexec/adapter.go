package wsexec

import (
	"fmt"
	"os"
	"strings"
)

// Adapter holds the injected store and runtime. Production wire constructs
// this with the real workspace store and a Runtime over StartTask /
// RegisterRunEngine. Tests inject fakes.
type Adapter struct {
	store   Store
	runtime Runtime
}

// Open builds the adapter. Nil store or runtime is labelled absence on every
// verb — there is no production memory fallback and no dummy completed view.
func Open(store Store, runtime Runtime) *Adapter {
	return &Adapter{store: store, runtime: runtime}
}

func (a *Adapter) require() error {
	if a == nil || a.store == nil || a.runtime == nil {
		return ErrAbsent
	}
	return nil
}

// currentRoad reads the same switch StartTask already honours. Unset is the
// session task tree; bash is the run engine. Tests are table-driven across both.
func currentRoad() string {
	if os.Getenv("CODEAF_TASK_BELT") == "bash" {
		return RoadBashRun
	}
	return RoadSessionTask
}

func joinable(state string) bool {
	switch state {
	case BindReserved, BindAdmitted, BindBound, BindPaused:
		return true
	default:
		return false
	}
}

func bindingView(b ExecutionBinding, joined bool) WorkView {
	workID := b.WorkID
	if workID == "" {
		workID = b.RequestKey
	}
	return WorkView{
		WorkID:        workID,
		RequestKey:    b.RequestKey,
		RunInstanceID: b.RunInstanceID,
		Road:          b.Road,
		State:         b.State,
		OwnerChatID:   b.OwnerChatID,
		GrantID:       b.GrantID,
		Joined:        joined,
	}
}

func validateLaunch(req LaunchRequest) error {
	if strings.TrimSpace(req.RequestKey) == "" {
		return fmt.Errorf("%w: request key is empty", ErrInvalid)
	}
	if strings.TrimSpace(req.GrantID) == "" {
		return fmt.Errorf("%w: grant id is empty", ErrInvalid)
	}
	return nil
}
