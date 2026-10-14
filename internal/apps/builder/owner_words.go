package builder

import (
	"errors"
	"log/slog"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

// stoppedWords is what the person reads when a build stops for a reason that
// only makes sense with the files open.
const stoppedWords = "The Builder could not finish this step. Try again."

// spoken marks an error whose text was written for the person: a model that
// could not be reached, a key that is missing.
type spoken struct{ error }

func (e spoken) Unwrap() error { return e.error }

// ownerWords is the one line of a failure the person reads. File names, JSON
// pointers and parser complaints stay in the debug log, where the detail is
// useful to whoever repairs the Builder.
func ownerWords(err error) string {
	var typed *bridge.Error
	if errors.As(err, &typed) {
		return typed.Message
	}
	var said spoken
	if errors.As(err, &said) {
		return said.Error()
	}
	return stoppedWords
}

func shortenedWords(lines int) string {
	if lines == 1 {
		return "Shortened one plan line to fit"
	}
	return "Shortened some plan lines to fit"
}

// detail keeps what the person is spared.
func (s *Service) detail(b *build, msg string, args ...any) {
	slog.Debug("apps builder: "+msg, append([]any{"app", b.app}, args...)...)
}

// failed publishes a failed step in the person's words and logs the cause.
func (s *Service) failed(b *build, step bridge.BuildStep, err error) {
	s.detail(b, "step failed", "step", string(step), "error", err.Error())
	s.step(b, step, bridge.BuildFailed, ownerWords(err))
}
