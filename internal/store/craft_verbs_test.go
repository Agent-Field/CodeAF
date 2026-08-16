package store

import (
	"encoding/json"
	"errors"
	"strconv"
	"testing"
)

// The four verbs a person aims at what the resident has learned are ordinary
// journaled commands: they queue, they survive a rebuild, and they come back
// out of the journal saying exactly what went in. Nothing about them is a
// side-channel.
func TestCraftAndSkillCommandsJournalAndReplay(t *testing.T) {
	s := openThreadStore(t)
	skill, err := s.RecordSkillCandidate(RootID, "tool:imgshrink", "imgshrink squeezes screenshots", "/tmp/imgshrink")
	if err != nil {
		t.Fatalf("record skill: %v", err)
	}
	if err := s.ActivateSkill(skill.Seq, skill.Artifact); err != nil {
		t.Fatalf("activate skill: %v", err)
	}

	wanted := []Command{
		{Kind: CommandCraftRun, Target: "release-notes", Instruction: "for the 0.4 tag"},
		{Kind: CommandCraftRevert, Target: "release-notes", Instruction: "the new link check misses half of them"},
		{Kind: CommandCraftRetire, Target: "fetch-pr-context", Instruction: "we do this by hand now"},
		{Kind: CommandSkillRetire, Target: strconv.FormatInt(skill.Seq, 10), Instruction: "it mangles the colours"},
	}
	for index := range wanted {
		wanted[index].SessionID = "room"
		requested, err := s.RequestCommand(wanted[index])
		if err != nil {
			t.Fatalf("request %s: %v", wanted[index].Kind, err)
		}
		if requested.Status != CommandPending {
			t.Fatalf("%s is %s", requested.Kind, requested.Status)
		}
		wanted[index].Seq = requested.Seq
	}

	assertQueued := func(where string) {
		t.Helper()
		pending, err := s.PendingCommands(0)
		if err != nil {
			t.Fatalf("%s: pending: %v", where, err)
		}
		if len(pending) != len(wanted) {
			t.Fatalf("%s: %d commands queued, want %d", where, len(pending), len(wanted))
		}
		for index, command := range pending {
			if command.Kind != wanted[index].Kind || command.Target != wanted[index].Target ||
				command.Instruction != wanted[index].Instruction || command.Seq != wanted[index].Seq {
				t.Fatalf("%s: command %d = %+v, want %+v", where, index, command, wanted[index])
			}
		}
	}
	assertQueued("as journaled")
	if err := s.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	assertQueued("after a rebuild")
}

// A craft's name is not a row in this database — the repository owns which
// names exist — so the funnel checks only that a name was given, and a workflow
// nobody has forged is refused in words where the repository is.
func TestACraftCommandNeedsANameAndNotANode(t *testing.T) {
	s := openThreadStore(t)
	if _, err := s.RequestCommand(Command{
		SessionID: "room", Kind: CommandCraftRun, Target: "never-forged", Instruction: "go on then",
	}); err != nil {
		t.Fatalf("a craft command was refused for naming no node: %v", err)
	}
	_, err := s.RequestCommand(Command{SessionID: "room", Kind: CommandCraftRun, Instruction: "go on then"})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("a targetless craft run was accepted: %v", err)
	}
}

// Retiring a tool names the belief's own number, and a number that is not a
// live tool is refused where the person asked rather than two ticks later.
func TestSkillRetireIsCheckedAtTheFunnel(t *testing.T) {
	s := openThreadStore(t)
	belief, err := s.RecordFact(RootID, "user", FactPlain, "prefers tables over prose")
	if err != nil {
		t.Fatalf("record fact: %v", err)
	}
	for name, target := range map[string]string{
		"a belief that is not a tool": strconv.FormatInt(belief.Seq, 10),
		"a number nobody has":         "99999",
		"not a number at all":         "imgshrink",
	} {
		if _, err := s.RequestCommand(Command{
			SessionID: "room", Kind: CommandSkillRetire, Target: target, Instruction: "let it go",
		}); err == nil {
			t.Fatalf("%s was accepted as a tool retirement", name)
		}
	}

	skill, err := s.RecordSkillCandidate(RootID, "tool:imgshrink", "imgshrink squeezes screenshots", "/tmp/imgshrink")
	if err != nil {
		t.Fatalf("record skill: %v", err)
	}
	if err := s.ActivateSkill(skill.Seq, skill.Artifact); err != nil {
		t.Fatalf("activate skill: %v", err)
	}
	if _, err := s.RequestCommand(Command{
		SessionID: "room", Kind: CommandSkillRetire,
		Target: strconv.FormatInt(skill.Seq, 10), Instruction: "let it go",
	}); err != nil {
		t.Fatalf("retiring a live tool was refused: %v", err)
	}
	if err := s.QuarantineFact(skill.Seq, 0, FactOriginUser); err != nil {
		t.Fatalf("quarantine: %v", err)
	}
	if _, err := s.RequestCommand(Command{
		SessionID: "room", Kind: CommandSkillRetire,
		Target: strconv.FormatInt(skill.Seq, 10), Instruction: "let it go",
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a tool that is already off the shelf was retired again: %v", err)
	}
}

// A way of working is its own kind of row in an arrival brief. It rode as a
// skill until this kind existed, which made the brief say "tool" about a
// four-step workflow.
func TestABriefMayCarryACraftRow(t *testing.T) {
	s := openThreadStore(t)
	posted, err := s.PostMessage(Message{
		SessionID: "room", Role: RoleAgent, Body: "While you were away: one new way of working",
		Brief: &Brief{SinceSeq: 1, ThroughSeq: 2, Items: []BriefItem{
			{Kind: BriefCraft, Body: "Learned how to do release-notes", Ref: "release-notes"},
		}},
	})
	if err != nil {
		t.Fatalf("post brief: %v", err)
	}
	if err := s.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	messages, err := s.Messages("room", 0, 0)
	if err != nil || len(messages) != 1 || messages[0].Brief == nil {
		t.Fatalf("messages = %+v err=%v", messages, err)
	}
	item := messages[0].Brief.Items[0]
	if item.Kind != BriefCraft || item.Ref != "release-notes" {
		t.Fatalf("the brief row came back as %+v (seq %d)", item, posted.Seq)
	}
}

// The forging event: the version lives in git, the MOMENT lives here, and the
// digest is written from exactly this interval.
func TestForgingAWayOfWorkingIsJournaledAndSurvivesARebuild(t *testing.T) {
	s := openThreadStore(t)
	before, err := s.LatestEventSeq()
	if err != nil {
		t.Fatalf("latest seq: %v", err)
	}
	seq, err := s.RecordCraftForged(CraftForged{
		Name: "release-notes", Commit: "abc1234", Refined: true, Because: "write the 0.4 notes",
	})
	if err != nil || seq <= before {
		t.Fatalf("record craft forged: seq %d err %v", seq, err)
	}
	if _, err := s.RecordCraftForged(CraftForged{Name: "   "}); err == nil {
		t.Fatal("a nameless forging was journaled")
	}

	read := func(where string) CraftForged {
		t.Helper()
		events, err := s.Events(before, 0)
		if err != nil {
			t.Fatalf("%s: events: %v", where, err)
		}
		for _, event := range events {
			if event.Kind != EventCraftForged {
				continue
			}
			var forged CraftForged
			if err := json.Unmarshal(event.Payload, &forged); err != nil {
				t.Fatalf("%s: decode: %v", where, err)
			}
			return forged
		}
		t.Fatalf("%s: no forging in the journal", where)
		return CraftForged{}
	}
	if forged := read("as journaled"); forged.Name != "release-notes" || !forged.Refined {
		t.Fatalf("forged = %+v", forged)
	}
	if err := s.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if forged := read("after a rebuild"); forged.Commit != "abc1234" {
		t.Fatalf("forged = %+v", forged)
	}
}
