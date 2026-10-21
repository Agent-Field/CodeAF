package relaybill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// machine is one computer of the identity: its own home, engine data and disk.
type machine struct {
	name  string
	sync  *syncsetup.Sync
	eng   cellstore.Engine
	roots string // where this machine keeps the chats it takes
}

// newMachine makes a machine that holds the identity and nothing else, so a
// chat reaches it only through the relay. The first machine passes id nil and
// makes the identity; every later one adopts it.
func newMachine(dir, name, binary string, id *identity.Identity) (*machine, error) {
	home := filepath.Join(dir, name, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, err
	}
	if err := giveIdentity(home, id); err != nil {
		return nil, err
	}
	s, ok, err := syncsetup.Open(home)
	if err != nil || !ok {
		return nil, fmt.Errorf("relaybill: open sync for %s: %v (on: %v)", name, err, ok)
	}
	eng := cellstore.EngineFor("")
	eng.DataRoot, eng.Binary, eng.Transport = filepath.Join(dir, name, "data"), binary, cellstore.Spawn{Binary: binary}
	return &machine{name: name, sync: s, eng: eng, roots: filepath.Join(dir, name, "roots")}, nil
}

func giveIdentity(home string, id *identity.Identity) error {
	if id == nil {
		_, err := identity.Ensure(home)
		return err
	}
	_, err := identity.Adopt(home, *id, false)
	return err
}

// continuer is this machine's take side, the one entry the screen calls.
func (m *machine) continuer() *syncsetup.Continuer {
	return m.sync.Continuer(m.eng, syncsetup.TakeOptions{DeviceName: m.name, RootFor: func(id string) string {
		return filepath.Join(m.roots, id)
	}})
}

// open holds chat c on this machine the way a window does: the drive side
// publishes each sealed turn, beats the lease, and gives it back on Close.
func (m *machine) open(ctx context.Context, c cell.Cell, work string) (*openChat, error) {
	eng := m.eng
	eng.Workspace = work
	drive, err := m.sync.Drive(ctx, eng, c, syncsetup.DriveOptions{DeviceName: m.name})
	if err != nil {
		return nil, err
	}
	seat, err := cellstore.SeatOver(executor.HostBound, c, work, nil, nil,
		func(cellstore.Engine) cellstore.Store { return drive.Store(eng) })
	if err != nil {
		return nil, err
	}
	return &openChat{drive: drive, seat: executor.Gated(seat, drive.Gate), cell: c, work: work}, nil
}

// workOf is the folder a taken chat works in.
func workOf(c cell.Cell) string { return session.Place{Dir: c.Root, Owned: true}.Work() }

// openChat is one chat open on one machine.
type openChat struct {
	drive *syncsetup.Drive
	seat  executor.Seat
	cell  cell.Cell
	work  string
	calls int
}

// say is one scripted model turn: the transcript grows by a line and a file is
// written, both sealed through the seat as the door seals a tool call.
func (o *openChat) say(ctx context.Context, text string) error {
	o.calls++
	note := filepath.Join(o.work, fmt.Sprintf("turn-%d-%d.txt", time.Now().UnixNano(), o.calls))
	line := fmt.Sprintf(`{"type":"message","role":"assistant","content":%q}`+"\n", text)
	if err := appendTo(session.Place{Dir: o.cell.Root}.Transcript(), line); err != nil {
		return err
	}
	call := executor.Call{Tool: "bash", Args: []byte(`{"command":"echo"}`)}
	return o.seat.Around(ctx, call, func() ([]byte, bool) {
		return nil, os.WriteFile(note, []byte(text), 0o600) != nil
	})
}

// close sends what is unsent and gives the lease back.
func (o *openChat) close(ctx context.Context) error { return o.drive.Close(ctx) }

func appendTo(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(text); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
