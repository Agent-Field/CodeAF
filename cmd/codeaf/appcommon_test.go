package main

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/data"
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
	crm, err := apps.Promote(draft, "Keep the CRM")
	if err != nil {
		t.Fatal(err)
	}
	cli := func(args []string, out any) {
		t.Helper()
		// The complete kit is larger than a pipe buffer.
		file, err := os.CreateTemp(root, "parts-*.json")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		prev := os.Stdout
		os.Stdout = file
		defer func() { os.Stdout = prev }()
		if err := runApp(args); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(file.Name())
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatal(err)
		}
	}
	var cliKit map[string]any
	var cliLib schema.CommonTypeLibrary
	cli([]string{"parts"}, &cliKit)
	cli([]string{"common-types"}, &cliLib)
	var fromCLI appwire.BindCommonTypeResult
	cli([]string{"--home", root, "bind-common-type", `{"common":"person","type":"contact"}`}, &fromCLI)
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
	var wireKit map[string]any
	wire.must("apps.parts", appwire.Empty{}, &wireKit)
	if !reflect.DeepEqual(wireKit, cliKit) || !reflect.DeepEqual(lib, cliLib) {
		t.Fatal("the CLI and wire served different catalogs")
	}
	var before appwire.DataTypesResult
	wire.must("data.types", appwire.DataScope{}, &before)
	var contactID string
	for _, typ := range before.Types {
		if typ.Name == "contact" {
			if typ.Common != "person" {
				t.Fatalf("CLI binding was not persisted: %+v", typ)
			}
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
	if fromCLI != receipt {
		t.Fatalf("CLI and wire binding receipts differ: %+v, %+v", fromCLI, receipt)
	}
	stopCommands, err := backend.serveAppCommands()
	if err != nil {
		t.Fatal(err)
	}
	defer stopCommands()
	var fromLiveCLI appwire.BindCommonTypeResult
	cli([]string{"bind-common-type", `{"common":"person","type":"contact"}`}, &fromLiveCLI)
	if fromLiveCLI != receipt {
		t.Fatalf("live CLI binding receipt: %+v", fromLiveCLI)
	}
	wire.mustFail("apps.bind_common_type", appwire.BindCommonTypeParams{Common: "person", Type: "company"}, appwire.CodeBadParams)
	wire.mustFail("apps.bind_common_type", appwire.BindCommonTypeParams{Common: "invented", Type: "contact"}, appwire.CodeBadParams)

	records, err := backend.dataStore("")
	if err != nil {
		t.Fatal(err)
	}
	support := data.AppPublication{App: "support", Version: 1, Metadata: []byte(`{}`),
		Definition: []byte(`{"id":"support","records":{"customer":{"uses":true,"common":"person","fields":{}}},
			"capabilities":{"records":{"customer":"rw"}}}`)}
	if err := records.PublishApp(support, 0); err != nil {
		t.Fatal(err)
	}
	var created appwire.DataWriteResult
	wire.must("data.create", appwire.DataCreateParams{
		DataScope: appwire.DataScope{App: support.App}, Type: "customer",
		Set: map[string]any{"name": "Shared customer"},
	}, &created)
	if len(created.Rows) != 1 {
		t.Fatalf("create through the Support alias returned %+v", created.Rows)
	}
	id, _ := created.Rows[0]["id"].(string)
	var read appwire.DataRowResult
	wire.must("data.get", appwire.DataRecordParams{
		DataScope: appwire.DataScope{App: crm.ID}, Type: "contact", ID: id,
	}, &read)
	if id == "" || read.Row["id"] != id || read.Row["name"] != "Shared customer" {
		t.Fatalf("CRM did not read the same record: %+v", read.Row)
	}
	catalog, err := backend.appBuildCatalog(support.App)
	if err != nil {
		t.Fatal(err)
	}
	offer, ok := catalog.Types.OfferCommonType("customer", schema.RecordType{})
	if !ok || offer.Existing != "contact" || catalog.AppNames[crm.ID] != crm.Name || catalog.Counts["contact"] != 1 {
		t.Fatalf("reuse offer %+v, app labels %v, counts %v", offer, catalog.AppNames, catalog.Counts)
	}
}
