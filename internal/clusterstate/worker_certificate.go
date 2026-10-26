package clusterstate

import (
	"crypto/ed25519"
	"sort"
)

const (
	MaxWorkerScopes      = 32
	MaxWorkerRecords     = 4096
	MaxWorkerSealedBytes = 4096
)

// WorkerCertificate delegates selected chats without exporting identity secrets.
// Learner is explicit consent to see the identity's routing log; it never
// grants a vote. Human labels and execution credentials belong in sealed grants.
type WorkerCertificate struct {
	V         uint16   `json:"v"`
	Identity  string   `json:"identity"`
	Cluster   string   `json:"cluster"`
	Key       []byte   `json:"key"`
	Made      int64    `json:"made"`
	Expires   int64    `json:"expires"`
	Cells     []string `json:"cells"`
	Learner   bool     `json:"learner"`
	Signature []byte   `json:"signature"`
}

// WorkerCertificateBytes has a separate domain from personal certificates, so
// delegation can never be presented as approval for personal-device pairing.
func WorkerCertificateBytes(c WorkerCertificate) ([]byte, error) {
	c.Signature = nil
	b, err := Encode(c)
	return append([]byte("codeaf/worker-cert/v1\x00"), b...), err
}
func (c WorkerCertificate) DeviceID() string { return DeviceIDOfKey(c.Key) }
func (c WorkerCertificate) Allows(cell string) bool {
	i := sort.SearchStrings(c.Cells, cell)
	return i < len(c.Cells) && c.Cells[i] == cell
}

// VerifyWorkerCertificate uses committed cluster time, including the exclusive
// expiry boundary. There is no local clock or shared-root worker credential.
func VerifyWorkerCertificate(c WorkerCertificate, public []byte, cluster string, now int64) error {
	if c.V != 1 || c.Cluster == "" || c.Cluster != cluster || len(public) != ed25519.PublicKeySize || c.Identity != IdentityID(public) || len(c.Key) != ed25519.PublicKeySize || c.Made < 0 || c.Expires <= c.Made || c.Expires > int64(MaxSafeInteger)-LeaseTTLMillis || now < c.Made || now >= c.Expires || len(c.Cells) == 0 || len(c.Cells) > MaxWorkerScopes {
		return ErrCodec
	}
	for i, cell := range c.Cells {
		if cell == "" || len(cell) > 256 || i > 0 && c.Cells[i-1] >= cell {
			return ErrCodec
		}
	}
	b, err := WorkerCertificateBytes(c)
	if err != nil || !ed25519.Verify(public, b, c.Signature) {
		return ErrCodec
	}
	return nil
}
