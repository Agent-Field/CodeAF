package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// These tests are the desk's Remote control switch (contract R1–R8 in
// NOTES-POLISH): the row the terminal's Settings > Devices tab writes, reached
// through settings.get and settings.set, announced by one hello word, and
// taking effect as the contract says. They never set CODEAF_REMOTE_CONTROL to
// a value except where the environment's own pin is the thing under test,
// because a set variable would make the row read only and the switch
// untestable.

// R1, R2, R3, R7: over the real wire, a fresh desk engine announces the switch,
// reads it off, turns it on and off through settings.set, and says
// settings.changed with the new value each time. A value that is not a plain
// true or false is refused and leaves the row alone.
func TestAppRemoteControlSwitchIsAnnouncedReadAndWrittenOverTheWire(t *testing.T) {
	s := newLocalSetup(t)
	t.Setenv("CODEAF_REMOTE_CONTROL", "")
	s.backend.emit = s.server
	profile := config.ProfileDir()
	if !strings.Contains(s.hello, appwire.FeatureSettingsRemoteControl) {
		t.Fatalf("the desk engine's hello omits %s: %s", appwire.FeatureSettingsRemoteControl, s.hello)
	}
	read := func(method string, params any, want bool) {
		t.Helper()
		var reply struct {
			Settings struct {
				RemoteControl *bool  `json:"remote_control"`
				PinnedBy      string `json:"remote_control_pinned_by"`
			} `json:"settings"`
		}
		s.must(method, params, &reply)
		got := reply.Settings.RemoteControl
		if got == nil || *got != want || reply.Settings.PinnedBy != "" {
			t.Fatalf("%s says remote_control=%v pinned_by=%q, want %v and no pin", method, got, reply.Settings.PinnedBy, want)
		}
	}
	read("settings.get", struct{}{}, false)
	if config.RemoteControlAt(profile) {
		t.Fatal("a fresh home has remote control on")
	}
	read("settings.set", map[string]any{"remote_control": true}, true)
	if !config.RemoteControlAt(profile) || settingsRow(profile, config.KeyRemoteControl) != "on" {
		t.Fatalf("turning it on wrote %q, and the engine reads %v", settingsRow(profile, config.KeyRemoteControl), config.RemoteControlAt(profile))
	}
	read("settings.get", struct{}{}, true)
	// A patch that names other fields leaves the switch where it was.
	read("settings.set", map[string]any{"dock_badge": false}, true)
	read("settings.set", map[string]any{"remote_control": false}, false)
	if config.RemoteControlAt(profile) {
		t.Fatal("turning it off left it on")
	}
	for _, bad := range []any{nil, "on", 1} {
		s.mustFail("settings.set", map[string]any{"remote_control": bad}, appwire.CodeBadParams)
	}
	if config.RemoteControlAt(profile) {
		t.Fatal("a refused write changed the row")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var told []string
	for _, line := range s.lines {
		if strings.Contains(line, "settings.changed") {
			told = append(told, line)
		}
	}
	if len(told) < 2 || !strings.Contains(told[0], `"remote_control":true`) || !strings.Contains(strings.Join(told, "\n"), `"remote_control":false`) {
		t.Fatalf("settings.changed did not carry the switch both ways: %v", told)
	}
}

// R4: when the environment fixes the value, the row says so by name, the write
// is refused in the engine's own sentence, and it is refused WHOLE: a patch that
// also names a field that would otherwise land changes nothing.
func TestAppRemoteControlSwitchPinnedByTheEnvironmentRefusesTheWholePatch(t *testing.T) {
	backend := settingsBackend(t)
	profile := config.ProfileDir()
	t.Setenv("CODEAF_REMOTE_CONTROL", "on")
	got, err := backend.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if got.RemoteControl == nil || !*got.RemoteControl || got.RemoteControlPinnedBy != "CODEAF_REMOTE_CONTROL" {
		t.Fatalf("a pinned row reads remote_control=%v pinned_by=%q", got.RemoteControl, got.RemoteControlPinnedBy)
	}
	off, deny := false, "deny"
	before := config.ToolApprovalModeAt(profile)
	_, err = backend.SetSettings(appwire.SettingsPatch{RemoteControl: &off, AsksBefore: &deny})
	if err == nil || !strings.Contains(err.Error(), "Remote control is set by CODEAF_REMOTE_CONTROL") {
		t.Fatalf("writing a pinned row answered %v", err)
	}
	var wireErr *appwire.Error
	if !errors.As(err, &wireErr) || wireErr.Code != appwire.CodeBadParams {
		t.Fatalf("the refusal is %v, want a bad_params error", err)
	}
	if config.ToolApprovalModeAt(profile) != before {
		t.Fatal("a refused patch still changed another field")
	}
	if _, saved := persistedRemoteControl(profile); saved {
		t.Fatal("a refused write was persisted behind the pin")
	}
	// With the pin gone the same write goes through.
	t.Setenv("CODEAF_REMOTE_CONTROL", "")
	got, err = backend.SetSettings(appwire.SettingsPatch{RemoteControl: &off})
	if err != nil || got.RemoteControl == nil || *got.RemoteControl || got.RemoteControlPinnedBy != "" {
		t.Fatalf("after the pin went: %+v, %v", got, err)
	}
}

// persistedRemoteControl reads only the saved row, which the environment does
// not touch.
func persistedRemoteControl(profile string) (bool, bool) {
	raw, err := os.ReadFile(config.BudgetConfigPath(profile))
	if err != nil {
		return false, false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false, false
	}
	value, ok := fields[config.KeyRemoteControl]
	if !ok {
		return false, false
	}
	var on bool
	_ = json.Unmarshal(value, &on)
	return on, true
}

// R5, R6: turning it ON starts nothing by itself and leaves the chats already
// open as they were; the NEXT chat the desktop opens is offered. Turning it OFF
// stops handing chats out at once and ends what another computer has open on
// them, which that computer reads as the chat closing, while the desktop's own
// chat goes on and keeps what was said.
func TestAppRemoteControlSwitchOnOffMovesTheDoorAsTheContractSays(t *testing.T) {
	r := newDeskHeldRig(t, false)
	// The rig fixes the variable; the switch under test must be free to move.
	t.Setenv("CODEAF_REMOTE_CONTROL", "")
	backend := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { backend.Close() })

	// A chat opened with the switch off is an ordinary chat.
	first := r.open()
	t.Cleanup(first.Release)
	if _, found := r.registry(); found {
		t.Fatal("remote control is off, and the desktop opened a door")
	}
	on, off := true, false
	if _, err := backend.SetSettings(appwire.SettingsPatch{RemoteControl: &on}); err != nil {
		t.Fatal(err)
	}
	if _, found := r.registry(); found {
		t.Fatal("turning the switch on started a door with no chat to offer")
	}
	if _, ok := deskHost.lookup(r.file); ok {
		t.Fatal("a chat opened before the switch was turned on was offered afterwards")
	}

	// The next chat opened is offered, and another computer joins it.
	file2 := filepath.Join(r.root, "second.jsonl")
	engine2, err := session.New(session.Config{Workspace: r.root, SessionFile: file2, Model: "test/model", APIKey: "fixture",
		BaseURL: r.provider.URL, OneModel: true, DeterministicNames: true, PromptProfile: "full"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := appOpenAssembled(remote.Hello{Workspace: r.root, Session: file2}, func(remote.Hello) (*remote.Engine, error) {
		return &remote.Engine{Agent: engine2, SessionFile: file2, Workspace: r.root}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Release)
	socket, found := r.registry()
	if !found {
		t.Fatal("the next chat opened after turning the switch on has no door")
	}
	// The rig joins the chat it was built for, so point it at this one for the
	// join and back again. It holds an atomic counter, so it is never copied.
	opened := r.file
	r.file = file2
	viewer, err := r.viewer(socket)
	r.file = opened
	if err != nil {
		t.Fatalf("another computer could not open the chat opened after the switch: %v", err)
	}
	if deskHost.viewers(file2) != 1 {
		t.Fatalf("the door counts %d viewers, want 1", deskHost.viewers(file2))
	}

	// Off: the door stops handing out at once and the viewer is told.
	if _, err := backend.SetSettings(appwire.SettingsPatch{RemoteControl: &off}); err != nil {
		t.Fatal(err)
	}
	if _, ok := deskHost.lookup(file2); ok {
		t.Fatal("the switch is off and the door still hands the chat out")
	}
	deadline := time.Now().Add(10 * time.Second)
	for viewer.Err() == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if viewer.Err() == nil {
		t.Fatal("turning the switch off left another computer's view of the chat open")
	}
	if deskHost.viewers(file2) != 0 {
		t.Fatalf("the door still counts %d viewers after the switch went off", deskHost.viewers(file2))
	}
	if engine2.Closed() {
		t.Fatal("turning the switch off closed the desktop's own chat")
	}
	if told := r.say(second, "still here"); len(told) == 0 {
		t.Fatal("the desktop's own chat stopped answering when the switch went off")
	}
}
