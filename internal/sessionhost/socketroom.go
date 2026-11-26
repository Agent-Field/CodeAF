package sessionhost

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/enginehost"
)

// ── ROOM FOR THE ENGINE'S SOCKET ───────────────────────────────────────────
//
// A chat's engine keeps working after its person disconnects only because it
// runs as a host behind a unix socket in the chat's own home
// (<data>/people/<person>/<chat>/home/v3/hosts/<hash>/host.sock). A unix socket
// path has a hard ceiling ([enginehost.SocketLimit]), and an engine whose home is
// too deep does not fail: it quietly runs inside the connection's own process
// instead, so closing the client ends the turn. THAT IS EXACTLY HOW THE FIRST
// DEPLOYMENT BEHAVED, with the data folder at /sessions, one byte over the
// ceiling, and nothing said anywhere. So the supervisor refuses to start with a
// folder that leaves no room, and says how much room there is.

// personDirWidth and sessionIDWidth are the widths of the two names layoutFor
// puts between the data folder and a chat's home. A test pins them to the real
// layout so the arithmetic here cannot drift from it.
const (
	personDirWidth = len("p_") + 16
	sessionIDWidth = len("hs_") + 24
)

// longestChatSocket is the engine-host socket of a chat under data, with names
// of the widest kind a chat can have.
func longestChatSocket(data string) string {
	dir := filepath.Join(data, "people", strings.Repeat("p", personDirWidth), strings.Repeat("s", sessionIDWidth))
	return enginehost.SocketPathUnder(filepath.Join(dir, "home"))
}

// checkSocketRoom is nil when every chat's engine host can listen under data.
func checkSocketRoom(data string) error {
	path := longestChatSocket(data)
	if enginehost.SocketPathFits(path) {
		return nil
	}
	room := len(data) - (len(path) - enginehost.SocketLimit)
	return fmt.Errorf("CODEAF_SESSIONHOST_DATA is too long: a chat's engine could not keep working after its person disconnects. Use a folder of at most %d characters, such as /chats", room)
}
