package chatlist

import (
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// CrewKindFactory is the kind of a crew the factory made: a factory item's
// room, whose members are the item's stage chats.
const CrewKindFactory = "factory"

// Crew is the crew a chat belongs to on the computer that holds it, as that
// computer sealed it into the chat's directory record.
//
// WHY THE RECORD AND NOT THE VAULT'S TEAM SYNC. A crew made on another
// computer has to show up here as one crew even while that computer is off,
// and must stay READ-ONLY here in this version. The directory record already
// carries the chat's sealed title and folder to every paired computer, keeps
// them while the holder is offline, and is rewritten with each saved turn, so
// the crew rides beside them (docs/design/remote-read/DESIGN.md, "Crews on
// another computer"). The vault's team sync (syncsetup/teams.go) is the other
// carrier, but it merges a crew INTO this computer's teams store as a crew
// this computer can edit and drive, which is exactly what a viewer must not
// do with a crew another computer is running; and the app engine only runs
// that round while pairing is still pending. Do not swap this for it without
// first deciding who drives a crew whose members live on two computers.
//
// These bytes are persisted in records and read by other builds: fields are
// only ever added. V is 1; an empty ID is "in no crew", which is sealed on
// purpose because an empty record field keeps the old value (clusterstate's
// keepIfEmpty), and a chat that left its crew must say so.
type Crew struct {
	V       int    `json:"v"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Manager bool   `json:"manager,omitempty"`
	Kind    string `json:"kind,omitempty"`
}

// maxCrewName bounds the sealed name; a crew name is a few words.
const maxCrewName = directory.MaxTitleBytes

// SealCrew seals c for a chat's directory record.
func SealCrew(key []byte, c Crew) (string, error) {
	c.V = 1
	if len(c.Name) > maxCrewName {
		c.Name = cutName(c.Name, maxCrewName)
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return directory.SealCrew(key, string(raw))
}

// OpenCrew reads a record's sealed crew. Anything that does not open, or
// names no crew, is no crew: a row is never put in a crew it cannot name.
func OpenCrew(key []byte, sealed string) Crew {
	if sealed == "" {
		return Crew{}
	}
	plain, err := directory.OpenCrew(key, sealed)
	if err != nil {
		return Crew{}
	}
	var c Crew
	if json.Unmarshal([]byte(plain), &c) != nil || c.ID == "" {
		return Crew{}
	}
	return c
}

// WithCrews fills each row's Crew from the listing it was built from.
func WithCrews(rows []Row, l directory.Listing, key []byte) {
	for i := range rows {
		rows[i].Crew = OpenCrew(key, l.Cells[rows[i].Cell].Crew)
	}
}

func cutName(s string, max int) string {
	for max > 0 && max < len(s) && s[max]&0xC0 == 0x80 {
		max--
	}
	return s[:max]
}
