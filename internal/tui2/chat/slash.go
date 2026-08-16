package chat

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/tui2/composer"
)

// The slash surface (5.22 rule 3), which is not a surface at all — it is the
// catalog, seen from the composer.
//
// 13.4's C8 recorded "no slash layer anywhere in tui2" and 13.5 restated it as
// the gap. The temptation the doc names by name is to close it with a table:
// "the 17-command slash table as a parallel system" is on 5.22's kill list, and
// v1 has exactly such a table (internal/tui/commands.go). So the whole of this
// file is a projection. It reads registry rows, it asks the SAME entryReason
// the `?` sheet asks, and it hands a completed row to the SAME runEntry the
// palette hands its RunEntry to. There is no branch anywhere below that a slash
// command can take and a palette row cannot.
//
// What that buys, concretely: a row added to internal/registry with a Slash
// alias appears here, in the palette, in the `?` sheet and in the footer's
// rotation on the same commit, with one description and one accelerator; and a
// row this room cannot run is refused here in the same sentence it is refused
// there, because both call one function to ask.

// slashCommands is the catalog as the composer's `/` line needs it.
//
// Only rows with an alias appear. That is not a filter this file invented — an
// entry without a Slash has no way of being TYPED after a slash, so listing it
// would be offering the reader a row they could not have reached by the grammar
// they are using. The palette and the sheet list those rows under their own
// keys, which is where they belong.
//
// The scope is the room's, so a room that cannot do a thing does not offer it.
func (a *App) slashCommands() []composer.Command {
	entries := registry.ForScope(registry.ScopeThread)
	out := make([]composer.Command, 0, len(entries))
	for _, entry := range entries {
		if entry.Slash == "" {
			continue
		}
		// KeyOn, not Key: this is a composer-first surface, so a bare-letter
		// accelerator resolves to nothing here rather than teaching a chord that
		// would land in the draft (registry.Entry.KeyOn). The `?` sheet makes
		// the same projection, which is why the two lists agree.
		out = append(out, composer.Command{
			ID:     entry.ID,
			Word:   entry.Slash,
			Title:  entry.Description,
			Key:    entry.KeyOn(registry.SurfaceComposerFirst),
			Reason: a.entryReason(entry.ID),
		})
	}
	return out
}

// runSlash performs a completed row. It is runEntry and nothing else — the one
// executor, reached by a third hand.
func (a *App) runSlash(id string) tea.Cmd {
	cmd := a.runEntry(id)
	a.shell.Invalidate()
	return cmd
}
