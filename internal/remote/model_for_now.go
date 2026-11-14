package remote

import (
	"errors"
	"fmt"
)

const MethodUseModelForNow = "UseModelForNow"

const modelForNowUnsupported = "run-only model selection is unsupported by this engine"

// ConfiguredModel reads the saved selection, rather than this run's replacement.
func (a *Agent) ConfiguredModel() string {
	if selected := a.ModelIdentity().Selected; selected != "" {
		return selected
	}
	return a.Model()
}

// UseModelForNow is acknowledged before the surface sends its next request.
// Older engines retain their existing SetModel behavior.
func (a *Agent) UseModelForNow(model string) error {
	_, err := a.c.call(nil, MethodUseModelForNow, model)
	if err != nil && (err.Error() == fmt.Sprintf("engine: no such method %q", MethodUseModelForNow) || err.Error() == modelForNowUnsupported) {
		a.SetModel(model)
		return nil
	}
	return err
}

func (s *server) modelForNowCall(agent WrappedAgent, call Frame) ([]byte, bool, error) {
	if call.Method != MethodUseModelForNow {
		return nil, false, nil
	}
	model, err := arg[string](call)
	if err != nil {
		return nil, true, err
	}
	door, ok := agent.(interface{ UseModelForNow(string) error })
	if !ok {
		return nil, true, errors.New(modelForNowUnsupported)
	}
	if refresh := s.session.engine.RefreshModelSources; refresh != nil {
		refresh()
	}
	if err := door.UseModelForNow(model); err != nil {
		return nil, true, err
	}
	s.session.announce()
	return nil, true, nil
}
