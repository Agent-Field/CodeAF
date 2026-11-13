package raftsim

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/raftsim/model"
	"github.com/anishathalye/porcupine"
)

// Replay contains only explicit inputs. Generation seeds, flush order and
// message digests can be compared against Evidence after exact reproduction.
type Replay struct {
	Config  Config
	Actions []Action
}
type Evidence struct {
	Replay     Replay
	Trace      []Trace
	FlushTrace []Trace
	History    []model.Operation
	Minimized  []model.Operation
	Check      porcupine.CheckResult
	Error      string
	Digest     string
	Events     int
}

func Run(replay Replay) (*Simulator, error) {
	// Unversioned evidence predates ranked election windows. Preserve those
	// bytes and event digests; new recordings always carry their timing version.
	if replay.Config.TimingVersion == 0 {
		replay.Config.TimingVersion = 1
	}
	s, err := New(replay.Config)
	if err != nil {
		return s, err
	}
	for _, a := range replay.Actions {
		s.Do(a)
	}
	return s, s.Err
}
func (s *Simulator) Evidence() Evidence {
	history := s.History()
	check := model.Check(history)
	e := Evidence{Replay: Replay{Config: s.initial, Actions: s.Actions}, Trace: s.Trace, History: history, Check: check, Digest: s.Digest(), Events: s.Events}
	if s.Err != nil {
		e.Error = s.Err.Error()
	}
	for _, entry := range s.Trace {
		switch entry.Phase {
		case "persist-write", "persist-flush", "persist-ack", "apply-write", "apply-flush", "apply-ack", "reply":
			e.FlushTrace = append(e.FlushTrace, entry)
		}
	}
	if check == porcupine.Illegal {
		e.Minimized = model.Minimize(history)
	}
	return e
}

// WriteEvidence is opt-in host I/O, outside the deterministic scheduler. Only
// synthetic histories belong here; callers choose a disposable output folder.
func (s *Simulator) WriteEvidence(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.Evidence(), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "evidence.json"), append(b, '\n'), 0600)
}
