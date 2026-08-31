package tui3

// ── WHAT A CONVERSATION IS ABOUT, ON HOME ───────────────────────────────────
//
// A conversation is filed under the project it is STANDING IN — the folder the
// window was opened in, which is what home's buckets are and always were. Since
// the places wave it can also be ABOUT somewhere else: a folder somebody named
// with `/folder` or `/attach`, or one the ground ladder resolved and the
// conversation wrote down (internal/session/places.go). That fact belongs on
// home, because home is where a person goes to find the conversation they had
// about a thing rather than the conversation they had in a directory.
//
// THE WORD HERE IS `folder` AND NEVER `place`. This surface already spends
// "place" on the seven rooms of the tab bar (homeplaces.go, pages.go), and a
// second meaning on the same screen would be one word for two things a person
// can point at. The session layer's own type keeps its name; everything this
// file draws or is called is a folder.
//
// TWO RULES, AND BOTH ARE THE EMPTINESS LAW:
//
//   - THE STANDING FOLDER IS NOT NEWS. The project heading over the row already
//     says where the conversation lives, so the folder it is standing in earns
//     nothing on the row under it.
//   - AND NEITHER IS A FOLDER THAT WOULD REPEAT THE PROJECT'S NAME. `wisp ·
//     also about wisp` is one fact said twice, in the width the name needed.

import (
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// homeAlsoWord leads the row's clause and captions the card's band, so the two
// are one sentence in two places rather than two spellings of one idea.
const homeAlsoWord = "also about"

// homeFolders are the folders one conversation refers to BEYOND the one it is
// standing in, newest first, exactly as the conversation accrued them.
//
// The standing folder is dropped by path and not by name: it arrives on the row
// twice — as the workspace the conversation was opened in and as the project
// directory home filed it under — and either spelling of it is the place the
// heading has already named.
func homeFolders(row session.SessionRow) []session.PlaceRef {
	if len(row.Places) == 0 {
		return nil
	}
	standing := map[string]bool{}
	for _, dir := range []string{row.Workspace, row.ProjectDir} {
		if dir = strings.TrimSpace(dir); dir != "" {
			standing[filepath.Clean(dir)] = true
		}
	}
	var out []session.PlaceRef
	seen := map[string]bool{}
	for _, place := range row.Places {
		path := filepath.Clean(strings.TrimSpace(place.Path))
		if path == "" || path == "." || standing[path] || seen[path] {
			continue
		}
		seen[path] = true
		place.Path = path
		out = append(out, place)
	}
	return out
}

// homeFolderNames are those folders as a person would name them — the basename,
// which is what somebody calls a project out loud and what they type into the
// box looking for it.
//
// A NAME THAT IS THE PROJECT'S OWN NAME IS DROPPED, for the reason at the head
// of this file. The path is kept in the set [homeFolders] answers, so the card
// still names a genuinely different folder that happens to share a basename;
// what is refused is only saying one word twice on one row.
func homeFolderNames(row session.SessionRow) []string {
	project := strings.ToLower(strings.TrimSpace(row.Project))
	var out []string
	for _, place := range homeFolders(row) {
		name := filepath.Base(place.Path)
		if strings.ToLower(name) == project {
			continue
		}
		out = append(out, name)
	}
	return out
}

// homeAlsoAbout is the row's own clause — `also about wisp`, and `also about
// wisp +2` when there are more — and "" for the conversation that is about
// exactly where it is standing, which is every conversation until one is not.
//
// IT NAMES ONE AND COUNTS THE REST rather than listing them, because a row is a
// fixed shape: a clause that grew with the data would push the age off the end
// of the line on the conversation that had the most to say for itself. The
// whole list is on the card beside it (homeband_folders.go).
func homeAlsoAbout(row session.SessionRow) string {
	names := homeFolderNames(row)
	if len(names) == 0 {
		return ""
	}
	clause := homeAlsoWord + " " + names[0]
	if rest := len(names) - 1; rest > 0 {
		clause += " +" + itoa(rest)
	}
	return clause
}
