package config

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/trace"
)

func TestAppCredentialsOutrankShellAndProfileAndRemovalDoesNotFallBack(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAPIKey(dir, "sk-or-profile-0123456789"); err != nil {
		t.Fatal(err)
	}
	t.Setenv(APIKeyEnv, "sk-or-shell-0123456789")
	t.Setenv("DEEPSEEK_API_KEY", "sk-shell-deepseek-0123456789")
	source := NewAppCredentials()
	source.Set("openrouter", "sk-or-app-0123456789")
	source.Set("deepseek", "sk-app-deepseek-0123456789")
	source.Set("github", "opaque-github-credential-0123456789")
	source.SetFactorySession("https://factory.example", "opaque-factory-session-0123456789")
	defer UseAppCredentials(source)()
	if APIKeyAt(dir) != "sk-or-app-0123456789" || APIKeySourceAt(dir) != "the app's key" {
		t.Fatal("memory key lost precedence")
	}
	t.Setenv(ProfileDirEnv, dir)
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	service, ok := loaded.Sources.ByID("deepseek")
	if !ok || service.Key != "sk-app-deepseek-0123456789" {
		t.Fatal("memory-only provider is not available to sessions")
	}
	if GitHubTokenAt(dir) != "opaque-github-credential-0123456789" {
		t.Fatal("GitHub did not use memory")
	}
	source.Set("openrouter", "")
	source.Set("deepseek", "")
	if APIKeyAt(dir) != "" {
		t.Fatal("removed key fell back to disk or shell")
	}
	service, _ = ResolveSources(dir, "", DefaultBaseURL).ByID("deepseek")
	if service.Key != "" {
		t.Fatal("removed provider fell back to shell")
	}
	for _, secret := range []string{"opaque-github-credential-0123456789", "opaque-factory-session-0123456789", "sk-or-app-0123456789"} {
		if strings.Contains(redact.Secrets("got "+secret), secret) || strings.Contains(string(trace.Scrub([]byte(secret))), secret) {
			t.Fatal("app secret escaped redaction")
		}
	}
}

func TestAppModeRefusesEveryProfileKeyWriterAndAllowsSettings(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAPIKey(dir, "sk-or-existing-0123456789"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(BudgetConfigPath(dir))
	source := NewAppCredentials()
	source.Set("openrouter", "sk-or-app-0123456789")
	defer UseAppCredentials(source)()
	row, _ := NewSettings(SettingsOptions{ProfileDir: dir}).Row(KeyAPIKey)
	for _, write := range []func() error{
		func() error { return WriteAPIKey(dir, "sk-or-new-0123456789") },
		func() error { return WriteGitHubToken(dir, "github-new-0123456789") },
		func() error {
			return WriteSources(dir, []PersistedSource{{ID: "deepseek", Written: "deepseek", Key: "sk-new-0123456789"}})
		},
		func() error { return row.write("sk-or-new-0123456789") },
		func() error { _, _, err := EnsurePersistedAPIKey(dir); return err },
	} {
		if err := write(); !errors.Is(err, ErrAppKeys) {
			t.Fatalf("writer did not refuse: %v", err)
		}
		current, _ := os.ReadFile(BudgetConfigPath(dir))
		if string(current) != string(before) {
			t.Fatal("refused key write changed disk")
		}
	}
	if err := WriteDailyBudgetUSD(dir, 20); err != nil {
		t.Fatal(err)
	}
	current, _ := os.ReadFile(BudgetConfigPath(dir))
	if strings.Contains(string(current), "sk-or-app") || PersistedAPIKey(dir) != "sk-or-existing-0123456789" {
		t.Fatal("ordinary setting copied an app key or removed the terminal key")
	}
}

func TestAppHandoffIsBoundedAtomicAndLeavesTheLifeLine(t *testing.T) {
	source := NewAppCredentials()
	reader := bufio.NewReader(strings.NewReader(`{"keys":{"openrouter":"opaque-key-123456789"},"factory_sessions":{"https://factory.example":"session-123456789"}}` + "\nstill open"))
	if err := ReadAppHandoff(reader, source); err != nil {
		t.Fatal(err)
	}
	key, _ := source.Key("openrouter")
	rest, _ := io.ReadAll(reader)
	if key != "opaque-key-123456789" || source.FactorySession("https://factory.example") != "session-123456789" || string(rest) != "still open" {
		t.Fatal("handoff swallowed lifetime bytes or lost credentials")
	}
	for _, body := range []string{"null\n", "[]\n", "invalid-secret-123456789\n", "{} {}\n", "{\"unknown\":true}\n", "{\"keys\":{\"openrouter\":1}}\n", "{\"keys\":{\"openrouter\":\"new-key\",\"\":\"invalid\"}}\n", strings.Repeat("x", AppHandoffLimit+1) + "\n", "{}"} {
		if err := ReadAppHandoff(bufio.NewReader(strings.NewReader(body)), source); err == nil || strings.Contains(err.Error(), "invalid-secret") {
			t.Fatalf("bad handoff accepted or echoed: %v", err)
		}
		if current, _ := source.Key("openrouter"); current != key {
			t.Fatal("invalid handoff changed credentials")
		}
	}
}

func TestAppCredentialPipeChild(t *testing.T) {
	if os.Getenv("CODEAF_TEST_APP_KEYS_CHILD") != "1" {
		return
	}
	fd, err := strconv.Atoi(os.Args[len(os.Args)-1])
	if err != nil {
		os.Exit(2)
	}
	input := os.NewFile(uintptr(fd), "keys")
	source := NewAppCredentials()
	if ReadAppHandoff(bufio.NewReader(input), source) != nil {
		os.Exit(3)
	}
	input.Close()
	defer UseAppCredentials(source)()
	key, _ := source.Key("openrouter")
	answer := map[string]bool{
		"received":          key == "opaque-pipe-key-123456789",
		"session":           source.FactorySession("https://factory.example") == "opaque-session-123456789",
		"environment_clean": os.Getenv(APIKeyEnv) == "" && os.Getenv("OPENAI_API_KEY") == "" && os.Getenv("GH_TOKEN") == "",
		"app_mode":          AppMode(),
		"writes_refused":    errors.Is(WriteAPIKey(os.Getenv("CODEAF_TEST_APP_KEYS_DIR"), key), ErrAppKeys),
	}
	if json.NewEncoder(os.Stdout).Encode(answer) != nil {
		os.Exit(4)
	}
	os.Exit(0)
}

func TestAppCredentialsUseAnInheritedPipeWithoutArgvOrEnvironmentSecrets(t *testing.T) {
	profile := t.TempDir()
	t.Setenv(ProfileDirEnv, profile)
	if err := WriteSources(profile, []PersistedSource{{ID: "deepseek", Written: "deepseek", KeyEnv: "CODEAF_TEST_APP_SOURCE_KEY"}}); err != nil {
		t.Fatal(err)
	}
	source := NewAppCredentials()
	source.Set("openrouter", "opaque-pipe-key-123456789")
	source.SetFactorySession("https://factory.example", "opaque-session-123456789")
	defer UseAppCredentials(source)()
	command := exec.Command(os.Args[0], "-test.run=^TestAppCredentialPipeChild$", "--")
	command.Env = append(os.Environ(), "CODEAF_TEST_APP_KEYS_CHILD=1", "CODEAF_TEST_APP_KEYS_DIR="+t.TempDir(), APIKeyEnv+"=sk-shell-123456789", "OPENAI_API_KEY=sk-openai-123456789", "GH_TOKEN=gh-shell-123456789", "CODEAF_TEST_APP_SOURCE_KEY=unrelated-custom-shell-credential")
	closePipe, err := PrepareAppCredentialPipe(command)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(command.Args, " "), "opaque-pipe-key") || strings.Contains(strings.Join(command.Env, " "), "sk-shell") {
		t.Fatal("secret reached arguments or environment")
	}
	if strings.Contains(strings.Join(command.Env, " "), "unrelated-custom-shell-credential") {
		t.Fatal("custom provider key remained in the child environment")
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		closePipe()
		t.Fatal(err)
	}
	err = command.Start()
	closePipe()
	if err != nil {
		t.Fatal(err)
	}
	var answer map[string]bool
	decodeErr := json.NewDecoder(stdout).Decode(&answer)
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
	if decodeErr != nil || len(answer) != 5 {
		t.Fatalf("pipe child: %v %v", answer, decodeErr)
	}
	for name, ok := range answer {
		if !ok {
			t.Fatalf("pipe child failed %s", name)
		}
	}
	failed := exec.Command("/no/such/codeaf", "engine")
	closePipe, err = PrepareAppCredentialPipe(failed)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Start() == nil {
		t.Fatal("invalid command started")
	}
	closePipe()
}
