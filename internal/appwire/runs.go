package appwire

import "github.com/Agent-Field/codeaf/internal/apps/runs"

const EventRunsChanged = "runs.changed"

type RunsChanged = runs.ChangeEvent

type RunStarter interface {
	StartRun(app, run string, params map[string]any) (runs.Run, error)
}

type RunReader interface {
	ListRuns(app, run string) ([]runs.Run, error)
	GetRun(app, id string) (runs.Run, error)
}

type RunStartParams struct {
	App    string         `json:"app"`
	Run    string         `json:"run"`
	Draft  string         `json:"draft,omitempty"`
	Params map[string]any `json:"params,omitempty"`
}

type RunListParams struct {
	App string `json:"app,omitempty"`
	Run string `json:"run,omitempty"`
}

type RunGetParams struct {
	App string `json:"app,omitempty"`
	ID  string `json:"id"`
}

type RunStarted struct {
	ID string `json:"id"`
}

type RunListResult struct {
	Runs []runs.Run `json:"runs"`
}

func init() {
	methods["runs.start"] = typed(func(e Engine, p RunStartParams) (*RunStarted, error) {
		s, ok := e.(RunStarter)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve runs.start")
		}
		app, err := required("app", p.App)
		if err != nil {
			return nil, err
		}
		run, err := required("run", p.Run)
		if err != nil {
			return nil, err
		}
		if p.Draft != "" {
			return nil, Errorf(CodeBadParams, "draft runs require a preview engine")
		}
		result, err := s.StartRun(app, run, p.Params)
		return &RunStarted{ID: result.ID}, err
	})
	methods["runs.list"] = typed(func(e Engine, p RunListParams) (*RunListResult, error) {
		reader, ok := e.(RunReader)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve runs.list")
		}
		list, err := reader.ListRuns(p.App, p.Run)
		return &RunListResult{Runs: nonNil(list)}, err
	})
	methods["runs.get"] = typed(func(e Engine, p RunGetParams) (*runs.Run, error) {
		reader, ok := e.(RunReader)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve runs.get")
		}
		id, err := required("id", p.ID)
		if err != nil {
			return nil, err
		}
		result, err := reader.GetRun(p.App, id)
		return &result, err
	})
}
