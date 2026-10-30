// Package teams is the one store for teams: named sets of conversations, the
// database they live in, each member's short handle, a team's manager and the
// Traffic log a team's members and manager write to. The conversations view
// (internal/tui3) and the team tools a model calls (internal/session) both read
// and write through it, so the two never keep two accounts of one team.
//
// THE MODEL. A team has a random id minted once, a name the person gave it, at
// most one parent team, an ordered list of members and at most one manager. A
// member is a conversation, known by its conversation key, with enough beside
// the key (its file, workspace and title) to open it again. A conversation may
// be in any number of teams. Everything that names a team names it by id,
// never by its place in the list or by its name.
//
// THE LAWS.
//
//   - The store is <profile>/teams.db, an SQLite database (db.go), resolved
//     with [config.ProfilePath], so an empty profile directory is the
//     ordinary launch and means this process's own profile, never "no
//     profile". It is never config.json, and teams.json, the file it
//     replaced, is never read: there is no migration.
//   - A missing store is no teams and no error. A store that is there but
//     unreadable is an error ([ErrUnreadable]), and nothing here writes to
//     it: [SetAside] moves it out of the way when the caller decides to start
//     again, if it is not this store's at all. A newer build's store is
//     [ErrNewer], and is neither written nor moved.
//   - Every field a later build wrote survives a load and a save.
//   - A team is a row and each membership is a row; a write changes only the
//     rows that differ. A member's briefs are rows of their own that a load
//     leaves out ([WithBriefs]).
//   - A chain of parents never loops, and a parent always exists.
//   - A handle is lowercase, 2 to 12 characters, unique within its team,
//     derived from the member's title once and never changed automatically.
//   - The manager is always a member. Removing it from the team clears it.
//   - Every write is a read-modify-write under an exclusive file lock
//     ([Update]) and one SQLite transaction, so two processes writing the same
//     store never lose each other's change. Reading takes no lock and never
//     waits on a writer.
//   - The Traffic log is append-only JSON lines, one file per team, rotated
//     once past a few megabytes.
//
// WHAT THIS PACKAGE DOES NOT KNOW. It imports neither the interface nor the
// session. A team's colour is kept here as data (a hue angle and a tier) with
// the pure arithmetic that spaces colours apart; which hues a palette reserves
// for meaning is the caller's to say. The live state of a member (running,
// asking, finished) is the caller's too, handed to [Digest] as [MemberState].
package teams
