package rotate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"net/http"
	"net/http/httptest"
	"testing"
)

type storageCountWrites struct {
	blobstore.Store
	writes *int
}

func (s storageCountWrites) PutFrame(ctx context.Context, b []byte) (blobstore.FrameID, error) {
	*s.writes++
	return s.Store.PutFrame(ctx, b)
}
func TestStorageOptInRotationOffFinishesWithoutWrites(t *testing.T) {
	for _, resume := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "frozen"}[resume], func(t *testing.T) {
			r := newRig(t)
			r.putVault()
			writes := 0
			connect := r.env.Connect
			r.env.Connect = func(s reqsign.Signer) Relay {
				side := connect(s)
				side.Store = storageCountWrites{side.Store, &writes}
				return side
			}
			r.env.StorageAllowed = func(context.Context, blobstore.Store) (bool, error) { return false, nil }
			if resume {
				r.env.After = crashAt(string(Frozen))
				if _, err := r.env.Rotate(t.Context()); err != errCrash {
					t.Fatalf("crash %v", err)
				}
				r.env.After = nil
			}
			result, err := r.env.Rotate(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if result.Chats != 2 {
				t.Fatalf("local chat count %d", result.Chats)
			}
			if writes != 0 || Pending(r.home) || r.relay.of(r.old.PublicKey()).state != "retired" {
				t.Fatalf("writes=%d pending=%v", writes, Pending(r.home))
			}
			id, err := identity.Load(r.home)
			if err != nil || id.ID() != result.NewID {
				t.Fatalf("identity %v", err)
			}
			vault, err := keys.Open(r.home)
			if err != nil {
				t.Fatal(err)
			}
			entry, err := vault.Get("p/KEY")
			if err != nil || entry.Value != "secret" {
				t.Fatalf("local vault %v", err)
			}
			// Local objects are shared by the engines before and after rotation.
			for _, chat := range []string{chatA, chatB} {
				head := listing(t, r.relay.of(r.old.PublicKey()), r.dev.ID()).Cells[chat].Head
				if missing, err := r.engineFor(id).Want(t.Context(), r.cell(chat), head); err != nil || len(missing) > 0 {
					t.Fatalf("local chat %s: missing %v, %v", chat, missing, err)
				}
			}
		})
	}
}

func TestStorageOptInCanceledRotationKeepsBoundSuccessor(t *testing.T) {
	r := newRig(t)
	r.env.After = crashAt(string(Frozen))
	if _, err := r.env.Rotate(t.Context()); err != errCrash {
		t.Fatal(err)
	}
	j, _, err := load(r.home)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.env.Abandon(t.Context()); err != nil {
		t.Fatal(err)
	}
	if Pending(r.home) {
		t.Fatal("cancellation still blocks ordinary sync")
	}
	r.env.After = nil
	result, err := r.env.Rotate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result.NewID != j.NewID {
		t.Fatal("new successor would conflict with first handoff")
	}
}

func TestStorageOptInOffRotationPlanHasNoUploadCost(t *testing.T) {
	r := newRig(t)
	r.env.StorageAllowed = func(context.Context, blobstore.Store) (bool, error) { return false, nil }
	plan, err := r.env.Plan(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Chats != 2 || plan.UpBytes != 0 {
		t.Fatalf("off plan: %+v", plan)
	}
}

// A failed handoff leaves the Frozen journal resumable, without another freeze
// or a resealing write before the retry succeeds.
func TestStorageOptInRotationHandoffBeforeUploads(t *testing.T) {
	for _, resume := range []bool{false, true} {
		for _, state := range []string{"on", "off"} {
			t.Run(fmt.Sprintf("resume=%v/storage=%s", resume, state), func(t *testing.T) {
				r := newRig(t)
				connect := r.env.Connect
				source := connect(signer{r.old.PublicKey(), r.dev})
				freezes, handoffs, writes := 0, 0, 0
				refuse := true
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
					switch request.URL.Path {
					case "/v1/identity/rotation":
						var req directory.RotationReq
						if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
							t.Error(err)
							return
						}
						var err error
						switch req.Op {
						case directory.OpFreeze:
							freezes++
							err = source.Gate.Freeze(request.Context())
						case directory.OpRetire:
							err = source.Gate.Retire(request.Context(), 0)
						default:
							t.Errorf("unexpected rotation %s", req.Op)
						}
						if err != nil {
							t.Error(err)
							http.Error(w, err.Error(), 500)
							return
						}
						w.Write([]byte(`{}`))
					case "/v1/store/stats":
						json.NewEncoder(w).Encode(blobstore.Counts{Storage: state})
					case "/v1/store/handoff":
						handoffs++
						if r.relay.of(r.old.PublicKey()).state != "frozen" {
							t.Error("handoff before freeze")
						}
						if refuse {
							http.Error(w, `{"err":"unavailable"}`, 503)
							return
						}
						w.Write([]byte(`{"storage":"` + state + `"}`))
					default:
						t.Errorf("unexpected request %s", request.URL.Path)
					}
				}))
				defer server.Close()
				sign := func(*http.Request, []byte) {}
				r.env.Connect = func(sig reqsign.Signer) Relay {
					side := connect(sig)
					if reqsign.IDOf(sig.IdentityKey()) == r.old.ID() {
						side.Gate = directory.Gate{Client: directory.NewHTTP(server.URL, sign, server.Client()), Store: blobstore.NewHTTP(server.URL, sign, server.Client()), Source: r.old.ID()}
					} else {
						side.Store = storageHandoffWrites{Store: side.Store, before: func() {
							writes++
							if handoffs < 2 || refuse {
								t.Error("upload before successful handoff")
							}
						}}
					}
					return side
				}
				r.env.StorageAllowed = func(context.Context, blobstore.Store) (bool, error) { return state == "on", nil }
				if resume {
					r.env.After = crashAt(string(Frozen))
					if _, err := r.env.Rotate(t.Context()); !errors.Is(err, errCrash) {
						t.Fatalf("freeze crash: %v", err)
					}
					if freezes != 1 || handoffs != 0 {
						t.Fatalf("freeze=%d handoff=%d", freezes, handoffs)
					}
					r.env.After = nil
				}
				if _, err := r.env.Rotate(t.Context()); err == nil {
					t.Fatal("handoff refusal was ignored")
				}
				if freezes != 1 || handoffs != 1 || writes != 0 {
					t.Fatalf("before retry freeze=%d handoff=%d writes=%d", freezes, handoffs, writes)
				}
				refuse = false
				if _, err := r.env.Rotate(t.Context()); err != nil {
					t.Fatal(err)
				}
				if freezes != 1 || handoffs != 2 || Pending(r.home) {
					t.Fatalf("freeze=%d handoff=%d pending=%v", freezes, handoffs, Pending(r.home))
				}
				if (state == "on" && writes == 0) || (state == "off" && writes != 0) {
					t.Fatalf("storage=%s writes=%d", state, writes)
				}
			})
		}
	}
}

type storageHandoffWrites struct {
	blobstore.Store
	before func()
}

func (s storageHandoffWrites) PutFrame(ctx context.Context, b []byte) (blobstore.FrameID, error) {
	s.before()
	return s.Store.PutFrame(ctx, b)
}
