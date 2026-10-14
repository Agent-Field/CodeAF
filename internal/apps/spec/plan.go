package spec

// Plan is plan.json, the source of the plan card. Every line carries an id
// so the card can edit lines in place (builder.plan.edit).
type Plan struct {
	Ask        string     `json:"ask"`
	Name       string     `json:"name"`
	Tint       string     `json:"tint,omitempty"`
	Data       []PlanLine `json:"data"`
	Automation []PlanLine `json:"automation"`
	Screens    []PlanLine `json:"screens"`
	Agent      PlanLine   `json:"agent"`
	Needs      string     `json:"needs,omitempty"`
}

// PlanLine is one editable line on the plan card.
type PlanLine struct {
	ID   string `json:"id,omitempty"`
	Line string `json:"line"`
}
