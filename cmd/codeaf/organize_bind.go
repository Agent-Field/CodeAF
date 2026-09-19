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
	"unicode/utf8"

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
	return v3EmbedderAccount(settings, nil, nil, v3StandingEmbedAccount())
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
	if explicitOrganizeJob(job) {
		return o.surveyExisting(ctx, svc, job)
	}
	return o.applyChat(ctx, svc, job, false)
}

func explicitOrganizeJob(job workspace.Job) bool {
	return job.CoalesceKey == workspace.OrganizeExistingKey
}

const organizeSurveyCap = 8

func (o *doorOrganizer) surveyExisting(ctx context.Context, svc *wsapi.Service, job workspace.Job) (string, string, error) {
	ids := surveyChatIDs(ctx, svc)
	start := surveyStartIndex(ids, job.Cursor)
	if start >= len(ids) {
		persistSurveyCursor(ctx, svc, job, "")
		return workspace.JobCompleted, wsapi.PlanNoAction, nil
	}
	kind := wsapi.PlanNoAction
	processed := 0
	for i := start; i < len(ids); i++ {
		if processed >= organizeSurveyCap {
			persistSurveyCursor(ctx, svc, job, ids[i])
			return workspace.JobDeferred, embed.LabelDelayed, nil
		}
		one := job
		one.ChatID = ids[i]
		state, detail, err := o.applyChat(ctx, svc, one, true)
		if err != nil || state == workspace.JobDeferred || state == workspace.JobFailed {
			persistSurveyCursor(ctx, svc, job, ids[i])
			return workspace.JobDeferred, embed.LabelDelayed, nil
		}
		if state == workspace.JobCompleted && detail != "" && detail != wsapi.PlanNoAction {
			kind = detail
		}
		processed++
		next := ""
		if i+1 < len(ids) {
			next = ids[i+1]
		}
		persistSurveyCursor(ctx, svc, job, next)
	}
	return workspace.JobCompleted, kind, nil
}

func surveyChatIDs(ctx context.Context, svc *wsapi.Service) []string {
	if svc == nil {
		return nil
	}
	root, err := svc.RootSnapshot(ctx)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(root.Unfiled))
	for _, place := range root.Unfiled {
		if id := strings.TrimSpace(place.Ref.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// surveyStartIndex is the first unfiled id at or after the checkpoint.
// A missing cursor id does not restart Unfiled[0..]; it continues in world
// order so eight no-action chats cannot pin the survey forever.
func surveyStartIndex(ids []string, cursor string) int {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return 0
	}
	for i, id := range ids {
		if id == cursor {
			return i
		}
	}
	past := false
	for _, id := range (folderWorld{}).ConversationIDs() {
		if id == cursor {
			past = true
			continue
		}
		if !past {
			continue
		}
		for i, unfiled := range ids {
			if unfiled == id {
				return i
			}
		}
	}
	return len(ids)
}

func persistSurveyCursor(ctx context.Context, svc *wsapi.Service, job workspace.Job, cursor string) {
	if svc == nil || strings.TrimSpace(job.ID) == "" || strings.TrimSpace(job.Fence) == "" {
		return
	}
	store := svc.Workspace()
	if store == nil {
		return
	}
	_ = store.SetJobCursor(ctx, job.ID, job.Fence, cursor)
}

func (o *doorOrganizer) applyChat(ctx context.Context, svc *wsapi.Service, job workspace.Job, survey bool) (string, string, error) {
	hits, query := gatherEvidence(ctx, svc, job)
	plan, err := o.rolePlan(ctx, session.OrganizeRequest{
		ChatID: job.ChatID, SourceRev: job.SourceRev,
		Evidence:  formatEvidence(query, hits),
		Hierarchy: formatHierarchy(ctx, svc, job.ChatID),
		Degraded:  evidenceDegraded(hits),
		Survey:    survey,
	})
	if err != nil {
		return workspace.JobDeferred, embed.LabelDelayed, nil
	}
	return commitPlan(ctx, svc, job, preparePlan(ctx, svc, actionPlanOf(plan, job, hits), survey))
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

// formatHierarchy is the folder graph RoleOrganize is contracted to see.
// Live J11 on SHA d993a93c still completed no-action: folder names without
// member conversation ids left the model unable to map cited chat: ids.
func formatHierarchy(ctx context.Context, svc *wsapi.Service, chatID string) string {
	if svc == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("folders\n")
	writeFolderCatalog(ctx, svc, &b)
	writeAlreadyIn(ctx, svc, chatID, &b)
	return b.String()
}

func writeFolderCatalog(ctx context.Context, svc *wsapi.Service, b *strings.Builder) {
	if svc == nil || b == nil {
		return
	}
	store := svc.Workspace()
	if store == nil {
		return
	}
	cols, err := store.Collections(ctx)
	if err != nil {
		return
	}
	for _, col := range cols {
		b.WriteString(col.ID)
		b.WriteByte(' ')
		b.WriteString(col.Name)
		b.WriteByte('\n')
		writeFolderMembers(ctx, store, col.ID, b)
	}
}

func writeFolderMembers(ctx context.Context, store *workspace.Store, folderID string, b *strings.Builder) {
	if store == nil || b == nil {
		return
	}
	members, err := store.Members(ctx, folderID)
	if err != nil {
		return
	}
	for _, member := range members {
		kind := string(member.Kind)
		if kind == "" {
			kind = string(workspace.ConversationKind)
		}
		b.WriteByte(' ')
		b.WriteString(kind)
		b.WriteByte(' ')
		b.WriteString(member.ID)
		b.WriteByte('\n')
	}
}

func writeAlreadyIn(ctx context.Context, svc *wsapi.Service, chatID string, b *strings.Builder) {
	if svc == nil || b == nil {
		return
	}
	here, err := svc.PlacementsOf(ctx, workspace.Ref{Kind: workspace.ConversationKind, ID: chatID})
	if err != nil || len(here) == 0 {
		return
	}
	b.WriteString("already in")
	for _, folder := range here {
		b.WriteByte(' ')
		b.WriteString(folder.ID)
	}
	b.WriteByte('\n')
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

func preparePlan(ctx context.Context, svc *wsapi.Service, plan wsapi.ActionPlan, allowCreate bool) wsapi.ActionPlan {
	plan = refuseInventedMembership(plan)
	if !allowCreate {
		plan = refuseEmptyRootCreate(ctx, svc, plan)
	}
	plan = refuseGreetingOrTiny(plan, chatQuery(plan.ChatID))
	for i := range plan.Actions {
		plan.Actions[i] = fillOneRevision(ctx, svc, plan.Actions[i], plan.ChatID)
	}
	return plan
}

// refuseGreetingOrTiny keeps a hello or an empty line from CreateFolder.
// That is success without a graph write, on the survey and the automatic path.
func refuseGreetingOrTiny(plan wsapi.ActionPlan, query string) wsapi.ActionPlan {
	if !isGreetingOrTiny(query) {
		return plan
	}
	plan.Kind = wsapi.PlanNoAction
	plan.Actions = nil
	return plan
}

func isGreetingOrTiny(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return true
	}
	n := utf8.RuneCountInString(text)
	if n > 24 {
		return false
	}
	switch strings.Trim(strings.ToLower(text), "!?., ") {
	case "hi", "hello", "hey", "yo", "thanks", "thank you", "ok", "okay",
		"yes", "no", "hi there", "hello there", "hey there", "good morning",
		"good night", "gm", "gn":
		return true
	}
	return n <= 4 && !strings.ContainsAny(text, `/\{}=:@`)
}

func refuseEmptyRootCreate(ctx context.Context, svc *wsapi.Service, plan wsapi.ActionPlan) wsapi.ActionPlan {
	if !planCreatesFolder(plan) || collectionCount(ctx, svc) > 0 {
		return plan
	}
	plan.Kind = wsapi.PlanNoAction
	plan.Actions = nil
	return plan
}

func planCreatesFolder(plan wsapi.ActionPlan) bool {
	if plan.Kind == wsapi.PlanCreateFolder {
		return true
	}
	for _, action := range plan.Actions {
		if action.Kind == wsapi.PlanCreateFolder {
			return true
		}
	}
	return false
}

func collectionCount(ctx context.Context, svc *wsapi.Service) int {
	if svc == nil {
		return 0
	}
	store := svc.Workspace()
	if store == nil {
		return 0
	}
	cols, err := store.Collections(ctx)
	if err != nil {
		return 0
	}
	return len(cols)
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
	if svc == nil {
		return action
	}
	if action.Kind == wsapi.PlanCreateFolder {
		if root, err := svc.RootSnapshot(ctx); err == nil {
			action.ExpectedRootRevision = root.Revision
		}
	}
	if action.ExpectedRevision != 0 || action.CollectionID == "" {
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
