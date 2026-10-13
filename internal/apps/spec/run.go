package spec

// Run is runs/<id>.yaml after YAML is parsed to JSON.
type Run struct {
	Run    string               `json:"run"`
	Title  string               `json:"title,omitempty"`
	Budget *RunBudget           `json:"budget,omitempty"`
	Params map[string]*RunParam `json:"params,omitempty"`
	Steps  []*Step              `json:"steps"`
}

// RunBudget caps one run in USD.
type RunBudget struct {
	PerRun float64 `json:"per_run"`
}

// RunParam declares one input of a run.
type RunParam struct {
	Type    string   `json:"type"`
	To      string   `json:"to,omitempty"`
	Values  []string `json:"values,omitempty"`
	Default any      `json:"default,omitempty"`
}

// Step is one run step. Besides an optional Gate, a step does one thing:
// Agent, Tool, a records.* call, Wait, nested Steps (with Loop) or Shell.
type Step struct {
	ID            string         `json:"id"`
	Title         string         `json:"title,omitempty"`
	If            string         `json:"if,omitempty"`
	OnError       string         `json:"on_error,omitempty"`
	Retry         *Retry         `json:"retry,omitempty"`
	Agent         string         `json:"agent,omitempty"`
	Prompt        string         `json:"prompt,omitempty"`
	Output        map[string]any `json:"output,omitempty"`
	Tool          string         `json:"tool,omitempty"`
	As            string         `json:"as,omitempty"`
	Args          map[string]any `json:"args,omitempty"`
	Gate          *Gate          `json:"gate,omitempty"`
	RecordsQuery  *RecordsStep   `json:"records.query,omitempty"`
	RecordsCreate *RecordsStep   `json:"records.create,omitempty"`
	RecordsUpdate *RecordsStep   `json:"records.update,omitempty"`
	RecordsDelete *RecordsStep   `json:"records.delete,omitempty"`
	Wait          *Wait          `json:"wait,omitempty"`
	OnTimeout     string         `json:"on_timeout,omitempty"`
	Loop          *Loop          `json:"loop,omitempty"`
	Steps         []*Step        `json:"steps,omitempty"`
	Workspace     string         `json:"workspace,omitempty"`
	Repo          string         `json:"repo,omitempty"`
	Branch        string         `json:"branch,omitempty"`
	Shell         string         `json:"shell,omitempty"`
}

// Retry retries a failed step, never a gate "No".
type Retry struct {
	Max     int    `json:"max"`
	Backoff string `json:"backoff,omitempty"`
}

// Gate is an approval a person answers before the step acts.
type Gate struct {
	Approval string `json:"approval"`
	Who      string `json:"who,omitempty"`
	Title    string `json:"title,omitempty"`
}

// RecordsStep is the argument of a records.* step.
type RecordsStep struct {
	Type  string         `json:"type,omitempty"`
	Query string         `json:"query,omitempty"`
	ID    string         `json:"id,omitempty"`
	IDs   any            `json:"ids,omitempty"`
	Set   map[string]any `json:"set,omitempty"`
	Limit int            `json:"limit,omitempty"`
}

// Wait pauses a run on an event, a time or a duration.
type Wait struct {
	For     string `json:"for,omitempty"`
	Until   string `json:"until,omitempty"`
	Thread  string `json:"thread,omitempty"`
	Timeout string `json:"timeout,omitempty"`
}

// Loop repeats nested steps over a list or until a condition; Max is required.
type Loop struct {
	Over  string `json:"over,omitempty"`
	As    string `json:"as,omitempty"`
	Until string `json:"until,omitempty"`
	Max   int    `json:"max"`
}
