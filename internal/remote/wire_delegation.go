package remote

import (
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── DELEGATION ACROSS THE WIRE ──────────────────────────────────────────────
//
// The delegation store (internal/teams' decision.go, spend.go, lifecycle.go
// and the four `teams.` defaults) lives in the engine's profile for
// wire_teams.go's reason: the far session's team tools read and write it
// there. So the teams page over --host asks for it through these doors,
// answered from [Engine.ProfileDir] and the engine machine's usage ledger,
// and never from this laptop.
//
// THEY ARE SHAPED FOR A CLOCK, like the teams doors. The two reads a page
// repeats, the open packets and a team's spend, carry the stamp the window
// last got and are answered `{"stamp":"…","same":true}` when nothing moved,
// which the engine learns from stats (a directory listing and a stat per team
// for the packets, two stats for the spend). The writes are single calls: a
// packet write is an append under the engine's lock, validated there, and
// needs no compare-and-swap because an append cannot undo another writer.
//
// THEY ARE ADDITIVE, and [Welcome.Delegation] says an engine has them. A
// window facing an engine without it leaves the delegation seam doors nil and
// says the teams page's inbox and spend are not available over that
// connection; it never reads the laptop's packet files, which the far
// manager never sees. Closing and reopening a team are edits to the teams
// file and cross by [MethodTeamsUpdate] like any other; only deleting, which
// removes the team's Traffic and packet files too, needs its own door.
const (
	// MethodTeamsDefaults is the engine profile's five `teams.` defaults.
	MethodTeamsDefaults = "Teams.Defaults" // struct{} → teamstore.Defaults
	// MethodTeamsApplyDefault writes one of those rows, the same way the
	// settings tab writes it locally (config's ApplyTeamDefault). The answer
	// is the five defaults after the write. [Welcome.TeamSettings] says an
	// engine has the door; an engine without it leaves the tab read-only.
	MethodTeamsApplyDefault = "Teams.ApplyDefault" // TeamDefaultArgs → teamstore.Defaults
	// MethodTeamsPackets is the packets waiting on a scope, or word that the
	// packet files have not moved since the stamp the window holds.
	MethodTeamsPackets = "Teams.Packets" // PacketsArgs → PacketsReading
	// MethodTeamsRaise records a new packet and answers it as written.
	MethodTeamsRaise = "Teams.Raise" // teamstore.Packet → teamstore.Packet
	// MethodTeamsDecide records a decision on a packet.
	MethodTeamsDecide = "Teams.Decide" // DecideArgs → teamstore.Packet
	// MethodTeamsEscalate sends a packet up.
	MethodTeamsEscalate = "Teams.Escalate" // EscalateArgs → teamstore.Packet
	// MethodTeamsSpend is one team's spend on a day, or word that neither the
	// teams file nor the ledger moved since the stamp the window holds.
	MethodTeamsSpend = "Teams.Spend" // SpendArgs → SpendReading
	// MethodTeamsDelete forgets a closed team and its files.
	MethodTeamsDelete = "Teams.Delete" // DeleteTeamArgs → DeleteTeamReply
)

// THE WRAP-UP'S TWO DOORS ([Welcome.WrapUp]). Traffic is the channel between
// the interface and the session, and over --host the window reads it
// ([MethodTeamsTraffic]) but has no door to write it: so the one line the
// person's `Wrap up first` writes crosses by a door of its own, which appends
// exactly [teamstore.WrapUpRequest] to the engine's log and nothing else; and
// accepting a closing report, which closes the team and logs it, crosses by
// [teamstore.AcceptClosing] on the engine. Neither is a general Traffic
// writer: a window cannot put words in a member's mouth through them.
const (
	MethodTeamsWrapUp        = "Teams.WrapUp"        // WrapUpArgs → struct{}
	MethodTeamsAcceptClosing = "Teams.AcceptClosing" // AcceptClosingArgs → AcceptClosingReply
)

// WrapUpArgs is the team to wrap up and the person's words, "" for the
// standard ones.
type WrapUpArgs struct {
	Team string `json:"team"`
	Text string `json:"text,omitempty"`
}

// AcceptClosingArgs names a decided closing packet.
type AcceptClosingArgs struct {
	ID string `json:"id"`
}

// AcceptClosingReply says whether this call closed the team, and the teams
// file's stamp after.
type AcceptClosingReply struct {
	Closed bool   `json:"closed"`
	Stamp  string `json:"stamp"`
}

// TeamDefaultArgs is one `teams.` row, as the settings tab would apply it:
// Key is the registry key, Raw is what was typed (on, off, a number, or blank).
type TeamDefaultArgs struct {
	Key string `json:"key"`
	Raw string `json:"raw"`
}

// PacketsArgs is a scope (a team id, teamstore.Person, or "" for every
// waiting packet) and the stamp the window last got, "" for none.
type PacketsArgs struct {
	Scope string `json:"scope,omitempty"`
	Stamp string `json:"stamp,omitempty"`
}

// PacketsReading is the waiting packets and their stamp; Same says the stamp
// is the one asked about, and then Packets is absent.
type PacketsReading struct {
	Stamp   string             `json:"stamp"`
	Same    bool               `json:"same,omitempty"`
	Packets []teamstore.Packet `json:"packets,omitempty"`
}

// DecideArgs is a decision on one packet.
type DecideArgs struct {
	ID       string `json:"id"`
	By       string `json:"by"`
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
}

// EscalateArgs sends one packet to To: a team above, or teamstore.Person.
type EscalateArgs struct {
	ID     string `json:"id"`
	By     string `json:"by"`
	To     string `json:"to"`
	Reason string `json:"reason,omitempty"`
}

// SpendArgs is one team's day ("" is the engine's today) and the stamp the
// window last got.
type SpendArgs struct {
	Team  string `json:"team"`
	Day   string `json:"day,omitempty"`
	Stamp string `json:"stamp,omitempty"`
}

// SpendReading is the team's spend and its stamp; Same says nothing moved and
// then Spend is absent.
type SpendReading struct {
	Stamp string           `json:"stamp"`
	Same  bool             `json:"same,omitempty"`
	Spend *teamstore.Spend `json:"spend,omitempty"`
}

// DeleteTeamArgs names the closed team to forget.
type DeleteTeamArgs struct {
	Team string `json:"team"`
}

// DeleteTeamReply is every team id forgotten and the teams file's stamp after.
type DeleteTeamReply struct {
	Gone  []string `json:"gone"`
	Stamp string   `json:"stamp"`
}
