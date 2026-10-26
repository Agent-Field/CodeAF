package syncsetup

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"net/http"
	"os"
	"path/filepath"
)

func (s *Sync) clusterClient() clusterctl.Client {
	return clusterctl.Client{Base: s.Relay, Sign: reqsign.SignFor(deviceSigner{s.Identity, s.Device}, s.now), HTTP: s.ClusterHTTP(&http.Client{Timeout: clusterctl.ForwardTimeout})}
}
func (s *Sync) ClusterStatus(ctx context.Context) (clusterctl.Status, error) {
	if !s.memberAllowed() {
		return clusterctl.Status{}, s.clusterRefusal(ctx, ErrQuiet)
	}
	status, err := s.clusterClient().Status(ctx)
	if err != nil {
		return status, s.clusterRefusal(ctx, err)
	}
	s.memberMu.Lock()
	defer s.memberMu.Unlock()
	if s.member != nil {
		return status, nil
	}
	m, e := s.openMember(ctx)
	if errors.Is(e, clusterctl.ErrUnsupported) || filelock.IsBusy(e) {
		return status, nil
	}
	if e != nil {
		return status, s.clusterRefusal(ctx, e)
	}
	m.close()
	status, err = s.clusterClient().Status(ctx)
	return status, s.clusterRefusal(ctx, err)
}

// clusterRefusal verifies removal through the directory's authenticated door.
// Both relays can return an untyped coordination refusal, including a plain
// forbidden response. That response alone does not establish device removal.
func (s *Sync) clusterRefusal(ctx context.Context, err error) error {
	if err == nil || errors.Is(err, directory.ErrRevoked) {
		return err
	}
	if _, refusal := s.Dir.List(ctx); errors.Is(refusal, directory.ErrRevoked) {
		return refusal
	}
	return err
}

type leadershipSession struct {
	Sequence uint64
	Pending  *cs.Command
}

// LeadCluster saves the exact signed request before transmission. A lost reply
// retries that same operation after restart, including its observed revision;
// it cannot silently promote a newer target or allocate a second request.
func (s *Sync) LeadCluster(ctx context.Context, node uint64) (receipt cs.LeadershipReceipt, err error) {
	defer func() { err = s.clusterRefusal(ctx, err) }()
	if Quiet(s.Home) {
		return receipt, ErrQuiet
	}
	path := filepath.Join(s.Home, "cluster-leadership-"+s.Device.ID()+".json")
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return receipt, err
	}
	defer lock.Close()
	if err = filelock.Lock(lock, true, true); err != nil {
		return receipt, errors.New("another window is requesting leadership on this computer")
	}
	defer filelock.Unlock(lock)
	var saved leadershipSession
	b, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(b, &saved)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return receipt, err
	}
	if saved.Pending != nil && (saved.Pending.Change.RequestLeadership == nil || saved.Pending.Actor != s.Device.ID() || saved.Pending.Session != "leadership" || saved.Pending.Sequence != saved.Sequence+1) {
		return receipt, errors.New("this computer has an unreadable pending leadership request; its record has been kept")
	}
	client := s.clusterClient()
	if saved.Pending == nil {
		status, e := client.Status(ctx)
		if e != nil {
			return receipt, e
		}
		if node == 0 {
			node = status.Self
		}
		if node == 0 {
			return receipt, errors.New("this computer must join the cluster with persistent storage before it can lead")
		}
		if status.LeadershipUnavailable {
			return receipt, clusterctl.ErrUnsupported
		}
		if saved.Sequence == ^uint64(0) {
			return receipt, errors.New("this computer's leadership request sequence is full")
		}
		c := cs.Command{Schema: cs.SchemaVersion, Cluster: status.Cluster, Actor: s.Device.ID(), Session: "leadership", Sequence: saved.Sequence + 1, Ack: saved.Sequence, Now: s.now().UnixMilli(), Change: cs.Mutation{RequestLeadership: &cs.RequestLeadership{Revision: status.Membership.Revision, Node: node, Device: s.Device.ID()}}}
		// A personal device may request another enrolled target, but must bind its
		// committed device ID rather than guessing it from a node number.
		if node != status.Self {
			return receipt, errors.New("run codeaf cluster lead on the computer that should lead")
		}
		c.RequestDigest, e = cs.Digest(c)
		if e != nil {
			return receipt, e
		}
		signed, e := cs.SigningBytes(c)
		if e != nil {
			return receipt, e
		}
		c.Signature = s.Device.Sign(signed)
		saved.Pending = &c
		if err = saveLeadership(path, saved); err != nil {
			return receipt, err
		}
	} else if node != 0 && saved.Pending.Change.RequestLeadership.Node != node {
		return receipt, errors.New("a leadership request is still pending; retry it before choosing another computer")
	}
	// A closed terminal may use a scoped lifetime over its existing member store.
	// Another open window already owns that store and keeps serving independently.
	var scoped *personalMember
	if s.displayMember() == nil {
		if _, e := os.Stat(filepath.Join(s.memberPath(), "identity.json")); e == nil {
			scoped, e = s.openMember(ctx)
			if e != nil && !filelock.IsBusy(e) {
				return receipt, e
			}
			if scoped != nil {
				defer scoped.close()
				if e = scoped.start(ctx); e != nil {
					return receipt, e
				}
			}
		}
	}
	receipt, err = client.Lead(ctx, *saved.Pending)
	if err != nil {
		var refused clusterctl.CommittedRefusal
		if errors.As(err, &refused) && refused.Sequence == saved.Pending.Sequence {
			saved.Sequence = saved.Pending.Sequence
			saved.Pending = nil
			if e := saveLeadership(path, saved); e != nil {
				return receipt, e
			}
		}
		return receipt, err
	}
	saved.Sequence = saved.Pending.Sequence
	saved.Pending = nil
	if err = saveLeadership(path, saved); err != nil {
		return cs.LeadershipReceipt{}, err
	}
	return receipt, nil
}
func saveLeadership(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".cluster-lead-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = fsreplace.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
