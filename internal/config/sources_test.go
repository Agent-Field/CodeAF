package config

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/modelsource/sourcestub"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/trace"
)

func vendoredSource(t *testing.T, id string) modelsource.Source {
	t.Helper()
	for _, source := range modelsource.Vendored() {
		if source.ID == id {
			return source
		}
	}
	t.Fatalf("vendored source %q was not found", id)
	return modelsource.Source{}
}

func TestSourcePersistenceSharesTheProfileAndKeepsItPrivate(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAPIKey(dir, "sk-default-1234567890"); err != nil {
		t.Fatal(err)
	}
	rows := []PersistedSource{
		{ID: "deepseek", Written: "deepseek-direct", Address: "https://ignored.example", Key: "sk-direct-1234567890", Order: 2},
		{ID: "z-ai", Written: "z-ai", Region: "intl", Key: "must-not-survive", KeyEnv: "MY_ZAI_KEY", Order: 1},
	}
	if err := WriteSources(dir, rows); err != nil {
		t.Fatal(err)
	}
	stored := PersistedSources(dir)
	if len(stored) != 2 || stored[0].Address != "" || stored[1].Key != "" {
		t.Fatalf("stored rows = %+v", stored)
	}
	if PersistedAPIKey(dir) != "sk-default-1234567890" {
		t.Fatal("writing services replaced the default key")
	}
	info, err := os.Stat(BudgetConfigPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("profile mode = %o, want 600", info.Mode().Perm())
	}
}

func TestSourceKeyPrecedenceAndUnknownRows(t *testing.T) {
	dir := t.TempDir()
	source := vendoredSource(t, "z-ai")
	row := PersistedSource{ID: source.ID, Written: source.Written, Key: "stored", KeyEnv: "MY_ZAI_KEY", Region: "intl", Order: 1}
	t.Setenv(source.KeyEnv, "conventional")
	t.Setenv(row.KeyEnv, "named")
	if got := SourceKeyAt(dir, row, source); got != "conventional" {
		t.Fatalf("conventional env = %q", got)
	}
	t.Setenv(source.KeyEnv, "")
	if got := SourceKeyAt(dir, row, source); got != "stored" {
		t.Fatalf("stored key = %q", got)
	}
	row.Key = ""
	if got := SourceKeyAt(dir, row, source); got != "named" {
		t.Fatalf("named env = %q", got)
	}

	if err := WriteSources(dir, []PersistedSource{row, {ID: "future", Written: "future", Order: 2}}); err != nil {
		t.Fatal(err)
	}
	resolved := ResolveSources(dir, "default-key", "https://router.example/v1")
	if len(resolved.All()) != 2 {
		t.Fatalf("resolved services = %+v", resolved.All())
	}
	if direct, ok := resolved.ByID("z-ai"); !ok || direct.Address != "https://api.z.ai/api/paas/v4" || direct.Key != "named" {
		t.Fatalf("resolved z-ai = %+v, found %t", direct, ok)
	}
}

func TestABadKeyIsRefusedInTheVendorsOwnWordsAndNothingIsStored(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
	server := sourcestub.New("deepseek-chat")
	defer server.Close()
	source := vendoredSource(t, "deepseek")
	source.Address = server.URL()
	source.Probe.Timeout = 100 * time.Millisecond
	row := PersistedSource{ID: source.ID, Written: "deepseek-direct", Key: "sk-direct-1234567890", Order: 1}
	dir := t.TempDir()
	if err := WriteAPIKey(dir, "sk-default-1234567890"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(BudgetConfigPath(dir))
	if err != nil {
		t.Fatal(err)
	}

	server.Refuse(401, `{"error":{"message":"Authentication Fails, Your api key is invalid"}}`)
	outcome, err := ConnectService(context.Background(), dir, row, source, []string{"deepseek"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != modelsource.OutcomeRefused || outcome.VendorSaid != "Authentication Fails, Your api key is invalid" {
		t.Fatalf("refusal = %+v", outcome)
	}
	after, _ := os.ReadFile(BudgetConfigPath(dir))
	if string(after) != string(before) {
		t.Fatal("a refused key changed the profile")
	}

	server.Hang(200 * time.Millisecond)
	outcome, err = ConnectService(context.Background(), dir, row, source, []string{"deepseek"})
	if err != nil || outcome.Kind != modelsource.OutcomeUnanswered {
		t.Fatalf("timeout = %+v, %v", outcome, err)
	}
	after, _ = os.ReadFile(BudgetConfigPath(dir))
	if string(after) != string(before) {
		t.Fatal("an unanswered service changed the profile")
	}
}

func TestAListingProbeConnectsAndCountsModels(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
	server := sourcestub.New("one", "two")
	defer server.Close()
	source := vendoredSource(t, "deepseek")
	source.Address = server.URL()
	row := PersistedSource{ID: source.ID, Written: "deepseek-direct", Key: "sk-direct-1234567890", Order: 1}
	outcome, err := ConnectService(context.Background(), t.TempDir(), row, source, []string{"deepseek"})
	if err != nil || outcome.Kind != modelsource.OutcomeConnected || !outcome.Listed || outcome.Models != 2 {
		t.Fatalf("connect = %+v, %v", outcome, err)
	}
	requests := server.Requests()
	if len(requests) != 1 || requests[0].Bearer != "Bearer sk-direct-1234567890" {
		t.Fatalf("requests = %+v", requests)
	}
}

func TestAListingWinsEvenWhenTheBillableProbeCannotBePaid(t *testing.T) {
	t.Setenv("ZHIPU_API_KEY", "")
	server := sourcestub.New("glm-5.3", "glm-5.3-flash")
	defer server.Close()
	server.RefuseCompletion(http.StatusTooManyRequests, `{"code":"1113","message":"Insufficient balance or no resource package. Please recharge."}`)
	source := vendoredSource(t, "z-ai")
	source.Address = server.URL()
	source.Doors = nil
	row := PersistedSource{ID: source.ID, Written: source.Written, Key: "zai-key", Order: 1}
	dir := t.TempDir()
	outcome, err := ConnectService(context.Background(), dir, row, source, nil)
	if err != nil || outcome.Kind != modelsource.OutcomeConnected || !outcome.Listed || outcome.Models != 2 {
		t.Fatalf("connect = %+v, %v", outcome, err)
	}
	requests := server.Requests()
	if len(requests) != 1 || requests[0].Method != http.MethodGet || len(outcome.ModelIDs) != 2 {
		t.Fatalf("connection did not stop on the listing: outcome=%+v requests=%+v", outcome, requests)
	}
	rows := PersistedSources(dir)
	if len(rows) != 1 || rows[0].Listed == nil || !*rows[0].Listed {
		t.Fatalf("discovered listing was not persisted: %+v", rows)
	}
}

func TestAListinglessProbeConnectsWithoutInventingACount(t *testing.T) {
	t.Setenv("ZHIPU_API_KEY", "")
	server := sourcestub.New()
	defer server.Close()
	server.Listingless()
	source := vendoredSource(t, "z-ai")
	source.Address = server.URL()
	source.Doors = nil
	row := PersistedSource{ID: source.ID, Written: source.Written, Key: "zai-key", Order: 1}
	outcome, err := ConnectService(context.Background(), t.TempDir(), row, source, nil)
	if err != nil || outcome.Kind != modelsource.OutcomeConnected || outcome.Listed || outcome.Models != 0 {
		t.Fatalf("connect = %+v, %v", outcome, err)
	}
	requests := server.Requests()
	if len(requests) != 2 || requests[0].Method != http.MethodGet || requests[0].Path != "/v1/models" ||
		requests[1].Method != http.MethodPost || requests[1].Path != "/v1"+modelsource.ChatCompletionsPath ||
		!strings.Contains(string(requests[1].Body), `"max_tokens":1`) || !strings.Contains(string(requests[1].Body), `"model":"glm-5.3-flash"`) {
		t.Fatalf("requests = %+v", requests)
	}
}

func TestAPaymentRefusalAcceptsTheKeyAndStoresTheAccount(t *testing.T) {
	t.Setenv("ZHIPU_API_KEY", "")
	server := sourcestub.New()
	defer server.Close()
	server.Listingless()
	server.RefuseCompletion(http.StatusTooManyRequests, `{"code":"1113","message":"Insufficient balance or no resource package. Please recharge."}`)
	source := vendoredSource(t, "z-ai")
	source.Address = server.URL()
	source.Doors = nil
	dir := t.TempDir()
	outcome, err := ConnectService(context.Background(), dir, PersistedSource{
		ID: source.ID, Written: source.Written, Key: "zai-key", Order: 1,
	}, source, nil)
	if err != nil || outcome.Kind != modelsource.OutcomeAccountCannotPay ||
		outcome.VendorSaid != "Insufficient balance or no resource package. Please recharge." {
		t.Fatalf("payment refusal = %+v, %v", outcome, err)
	}
	rows := PersistedSources(dir)
	if len(rows) != 1 || rows[0].Key != "zai-key" || rows[0].Listed == nil || *rows[0].Listed {
		t.Fatalf("authenticated account was not stored as listing-less: %+v", rows)
	}
}

func twoDoorSource(t *testing.T, plan, metered *sourcestub.Server) modelsource.Source {
	t.Helper()
	source := vendoredSource(t, "z-ai")
	source.Doors = []modelsource.Door{
		{ID: "coding-plan", Name: "coding plan", Address: plan.URL(), Models: []string{"glm-5.3-flash"}},
		{ID: "metered", Name: "pay-as-you-go", Address: metered.URL(), Metered: true},
	}
	source.Probe.Timeout = 100 * time.Millisecond
	return source
}

func TestAPlanKeyBindsToThePlanDoor(t *testing.T) {
	t.Setenv("ZHIPU_API_KEY", "")
	plan, metered := sourcestub.New("too-wide"), sourcestub.New("metered")
	defer plan.Close()
	defer metered.Close()
	metered.RefuseCompletion(http.StatusTooManyRequests, `{"code":"1113","message":"empty"}`)
	dir := t.TempDir()
	outcome, err := ConnectService(context.Background(), dir, PersistedSource{
		ID: "z-ai", Written: "z-ai", Key: "plan-test-key", Order: 1,
	}, twoDoorSource(t, plan, metered), nil)
	if err != nil || outcome.Kind != modelsource.OutcomeConnected || outcome.Door.ID != "coding-plan" ||
		!reflect.DeepEqual(outcome.ModelIDs, []string{"glm-5.3-flash"}) {
		t.Fatalf("plan outcome = %+v, %v", outcome, err)
	}
	if len(plan.Requests()) != 1 || len(metered.Requests()) != 0 || plan.Requests()[0].Agent != provider.DirectUserAgent {
		t.Fatalf("requests: plan=%d metered=%d", len(plan.Requests()), len(metered.Requests()))
	}
	rows := PersistedSources(dir)
	if len(rows) != 1 || rows[0].Door != "coding-plan" {
		t.Fatalf("bound row = %+v", rows)
	}
}

func TestAnExhaustedWindowBindsThePlanAndDoesNotSpendAMeteredProbe(t *testing.T) {
	t.Setenv("ZHIPU_API_KEY", "")
	plan, metered := sourcestub.New("too-wide"), sourcestub.New("metered")
	defer plan.Close()
	defer metered.Close()
	plan.RefuseCompletion(http.StatusTooManyRequests, `{"code":"1316","message":"Usage limit reached for the past 5 hours. Your limit will reset at 18:30 UTC"}`)
	dir := t.TempDir()
	outcome, err := ConnectService(context.Background(), dir, PersistedSource{
		ID: "z-ai", Written: "z-ai", Key: "paused-plan-test-key", Order: 1,
	}, twoDoorSource(t, plan, metered), nil)
	if err != nil || outcome.Kind != modelsource.OutcomeConnected || outcome.Door.ID != "coding-plan" ||
		!outcome.PlanPaused || outcome.PlanReset != "18:30 UTC" || outcome.Overflow == nil || !outcome.Overflow.Metered {
		t.Fatalf("paused plan outcome = %+v, %v", outcome, err)
	}
	// A spent window proves the key authenticated and the plan exists. Treating
	// it as a refusal would buy a metered probe and persist that paid road while
	// the person's setting still says wait.
	if len(plan.Requests()) != 1 || len(metered.Requests()) != 0 {
		t.Fatalf("paused connection spent elsewhere: plan=%d metered=%d", len(plan.Requests()), len(metered.Requests()))
	}
	if rows := PersistedSources(dir); len(rows) != 1 || rows[0].Door != "coding-plan" || rows[0].PlanPaused != "" {
		t.Fatalf("paused plan binding = %+v", rows)
	}
}

func TestAKeyWithNoPlanFallsBackToPayAsYouGo(t *testing.T) {
	t.Setenv("ZHIPU_API_KEY", "")
	plan, metered := sourcestub.New(), sourcestub.New("glm-5.3-flash")
	defer plan.Close()
	defer metered.Close()
	plan.RefuseCompletion(http.StatusTooManyRequests, `{"code":"1309","message":"plan expired"}`)
	dir := t.TempDir()
	outcome, err := ConnectService(context.Background(), dir, PersistedSource{
		ID: "z-ai", Written: "z-ai", Key: "metered-test-key", Order: 1,
	}, twoDoorSource(t, plan, metered), nil)
	if err != nil || outcome.Kind != modelsource.OutcomeConnected || outcome.Door.ID != "metered" || outcome.Models != 1 {
		t.Fatalf("metered outcome = %+v, %v", outcome, err)
	}
	if len(plan.Requests()) != 1 || len(metered.Requests()) != 2 {
		t.Fatalf("requests: plan=%d metered=%d", len(plan.Requests()), len(metered.Requests()))
	}
	if rows := PersistedSources(dir); len(rows) != 1 || rows[0].Door != "metered" {
		t.Fatalf("bound row = %+v", rows)
	}
}

func TestEveryDoorRefusingIsStillCannotPay(t *testing.T) {
	t.Setenv("ZHIPU_API_KEY", "")
	plan, metered := sourcestub.New(), sourcestub.New()
	defer plan.Close()
	defer metered.Close()
	plan.RefuseCompletion(http.StatusTooManyRequests, `{"code":"1311","message":"model is not in this plan"}`)
	metered.RefuseCompletion(http.StatusTooManyRequests, `{"code":"1113","message":"Please recharge."}`)
	dir := t.TempDir()
	outcome, err := ConnectService(context.Background(), dir, PersistedSource{
		ID: "z-ai", Written: "z-ai", Key: "refused-test-key", Order: 1,
	}, twoDoorSource(t, plan, metered), nil)
	if err != nil || outcome.Kind != modelsource.OutcomeAccountCannotPay || outcome.VendorSaid != "Please recharge." {
		t.Fatalf("refused outcome = %+v, %v", outcome, err)
	}
	if len(PersistedSources(dir)) != 0 {
		t.Fatal("every refused door still stored a service")
	}
}

func TestABadKeyDoesNotWalkEveryDoor(t *testing.T) {
	t.Setenv("ZHIPU_API_KEY", "")
	plan, metered := sourcestub.New(), sourcestub.New()
	defer plan.Close()
	defer metered.Close()
	plan.RefuseCompletion(http.StatusUnauthorized, `{"error":{"message":"token expired or incorrect"}}`)
	outcome, err := ConnectService(context.Background(), t.TempDir(), PersistedSource{
		ID: "z-ai", Written: "z-ai", Key: "bad-test-key", Order: 1,
	}, twoDoorSource(t, plan, metered), nil)
	if err != nil || outcome.Kind != modelsource.OutcomeRefused || outcome.VendorSaid != "token expired or incorrect" {
		t.Fatalf("bad-key outcome = %+v, %v", outcome, err)
	}
	if len(plan.Requests()) != 1 || len(metered.Requests()) != 0 {
		t.Fatalf("bad key walked doors: plan=%d metered=%d", len(plan.Requests()), len(metered.Requests()))
	}
}

func TestOnlyAnExplicitReconnectRebindsTheDoor(t *testing.T) {
	t.Setenv("ZHIPU_API_KEY", "")
	plan, metered := sourcestub.New(), sourcestub.New("glm-5.3-flash")
	defer plan.Close()
	defer metered.Close()
	source := twoDoorSource(t, plan, metered)
	dir := t.TempDir()
	row := PersistedSource{ID: "z-ai", Written: "z-ai", Key: "reconnect-test-key", Order: 1}
	if outcome, err := ConnectService(context.Background(), dir, row, source, nil); err != nil || outcome.Door.ID != "coding-plan" {
		t.Fatalf("first connection = %+v, %v", outcome, err)
	}
	plan.RefuseCompletion(http.StatusTooManyRequests, `{"code":"1309","message":"plan expired"}`)
	before, ok := ResolveSources(dir, "", "").ByID("z-ai")
	if !ok || before.Door.ID != "coding-plan" || before.Address != "https://api.z.ai/api/coding/paas/v4" {
		t.Fatalf("reload silently rebound the door: %+v, found=%t", before, ok)
	}
	row = PersistedSources(dir)[0]
	reconnectSource := before.Source
	reconnectSource.Doors = source.Doors
	if outcome, err := ConnectService(context.Background(), dir, row, reconnectSource, nil); err != nil || outcome.Door.ID != "metered" {
		t.Fatalf("explicit reconnect = %+v, %v", outcome, err)
	}
	after, ok := ResolveSources(dir, "", "").ByID("z-ai")
	if !ok || after.Door.ID != "metered" || after.Address != "https://api.z.ai/api/paas/v4" {
		t.Fatalf("reconnected source = %+v, found=%t", after, ok)
	}
}

func TestAOneDoorServiceIsUnchanged(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
	server := sourcestub.New("deepseek-chat")
	defer server.Close()
	source := vendoredSource(t, "deepseek")
	source.Address = server.URL()
	outcome, err := ConnectService(context.Background(), t.TempDir(), PersistedSource{
		ID: source.ID, Written: "deepseek-direct", Key: "sk-direct-1234567890", Order: 1,
	}, source, nil)
	if err != nil || outcome.Kind != modelsource.OutcomeConnected || outcome.Door.ID != "" || outcome.Models != 1 {
		t.Fatalf("one-door outcome = %+v, %v", outcome, err)
	}
	requests := server.Requests()
	if len(requests) != 1 || requests[0].Method != http.MethodGet {
		t.Fatalf("one-door requests = %+v", requests)
	}
}

func TestVendorWordsDropsOnlyACodeThatRepeatsReadableWords(t *testing.T) {
	if got := vendorWords([]byte(`{"code":"1113","message":"Please recharge."}`)); got != "Please recharge." {
		t.Fatalf("readable refusal = %q", got)
	}
	if got := vendorWords([]byte(`{"code":"1000"}`)); got != "1000" {
		t.Fatalf("code-only refusal = %q", got)
	}
}

func TestDisconnectServiceRemovesTheRowAndItsKey(t *testing.T) {
	dir := t.TempDir()
	if err := WriteSources(dir, []PersistedSource{{ID: "deepseek", Written: "deepseek-direct", Key: "secret", Order: 1}, {ID: "z-ai", Written: "z-ai", Key: "other", Order: 2}}); err != nil {
		t.Fatal(err)
	}
	if err := DisconnectService(dir, "DEEPSEEK"); err != nil {
		t.Fatal(err)
	}
	rows := PersistedSources(dir)
	if len(rows) != 1 || rows[0].ID != "z-ai" {
		t.Fatalf("rows after disconnect = %+v", rows)
	}
	raw, _ := os.ReadFile(BudgetConfigPath(dir))
	if strings.Contains(string(raw), "secret") {
		t.Fatalf("disconnected key remains in profile: %s", raw)
	}
}

func TestACollidingServiceConnectsUnderTheSuggestedName(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
	dir := t.TempDir()
	server := sourcestub.New("deepseek-chat", "deepseek-reasoner")
	defer server.Close()
	source := vendoredSource(t, "deepseek")
	source.Address = server.URL()
	row := PersistedSource{ID: source.ID, Written: "deepseek", Key: "sk-direct-1234567890", Order: 1}
	outcome, err := ConnectService(context.Background(), dir, row, source, []string{"deepseek"})
	if err != nil || outcome.Kind != modelsource.OutcomeConnected {
		t.Fatalf("connection = %+v, %v", outcome, err)
	}
	rows := PersistedSources(dir)
	if len(rows) != 1 || rows[0].Written != "deepseek-direct" {
		t.Fatalf("connected rows = %+v", rows)
	}
}

func TestACollidingServicesModelsUseTheSuggestedName(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
	dir := t.TempDir()
	server := sourcestub.New("deepseek-chat", "deepseek-reasoner")
	defer server.Close()
	source := vendoredSource(t, "deepseek")
	source.Address = server.URL()
	outcome, err := ConnectService(context.Background(), dir, PersistedSource{
		ID: source.ID, Written: "deepseek", Key: "sk-direct-1234567890", Order: 1,
	}, source, []string{"deepseek"})
	if err != nil || outcome.Kind != modelsource.OutcomeConnected {
		t.Fatalf("connection = %+v, %v", outcome, err)
	}
	connected, ok := ResolveSources(dir, "", DefaultBaseURL).ByID("deepseek")
	if !ok {
		t.Fatal("the connected service was not resolved")
	}
	got := make([]string, 0, len(outcome.ModelIDs))
	for _, id := range outcome.ModelIDs {
		got = append(got, connected.Qualify(id))
	}
	want := []string{"deepseek-direct/deepseek-chat", "deepseek-direct/deepseek-reasoner"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("qualified models = %v, want %v", got, want)
	}
}

func TestReconnectingACollidingServiceKeepsTheFirstSuggestedName(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
	dir := t.TempDir()
	server := sourcestub.New("deepseek-chat")
	defer server.Close()
	source := vendoredSource(t, "deepseek")
	source.Address = server.URL()
	row := PersistedSource{ID: source.ID, Written: "deepseek", Key: "sk-direct-1234567890", Order: 1}
	if outcome, err := ConnectService(context.Background(), dir, row, source, []string{"deepseek"}); err != nil || outcome.Kind != modelsource.OutcomeConnected {
		t.Fatalf("first connection = %+v, %v", outcome, err)
	}
	row = PersistedSources(dir)[0]
	if outcome, err := ConnectService(context.Background(), dir, row, source, []string{"deepseek"}); err != nil || outcome.Kind != modelsource.OutcomeConnected {
		t.Fatalf("reconnection = %+v, %v", outcome, err)
	}
	rows := PersistedSources(dir)
	if len(rows) != 1 || rows[0].Written != "deepseek-direct" {
		t.Fatalf("reconnected rows = %+v", rows)
	}
}

func TestAnUnqualifiedIdResolvesExactlyAsItDidBefore(t *testing.T) {
	models := []string{
		DefaultModel, DefaultVoiceModel, DefaultReflexModel, DefaultLowModel,
		"z-ai/glm-5.3-flash", "moonshotai/kimi-k3", "anthropic/claude-opus-5",
	}
	for _, base := range []string{DefaultBaseURL, "https://company.example/v1"} {
		for _, model := range models {
			configured := Config{APIKey: "default-key", BaseURL: base, Model: model}.ClientConfig(model)
			assertComparableProviderConfig(t, configured, provider.Config{
				APIKey: "default-key", BaseURL: base, Model: model, Effort: provider.EffortNone,
			})
		}
	}

	dir := t.TempDir()
	fixture, err := os.ReadFile(filepath.Join("testdata", "profile-without-model-sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(BudgetConfigPath(dir), fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ProfileDirEnv, dir)
	t.Setenv(APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")
	for _, base := range []string{"", "https://company.example/v1"} {
		t.Setenv("CODEAF_BASE_URL", base)
		loaded, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		wantBase := base
		if wantBase == "" {
			wantBase = DefaultBaseURL
		}
		for _, model := range models {
			configured := loaded.ClientConfig(model)
			assertComparableProviderConfig(t, configured, provider.Config{
				APIKey: "sk-profile-captured-1234567890", BaseURL: wantBase,
				Model: model, Effort: provider.EffortNone, Timeout: DefaultTimeout,
			})
		}
	}
}

func assertComparableProviderConfig(t *testing.T, got, want provider.Config) {
	t.Helper()
	typeOf := reflect.TypeOf(got)
	gotValue, wantValue := reflect.ValueOf(got), reflect.ValueOf(want)
	for index := 0; index < typeOf.NumField(); index++ {
		field := typeOf.Field(index)
		if !field.Type.Comparable() {
			continue
		}
		if !reflect.DeepEqual(gotValue.Field(index).Interface(), wantValue.Field(index).Interface()) {
			t.Errorf("provider.Config.%s = %#v, want %#v", field.Name, gotValue.Field(index).Interface(), wantValue.Field(index).Interface())
		}
	}
}

func TestAQualifiedIdUsesItsServicesKeyAndAddress(t *testing.T) {
	sources := modelsource.NewSet(
		modelsource.Connected{Source: modelsource.DefaultSource("https://router.example/v1"), Key: "router-key", Address: "https://router.example/v1"},
		modelsource.Connected{Source: modelsource.Source{ID: "deepseek", Written: "deepseek-direct"}, Key: "direct-key", Address: "https://direct.example/v1"},
	)
	configured := ClientConfigFor(sources, "deepseek-direct/deepseek-chat:high")
	if configured.APIKey != "direct-key" || configured.BaseURL != "https://direct.example/v1" || configured.Model != "deepseek-chat" || configured.Effort != provider.EffortHigh {
		t.Fatalf("qualified config = %+v", configured)
	}
}

func TestNoServiceKeyReachesTheRecordWhateverItsShape(t *testing.T) {
	dir := t.TempDir()
	key := "zai_plain_secret_9F3B7A2C"
	t.Setenv("ZHIPU_API_KEY", key)
	server := sourcestub.New()
	defer server.Close()
	server.Listingless()
	source := vendoredSource(t, "z-ai")
	source.Address = server.URL()
	source.Doors = nil
	row := PersistedSource{ID: "z-ai", Written: "z-ai", Order: 1}
	outcome, err := ConnectService(context.Background(), dir, row, source, nil)
	if err != nil || outcome.Kind != modelsource.OutcomeConnected {
		t.Fatalf("production connect = %+v, %v", outcome, err)
	}
	if !contains(Credentials(dir), key) {
		t.Fatalf("Credentials missed non-sk service key: %v", Credentials(dir))
	}
	scrubbed := string(trace.Scrub([]byte("vendor echoed " + key + " in an error")))
	if strings.Contains(scrubbed, key) || !strings.Contains(scrubbed, "[redacted]") {
		t.Fatalf("scrubbed record = %q", scrubbed)
	}
}

func TestARefusalCannotEchoTheSubmittedKey(t *testing.T) {
	key := "zai_echo_secret_7A31D9"
	t.Setenv("ZHIPU_API_KEY", "")
	server := sourcestub.New()
	defer server.Close()
	server.Listingless()
	server.Refuse(401, `{"error":{"message":"account `+key+` has no credit"}}`)
	source := vendoredSource(t, "z-ai")
	source.Address = server.URL()
	source.Doors = nil
	outcome, err := ConnectService(context.Background(), t.TempDir(), PersistedSource{
		ID: "z-ai", Written: "z-ai", Key: key, Order: 1,
	}, source, nil)
	if err != nil || outcome.Kind != modelsource.OutcomeRefused {
		t.Fatalf("refusal = %+v, %v", outcome, err)
	}
	if strings.Contains(outcome.VendorSaid, key) || outcome.VendorSaid != "account [redacted] has no credit" {
		t.Fatalf("vendor words retained submitted key: %q", outcome.VendorSaid)
	}
}

func TestOnlyAnExplicitlyKeyOptionalServiceAcceptsBlank(t *testing.T) {
	t.Setenv("ZHIPU_API_KEY", "")
	server := sourcestub.New("local/model")
	defer server.Close()

	zai := vendoredSource(t, "z-ai")
	zai.Address = server.URL()
	dir := t.TempDir()
	outcome, err := ConnectService(context.Background(), dir, PersistedSource{
		ID: "z-ai", Written: "z-ai", Order: 1,
	}, zai, nil)
	if err != nil || outcome.Kind != modelsource.OutcomeWrongShape {
		t.Fatalf("blank required key = %+v, %v", outcome, err)
	}
	if len(server.Requests()) != 0 || len(PersistedSources(dir)) != 0 {
		t.Fatal("blank required key reached the network or profile")
	}

	// A permissive shape predicate is not a declaration that the service is
	// key-optional. That fact has its own field and must never be inferred.
	predicateOnly := zai
	predicateOnly.KeyShape = func(string) bool { return true }
	outcome, err = ConnectService(context.Background(), dir, PersistedSource{
		ID: "z-ai", Written: "z-ai", Order: 1,
	}, predicateOnly, nil)
	if err != nil || outcome.Kind != modelsource.OutcomeWrongShape {
		t.Fatalf("predicate-only blank key = %+v, %v", outcome, err)
	}
	if len(server.Requests()) != 0 {
		t.Fatal("a permissive shape predicate silently made the key optional")
	}

	ollama := vendoredSource(t, "ollama")
	ollama.Address = server.URL()
	outcome, err = ConnectService(context.Background(), dir, PersistedSource{
		ID: "ollama", Written: "ollama", Order: 1,
	}, ollama, nil)
	if err != nil || outcome.Kind != modelsource.OutcomeConnected {
		t.Fatalf("explicitly optional key = %+v, %v", outcome, err)
	}
}
