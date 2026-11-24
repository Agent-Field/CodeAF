package sessionhost

import (
	"fmt"
	"strconv"
	"time"
	"unicode/utf8"
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
}

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
		Data:       value("DATA", "/sessions"),
		Require:    value("REQUIRE", modeFenced),
		Model:      value("MODEL", ""),
		ForceProbe: value("FORCE_PROBE", ""),
		Engine:     value("ENGINE", ""),
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
	return cfg, nil
}

func validUIDRange(base, count int) bool {
	return base >= minSessionUID && int64(base) < maxSessionUID &&
		count >= 1 && int64(count) <= maxSessionUID-int64(base)
}

func (c Config) ownsUID(uid int) bool {
	return uid >= c.UIDBase && uid-c.UIDBase < c.UIDCount && uid >= minSessionUID
}
