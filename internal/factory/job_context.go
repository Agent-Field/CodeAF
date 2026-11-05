package factory

// Job metadata bounds are shared by the wire and the Factory HTTP handlers.
const (
	MaxJobLabel         = 200
	MaxJobContextItems  = 20
	MaxJobContext       = 100000
	MaxJobContextTitle  = 200
	MaxJobContextSource = 500
)

// JobContext is reference material attached before a job's first run.
type JobContext struct {
	Title    string `json:"title"`
	Markdown string `json:"markdown"`
	Source   string `json:"source,omitempty"`
}
