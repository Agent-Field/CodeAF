package session

import (
	"reflect"
	"sync"
	"testing"
)

// AN ATTACHMENT KEEPS THE POSITION IT WAS GIVEN, because the order is the
// conflict rule a worker reads and not a display preference: re-attaching a
// name already on the list must not move it past a skill the person put ahead
// of it.
func TestAttachingASkillTwiceKeepsItsFirstPlace(t *testing.T) {
	agent := &Agent{}
	agent.AttachSkills("linter", "release")
	if got := agent.AttachSkills("release"); !reflect.DeepEqual(got, []string{"linter", "release"}) {
		t.Fatalf("re-attaching re-ranked the set: %v", got)
	}
	if got := agent.AttachSkills("Linter"); !reflect.DeepEqual(got, []string{"linter", "release"}) {
		t.Fatalf("a name in capitals attached a second copy: %v", got)
	}
}

// A BLANK NAME IS NOT A SKILL. A picker with an empty search box and a person
// pressing enter is the ordinary way this arrives, and a blank entry would
// render as a bullet pointing at nothing.
func TestABlankNameNeverAttaches(t *testing.T) {
	agent := &Agent{}
	if got := agent.AttachSkills("", "   "); len(got) != 0 {
		t.Fatalf("blank names attached: %v", got)
	}
	if got := agent.AttachedSkills(); got != nil {
		t.Fatalf("an untouched conversation carries an attachment: %v", got)
	}
}

// TAKING OFF A NAME NOBODY ATTACHED IS AN ANSWER, NOT AN ERROR: a surface
// drawing checkboxes cannot know the list changed while it drew.
func TestDetachSaysWhetherTheNameWasThere(t *testing.T) {
	agent := &Agent{}
	agent.AttachSkills("linter")
	if agent.DetachSkill("absent") {
		t.Fatal("detaching an absent name reported a removal")
	}
	if !agent.DetachSkill("LINTER") {
		t.Fatal("detaching by a differently spelled name missed it")
	}
	if got := agent.AttachedSkills(); got != nil {
		t.Fatalf("the set survived its last detach: %v", got)
	}
}

// THE SET HANDED OUT IS A COPY, because the caller is a surface drawing a list
// while a turn may be writing one.
func TestTheAttachedSetIsHandedOutAsACopy(t *testing.T) {
	agent := &Agent{}
	agent.AttachSkills("linter", "release")
	held := agent.AttachedSkills()
	held[0] = "rewritten"
	if again := agent.AttachedSkills(); again[0] != "linter" {
		t.Fatalf("a caller's write reached the agent's own set: %v", again)
	}
	if count := agent.ClearAttachedSkills(); count != 2 {
		t.Fatalf("clear reported %d attachments, wanted 2", count)
	}
}

// EVERY DOOR IS UNDER THE AGENT'S ONE LOCK, which the race detector is the only
// honest way to say.
func TestTheAttachmentDoorsAreSafeUnderRace(t *testing.T) {
	agent := &Agent{}
	var wait sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			agent.AttachSkills("linter", "release")
			agent.AttachedSkills()
			agent.DetachSkill("release")
		}()
	}
	wait.Wait()
}
