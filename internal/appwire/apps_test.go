package appwire

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/store"
)

type definitionEngine struct {
	*fakeEngine
	apps *store.Store
}

func (e definitionEngine) AppDefinitions() *store.Store { return e.apps }

func TestAppPartsReturnsOneDescriptorFromTheServedKit(t *testing.T) {
	fake := &fakeEngine{}
	w := startWireOver(t, definitionEngine{fake, store.NewAt(t.TempDir(), nil)}, fake)
	w.next()
	w.send(`{"id":1,"method":"apps.kit","params":{}}`)
	kit := w.next()["result"].(map[string]any)
	w.send(`{"id":2,"method":"apps.parts","params":{"for":"Button"}}`)
	reply := w.next()
	if reply["error"] != nil {
		t.Fatalf("parts: %v", reply["error"])
	}
	found := reply["result"].(map[string]any)
	if found["kit"] != kit["kit"] || found["parts"] != nil || found["part"].(map[string]any)["name"] != "Button" {
		t.Fatalf("expected one descriptor from the served kit, got %v", found)
	}
}

func TestAppsMethodsServeDefinitions(t *testing.T) {
	fake := &fakeEngine{}
	apps := store.NewAt(t.TempDir(), nil)
	w := startWireOver(t, definitionEngine{fake, apps}, fake)
	w.next()
	call := func(method, params string) map[string]any {
		t.Helper()
		w.send(fmt.Sprintf(`{"id":1,"method":%q,"params":%s}`, method, params))
		reply := w.next()
		if err := reply["error"]; err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		return reply["result"].(map[string]any)
	}
	if got := call("apps.list", "{}")["apps"].([]any); len(got) != 0 {
		t.Fatalf("empty list = %v", got)
	}
	draft, err := apps.NewDraft("")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(draft.Dir(), "screens"), 0o700); err != nil {
		t.Fatal(err)
	}
	for file, contents := range map[string]string{
		"app.json":           `{"id":"crm","name":"CRM","version":1,"kit":0,"agent":{"name":"CRM agent","file":"agent.md"},"records":{"contact":{"title":"name","fields":{"name":{"type":"text"}}}},"capabilities":{"records":{"contact":"rw"}}}`,
		"plan.json":          `{"ask":"Contacts","data":[{"id":"contact","line":"Contact names"}],"screens":[{"id":"deals","line":"Contacts"}],"agent":{"line":"Manage contacts"}}`,
		"screens/deals.json": `{"screen":"deals","title":"Contacts","body":{"part":"Stack"}}`,
		"agent.md":           "Manage contacts.",
	} {
		if err := os.WriteFile(filepath.Join(draft.Dir(), file), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	meta, err := apps.Promote(draft, "Keep contacts")
	if err != nil {
		t.Fatal(err)
	}
	params := fmt.Sprintf(`{"app":%q}`, meta.ID)
	if got := call("apps.list", "{}")["apps"].([]any)[0].(map[string]any); got["id"] != meta.ID || got["status"] != "active" {
		t.Fatalf("list = %v", got)
	}
	if got := call("apps.get", params); got["agent_md"] != "Manage contacts." || got["app"].(map[string]any)["id"] != meta.ID || got["screens"].([]any)[0] != "deals" {
		t.Fatalf("get = %v", got)
	}
	if got := call("apps.screen", fmt.Sprintf(`{"app":%q,"screen":"deals"}`, meta.ID)); got["screen"].(map[string]any)["body"].(map[string]any)["part"] != "Stack" {
		t.Fatalf("screen = %v", got)
	}
	if got := call("apps.versions", params)["versions"].([]any)[0].(map[string]any); got["request"] != "Keep contacts" {
		t.Fatalf("versions = %v", got)
	}
	if got := call("apps.revert", fmt.Sprintf(`{"app":%q,"n":1}`, meta.ID))["meta"].(map[string]any); got["version"] != float64(2) {
		t.Fatalf("revert = %v", got)
	}
	if got := call("apps.kit", "{}"); got["kit"] != float64(2) || len(got["parts"].([]any)) != 64 {
		t.Fatalf("kit = %v", got)
	}
	if got := call("apps.kit", `{"kit":0}`); got["kit"] != float64(0) || len(got["parts"].([]any)) != 20 {
		t.Fatalf("legacy kit = %v", got)
	}
	if got := call("apps.archive", params)["meta"].(map[string]any); got["archived"] != true {
		t.Fatalf("archive = %v", got)
	}
	if got := call("apps.delete", params); len(got) != 0 {
		t.Fatalf("delete = %v", got)
	}
	w.send(fmt.Sprintf(`{"id":2,"method":"apps.get","params":%s}`, params))
	if got := w.next()["error"].(map[string]any)["code"]; got != CodeNotFound {
		t.Fatalf("after delete = %v", got)
	}
	w.send(`{"id":3,"method":"apps.screen","params":{"app":"../other","screen":"../secret"}}`)
	if got := w.next()["error"].(map[string]any)["code"]; got != CodeBadParams {
		t.Fatalf("invalid path = %v", got)
	}
}

func TestAppsContentReadServesPublishedPlanFiles(t *testing.T) {
	fake := &fakeEngine{}
	apps := store.NewAt(t.TempDir(), nil)
	draft, err := apps.NewDraft("")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(draft.Dir(), os.DirFS("../apps/schema/testdata/line-handover")); err != nil {
		t.Fatal(err)
	}
	meta, err := apps.Promote(draft, "Keep the shift plan")
	if err != nil {
		t.Fatal(err)
	}
	w := startWireOver(t, definitionEngine{fake, apps}, fake)
	w.next()
	call := func(ref string) map[string]any {
		w.send(fmt.Sprintf(`{"id":1,"method":"apps.content.read","params":{"app":%q,"ref":%q}}`, meta.ID, ref))
		return w.next()
	}
	raw, err := os.ReadFile("../apps/schema/testdata/line-handover/plan/shifts.json")
	if err != nil {
		t.Fatal(err)
	}
	if got := call("plan/shifts.json"); got["error"] != nil || got["result"].(map[string]any)["text"] != string(raw) {
		t.Fatalf("read plan: %v", got)
	}
	for _, ref := range []string{"app.json", "plan/../agent.md", "plan/missing.json"} {
		if got := call(ref); got["error"].(map[string]any)["code"] != CodeNotFound {
			t.Fatalf("read %q: %v", ref, got)
		}
	}
}
