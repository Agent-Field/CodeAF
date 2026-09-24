package remote

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// THE DELEGATION DOORS CROSS, AND THEY ANSWER FROM THE ENGINE'S PROFILE AND
// LEDGER. The welcome says the engine has them; the defaults are the engine
// profile's; a packet raised over the wire lands in the engine's file; a read
// at the held stamp is a few bytes; a decision and an escalation cross; the
// spend is the engine's ledger; and a delete is refused for an open team and
// takes a closed one's files.
func TestTheDelegationDoorsCrossFromTheEnginesProfile(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	loop, dir := teamsLoop(t)
	if !loop.Client.Welcome().Delegation {
		t.Fatal("an engine of this build does not say it has the delegation doors")
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"teams.cap_usd_day": 7}`), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := loop.Client.TeamsDefaults()
	if err != nil || d.CapUSDDay != 7 || d.DepthLimit != 3 || !d.QuestionsUp {
		t.Fatalf("the engine's defaults: %+v, %v", d, err)
	}

	session := "0123456789abcdef"
	key := filepath.Join("/srv", session, "transcript.jsonl")
	if err := teamstore.Save(dir, []teamstore.Team{
		{ID: "0a0a0a0a0a0a", Name: "harbor", Manager: "hm",
			Members: []teamstore.Member{{Key: "hm", Handle: "boss"}, {Key: key, Handle: "web"}}},
	}); err != nil {
		t.Fatal(err)
	}
	p, err := loop.Client.TeamsRaise(teamstore.Packet{Team: "0a0a0a0a0a0a", Kind: teamstore.PacketJudgement,
		RaisedBy: "web", Question: "ship today?",
		Options: []teamstore.Option{{Label: "yes", Consequence: "it ships"}, {Label: "no", Consequence: "it waits"}}})
	if err != nil || p.ID == "" || p.State != teamstore.PacketOpen {
		t.Fatalf("raise: %+v, %v", p, err)
	}
	if on, _ := teamstore.PacketByID(dir, p.ID); on.Question != "ship today?" {
		t.Fatal("the packet is not in the engine's file")
	}
	first, err := loop.Client.TeamsPackets("0a0a0a0a0a0a", "")
	if err != nil || len(first.Packets) != 1 || first.Same {
		t.Fatalf("packets: %+v, %v", first, err)
	}
	same, err := loop.Client.TeamsPackets("0a0a0a0a0a0a", first.Stamp)
	if err != nil || !same.Same || same.Packets != nil {
		t.Fatalf("a read at the held stamp: %+v, %v", same, err)
	}
	if raw, _ := json.Marshal(same); len(raw) > 64 {
		t.Fatalf("an unchanged packets answer is %d bytes: %s", len(raw), raw)
	}
	up, err := loop.Client.TeamsEscalate(p.ID, "boss", teamstore.Person, "not mine to call")
	if err != nil || up.Team != teamstore.Person {
		t.Fatalf("escalate: %+v, %v", up, err)
	}
	if _, err := loop.Client.TeamsDecide(p.ID, "boss", "1", ""); err == nil ||
		!strings.Contains(err.Error(), "only the manager") {
		t.Fatalf("the manager it left decided over the wire: %v", err)
	}
	done, err := loop.Client.TeamsDecide(p.ID, teamstore.Person, "2", "tomorrow")
	if err != nil || done.State != teamstore.PacketDecided || done.DecidedBy != teamstore.Person {
		t.Fatalf("decide: %+v, %v", done, err)
	}

	day := teamstore.Today()
	ledger := teamstore.UsageLedgerPath()
	if err := os.MkdirAll(filepath.Dir(ledger), 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"at":"` + time.Now().Format(time.RFC3339Nano) + `","day":"` + day + `","usd":1.2,"calls":1,"session":"` + session + `"}` + "\n"
	if err := os.WriteFile(ledger, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	spent, err := loop.Client.TeamsSpend("0a0a0a0a0a0a", "", "")
	if err != nil || spent.Spend == nil || spent.Spend.USD != 1.2 || spent.Spend.Day != day {
		t.Fatalf("spend: %+v, %v", spent, err)
	}
	quiet, err := loop.Client.TeamsSpend("0a0a0a0a0a0a", "", spent.Stamp)
	if err != nil || !quiet.Same || quiet.Spend != nil {
		t.Fatalf("a quiet spend: %+v, %v", quiet, err)
	}

	if _, err := loop.Client.TeamsDelete("0a0a0a0a0a0a"); err == nil {
		t.Fatal("an open team was deleted over the wire")
	}
	if err := teamstore.Update(dir, func(f *teamstore.File) error {
		return f.Close("0a0a0a0a0a0a", time.Time{}, p.ID)
	}); err != nil {
		t.Fatal(err)
	}
	gone, err := loop.Client.TeamsDelete("0a0a0a0a0a0a")
	if err != nil || len(gone.Gone) != 1 {
		t.Fatalf("delete: %+v, %v", gone, err)
	}
	if _, err := os.Stat(teamstore.TeamDir(dir, "0a0a0a0a0a0a")); !os.IsNotExist(err) {
		t.Fatalf("the team's files are still there: %v", err)
	}
}

// THE WRAP-UP'S DOORS CROSS: the request lands in the engine's Traffic as
// exactly the marker the manager's session reads, and accepting a decided
// closing report closes the team on the engine, once.
func TestTheWrapUpDoorsCrossToTheEngine(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	loop, dir := teamsLoop(t)
	if !loop.Client.Welcome().WrapUp {
		t.Fatal("an engine of this build does not say it has the wrap-up doors")
	}
	if err := teamstore.Save(dir, []teamstore.Team{
		{ID: "0a0a0a0a0a0a", Name: "harbor", Manager: "hm",
			Members: []teamstore.Member{{Key: "hm", Handle: "boss"}, {Key: "w", Handle: "web"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := loop.Client.TeamsWrapUp("0a0a0a0a0a0a", ""); err != nil {
		t.Fatal(err)
	}
	log, err := teamstore.ReadTraffic(dir, "0a0a0a0a0a0a", "", 0)
	if err != nil || len(log) != 1 || !teamstore.IsWrapUp(log[0]) {
		t.Fatalf("the engine's traffic: %+v %v", log, err)
	}
	p, err := teamstore.Raise(dir, teamstore.Packet{Team: teamstore.Person, Origin: "0a0a0a0a0a0a", Kind: teamstore.PacketClosing,
		RaisedBy: teamstore.FromManager, Question: "close harbor?", Report: &teamstore.ClosingReport{Done: "all of it"},
		Options: []teamstore.Option{{ID: teamstore.OptionClose, Label: "Close", Consequence: "it closes"},
			{ID: teamstore.OptionKeepGoing, Label: "Keep going", Consequence: "it stays"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loop.Client.TeamsDecide(p.ID, teamstore.Person, teamstore.OptionClose, ""); err != nil {
		t.Fatal(err)
	}
	reply, err := loop.Client.TeamsAcceptClosing(p.ID)
	if err != nil || !reply.Closed {
		t.Fatalf("accept: %+v %v", reply, err)
	}
	if again, err := loop.Client.TeamsAcceptClosing(p.ID); err != nil || again.Closed {
		t.Fatalf("a second accept: %+v %v", again, err)
	}
	f, _ := teamstore.Load(dir)
	if team, _ := f.Team("0a0a0a0a0a0a"); !team.Closed() {
		t.Fatal("the engine's team did not close")
	}
}
