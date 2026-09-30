package directorytest

import (
	"context"
	"net/http"
	"testing"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// WatchRig is one fresh relay as the watch suite sees it: a Rig whose devices
// all belong to one identity, plus what the suite needs to open sockets, to
// act as a stranger, and to know how many sockets the relay lets one identity
// hold.
type WatchRig struct {
	Rig
	// Base is the relay's base URL, without a trailing slash.
	Base string
	// Sign stamps requests as the named device of the rig's one identity.
	Sign func(device string) wireauth.Sign
	// Stranger stamps requests as a device of a different, fresh identity.
	Stranger func() wireauth.Sign
	// Cap is the watcher cap the relay was built with; zero means the relay
	// keeps the default directory.MaxWatchers.
	Cap int
}

// WatchRigFactory builds a fresh WatchRig for one test.
type WatchRigFactory func(t *testing.T) WatchRig

// RunWatch runs every directory watch case of the contract (section 21.9)
// against a fresh rig each. Cases run in parallel because several wait on
// real sockets, and every case makes its own identity so none can hear another.
func RunWatch(t *testing.T, factory WatchRigFactory) {
	for name, fn := range watchCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fn(newWatchEnv(t, factory(t)))
		})
	}
}

// DialWatch opens the watch socket the way every client must: a GET of the
// watch route signed over an empty body like any directory request, dialled
// with the signed headers as the handshake. On a refusal before the upgrade
// the error is non-nil and the response holds the status and the body.
func DialWatch(ctx context.Context, base string, sign wireauth.Sign) (*websocket.Conn, *http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+directory.WatchPath, nil)
	if err != nil {
		return nil, nil, err
	}
	sign(req, nil)
	return websocket.Dial(ctx, req.URL.String(), &websocket.DialOptions{HTTPHeader: req.Header})
}

var unsigned wireauth.Sign = func(*http.Request, []byte) {}
