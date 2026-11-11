package appwire

import (
	"reflect"
	"strings"
	"time"
)

// RENDER.LIST, RENDER.GET AND RENDER.CANCEL are the app's door to the video and
// music renders a conversation has started. They are an OPTIONAL capability: an
// engine that does not implement [RenderController] — an older host, a remote
// one — answers unknown_method, which means "unsupported", and is never
// answered with an empty list. The contract, field by field, is
// the render contract (RENDER-CONTRACT.md in the media design notes).

// The bounds of render.list.
const (
	RenderPageDefault = 50
	RenderPageMax     = 100
)

// Render is one video or music render. Unknown stays unknown: an absent charge
// is "unknown", never a zero.
type Render struct {
	Persistence string     `json:"persistence,omitempty"`
	ID          string     `json:"id"`
	Job         int        `json:"job"`
	Session     string     `json:"session,omitempty"`
	Kind        string     `json:"kind"`
	State       string     `json:"state"`
	Model       string     `json:"model"`
	Started     time.Time  `json:"started"`
	Ended       *time.Time `json:"ended,omitempty"`
	DurationMS  *int64     `json:"duration_ms,omitempty"`
	Run         string     `json:"run,omitempty"`
	Call        string     `json:"call,omitempty"`
	Committed   bool       `json:"committed"`
	Path        string     `json:"path,omitempty"`
	Charge      string     `json:"charge"`
	CostUSD     *float64   `json:"cost_usd,omitempty"`
	Upstream    string     `json:"upstream,omitempty"`
	Error       string     `json:"error,omitempty"`
	Refusal     string     `json:"refusal,omitempty"`
	Options     []string   `json:"options,omitempty"`
}

// RenderPage is one window of renders, newest first.
type RenderPage struct {
	Renders []Render `json:"renders"`
	// Next is absent only when the whole history has been read. Incomplete says
	// the page is short because the read budget ran out: keep following Next.
	Next       string `json:"next,omitempty"`
	Incomplete bool   `json:"incomplete,omitempty"`
}

// EventRenderChanged carries the whole record of a render that started,
// finished, failed, was cancelled, or gained a charge receipt after it ended.
// It replaces the record with the same id; it is never a second completion.
const EventRenderChanged = "render.changed"

// RenderChanged is render.changed's data.
type RenderChanged struct {
	Agent  string `json:"agent"`
	Render Render `json:"render"`
}

// RenderCancel is render.cancel's answer: cancelled, too_late or already_ended.
type RenderCancel struct {
	Outcome string `json:"outcome"`
	Render  Render `json:"render"`
}

// RenderResult is render.get's answer.
type RenderResult struct {
	Render Render `json:"render"`
}

// RenderController is the optional engine half of the render methods.
type RenderController interface {
	ListRenders(agent, cursor string, limit int) (RenderPage, error)
	GetRender(agent, id string) (Render, error)
	// CancelRender never waits on the provider and never claims the provider
	// was told to cancel.
	CancelRender(agent, id string) (RenderCancel, error)
}

func init() {
	eventShapes[EventRenderChanged] = reflect.TypeFor[RenderChanged]()
	methods["render.list"] = typed(func(e Engine, p struct {
		Agent  string `json:"agent"`
		Cursor string `json:"cursor,omitempty"`
		Limit  int    `json:"limit,omitempty"`
	}) (*RenderPage, error) {
		c, ok := e.(RenderController)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve render.list")
		}
		if strings.TrimSpace(p.Agent) == "" || p.Limit < 0 {
			return nil, Errorf(CodeBadParams, "render.list needs an agent and a limit of zero or more")
		}
		limit := p.Limit
		if limit == 0 {
			limit = RenderPageDefault
		}
		if limit > RenderPageMax {
			limit = RenderPageMax
		}
		page, err := c.ListRenders(p.Agent, p.Cursor, limit)
		if err != nil {
			return nil, err
		}
		if page.Renders == nil {
			page.Renders = []Render{}
		}
		return &page, nil
	})
	methods["render.get"] = typed(func(e Engine, p struct {
		Agent string `json:"agent"`
		ID    string `json:"id"`
	}) (*RenderResult, error) {
		c, ok := e.(RenderController)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve render.get")
		}
		if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.ID) == "" {
			return nil, Errorf(CodeBadParams, "render.get needs an agent and an id")
		}
		render, err := c.GetRender(p.Agent, p.ID)
		if err != nil {
			return nil, err
		}
		return &RenderResult{Render: render}, nil
	})
	methods["render.cancel"] = typed(func(e Engine, p struct {
		Agent string `json:"agent"`
		ID    string `json:"id"`
	}) (*RenderCancel, error) {
		c, ok := e.(RenderController)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve render.cancel")
		}
		if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.ID) == "" {
			return nil, Errorf(CodeBadParams, "render.cancel needs an agent and an id")
		}
		result, err := c.CancelRender(p.Agent, p.ID)
		if err != nil {
			return nil, err
		}
		return &result, nil
	})
}
