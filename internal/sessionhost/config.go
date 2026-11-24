package sessionhost

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/executor"
)

const (
	minSessionUID = 1000
	maxSessionUID = 1 << 31
	modeFenced    = "uid+fence"
	modeUID       = "uid"
	modeRefused   = "refused"
)

// Config holds the operator settings for one supervisor.
type Config struct {
	Listen       string
	Secret       string `json:"-"`
	Data         string
	UIDBase      int
	UIDCount     int
	MaxRunning   int
	MaxPerPerson int
	Idle         time.Duration
	NProc        int
	Require      string
	Model        string
	ForceProbe   string
	Engine       string
	// LimitPorts says a chat's commands may connect only to ConnectPorts and to
	// the ports of the organization's own addresses (the model door and git,
	// which a chat's credentials name). It is set by ConfigFromEnv and left
	// false by a test that builds a Config by hand, which keeps the engine's
	// environment exactly what it was before the limit existed. It has no
	// effect in the process-only mode (REQUIRE=uid), where no command is fenced.
	LimitPorts   bool
	ConnectPorts []int
}

// DefaultConnectPorts are the TCP ports a chat's commands may reach unless the
// operator says otherwise: HTTPS and HTTP for packages and web pages, SSH and
// the git protocol for repositories. Landlock limits connections by port and
// not by address, so this keeps a chat off the other services of the machine's
// private network (an agent's API, a model stub) and not off a hostile service
// that happens to listen on one of these.
const DefaultConnectPorts = "443,80,22,9418"

// ConfigFromEnv reads the supervisor's own settings without sourcing a profile.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	value := func(name, fallback string) string {
		if v := getenv("CODEAF_SESSIONHOST_" + name); v != "" {
			return v
		}
		return fallback
	}
	cfg := Config{
		Listen:     value("LISTEN", "0.0.0.0:8790"),
		Secret:     value("SECRET", ""),
		Data:       value("DATA", "/chats"),
		Require:    value("REQUIRE", modeFenced),
		Model:      value("MODEL", ""),
		ForceProbe: value("FORCE_PROBE", ""),
		Engine:     value("ENGINE", ""),
	}
	if err := checkSocketRoom(cfg.Data); err != nil {
		return Config{}, err
	}
	if utf8.RuneCountInString(cfg.Secret) < 32 {
		return Config{}, fmt.Errorf("CODEAF_SESSIONHOST_SECRET must be at least 32 characters long")
	}
	for _, entry := range []struct {
		name     string
		fallback string
		dest     *int
	}{
		{"UID_BASE", "200000", &cfg.UIDBase},
		{"UID_COUNT", "10000", &cfg.UIDCount},
		{"MAX_RUNNING", "20", &cfg.MaxRunning},
		{"MAX_PER_PERSON", "3", &cfg.MaxPerPerson},
		{"NPROC", "512", &cfg.NProc},
	} {
		n, err := strconv.Atoi(value(entry.name, entry.fallback))
		if err != nil || n < 1 {
			return Config{}, fmt.Errorf("CODEAF_SESSIONHOST_%s must be a positive integer", entry.name)
		}
		*entry.dest = n
	}
	if !validUIDRange(cfg.UIDBase, cfg.UIDCount) {
		return Config{}, fmt.Errorf("CODEAF_SESSIONHOST_UID_BASE and CODEAF_SESSIONHOST_UID_COUNT must define a range from 1000 up to 2147483647")
	}
	idle, err := time.ParseDuration(value("IDLE", "30m"))
	if err != nil || idle < 5*time.Second {
		return Config{}, fmt.Errorf("CODEAF_SESSIONHOST_IDLE must be a duration of at least 5s")
	}
	cfg.Idle = idle
	if cfg.Require != modeFenced && cfg.Require != modeUID {
		return Config{}, fmt.Errorf("CODEAF_SESSIONHOST_REQUIRE must be uid+fence or uid")
	}
	ports, err := connectPortsSetting(value("CONNECT_PORTS", DefaultConnectPorts))
	if err != nil {
		return Config{}, err
	}
	cfg.LimitPorts, cfg.ConnectPorts = true, ports
	return cfg, nil
}

// connectPortsSetting reads CODEAF_SESSIONHOST_CONNECT_PORTS: a list of TCP
// ports, or the word none for the organization's own ports alone. The error
// names the setting and the entry at fault.
func connectPortsSetting(text string) ([]int, error) {
	if strings.EqualFold(strings.TrimSpace(text), "none") {
		return nil, nil
	}
	ports, err := executor.ParseConnectPorts(text)
	if err != nil || len(ports) == 0 {
		if err == nil {
			err = fmt.Errorf("it names no port")
		}
		return nil, fmt.Errorf("CODEAF_SESSIONHOST_CONNECT_PORTS must be a list of TCP ports such as %s, or none: %v", DefaultConnectPorts, err)
	}
	return ports, nil
}

func validUIDRange(base, count int) bool {
	return base >= minSessionUID && int64(base) < maxSessionUID &&
		count >= 1 && int64(count) <= maxSessionUID-int64(base)
}

func (c Config) ownsUID(uid int) bool {
	return uid >= c.UIDBase && uid-c.UIDBase < c.UIDCount && uid >= minSessionUID
}
