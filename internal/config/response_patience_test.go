package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/lane"
)

// Derived from the validation contract for slow-local-model patience: what a
// person may write, what is refused and where the refusal names the row or the
// variable, and what reaches the wire.

func resetPatience(t *testing.T) {
	t.Helper()
	lane.UseResponsePatience(0)
	t.Cleanup(func() { lane.UseResponsePatience(0) })
}

// CONTRACT: `auto`, or a number from 1 to 100 with an optional `x`, decimals
// allowed; anything else is refused in a message that names what was wrong.
func TestResponsePatienceAcceptsAutoAndOneToAHundred(t *testing.T) {
	accepted := map[string]float64{
		"auto": 0, "AUTO": 0, " auto ": 0, "": 0,
		"1": 1, "1x": 1, "3": 3, "3x": 3, "3X": 3, " 10 ": 10, "10x": 10, "2.5": 2.5, "2.5x": 2.5,
		"100": 100, "100x": 100, "1.0": 1,
	}
	for raw, want := range accepted {
		got, err := ParseResponsePatience(raw)
		if err != nil || got != want {
			t.Errorf("ParseResponsePatience(%q) = %v, %v; want %v", raw, got, err, want)
		}
	}
	for _, raw := range []string{"0", "0x", "0.5", "0.999", "100.5", "101", "1000", "-3", "fast", "x", "10xx", "ten", "1,5", "NaN", "Inf", "+Inf", "auto10"} {
		_, err := ParseResponsePatience(raw)
		if err == nil {
			t.Errorf("ParseResponsePatience(%q) was accepted, want a refusal", raw)
			continue
		}
		for _, want := range []string{"auto", "1", "100", "10x", strings.TrimSpace(raw)} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal of %q = %q, which does not say %q", raw, err.Error(), want)
			}
		}
	}
	if got := FormatResponsePatience(0); got != "auto" {
		t.Errorf("FormatResponsePatience(0) = %q", got)
	}
	if got := FormatResponsePatience(10); got != "10x" {
		t.Errorf("FormatResponsePatience(10) = %q", got)
	}
	if got := FormatResponsePatience(2.5); got != "2.5x" {
		t.Errorf("FormatResponsePatience(2.5) = %q", got)
	}
}

// CONTRACT: unset is `auto`; the environment beats the row; a typo in the
// environment errors naming the variable, and a bad row quietly reads as auto.
func TestResponsePatienceResolvesEnvironmentThenRowThenAuto(t *testing.T) {
	resetPatience(t)
	dir := t.TempDir()
	t.Setenv(EnvResponsePatience, "")
	if got, err := ResponsePatienceAt(dir); err != nil || got != 0 {
		t.Fatalf("nothing written = %v, %v; want auto", got, err)
	}

	for text, want := range map[string]float64{`"3x"`: 3, `"auto"`: 0, `4`: 4, `2.5`: 2.5, `"12"`: 12} {
		writeRawProfile(t, dir, `{"response.patience": `+text+`}`)
		if got, err := ResponsePatienceAt(dir); err != nil || got != want {
			t.Errorf("row %s resolved to %v, %v; want %v", text, got, err, want)
		}
	}
	for _, bad := range []string{`"fast"`, `0`, `0.5`, `500`, `true`, `null`, `[3]`, `"101"`} {
		writeRawProfile(t, dir, `{"response.patience": `+bad+`}`)
		if got, err := ResponsePatienceAt(dir); err != nil || got != 0 {
			t.Errorf("a persisted %s resolved to %v, %v; want auto with no error", bad, got, err)
		}
	}

	writeRawProfile(t, dir, `{"response.patience": "3x"}`)
	t.Setenv(EnvResponsePatience, "7")
	if got, err := ResponsePatienceAt(dir); err != nil || got != 7 {
		t.Errorf("the environment lost to the row: %v, %v", got, err)
	}
	t.Setenv(EnvResponsePatience, "auto")
	if got, err := ResponsePatienceAt(dir); err != nil || got != 0 {
		t.Errorf("an explicit auto pin lost to the row: %v, %v", got, err)
	}
	for _, typo := range []string{"fast", "0", "0.5", "101", "-1", "10 minutes"} {
		t.Setenv(EnvResponsePatience, typo)
		_, err := ResponsePatienceAt(dir)
		if err == nil || !strings.Contains(err.Error(), "CODEAF_RESPONSE_PATIENCE") {
			t.Errorf("CODEAF_RESPONSE_PATIENCE=%q: error = %v, want one naming the variable", typo, err)
		}
	}
}

// CONTRACT: a launch with a malformed pin stops, naming the variable; a launch
// with a good one reaches the wire as the number it was.
func TestLoadRefusesATypoedPinAndInstallsAGoodOne(t *testing.T) {
	resetPatience(t)
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv("CODEAF_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())

	t.Setenv(EnvResponsePatience, "fast")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "CODEAF_RESPONSE_PATIENCE") || !strings.Contains(err.Error(), `"fast"`) {
		t.Fatalf("Load with a typoed pin = %v, want an error naming the variable and the value", err)
	}

	t.Setenv(EnvResponsePatience, "3x")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	if got := lane.PatienceFor("https://openrouter.ai/api/v1", ""); got != 3 {
		t.Errorf("after loading 3x a hosted endpoint is held to %v, want 3", got)
	}
	if got := lane.PatienceFor("http://localhost:11434/v1", ""); got != 3 {
		t.Errorf("after loading 3x a local endpoint is held to %v, want 3 (a number applies to every endpoint, unmultiplied)", got)
	}

	t.Setenv(EnvResponsePatience, "")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	if got := lane.PatienceFor("https://openrouter.ai/api/v1", ""); got != 1 {
		t.Errorf("auto, hosted = %v, want 1", got)
	}
	if got := lane.PatienceFor("http://localhost:11434/v1", ""); got != lane.LocalPatience {
		t.Errorf("auto, local = %v, want %v", got, lane.LocalPatience)
	}
}

// CONTRACT: the row is on the settings sheet, reads what the product will use,
// refuses a bad value naming itself, persists one spelling, takes effect on the
// next request without a restart, and cannot be written while the variable pins it.
func TestTheResponsePatienceRow(t *testing.T) {
	resetPatience(t)
	dir := t.TempDir()
	t.Setenv(EnvResponsePatience, "")
	row, found := registry(t, dir).Row(KeyResponsePatience)
	if !found {
		t.Fatal("response.patience is not a settings row")
	}
	if row.Env != "CODEAF_RESPONSE_PATIENCE" || row.Category != CategoryModels {
		t.Errorf("row = %+v", row)
	}
	if row.Value() != "auto" {
		t.Errorf("an unwritten row reads %q, want auto", row.Value())
	}
	for _, want := range []string{"auto", "10x", "1", "100", "hosted"} {
		if !strings.Contains(row.Hint, want) {
			t.Errorf("hint %q does not say %q", row.Hint, want)
		}
	}

	if err := row.Apply("fast"); err == nil || !strings.Contains(err.Error(), "response.patience") {
		t.Errorf("Apply(fast) = %v, want a refusal naming response.patience", err)
	}
	if err := row.Apply("0.5"); err == nil {
		t.Error("Apply(0.5) was accepted")
	}
	if err := row.Apply("101"); err == nil {
		t.Error("Apply(101) was accepted")
	}
	if got, _ := ResponsePatienceAt(dir); got != 0 {
		t.Errorf("a refused write changed the row to %v", got)
	}

	if err := row.Apply("10"); err != nil {
		t.Fatal(err)
	}
	reread, _ := registry(t, dir).Row(KeyResponsePatience)
	if reread.Value() != "10x" {
		t.Errorf("a written 10 reads back as %q, want the one spelling 10x", reread.Value())
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil || !strings.Contains(string(raw), `"response.patience": "10x"`) {
		t.Errorf("persisted file = %s (%v)", raw, err)
	}
	if got := lane.PatienceFor("https://api.openai.com/v1", ""); got != 10 {
		t.Errorf("the very next hosted request is held to %v after writing 10, want 10", got)
	}

	if err := row.Apply("auto"); err != nil {
		t.Fatal(err)
	}
	if got := lane.PatienceFor("https://api.openai.com/v1", ""); got != 1 {
		t.Errorf("after auto, hosted = %v, want 1", got)
	}
	if got := lane.PatienceFor("http://192.168.0.9:8000/v1", ""); got != lane.LocalPatience {
		t.Errorf("after auto, local = %v, want %v", got, lane.LocalPatience)
	}

	t.Setenv(EnvResponsePatience, "5")
	pinned, _ := registry(t, dir).Row(KeyResponsePatience)
	if name, isPinned := pinned.PinnedBy(); !isPinned || name != "CODEAF_RESPONSE_PATIENCE" {
		t.Errorf("pin = %q, %v", name, isPinned)
	}
	if pinned.Value() != "5x" {
		t.Errorf("a pinned row reads %q, want the pin 5x", pinned.Value())
	}
	if err := pinned.Apply("2"); err == nil {
		t.Error("a pinned row accepted a write")
	}
}

// CONTRACT: the default's figure is the one source, so the row's hint and the
// manual's claim cannot drift from what the transport does.
func TestTheLocalDefaultIsOneNamedConstant(t *testing.T) {
	row, _ := registry(t, t.TempDir()).Row(KeyResponsePatience)
	if !strings.Contains(row.Hint, "10x for one on your own machine") {
		t.Errorf("the hint does not carry lane.LocalPatience (%v): %q", lane.LocalPatience, row.Hint)
	}
}

func writeRawProfile(t *testing.T, dir, body string) {
	t.Helper()
	if !json.Valid([]byte(body)) {
		t.Fatalf("test fixture is not JSON: %s", body)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
