package tui3

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

func TestMirrorRefreshActsOnEveryPendingLocalStartAndStopExactlyOnce(t *testing.T) {
	a, id, _, _ := trafficApp(t)
	enableSurfaceTeamSharing(t, a)
	a.start = nil
	trafficReadNow(t, a)
	first := a.traffic.cursor[id]
	var ids []string
	for i := 0; i < trafficPage+7; i++ {
		kind := teamstore.KindStart
		if i%2 == 1 {
			kind = teamstore.KindStop
		}
		line, err := teamstore.AppendTrafficID(a.profileDir, id, teamstore.Entry{Kind: kind, From: teamstore.FromManager, To: "boss", Member: a.frontTabKey(), Text: fmt.Sprint(i)})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, line)
	}
	if err := teamstore.WriteTrafficMirror(a.profileDir, id, []teamstore.Entry{{ID: "elsewhere/1", At: time.Now(), Text: "mirror changed"}}); err != nil {
		t.Fatal(err)
	}
	trafficReadNow(t, a)
	if a.traffic.cursor[id] == ids[len(ids)-1] || a.traffic.cursor[id] == first {
		t.Fatal("refresh skipped or failed local page", a.traffic.cursor[id])
	}
	trafficReadNow(t, a)
	for _, line := range ids {
		if !a.traffic.done[id+"/"+line] {
			t.Fatalf("pending action lost: %s", line)
		}
	}
	count := len(a.traffic.done)
	trafficReadNow(t, a)
	if len(a.traffic.done) != count || count != len(ids) {
		t.Fatal("actions repeated", count, len(a.traffic.done))
	}
}

func TestSameChatGesturesCheckCachedRemoteOwnershipBeforeReturning(t *testing.T) {
	celltest.Available(t)
	for _, gesture := range []string{"traffic", "mention", "team-link"} {
		t.Run(gesture, func(t *testing.T) {
			a := continueHome(t, &fakeTaker{}, continueRows, 140)
			enableSurfaceTeamSharing(t, a)
			row := continueRows[0]
			m := teamMember{Key: a.frontTabKey(), File: a.file, Handle: "worker", Word: row.Title, SyncID: "cell:" + row.Cell}
			a.wall.loaded = true
			a.wall.teams = []team{{ID: "alpha", Name: "alpha", Members: []teamMember{m}}}
			a.leavePlace()
			switch gesture {
			case "traffic":
				a.trafficJumpFromTeam("alpha", m.Key, "000000000001")
			case "mention":
				a.mentionChatPress(m.Key)
			case "team-link":
				a.teamLinkPress(taskLink{team: "alpha", member: m.Key})
			}
			if !a.at(pageHome) {
				t.Fatal("front-chat shortcut bypassed ownership")
			}
			ask, ok := a.homeAsking()
			if !ok || ask.question.Head != continueAsk {
				t.Fatal("no continuation question", ask, ok)
			}
			frame, _, _ := a.frame()
			if !strings.Contains(plain(frame), continueAsk) {
				t.Fatal("the continuation question is not visible")
			}
		})
	}
}

func TestRemoteMemberCompletionIsDeduplicatedAndItsLinkContinuesHere(t *testing.T) {
	celltest.Available(t)
	a := continueHome(t, &fakeTaker{}, continueRows, 140)
	enableSurfaceTeamSharing(t, a)
	row := continueRows[0]
	m := teamMember{Key: teamstore.RemoteKey(row.Cell), Handle: "worker", Word: "remote worker", SyncID: "cell:" + row.Cell}
	a.wall.loaded = true
	a.wall.teams = []team{{ID: "alpha", Name: "alpha", Members: []teamMember{m}}, {ID: "beta", Name: "beta", Members: []teamMember{m}}}
	a.comp.recents = []mentionChat{{key: m.Key, title: m.Word, handle: m.Handle}}
	a.comp.open, a.comp.query = true, "chat:worker"
	a.fillMentions()
	a.comp.rank()
	if len(a.comp.chatHits) != 1 || a.comp.chatHits[0].key != m.Key {
		t.Fatal("remote completion absent or duplicated", a.comp.chatHits)
	}
	chosen := a.comp.chatHits[0]
	a.input.setText("@chat:worker")
	a.completeChat(chosen)
	a.mentionChatPress(chosen.key)
	ask, ok := a.homeAsking()
	if !ok || ask.question.Head != continueAsk {
		t.Fatal("chosen remote link missed continuation")
	}
}

func TestHostedTeamMemberNeverUsesTheLaptopsContinuation(t *testing.T) {
	celltest.Available(t)
	a := continueHome(t, &fakeTaker{}, continueRows, 140)
	enableSurfaceTeamSharing(t, a)
	a.host = "engine.example"
	row := continueRows[0]
	a.teamsDisk.door = TeamsSeam{Load: func([]float64) ([]teamstore.Team, string, bool) { return nil, "", true }}
	// Even a colliding laptop row belongs to a different continuation context.
	a.wall.loaded = true
	m := teamMember{Key: teamstore.RemoteKey(row.Cell), Word: "engine member"}
	a.wall.teams = []team{{ID: "alpha", Name: "alpha", Members: []teamMember{m}}}
	a.trafficGo(m.Key)
	if _, ok := a.homeAsking(); ok {
		t.Fatal("hosted member entered laptop continuation")
	}
	if !strings.Contains(a.tp.msg, "on the other computer") {
		t.Fatal(a.tp.msg)
	}
}

func TestUnknownComputerNamesNeverExposeDeviceIDs(t *testing.T) {
	a := teamsPlaceLab(t)
	enableSurfaceTeamSharing(t, a)
	id := "dev_1234567890abcdef"
	a.machineRead.rows = []chatlist.Row{{DeviceID: id, Device: "12345678"}}
	a.devRow.devices = []chatlist.Device{{ID: id, Name: "12345678"}}
	if got := a.teamComputerName(id); got != "the other computer" {
		t.Fatal(got)
	}
	a.teamsRemoteGo(teamMember{Key: teamstore.OtherComputerKey("device:" + id + ":chat"), Handle: "worker"})
	if strings.Contains(a.tp.msg, id) || !strings.Contains(a.tp.msg, "the other computer") {
		t.Fatal(a.tp.msg)
	}
}

func TestMissingMirrorReferenceNamesTheOtherComputersHistory(t *testing.T) {
	a, _, _, _ := trafficApp(t)
	a.trafficJump("", "elsewhere/000000000001")
	if got := a.trafficJumpWords(); got != "that message is on the other computer's history" {
		t.Fatal(got)
	}
}
