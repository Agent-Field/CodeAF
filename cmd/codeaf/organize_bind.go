package main

// Production session.Organizer bound at [v3Organizer]. Tick and the in-window
// standing pass both reach it through [v3OrganizePass]. Journals are ingested
// into discovery.db from this door — not only from tests — then hybrid
// evidence is gathered, RoleOrganize is called through session.Agent.Organize,
// and a typed wsapi.ActionPlan is validated before apply. A down embedder or
// organizer defers with "discovery delayed". Construction that cannot even
// form the type returns nil so pending jobs stay pending.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/embed"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workspace"
	"github.com/Agent-Field/codeaf/internal/wsapi"
	"github.com/Agent-Field/codeaf/internal/wsdiscover"
)

type organizeFn func(context.Context, session.OrganizeRequest) (session.OrganizePlan, error)

type doorOrganizer struct {
	embedder embed.Embedder
	organize organizeFn
}

func newDoorOrganizer(embedder embed.Embedder, organize organizeFn) session.Organizer {
	return &doorOrganizer{embedder: embedder, organize: organize}
}

func v3TickEmbedder() embed.Embedder {
	settings, err := config.LoadKeyless()
	if err != nil {
		return nil
	}
	return v3Embedder(settings, nil, nil)
}

func (o *doorOrganizer) Organize(ctx context.Context, job workspace.Job) (string, string, error) {
	if o == nil {
		return workspace.JobDeferred, embed.LabelDelayed, nil
	}
	svc, disc := openV3FolderServiceWith(o.embedder)
	if svc == nil {
		return workspace.JobDeferred, embed.LabelDelayed, nil
	}
	defer svc.Close()
	if err := ingestWorldJournals(ctx, disc); err != nil {
		return workspace.JobDeferred, embed.LabelDelayed, nil
	}
	if discoveryIsDelayed(ctx, svc) {
		return workspace.JobDeferred, embed.LabelDelayed, nil
	}
	return o.applyJob(ctx, svc, job)
}

func (o *doorOrganizer) applyJob(ctx context.Context, svc *wsapi.Service, job workspace.Job) (string, string, error) {
	hits, query := gatherEvidence(ctx, svc, job)
	plan, err := o.rolePlan(ctx, session.OrganizeRequest{
		ChatID: job.ChatID, SourceRev: job.SourceRev,
		Evidence:  formatEvidence(query, hits),
		Hierarchy: formatHierarchy(ctx, svc, job.ChatID),
		Degraded:  evidenceDegraded(hits),
	})
	if err != nil {
		return workspace.JobDeferred, embed.LabelDelayed, nil
	}
	return commitPlan(ctx, svc, job, preparePlan(ctx, svc, actionPlanOf(plan, job, hits)))
}

func (o *doorOrganizer) rolePlan(ctx context.Context, req session.OrganizeRequest) (session.OrganizePlan, error) {
	if o != nil && o.organize != nil {
		return o.organize(ctx, req)
	}
	return roleOrganize(ctx, req)
}

func roleOrganize(ctx context.Context, req session.OrganizeRequest) (session.OrganizePlan, error) {
	settings, err := config.LoadKeyless()
	if err != nil {
		return session.OrganizePlan{}, err
	}
	cfg, err := v3StandingPosture(settings)
	if err != nil {
		return session.OrganizePlan{}, err
	}
	agent, err := session.New(cfg)
	if err != nil {
		return session.OrganizePlan{}, err
	}
	defer func() { _ = agent.Close() }()
	return agent.Organize(ctx, req)
}

func ingestWorldJournals(ctx context.Context, disc *discoveryAdapter) error {
	if disc == nil {
		return nil
	}
	for _, row := range session.ReadWorld(session.PlacesRoot()).Sessions() {
		records := journalRecords(row.ID, session.ReadTranscript(row.Transcript))
		if len(records) == 0 {
			continue
		}
		if err := disc.Ingest(ctx, records); err != nil {
			return err
		}
	}
	return nil
}

func journalRecords(id string, rec session.Record) []wsdiscover.Record {
	entries := wholeTranscript(rec)
	out := make([]wsdiscover.Record, 0, len(entries))
	for i, entry := range entries {
		if !ingestible(entry.Role, entry.Text) {
			continue
		}
		out = append(out, wsdiscover.Record{
			SessionID:  id,
			SourceRef:  "chat:" + id,
			Speaker:    entry.Role,
			Text:       strings.TrimSpace(entry.Text),
			Generation: 1,
			Ordinal:    int64(i + 1),
		})
	}
	return out
}

func wholeTranscript(rec session.Record) []session.DisplayEntry {
	if len(rec.Earlier) == 0 {
		return rec.Entries
	}
	out := append([]session.DisplayEntry{}, rec.Earlier...)
	if rec.Floor >= 0 && rec.Floor < len(rec.Entries) {
		out = append(out, rec.Entries[rec.Floor:]...)
	}
	return out
}

func ingestible(role, text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	switch role {
	case "user", "assistant", "note":
		return true
	}
	return false
}

func discoveryIsDelayed(ctx context.Context, svc *wsapi.Service) bool {
	if svc == nil {
		return true
	}
	view, err := svc.IndexProgress(ctx)
	if err != nil {
		return true
	}
	return view.Delayed
}

func gatherEvidence(ctx context.Context, svc *wsapi.Service, job workspace.Job) ([]wsapi.SearchHit, string) {
	query := chatQuery(job.ChatID)
	if svc == nil || query == "" {
		return nil, query
	}
	hits, err := svc.SearchEvidence(ctx, wsapi.SearchQuery{Query: query, Limit: 20})
	if err != nil {
		return nil, query
	}
	return hits, query
}

func chatQuery(chatID string) string {
	for _, row := range session.ReadWorld(session.PlacesRoot()).Sessions() {
		if row.ID != chatID {
			continue
		}
		return lastUserText(session.ReadTranscript(row.Transcript))
	}
	return ""
}

func lastUserText(rec session.Record) string {
	entries := wholeTranscript(rec)
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Role == "user" {
			if text := strings.TrimSpace(entries[i].Text); text != "" {
				return text
			}
		}
	}
	return ""
}

func formatEvidence(query string, hits []wsapi.SearchHit) string {
	var b strings.Builder
	if query != "" {
		b.WriteString("query: ")
		b.WriteString(query)
		b.WriteByte('\n')
	}
	for _, hit := range hits {
		b.WriteString(hit.ScoreKind)
		b.WriteByte(' ')
		b.WriteString(hit.Ref)
		b.WriteByte(' ')
		b.WriteString(hit.Passage)
		b.WriteByte('\n')
	}
	return b.String()
}

// formatHierarchy is the folder catalog RoleOrganize is contracted to see.
// Live J11 on SHA 709bf019 completed no-action with Security evidence because
// the prompt had no collection_id to cite.
func formatHierarchy(ctx context.Context, svc *wsapi.Service, chatID string) string {
	if svc == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("folders\n")
	if store := svc.Workspace(); store != nil {
		cols, err := store.Collections(ctx)
		if err == nil {
			for _, col := range cols {
				b.WriteString(col.ID)
				b.WriteByte(' ')
				b.WriteString(col.Name)
				b.WriteByte('\n')
			}
		}
	}
	here, err := svc.PlacementsOf(ctx, workspace.Ref{Kind: workspace.ConversationKind, ID: chatID})
	if err != nil || len(here) == 0 {
		return b.String()
	}
	b.WriteString("already in")
	for _, folder := range here {
		b.WriteByte(' ')
		b.WriteString(folder.ID)
	}
	b.WriteByte('\n')
	return b.String()
}

func evidenceDegraded(hits []wsapi.SearchHit) bool {
	for _, hit := range hits {
		if hit.Degraded || hit.ScoreKind == wsapi.ScoreExpansion {
			return true
		}
	}
	return false
}

func actionPlanOf(plan session.OrganizePlan, job workspace.Job, hits []wsapi.SearchHit) wsapi.ActionPlan {
	kind := strings.TrimSpace(plan.Kind)
	if kind == "" {
		kind = wsapi.PlanNoAction
	}
	return wsapi.ActionPlan{
		Kind:          kind,
		ChatID:        firstNonEmpty(plan.ChatID, job.ChatID),
		SourceRev:     firstNonEmpty(plan.SourceRev, job.SourceRev),
		Model:         plan.Model,
		PromptVersion: plan.PromptVersion,
		Degraded:      plan.Degraded,
		Evidence:      evidenceOf(hits),
		Actions:       decodeActions(plan.Actions),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func decodeActions(raw []json.RawMessage) []wsapi.Action {
	out := make([]wsapi.Action, 0, len(raw))
	for _, item := range raw {
		action, ok := decodeAction(item)
		if !ok {
			continue
		}
		out = append(out, action)
	}
	return out
}

func decodeAction(raw json.RawMessage) (wsapi.Action, bool) {
	var action wsapi.Action
	_ = json.Unmarshal(raw, &action)
	fillWireAction(&action, raw)
	return action, action.Kind != ""
}

func fillWireAction(action *wsapi.Action, raw json.RawMessage) {
	var wire struct {
		Kind         string   `json:"kind"`
		CollectionID string   `json:"collection_id"`
		FromID       string   `json:"from_id"`
		ToID         string   `json:"to_id"`
		FolderName   string   `json:"folder_name"`
		Purpose      string   `json:"purpose"`
		Reason       string   `json:"reason"`
		ParentIDs    []string `json:"parent_ids"`
		ObjectID     string   `json:"object_id"`
		Ref          struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
		} `json:"ref"`
	}
	if json.Unmarshal(raw, &wire) != nil {
		return
	}
	if action.Kind == "" {
		action.Kind = wire.Kind
	}
	if action.CollectionID == "" {
		action.CollectionID = wire.CollectionID
	}
	if action.FromID == "" {
		action.FromID = wire.FromID
	}
	if action.ToID == "" {
		action.ToID = wire.ToID
	}
	if action.FolderName == "" {
		action.FolderName = wire.FolderName
	}
	if action.Purpose == "" {
		action.Purpose = wire.Purpose
	}
	if action.Reason == "" {
		action.Reason = wire.Reason
	}
	if len(action.ParentIDs) == 0 {
		action.ParentIDs = wire.ParentIDs
	}
	fillActionRef(action, wire.Ref.Kind, wire.Ref.ID, wire.ObjectID)
}

func fillActionRef(action *wsapi.Action, kind, id, objectID string) {
	if action == nil || action.Ref.ID != "" {
		return
	}
	if id != "" {
		action.Ref = workspace.Ref{Kind: workspace.Kind(kind), ID: id}
		return
	}
	if objectID != "" {
		action.Ref = workspace.Ref{Kind: workspace.ConversationKind, ID: objectID}
	}
}

func evidenceOf(hits []wsapi.SearchHit) []wsapi.EvidenceRef {
	out := make([]wsapi.EvidenceRef, 0, len(hits))
	for _, hit := range hits {
		if strings.TrimSpace(hit.Passage) == "" {
			continue
		}
		out = append(out, wsapi.EvidenceRef{
			SourceRef:   hit.Ref,
			PassageHash: passageHash(hit.Passage),
			Quote:       hit.Passage,
		})
	}
	return out
}

func passageHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func preparePlan(ctx context.Context, svc *wsapi.Service, plan wsapi.ActionPlan) wsapi.ActionPlan {
	plan = refuseInventedMembership(plan)
	for i := range plan.Actions {
		plan.Actions[i] = fillOneRevision(ctx, svc, plan.Actions[i], plan.ChatID)
	}
	return plan
}

func refuseInventedMembership(plan wsapi.ActionPlan) wsapi.ActionPlan {
	if plan.Kind == wsapi.PlanNoAction {
		plan.Actions = nil
		return plan
	}
	if plan.Model == "" || plan.Degraded || !hasPassageEvidence(plan) {
		plan.Kind = wsapi.PlanNoAction
		plan.Actions = nil
	}
	return plan
}

func hasPassageEvidence(plan wsapi.ActionPlan) bool {
	if evidenceHasHash(plan.Evidence) {
		return true
	}
	for _, action := range plan.Actions {
		if evidenceHasHash(action.Evidence) {
			return true
		}
	}
	return false
}

func evidenceHasHash(refs []wsapi.EvidenceRef) bool {
	for _, ref := range refs {
		if strings.TrimSpace(ref.PassageHash) != "" {
			return true
		}
	}
	return false
}

func fillOneRevision(ctx context.Context, svc *wsapi.Service, action wsapi.Action, chatID string) wsapi.Action {
	if action.Ref.ID == "" && chatID != "" {
		action.Ref = workspace.Ref{Kind: workspace.ConversationKind, ID: chatID}
	}
	if action.ExpectedRevision != 0 || action.CollectionID == "" || svc == nil {
		return action
	}
	folder, _, err := svc.FolderSnapshot(ctx, action.CollectionID)
	if err == nil {
		action.ExpectedRevision = folder.Revision
	}
	return action
}

func commitPlan(ctx context.Context, svc *wsapi.Service, job workspace.Job, plan wsapi.ActionPlan) (string, string, error) {
	if err := svc.ValidateActionPlan(ctx, plan); err != nil {
		plan = noActionOf(job, plan.Model)
	}
	if _, err := svc.ApplyActionPlan(ctx, plan); err != nil {
		return workspace.JobDeferred, embed.LabelDelayed, nil
	}
	if plan.Kind == "" {
		plan.Kind = wsapi.PlanNoAction
	}
	return workspace.JobCompleted, plan.Kind, nil
}

func noActionOf(job workspace.Job, model string) wsapi.ActionPlan {
	return wsapi.ActionPlan{
		Kind: wsapi.PlanNoAction, ChatID: job.ChatID, SourceRev: job.SourceRev, Model: model,
	}
}
