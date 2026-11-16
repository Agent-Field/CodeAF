package serve

import (
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// Between people (Workstreams final spec, "Is there chat between people?"):
// santosh and abir, two signed-in people on one server.

func TestAHandedOverTaskSaysWhoHandedItOver(t *testing.T) {
	o, path := taskOrg(t, "edit")
	made := o.task(t, "santosh", path, api.TaskCreate{Title: "Review the Q4 budget", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	if made.HandedBy != nil {
		t.Fatalf("a task nobody handed on says it was handed: %+v", made.HandedBy)
	}
	var cur api.WorkstreamTask
	o.do(t, "santosh", "PATCH", path+"/tasks/"+made.ID, made.Rev, map[string]any{"owner": factory.TaskOwner{Kind: "person", ID: "abir", Name: "abir"}}, 200, &cur)
	if cur.HandedBy == nil || cur.HandedBy.ID != "santosh" {
		t.Fatalf("handed to abir by santosh: %+v", cur.HandedBy)
	}
	got := o.tasks(t, "abir", path)
	if len(got) != 1 || got[0].HandedBy == nil || got[0].HandedBy.ID != "santosh" {
		t.Fatalf("abir reads %+v", got)
	}
	// Taking it back by your own hand is not a hand-over.
	var back api.WorkstreamTask
	o.do(t, "santosh", "PATCH", path+"/tasks/"+made.ID, cur.Rev, map[string]any{"owner": factory.TaskOwner{Kind: "person", ID: "santosh", Name: "santosh"}}, 200, &back)
	if back.HandedBy != nil {
		t.Fatalf("taken back, still handed by %+v", back.HandedBy)
	}
}

func TestACommentReachesTheOwnerAndWhoeverItNamesUntilTheyReadIt(t *testing.T) {
	o, path := taskOrg(t, "edit")
	// santosh owns the notes; abir owns the budget.
	notes := o.task(t, "santosh", path, api.TaskCreate{Title: "Draft the March release notes", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	budget := o.task(t, "santosh", path, api.TaskCreate{Title: "Review the Q4 budget", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi},
		Owner: &factory.TaskOwner{Kind: "person", ID: "abir", Name: "abir"}})

	forYou := func(who, id string) *factory.TaskForYou {
		t.Helper()
		all := o.tasks(t, who, path)
		i := slices.IndexFunc(all, func(x api.WorkstreamTask) bool { return x.ID == id })
		if i < 0 {
			t.Fatalf("%s cannot read %s", who, id)
		}
		return all[i].ForYou
	}

	// abir comments on santosh's task: it reaches santosh, the owner.
	var ev api.WorkstreamTaskEvent
	o.do(t, "abir", "POST", path+"/tasks/"+notes.ID+"/comment", -1, map[string]string{"text": "Mention the import fix. People asked for it."}, 201, &ev)
	if len(ev.Mentions) != 0 {
		t.Fatalf("no @, yet mentions %v", ev.Mentions)
	}
	if fy := forYou("santosh", notes.ID); fy == nil || fy.Comments != 1 || fy.Mentioned || fy.Last.Text != "Mention the import fix. People asked for it." {
		t.Fatalf("santosh's notes for him: %+v", fy)
	}
	if fy := forYou("abir", notes.ID); fy != nil {
		t.Fatalf("abir's own comment is not for abir: %+v", fy)
	}

	// santosh names abir on santosh's own task: it reaches abir, who does not own it.
	var named api.WorkstreamTaskEvent
	o.do(t, "santosh", "POST", path+"/tasks/"+notes.ID+"/comment", -1, map[string]string{"text": "@Abir can you check the numbers?"}, 201, &named)
	if !slices.Equal(named.Mentions, []string{"abir"}) {
		t.Fatalf("@Abir names abir: %v", named.Mentions)
	}
	if fy := forYou("abir", notes.ID); fy == nil || fy.Comments != 1 || !fy.Mentioned {
		t.Fatalf("abir named: %+v", fy)
	}
	// An @ that names nobody in the workstream reaches nobody.
	var plain api.WorkstreamTaskEvent
	o.do(t, "santosh", "POST", path+"/tasks/"+budget.ID+"/comment", -1, map[string]string{"text": "email@example.com and @mia"}, 201, &plain)
	if len(plain.Mentions) != 0 {
		t.Fatalf("mia is not in Pantry and an address is not an @: %v", plain.Mentions)
	}
	if fy := forYou("abir", budget.ID); fy == nil || fy.Comments != 1 || fy.Mentioned {
		t.Fatalf("abir owns the budget, so the comment reaches him: %+v", fy)
	}

	// Reading the thread from a screen marks it read; an agent's read does not.
	o.do(t, "santosh", "GET", path+"/tasks/"+notes.ID+"/events", -1, nil, 200, nil)
	if fy := forYou("santosh", notes.ID); fy == nil {
		t.Fatalf("a read without seen=1 marked it read")
	}
	o.do(t, "santosh", "GET", path+"/tasks/"+notes.ID+"/events?seen=1", -1, nil, 200, nil)
	if fy := forYou("santosh", notes.ID); fy != nil {
		t.Fatalf("read, still for santosh: %+v", fy)
	}
	if fy := forYou("abir", notes.ID); fy == nil {
		t.Fatalf("santosh's read is not abir's")
	}
	// A new comment after the read is unread again.
	o.do(t, "abir", "POST", path+"/tasks/"+notes.ID+"/comment", -1, map[string]string{"text": "Done, the numbers hold."}, 201, nil)
	if fy := forYou("santosh", notes.ID); fy == nil || fy.Comments != 1 || fy.Last.By.ID != "abir" {
		t.Fatalf("the reply: %+v", fy)
	}
}

func TestMentionsMatchAHandleANameOrAFirstName(t *testing.T) {
	people := map[string]string{"abir": "Abir Das", "santosh": "Santosh Kumar", "mia": "mia"}
	for text, want := range map[string][]string{
		"@abir please":             {"abir"},
		"(@Santosh) and @AbirDas.": {"santosh", "abir"},
		"cc @abir, @abir":          {"abir"},
		"mail abir@example.com":    nil,
		"@nobody here":             nil,
		"@mia: and @Mia":           {"mia"},
		"no at all":                nil,
	} {
		if got := factory.Mentions(text, people); !slices.Equal(got, want) {
			t.Errorf("%q: %v, want %v", text, got, want)
		}
	}
}
