package main

import (
	"errors"
	"os"
	"strings"

	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workgraph"
)

// graphJournalStamp follows conversation identities, never a stored absolute
// path: a restored home may have moved since the task recorded its origin.
func graphJournalStamp(agent string) string {
	dir, ok := workGraphs.locate(agent)
	if !ok {
		return "-"
	}
	return session.FileStamp(session.Place{Dir: dir}.Transcript())
}

// graphBriefPlan binds crew goals to their submitted client identity. A goal
// edited in settings has no chat message, and must not borrow the first one.
func (b *appBackend) graphBriefPlan(plan *graphPlan) error {
	if plan.scope.Kind == workgraph.ScopeAgent {
		plan.briefAgent = plan.scope.ID
	}
	if plan.scope.Kind == workgraph.ScopeCrew {
		err := b.crewLayer().read(func(f *appCrewLayerFile) {
			if extra := f.Crews[plan.scope.ID]; extra != nil {
				plan.briefText = extra.Goal
				plan.briefAgent, plan.briefClient = extra.GoalAgent, extra.GoalClientID
			}
		})
		if err != nil {
			return err
		}
	}
	plan.originSig = graphJournalStamp(plan.briefAgent) + session.FileStamp(b.crewLayer().path)
	return nil
}

type graphMessage struct {
	source workgraph.MessageSource
	text   string
	cut    bool
}

type graphConversation struct {
	lines   map[int]graphMessage
	clients map[string]graphMessage
	first   *graphMessage
	missing bool
}

// graphConversationOf uses the same replay and user counter as agents.entries.
// Rewritten overlap lines resolve only when they retain a durable client id.
func graphConversationOf(agent string, record session.Record) graphConversation {
	return graphConversationSelect(agent, record, nil, "")
}

// graphConversationSelect retains only requested addresses after the record
// lease ends. A page cannot pin the whole history of every task's owner.
func graphConversationSelect(agent string, record session.Record, wanted map[int]bool, client string) graphConversation {
	out := graphConversation{lines: map[int]graphMessage{}, clients: map[string]graphMessage{}}
	n := 0
	floor := min(max(record.Floor, 0), len(record.Entries))
	clients := map[string]bool{client: true}
	for index, item := range record.Entries[:floor] {
		line := record.JournalLine(false, index)
		if (wanted == nil || wanted[line]) && item.Person != nil {
			clients[item.Person.ClientID] = true
		}
	}
	add := func(items []session.DisplayEntry, earlier bool, offset int) {
		for index, item := range items {
			if item.Role != "user" {
				continue
			}
			line := record.JournalLine(earlier, offset+index)
			message := graphMessage{source: workgraph.MessageSource{Agent: agent, Anchor: appUserEntryID(n), At: workgraph.Stamp(item.At)}}
			if out.first == nil {
				first := message
				text, cut := workgraph.Cut(item.Text, workgraph.MaxTextRunes)
				first.cut = cut
				first.text = strings.Clone(text)
				out.first = &first
			}
			if line > 0 && (wanted == nil || wanted[line]) {
				out.lines[line] = message
			}
			if item.Person != nil && item.Person.ClientID != "" && (wanted == nil || clients[item.Person.ClientID]) {
				out.clients[item.Person.ClientID] = message
			}
			n++
		}
	}
	add(record.Earlier, true, 0)
	add(record.Entries[floor:], false, floor)
	// A rewritten overlap can point back only through a durable message identity.
	// Its position or words are not evidence that it is the person's request.
	for index, item := range record.Entries[:floor] {
		line := record.JournalLine(false, index)
		if item.Role == "user" && item.Person != nil {
			if message, ok := out.clients[item.Person.ClientID]; ok {
				out.lines[line] = message
			}
		}
	}

	return out
}

type graphOriginReader struct {
	by     map[string]graphConversation
	wanted map[string]map[int]bool
	client string
}

// conversation reads each origin once per response, through the bounded shared
// record cache. Permission and parse errors remain errors, never removal claims.
func (r *graphOriginReader) conversation(agent string) (graphConversation, error) {
	if c, ok := r.by[agent]; ok {
		return c, nil
	}
	c := graphConversation{missing: true}
	dir, ok := workGraphs.locate(agent)
	if ok {
		path := session.Place{Dir: dir}.Transcript()
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			r.by[agent] = c
			return c, nil
		}
		if err != nil {
			return c, err
		}
		if !info.Mode().IsRegular() {
			return c, errors.New("the origin journal is not a regular file")
		}
		err = appRecords.WithRecord(session.Place{Dir: dir}.Transcript(), func(record session.Record) error {
			if record.Unreadable != "" {
				return errors.New(record.Unreadable)
			}
			wanted := r.wanted[agent]
			if r.wanted != nil && wanted == nil {
				wanted = map[int]bool{}
			}
			c = graphConversationSelect(agent, record, wanted, r.client)
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return c, err
		}
	}
	r.by[agent] = c
	return c, nil
}

func (r *graphOriginReader) source(anchor *workgraph.TextAnchor) (*workgraph.MessageSource, string, error) {
	if anchor == nil {
		return nil, "none", nil
	}
	if anchor.Machine != "" {
		return nil, "offline", nil
	}
	c, err := r.conversation(anchor.Agent)
	if err != nil {
		return nil, "", err
	}
	if m, ok := c.lines[anchor.Line]; ok {
		source := m.source
		return &source, "", nil
	}
	if c.missing {
		return nil, "removed", nil
	}
	return nil, "none", nil
}

func (b *appBackend) graphOrigins(res *workgraph.GraphResult, plan graphPlan) error {
	reader := graphOriginReader{by: map[string]graphConversation{}, wanted: map[string]map[int]bool{}, client: plan.briefClient}
	want := func(a *workgraph.TextAnchor) {
		if a == nil {
			return
		}
		if reader.wanted[a.Agent] == nil {
			reader.wanted[a.Agent] = map[int]bool{}
		}
		reader.wanted[a.Agent][a.Line] = true
	}
	for _, n := range res.Nodes {
		want(n.OriginAnchor)
	}
	if res.About.Root != nil {
		want(res.About.Root.OriginAnchor)
	}
	for i := range res.Nodes {
		n := &res.Nodes[i]
		var err error
		n.OriginSource, n.Origin, err = reader.source(n.OriginAnchor)
		if err != nil {
			return err
		}
	}
	if root := res.About.Root; root != nil {
		var err error
		root.OriginSource, root.Origin, err = reader.source(root.OriginAnchor)
		if err != nil {
			return err
		}
		text := ""
		if root.Instructions != nil {
			text = root.Instructions.Text
		}
		res.About.BriefFact = &workgraph.BriefFact{Text: text, Source: root.OriginSource, Origin: root.Origin, Cut: root.Instructions != nil && root.Instructions.Cut}
	} else {
		brief, err := reader.brief(plan)
		if err != nil {
			return err
		}
		res.About.BriefFact = brief
	}
	b.graphExternal(res)
	return nil
}

func (r *graphOriginReader) brief(plan graphPlan) (*workgraph.BriefFact, error) {
	text, cut := workgraph.Cut(plan.briefText, workgraph.MaxTextRunes)
	brief := &workgraph.BriefFact{Text: redact.Secrets(text), Cut: cut, Origin: "none"}
	if plan.briefAgent == "" {
		return brief, nil
	}
	c, err := r.conversation(plan.briefAgent)
	if err != nil {
		return nil, err
	}
	message := c.first
	if plan.scope.Kind == workgraph.ScopeCrew {
		m, ok := c.clients[plan.briefClient]
		message = nil
		if ok {
			message = &m
		}
		// CrewStart submits the goal as the new manager's first message. Older
		// callers supplied no client id; GoalAgent still records that exact door.
		if plan.briefClient == "" {
			message = c.first
		}
	}
	if message != nil {
		brief.Source, brief.Origin = &message.source, ""
		if plan.scope.Kind == workgraph.ScopeAgent {
			brief.Text, brief.Cut = message.text, message.cut
		}
	} else if c.missing {
		brief.Origin = "removed"
	}
	brief.Text = redact.Secrets(brief.Text)
	return brief, nil
}

// graphExternal preserves every endpoint while distinguishing another page
// from another level and a deleted record. The desk never has to infer removal
// from an absent card or infer a location from a task's title.
func (b *appBackend) graphExternal(res *workgraph.GraphResult) {
	visible := map[string]bool{}
	for _, n := range res.Nodes {
		visible[n.ID] = true
	}
	seen := map[string]bool{}
	names := map[string]string{}
	for _, edge := range res.Edges {
		if visible[edge.To] || seen[edge.To] {
			continue
		}
		seen[edge.To] = true
		target := workgraph.ExternalTask{Ref: edge.ToRef, Status: "outside"}
		switch edge.Target {
		case workgraph.TargetNode:
			target.Status = "paged"
		case workgraph.TargetMissing:
			target.Status = "removed"
		default:
			target.Where = b.graphOwnerName(edge.ToRef.Owner, names)
		}
		res.External = append(res.External, target)
	}
}

// graphOwnerName shares one lookup per owner and keeps the same external-owner
// bound as the task reader; an unread name stays unknown rather than costing a
// new traversal for every edge on a dense page.
func (b *appBackend) graphOwnerName(owner string, names map[string]string) string {
	if name, ok := names[owner]; ok {
		return name
	}
	if len(names) >= graphMaxExternal {
		return ""
	}
	name := ""
	if a, err := b.find(owner); err == nil {
		b.locked(func() { name = a.view.Title })
	}
	names[owner] = name
	return name
}
