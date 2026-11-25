package main

import (
	"os"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/home"
)

func TestCommonTypeWireAdoptsExistingType(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	apps := store.New()
	draft, err := apps.NewDraft("")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(draft.Dir(), os.DirFS("../../internal/apps/evals/testdata/crm")); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.Promote(draft, "Keep the CRM"); err != nil {
		t.Fatal(err)
	}
	backend := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { backend.Close() })
	wire := wireOver(t, backend)

	var kit schema.Kit
	var lib schema.CommonTypeLibrary
	wire.must("apps.kit", appwire.Empty{}, &kit)
	wire.must("apps.common_types", appwire.Empty{}, &lib)
	if kit.CommonTypes.Version != lib.Version || lib.Types["person"].Fields["email"].Role != "email" {
		t.Fatalf("wire catalog: %+v", lib)
	}
	var before appwire.DataTypesResult
	wire.must("data.types", appwire.DataScope{}, &before)
	var contactID string
	for _, typ := range before.Types {
		if typ.Name == "contact" {
			contactID = typ.ID
		}
	}
	if contactID == "" {
		t.Fatal("the CRM has no contact type")
	}
	p := appwire.BindCommonTypeParams{Common: "person", Type: "contact"}
	var receipt appwire.BindCommonTypeResult
	wire.must("apps.bind_common_type", p, &receipt)
	if receipt.TypeID != contactID || receipt.Type != p.Type || receipt.Common != p.Common {
		t.Fatalf("binding receipt: %+v", receipt)
	}
	wire.must("apps.bind_common_type", p, &receipt)
	wire.mustFail("apps.bind_common_type", appwire.BindCommonTypeParams{Common: "person", Type: "company"}, appwire.CodeBadParams)
	wire.mustFail("apps.bind_common_type", appwire.BindCommonTypeParams{Common: "invented", Type: "contact"}, appwire.CodeBadParams)
}
