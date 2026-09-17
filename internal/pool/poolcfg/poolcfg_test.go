package poolcfg

import (
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"
)

// envOf builds a lookup over a map. A name present with an empty value counts
// as set, because a name that is set and empty is not the same as a name
// nobody set.
func envOf(pairs map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, set := pairs[name]
		return value, set
	}
}

func TestModeString(t *testing.T) {
	cases := []struct {
		mode Mode
		want string
	}{
		{On, "on"},
		{Read, "read"},
		{Off, "off"},
	}
	for _, c := range cases {
		if got := c.mode.String(); got != c.want {
			t.Errorf("Mode(%d).String() = %q, want %q", int(c.mode), got, c.want)
		}
	}
}

func TestResolveDefaults(t *testing.T) {
	want := Config{
		Mode:      On,
		IndexURL:  DefaultIndexURL,
		SubmitURL: DefaultSubmitURL,
		TTL:       24 * time.Hour,
		Source: Sources{
			Mode:      "default",
			IndexURL:  "default",
			SubmitURL: "default",
			TTL:       "default",
		},
	}
	lookups := []struct {
		name   string
		lookup func(string) (string, bool)
	}{
		{"nil lookup", nil},
		{"nobody set", envOf(nil)},
		{"other names only", envOf(map[string]string{"OTHER": "on"})},
	}
	for _, l := range lookups {
		if got := Resolve("", l.lookup); got != want {
			t.Errorf("%s: Resolve() = %+v, want %+v", l.name, got, want)
		}
	}
}

func TestResolveMode(t *testing.T) {
	cases := []struct {
		name    string
		setting string
		lookup  func(string) (string, bool)
		want    Mode
		wantSrc string
	}{
		{"env on", "", envOf(map[string]string{"CODEAF_MODEL_POOL": "on"}), On, "env"},
		{"env read", "", envOf(map[string]string{"CODEAF_MODEL_POOL": "read"}), Read, "env"},
		{"env off", "", envOf(map[string]string{"CODEAF_MODEL_POOL": "off"}), Off, "env"},
		{"env padded and mixed case", "", envOf(map[string]string{"CODEAF_MODEL_POOL": "\t OFF "}), Off, "env"},
		{"env unknown word falls through", "read", envOf(map[string]string{"CODEAF_MODEL_POOL": "maybe"}), Read, "setting"},
		{"env set and empty falls through", "read", envOf(map[string]string{"CODEAF_MODEL_POOL": ""}), Read, "setting"},
		{"setting on", "on", envOf(nil), On, "setting"},
		{"setting read", "read", envOf(nil), Read, "setting"},
		{"setting padded and mixed case", "  Off\t", envOf(nil), Off, "setting"},
		{"setting unknown word falls through", "sometimes", envOf(nil), On, "default"},
		{"setting blank falls through", "   ", envOf(nil), On, "default"},
		{"env beats setting", "off", envOf(map[string]string{"CODEAF_MODEL_POOL": "on"}), On, "env"},
	}
	for _, c := range cases {
		got := Resolve(c.setting, c.lookup)
		if got.Mode != c.want || got.Source.Mode != c.wantSrc {
			t.Errorf("%s: Mode = %v from %q, want %v from %q", c.name, got.Mode, got.Source.Mode, c.want, c.wantSrc)
		}
	}
}

func TestResolveModeFromCI(t *testing.T) {
	cases := []struct {
		name    string
		setting string
		lookup  func(string) (string, bool)
		want    Mode
		wantSrc string
	}{
		{"ci true", "", envOf(map[string]string{"CI": "true"}), Read, "ci"},
		{"ci one", "", envOf(map[string]string{"CI": "1"}), Read, "ci"},
		{"ci yes", "", envOf(map[string]string{"CI": "yes"}), Read, "ci"},
		{"ci padded and mixed case", "", envOf(map[string]string{"CI": "  YES "}), Read, "ci"},
		{"ci false is not an answer", "", envOf(map[string]string{"CI": "false"}), On, "default"},
		{"ci zero is not an answer", "", envOf(map[string]string{"CI": "0"}), On, "default"},
		{"ci no is not an answer", "", envOf(map[string]string{"CI": "no"}), On, "default"},
		{"ci set and empty is not an answer", "", envOf(map[string]string{"CI": ""}), On, "default"},
		{"ci reads when the setting is unknown", "maybe", envOf(map[string]string{"CI": "true"}), Read, "ci"},
		{"ci reads when the setting is blank", "  ", envOf(map[string]string{"CI": "1"}), Read, "ci"},
		{"setting beats ci", "off", envOf(map[string]string{"CI": "true"}), Off, "setting"},
		{"env on beats ci", "off", envOf(map[string]string{"CI": "true", "CODEAF_MODEL_POOL": "on"}), On, "env"},
		{"env off beats ci", "on", envOf(map[string]string{"CI": "true", "CODEAF_MODEL_POOL": "off"}), Off, "env"},
		{"env unknown and setting unknown leave ci", "maybe", envOf(map[string]string{"CI": "true", "CODEAF_MODEL_POOL": "maybe"}), Read, "ci"},
	}
	for _, c := range cases {
		got := Resolve(c.setting, c.lookup)
		if got.Mode != c.want || got.Source.Mode != c.wantSrc {
			t.Errorf("%s: Mode = %v from %q, want %v from %q", c.name, got.Mode, got.Source.Mode, c.want, c.wantSrc)
		}
	}
}

func TestResolveIndexURL(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		set     bool
		want    string
		wantSrc string
	}{
		{"nobody set", "", false, DefaultIndexURL, "default"},
		{"https", "https://pool.example.com/index.json", true, "https://pool.example.com/index.json", "env"},
		{"https with port", "https://pool.example.com:8443/i", true, "https://pool.example.com:8443/i", "env"},
		{"https scheme in upper case", "HTTPS://pool.example.com", true, "HTTPS://pool.example.com", "env"},
		{"http localhost", "http://localhost", true, "http://localhost", "env"},
		{"http localhost with port", "http://localhost:8080", true, "http://localhost:8080", "env"},
		{"http localhost in upper case", "http://LOCALHOST", true, "http://LOCALHOST", "env"},
		{"http loopback", "http://127.0.0.1", true, "http://127.0.0.1", "env"},
		{"http loopback with port and path", "http://127.0.0.1:9000/pool.json", true, "http://127.0.0.1:9000/pool.json", "env"},
		{"file", "file:///var/tmp/pool.json", true, "file:///var/tmp/pool.json", "env"},
		{"absolute path", "/var/tmp/pool.json", true, "/var/tmp/pool.json", "env"},
		{"padded", "  https://pool.example.com  ", true, "https://pool.example.com", "env"},
		{"plain http elsewhere", "http://pool.example.com", true, DefaultIndexURL, "default"},
		{"plain http elsewhere with port", "http://pool.example.com:8080", true, DefaultIndexURL, "default"},
		{"http host that merely contains localhost", "http://localhost.example.com", true, DefaultIndexURL, "default"},
		{"http loopback that is not 127.0.0.1", "http://127.0.0.2", true, DefaultIndexURL, "default"},
		// The host is the host the request would reach, so a loopback name in
		// the userinfo or as a label of somebody else's name is not one. These
		// rows pin the read: a shape check written against the raw text rather
		// than the parsed host would take all three.
		{"http loopback name in the userinfo", "http://localhost@pool.example.com", true, DefaultIndexURL, "default"},
		{"http loopback name and port in the userinfo", "http://localhost:80@pool.example.com", true, DefaultIndexURL, "default"},
		{"http loopback address as a leading label", "http://127.0.0.1.pool.example.com", true, DefaultIndexURL, "default"},
		{"other scheme", "ftp://pool.example.com", true, DefaultIndexURL, "default"},
		{"scheme without the double slash", "https:pool.example.com", true, DefaultIndexURL, "default"},
		{"http without the double slash", "http:/localhost", true, DefaultIndexURL, "default"},
		{"bare host", "pool.example.com/index.json", true, DefaultIndexURL, "default"},
		{"relative path", "tmp/pool.json", true, DefaultIndexURL, "default"},
		{"dot relative path", "./pool.json", true, DefaultIndexURL, "default"},
		{"set and empty", "", true, DefaultIndexURL, "default"},
	}
	for _, c := range cases {
		lookup := envOf(nil)
		if c.set {
			lookup = envOf(map[string]string{"CODEAF_MODEL_POOL_URL": c.value})
		}
		got := Resolve("", lookup)
		if got.IndexURL != c.want || got.Source.IndexURL != c.wantSrc {
			t.Errorf("%s: IndexURL = %q from %q, want %q from %q", c.name, got.IndexURL, got.Source.IndexURL, c.want, c.wantSrc)
		}
	}
}

func TestResolveSubmitURL(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		set     bool
		want    string
		wantSrc string
	}{
		{"nobody set", "", false, DefaultSubmitURL, "default"},
		{"https", "https://submit.example.com", true, "https://submit.example.com", "env"},
		{"http loopback with port", "http://127.0.0.1:9000/submit", true, "http://127.0.0.1:9000/submit", "env"},
		{"file", "file:///tmp/submit.json", true, "file:///tmp/submit.json", "env"},
		{"absolute path", "/tmp/submit.json", true, "/tmp/submit.json", "env"},
		{"empty sends nowhere", "", true, "", "env"},
		{"blank sends nowhere", "   ", true, "", "env"},
		{"plain http elsewhere", "http://submit.example.com", true, DefaultSubmitURL, "default"},
		{"path that is not absolute", "submit.json", true, DefaultSubmitURL, "default"},
		{"other scheme", "ftp://submit.example.com", true, DefaultSubmitURL, "default"},
	}
	for _, c := range cases {
		lookup := envOf(nil)
		if c.set {
			lookup = envOf(map[string]string{"CODEAF_MODEL_POOL_SUBMIT_URL": c.value})
		}
		got := Resolve("", lookup)
		if got.SubmitURL != c.want || got.Source.SubmitURL != c.wantSrc {
			t.Errorf("%s: SubmitURL = %q from %q, want %q from %q", c.name, got.SubmitURL, got.Source.SubmitURL, c.want, c.wantSrc)
		}
	}
}

func TestResolveTTL(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		set     bool
		want    time.Duration
		wantSrc string
	}{
		{"nobody set", "", false, 24 * time.Hour, "default"},
		{"readable", "2h45m", true, 2*time.Hour + 45*time.Minute, "env"},
		{"padded", " 3h ", true, 3 * time.Hour, "env"},
		{"under the floor", "30s", true, time.Minute, "env"},
		{"at the floor", "1m", true, time.Minute, "env"},
		{"negative", "-1h", true, time.Minute, "env"},
		{"zero", "0", true, time.Minute, "env"},
		{"over the ceiling", "1000h", true, 7 * 24 * time.Hour, "env"},
		{"at the ceiling", "168h", true, 7 * 24 * time.Hour, "env"},
		{"unreadable", "banana", true, 24 * time.Hour, "default"},
		{"no day unit", "7d", true, 24 * time.Hour, "default"},
		{"set and empty", "", true, 24 * time.Hour, "default"},
	}
	for _, c := range cases {
		lookup := envOf(nil)
		if c.set {
			lookup = envOf(map[string]string{"CODEAF_MODEL_POOL_TTL": c.value})
		}
		got := Resolve("", lookup)
		if got.TTL != c.want || got.Source.TTL != c.wantSrc {
			t.Errorf("%s: TTL = %v from %q, want %v from %q", c.name, got.TTL, got.Source.TTL, c.want, c.wantSrc)
		}
	}
}

func TestResolveFieldsAreIndependent(t *testing.T) {
	// Off, an unreadable URL and a readable TTL: the mode does not change how
	// the URLs or the TTL are read, and the unreadable URL does not disturb
	// the TTL.
	got := Resolve("bogus", envOf(map[string]string{
		"CODEAF_MODEL_POOL":            "off",
		"CODEAF_MODEL_POOL_URL":        "http://pool.example.com",
		"CODEAF_MODEL_POOL_SUBMIT_URL": "https://submit.example.com",
		"CODEAF_MODEL_POOL_TTL":        "2h",
	}))
	want := Config{
		Mode:      Off,
		IndexURL:  DefaultIndexURL,
		SubmitURL: "https://submit.example.com",
		TTL:       2 * time.Hour,
		Source: Sources{
			Mode:      "env",
			IndexURL:  "default",
			SubmitURL: "env",
			TTL:       "env",
		},
	}
	if got != want {
		t.Errorf("Resolve() = %+v, want %+v", got, want)
	}

	// An unreadable TTL does not disturb the URLs either.
	got = Resolve("", envOf(map[string]string{
		"CODEAF_MODEL_POOL_URL": "https://pool.example.com",
		"CODEAF_MODEL_POOL_TTL": "banana",
	}))
	if got.IndexURL != "https://pool.example.com" || got.Source.IndexURL != "env" {
		t.Errorf("IndexURL = %q from %q, want the env value from %q", got.IndexURL, got.Source.IndexURL, "env")
	}
	if got.TTL != 24*time.Hour || got.Source.TTL != "default" {
		t.Errorf("TTL = %v from %q, want 24h from %q", got.TTL, got.Source.TTL, "default")
	}
}

func TestCanSendAndCanRead(t *testing.T) {
	cases := []struct {
		name        string
		config      Config
		wantCanSend bool
		wantCanRead bool
	}{
		{"on with an address", Config{Mode: On, SubmitURL: DefaultSubmitURL}, true, true},
		{"on with no address", Config{Mode: On}, false, true},
		{"read with an address", Config{Mode: Read, SubmitURL: DefaultSubmitURL}, false, true},
		{"read with no address", Config{Mode: Read}, false, true},
		{"off with an address", Config{Mode: Off, SubmitURL: DefaultSubmitURL}, false, false},
	}
	for _, c := range cases {
		if got := c.config.CanSend(); got != c.wantCanSend {
			t.Errorf("%s: CanSend() = %v, want %v", c.name, got, c.wantCanSend)
		}
		if got := c.config.CanRead(); got != c.wantCanRead {
			t.Errorf("%s: CanRead() = %v, want %v", c.name, got, c.wantCanRead)
		}
	}
}

func TestResolvedConfigCanSendAndCanRead(t *testing.T) {
	// An empty submit address turns sending off while everything else stays on.
	got := Resolve("", envOf(map[string]string{"CODEAF_MODEL_POOL_SUBMIT_URL": ""}))
	if got.SubmitURL != "" || got.Source.SubmitURL != "env" {
		t.Fatalf("SubmitURL = %q from %q, want %q from %q", got.SubmitURL, got.Source.SubmitURL, "", "env")
	}
	if got.CanSend() {
		t.Error("an empty submit address must not send")
	}
	if !got.CanRead() {
		t.Error("an empty submit address must still read")
	}

	if got := Resolve("", envOf(nil)); !got.CanSend() || !got.CanRead() {
		t.Errorf("the defaults must send and read: CanSend() = %v, CanRead() = %v", got.CanSend(), got.CanRead())
	}
	if got := Resolve("read", envOf(nil)); got.CanSend() || !got.CanRead() {
		t.Errorf("read must read and not send: CanSend() = %v, CanRead() = %v", got.CanSend(), got.CanRead())
	}
	if got := Resolve("off", envOf(nil)); got.CanSend() || got.CanRead() {
		t.Errorf("off must neither send nor read: CanSend() = %v, CanRead() = %v", got.CanSend(), got.CanRead())
	}
}

func TestResolveNilLookupUsesSetting(t *testing.T) {
	got := Resolve("read", nil)
	if got.Mode != Read || got.Source.Mode != "setting" {
		t.Errorf("Resolve(\"read\", nil): Mode = %v from %q, want read from %q", got.Mode, got.Source.Mode, "setting")
	}
	if got.IndexURL != DefaultIndexURL || got.SubmitURL != DefaultSubmitURL || got.TTL != 24*time.Hour {
		t.Errorf("Resolve(\"read\", nil) = %+v, want the defaults beside the setting", got)
	}
}

func TestResolveAsksFiveNames(t *testing.T) {
	var asked []string
	Resolve("", func(name string) (string, bool) {
		asked = append(asked, name)
		return "", false
	})
	sort.Strings(asked)
	want := []string{envMode, envCI, envIndex, envSubmit, envTTL}
	sort.Strings(want)
	if !reflect.DeepEqual(asked, want) {
		t.Errorf("Resolve asked lookup for %v, want %v", asked, want)
	}
}

func TestResolveIsPure(t *testing.T) {
	first := envOf(map[string]string{"CODEAF_MODEL_POOL": "read", "CODEAF_MODEL_POOL_TTL": "3h"})
	second := envOf(map[string]string{"CODEAF_MODEL_POOL": "off"})
	want := Resolve("", first)
	Resolve("", second)
	if got := Resolve("", first); got != want {
		t.Errorf("a second call kept state: %+v, want %+v", got, want)
	}
	if got := Resolve("", second); got.Mode != Off || got.TTL != 24*time.Hour {
		t.Errorf("a fresh environment was polluted: %+v", got)
	}
}

func TestResolveConcurrent(t *testing.T) {
	lookup := envOf(map[string]string{
		"CODEAF_MODEL_POOL":     "read",
		"CODEAF_MODEL_POOL_TTL": "90m",
	})
	want := Resolve("", lookup)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 64; j++ {
				if got := Resolve("", lookup); got != want {
					t.Errorf("Resolve() = %+v, want %+v", got, want)
					return
				}
			}
		}()
	}
	wg.Wait()
}
