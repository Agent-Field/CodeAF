package directory_test

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
)

func TestMemoryConforms(t *testing.T) {
	directorytest.Run(t, func(_ *testing.T, clock *directorytest.FakeClock) directorytest.Devices {
		return directory.NewMemory(clock.Now).For
	})
}
