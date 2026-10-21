package cellstore

import (
	"context"
	"errors"
	"net"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// startAndDial cannot start a Unix engine on Windows. Startup leaves the whole
// capability absent; this guard also protects direct transport callers.
func startAndDial(context.Context, Daemon) (net.Conn, error) {
	return nil, errors.New(cell.WindowsReason)
}

// StopSpawned has nothing to reap on a platform that never launches furrow.
func StopSpawned() {}
