package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/home"
)

// HEALTH IS FACTS: the build, how long this engine has run, a reading for the
// work folder's filesystem and codeaf's home, the local factory's state, and,
// once a start carried on what the last quit stopped, when and how much.
func TestAppHealthCheckSaysTheEnginesFacts(t *testing.T) {
	useLocalHome(t)
	b := newFakeBackend(newAppRecorder(), nil)
	defer b.Close()
	work := t.TempDir()
	b.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	if err := b.state.write(func(f *appStateFile) error { f.WorkFolder = work; return nil }); err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	b.startedAt = started
	b.now = func() time.Time { return started.Add(90 * time.Second) }
	health, err := b.HealthCheck(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if health.Version == "" || !health.StartedAt.Equal(started) || health.UptimeS != 90 || health.Resume != nil {
		t.Fatalf("health reads %+v", health)
	}
	if health.Factory == nil || health.Factory.State != appwire.LocalStopped {
		t.Fatalf("the factory reads %+v", health.Factory)
	}
	want := map[string]string{appwire.DiskWorkFolder: work, appwire.DiskCodeafHome: home.Dir()}
	if len(health.Disks) != len(want) {
		t.Fatalf("disks read %+v", health.Disks)
	}
	for _, disk := range health.Disks {
		if want[disk.Role] != disk.Path || disk.TotalBytes == 0 || disk.Error != "" {
			t.Fatalf("disk reads %+v", disk)
		}
	}

	if err := b.state.write(func(f *appStateFile) error {
		f.ResumeAfterQuit, f.QuitReplies, f.QuitTasks = []string{"a1", "a2"}, []string{"a1"}, []string{"a2/t1"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	b.resumeAfterQuit()
	health, err = b.HealthCheck(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r := health.Resume; r == nil || !r.RepliesResumed || r.Replies != 1 || r.Tasks != 1 || !r.At.Equal(started.Add(90*time.Second)) {
		t.Fatalf("the resume reads %+v", health.Resume)
	}
}
