package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"sync/atomic"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workstream"
	"github.com/Agent-Field/codeaf/internal/workstream/pagedoc"
)

const workstreamCreatedBy = "workstream"

var appWorkstreamAgents atomic.Pointer[appBackend]

type workstreamAgentDoor struct {
	backend *appBackend
	agentID string
}

var _ session.WorkstreamDoor = (*workstreamAgentDoor)(nil)

func appWorkstreamAgentDoor(sessionFile string) session.WorkstreamDoor {
	if b := appWorkstreamAgents.Load(); b != nil {
		return b.workstreamAgentDoor(sessionFile)
	}
	return nil
}

func (b *appBackend) workstreamAgentDoor(sessionFile string) session.WorkstreamDoor {
	id := appAgentID(sessionFile)
	if b.createdBy(id) != workstreamCreatedBy {
		return nil
	}
	return &workstreamAgentDoor{backend: b, agentID: id}
}

func (d *workstreamAgentDoor) workstreamOwner(ctx context.Context, writing bool) (*workstream.Store, workstream.Workstream, error) {
	s, err := d.backend.workstreamStore(false)
	if err != nil {
		return nil, workstream.Workstream{}, err
	}
	if s == nil {
		return nil, workstream.Workstream{}, session.ErrWorkstreamMissing
	}
	w, err := s.ByAgent(ctx, d.agentID)
	if errors.Is(err, workstream.ErrNotFound) {
		err = session.ErrWorkstreamMissing
	}
	if err == nil && writing && !w.DoneAt.IsZero() {
		err = session.ErrWorkstreamDone
	}
	return s, w, err
}

func (d *workstreamAgentDoor) Brief(ctx context.Context) (session.WorkstreamBrief, error) {
	_, w, err := d.workstreamOwner(ctx, false)
	return session.WorkstreamBrief{Name: w.Name, Why: w.Why}, err
}

func (d *workstreamAgentDoor) Read(ctx context.Context, ask session.WorkstreamReadAsk) (json.RawMessage, error) {
	s, w, err := d.workstreamOwner(ctx, false)
	if err != nil {
		return nil, err
	}
	switch ask.What {
	case "workstream":
		out, err := d.backend.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID})
		if err != nil {
			return nil, err
		}
		return json.Marshal(out.Workstream)
	case "pages":
		return workstreamAgentReadPages(ctx, s, w.ID)
	case "page":
		p, err := workstreamAgentPage(ctx, s, w.ID, ask.PageID, true)
		if err != nil {
			return nil, err
		}
		return json.Marshal(struct {
			appwire.WorkstreamPageNode
			Markdown *string `json:"markdown"`
		}{appWorkstreamPageNode(p.PageNode), p.Markdown})
	case "records":
		return workstreamAgentReadRecords(ctx, s, w.ID, ask)
	case "plan":
		p, err := s.Plan(ctx, w.ID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(appwire.WorkstreamPlanGetResult{Plan: appWorkstreamPlan(p)})
	case "goaheads":
		return workstreamAgentReadGoAheads(ctx, s, w.ID)
	}
	return nil, errors.New("what must name a workstream part")
}

func workstreamAgentReadPages(ctx context.Context, s *workstream.Store, id string) (json.RawMessage, error) {
	pages, err := s.Pages(ctx, id)
	if err != nil {
		return nil, err
	}
	out := appwire.WorkstreamPageListResult{Pages: []appwire.WorkstreamPageNode{}}
	for _, p := range pages {
		out.Pages = append(out.Pages, appWorkstreamPageNode(p))
	}
	return json.Marshal(out)
}

func workstreamAgentReadRecords(ctx context.Context, s *workstream.Store, id string, ask session.WorkstreamReadAsk) (json.RawMessage, error) {
	records, err := s.Records(ctx, id, ask.Kind, ask.State)
	if err != nil {
		return nil, err
	}
	out := appwire.WorkstreamRecordListResult{Records: []appwire.WorkstreamRecord{}}
	for _, r := range records {
		out.Records = append(out.Records, appWorkstreamRecord(r))
	}
	return json.Marshal(out)
}

func workstreamAgentReadGoAheads(ctx context.Context, s *workstream.Store, id string) (json.RawMessage, error) {
	grants, err := s.GoAheads(ctx, id, true)
	if err != nil {
		return nil, err
	}
	out := appwire.WorkstreamGoAheadListResult{GoAheads: []appwire.WorkstreamGoAhead{}}
	for _, g := range grants {
		out.GoAheads = append(out.GoAheads, appWorkstreamGoAhead(g))
	}
	return json.Marshal(out)
}

func workstreamAgentPage(ctx context.Context, s *workstream.Store, id, pageID string, markdown bool) (workstream.Page, error) {
	p, err := s.Page(ctx, pageID, markdown)
	if err == nil && p.WorkstreamID != id {
		err = workstream.ErrNotFound
	}
	return p, err
}

func (d *workstreamAgentDoor) Propose(ctx context.Context, p session.WorkstreamProposal) (session.WorkstreamProposed, error) {
	s, w, err := d.workstreamOwner(ctx, true)
	if err != nil {
		return session.WorkstreamProposed{}, err
	}
	if p.GoAhead != nil {
		p.Kind = "decision"
		p.Text, err = d.workstreamGoAheadText(ctx, s, w, *p.GoAhead)
		if err != nil {
			return session.WorkstreamProposed{}, err
		}
	}
	source, err := d.workstreamSource(ctx, s, w, p.Source)
	if err != nil {
		return session.WorkstreamProposed{}, err
	}
	r, change, err := s.ProposeRecord(ctx, workstream.RecordPropose{WorkstreamID: w.ID, Kind: p.Kind, Text: p.Text, Source: source, By: "agent", Kept: false})
	d.backend.workstreamChanged(change)
	return session.WorkstreamProposed{ID: r.ID}, workstreamAgentError(err)
}

func (d *workstreamAgentDoor) workstreamSource(ctx context.Context, s *workstream.Store, w workstream.Workstream, asked *workstream.Source) (*workstream.Source, error) {
	if asked == nil {
		return &workstream.Source{Kind: "chat", ID: d.agentID}, nil
	}
	source := *asked
	if source.ChatID != "" || source.Kind != "factory_job" && source.Factory != "" {
		return nil, errors.New("The source has fields for another kind.")
	}
	switch source.Kind {
	case "factory_job":
		n, err := strconv.Atoi(source.ID)
		if err != nil || strconv.Itoa(n) != source.ID || !workstreamAgentMember(w, source.Factory, n) {
			return nil, errors.New("The source job must be a member of this workstream, with its factory and decimal job number.")
		}
	case "page":
		if _, err := workstreamAgentPage(ctx, s, w.ID, source.ID, false); err != nil {
			return nil, err
		}
	case "url":
		if err := workstreamAgentSourceURL(source.ID); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("source.kind must be factory_job, page or url")
	}
	return &source, nil
}

func workstreamAgentSourceURL(id string) error {
	u, err := url.Parse(id)
	if err != nil || u.Host == "" || u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("A URL source must be an http(s):// URL.")
	}
	return nil
}

func workstreamAgentMember(w workstream.Workstream, factory string, number int) bool {
	if factory == "" || number <= 0 {
		return false
	}
	for _, job := range w.Jobs() {
		if job.Factory == factory && job.Number == number {
			return true
		}
	}
	return false
}

func (d *workstreamAgentDoor) Plan(ctx context.Context) (session.WorkstreamPlanState, error) {
	s, w, err := d.workstreamOwner(ctx, false)
	if err != nil {
		return session.WorkstreamPlanState{}, err
	}
	p, err := s.Plan(ctx, w.ID)
	return workstreamAgentPlan(p), err
}

func workstreamAgentPlan(p workstream.Plan) session.WorkstreamPlanState {
	return session.WorkstreamPlanState{Version: p.Version, Milestones: p.Milestones, Waves: p.Waves}
}

func (d *workstreamAgentDoor) SetPlan(ctx context.Context, base int, p session.WorkstreamPlanState) (session.WorkstreamPlanState, error) {
	s, w, err := d.workstreamOwner(ctx, true)
	if err != nil {
		return session.WorkstreamPlanState{}, err
	}
	saved, change, err := s.SetPlan(ctx, w.ID, base, p.Milestones, p.Waves, workstream.Attribution{By: "agent"})
	d.backend.workstreamChanged(change)
	return workstreamAgentPlan(saved), workstreamAgentError(err)
}

func (d *workstreamAgentDoor) workstreamGoAheadText(ctx context.Context, s *workstream.Store, w workstream.Workstream, ask session.WorkstreamGoAheadAsk) (string, error) {
	if err := session.WorkstreamValidateGoAhead(ask, d.backend.now()); err != nil {
		return "", err
	}
	scope, err := workstreamAgentGoAheadScope(ctx, s, w, ask)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s, up to $%.2f, until %s. %s", session.WorkstreamGoAheadProposalPrefix, scope, ask.CapUSD, ask.EndsOn, ask.Why), nil
}

func workstreamAgentGoAheadScope(ctx context.Context, s *workstream.Store, w workstream.Workstream, ask session.WorkstreamGoAheadAsk) (string, error) {
	if ask.Scope == "job" {
		if !workstreamAgentMember(w, ask.Factory, ask.Job) {
			return "", errors.New("The proposed go-ahead job must be a member of this workstream.")
		}
		return fmt.Sprintf("job %s#%d", ask.Factory, ask.Job), nil
	}
	p, err := s.Plan(ctx, w.ID)
	if err != nil {
		return "", err
	}
	if ask.Scope == "wave" {
		for _, wave := range p.Waves {
			if wave.N == ask.Wave {
				return fmt.Sprintf("wave %d", wave.N), nil
			}
		}
		return "", errors.New("The proposed go-ahead wave must exist in the plan.")
	}
	for _, milestone := range p.Milestones {
		if milestone.ID == ask.MilestoneID {
			return fmt.Sprintf("milestone \"%s\" (%s)", milestone.Title, milestone.ID), nil
		}
	}
	return "", errors.New("The proposed go-ahead milestone must exist in the plan.")
}

func (d *workstreamAgentDoor) WritePage(ctx context.Context, ask session.WorkstreamPageWrite) (session.WorkstreamPageWritten, error) {
	s, w, err := d.workstreamOwner(ctx, true)
	if err != nil {
		return session.WorkstreamPageWritten{}, err
	}
	source, err := d.workstreamSource(ctx, s, w, ask.Source)
	if err != nil {
		return session.WorkstreamPageWritten{}, err
	}
	source.Label = ask.UpdatedFrom
	doc := pagedoc.FromMarkdown(ask.Markdown)
	if len(doc) > workstream.DocMax {
		return session.WorkstreamPageWritten{}, session.ErrWorkstreamTooLarge
	}
	p, change, err := workstreamAgentWritePage(ctx, s, w.ID, ask, doc, workstream.Attribution{By: "agent", From: source})
	d.backend.workstreamChanged(change)
	return session.WorkstreamPageWritten{ID: p.ID, Title: p.Title, Version: p.Version}, workstreamAgentError(err)
}

func workstreamAgentWritePage(ctx context.Context, s *workstream.Store, id string, ask session.WorkstreamPageWrite, doc json.RawMessage, who workstream.Attribution) (workstream.Page, workstream.Change, error) {
	if ask.PageID == "" {
		return s.CreatePage(ctx, workstream.PageCreate{WorkstreamID: id, ParentID: ask.ParentID, Title: ask.Title, Owner: "agent", ReadByJobs: false, Doc: doc, Attribution: who})
	}
	p, err := workstreamAgentPage(ctx, s, id, ask.PageID, false)
	if err != nil {
		return workstream.Page{}, workstream.Change{}, err
	}
	return s.SavePage(ctx, p.ID, p.Version, doc, who)
}

func workstreamAgentError(err error) error {
	var stale *workstream.VersionConflict
	switch {
	case errors.As(err, &stale):
		return session.ErrWorkstreamVersion
	case errors.Is(err, workstream.ErrNotAgentPage):
		return session.ErrWorkstreamNotAgentPage
	case errors.Is(err, workstream.ErrTooLarge):
		return session.ErrWorkstreamTooLarge
	case errors.Is(err, workstream.ErrConflict):
		return session.ErrWorkstreamDone
	}
	return err
}

func (b *appBackend) workstreamAgentTurnSettled(id string) {
	if b.createdBy(id) != workstreamCreatedBy {
		return
	}
	s, err := b.workstreamStore(false)
	if err != nil || s == nil {
		return
	}
	w, err := s.ByAgent(context.Background(), id)
	if err == nil {
		b.workstreamChanged(workstream.Change{ID: w.ID, Parts: []workstream.ChangedPart{workstream.PartWorkstream}})
	}
}
