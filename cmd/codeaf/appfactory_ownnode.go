package main

import (
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// afOwnNodeRefusal is what a person reads when a rule or a pipeline step
// names this organization's own AgentField node as the agent to run on.
const afOwnNodeRefusal = "that agent is this organization itself; pick another"

// isOwnNode says target ("<node>.<reasoner>") is on self, this organization's
// own AgentField node. Node ids are compared as the control plane lists them,
// ignoring case, the way [appBackend.routeReasonersKnown] compares reasoners.
//
// THE OWN NODE IS A LOOP, NEVER A TARGET: its intake files the run as a new
// job, which matches the same rule (or carries the same step) and dispatches
// again, without end.
func isOwnNode(target, self string) bool {
	self = strings.TrimSpace(self)
	if self == "" {
		return false
	}
	node, _, _ := strings.Cut(strings.TrimSpace(target), ".")
	return strings.EqualFold(node, self)
}

// ownNodeSteps refuses pipeline text whose agentfield step runs on this
// organization's own node, before the text is saved anywhere. Only this
// machine's factory holds the connection that knows its node id; a team
// factory's file is held to its spelling alone, and text that does not parse
// is left to the save's own refusal.
func (b *appBackend) ownNodeSteps(id, text string) error {
	host, err := b.localHostOf(id)
	if err != nil || host == nil {
		return nil
	}
	_, self, connected := host.agentFieldConn()
	if !connected {
		return nil
	}
	return ownNodeStepIn(text, self)
}

// ownNodeStepIn is [appBackend.ownNodeSteps] once the node id is known.
func ownNodeStepIn(text, self string) error {
	f, err := pipeline.Parse(text)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(f.Steps))
	for name := range f.Steps {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if st := f.Steps[name]; st.Kind == string(factory.StageAgentField) && isOwnNode(st.Prompt, self) {
			return appwire.Errorf(appwire.CodeBadParams, "step %q: %s", name, afOwnNodeRefusal)
		}
	}
	return nil
}
