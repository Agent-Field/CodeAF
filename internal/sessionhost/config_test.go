package sessionhost

import (
	"strings"
	"testing"
	"time"
)

func TestConfigFromEnv(t *testing.T) {
	secret := strings.Repeat("S", 32)
	values := map[string]string{"CODEAF_SESSIONHOST_SECRET": secret}
	cfg, err := ConfigFromEnv(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "0.0.0.0:8790" || cfg.Data != "/chats" ||
		cfg.UIDBase != 200000 || cfg.UIDCount != 10000 || cfg.MaxRunning != 20 ||
		cfg.MaxPerPerson != 3 || cfg.Idle != 30*time.Minute || cfg.NProc != 512 ||
		cfg.Require != "uid+fence" || cfg.Secret != secret {
		t.Fatal("incorrect default configuration")
	}
	for key, value := range map[string]string{
		"SECRET": "short", "UID_BASE": "999", "UID_COUNT": "2147483648",
		"MAX_RUNNING": "0", "MAX_PER_PERSON": "-1", "IDLE": "4s",
		"NPROC": "0", "REQUIRE": "open",
	} {
		t.Run(key, func(t *testing.T) {
			_, err := ConfigFromEnv(func(name string) string {
				if name == "CODEAF_SESSIONHOST_"+key {
					return value
				}
				return values[name]
			})
			if err == nil || strings.Contains(err.Error(), secret) {
				t.Fatalf("expected a configuration error with no secret for %s", key)
			}
		})
	}
}

// C5: a chat's engine can only outlive its connection if its socket path fits,
// so a data folder that leaves no room is refused at start, with the room said.
func TestC5_DataFolderLeavesRoomForTheEngineSocket(t *testing.T) {
	secret := strings.Repeat("S", 32)
	configure := func(data string) error {
		_, err := ConfigFromEnv(func(name string) string {
			switch name {
			case "CODEAF_SESSIONHOST_SECRET":
				return secret
			case "CODEAF_SESSIONHOST_DATA":
				return data
			}
			return ""
		})
		return err
	}
	if err := configure("/chats"); err != nil {
		t.Fatalf("the default folder must leave room: %v", err)
	}
	err := configure("/sessions")
	if err == nil || !strings.Contains(err.Error(), "at most 8 characters") {
		t.Fatalf("a folder one byte too long was accepted or unexplained: %v", err)
	}
	if err := configure("/" + strings.Repeat("d", 120)); err == nil {
		t.Fatal("a very deep folder was accepted")
	}
}
