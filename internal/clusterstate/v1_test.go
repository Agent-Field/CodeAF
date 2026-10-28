package clusterstate_test

import (
	"crypto/ed25519"
	"strconv"
	"testing"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

// R25 requires legal legacy empty frame plans to survive canonical forwarding
// without invalidating the original signature or retaining an old frame hint.
func TestR25V1EmptyFramePlansKeepOriginalAuthority(t *testing.T) {
	root := actorKey("root")
	device := actorKey("a")
	identity := cs.IdentityRec{V: 1, Identity: cs.IdentityID(root.Public().(ed25519.PublicKey)), SigningKey: root.Public().(ed25519.PublicKey)}
	for _, path := range []string{"/v1/dir/cells/chat", "/v1/dir/cells/chat/publish"} {
		body := []byte(`{"head":"new","frames":[]}`)
		if path == "/v1/dir/cells/chat/publish" {
			body = []byte(`{"fence":1,"old_head":"old","head":"new","frames":[]}`)
		}
		proof := cs.V1Request{Method: "POST", URI: path, IdentityKey: identity.SigningKey, Certificate: joinCert(t, "a"), At: strconv.Itoa(1000), Body: body}
		proof.Signature = ed25519.Sign(device, cs.V1SigningMessage(proof.Method, proof.URI, proof.At, body))
		command, err := cs.V1Command("test", 1000, proof)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := cs.EncodeCommand(command)
		if err != nil {
			t.Fatal(err)
		}
		forwarded, err := cs.DecodeCommand(raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := cs.V1AuthorityAt(forwarded, identity, 1000); !ok {
			t.Fatal("R25 original empty frame request lost its authority", path)
		}
		if forwarded.Change.Create != nil && forwarded.Change.Create.Init.Frames != nil || forwarded.Change.Publish != nil && forwarded.Change.Publish.Publish.Frames != nil {
			t.Fatal("empty frame plan was not normalized")
		}
	}
}
