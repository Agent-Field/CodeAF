package clusterstate_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"reflect"
	"strconv"
	"strings"
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

func TestV1RegisterClientAndScopedClientRefusal(t *testing.T) {
	a := newApplication(t)
	root := actorKey("root").Public().(ed25519.PublicKey)
	fullPriv := actorKey("full")
	fullID := cs.DeviceIDOfKey(fullPriv.Public().(ed25519.PublicKey))
	fullCert := joinCert(t, "full")

	clientPriv := actorKey("client")
	clientID := cs.DeviceIDOfKey(clientPriv.Public().(ed25519.PublicKey))
	clientCertB64 := joinCert(t, "client")
	clientCertJSON, err := base64.RawURLEncoding.DecodeString(clientCertB64)
	if err != nil {
		t.Fatal(err)
	}

	v1Eval := func(priv ed25519.PrivateKey, cert, method, uri string, body []byte, now int64) cs.Result {
		t.Helper()
		proof := cs.V1Request{
			Method:      method,
			URI:         uri,
			IdentityKey: root,
			Certificate: cert,
			At:          strconv.FormatInt(now, 10),
			Body:        body,
		}
		proof.Signature = ed25519.Sign(priv, cs.V1SigningMessage(proof.Method, proof.URI, proof.At, body))
		cmd, err := cs.V1Command("test", now, proof)
		if err != nil {
			t.Fatalf("V1Command(%s %s): %v", method, uri, err)
		}
		return a.evaluate(cmd).Result
	}

	// Self-registration via PUT /v1/dir/devices/{id} must refuse kind or methods.
	for _, badBody := range []string{
		`{"V":1,"name":"n","added_by":"","revoked":false,"caps":{"os":"linux","arch":"amd64","sandbox":null,"container":null,"gpu":null,"cow":"none"},"kind":"client"}`,
		`{"V":1,"name":"n","added_by":"","revoked":false,"caps":{"os":"linux","arch":"amd64","sandbox":null,"container":null,"gpu":null,"cow":"none"},"methods":["chat.read"]}`,
	} {
		proof := cs.V1Request{Method: "PUT", URI: "/v1/dir/devices/" + fullID, IdentityKey: root, Certificate: fullCert, At: "1000", Body: []byte(badBody)}
		proof.Signature = ed25519.Sign(fullPriv, cs.V1SigningMessage(proof.Method, proof.URI, proof.At, proof.Body))
		if _, err := cs.V1Command("test", 1000, proof); err == nil {
			t.Fatalf("expected PUT /v1/dir/devices with kind/methods to fail V1Command, got nil")
		}
	}
	if r := a.run("a", 1000, cs.Mutation{PutDevice: &cs.PutDevice{ID: "a", Device: cs.Device{V: 1, Kind: "client"}}}); r.Code != cs.BadRequest {
		t.Fatalf("PutDevice with Kind=client: got %s, want bad_request", r.Code)
	}
	if r := a.run("a", 1001, cs.Mutation{PutDevice: &cs.PutDevice{ID: "a", Device: cs.Device{V: 1, Methods: []string{"m"}}}}); r.Code != cs.BadRequest {
		t.Fatalf("PutDevice with Methods: got %s, want bad_request", r.Code)
	}

	// Stamped must preserve stored Kind and Methods and ignore caller values.
	stamped := cs.Stamped(cs.Device{V: 1, Kind: "client", Methods: []string{"chat.read"}}, true, cs.Device{V: 1, Kind: "", Methods: []string{"evil"}}, 2000)
	if stamped.Kind != "client" || !reflect.DeepEqual(stamped.Methods, []string{"chat.read"}) {
		t.Fatalf("Stamped did not preserve Kind/Methods: %+v", stamped)
	}

	// Invalid PUT /v1/clients/{device} payloads must fail V1Command.
	for _, tc := range []struct {
		uri  string
		body string
	}{
		{"/v1/clients/not-a-device", `{"cert":` + string(clientCertJSON) + `,"methods":[]}`},
		{"/v1/clients/" + fullID, `{"cert":` + string(clientCertJSON) + `,"methods":[]}`},
		{"/v1/clients/" + clientID, `{"cert":` + string(clientCertJSON) + `,"methods":["bad method"]}`},
		{"/v1/clients/" + clientID, `{"cert":` + string(clientCertJSON) + `,"methods":["dup","dup"]}`},
		{"/v1/clients/" + clientID, `{"cert":` + string(clientCertJSON) + `,"methods":[],"name":"` + strings.Repeat("n", cs.MaxClientName+1) + `"}`},
	} {
		proof := cs.V1Request{Method: "PUT", URI: tc.uri, IdentityKey: root, Certificate: fullCert, At: "1002", Body: []byte(tc.body)}
		proof.Signature = ed25519.Sign(fullPriv, cs.V1SigningMessage(proof.Method, proof.URI, proof.At, proof.Body))
		if _, err := cs.V1Command("test", 1002, proof); err == nil {
			t.Fatalf("expected invalid client registration (%s) to fail V1Command", tc.uri)
		}
	}

	// A full device cannot register itself as a client.
	fullCertJSON, _ := base64.RawURLEncoding.DecodeString(fullCert)
	if r := v1Eval(fullPriv, fullCert, "PUT", "/v1/clients/"+fullID, []byte(`{"cert":`+string(fullCertJSON)+`,"methods":[]}`), 1003); r.Code != cs.Exists {
		t.Fatalf("self client registration: got %s, want exists", r.Code)
	}

	// Valid client registration succeeds and commits Kind="client" and Methods.
	regBody := []byte(`{"cert":` + string(clientCertJSON) + `,"methods":["chat.read","chat.send"],"name":"sealed-phone"}`)
	if r := v1Eval(fullPriv, fullCert, "PUT", "/v1/clients/"+clientID, regBody, 1004); r.Code != cs.OK {
		t.Fatalf("RegisterClient: got %s, want ok", r.Code)
	}
	var stored cs.Device
	if err := cs.Decode(a.rows[cs.DeviceKey(clientID)], &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Kind != "client" || !reflect.DeepEqual(stored.Methods, []string{"chat.read", "chat.send"}) || stored.Name != "sealed-phone" || stored.AddedBy != cs.IdentityID(root) || stored.Created != 1004 {
		t.Fatalf("unexpected stored client device: %+v", stored)
	}

	// Re-registering the same client returns Exists.
	if r := v1Eval(fullPriv, fullCert, "PUT", "/v1/clients/"+clientID, regBody, 1005); r.Code != cs.Exists {
		t.Fatalf("duplicate RegisterClient: got %s, want exists", r.Code)
	}

	// A scoped client is refused every directory mutation (both v1 and v2).
	otherPriv := actorKey("other")
	otherID := cs.DeviceIDOfKey(otherPriv.Public().(ed25519.PublicKey))
	otherCertJSON, _ := base64.RawURLEncoding.DecodeString(joinCert(t, "other"))
	for _, tc := range []struct {
		method string
		uri    string
		body   string
	}{
		{"PUT", "/v1/dir/devices/" + clientID, `{"V":1,"name":"n","added_by":"","revoked":false,"caps":{"os":"linux","arch":"amd64","sandbox":null,"container":null,"gpu":null,"cow":"none"}}`},
		{"POST", "/v1/dir/vault", `{"old":"","new":"v1"}`},
		{"POST", "/v1/dir/cells/chat", `{"head":"h"}`},
		{"POST", "/v1/dir/devices/" + fullID + "/revoke", ``},
		{"PUT", "/v1/clients/" + otherID, `{"cert":` + string(otherCertJSON) + `,"methods":[]}`},
	} {
		if r := v1Eval(clientPriv, clientCertB64, tc.method, tc.uri, []byte(tc.body), 1006); r.Code != cs.Unauthorized {
			t.Fatalf("scoped client %s %s: got %s, want unauthorized", tc.method, tc.uri, r.Code)
		}
	}

	// Revoking the client marks Revoked=true while keeping Kind="client", and re-registration is still Exists.
	if r := v1Eval(fullPriv, fullCert, "POST", "/v1/dir/devices/"+clientID+"/revoke", nil, 1007); r.Code != cs.OK {
		t.Fatalf("revoke client: got %s, want ok", r.Code)
	}
	if err := cs.Decode(a.rows[cs.DeviceKey(clientID)], &stored); err != nil {
		t.Fatal(err)
	}
	if !stored.Revoked || stored.Kind != "client" {
		t.Fatalf("revoked client record: %+v", stored)
	}
	if r := v1Eval(fullPriv, fullCert, "PUT", "/v1/clients/"+clientID, regBody, 1008); r.Code != cs.Exists {
		t.Fatalf("re-register revoked client: got %s, want exists", r.Code)
	}
}
