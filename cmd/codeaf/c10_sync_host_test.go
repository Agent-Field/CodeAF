package main

import (
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/remote"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// C10 requires a changed relay setting to be visible even to a launch finding a real existing host.
func TestC10SyncOffMustBeSeenByExistingEngineHost(t *testing.T) {
	shortEngineHome(t)
	workspace := t.TempDir()
	t.Setenv("CODEAF_SYNC_URL", "http://127.0.0.1:41001")
	child := environmentChild(t, workspace, os.Getenv(config.APIKeyEnv))
	_ = child
	waitEnvironmentHost(t, workspace, func(self remote.HostSelf) bool { return !self.Busy })
	t.Setenv("CODEAF_SYNC_URL", "off")
	if !enginehost.EnvironmentDiffers(workspace) {
		t.Fatal("real engine host retains its old sync URL, but the new off launch sees no environment difference and reuses it")
	}
}

// C10 also applies during an upgrade: an older host with no sync fingerprint
// must not keep its old relay when this terminal explicitly chooses off.
func TestC10SyncOffDoesNotReuseAHostWithoutSyncFingerprint(t *testing.T) {
	for _, busy := range []bool{false, true} {
		t.Run(fmt.Sprintf("busy=%t", busy), func(t *testing.T) {
			workspace, _ := environmentTestHome(t)
			t.Setenv("CODEAF_SYNC_URL", "http://127.0.0.1:41001")
			environmentChild(t, workspace, os.Getenv(config.APIKeyEnv))
			if busy {
				environmentWindow(t, workspace, "")
			}
			waitEnvironmentHost(t, workspace, func(self remote.HostSelf) bool { return self.Busy == busy })
			dir, err := enginehost.Dir(workspace)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(dir, "sync-environment")); err != nil {
				t.Fatal(err)
			}
			t.Setenv("CODEAF_SYNC_URL", "off")
			_, err = clearStaleEngineHost(workspace)
			if busy {
				var refused *staleHost
				if !errors.As(err, &refused) {
					t.Fatalf("unconfirmed old sync setting was reused: %v", err)
				}
			} else if err != nil || !nothingHolds(workspace) {
				t.Fatalf("idle old host was reused: %v", err)
			}
		})
	}
}

// C10 requires an idle host to retire for a relay change and a busy one to
// refuse attachment, including when the existing host is another build.
func TestC10ChangedSyncSettingRetiresIdleAndRefusesBusyHost(t *testing.T) {
	for _, target := range []string{"http://127.0.0.1:41002", "off"} {
		for _, busy := range []bool{false, true} {
			for _, age := range []string{"same", "older", "newer"} {
				t.Run(fmt.Sprintf("%s/busy=%t/%s", target, busy, age), func(t *testing.T) {
					workspace, _ := environmentTestHome(t)
					t.Setenv("CODEAF_SYNC_URL", "http://127.0.0.1:41001")
					child := environmentChild(t, workspace, os.Getenv(config.APIKeyEnv))
					if busy {
						environmentWindow(t, workspace, "")
					}
					before := waitEnvironmentHost(t, workspace, func(self remote.HostSelf) bool { return self.Busy == busy })
					me := thisEngineBuild()
					if age != "same" {
						me.Build = "another-build"
						me.BuiltAt = before.BuiltAt.Add(time.Hour)
						if age == "newer" {
							me.BuiltAt = before.BuiltAt.Add(-time.Hour)
						}
					}
					t.Setenv("CODEAF_SYNC_URL", target)
					note, err := clearStaleEngineHostAs(workspace, me)
					if busy {
						var refused *staleHost
						if !errors.As(err, &refused) || err.Error() != differentEngineEnvironmentSentence(workspace) {
							t.Fatalf("busy mismatched host allowed attachment: note=%q err=%v", note, err)
						}
						self, err := enginehost.Ask(workspace, remote.WhoIs{})
						if err != nil || self.PID != child.Process.Pid {
							t.Fatalf("busy host was stopped: %+v %v", self, err)
						}
					} else if err != nil || !strings.Contains(note, "restarted") || !nothingHolds(workspace) {
						t.Fatalf("idle mismatched host was reused: note=%q err=%v", note, err)
					}
				})
			}
		}
	}
}
