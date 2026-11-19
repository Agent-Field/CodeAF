package exec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeProfileKey is shaped like a provider key and is not one. A real key must
// never appear in a fixture, a log line or a failure message.
const fakeProfileKey = "sk-or-v1-FAKE0000000000000000000000000000000000000000000000000000"

// THE LEAF'S OWN DOOR ASKS THE CREDENTIAL FLOOR. `codeaf exec` ran tools with no
// floor in front of them, so a lane that pointed CODEAF_HOME at a temporary
// folder watched an agent `cat` that folder's config.json and send the key to
// the model. Each row is a place the profile can live and the way an agent
// reached for it; none of them may hand the key back.
func TestALeafCannotReadTheProfileKeyWhereverItLives(t *testing.T) {
	quietExecLog(t)
	for _, place := range []struct {
		name string
		// arrange answers the profile's folder, the workspace, and the command.
		arrange func(t *testing.T, person string) (profileDir, work, command string)
	}{
		{"the default home", func(t *testing.T, person string) (string, string, string) {
			t.Setenv("CODEAF_HOME", "")
			dir := filepath.Join(person, ".codeaf")
			return dir, t.TempDir(), "cat ~/.codeaf/config.json"
		}},
		{"a CODEAF_HOME temp home", func(t *testing.T, person string) (string, string, string) {
			dir := t.TempDir()
			t.Setenv("CODEAF_HOME", dir)
			return dir, t.TempDir(), "cat " + filepath.Join(dir, "config.json")
		}},
		{"the variable itself", func(t *testing.T, person string) (string, string, string) {
			dir := t.TempDir()
			t.Setenv("CODEAF_HOME", dir)
			return dir, t.TempDir(), `cat "$CODEAF_HOME/config.json"`
		}},
		{"a symlinked home", func(t *testing.T, person string) (string, string, string) {
			real := t.TempDir()
			link := filepath.Join(t.TempDir(), "home-link")
			if err := os.Symlink(real, link); err != nil {
				t.Skip("this filesystem cannot make a link:", err)
			}
			t.Setenv("CODEAF_HOME", link)
			return link, t.TempDir(), "cat " + filepath.Join(real, "config.json")
		}},
		{"a relative read from inside the home", func(t *testing.T, person string) (string, string, string) {
			dir := t.TempDir()
			t.Setenv("CODEAF_HOME", dir)
			return dir, dir, "cat config.json"
		}},
		{"a home named only on the command line", func(t *testing.T, person string) (string, string, string) {
			t.Setenv("CODEAF_HOME", "")
			dir := t.TempDir()
			return dir, t.TempDir(), "CODEAF_HOME=" + dir + " && base64 " + filepath.Join(dir, "config.json")
		}},
	} {
		t.Run(place.name, func(t *testing.T) {
			person := t.TempDir()
			t.Setenv("HOME", person)
			t.Setenv("CODEAF_PROFILE_DIR", "")
			profileDir, work, command := place.arrange(t, person)
			if err := os.MkdirAll(profileDir, 0o755); err != nil {
				t.Fatal(err)
			}
			profile := `{"api_key":"` + fakeProfileKey + `"}`
			if err := os.WriteFile(filepath.Join(profileDir, "config.json"), []byte(profile), 0o600); err != nil {
				t.Fatal(err)
			}
			workspace, err := NewWorkspace(work)
			if err != nil {
				t.Fatal(err)
			}
			arguments := `{"cmd":` + quoteJSON(command) + `}`
			result := NewToolbox(workspace, "1", nil).Execute(context.Background(), "sh", arguments)
			if strings.Contains(result.Content, fakeProfileKey) || strings.Contains(result.Content, "FAKE0000") {
				t.Fatal("the profile key reached the model")
			}
			if !result.IsError || !strings.Contains(result.Content, "reaches for a credential") {
				t.Fatalf("the read was not refused by the floor: %.200s", result.Content)
			}
		})
	}
}

// An ordinary command on the same door still runs.
func TestTheFloorLeavesOrdinaryCommandsAlone(t *testing.T) {
	quietExecLog(t)
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "config.json"), []byte(`{"port":8080}`), 0o644); err != nil {
		t.Fatal(err)
	}
	workspace, err := NewWorkspace(work)
	if err != nil {
		t.Fatal(err)
	}
	result := NewToolbox(workspace, "1", nil).Execute(context.Background(), "sh", `{"cmd":"cat config.json"}`)
	if result.IsError || !strings.Contains(result.Content, "8080") {
		t.Fatalf("a project's own config.json was not readable: %.200s", result.Content)
	}
}

func quoteJSON(text string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(text) + `"`
}
