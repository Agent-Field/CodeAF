package factory

// Creation bounds are shared by the wire and the Factory HTTP handler.
const (
	MaxJobLabel         = 200
	MaxJobContextItems  = 20
	MaxJobContext       = 100000
	MaxJobContextTitle  = 200
	MaxJobContextSource = 500
)

// JobContext is reference material attached when a person creates a job.
type JobContext struct {
	Title    string `json:"title"`
	Markdown string `json:"markdown"`
	Source   string `json:"source,omitempty"`
}
