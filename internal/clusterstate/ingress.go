package clusterstate

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
)

const ClusterAPIPath = "/v2/cluster/api"

// ClusterRequest retains the caller's original signature across routing. The
// domain covers the selected read as well as commands; a scoped delegation is
// never accepted as a personal-device certificate.
type ClusterRequest struct {
	IdentityKey []byte             `json:"identity_key"`
	Cluster     string             `json:"cluster"`
	Certificate string             `json:"certificate,omitempty"`
	Worker      *WorkerCertificate `json:"worker,omitempty"`
	At          int64              `json:"at"`
	Action      string             `json:"action"`
	Incarnation uint64             `json:"incarnation"`
	Cell        string             `json:"cell,omitempty"`
	Assignment  *AssignmentRef     `json:"assignment,omitempty"`
	Command     *Command           `json:"command,omitempty"`
	Signature   []byte             `json:"signature"`
}

type ClusterResponse struct {
	Result     *Result     `json:"result,omitempty"`
	View       *CellView   `json:"view,omitempty"`
	Grant      []byte      `json:"grant,omitempty"`
	Assignment *Assignment `json:"assignment,omitempty"`
}

func ClusterRequestBytes(request ClusterRequest) ([]byte, error) {
	request.Signature = nil
	raw, err := Encode(request)
	return append([]byte("codeaf/cluster-api/1\n"), raw...), err
}

// VerifyClusterRequest authenticates before opening a namespace. Hosts repeat
// it at committed time under the actor queue and check the retained root,
// revocation, scope and incarnation at that same quorum-backed view.
func VerifyClusterRequest(r ClusterRequest, now int64) (string, error) {
	if len(r.IdentityKey) != ed25519.PublicKeySize || r.Cluster == "" || r.At < 0 || r.At > int64(MaxSafeInteger) || now < 0 || r.At-now > V1SkewMillis || now-r.At > V1SkewMillis {
		return "", ErrCodec
	}
	var public []byte
	var actor string
	if r.Worker != nil {
		if r.Certificate != "" || VerifyWorkerCertificate(*r.Worker, r.IdentityKey, r.Cluster, now) != nil {
			return "", ErrCodec
		}
		public = r.Worker.Key
		actor = r.Worker.DeviceID()
	} else {
		var cert v1Certificate
		raw, err := base64.RawURLEncoding.DecodeString(r.Certificate)
		if err != nil || len(raw) > 1024 || legacyBody(raw, &cert, false) != nil {
			return "", ErrCodec
		}
		public, err = hex.DecodeString(cert.Device)
		if err != nil || len(public) != ed25519.PublicKeySize {
			return "", ErrCodec
		}
		actor = DeviceIDOfKey(public)
		if PersonalCertificate(IdentityRec{Identity: IdentityID(r.IdentityKey), SigningKey: r.IdentityKey}, actor, r.Certificate) != nil || r.Incarnation != 0 {
			return "", ErrCodec
		}
	}
	switch r.Action {
	case "submit":
		if r.Command == nil || r.Cell != "" || r.Assignment != nil || r.Command.Actor != actor || r.Command.Cluster != r.Cluster || r.Command.WorkerIncarnation != r.Incarnation || r.Command.Now > r.At || ClientCommand(*r.Command) != nil {
			return "", ErrCodec
		}
		// Reject unrelated or malformed certificate bytes BEFORE they can enter
		// the replicated log. Apply still checks the committed root and role.
		if r.Command.Certificate != "" && (r.Worker != nil || PersonalCertificate(IdentityRec{Identity: IdentityID(r.IdentityKey), SigningKey: r.IdentityKey}, actor, r.Command.Certificate) != nil) {
			return "", ErrCodec
		}
		// Membership belongs to the enrollment/control protocol. A generic
		// selected-work submission cannot leave a half-enrolled personal member.
		if membershipMutation(r.Command.Change) {
			return "", ErrCodec
		}
		raw, err := SigningBytes(*r.Command)
		if err != nil || !ed25519.Verify(public, raw, r.Command.Signature) {
			return "", ErrCodec
		}
	case "cell":
		if r.Cell == "" || r.Command != nil || r.Assignment != nil || r.Worker == nil || r.Incarnation == 0 {
			return "", ErrCodec
		}
	case "assignment":
		if r.Assignment == nil || r.Command != nil || r.Cell != "" || r.Worker == nil || r.Incarnation == 0 || r.Assignment.Target != actor || r.Assignment.Incarnation != r.Incarnation {
			return "", ErrCodec
		}
	default:
		return "", ErrCodec
	}
	raw, err := ClusterRequestBytes(r)
	if err != nil || !ed25519.Verify(public, raw, r.Signature) {
		return "", ErrCodec
	}
	return actor, nil
}

// NextSession resumes an anchor's one bounded control session from durable
// application state. Each idle generation advances the cursor instead of
// allocating another retained session ID forever.
func NextSession(raw []byte) (sequence, ack uint64, err error) {
	if len(raw) == 0 {
		return 1, 0, nil
	}
	var saved session
	if Decode(raw, &saved) != nil || saved.Closed || saved.Sequence == ^uint64(0) {
		return 0, 0, ErrCodec
	}
	return saved.Sequence + 1, saved.Sequence, nil
}

// PersonalCommandAuthority accepts the existing root-signed personal certificate
// on an original signed v2 command. It grants no worker or service role and
// never overrides a retained role or committed device revocation.
func PersonalCommandAuthority(c Command, identity IdentityRec) (Authority, error) {
	if c.V1 != nil || c.WorkerIncarnation != 0 || c.Certificate == "" {
		return Authority{}, ErrCodec
	}
	key, err := legacyCertificate(LegacyConfig{Identity: identity.Identity, SigningKey: identity.SigningKey}, c.Certificate)
	if err != nil || DeviceIDOfKey(key) != c.Actor {
		return Authority{}, ErrCodec
	}
	return Authority{Cluster: c.Cluster, Key: key, Personal: true}, nil
}
