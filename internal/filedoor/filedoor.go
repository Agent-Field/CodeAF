// Package filedoor is the loopback door: a 127.0.0.1 HTTP listener, owned by
// one chat surface, that turns files on the FAR machine into things this
// machine's own programs can open — a browser tab, cmd+click on an OSC 8 link,
// the platform opener.
//
// ── CAPABILITIES, NOT PATHS ─────────────────────────────────────────────────
//
// A file URL is /f/<id>, where the id is minted here and maps to a path in a
// table nobody else can write. The door cannot be asked for an arbitrary path:
// a local process that guesses URLs can reach only what the surface itself
// chose to link, and each id dies with the door. The browse page's token is
// the same idea for the listing side: /browse/<token> and /api/... require the
// one token minted at Open, so another local user's curl gets 404, never a
// listing.
//
// ── THE DOOR SERVES, THE SOURCE DECIDES ─────────────────────────────────────
//
// Every byte and every listing comes through [Source], which is the engine's
// law speaking (internal/remote's handOver two-roots rule). The door adds no
// judgement of its own about what may be shown: a path the source refuses is a
// sentence passed through verbatim, exactly as the surface passes the engine's
// refusals through today.
//
// ── UPLOADS LAND IN attachments/ AND NOWHERE ELSE ───────────────────────────
//
// Dragging a file onto the browse page sends it up the same lane /attach uses,
// and it lands where an attachment lands: the far session's attachments/
// folder. The door never writes an arbitrary remote path — moving a file INTO
// the workspace proper is the conversation's job ("put attachments/x.csv next
// to the others"), because that is a write on somebody's machine and writes
// belong to the lane that already owns consent for them.
package filedoor

import (
	"errors"
	"io"
	"time"
)

// Entry is one row of a remote listing, the door's own shape so the package
// depends on internal/remote only through [Source]'s implementor.
type Entry struct {
	Name    string
	Dir     bool
	Size    int64
	ModTime time.Time
	MIME    string
}

// File is one fetched file: its bytes and what to call them on the wire out.
type File struct {
	Name  string
	MIME  string
	Bytes []byte
}

// Source is where every listing and every byte comes from. The chat surface
// implements it over its remote client; a test implements it over a map.
type Source interface {
	// Host is the far machine's name as the person typed it — the browse
	// page's title, so a person with three doors open knows whose disk this is.
	Host() string
	// List returns one directory under the source's own law.
	List(path string) (resolved string, entries []Entry, truncated bool, err error)
	// Fetch returns one file under the same law.
	Fetch(path string) (File, error)
	// Deposit sends bytes to the far session's attachments folder and returns
	// the path they landed at, on the engine's disk.
	Deposit(name string, data []byte) (landed string, err error)
}

// Door is one running listener. Zero value is not usable; Open makes one.
type Door struct {
	source Source
	// built by Open in lane B: the listener, the token, the id table, the mux.
}

// ErrNotBuilt marks the seam lane B fills. Nothing ships while it exists.
var ErrNotBuilt = errors.New("filedoor: not built yet")

// Open starts the door on an OS-chosen 127.0.0.1 port and mints its token.
func Open(source Source) (*Door, error) {
	if source == nil {
		return nil, errors.New("filedoor: a door needs a source")
	}
	return nil, ErrNotBuilt
}

// FileURL mints (or reuses) the capability URL for one remote path, for OSC 8
// links and for the open flow. Idempotent per path for the door's lifetime.
func (d *Door) FileURL(path string) (string, error) { return "", ErrNotBuilt }

// BrowseURL is the browse page's own address, token included.
func (d *Door) BrowseURL() string { return "" }

// Close stops the listener and forgets every id and the token.
func (d *Door) Close() error { return nil }

// discard keeps vet quiet about the seam until lane B replaces this file's
// bodies with the real ones.
var _ = io.Discard
