package identity

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"sort"
	"time"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
)

// WorkerCert is deliberately a different wire type and signing domain from
// Cert. A delegated worker cannot be passed to Seed.Join or personal pairing.
type WorkerCert = clusterstate.WorkerCertificate

// IssueWorkerCert signs an expiring, selected-chat delegation. The caller seals
// selected chat keys and execution credentials separately for the worker's key.
// Learner consent discloses routing metadata for the whole identity.
func IssueWorkerCert(id Identity, public ed25519.PublicKey, cluster string, cells []string, made, expires time.Time, learner bool) (WorkerCert, error) {
	cells = append([]string(nil), cells...)
	sort.Strings(cells)
	c := WorkerCert{V: 1, Identity: id.ID(), Cluster: cluster, Key: bytes.Clone(public), Made: made.UnixMilli(), Expires: expires.UnixMilli(), Cells: cells, Learner: learner}
	b, err := clusterstate.WorkerCertificateBytes(c)
	if err != nil {
		return WorkerCert{}, err
	}
	c.Signature = id.Sign(b)
	if err := clusterstate.VerifyWorkerCertificate(c, id.PublicKey(), cluster, made.UnixMilli()); err != nil {
		return WorkerCert{}, err
	}
	return c, nil
}

// Worker holds only a worker's own private seed and its public delegation. It
// contains none of the identity seed, dedup secret or identity-wide content key.
type Worker struct {
	Certificate WorkerCert
	seed        key
}

func (s Seed) JoinWorker(c WorkerCert, public ed25519.PublicKey, cluster string, now time.Time) (Worker, error) {
	if !bytes.Equal(c.Key, s.Public()) {
		return Worker{}, errors.New("identity: the certificate is for another worker")
	}
	if err := clusterstate.VerifyWorkerCertificate(c, public, cluster, now.UnixMilli()); err != nil {
		return Worker{}, err
	}
	// Detach the signed scope from the enrollment caller's mutable buffers.
	raw, _ := clusterstate.Encode(c)
	var owned WorkerCert
	if err := clusterstate.Decode(raw, &owned); err != nil {
		return Worker{}, err
	}
	return Worker{Certificate: owned, seed: s.seed}, nil
}
func (w Worker) ID() string           { return w.Certificate.DeviceID() }
func (w Worker) Sign(b []byte) []byte { return ed25519.Sign(ed25519.NewKeyFromSeed(w.seed[:]), b) }

// Marshal persists only this worker's own seed and public delegation. Like
// Dev.Marshal, these private bytes are local credentials, never an enrollment
// payload or part of a log/snapshot. No identity-wide secret is present.
func (w Worker) Marshal() ([]byte, error) {
	return clusterstate.Encode(workerDocument{V: 1, Seed: hex.EncodeToString(w.seed[:]), Certificate: w.Certificate})
}

type workerDocument struct {
	V           uint16     `json:"v"`
	Seed        string     `json:"worker_seed"`
	Certificate WorkerCert `json:"certificate"`
}

// UnmarshalWorker resumes a locally retained worker without loading or making
// a personal identity. Expired/tampered delegations retain their saved bytes but
// cannot become active credentials again.
func UnmarshalWorker(raw []byte, public ed25519.PublicKey, cluster string, now time.Time) (Worker, error) {
	var doc workerDocument
	if err := clusterstate.Decode(raw, &doc); err != nil {
		return Worker{}, err
	}
	if doc.V != 1 {
		return Worker{}, errors.New("identity: unsupported worker credential")
	}
	var seed key
	if err := decodeKey(doc.Seed, &seed); err != nil {
		return Worker{}, err
	}
	return (Seed{seed: seed}).JoinWorker(doc.Certificate, public, cluster, now)
}
