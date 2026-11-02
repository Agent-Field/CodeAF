package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/trace"
)

type remoteUsage struct {
	Task, Computer, Event string
	Seat                  Seat
}
type remoteLedgerCursor struct {
	offset int64
	seen   map[string]bool
}

var remoteUsageMemo = struct {
	sync.Mutex
	ledgers map[string]*remoteLedgerCursor
}{ledgers: map[string]*remoteLedgerCursor{}}

// Mirrored calls are paid elsewhere. Refreshing the appended tail under a
// process-shared lock prevents a second chat owner from repeating a paid call.
func appendRemoteUsage(path string, line UsageLine) error {
	remoteUsageMemo.Lock()
	defer remoteUsageMemo.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(path+".remote-lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, false); err != nil {
		return err
	}
	defer filelock.Unlock(lock)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	memo := remoteUsageMemo.ledgers[path]
	if memo == nil || info.Size() < memo.offset {
		memo = &remoteLedgerCursor{seen: map[string]bool{}}
		remoteUsageMemo.ledgers[path] = memo
	}
	newline, err := memo.readTail(f)
	if err != nil {
		return err
	}
	if memo.seen[line.RemoteEvent] {
		return nil
	}
	raw, err := json.Marshal(line)
	if err != nil {
		return err
	}
	if newline {
		raw = append([]byte{'\n'}, raw...)
	}
	if _, err = f.Write(append(raw, '\n')); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	memo.seen[line.RemoteEvent] = true
	// Leave offset at the last read boundary: another process or the ordinary
	// asynchronous ledger writer may have appended while this call was written.
	return nil
}

// readTail recognizes complete EOF objects without turning a replay into another charge.
// A partial object is rolled back to the last complete boundary before any new write.
func (memo *remoteLedgerCursor) readTail(f *os.File) (bool, error) {
	if _, err := f.Seek(memo.offset, io.SeekStart); err != nil {
		return false, err
	}
	reader := bufio.NewReader(f)
	for {
		raw, err := reader.ReadBytes('\n')
		if err != nil && err != io.EOF {
			return false, err
		}
		if len(raw) == 0 {
			return false, nil
		}
		var old UsageLine
		readable := json.Unmarshal(raw, &old) == nil
		if readable && old.RemoteEvent != "" {
			memo.seen[old.RemoteEvent] = true
		}
		if err == io.EOF {
			if readable {
				return true, nil
			}
			return false, f.Truncate(memo.offset)
		}
		memo.offset += int64(len(raw))
	}
}

// The transcript records the same event identity before changing live totals.
// A crash on either side of that write replays exactly one charge on reopen.
func (a *Agent) foldRemoteCharge(event string, charge RunCharge) error {
	if charge.TokensIn == 0 && charge.TokensOut == 0 && charge.USD == 0 {
		return nil
	}
	used := Usage{Input: charge.TokensIn, Output: charge.TokensOut, CacheRead: charge.Cached, Calls: 1, CostUSD: charge.USD}
	if a.file != nil {
		added, err := a.file.appendRemoteCharge(event, charge.Model, used)
		if err != nil {
			return err
		}
		if !added {
			return nil
		}
	}
	a.bank(bankedCall{used: used, model: charge.Model, late: true})
	return nil
}
func (s *sessionFile) appendRemoteCharge(event, model string, used Usage) (bool, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false, os.ErrClosed
	}
	if s.remoteSeen == nil {
		s.remoteSeen = map[string]bool{}
	}
	if s.remoteSeen[event] {
		s.mu.Unlock()
		return false, nil
	}
	raw, err := json.Marshal(sessionEntry{Type: "usage", Timestamp: stamp(), Usage: &journalUsage{RemoteEvent: event, Model: model, Input: used.Input, Output: used.Output, CacheRead: used.CacheRead, CostUSD: used.CostUSD, Calls: used.Calls, Aux: true}})
	if err == nil {
		_, err = s.file.Write(append(trace.ScrubRegistered(raw), '\n'))
	}
	if err == nil {
		err = s.file.Sync()
	}
	if err != nil {
		s.mu.Unlock()
		return false, errors.New("the task's remote spending could not be saved: " + err.Error())
	}
	s.remoteSeen[event] = true
	s.mu.Unlock()
	if s.onWrite != nil {
		s.onWrite()
	}
	return true, nil
}
