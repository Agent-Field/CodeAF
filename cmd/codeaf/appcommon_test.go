package main

import (
	"encoding/json"
	"os"
	"reflect"
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
	var kit schema.Kit
	var lib schema.CommonTypeLibrary
	wire.must("apps.kit", appwire.Empty{}, &kit)
	wire.must("apps.common_types", appwire.Empty{}, &lib)
	if kit.CommonTypes.Version != lib.Version || lib.Types["person"].Fields["email"].Role != "email" {
		t.Fatalf("wire catalog: %+v", lib)
	}
	var cliKit map[string]any
	var cliLib schema.CommonTypeLibrary
	cli([]string{"parts"}, &cliKit)
	cli([]string{"common-types"}, &cliLib)
	var wireKit map[string]any
	wire.must("apps.kit", appwire.Empty{}, &wireKit)
	if !reflect.DeepEqual(wireKit, cliKit) || !reflect.DeepEqual(lib, cliLib) {
		t.Fatal("the CLI and wire served different catalogs")
	}
	closeSocket, err := backend.serveAppCommands()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeSocket)
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
	var fromCLI appwire.BindCommonTypeResult
	cli([]string{"bind-common-type", `{"common":"person","type":"contact"}`}, &fromCLI)
	if fromCLI != receipt {
		t.Fatalf("CLI and wire binding receipts differ: %+v, %+v", fromCLI, receipt)
	}
	wire.mustFail("apps.bind_common_type", appwire.BindCommonTypeParams{Common: "person", Type: "company"}, appwire.CodeBadParams)
	wire.mustFail("apps.bind_common_type", appwire.BindCommonTypeParams{Common: "invented", Type: "contact"}, appwire.CodeBadParams)
}
