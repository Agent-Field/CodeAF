package appwire

import "reflect"

const CodeDeskUnavailable = "desk_unavailable"

const EventAppLookRequested = "apps.look_requested"

type AppLookViewport struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type AppLookParams struct {
	Theme      string           `json:"theme,omitempty"`
	Viewport   *AppLookViewport `json:"viewport,omitempty"`
	Checkpoint string           `json:"checkpoint,omitempty"`
	App        string           `json:"app,omitempty"`
	Draft      string           `json:"draft,omitempty"`
	Screen     string           `json:"screen"`
	Params     map[string]any   `json:"params,omitempty"`
}

type AppLookRequest struct {
	Checkpoint string           `json:"checkpoint,omitempty"`
	Theme      string           `json:"theme,omitempty"`
	Viewport   *AppLookViewport `json:"viewport,omitempty"`
	Fixture    string           `json:"fixture,omitempty"`
	ID         string           `json:"id"`
	Scope      string           `json:"scope"`
	App        string           `json:"app"`
	Screen     string           `json:"screen"`
	Params     map[string]any   `json:"params,omitempty"`
}

type AppLookPage struct {
	PNGPath  string   `json:"png_path,omitempty"`
	Problems []string `json:"problems"`
}

type AppLookCapture struct {
	ID       string   `json:"id"`
	PNG      string   `json:"png,omitempty"`
	Problems []string `json:"problems,omitempty"`
	Problem  string   `json:"problem,omitempty"`
}

type AppLookRenderer interface {
	LookApp(AppLookParams) (AppLookPage, error)
	AppLookReady(bool)
	FinishAppLook(AppLookCapture) error
}

func init() {
	eventShapes[EventAppLookRequested] = reflect.TypeFor[AppLookRequest]()
	methods["apps.look"] = typed(func(e Engine, p AppLookParams) (*AppLookPage, error) {
		look, ok := e.(AppLookRenderer)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "This engine does not serve app look.")
		}
		res, err := look.LookApp(p)
		return &res, err
	})
	methods["apps.look_ready"] = typed(func(e Engine, p struct {
		Ready bool `json:"ready"`
	}) (*Empty, error) {
		look, ok := e.(AppLookRenderer)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "This engine does not serve app look.")
		}
		look.AppLookReady(p.Ready)
		return &Empty{}, nil
	})
	methods["apps.look_result"] = typed(func(e Engine, p AppLookCapture) (*Empty, error) {
		look, ok := e.(AppLookRenderer)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "This engine does not serve app look.")
		}
		return done(look.FinishAppLook(p))
	})
}
