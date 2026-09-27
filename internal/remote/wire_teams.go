package remote

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── TEAMS ACROSS THE WIRE ───────────────────────────────────────────────────
//
// A team's file and its Traffic logs live in the profile of the machine the
// SESSION runs on, because the team tools a model calls (internal/session)
// read and write them there. Over --host the window is on the laptop and the
// session is on the far machine, so a window that kept teams in its own
// profile would draw a list the manager never reads and a rail that is an
// empty log drawn as the team's. These three doors let the window read and
// write the ENGINE's teams, answered from [Engine.ProfileDir].
//
// THEY ARE SHAPED FOR A CLOCK OVER SSH. The window asks about a managed team
// about once a second while it holds one of its conversations, and never at
// any other time. So every question carries what the window already has (the
// file's stamp, the log's cursor), and an answer about nothing new is a few
// bytes: `{"stamp":"…","same":true}` for the file and `{"stamp":"…"}` for a
// log. The engine stats before it reads ([teamstore.Watch]), so a quiet second
// costs it a stat per file too.
//
// THEY ARE ADDITIVE TO THIS VERSION, and [Welcome.Teams] is how a window knows
// they are there: an engine from before them sends no field, and the window
// turns teams off over that connection with the sentence it has always said
// rather than falling back to the laptop's file (internal/tui3's host.go).
const (
	// MethodTeamsRead is the engine's teams file, or word that it has not
	// moved since the stamp the window holds.
	MethodTeamsRead = "Teams.Read" // TeamsReadArgs → TeamsReading
	// MethodTeamsUpdate writes the whole list back IF the file is still at the
	// stamp the window read it at, and answers Stale when it is not. The window
	// then reads again, makes its change again and retries (cmd/codeaf's
	// hostTeams), so a manager the far session set in between is never undone.
	MethodTeamsUpdate = "Teams.Update" // TeamsUpdateArgs → TeamsReading
	// MethodTeamsTraffic is one team's log after a cursor, at most a page: the
	// Ledger's Since pattern, so every call after the first carries only the
	// lines written since.
	MethodTeamsTraffic = "Teams.Traffic" // TeamsTrafficArgs → TeamsTraffic
)

// TeamsReadArgs is what the window holds of the file. An empty Stamp is a
// window that has not read it yet and is always answered with the file.
// Reserved is the window's palette's reserved hues: a team the file left
// without a colour is coloured around them and the colour written back, as a
// local load does ([teamstore.LoadHued]).
type TeamsReadArgs struct {
	Stamp    string    `json:"stamp,omitempty"`
	Reserved []float64 `json:"reserved,omitempty"`
}

// TeamsReading is the file as the engine holds it. Same says it is still at
// the stamp asked about, and then Teams is absent. Stale is only ever set by
// [MethodTeamsUpdate]: the file moved since the base, nothing was written, and
// Stamp is where it is now.
type TeamsReading struct {
	Stamp string           `json:"stamp"`
	Same  bool             `json:"same,omitempty"`
	Stale bool             `json:"stale,omitempty"`
	Teams []teamstore.Team `json:"teams,omitempty"`
}

// TeamsUpdateArgs is the whole list the window wants written, and the stamp
// of the file it made that list from.
type TeamsUpdateArgs struct {
	Base  string           `json:"base"`
	Teams []teamstore.Team `json:"teams"`
}

// TeamsTrafficArgs is one team's log after a cursor: After "" is the tail,
// the last Limit entries; an id pages forward from it.
type TeamsTrafficArgs struct {
	Team  string `json:"team"`
	After string `json:"after,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

// TeamsTraffic is what came after the cursor, oldest first, and the log's
// stamp when it was read.
type TeamsTraffic struct {
	Entries []teamstore.Entry `json:"entries,omitempty"`
	Stamp   string            `json:"stamp,omitempty"`
}

// ── AND THE TWO ASKS THE WALL MAKES OF THE ENGINE'S MODEL ───────────────────
//
// A team's suggested name and Organize's proposals are one cheap call each on
// the naming role (internal/session's teamname.go and teampropose.go), made by
// the ENGINE because the model, its key and its bill are on the engine's
// machine. Over --host the wall is on the laptop, so without these two doors
// the default road never asked: the card kept the word it opened with and
// Organize showed the folder pass alone, in every terminal and in no test.
//
// THEY CARRY A BUDGET, NOT A DEADLINE, for [RefreshRunSummaryArgs.Budget]'s
// reason: the engine's clock is not this one's. The wall bounds each ask
// (internal/tui3's teamNameWait and organizeWait), and the engine bounds the
// model call by the same span so it stops when the wall stops listening.
//
// [Welcome.TeamAsk] says the engine answers them. An engine from before them
// sends no field, and this end refuses at once rather than spend a round trip
// on a refusal, which is exactly the failure the wall already turns into the
// word it holds and the folder pass alone.
const (
	// MethodTeamsName asks the engine's naming role for one short name for a
	// group of conversations, given their titles.
	MethodTeamsName = "Teams.Name" // TeamNameArgs → string
	// MethodTeamsPropose asks the engine's naming role which teams the
	// conversations offered could form and which existing teams more of them
	// belong in.
	MethodTeamsPropose = "Teams.Propose" // TeamProposeArgs → session.TeamProposal
)

// TeamNameArgs is the titles a group is named from, and how long the wall
// will wait. Zero Budget is a caller with no deadline.
type TeamNameArgs struct {
	Titles []string      `json:"titles"`
	Budget time.Duration `json:"budget,omitempty"`
}

// TeamProposeArgs is everything one Organize ask is about, and how long the
// wall will wait.
type TeamProposeArgs struct {
	In     session.TeamProposalInput `json:"in"`
	Budget time.Duration             `json:"budget,omitempty"`
}
