package appapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/orchestrate"
)

func settingsServer(t *testing.T) *Server {
	t.Helper()
	keys := config.NewAppCredentials()
	keys.Set("openrouter", "memory-only-test-key")
	s := New(testToken, Options{Credentials: keys, ProfileDir: t.TempDir(), Context: t.Context(), ModelCatalog: func(context.Context) ([]CatalogModel, error) {
		return []CatalogModel{{ID: "vendor/z-first", Name: "First returned", Provider: "openrouter", Available: true, Efforts: []string{"low", "medium", "high"}}, {ID: "vendor/a-second", Name: "Second returned", Provider: "openrouter", Available: true, Efforts: []string{"low", "medium", "high"}}, {ID: "vendor/a-second:free", Name: "Second returned", Provider: "openrouter", Available: true, Efforts: []string{"low", "medium", "high"}}}, nil
	}})
	t.Cleanup(s.Close)
	return s
}

func TestModelsReportTheResolvedConversationModel(t *testing.T) {
	for _, tc := range []struct {
		name  string
		talk  string
		reset bool
		want  string
	}{
		{name: "fresh profile", want: config.DefaultModel},
		{name: "saved talk model", talk: "vendor/z-first", want: "vendor/z-first"},
		{name: "cleared talk model", talk: "vendor/z-first", reset: true, want: config.DefaultModel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := settingsServer(t)
			if tc.talk != "" {
				if err := config.WriteChatModel(s.options.ProfileDir, tc.talk); err != nil {
					t.Fatal(err)
				}
			}
			wantDefault := tc.talk
			if tc.reset {
				if err := config.WriteChatModel(s.options.ProfileDir, ""); err != nil {
					t.Fatal(err)
				}
				wantDefault = ""
			}
			w := request(t, s, "GET", "/app/v1/models", "", bearer(), "127.0.0.1:4000")
			var models ModelsResponse
			if err := json.Unmarshal(w.Body.Bytes(), &models); err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 || models.Resolved != tc.want || models.Default != wantDefault {
				t.Fatalf("models: %d %s; want resolved %q and default %q", w.Code, w.Body, tc.want, wantDefault)
			}
		})
	}
}

func TestModelsReportKnownTariffsAndOmitUnknownPrices(t *testing.T) {
	orchestrate.UsePrices(orchestrate.CatalogPrices(func(id string) (float64, float64, bool) {
		switch id {
		case "vendor/z-first":
			return .000002, .000008, true
		case "vendor/a-second":
			return 0, 0, true
		default:
			return 0, 0, false
		}
	}))
	t.Cleanup(func() { orchestrate.UsePrices(nil) })
	s := settingsServer(t)
	if err := config.WriteChatModel(s.options.ProfileDir, "unknown/model"); err != nil {
		t.Fatal(err)
	}
	w := request(t, s, "GET", "/app/v1/models", "", bearer(), "127.0.0.1:4000")
	var response struct {
		Models []map[string]json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(response.Models) != 3 {
		t.Fatalf("models: %d %s", w.Code, w.Body)
	}
	for _, tc := range []struct {
		id    string
		known bool
		in    float64
		out   float64
	}{
		{id: "vendor/z-first", known: true, in: 2, out: 8},
		{id: "vendor/a-second", known: true},
		{id: "unknown/model"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			for _, row := range response.Models {
				var id string
				if err := json.Unmarshal(row["id"], &id); err != nil {
					t.Fatal(err)
				}
				if id != tc.id {
					continue
				}
				for _, field := range []struct {
					key  string
					want float64
				}{{"price_in", tc.in}, {"price_out", tc.out}} {
					raw, present := row[field.key]
					if present != tc.known {
						t.Fatalf("%s present = %v, want %v", field.key, present, tc.known)
					}
					if !present {
						continue
					}
					var price float64
					if err := json.Unmarshal(raw, &price); err != nil || price != field.want {
						t.Fatalf("%s = %s, want %g: %v", field.key, raw, field.want, err)
					}
				}
				return
			}
			t.Fatal("model row missing")
		})
	}
}

func TestSettingsModelRouteTableUsesTheTransportGuard(t *testing.T) {
	s := settingsServer(t)
	paths := map[string][]string{
		"/app/v1/models": {"GET"}, "/app/v1/models/default": {"PUT"}, "/app/v1/models/pinned": {"PUT"},
		"/app/v1/models/roles": {"GET"}, "/app/v1/models/roles/checks": {"PUT"}, "/app/v1/settings": {"GET", "PATCH"},
		"/app/v1/setup": {"GET", "POST"}, "/app/v1/providers/openrouter/sign-in": {"GET", "POST", "DELETE"}, "/app/v1/providers/openrouter/sign-in/claim": {"POST"},
	}
	for path, allowed := range paths {
		t.Run(path, func(t *testing.T) {
			for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"} {
				w := request(t, s, method, path, `{}`, nil, "127.0.0.1:4000")
				if w.Code != 401 {
					t.Fatalf("unguarded %s: %d", method, w.Code)
				}
				w = request(t, s, method, path, `{}`, bearer(), "evil.example:4000")
				if w.Code != 421 {
					t.Fatal("host guard bypassed")
				}
				known := false
				for _, accept := range allowed {
					if method == accept {
						known = true
					}
				}
				if known {
					continue
				}
				w = request(t, s, method, path, `{}`, bearer(), "127.0.0.1:4000")
				if w.Code != 405 || w.Header().Get("Allow") != strings.Join(allowed, ", ") {
					t.Fatalf("method table %s: %d %s", method, w.Code, w.Header().Get("Allow"))
				}
			}
		})
	}
	missing := New(testToken, Options{})
	for path := range paths {
		w := request(t, missing, "GET", path, "", bearer(), "127.0.0.1:4000")
		if w.Code != 404 {
			t.Fatalf("missing profile read %s", path)
		}
	}
}

func TestModelsAndRolesHaveOnePersistedChoiceAndOnlyThreeEfforts(t *testing.T) {
	s := settingsServer(t)
	writes := map[string]string{
		"/app/v1/models/default":       `{"model":"vendor/z-first","effort":"medium"}`,
		"/app/v1/models/pinned":        `{"models":["vendor/z-first","vendor/a-second:free",""]}`,
		"/app/v1/models/roles/checks":  `{"model":"vendor/a-second:free","effort":"high"}`,
		"/app/v1/models/roles/planner": `{"model":"vendor/z-first","effort":"low"}`,
	}
	for path, body := range writes {
		w := request(t, s, "PUT", path, body, bearer(), "127.0.0.1:4000")
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
	}
	w := request(t, s, "GET", "/app/v1/models", "", bearer(), "127.0.0.1:4000")
	var models ModelsView
	if json.Unmarshal(w.Body.Bytes(), &models) != nil || models.Default != "vendor/z-first" || len(models.Pinned) != 3 || models.Pinned[2] != "" || len(models.Models) != 2 {
		t.Fatalf("models: %s", w.Body)
	}
	w = request(t, s, "GET", "/app/v1/models/roles", "", bearer(), "127.0.0.1:4000")
	var roles struct {
		Roles []RoleView `json:"roles"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &roles)
	if len(roles.Roles) != 2 || roles.Roles[0].ID != "checks" || roles.Roles[1].ID != "planner" {
		t.Fatalf("roles: %s", w.Body)
	}
	if config.ChatModelAt(s.options.ProfileDir) != "vendor/z-first" {
		t.Fatal("default was not the terminal's native row")
	}
	restarted := New(testToken, s.options)
	defer restarted.Close()
	w = request(t, restarted, "GET", "/app/v1/models/roles", "", bearer(), "127.0.0.1:4000")
	if !strings.Contains(w.Body.String(), "vendor/a-second:free") {
		t.Fatal("role lost across restart")
	}
	for _, body := range []string{`{"model":"invented/model"}`, `{"model":"vendor/z-first","effort":"max"}`, `{"model":"vendor/z-first","effort":"xhigh"}`, `{"model":"vendor/z-first","effort":"auto"}`} {
		w = request(t, s, "PUT", "/app/v1/models/default", body, bearer(), "127.0.0.1:4000")
		if w.Code != 422 {
			t.Fatalf("accepted %s: %d", body, w.Code)
		}
	}
	w = request(t, s, "PUT", "/app/v1/models/roles/title", `{"model":"vendor/z-first","effort":"medium"}`, bearer(), "127.0.0.1:4000")
	if w.Code != 404 {
		t.Fatal("exposed an extra app role")
	}
	w = request(t, s, "PUT", "/app/v1/models/roles/checks", `{"model":"","effort":"high"}`, bearer(), "127.0.0.1:4000")
	if w.Code != 200 {
		t.Fatal("could not reset role to follow the agent")
	}
	var reset RoleView
	if json.Unmarshal(w.Body.Bytes(), &reset) != nil || reset.Model != "" || reset.Effort != "medium" {
		t.Fatalf("reset echoed the old effort instead of the saved fallback: %s", w.Body)
	}
}

func TestSettingsPatchIsAtomicConditionalAndPublishesSmallFacts(t *testing.T) {
	s := settingsServer(t)
	w := request(t, s, "GET", "/app/v1/settings", "", bearer(), "127.0.0.1:4000")
	version := strings.Trim(w.Header().Get("ETag"), `"`)
	if version == "" {
		t.Fatal("no settings version")
	}
	body := `{"models.effort":"high","advanced.proposal_s":0,"accounts.mcp:calendar":"off","expect":{"version":"` + version + `"}}`
	w = request(t, s, "PATCH", "/app/v1/settings", body, bearer(), "127.0.0.1:4000")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"advanced.proposal_s":0`) {
		t.Fatalf("patch: %d %s", w.Code, w.Body)
	}
	batch, _, _, _, _ := s.events.read(0)
	if len(batch) != 1 || batch[0].kind != EventSettingsChanged || strings.Contains(string(batch[0].data), "memory-only") {
		t.Fatal("settings event was missing or held a secret")
	}
	w = request(t, s, "PATCH", "/app/v1/settings", body, bearer(), "127.0.0.1:4000")
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"reason":"stale"`) {
		t.Fatalf("stale patch: %d %s", w.Code, w.Body)
	}
	w = request(t, s, "PATCH", "/app/v1/settings", `{"models.effort":"low","theme":"dark"}`, bearer(), "127.0.0.1:4000")
	if w.Code != 422 {
		t.Fatal("accepted UI state")
	}
	values, _, _ := config.AppSettingsAt(s.options.ProfileDir)
	if values["models.effort"] != "high" {
		t.Fatal("invalid patch partly saved")
	}
	raw, _ := os.ReadFile(config.BudgetConfigPath(s.options.ProfileDir))
	if strings.Contains(string(raw), "memory-only-test-key") {
		t.Fatal("settings wrote a memory key")
	}
}

func TestModelListingReusesKeyCheckAndQualifiesDirectProviders(t *testing.T) {
	calls := 0
	s, keys := keyServer(t, func(r *http.Request) (*http.Response, error) {
		calls++
		return keyAnswer(200, `{"data":[{"id":"z-first"},{"id":"a-second"}]}`), nil
	})
	defer s.Close()
	restore := config.UseAppCredentials(keys)
	defer restore()
	for _, id := range []string{"openrouter", "anthropic"} {
		w := request(t, s, "PUT", "/app/v1/keys/"+id, `{"key":"key-for-`+id+`"}`, bearer(), "127.0.0.1:4000")
		if w.Code != 200 {
			t.Fatal(w.Body)
		}
	}
	w := request(t, s, "GET", "/app/v1/models", "", bearer(), "127.0.0.1:4000")
	if calls != 2 || !strings.Contains(w.Body.String(), `"id":"anthropic/z-first"`) {
		t.Fatalf("listing: %d calls %s", calls, w.Body)
	}
	var view ModelsView
	_ = json.Unmarshal(w.Body.Bytes(), &view)
	for _, row := range view.Models {
		if row.Provider == "anthropic" && row.Available {
			t.Fatal("listed a provider without an engine transport as available")
		}
	}

	keys.Set("deepseek", "stdin-key")
	w = request(t, s, "GET", "/app/v1/models", "", bearer(), "127.0.0.1:4000")
	if calls != 3 || !strings.Contains(w.Body.String(), "deepseek/z-first") {
		t.Fatal("launch key did not discover its catalog")
	}
	_ = request(t, s, "DELETE", "/app/v1/keys/anthropic", "", bearer(), "127.0.0.1:4000")
	w = request(t, s, "GET", "/app/v1/models", "", bearer(), "127.0.0.1:4000")
	if strings.Contains(w.Body.String(), "anthropic/z-first") {
		t.Fatal("removed provider remained available")
	}
}

func TestMalformedMutationsCannotChangeSettings(t *testing.T) {
	s := settingsServer(t)
	paths := map[string]string{"/app/v1/settings": "PATCH", "/app/v1/setup": "POST", "/app/v1/models/default": "PUT", "/app/v1/models/pinned": "PUT", "/app/v1/models/roles/checks": "PUT", "/app/v1/providers/openrouter/sign-in": "POST", "/app/v1/providers/openrouter/sign-in/claim": "POST"}
	for path, method := range paths {
		for _, body := range []string{"", "null", "[]", "invalid", `{} {}`, `{"unknown":true}`, `{"x":"` + strings.Repeat("x", 32<<10) + `"}`} {
			w := request(t, s, method, path, body, bearer(), "127.0.0.1:4000")
			if w.Code != 422 {
				t.Fatalf("%s accepted %q: %d", path, body[:min(len(body), 30)], w.Code)
			}
		}
	}
}

func TestNewAgentsUseSavedSettingsAndExplicitCreationValuesWin(t *testing.T) {
	f := newAgentFixture(t)
	f.server = New(testToken, Options{Agents: f.registry, ProfileDir: f.home, ModelCatalog: func(context.Context) ([]CatalogModel, error) {
		return []CatalogModel{{ID: "vendor/default", Available: true}}, nil
	}})
	t.Cleanup(f.server.Close)
	w := f.request(t, "PATCH", "/app/v1/settings", `{"models.default":"vendor/default","models.effort":"high","spending.per_agent":{"usd":"8.00"},"permissions.mode":"ask_everything"}`)
	if w.Code != 200 {
		t.Fatalf("saved defaults: %d %s", w.Code, w.Body)
	}
	created := readAgent(t, f.request(t, "POST", "/app/v1/agents", `{"start":"empty"}`), 201)
	if created.Model != "vendor/default" || created.Effort != "high" || created.Budget.USD != "8.00" || f.opens[0].Approvals != "everything" {
		t.Fatalf("saved defaults were not applied: %+v %+v", created, f.opens[0])
	}
	explicit := readAgent(t, f.request(t, "POST", "/app/v1/agents", `{"start":"empty","model":"test/explicit","effort":"low","budget":{"usd":"3.00"},"approvals":"risky"}`), 201)
	if explicit.Model != "test/explicit" || explicit.Effort != "low" || explicit.Budget.USD != "3.00" || f.opens[1].Approvals != "risky" {
		t.Fatalf("explicit choice lost: %+v", explicit)
	}
	old := readAgent(t, f.request(t, "GET", "/app/v1/agents/"+created.ID, ""), 200)
	if old.Effort != "high" || old.Budget.USD != "8.00" {
		t.Fatal("new defaults changed an existing agent")
	}
}

func TestAppAgentReadNeverShowsSavedTerminalOnlyEffortRungs(t *testing.T) {
	f := newAgentFixture(t)
	created := readAgent(t, f.request(t, "POST", "/app/v1/agents", `{"start":"empty"}`), 201)
	for _, level := range []string{"xhigh", "max", "auto"} {
		err := f.registry.disk(func(d *agentDisk) error {
			record := d.Records[created.ID]
			record.Agent.Effort = level
			d.Records[created.ID] = record
			return nil
		}, true)
		if err != nil {
			t.Fatal(err)
		}
		read := readAgent(t, f.request(t, "GET", "/app/v1/agents/"+created.ID, ""), 200)
		want := "high"
		if level == "auto" {
			want = "medium"
		}
		if read.Effort != want {
			t.Fatalf("app showed %s as %s", level, read.Effort)
		}
		mutated := readAgent(t, f.request(t, "PATCH", "/app/v1/agents/"+created.ID, `{"pinned":true}`), 200)
		if mutated.Effort != want {
			t.Fatalf("mutation showed terminal rung %s", mutated.Effort)
		}

		err = f.registry.disk(func(d *agentDisk) error {
			if d.Records[created.ID].Agent.Effort != level {
				t.Error("reading rewrote the terminal choice")
			}
			return nil
		}, false)
		if err != nil {
			t.Fatal(err)
		}
	}
}
