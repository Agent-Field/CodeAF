package clusterstate_test

import (
	"crypto/ed25519"
	"encoding/json"
	"strings"
	"testing"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

func prefixedProof(t *testing.T, prefix, method, path, body string) cs.V1Request {
	t.Helper()
	root := actorKey("root").Public().(ed25519.PublicKey)
	p := cs.V1Request{Method: method, URI: prefix + path, IdentityKey: root, Certificate: joinCert(t, "a"), At: "1000", Body: []byte(body)}
	p.Signature = ed25519.Sign(actorKey("a"), cs.V1SigningMessage(method, p.URI, p.At, p.Body))
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if prefix != "" {
		fields["base_path"], err = json.Marshal(prefix)
		if err != nil {
			t.Fatal(err)
		}
	}
	raw, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := cs.Decode(raw, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestR5_3CommittedPrefixCannotChangeSignedOperation(t *testing.T) {
	root := actorKey("root").Public().(ed25519.PublicKey)
	id := cs.IdentityRec{V: 1, Identity: cs.IdentityID(root), SigningKey: root}
	p := prefixedProof(t, "/a/relay", "POST", "/v1/dir/cells/chat", `{"head":"saved"}`)
	c, err := cs.V1Command("test", 1000, p)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"uri", "operation", "prefix"} {
		t.Run(change, func(t *testing.T) {
			raw, err := cs.EncodeCommand(c)
			if err != nil {
				t.Fatal(err)
			}
			bad, err := cs.DecodeCommand(raw)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "uri":
				bad.V1.URI = "/a/relay/v1/dir/cells/other"
			case "operation":
				bad.Change.Create.Cell = "other"
			case "prefix":
				var fields map[string]any
				proof, _ := json.Marshal(bad.V1)
				if err := json.Unmarshal(proof, &fields); err != nil {
					t.Fatal(err)
				}
				fields["base_path"] = "/a"
				proof, _ = json.Marshal(fields)
				if err := cs.Decode(proof, bad.V1); err != nil {
					t.Fatal(err)
				}
			}
			if _, ok := cs.V1Authority(bad, id); ok {
				t.Fatal("altered routing proof gained authority")
			}
		})
	}
	for _, prefix := range []string{"", "/a", "/a/relay/", "/a/relayx", "a/relay"} {
		candidate := prefixedProof(t, prefix, "GET", "/v1/dir/list", "")
		candidate.URI = p.URI
		if _, err := candidate.Mutation(); err == nil {
			t.Fatalf("mismatched prefix %q accepted", prefix)
		}
	}
	var fields map[string]any
	raw, _ := json.Marshal(p)
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields["unknown"] = true
	raw, _ = json.Marshal(fields)
	if cs.Decode(raw, &p) == nil {
		t.Fatal("unknown proof field accepted")
	}
}

func TestR5_5PersonalAdmissionUsesCommittedPrefix(t *testing.T) {
	root := actorKey("root").Public().(ed25519.PublicKey)
	id := cs.IdentityRec{V: 1, Identity: cs.IdentityID(root), SigningKey: root}
	for _, prefix := range []string{"", "/fabric", "/my-relay", "/a/relay"} {
		t.Run(prefix, func(t *testing.T) {
			p := prefixedProof(t, prefix, "POST", cs.PersonalAdmissionPath, `{"store":"`+strings.Repeat("01", 32)+`","protocol":1}`)
			request, auth, actor, err := cs.VerifyPersonalAdmission(p, id, 1000)
			if err != nil || request.Protocol != 1 || !auth.Personal || actor != cs.DeviceIDOfKey(actorKey("a").Public().(ed25519.PublicKey)) {
				t.Fatalf("prefixed admission refused: %+v %v", request, err)
			}
			p.URI = prefix + "/v2/cluster/control"
			if _, _, _, err := cs.VerifyPersonalAdmission(p, id, 1000); err == nil {
				t.Fatal("different signed route admitted")
			}
		})
	}
}

func TestR5_3StagedRotationUsesCommittedPrefix(t *testing.T) {
	for _, prefix := range []string{"/my-relay", "/a/relay"} {
		t.Run(prefix, func(t *testing.T) {
			p := prefixedProof(t, prefix, "POST", "/v1/identity/rotation", `{"op":"thaw"}`+strings.Repeat(" ", cs.MaxCommandBytes))
			c, err := cs.V1Command("test", 1000, p)
			if err != nil {
				t.Fatal(err)
			}
			identity := cs.IdentityRec{V: 1, Identity: cs.IdentityID(p.IdentityKey), SigningKey: p.IdentityKey, Rotation: &cs.Rotation{State: "frozen", By: c.Actor, At: 1000}}
			raw, err := cs.Encode(identity)
			if err != nil {
				t.Fatal(err)
			}
			rows := map[cs.RecordKey][]byte{cs.IdentityKey(): raw}
			meta := cs.ApplicationMeta{Cluster: "test", Now: 1000, Ceiling: cs.DefaultApplicationBytes, Bytes: cs.RecordBytes(cs.IdentityKey(), raw)}
			parts, err := cs.V1Commands(c)
			if err != nil {
				t.Fatal(err)
			}
			if len(parts) < 2 {
				t.Fatal("large signed request did not stage")
			}
			for _, part := range parts {
				if result := applyV1Page(t, part, rows, &meta); result.Code != cs.OK {
					t.Fatal(result.Code)
				}
			}
			if cs.Decode(rows[cs.IdentityKey()], &identity) != nil || identity.Rotation != nil {
				t.Fatal("prefixed thaw did not commit")
			}
		})
	}
}
