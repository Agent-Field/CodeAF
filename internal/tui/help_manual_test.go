package tui

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/manual"
)

// The ? overlay is the surface layer and the manual is the deep one, and they
// answer the same question at different depths. This is the help completeness
// test pointed one level down: a command, a slot, or a chord that lands in the
// registries without landing in a manual page leaves aforge with a feature it
// can show you but cannot explain, so the build fails instead.
func TestManualExplainsEverySurfaceTheRegistriesOffer(t *testing.T) {
	for _, command := range slashCommands {
		if !manual.Mentions("/" + command.name) {
			t.Fatalf("no manual page mentions /%s", command.name)
		}
	}
	for _, slot := range modelSlots {
		if !manual.Mentions(slot) {
			t.Fatalf("no manual page mentions the %s model slot", slot)
		}
	}
	for _, chord := range []string{
		keyBindings.graph, keyBindings.thread, keyBindings.board, keyBindings.self,
		keyBindings.voice, keyBindings.boost, keyBindings.newline,
	} {
		if !manual.Mentions(chord) {
			t.Fatalf("no manual page mentions the %s chord", chord)
		}
	}
}
