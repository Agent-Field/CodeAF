package main

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// The demo includes shared membership and enough exchanges to exercise the
// overview's independent scrolling. Waking is disabled in this throwaway data
// so opening a fixture never sends its historical requests to a provider.
func writeTeams(root string, projects map[string]*demoProject, ids map[string]string, now time.Time) error {
	quiet, cap := false, 5.0
	var members []teamstore.Member
	for i, talk := range demoConversations {
		if talk.archived {
			continue
		}
		p := projects[talk.project]
		file := filepath.Join(p.bucket, ids[talk.title], "transcript.jsonl")
		members = append(members, teamstore.Member{Key: file, File: file, Where: p.dir, Word: talk.title, Handle: fmt.Sprintf("member%d", i+1)})
		if len(members) == 6 {
			break
		}
	}
	primary := teamstore.Team{ID: teamstore.NewID(), Name: "Interface cleanup", Members: members,
		Manager: members[0].Key, Made: now.Add(-48 * time.Hour), Settings: teamstore.Settings{Wake: &quiet, CapUSDDay: &cap}}
	primary.SetHue(teamstore.HueSpec{Hue: 200})
	secondary := teamstore.Team{ID: teamstore.NewID(), Name: "Release notes", Members: []teamstore.Member{members[1], members[3]},
		Manager: members[3].Key, Made: now.Add(-24 * time.Hour), Settings: teamstore.Settings{Wake: &quiet}}
	secondary.SetHue(teamstore.HueSpec{Hue: 80})
	if err := teamstore.Save(root, []teamstore.Team{primary, secondary}); err != nil {
		return err
	}
	for i := 0; i < 24; i++ {
		member := members[1+i%(len(members)-1)]
		text := fmt.Sprintf("Check the layout at width %d and report any clipped labels or missing actions.", 80+i*4)
		at := now.Add(-time.Duration(24-i) * 3 * time.Minute)
		id, err := teamstore.AppendTrafficID(root, primary.ID, teamstore.Entry{Kind: teamstore.KindNote, From: teamstore.FromManager, To: member.Handle, Text: text, At: at})
		if err != nil {
			return err
		}
		replyText := "The labels fit. The member preview and interaction links remain reachable at this width."
		replyID, err := teamstore.AppendTrafficID(root, primary.ID, teamstore.Entry{Kind: teamstore.KindNote, From: member.Handle, To: teamstore.ToManager, Text: replyText, Answers: id, At: at.Add(time.Minute)})
		if err != nil {
			return err
		}
		// These journal entries use the same tool receipts and delivered message
		// envelope as live sessions, so every demo interaction link can land.
		callID := fmt.Sprintf("demo-team-send-%d", i)
		for _, line := range []journalLine{
			{Type: "message", Role: "assistant", ToolCalls: []ai.ToolCall{call(callID, "team_send", map[string]any{"team": primary.ID, "to": member.Handle, "text": text})}},
			answer(callID, "Sent ("+teamstore.ThreadNumber(id)+")."),
			{Type: "message", Role: "user", Content: fmt.Sprintf("Team traffic in %q\nfrom @%s %s: %s", primary.Name, member.Handle, teamstore.ThreadNumber(replyID), replyText)},
		} {
			if err := appendJSONL(members[0].File, line); err != nil {
				return err
			}
		}
		postID := fmt.Sprintf("demo-team-post-%d", i)
		for _, line := range []journalLine{
			{Type: "message", Role: "user", Content: fmt.Sprintf("Team traffic in %q\n◆ from manager %s: %s", primary.Name, teamstore.ThreadNumber(id), text)},
			{Type: "message", Role: "assistant", ToolCalls: []ai.ToolCall{call(postID, "team_post", map[string]any{"text": replyText})}},
			answer(postID, "Posted as "+teamstore.ThreadNumber(replyID)+"."),
		} {
			if err := appendJSONL(member.File, line); err != nil {
				return err
			}
		}
	}
	_, err := teamstore.Raise(root, teamstore.Packet{Team: teamstore.Person, Origin: primary.ID, RaisedBy: "manager", Kind: teamstore.PacketQuestion,
		Question: "Which layout should we review first?", Options: []teamstore.Option{
			{ID: "wide", Label: "Wide terminal", Consequence: "Review the full member grid and interaction table."},
			{ID: "narrow", Label: "Narrow terminal", Consequence: "Review scrolling and access to every member."},
		}})
	return err
}
