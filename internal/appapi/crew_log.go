package appapi

import (
	"github.com/Agent-Field/codeaf/internal/teams"
)

// The registry lock serializes these launch-local cursors with the native
// sampler. The first sample establishes a baseline; history is read through
// GET, while later member, manager and terminal writes reach the event stream.
type crewLogState struct{ after map[string]string }

func (s *Server) sampleCrewLogs(home string, f *teams.File) error {
	if s.crewLogs.after == nil {
		s.crewLogs.after = map[string]string{}
	}
	for _, t := range f.Teams {
		if t.Manager == "" || t.Root {
			continue
		}
		after, known := s.crewLogs.after[t.ID]
		limit := 128
		if !known {
			limit = 1
		}
		entries, err := teams.ReadTraffic(home, t.ID, after, limit)
		if err != nil {
			return err
		}
		if !known {
			s.crewLogs.after[t.ID] = "000000000000"
			if len(entries) > 0 {
				s.crewLogs.after[t.ID] = entries[len(entries)-1].ID
			}
			continue
		}
		for _, entry := range entries {
			if entry.ID <= after {
				continue
			}
			_, _ = s.Events().Publish(EventCrewLog, CrewLogEvent{Crew: t.ID, From: entry.From, To: entry.To, Text: entry.Text})
			s.crewLogs.after[t.ID] = entry.ID
		}
	}
	return nil
}
