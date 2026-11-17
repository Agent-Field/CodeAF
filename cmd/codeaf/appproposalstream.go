package main

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

type appProposalStream struct {
	proposal   appwire.Proposal
	handles    map[string]string
	pending    string
	received   int
	seq        int
	size       int
	incomplete bool
	emit       func(int, json.RawMessage)
}

func (s *appProposalStream) write(chunk string) {
	s.received += len(chunk)
	s.pending += chunk
	for {
		line, rest, ok := strings.Cut(s.pending, "\n")
		if !ok {
			return
		}
		s.pending = rest
		s.line(line)
	}
}

func (s *appProposalStream) finish(clean bool) {
	before := s.seq
	s.line(s.pending)
	s.incomplete = strings.HasPrefix(strings.TrimSpace(s.pending), "{") && s.seq == before
	s.incomplete = s.incomplete || !clean || (s.size > 0 && len(s.proposal.Helpers) < s.size)
	s.pending = ""
}

func (s *appProposalStream) line(line string) string {
	line = strings.TrimSpace(line)
	var kind struct {
		T string `json:"t"`
	}
	if json.Unmarshal([]byte(line), &kind) != nil {
		return "invalid JSON"
	}
	switch kind.T {
	case "shape":
		var shape struct {
			Size  int    `json:"size"`
			Teams int    `json:"teams"`
			Why   string `json:"why"`
		}
		if json.Unmarshal([]byte(line), &shape) != nil || shape.Size < 1 || shape.Teams < 0 || strings.TrimSpace(shape.Why) == "" {
			return "invalid shape fields"
		}
		s.size = shape.Size
	case "name":
		var name struct {
			Name string `json:"name"`
			Goal string `json:"goal"`
		}
		if json.Unmarshal([]byte(line), &name) != nil || strings.TrimSpace(name.Name) == "" {
			return "invalid name fields"
		}
		s.proposal.Name, s.proposal.Goal = strings.TrimSpace(name.Name), strings.TrimSpace(name.Goal)
	case "team":
		var team appwire.ProposalTeam
		if json.Unmarshal([]byte(line), &team) != nil {
			return "invalid team fields"
		}
		original := team.Lead
		team.Lead = s.handle(team.Lead)
		if strings.TrimSpace(team.ID) == "" || teamstore.ValidHandle(team.Lead) != nil || strings.TrimSpace(team.Name) == "" {
			return "invalid team fields"
		}
		for _, old := range s.proposal.Teams {
			if old.ID == team.ID {
				return "duplicate team ID"
			}
		}
		if s.handles == nil {
			s.handles = map[string]string{}
		}
		s.handles[original] = team.Lead
		s.proposal.Teams = append(s.proposal.Teams, team)
		if original != team.Lead {
			var fields map[string]json.RawMessage
			_ = json.Unmarshal([]byte(line), &fields)
			fields["lead"], _ = json.Marshal(team.Lead)
			normalized, _ := json.Marshal(fields)
			line = string(normalized)
		}
	case "member":
		var member appwire.ProposalHelper
		if json.Unmarshal([]byte(line), &member) != nil {
			return "invalid member fields"
		}
		original, reportsTo := member.Handle, member.ReportsTo
		member.Handle = s.handle(member.Handle)
		if err := teamstore.ValidHandle(member.Handle); err != nil {
			return "invalid member handle: " + err.Error()
		}
		if strings.TrimSpace(member.Brief) == "" {
			return "member assignment is empty"
		}
		for _, old := range s.proposal.Helpers {
			if old.Handle == member.Handle {
				return "duplicate member handle"
			}
		}
		if repaired, ok := s.handles[member.ReportsTo]; ok {
			member.ReportsTo = repaired
		}
		if member.ReportsTo == "" {
			member.ReportsTo = "manager"
		}
		if member.ReportsTo != "manager" {
			found := false
			for _, old := range s.proposal.Helpers {
				if old.Handle == member.ReportsTo && old.ReportsTo == "manager" {
					found = true
				}
			}
			if !found {
				return "reports_to must name manager or an earlier lead"
			}
		}
		if member.Team != "" {
			found := false
			for _, team := range s.proposal.Teams {
				if team.ID == member.Team {
					found = true
				}
			}
			if !found {
				return "team must name a declared team"
			}
		}
		if s.handles == nil {
			s.handles = map[string]string{}
		}
		s.handles[original] = member.Handle
		s.proposal.Helpers = append(s.proposal.Helpers, member)
		if original != member.Handle || reportsTo != "" && reportsTo != member.ReportsTo {
			var fields map[string]json.RawMessage
			_ = json.Unmarshal([]byte(line), &fields)
			fields["handle"], _ = json.Marshal(member.Handle)
			fields["reports_to"], _ = json.Marshal(member.ReportsTo)
			normalized, _ := json.Marshal(fields)
			line = string(normalized)
		}
	default:
		return "unknown proposal line type"
	}
	s.seq++
	if s.emit != nil {
		s.emit(s.seq, json.RawMessage(line))
	}
	return ""
}

func appProposalShortHandle(handle string) string {
	if len(handle) <= teamstore.HandleMax {
		return handle
	}
	for i, r := range handle {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' && i > 0) {
			return handle
		}
	}
	short := handle[:teamstore.HandleMax]
	if at := strings.LastIndex(short, "-"); at >= teamstore.HandleMin {
		return short[:at]
	}
	return strings.TrimRight(short, "-")
}

func (s *appProposalStream) handle(original string) string {
	if chosen, ok := s.handles[original]; ok {
		return chosen
	}
	base := appProposalShortHandle(original)
	if teamstore.ValidHandle(base) != nil && base == original {
		return original
	}
	candidate := base
	for n := 2; ; n++ {
		clash := false
		for _, chosen := range s.handles {
			if chosen == candidate {
				clash = true
				break
			}
		}
		if !clash && teamstore.ValidHandle(candidate) == nil {
			return candidate
		}
		suffix := strconv.Itoa(n)
		head := base
		if limit := teamstore.HandleMax - len(suffix); len(head) > limit {
			head = head[:limit]
		}
		candidate = strings.TrimRight(head, "-") + suffix
	}
}
