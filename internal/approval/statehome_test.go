package approval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeProfileKey is shaped like a provider key and is not one. A real key must
// never appear in a fixture, a log line or a failure message.
const fakeProfileKey = "sk-or-v1-FAKE0000000000000000000000000000000000000000000000000000"

// writeProfile puts a profile holding the fake key at folder/config.json.
func writeProfile(t *testing.T, folder string) string {
	t.Helper()
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(folder, "config.json")
	if err := os.WriteFile(name, []byte(`{"model":"some/model","api_key":"`+fakeProfileKey+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return name
}

// stateHome is one place a running codeaf can keep its profile, and the scope
// the floor builds while it stands there.
type stateHome struct {
	name string
	// arrange sets the process up and answers the profile's folder and the
	// folder the agent works in.
	arrange func(t *testing.T, person string) (profile, work string)
}

var stateHomes = []stateHome{
	{"the default home", func(t *testing.T, person string) (string, string) {
		t.Setenv("CODEAF_HOME", "")
		return filepath.Join(person, ".codeaf"), filepath.Join(person, "project")
	}},
	{"a CODEAF_HOME temp home", func(t *testing.T, person string) (string, string) {
		root := t.TempDir()
		t.Setenv("CODEAF_HOME", root)
		return root, filepath.Join(person, "project")
	}},
	{"a CODEAF_PROFILE_DIR profile", func(t *testing.T, person string) (string, string) {
		root := t.TempDir()
		t.Setenv("CODEAF_PROFILE_DIR", filepath.Join(root, "profile"))
		return filepath.Join(root, "profile"), filepath.Join(person, "project")
	}},
	{"a symlinked home", func(t *testing.T, person string) (string, string) {
		real := filepath.Join(t.TempDir(), "real-home")
		if err := os.MkdirAll(real, 0o755); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(t.TempDir(), "linked-home")
		if err := os.Symlink(real, link); err != nil {
			t.Skip("this filesystem cannot make a link:", err)
		}
		t.Setenv("CODEAF_HOME", link)
		return link, filepath.Join(person, "project")
	}},
	{"working inside the home", func(t *testing.T, person string) (string, string) {
		root := t.TempDir()
		t.Setenv("CODEAF_HOME", root)
		return root, root
	}},
}

// THE PROFILE THE RUNNING ENGINE USES IS A CREDENTIAL WHEREVER IT LIVES. A lane
// that tested with CODEAF_HOME pointed at a temporary folder watched an agent
// `cat` that folder's config.json and send the key to the model. Every place a
// home can be, and every ordinary way of reading it, is refused here.
func TestTheEnginesOwnProfileIsACredentialWhereverItLives(t *testing.T) {
	for _, place := range stateHomes {
		t.Run(place.name, func(t *testing.T) {
			person := t.TempDir()
			t.Setenv("HOME", person)
			t.Setenv("USERPROFILE", person)
			t.Setenv("CODEAF_HOME", "")
			t.Setenv("CODEAF_PROFILE_DIR", "")
			profileDir, work := place.arrange(t, person)
			profile := writeProfile(t, profileDir)
			if err := os.MkdirAll(work, 0o755); err != nil {
				t.Fatal(err)
			}
			scope := DefaultCredentialScope(work)
			relative, err := filepath.Rel(work, profile)
			if err != nil {
				t.Fatal(err)
			}
			resolved, err := filepath.EvalSymlinks(profile)
			if err != nil {
				t.Fatal(err)
			}
			for _, command := range []string{
				"cat " + profile,
				"cat " + resolved,
				"head -n 5 " + profile,
				"less " + profile,
				"cp " + profile + " /tmp/copy.json",
				"base64 " + profile,
				"jq .api_key " + profile,
				"cat " + relative,
				"cd " + profileDir + " && cat config.json",
				"cat " + filepath.Join(profileDir, "config.json.bak"),
			} {
				if _, refused := scope.Command(command); !refused {
					t.Errorf("%s was not refused", strings.ReplaceAll(command, person, "~"))
				}
			}
			for _, path := range []string{profile, relative} {
				args, _ := json.Marshal(map[string]string{"path": path})
				if _, refused := scope.Read("read", args); !refused {
					t.Errorf("read %s was not refused", path)
				}
			}
		})
	}
}

// A HOME NAMED ONLY ON THE COMMAND LINE IS A HOME. The variable is set for the
// one command, so the engine's own scope cannot know it; the floor reads it off
// the line the way a shell would.
func TestAHomeNamedOnTheCommandLineIsCovered(t *testing.T) {
	scope, _ := scopeForTest(t)
	other := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	// An identity file has no api_key to sniff, so only the named home catches it.
	identity := filepath.Join(other, "identity.json")
	if err := os.WriteFile(identity, []byte(`{"id":"fake"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{
		"CODEAF_HOME=" + other + " cat " + identity,
		"export CODEAF_HOME=" + other + "; cat $CODEAF_HOME/identity.json",
		"export CODEAF_PROFILE_DIR=" + other + " && head " + identity,
	} {
		if _, refused := scope.Command(command); !refused {
			t.Errorf("%s was not refused", command)
		}
	}
	if _, refused := scope.Command("cat " + identity); refused {
		t.Errorf("an identity.json outside every named home was refused")
	}
}

// A PROFILE HOLDING A KEY IS A KEY FILE IN ANY FOLDER, which is the case a test
// home the engine never heard of falls into. A project's own config.json with no
// key in it is ordinary work and stays readable.
func TestAConfigHoldingAnAPIKeyIsRefusedAnywhere(t *testing.T) {
	scope, home := scopeForTest(t)
	stray := writeProfile(t, filepath.Join(t.TempDir(), "some-old-test-home"))
	if _, refused := scope.Command("cat " + stray); !refused {
		t.Errorf("a stray profile holding a key was readable")
	}
	plain := filepath.Join(home, "project", "config.json")
	if err := os.WriteFile(plain, []byte(`{"port":8080,"api_key":""}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, refused := scope.Command("cat config.json"); refused {
		t.Errorf("a project's own config.json with no key was refused")
	}
	args, _ := json.Marshal(map[string]string{"path": "config.json"})
	if _, refused := scope.Read("read", args); refused {
		t.Errorf("reading a project's own config.json with no key was refused")
	}
}

// The refusal names the file and never its contents.
func TestTheRefusalNeverCarriesTheKey(t *testing.T) {
	scope, home := scopeForTest(t)
	profile := writeProfile(t, filepath.Join(home, ".codeaf"))
	read, refused := scope.Command("cat " + profile)
	if !refused {
		t.Fatal("the profile was readable")
	}
	if strings.Contains(read.Message(), fakeProfileKey) {
		t.Fatal("the refusal carried the key")
	}
}
