// Package service keeps the processes a person agreed to keep running.
//
// A kept-running service is one exact shell command in one exact folder, with
// one health check, that a person said yes to. It runs in its own detached
// process group so it outlives the conversation that asked for it, the codeaf
// that started it and that codeaf crashing. A later codeaf takes it back only
// when it can PROVE the process is the one that was started — the leader's
// pid, its start identity and the boot it was recorded in
// (internal/processgroup) — and otherwise says it is not running and does
// nothing to whatever holds that pid now.
//
// The package is the engine's and nothing else's: it knows commands, folders,
// checks, policies and owners, and it does not know conversations, models,
// cards or windows. A conversation proposes a [Spec] and a person's answer
// decides its [Policy]; the [Supervisor] is the one owner of the timer, the
// probes, the restarts and the file, for every service in this codeaf.
//
// The resident's services (internal/resident) are a different product with a
// different store. The two share only the process-group primitives underneath.
package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// The rhythm every service is checked on. They are the board's numbers and
// they are not settings: a check the agent could tune is a check it could make
// lenient.
const (
	// CheckEvery is how often a running service is checked, and how long after
	// a start the first check comes.
	CheckEvery = 10 * time.Second
	// CheckTimeout is how long one check may take before it counts as failed.
	CheckTimeout = 5 * time.Second
	// UnhealthyAfter is how many failed checks in a row make a service
	// unhealthy. One passing check makes it healthy again.
	UnhealthyAfter = 3
	// RestartLimit is how many automatic restarts fit in RestartWindow. The
	// next failure inside the window gives up.
	RestartLimit = 3
	// RestartWindow is the sliding window restarts are counted in, and how
	// long a service has to stay healthy for its count to start again at 0.
	RestartWindow = 10 * time.Minute
	// StopGrace is how long SIGTERM has before SIGKILL follows.
	StopGrace = 5 * time.Second
	// FirstBackoff is the wait before the first automatic restart; each later
	// one in the window doubles it: 2s, 4s, 8s.
	FirstBackoff = 2 * time.Second
)

// The bounds of what a service may say about itself. They keep the file, the
// rows and the events small whatever the agent proposed.
const (
	maxCommand     = 4096
	maxHealthCmd   = 1024
	maxURL         = 2048
	maxName        = 64
	maxProvenance  = 160
	maxReason      = 200
	maxOwner       = 128
	maxServices    = 64
	maxPerOwner    = 16
	maxConsent     = 256
	maxProbeDetail = 160
)

// Policy is what happens when a service fails. Only a person sets it.
type Policy string

const (
	// PolicyNever restarts nothing: a failure shows its state and one line.
	PolicyNever Policy = "never"
	// PolicyOnFailure restarts after a non-zero exit or UnhealthyAfter failed
	// checks in a row, up to RestartLimit times in RestartWindow.
	PolicyOnFailure Policy = "on_failure"
	// PolicyAlways is PolicyOnFailure that also restarts after exit 0.
	PolicyAlways Policy = "always"
)

// Valid reports whether p is one of the three policies.
func (p Policy) Valid() bool {
	return p == PolicyNever || p == PolicyOnFailure || p == PolicyAlways
}

// ParsePolicy reads a policy, with "" meaning on_failure — the board's default
// proposal.
func ParsePolicy(text string) (Policy, error) {
	switch p := Policy(strings.TrimSpace(text)); {
	case p == "":
		return PolicyOnFailure, nil
	case p.Valid():
		return p, nil
	}
	return "", fmt.Errorf("restart policy is never, on_failure or always, not %q", text)
}

// HealthKind is which of the three checks a service has.
type HealthKind string

const (
	HealthPort HealthKind = "port"
	HealthURL  HealthKind = "url"
	HealthCmd  HealthKind = "cmd"
)

// Health is a service's one check: a port that accepts a connection, a URL that
// answers 200, or a command that exits 0 in the service's folder. Exactly one
// of Port, URL and Cmd is set, and Kind says which.
type Health struct {
	Kind HealthKind `json:"kind"`
	Port int        `json:"port,omitempty"`
	URL  string     `json:"url,omitempty"`
	Cmd  string     `json:"cmd,omitempty"`
}

// Words is the check as the row says it: "port 5173", "URL http://…",
// "command pg_isready".
func (h Health) Words() string {
	switch h.Kind {
	case HealthPort:
		return "port " + strconv.Itoa(h.Port)
	case HealthURL:
		return "URL " + h.URL
	case HealthCmd:
		return "command " + h.Cmd
	}
	return ""
}

// OpenURL is the address a person may open for this check: the URL itself, a
// port's local address, and nothing for a command check, whose service may not
// speak HTTP at all.
func (h Health) OpenURL() string {
	switch h.Kind {
	case HealthPort:
		return "http://localhost:" + strconv.Itoa(h.Port) + "/"
	case HealthURL:
		return h.URL
	}
	return ""
}

// Check validates one health check. The fields are typed and checked one by
// one, so nothing the agent wrote is ever concatenated into a shell line: a
// port is a number, a URL is parsed and must be plain http(s) with no
// credentials in it, and a command is run whole as the person approved it.
func (h Health) Check() (Health, error) {
	set := 0
	if h.Port != 0 {
		set++
	}
	if strings.TrimSpace(h.URL) != "" {
		set++
	}
	if strings.TrimSpace(h.Cmd) != "" {
		set++
	}
	if set != 1 {
		return Health{}, errors.New("a kept-running process has exactly one health check: a port, a URL or a command")
	}
	switch {
	case h.Port != 0:
		if h.Port < 1 || h.Port > 65535 {
			return Health{}, fmt.Errorf("port %d is not a port", h.Port)
		}
		return Health{Kind: HealthPort, Port: h.Port}, nil
	case strings.TrimSpace(h.URL) != "":
		raw := strings.TrimSpace(h.URL)
		if len(raw) > maxURL || strings.ContainsAny(raw, "\x00\r\n\t ") {
			return Health{}, errors.New("the health URL is not one plain address")
		}
		parsed, err := url.Parse(raw)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return Health{}, errors.New("the health URL must be an http or https address")
		}
		// A credential in the URL would ride every row, event and log line that
		// names the check, so a URL carrying one is refused rather than kept.
		if parsed.User != nil {
			return Health{}, errors.New("the health URL may not carry a user name or password")
		}
		return Health{Kind: HealthURL, URL: parsed.String()}, nil
	default:
		cmd := strings.TrimSpace(h.Cmd)
		if len(cmd) > maxHealthCmd || strings.ContainsRune(cmd, 0) || !utf8.ValidString(cmd) {
			return Health{}, errors.New("the health command is too long or not text")
		}
		return Health{Kind: HealthCmd, Cmd: cmd}, nil
	}
}

// Spec is what an agent proposes: the exact command, the exact folder, a short
// name and one check.
type Spec struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Dir     string `json:"dir"`
	Health  Health `json:"health"`
}

// Check validates a spec and returns it in its one canonical spelling, which
// is what consent is keyed on and what is run.
func (s Spec) Check() (Spec, error) {
	command := strings.TrimSpace(s.Command)
	if command == "" {
		return Spec{}, errors.New("a kept-running process needs a command")
	}
	if len(command) > maxCommand || strings.ContainsRune(command, 0) || !utf8.ValidString(command) {
		return Spec{}, errors.New("the command is too long or not text")
	}
	// THE FOLDER IS THE PATH EXACTLY AS IT IS ON DISK. A folder name may end
	// in a space, and trimming one would run the command somewhere else — or
	// nowhere. Clean only folds separators and dot segments.
	dir := s.Dir
	if dir == "" || !filepath.IsAbs(dir) || strings.ContainsRune(dir, 0) || len(dir) > maxCommand {
		return Spec{}, errors.New("a kept-running process needs an absolute folder")
	}
	health, err := s.Health.Check()
	if err != nil {
		return Spec{}, err
	}
	name := oneLine(s.Name, maxName)
	if name == "" {
		name = oneLine(firstWords(command), maxName)
	}
	return Spec{Name: name, Command: command, Dir: filepath.Clean(dir), Health: health}, nil
}

// ConsentKey names what a person is asked about once: this command, in this
// folder. The yes banked under it also holds the check and the policy they
// saw ([Supervisor.Approved]); a proposal that changes the check is asked
// about again, because a check command is a second command run every ten
// seconds and a moved port or URL is a different promise about the service.
func (s Spec) ConsentKey() string {
	sum := sha256.New()
	sum.Write([]byte(s.Command))
	sum.Write([]byte{0})
	sum.Write([]byte(s.Dir))
	return hex.EncodeToString(sum.Sum(nil))
}

// oneLine is text cut to one line of at most limit runes.
func oneLine(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}

// firstWords is a name for a command that came with none: its first two words.
func firstWords(command string) string {
	line, _, _ := strings.Cut(command, "\n")
	fields := strings.Fields(line)
	if len(fields) > 2 {
		fields = fields[:2]
	}
	return strings.Join(fields, " ")
}
