package appwire

// AppAILaunch selects a declared proposal action and a bounded record scope.
type AppAILaunch struct {
	App     string         `json:"app"`
	Action  string         `json:"action"`
	Screen  string         `json:"screen,omitempty"`
	Records []string       `json:"records,omitempty"`
	Query   *string        `json:"query,omitempty"`
	Params  map[string]any `json:"params,omitempty"`
}

type AppAIReview struct {
	App      string           `json:"app"`
	Queue    string           `json:"queue"`
	Decision string           `json:"decision"`
	IDs      []AppAISelection `json:"ids"`
}

type AppAISelection struct {
	ID          string `json:"id"`
	ExpectedRev string `json:"expected_rev"`
}

type AppAILaunched struct {
	Queue string `json:"queue"`
}

type appAIActions interface {
	LaunchAppAI(AppAILaunch) (string, error)
	ReviewAppAI(AppAIReview) error
}

func init() {
	methods["agents.launch"] = typed(func(e Engine, p AppAILaunch) (*AppAILaunched, error) {
		if api, ok := e.(appAIActions); ok {
			queue, err := api.LaunchAppAI(p)
			return &AppAILaunched{Queue: queue}, err
		}
		return nil, Errorf(CodeUnknownMethod, "App AI actions are unavailable.")
	})
	methods["agents.review"] = typed(func(e Engine, p AppAIReview) (*Empty, error) {
		if api, ok := e.(appAIActions); ok {
			return done(api.ReviewAppAI(p))
		}
		return nil, Errorf(CodeUnknownMethod, "App AI review is unavailable.")
	})
}
