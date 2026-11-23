package session

import (
	"errors"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// ConversationDeleteChoices is the terminal's manager guard: only optional global leadership can be cleared here.
func ConversationDeleteChoices(f *teams.File, file string) (map[string]string, error) {
	managed := f.ManagedTeams(file)
	if len(managed) == 1 && managed[0].Root {
		return map[string]string{managed[0].ID: ""}, nil
	}
	if len(managed) > 0 {
		return nil, errors.New(f.ManagerRemovalMessage(file))
	}
	return nil, nil
}
