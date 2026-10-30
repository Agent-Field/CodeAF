package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/teams"
)

func TestLocalTeamDeliverySurvivesMissingFilesAndLookupFailure(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEAF_HOME", root)
	if _, err := identity.EnsureSolo(root); err != nil {
		t.Fatal(err)
	}
	f := newTeamFixture(t, true)
	a := teamAgent(t, f, f.manager, nil, manualRelayTeamClock)
	a.config.TeamComputers = func(context.Context, []teams.Member) (map[string]string, error) { return nil, errors.New("offline") }
	if err := teams.Update(f.profile, func(file *teams.File) error {
		for i := range file.Teams[0].Members {
			if file.Teams[0].Members[i].Handle == "web" {
				file.Teams[0].Members[i].File = ""
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	said, failed, err := a.teamSendTool(t.Context(), json.RawMessage(`{"to":"web","text":"local delivery"}`))
	if err != nil || failed {
		t.Fatal(said, failed, err)
	}
	if refusal := a.teamDeliveryRefusal(t.Context(), []teams.Member{{Key: "claim-with-no-file"}, {Key: filepath.Join(root, "missing"), File: filepath.Join(root, "missing")}, {Key: teams.RemoteKey("old")}}); refusal != "" {
		t.Fatal("unpaired refusal", refusal)
	}
	lines, _ := teams.ReadTraffic(f.profile, f.teamID, "", 0)
	if len(lines) != 1 || lines[0].Text != "local delivery" {
		t.Fatal(lines)
	}
}

func TestMixedTeamRecipientsWriteOnceAndNameThoseNotReached(t *testing.T) {
	for _, mode := range []string{"everyone", "several", "room", "all-remote-room", "single"} {
		t.Run(mode, func(t *testing.T) {
			f := newTeamFixture(t, true)
			transcript, args := f.manager, `{"to":"everyone","text":"hello"}`
			if mode == "several" {
				args = `{"to":"web, parser","text":"hello"}`
			}
			if mode == "single" {
				args = `{"to":"web","text":"hello"}`
			}
			if strings.Contains(mode, "room") {
				transcript, args = f.web, `{"text":"hello"}`
			}
			a := teamAgent(t, f, transcript, nil, manualRelayTeamClock)
			calls := 0
			a.config.TeamComputers = func(_ context.Context, members []teams.Member) (map[string]string, error) {
				calls++
				names := map[string]string{}
				for _, m := range members {
					if m.Handle == "web" || m.Handle == "boss" || mode == "all-remote-room" {
						names[m.Key] = "desktop"
					}
				}
				return names, nil
			}
			run := a.teamSendTool
			if strings.Contains(mode, "room") {
				run = a.teamPostTool
			}
			said, failed, err := run(t.Context(), json.RawMessage(args))
			wantRefusal := mode == "single" || mode == "all-remote-room"
			if err != nil || failed != wantRefusal || calls != 1 {
				t.Fatal(said, failed, err, calls)
			}
			lines, _ := teams.ReadTraffic(f.profile, f.teamID, "", 0)
			if wantRefusal {
				if len(lines) != 0 || !strings.Contains(said, "message would not reach") {
					t.Fatal(said, lines)
				}
				return
			}
			if len(lines) != 1 || !strings.Contains(said, "Not reached:") || !strings.Contains(said, "desktop") {
				t.Fatal(said, lines)
			}
			if strings.Contains(mode, "room") {
				if !strings.Contains(said, "@boss") || !strings.Contains(teamLine(teamRole{handle: "parser"}, lines[0]), "hello") {
					t.Fatal(said, lines)
				}
			} else if !strings.Contains(teamLine(teamRole{handle: "parser"}, lines[0]), "hello") {
				t.Fatal("local recipient lost", lines)
			}
		})
	}
}

func TestPacketDecideAndEscalateRefuseRemoteDestinationsBeforeMutation(t *testing.T) {
	for _, escalate := range []bool{false, true} {
		t.Run(map[bool]string{false: "decide", true: "escalate"}[escalate], func(t *testing.T) {
			f := newTeamFixture(t, true)
			parentKey := "remote-parent"
			if err := teams.Update(f.profile, func(file *teams.File) error {
				file.Teams = append(file.Teams, teams.Team{ID: "parent", Name: "parent", Members: []teams.Member{{Key: parentKey, Handle: "upstream", Word: "upstream"}, {Key: convKeyOf(t, f.manager), File: f.manager, Handle: "boss", Word: "boss"}}, Manager: parentKey})
				file.Teams[0].Parent = "parent"
				for i := range file.Teams[1].Members {
					if file.Teams[1].Members[i].Key == convKeyOf(t, f.manager) {
						file.Teams[1].Members[i].Home = true
					}
				}
				for i := range file.Teams[0].Members {
					if file.Teams[0].Members[i].Key == convKeyOf(t, f.manager) {
						file.Teams[0].Members[i].Home = false
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			p, err := teams.Raise(f.profile, teams.Packet{Kind: teams.PacketQuestion, Team: f.teamID, Origin: f.teamID, RaisedBy: "web", Question: "which?", Parties: []teams.Party{{Key: convKeyOf(t, f.web), Handle: "web", Team: f.teamID}}})
			if err != nil {
				t.Fatal(err)
			}
			before, _ := teams.ReadTraffic(f.profile, f.teamID, "", 0)
			a := teamAgent(t, f, f.manager, nil, manualRelayTeamClock)
			a.config.TeamComputers = func(_ context.Context, members []teams.Member) (map[string]string, error) {
				names := map[string]string{}
				for _, m := range members {
					if m.Handle == "web" || m.Key == parentKey {
						names[m.Key] = "desktop"
					}
				}
				return names, nil
			}
			run, args := a.teamDecideTool, `{"packet":"`+p.ID+`","answer":"one"}`
			if escalate {
				run, args = a.teamEscalateTool, `{"packet":"`+p.ID+`","reason":"need advice"}`
			}
			said, failed, err := run(t.Context(), json.RawMessage(args))
			after, _ := teams.ReadTraffic(f.profile, f.teamID, "", 0)
			held, readErr := teams.PacketByID(f.profile, p.ID)
			if err != nil || readErr != nil || !failed || !strings.Contains(said, "desktop") || held.State != p.State || held.Team != p.Team || len(after) != len(before) {
				t.Fatal(said, failed, err, held, after)
			}
		})
	}
}

func TestCursorIdentityCacheInvalidatesOnPairingAndReplacement(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEAF_HOME", root)
	if _, err := identity.EnsureSolo(root); err != nil {
		t.Fatal(err)
	}
	if got := teamCursorDevice(); got != "" {
		t.Fatal(got)
	}
	if err := identity.EndSolo(root); err != nil {
		t.Fatal(err)
	}
	first := teamCursorDevice()
	cursorDeviceCache.Lock()
	before := cursorDeviceCache.reads
	cursorDeviceCache.Unlock()
	for range 20 {
		if got := teamCursorDevice(); got != first {
			t.Fatal(got)
		}
	}
	cursorDeviceCache.Lock()
	after := cursorDeviceCache.reads
	cursorDeviceCache.Unlock()
	if first == "" || after != before {
		t.Fatal("identity files read on quiet saves", before, after)
	}
	if err := os.Remove(filepath.Join(root, identity.DeviceFile)); err != nil {
		t.Fatal(err)
	}
	dev, err := identity.Device(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := teamCursorDevice(); got != dev.ID() || got == first {
		t.Fatal("replacement did not invalidate", got, first)
	}
}
