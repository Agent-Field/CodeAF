package remote

import (
	"encoding/json"
	"errors"

	"github.com/Agent-Field/codeaf/internal/session"
)

// These additive doors carry explicit landing gestures to the session that
// owns the checkout. Ordinary local chat reaches that session through this wire.
const (
	MethodLandingPreview = "Places.LandingPreview"
	MethodLand           = "Places.Land"
)

type folderLander interface {
	LandingFor(string) (session.FolderLanding, bool)
	Land(string) (session.FolderLanding, error)
}

type landingPreview struct {
	Landing session.FolderLanding `json:"landing"`
	Found   bool                  `json:"found"`
}

func (s *server) landingCall(call Frame) (json.RawMessage, bool, error) {
	folder, err := arg[string](call)
	if err != nil {
		return nil, true, err
	}
	door, ok := s.session.current().(folderLander)
	if !ok {
		return nil, true, errors.New("this conversation cannot land changes")
	}
	if call.Method == MethodLandingPreview {
		landing, found := door.LandingFor(folder)
		payload, err := json.Marshal(landingPreview{Landing: landing, Found: found})
		return payload, true, err
	}
	landing, err := door.Land(folder)
	if err != nil {
		return nil, true, err
	}
	// Every attached window must lose the waiting row when the work lands.
	s.session.announce()
	payload, err := json.Marshal(landing)
	return payload, true, err
}

// UnlandedChanges is a memory read because the composer asks on every frame.
func (a *Agent) UnlandedChanges() []session.StandingChange {
	return a.c.facts.read().Unlanded
}

// LandingFor asks the owning session only when the person requests a preview.
func (a *Agent) LandingFor(folder string) (session.FolderLanding, bool) {
	payload, err := a.c.call(nil, MethodLandingPreview, folder)
	if err != nil {
		return session.FolderLanding{}, false
	}
	var preview landingPreview
	if err := json.Unmarshal(payload, &preview); err != nil {
		return session.FolderLanding{}, false
	}
	return preview.Landing, preview.Found
}

// Land keeps Git and copy operations on the session's machine, including when
// the window and its local session host are separate processes on one machine.
func (a *Agent) Land(folder string) (session.FolderLanding, error) {
	payload, err := a.c.call(nil, MethodLand, folder)
	if err != nil {
		return session.FolderLanding{}, err
	}
	var landing session.FolderLanding
	if err := json.Unmarshal(payload, &landing); err != nil {
		return session.FolderLanding{}, err
	}
	return landing, nil
}
