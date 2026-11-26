package schema

const (
	SourceMessageReceived   = "message.received"
	SourceMeetingStarting   = "event.starts"
	SourceTranscriptCreated = "transcript.created"
	MaxMeetingLeadMinutes   = 1440
	GraphSourceConnector    = "microsoft-365"
)

// GraphSourceTrigger recognizes the declared delegated Graph events.
func GraphSourceTrigger(connector, event string) bool {
	if connector != GraphSourceConnector {
		return false
	}
	switch event {
	case SourceMessageReceived, SourceMeetingStarting, SourceTranscriptCreated:
		return true
	}
	return false
}

func (c *checker) checkGraphSourceTrigger(t Trigger, at string) {
	if !contains(c.app.Capabilities.Connectors[t.Connector].Can, t.Event) {
		c.add(AppFile, at+"/event", CodeToolNotAllowed, "Grant %s.%s before using it as a trigger.", t.Connector, t.Event)
	}
	for _, name := range []string{"connection", "read"} {
		if !fileIDPattern.MatchString(asString(t.Match[name])) {
			c.add(AppFile, at+"/match/"+name, CodeTriggerShape, "The trigger needs a declared %s name.", name)
		}
	}
	if t.When != "" || t.Type != "" || t.Cron != "" {
		c.add(AppFile, at, CodeTriggerShape, "Put the source trigger's filter in match.")
	}
	for name, value := range t.Match {
		switch name {
		case "connection", "read":
			continue
		case "query":
			if v, ok := value.(string); ok && len(v) > 0 && len(v) <= 256 && t.Event == SourceMessageReceived {
				continue
			}
		case "minutes":
			if n, ok := value.(float64); ok && n >= 1 && n <= MaxMeetingLeadMinutes && n == float64(int(n)) && t.Event == SourceMeetingStarting {
				continue
			}
		}
		c.add(AppFile, at+"/match/"+name, CodeTriggerShape, "Use an Outlook subject or sender filter, or a whole number of minutes from 1 to 1440 for Calendar.")
	}
	if t.Event == SourceMeetingStarting && t.Match["minutes"] == nil {
		c.add(AppFile, at+"/match/minutes", CodeTriggerShape, "Set how many minutes before an event the run should start.")
	}
}
