package tui3

// CLEANING THE CACHE, AND ASKING IN WORDS.
//
// /cache reads and /cache clean destroys, and the gap between them is guarded
// the way this surface guards everything irreversible — confirmed once, with
// the safe answer as the default (stop.go states the law over ending work).
// Here the confirmation is TYPED rather than a raised card, and the reason is
// what the card exists for: a card keeps a single keystroke from ending
// something, and there is no single keystroke here. The act is reached only by
// writing "/cache clean now" out in full, after the first form has said the
// size, the path, what goes cold, and what is out of reach — which is /quit's
// own argument ("it is typed out on purpose") with the blast radius stated
// first. A bare "/cache clean" always stops at the question; nothing on this
// road deletes anything the first time it is asked.
//
// WHAT "THE CACHE" IS is said out loud in both answers, because the word is
// one people reasonably stretch over their conversations and their dashboard —
// and the only good moment to correct that is before a deletion. This verb
// reaches ~/.aforge/cache and nothing else; internal/cachedir owns the radius
// for this surface and the CLI both.

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/cachedir"
)

// cacheNoteMsg is one finished cache errand's answer, written as the line to
// show. The size walk and the deletion both run off the loop — the module
// cache is tens of thousands of small files, and a frame frozen behind a
// directory walk is a surface that looks crashed — so every branch below
// returns a command and the note arrives here.
type cacheNoteMsg struct{ line string }

// runCacheCommand consumes /cache and its forms.
func (a *app) runCacheCommand(rest string) tea.Cmd {
	tilde := a.tilde
	switch strings.ToLower(strings.TrimSpace(rest)) {
	case "":
		return func() tea.Msg {
			size := cachedir.Size()
			place := shortPath(cachedir.Root(), tilde, 0)
			if size == 0 {
				return cacheNoteMsg{line: "the cache is empty · " + place}
			}
			return cacheNoteMsg{line: "the cache holds " + cachedir.Human(size) + " · " + place +
				" · toolchain caches tasks fill as they build · /cache clean frees it"}
		}
	case "clean":
		return func() tea.Msg {
			size := cachedir.Size()
			if size == 0 {
				return cacheNoteMsg{line: cacheEmptyWord}
			}
			return cacheNoteMsg{line: "this deletes the shared build cache — " + cachedir.Human(size) +
				" at " + shortPath(cachedir.Root(), tilde, 0) +
				". builds start cold afterwards; conversations and settings are not touched. type /cache clean now to go ahead."}
		}
	case "clean now":
		return func() tea.Msg {
			freed, err := cachedir.Clean()
			switch {
			case err != nil:
				return cacheNoteMsg{line: "cache clean failed: " + err.Error()}
			case freed == 0:
				return cacheNoteMsg{line: cacheEmptyWord}
			}
			return cacheNoteMsg{line: "cache cleaned · " + cachedir.Human(freed) + " freed"}
		}
	default:
		// An argument nothing answers to changes nothing and says the two forms,
		// which is the shape every choice row on this surface refuses in.
		a.note("/cache takes clean, or nothing · /cache shows what it holds")
		return nil
	}
}

// cacheEmptyWord answers both the clean that found nothing and the clean that
// arrived second: one fact, one sentence, wherever it comes up.
const cacheEmptyWord = "the cache is already empty — nothing to delete."
