package clusterstate

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"strconv"
	"strings"
)

const PersonalAdmissionPath = "/v2/cluster/enroll"

// PersonalAdmission binds a new durable store to an existing paired device.
// The store token is created once WITH the store, not once per connection.
type PersonalAdmission struct {
	Store    string `json:"store"`
	Protocol uint16 `json:"protocol"`
}

// VerifyPersonalAdmission retains the original HTTP proof in the replicated
// command. Neither a host signature nor a presented transport key substitutes
// for this certificate under the currently committed identity root.
func VerifyPersonalAdmission(p V1Request, identity IdentityRec, now int64) (PersonalAdmission, Authority, string, error) {
	var request PersonalAdmission
	if p.Method != "POST" || strings.TrimPrefix(p.URI, "/fabric") != PersonalAdmissionPath || !bytes.Equal(p.IdentityKey, identity.SigningKey) || legacyBody(p.Body, &request, false) != nil {
		return request, Authority{}, "", ErrCodec
	}
	token, err := hex.DecodeString(request.Store)
	if err != nil || len(token) != 32 || hex.EncodeToString(token) != request.Store {
		return request, Authority{}, "", ErrCodec
	}
	at, err := strconv.ParseInt(p.At, 10, 64)
	if err != nil || at < 0 || now < 0 || at > int64(MaxSafeInteger) || now > int64(MaxSafeInteger) || at < now-V1SkewMillis || at > now+V1SkewMillis {
		return request, Authority{}, "", ErrCodec
	}
	key, err := legacyCertificate(LegacyConfig{Identity: identity.Identity, SigningKey: identity.SigningKey}, p.Certificate)
	if err != nil || !ed25519.Verify(key, V1SigningMessage(p.Method, p.URI, p.At, p.Body), p.Signature) {
		return request, Authority{}, "", ErrCodec
	}
	return request, Authority{Key: key, Personal: true}, DeviceIDOfKey(key), nil
}

// MemberStoreKey is permanent even after retirement. Retrying an old store
// cannot retire its replacement or allocate another durable node identity.
func MemberStoreKey(device, store string) RecordKey {
	return RecordKey{"membership", "store/" + segment(device) + "/" + store}
}
