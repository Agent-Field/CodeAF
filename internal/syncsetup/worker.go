package syncsetup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
	"github.com/Agent-Field/codeaf/internal/handoff"
	"github.com/Agent-Field/codeaf/internal/identity"
)

var ErrWorkerIncomplete = errors.New("this work may be incomplete; its saved records have been kept")

// WorkerAdapter is an internal selected-chat door. Its caller supplies scoped
// storage/keys and a quorum-backed directory; it never opens a personal home.
// Submit preserves the signed request and returns only after durable commit.
// Current passes a fresh quorum barrier and checks current worker authority,
// assignment revision/incarnation and the chat fence in the SAME applied view.
type WorkerAdapter struct {
	Credential  identity.Worker
	Incarnation uint64
	StateDir    string
	Taker       handoff.Taker
	Submit      func(context.Context, cs.Command) (cs.Result, error)
	Current     func(context.Context, cs.AssignmentRef) (cs.Assignment, directory.CellView, error)
	Now         func() int64
}

type workerAttempt struct {
	Ref     cs.AssignmentRef `json:"ref"`
	Claim   cs.Command       `json:"claim"`
	Started bool             `json:"started"`
	Done    bool             `json:"done"`
	Finish  *cs.Command      `json:"finish,omitempty"`
}

// Run stages through the ordinary handoff preparation/reconciliation path.
// A durable intent precedes Execute, and any restart after that point displays
// incomplete work instead of invoking it again. Execute uses the existing
// call journal and cellsync upload/publish path, returning its published head.
func (w WorkerAdapter) Run(ctx context.Context, a cs.Assignment, execute func(context.Context, handoff.Taken) (string, error)) error {
	if w.Now == nil || w.Submit == nil || w.Current == nil || w.Taker.Dir == nil || w.Taker.RootFor == nil || w.Taker.Fetch == nil || w.Taker.Local == nil || execute == nil || w.StateDir == "" || a.Spec.Target != w.Credential.ID() || a.Spec.Incarnation != w.Incarnation || !w.Credential.Certificate.Allows(a.Spec.Cell) {
		return directory.ErrUnauthorized
	}
	if err := os.MkdirAll(w.StateDir, 0700); err != nil {
		return err
	}
	// The key includes routing identity, so neither another worker nor a new
	// incarnation can accidentally adopt an old process's execution permission.
	key, _ := cs.Encode(struct {
		Cluster string
		Ref     cs.AssignmentRef
	}{w.Credential.Certificate.Cluster, a.Ref()})
	sum := sha256.Sum256(key)
	path := filepath.Join(w.StateDir, hex.EncodeToString(sum[:])+".json")
	cellHash := sha256.Sum256([]byte(w.Credential.Certificate.Cluster + "\x00" + a.Spec.Cell))
	cellPath := filepath.Join(w.StateDir, "cell-"+hex.EncodeToString(cellHash[:])+".json")
	lock, err := os.OpenFile(cellPath+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = filelock.Lock(lock, true, true); err != nil {
		return err
	}
	defer filelock.Unlock(lock)
	if b, e := os.ReadFile(cellPath); e == nil {
		var previous workerAttempt
		if json.Unmarshal(b, &previous) != nil || previous.Started && !previous.Done && previous.Ref != a.Ref() {
			return ErrWorkerIncomplete
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	attempt := workerAttempt{Ref: a.Ref()}
	raw, err := os.ReadFile(path)
	if err == nil {
		if json.Unmarshal(raw, &attempt) != nil || attempt.Ref != a.Ref() {
			return ErrWorkerIncomplete
		}
		if attempt.Done {
			return nil
		}
		if attempt.Started {
			// A recorded finish can safely retry coordination, never tool execution.
			if attempt.Finish == nil {
				return ErrWorkerIncomplete
			}
			r, e := w.Submit(ctx, *attempt.Finish)
			if e != nil {
				return e
			}
			if e = workerResultError(r.Code); e != nil {
				return e
			}
			attempt.Done = true
			return errors.Join(saveWorkerAttempt(path, attempt), saveWorkerAttempt(cellPath, attempt))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else {
		attempt.Claim, err = w.command("claim", a.Ref(), cs.Mutation{ClaimAssignment: &cs.ClaimAssignment{Ref: a.Ref(), Expect: a.Spec.Expect}})
		if err != nil {
			return err
		}
		if err = saveWorkerAttempt(path, attempt); err != nil {
			return err
		}
	}
	taker := w.Taker
	taker.Dir = workerClaimDirectory{Client: taker.Dir, claim: func(ctx context.Context, id string, o directory.AcquireOpts) (directory.CellView, error) {
		if id != a.Spec.Cell || o.Expect == nil {
			return directory.CellView{}, directory.ErrLeaseHeld
		}
		r, e := w.Submit(ctx, attempt.Claim)
		if e != nil {
			return directory.CellView{}, e
		}
		if e = workerResultError(r.Code); e != nil {
			return directory.CellView{}, e
		}
		if r.View == nil || r.Assignment == nil || r.Assignment.Ref() != a.Ref() || r.Assignment.Status != "claimed" {
			return directory.CellView{}, directory.ErrUnauthorized
		}
		current, view, e := w.Current(ctx, a.Ref())
		if e != nil {
			return directory.CellView{}, e
		}
		if current.Ref() != a.Ref() || current.Status != "claimed" || current.Fence != r.View.Cell.Lease.Fence || view.Cell.Lease.Device != w.Credential.ID() || view.Cell.Lease.Fence != current.Fence {
			return directory.CellView{}, directory.ErrFenceStale
		}
		return view, nil
	}}
	taken, err := taker.TakeSeen(ctx, a.Spec.Cell, nil)
	if err != nil {
		return err
	}
	current, view, err := w.Current(ctx, a.Ref())
	if err != nil {
		return err
	}
	if current.Ref() != a.Ref() || current.Status != "claimed" || current.Fence != taken.Driving.Fence || view.Cell.Lease.Device != w.Credential.ID() || view.Cell.Lease.Fence != current.Fence || view.Cell.Head != taken.Driving.Head {
		return directory.ErrFenceStale
	}
	// Reassignment recovers existing records; it never instructs a replacement
	// process to replay tools whose completion may be missing elsewhere.
	if a.Spec.Revision > 1 {
		return ErrWorkerIncomplete
	}
	attempt.Started = true
	if err = saveWorkerAttempt(path, attempt); err != nil {
		return err
	}
	if err = saveWorkerAttempt(cellPath, attempt); err != nil {
		return err
	}
	head, err := execute(ctx, taken)
	if err != nil {
		return err
	}
	finish, err := w.command("finish", a.Ref(), cs.Mutation{FinishAssignment: &cs.FinishAssignment{Ref: a.Ref(), Fence: taken.Driving.Fence, Head: head}})
	if err != nil {
		return err
	}
	attempt.Finish = &finish
	if err = saveWorkerAttempt(path, attempt); err != nil {
		return err
	}
	r, err := w.Submit(ctx, finish)
	if err != nil {
		return err
	}
	if err = workerResultError(r.Code); err != nil {
		return err
	}
	attempt.Done = true
	return errors.Join(saveWorkerAttempt(path, attempt), saveWorkerAttempt(cellPath, attempt))
}
func (w WorkerAdapter) command(kind string, ref cs.AssignmentRef, m cs.Mutation) (cs.Command, error) {
	b, _ := cs.Encode(ref)
	h := sha256.Sum256(b)
	c := cs.Command{Schema: cs.SchemaVersion, Cluster: w.Credential.Certificate.Cluster, Actor: w.Credential.ID(), WorkerIncarnation: w.Incarnation, Session: "worker-" + kind + "-" + hex.EncodeToString(h[:]), Sequence: 1, Now: w.Now(), Change: m}
	var err error
	c.RequestDigest, err = cs.Digest(c)
	if err != nil {
		return c, err
	}
	signed, err := cs.SigningBytes(c)
	if err != nil {
		return c, err
	}
	c.Signature = w.Credential.Sign(signed)
	return c, nil
}

type workerClaimDirectory struct {
	directory.Client
	claim func(context.Context, string, directory.AcquireOpts) (directory.CellView, error)
}

func (d workerClaimDirectory) Acquire(ctx context.Context, id string, o directory.AcquireOpts) (directory.CellView, error) {
	return d.claim(ctx, id, o)
}
func workerResultError(code cs.Code) error {
	switch code {
	case cs.OK:
		return nil
	case cs.LeaseHeld:
		return directory.ErrLeaseHeld
	case cs.FenceStale:
		return directory.ErrFenceStale
	case cs.HeadMoved:
		return directory.ErrHeadMoved
	case cs.Revoked:
		return directory.ErrRevoked
	case cs.Unauthorized:
		return directory.ErrUnauthorized
	default:
		return fmt.Errorf("worker coordination: %s", code)
	}
}

// The execution intent uses a full local durability boundary, independent of
// the ordinary call-record batching window. Failure retains the prior record.
func saveWorkerAttempt(path string, v workerAttempt) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".worker-")
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
	err = errors.Join(err, f.Close())
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
