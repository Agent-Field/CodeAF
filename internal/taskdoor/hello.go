package taskdoor

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
)

const (
	RemovedComputerLine = "this computer was removed from your devices"
	TakingTasksLine     = "takes tasks from your paired computers"
)

func NotTakingTasks(name string) string {
	return name + " is not taking tasks right now — run codeaf serve on it"
}
func CouldNotConfirm(name string) string { return "could not confirm this is " + name }
func DifferentVersion(name string) string {
	return name + " runs a different codeaf that cannot take this task — update both"
}
func Busy(name string) string {
	// The words come from the admission cap, rather than a second limit hidden
	// in a sentence that could become wrong when capacity changes.
	return name + " is already running " + numberWord(MaxRunning) + " tasks"
}
func numberWord(n int) string {
	if n == 2 {
		return "two"
	}
	return strconv.Itoa(n)
}

// rosterCache refuses stale authority after its bounded lifetime. Failed
// refreshes never extend the previous roster, even during an outage.
type rosterCache struct {
	fetch   func(context.Context) (map[string]bool, error)
	clock   Clock
	mu      sync.Mutex
	active  map[string]bool
	fetched time.Time
}

func (r *rosterCache) allows(ctx context.Context, device string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.clock.Now()
	if r.active == nil || now.Before(r.fetched) || now.Sub(r.fetched) >= RosterFor {
		rows, err := r.fetch(ctx)
		if err != nil {
			return false, err
		}
		r.active = make(map[string]bool, len(rows))
		for k, v := range rows {
			r.active[k] = v
		}
		r.fetched = now
	}
	return r.active[device], nil
}
func helloFor(dev identity.Dev, tunnel *pair.Tunnel) Hello {
	return Hello{V: Version, Cert: dev.Cert, Sig: dev.Sign(tunnel.ChannelBinding())}
}
func checkHello(id identity.Identity, tunnel *pair.Tunnel, hello Hello) error {
	if hello.Cert.V != 1 || hello.Cert.Verify(id.PublicKey()) != nil {
		return errors.New("could not confirm this computer")
	}
	pub, err := hex.DecodeString(hello.Cert.Device)
	if err != nil || len(pub) != ed25519.PublicKeySize || !ed25519.Verify(ed25519.PublicKey(pub), tunnel.ChannelBinding(), hello.Sig) {
		return errors.New("could not confirm this computer")
	}
	key, err := DoorKey(id, hello.Cert.DeviceID())
	if err != nil || !bytes.Equal(key.PublicKey().Bytes(), tunnel.Peer()) {
		return errors.New("could not confirm this computer")
	}
	return nil
}
func sendHello(tunnel *pair.Tunnel, dev identity.Dev) error {
	frame, err := NewFrame("", helloFor(dev, tunnel))
	if err != nil {
		return err
	}
	return WriteFrame(tunnel, frame)
}
func refuse(tunnel *pair.Tunnel, reason string) error {
	frame, err := NewFrame("", Refused{Reason: reason})
	if err != nil {
		return err
	}
	return WriteFrame(tunnel, frame)
}
func acceptHello(ctx context.Context, tunnel *pair.Tunnel, id identity.Identity, dev identity.Dev, roster *rosterCache, name string) error {
	frame, err := ReadFrame(tunnel)
	if err != nil {
		return err
	}
	var hello Hello
	reason := ""
	switch {
	case frame.Type != "hello" || frame.Decode(&hello) != nil:
		reason = "could not confirm this computer"
	case hello.V != Version:
		reason = DifferentVersion(name)
	default:
		if err := checkHello(id, tunnel, hello); err != nil {
			reason = err.Error()
		} else {
			active, err := roster.allows(ctx, hello.Cert.DeviceID())
			if err != nil {
				reason = "could not check your devices right now"
			} else if !active {
				reason = RemovedComputerLine
			}
		}
	}
	if reason != "" {
		_ = refuse(tunnel, reason)
		return errors.New(reason)
	}
	return sendHello(tunnel, dev)
}
