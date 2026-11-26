//go:build linux

package sessionhost

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/coder/websocket"
)

const (
	wireReadLimit = 4 << 20
	childGrace    = 2 * time.Second
)

type wireAttach struct {
	ctx    context.Context
	cancel context.CancelFunc
	child  *engineChild
}

// wireResponse keeps the transport's handshake errors in the same plain
// JSON format as the admission errors.
type wireResponse struct {
	*requestWriter
	rejected bool
}

func (w *wireResponse) WriteHeader(status int) {
	if status == http.StatusSwitchingProtocols {
		w.requestWriter.WriteHeader(status)
		return
	}
	w.rejected = true
	respondError(w.requestWriter, status, badWire)
}

func (w *wireResponse) Write(p []byte) (int, error) {
	if w.rejected {
		return len(p), nil
	}
	return w.requestWriter.Write(p)
}

func (s *supervisor) wire(w *requestWriter, r *http.Request, person, id string, logRequest func()) {
	attach := s.reserveWire(w, person, id)
	if attach == nil {
		return
	}
	defer s.finishWire(id, attach)
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Second))
	conn, err := websocket.Accept(&wireResponse{requestWriter: w}, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
		CompressionMode:    websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	logRequest()
	child := s.startWire(id, attach)
	if child == nil {
		_ = conn.Close(websocket.StatusInternalError, refusalText[unavailable])
		return
	}

	bridge(attach.ctx, conn, child)
	if child.waitErr != nil {
		// A child can write any text, including credentials. Only a known
		// diagnostic and the size of the captured tail may reach server logs.
		diagnostic := ""
		if strings.TrimSpace(child.stderr.text()) == boundingError {
			diagnostic = boundingError
		}
		status := -1
		var exit *exec.ExitError
		if errors.As(child.waitErr, &exit) {
			status = exit.ExitCode()
		}
		s.log.Warn("chat connection process exited", "id", id, "status", status,
			"stderr_bytes", child.stderr.size, "diagnostic", diagnostic)
	}
}

func (s *supervisor) reserveWire(w http.ResponseWriter, person, id string) *wireAttach {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, exists := s.registry.sessions[id]
	switch {
	case !exists || row.Person != person:
		respondError(w, http.StatusNotFound, noChat)
	case !s.report.OK:
		respondError(w, http.StatusServiceUnavailable, cannotIsolate)
	case s.closing || row.Doomed:
		respondError(w, http.StatusServiceUnavailable, unavailable)
	case !s.creds[id].valid():
		respondError(w, http.StatusConflict, noModel)
	default:
		if s.checkSlot(w, row) && s.prepareWireLayout(w, row) {
			ctx, cancel := context.WithCancel(context.Background())
			attach := &wireAttach{ctx: ctx, cancel: cancel}
			if s.wires[id] == nil {
				s.wires[id] = make(map[*wireAttach]struct{})
			}
			s.wires[id][attach] = struct{}{}
			s.wireWG.Add(1)
			return attach
		}
	}
	return nil
}

func (s *supervisor) checkSlot(w http.ResponseWriter, row sessionRow) bool {
	live, err := s.processes.snapshot()
	if err != nil {
		respondError(w, http.StatusInternalServerError, readFailed)
		return false
	}
	for wire := range s.wires[row.ID] {
		if wire.ctx.Err() != nil {
			respondError(w, http.StatusServiceUnavailable, unavailable)
			return false
		}
	}
	if len(live[row.UID]) > 0 || len(s.wires[row.ID]) > 0 {
		return true
	}
	total, person := s.runningCount(live, row.Person)
	if total >= s.cfg.MaxRunning {
		respondError(w, http.StatusTooManyRequests, hostFull, total, s.cfg.MaxRunning)
		return false
	}
	if person >= s.cfg.MaxPerPerson {
		respondError(w, http.StatusTooManyRequests, personFull, person)
		return false
	}
	return true
}

func (s *supervisor) runningCount(live processTable, person string) (total, owned int) {
	for id, row := range s.registry.sessions {
		if len(live[row.UID]) == 0 && len(s.wires[id]) == 0 {
			continue
		}
		total++
		if row.Person == person {
			owned++
		}
	}
	return total, owned
}

func (s *supervisor) prepareWireLayout(w http.ResponseWriter, row sessionRow) bool {
	_, err := s.folders(row)
	if err == nil {
		if !row.Missing {
			return true
		}
		row.Missing = false
		if s.registry.put(row) == nil {
			return true
		}
	}
	if errors.Is(err, os.ErrNotExist) {
		made, err := s.buildLayout(row)
		if err == nil {
			row.Missing = false
			err = s.registry.put(row)
		}
		if err == nil {
			return true
		}
		if made {
			s.rollbackLayout(row)
		}
	}
	respondError(w, http.StatusInternalServerError, unavailable)
	return false
}

func (s *supervisor) startWire(id string, attach *wireAttach) *engineChild {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, reserved := s.wires[id][attach]; !reserved || s.closing || attach.ctx.Err() != nil {
		return nil
	}
	row := s.registry.sessions[id]
	folders, err := s.folders(row)
	if err != nil {
		return nil
	}
	row.LastActive = s.now().UTC()
	if s.registry.put(row) != nil {
		return nil
	}
	attach.child, err = s.launcher.start(row.UID, folders, engineEnv(s.cfg, folders, s.creds[id]))
	if err != nil {
		s.log.Warn("could not start the chat connection process", "id", id)
	}
	return attach.child
}

func (s *supervisor) finishWire(id string, attach *wireAttach) {
	defer s.wireWG.Done()
	// THE CANCEL COMES AFTER THE REMOVAL. A cancelled wire that is still in the
	// chat's group is how reserveWire tells that the chat is being stopped, so a
	// connection that merely ended must be out of the group before it reads as
	// cancelled. Cancelling first left a window, as long as the wait for the
	// lock, in which a reconnect was refused as "unavailable".
	defer attach.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	group := s.wires[id]
	if _, reserved := group[attach]; !reserved {
		return
	}
	delete(group, attach)
	if len(group) == 0 {
		delete(s.wires, id)
	}
	if attach.child != nil {
		row := s.registry.sessions[id]
		row.LastActive = s.now().UTC()
		if s.registry.put(row) != nil {
			s.log.Warn("could not save recent chat activity", "id", id)
		}
	}
}

func bridge(ctx context.Context, conn *websocket.Conn, child *engineChild) {
	tunnel := websocket.NetConn(ctx, conn, websocket.MessageBinary)
	// NetConn clears the library read limit, so restore it before copying.
	conn.SetReadLimit(wireReadLimit)
	inDone, outDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(inDone)
		_, _ = io.Copy(child.in, tunnel)
	}()
	go func() {
		defer close(outDone)
		_, _ = io.Copy(tunnel, child.out)
	}()
	select {
	case <-inDone:
	case <-ctx.Done():
	case <-outDone:
		_ = conn.Close(websocket.StatusNormalClosure, "")
	case <-child.done:
		// Let already queued output reach the client before the close frame.
		timer := time.NewTimer(childGrace)
		select {
		case <-outDone:
		case <-inDone:
		case <-ctx.Done():
		case <-timer.C:
		}
		timer.Stop()
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}
	_ = conn.CloseNow()
	child.in.Close()
	child.out.Close()
	terminateBridgeChild(child)
}

func terminateBridgeChild(child *engineChild) {
	select {
	case <-child.done:
		return
	default:
	}
	_ = child.signal(syscall.SIGTERM)
	timer := time.NewTimer(childGrace)
	defer timer.Stop()
	select {
	case <-child.done:
	case <-timer.C:
		_ = child.signal(os.Kill)
		<-child.done
	}
}
