package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
)

// C9: Doctor identifies every answered rung and explains unused shell keys,
// while neither a selected nor an ignored credential appears in its output.
func TestDoctorExplainsUnusedOpenAIKeys(t *testing.T) {
	for _, row := range []struct{ name, base, router, profile, openai, where, why string }{
		{"router outranks", "", "sk-or-v1-test-router", "sk-or-v1-test-profile", "sk-proj-test-ignored", config.APIKeyEnv, "outranked by OPENROUTER_API_KEY"},
		{"profile outranks usable fallback", "", "", "sk-or-v1-test-profile", "sk-or-v1-test-fallback", "profile", "outranked by the profile key"},
		{"profile outranks project key", "", "", "sk-or-v1-test-profile", "sk-proj-test-ignored", "profile", "outranked by the profile key"},
		{"project key alone", "", "", "", "sk-proj-test-ignored", "", "not an OpenRouter key, so it is not used"},
		{"OpenAI key alone", "", "", "", "sk-test-ignored", "", "not an OpenRouter key, so it is not used"},
		{"usable fallback", "", "", "", "sk-or-v1-test-fallback", fallbackKeyEnv, ""},
		{"custom endpoint", "https://custom.invalid/v1", "", "sk-or-v1-test-profile", "sk-proj-test-custom", fallbackKeyEnv, ""},
		{"custom router outranks", "https://custom.invalid/v1", "sk-or-v1-test-router", "", "sk-proj-test-custom", config.APIKeyEnv, "outranked by OPENROUTER_API_KEY"},
		{"profile only", "", "", "sk-or-v1-test-profile", "", "profile", ""},
		{"nothing", "", "", "", "", "", ""},
	} {
		t.Run(row.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("CODEAF_BASE_URL", row.base)
			t.Setenv(config.APIKeyEnv, row.router)
			t.Setenv(fallbackKeyEnv, row.openai)
			if row.profile != "" {
				if err := config.WriteAPIKey(dir, row.profile); err != nil {
					t.Fatal(err)
				}
			}
			want := row.where
			if want == "profile" {
				want = config.BudgetConfigPath(dir)
			}
			report := readKeyReport(dir)
			if report.Where != want {
				t.Fatalf("rung %q, want %q", report.Where, want)
			}
			output := formatKey(report)
			if row.why != "" && !strings.Contains(output, row.why) {
				t.Fatalf("unused explanation missing: %s", output)
			}
			if row.why == "" && report.Unused != "" {
				t.Fatalf("unexpected explanation: %s", output)
			}
			for _, secret := range []string{row.router, row.profile, row.openai} {
				if secret != "" && strings.Contains(output, secret) {
					t.Fatal("doctor printed a credential")
				}
			}
		})
	}
}

// C8: The real launch setup gate treats an unrelated OpenAI key as keyless,
// so interactive launches offer connection before any provider request.
func TestForgottenOpenAIKeyStillOffersFirstRunConnection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	t.Setenv("CODEAF_BASE_URL", "")
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "sk-proj-test-forgotten")
	if config.APIKeyAt(config.ProfileDir()) != "" || v3MachineIsSetUp() {
		t.Fatal("unrelated OpenAI key bypassed first-run setup")
	}
	if v3TakeHostRoad(t.TempDir(), v3HostChoice{setup: !v3MachineIsSetUp()}) {
		t.Fatal("keyless first run left its setup in a daemon")
	}
}

// C12: Help and every missing-key remedy offer the default-provider door, and
// the environment reference explains idle restart and busy-engine recovery.
func TestKeyAdviceExplainsTheEnvironmentAndUsableDoor(t *testing.T) {
	for _, needle := range []string{"idle folder engine restarts", "busy engine stays running", "codeaf engine --stop --workspace <dir>", "profile; then", "OpenRouter-shaped", "custom CODEAF_BASE_URL"} {
		if !strings.Contains(environmentText, needle) {
			t.Errorf("help env missing %q", needle)
		}
	}
	// A named credential source on an authentication refusal is not the
	// missing-key failure and must not gain that failure's remedy.
	if got := remedyFor("your key was not accepted for this model — the shell's OPENROUTER_API_KEY"); got != "" {
		t.Errorf("auth refusal gained missing-key advice: %q", got)
	}
	for _, failure := range []string{config.ErrNoAPIKey.Error(), "no auth credentials found", "OPENROUTER_API_KEY (or OPENAI_API_KEY) is required"} {
		remedy := remedyFor(failure)
		if !strings.Contains(remedy, config.APIKeyEnv) || strings.Contains(remedy, fallbackKeyEnv) {
			t.Errorf("wrong remedy for %q: %s", failure, remedy)
		}
	}
}

// C9 and F4: Doctor recognizes every OpenRouter address variant by host and
// explains the answering or ignored rung without revealing either key.
func TestDoctorUsesTheDefaultLadderForOpenRouterAddressVariants(t *testing.T) {
	for _, base := range []string{
		"https://openrouter.ai./api/v1", "https://OPENROUTER.AI./other/",
		"https://www.openrouter.ai", "http://WWW.OPENROUTER.AI./any/path/",
		"openrouter.ai/api/v1", "OPENROUTER.AI./other/",
		"www.openrouter.ai/api/v1/", "WWW.OPENROUTER.AI./",
	} {
		t.Run(base, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("CODEAF_BASE_URL", base)
			t.Setenv(config.APIKeyEnv, "")
			t.Setenv("OPENAI_API_KEY", "sk-proj-test-ignored")
			report := readKeyReport(dir)
			if report.Where != "" || !strings.Contains(report.Unused, "not an OpenRouter key, so it is not used") {
				t.Fatalf("keyless report = %+v", report)
			}
			if err := config.WriteAPIKey(dir, "sk-or-v1-test-saved"); err != nil {
				t.Fatal(err)
			}
			report = readKeyReport(dir)
			if report.Where != config.BudgetConfigPath(dir) || !strings.Contains(report.Unused, "outranked by the profile key") {
				t.Fatalf("connected report = %+v", report)
			}
			if output := formatKey(report); strings.Contains(output, "sk-proj-test-ignored") || strings.Contains(output, "sk-or-v1-test-saved") {
				t.Fatal("doctor exposed a key")
			}
		})
	}
}
