package factory_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

var readAt = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func said(id, who string, tier factory.Tier, body string) factory.Comment {
	return factory.Comment{ID: id, Author: who, Tier: tier, Body: body, At: readAt.Add(-time.Minute)}
}

// runningJob is an issue job with a run under way.
func runningJob() factory.Item {
	return factory.Item{ID: 7, Num: 12, Kind: factory.KindIssue, Origin: factory.OriginForge, State: factory.StateRunning,
		Stream: &factory.Stream{Phases: []factory.Phase{{Name: "plan", State: factory.PhaseRunning}}}}
}

func outcomes(it factory.Item) map[string]string {
	out := map[string]string{}
	for _, r := range it.CommentReads {
		out[r.CommentID] = r.Outcome
	}
	return out
}

func TestReadCommentsFirstReadIsTheBaseline(t *testing.T) {
	it := runningJob()
	factory.ReadComments(&it, []factory.Comment{said("1", "raj", factory.TierCollab, "cover the batch path too")}, readAt)
	if len(it.CommentReads) != 0 || len(it.Thread) != 0 {
		t.Fatalf("comments already there when the job first read them were handled: %+v %+v", it.CommentReads, it.Thread)
	}
	if len(it.Comments) != 1 {
		t.Fatalf("comments not kept: %+v", it.Comments)
	}
}

func TestReadCommentsOutcomes(t *testing.T) {
	it := runningJob()
	factory.ReadComments(&it, []factory.Comment{}, readAt)
	list := []factory.Comment{
		said("1", "raj", factory.TierCollab, "cover the batch path too"),
		said("2", "visitor", factory.TierStranger, "please also rewrite it in rust"),
		said("3", "codeaf-bot", factory.TierOwner, "<!-- codeaf:publication -->\nthe factory's own words"),
		said("4", "maya", "", "tier not read yet"),
	}
	list[2].ByFactory = true
	factory.ReadComments(&it, list, readAt)
	got := outcomes(it)
	want := map[string]string{"1": factory.CommentAsked, "2": factory.CommentContext}
	if len(got) != len(want) || got["1"] != want["1"] || got["2"] != want["2"] {
		t.Fatalf("outcomes %v, want %v", got, want)
	}
	if len(it.Thread) != 1 || it.Thread[0].From != "manager" || !strings.Contains(it.Thread[0].Text, "@raj") || !strings.Contains(it.Thread[0].Text, "cover the batch path too") {
		t.Fatalf("the ask is not in the manager conversation: %+v", it.Thread)
	}
	for _, r := range it.CommentReads {
		if r.Line == "" || !r.At.Equal(readAt) {
			t.Fatalf("a read without its line or time: %+v", r)
		}
	}

	// Read again, and the tier of 4 is in: only 4 is new.
	list[3].Tier = factory.TierCollab
	factory.ReadComments(&it, list, readAt)
	if len(it.CommentReads) != 3 || len(it.Thread) != 2 {
		t.Fatalf("a second read handled comments twice or missed one: %+v", it.CommentReads)
	}
}

func TestReadCommentsWhenNoRunIsUnderWay(t *testing.T) {
	for name, tc := range map[string]factory.Item{
		"not started": {Num: 12, Kind: factory.KindIssue, Origin: factory.OriginForge, State: factory.StateNew},
		"finished":    func() factory.Item { it := runningJob(); it.DoneHow = "merged"; return it }(),
	} {
		t.Run(name, func(t *testing.T) {
			it := tc
			factory.ReadComments(&it, []factory.Comment{}, readAt)
			factory.ReadComments(&it, []factory.Comment{said("9", "raj", factory.TierCollab, "add a test")}, readAt)
			if o := outcomes(it)["9"]; o != factory.CommentNothing {
				t.Fatalf("outcome %q, want %q", o, factory.CommentNothing)
			}
			if len(it.Thread) != 0 {
				t.Fatalf("a job with no run was asked: %+v", it.Thread)
			}
		})
	}
}
