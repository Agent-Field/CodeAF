package prompt

// agentBodyLines caps the app agent's job description: long enough for who it
// helps, its jobs, how it answers and what it never does; short enough that a
// small model keeps all of it in view.
const agentBodyLines = 20

// AgentBodyLines renders the agent.md length rule from its constant.
func (view) AgentBodyLines() int { return agentBodyLines }
