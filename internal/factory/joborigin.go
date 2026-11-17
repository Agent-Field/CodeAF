package factory

import (
	"slices"
	"strings"
)

// AgentFieldSource is the source name of a job an AgentField reasoner runs
// ([JobSource]).
const AgentFieldSource = "agentfield"

// agentFieldTriggerPrefix begins the origin of every job an AgentField
// trigger filed (internal/factory/agentfield's SourcePrefix, which imports
// this package and so cannot be imported here).
const agentFieldTriggerPrefix = "agentfield:"

// JobOrigin is where on AgentField a job came from: the node and reasoner
// that run it, and the trigger whose event filed it.
type JobOrigin struct {
	Node     string `json:"node"`
	Reasoner string `json:"reasoner,omitempty"`
	Trigger  string `json:"trigger,omitempty"`
}

// JobOriginOf is the job's [JobOrigin]; ok is false for a job no AgentField
// node runs. THE NODE IS REQUIRED: an origin is a claim about which machine
// does the work, and a job this factory runs itself has none to name, even
// when an AgentField trigger filed it ([JobSource] still says so).
//
// The trigger is the source the control plane's trigger listens to
// (`generic-bearer`, `linear`), which is the only name a trigger has there; a
// direct execute is not a trigger.
func JobOriginOf(it Item) (JobOrigin, bool) {
	node, reasoner, _ := strings.Cut(strings.TrimSpace(it.RunOn), ".")
	if node == "" {
		return JobOrigin{}, false
	}
	o := JobOrigin{Node: node, Reasoner: reasoner}
	if t, ok := strings.CutPrefix(string(it.Origin), agentFieldTriggerPrefix); ok && t != "" && t != "execute" {
		o.Trigger = t
	}
	return o, true
}

// JobLabelsMost is how many labels a job's read carries.
const JobLabelsMost = 20

// JobLabels is the job's labels as a reader filters on them: the source's own
// (a GitHub issue's labels, a Jira ticket's labels and components, an
// AgentField event's labels and tags — each gathered where the job is filed,
// with nothing for the developer to do) and, for a job an AgentField node
// runs, `node:<node>`. Trimmed, lowercased, each once in its first place,
// at most [JobLabelsMost]; the node's label always survives the cap.
func JobLabels(it Item) []string {
	var out []string
	add := func(l string) {
		l = strings.ToLower(strings.TrimSpace(l))
		if l != "" && !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	node := ""
	if o, ok := JobOriginOf(it); ok {
		node = "node:" + strings.ToLower(o.Node)
		add(node)
	}
	for _, l := range it.Labels {
		add(l)
	}
	if len(out) > JobLabelsMost {
		out = out[:JobLabelsMost]
	}
	return out
}
