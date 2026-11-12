package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/clipboard"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestLinkCreateDoesNotCopyIntoAChangedCaller(t *testing.T) {
	for _, change := range []string{"replaced", "signed_out"} {
		t.Run(change, func(t *testing.T) {
			f := newLinkFactory(t)
			f.post = func(w http.ResponseWriter, r *http.Request) {
				if change == "replaced" {
					f.hub.hold(f.id, "replacement-caller-fixture-782dbf01", 0)
				} else {
					f.hub.dropSession(f.id, linkTestBearer)
				}
				f.mint(w, r)
			}
			out, err := f.create(appwire.FactoryLinkCreateParams{})
			if err == nil {
				t.Fatal("changed caller received the earlier caller's invite receipt")
			}
			if f.clip.count() != 0 {
				t.Fatal("earlier caller's invite reached the changed caller's clipboard")
			}
			if out.Entry.ID != "" || out.Ref != "" {
				t.Fatal("earlier caller's scoped metadata escaped")
			}
			if f.posts() != 1 || f.deletes() != 0 {
				t.Fatal("caller change repeated or cleaned up the mint")
			}
			f.noSecretAnywhere()
		})
	}
}

func TestLinkRetryDoesNotCopyAfterCallerChangesDuringList(t *testing.T) {
	f := newLinkFactory(t)
	f.clip.set(clipboard.ErrFailed)
	first, err := f.create(appwire.FactoryLinkCreateParams{})
	if err != nil || first.Ref == "" {
		t.Fatal("expected a held invitation")
	}
	before := f.clip.count()
	f.clip.set(nil)
	f.listH = func(w http.ResponseWriter, r *http.Request) {
		f.hub.hold(f.id, "replacement-copy-caller-fixture-d93acf50", 0)
		f.mu.Lock()
		rows := append([]api.AccessLink(nil), f.rows...)
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"links": rows})
	}
	out, err := f.copyAgain(first.Ref)
	if err == nil {
		t.Fatal("changed caller received the held invite receipt")
	}
	if f.clip.count() != before {
		t.Fatal("stale held secret was copied after its scope was cleared")
	}
	if out.Entry.ID != "" || out.Ref != "" {
		t.Fatal("old held metadata escaped to a changed caller")
	}
	if f.posts() != 1 || f.deletes() != 0 {
		t.Fatal("retry minted or deleted an invite")
	}
	f.noSecretAnywhere()
}

func TestLinkListDoesNotReturnEarlierCallerMetadataAfterSwap(t *testing.T) {
	f := newLinkFactory(t)
	f.listH = func(w http.ResponseWriter, r *http.Request) {
		f.hub.hold(f.id, "replacement-list-caller-fixture-042f7c8a", 0)
		_ = json.NewEncoder(w).Encode(map[string]any{"links": []api.AccessLink{{ID: "tok_parentPrivate", Name: "Earlier caller private invitation", Status: "open"}}})
	}
	out, err := f.list()
	if err == nil || len(out.Items) != 0 {
		t.Fatal("list returned the previous caller's scoped metadata")
	}
	f.noSecretAnywhere()
}
