package directory

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

func TestStorageOptInFreezeNegotiatesExactLegacyBytes(t *testing.T) {
	id, err := identity.Mint()
	if err != nil {
		t.Fatal(err)
	}
	dev, err := identity.NewDevice(id)
	if err != nil {
		t.Fatal(err)
	}
	next, err := identity.Mint()
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"", "on", "off"} {
		t.Run("state="+state, func(t *testing.T) {
			var calls []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.URL.Path)
				raw, _ := io.ReadAll(r.Body)
				if _, _, err := reqsign.AuthenticateAt(time.Now)(r, raw); err != nil {
					t.Errorf("device signature: %v", err)
				}
				switch r.URL.Path {
				case "/fabric/v1/store/stats":
					json.NewEncoder(w).Encode(blobstore.Counts{Storage: state})
				case "/fabric/v1/identity/rotation":
					if string(raw) != `{"op":"freeze"}` {
						t.Errorf("changed origin/dev freeze bytes: %q", raw)
					}
					// The restored production decoder rejects unknown rotation fields.
					cert, _ := json.Marshal(dev.Cert)
					proof := clusterstate.V1Request{Method: "POST", URI: "/v1/identity/rotation", At: "1000", IdentityKey: id.PublicKey(), Certificate: base64.RawURLEncoding.EncodeToString(cert), Body: raw}
					proof.Signature = dev.Sign(clusterstate.V1SigningMessage(proof.Method, proof.URI, proof.At, proof.Body))
					if _, err := clusterstate.V1Command("test", 1000, proof); err != nil {
						t.Errorf("origin/dev decoder: %v", err)
					}
					w.Write([]byte(`{}`))
				case "/fabric/v1/store/handoff":
					var fields map[string]string
					if err := json.Unmarshal(raw, &fields); err != nil {
						t.Error(err)
						return
					}
					pub, _ := hex.DecodeString(fields["successor_key"])
					sig, _ := hex.DecodeString(fields["proof"])
					message := []byte("codeaf/storage-handoff/v1\n" + id.ID() + "\n" + next.ID())
					if len(fields) != 3 || fields["successor"] != next.ID() || !reflect.DeepEqual(pub, []byte(next.PublicKey())) || !ed25519.Verify(next.PublicKey(), message, sig) {
						t.Errorf("bad handoff: %s", raw)
					}
					w.Write([]byte(`{"successor":"` + next.ID() + `","storage":"on"}`))
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
				}
			}))
			defer server.Close()
			sign := reqsign.SignFor(storageHandoffSigner{id, dev}, time.Now)
			gate := Gate{Client: NewHTTP(server.URL+"/fabric", sign, server.Client()), Store: blobstore.NewHTTP(server.URL+"/fabric", sign, server.Client()), Source: id.ID()}
			if err := gate.Freeze(context.Background()); err != nil {
				t.Fatal(err)
			}
			accepted, err := gate.HandoffTo(context.Background(), next)
			if err != nil {
				t.Fatal(err)
			}
			if accepted != (state != "") {
				t.Fatalf("handoff accepted=%v with storage=%q", accepted, state)
			}
			want := []string{"/fabric/v1/identity/rotation", "/fabric/v1/store/stats"}
			if state != "" {
				want = append(want, "/fabric/v1/store/handoff")
			}
			if !reflect.DeepEqual(calls, want) {
				t.Fatalf("calls %v, want %v", calls, want)
			}
		})
	}
}

type storageHandoffSigner struct {
	id  identity.Identity
	dev identity.Dev
}

func (s storageHandoffSigner) IdentityKey() ed25519.PublicKey { return s.id.PublicKey() }
func (s storageHandoffSigner) Cert() identity.Cert            { return s.dev.Cert }
func (s storageHandoffSigner) Sign(msg []byte) []byte         { return s.dev.Sign(msg) }
