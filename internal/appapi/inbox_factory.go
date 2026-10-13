package appapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	factoryapi "github.com/Agent-Field/codeaf/internal/factory/api"
)

// FactoryInboxSource uses the factory's person-scoped Contract 2 view and
// permissions. Solo uses its launch bearer; attached teams use their session.
// Tokens remain memory-only and never appear in item ids or errors.
type FactoryInboxSource struct {
	ID, URL, Token string
	Client         *http.Client
	mu             sync.Mutex
	jobs           map[string]factoryapi.Job
	resolutions    map[string]inboxSourceResolution
}

func (f *FactoryInboxSource) request(ctx context.Context, method, path string, body any, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	r, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(f.URL, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return agentError(503, "not_allowed", "factory is unavailable")
	}
	r.Header.Set("Authorization", "Bearer "+f.Token)
	r.Header.Set("Content-Type", "application/json")
	client := f.Client
	if client == nil {
		client = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	response, err := client.Do(r)
	if err != nil {
		return agentError(503, "not_allowed", "factory is unavailable")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20+1))
	if err != nil || len(data) > 8<<20 {
		return agentError(502, "not_allowed", "factory response is unavailable")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct{ Error, Reason string }
		_ = json.Unmarshal(data, &failure)
		if failure.Reason == "" {
			failure.Reason = "not_allowed"
		}
		if failure.Error == "" {
			failure.Error = "factory refused the decision"
		}
		return agentError(response.StatusCode, failure.Reason, failure.Error)
	}
	if out != nil && json.Unmarshal(data, out) != nil {
		return agentError(502, "not_allowed", "factory response is unavailable")
	}
	return nil
}
func factoryInboxKind(kind string) string {
	switch kind {
	case "approve_plan":
		return "approval_plan"
	case "approve_proof":
		return "approval_proof"
	case "approve_start":
		return "approval_start"
	}
	return kind
}
func factoryInboxID(factory string, job factoryapi.Job) string {
	return "job:" + url.QueryEscape(factory) + ":" + url.QueryEscape(job.JobID) + ":" + factoryInboxKind(job.NeedsYou.Kind) + ":" + job.NeedsYou.Since.UTC().Format(time.RFC3339Nano)
}
func (f *FactoryInboxSource) InboxItems(ctx context.Context) ([]InboxItem, error) {
	var queue factoryapi.Queue
	if err := f.request(ctx, "GET", "/v1/queue", nil, &queue); err != nil {
		return nil, err
	}
	out := []InboxItem{}
	f.mu.Lock()
	previous := f.jobs
	f.mu.Unlock()
	jobs := map[string]factoryapi.Job{}
	for _, job := range queue.Jobs {
		needs := job.NeedsYou
		if needs == nil || !inboxKind(factoryInboxKind(needs.Kind)) {
			continue
		}
		verbs := []string{}
		if job.Can.Approve {
			verbs = append(verbs, "approve")
		}
		if job.Can.Answer {
			verbs = append(verbs, "answer")
		}
		if job.Can.RetryPublish {
			verbs = append(verbs, "retry_publish")
		}
		if job.Can.RunAgain {
			verbs = append(verbs, "run_again")
		}
		if job.Can.Reply {
			verbs = append(verbs, "reply")
		}
		if len(verbs) == 0 && !job.Can.Undo {
			continue
		}
		// Assignee-only items must not leak into a different person's Inbox.
		who := needs.WhoCanAnswer
		if who == "job_person" && job.Assignee != queue.Me {
			continue
		}
		if needs.Question != nil && needs.Question.Answered != nil {
			continue
		}
		source := SourceRef{Kind: "job", ID: job.JobID, Factory: f.ID}
		title := job.Title
		if needs.Question != nil && needs.Question.Text != "" {
			title = needs.Question.Text
		}
		before := []DisplayEntry{}
		if job.Desk != nil {
			for _, entry := range job.Desk.Thread {
				before = append(before, DisplayEntry{Kind: "message", Text: entry.Text})
			}
		}
		id := factoryInboxID(f.ID, job)
		// Recording the approval can update the factory's fallback since
		// stamp. Its pending decision still names the original waiting item.
		if job.Approval != nil && job.Approval.State == "approving" {
			for oldID, old := range previous {
				if old.JobID == job.JobID && old.NeedsYou.Kind == needs.Kind {
					id = oldID
					break
				}
			}
		}
		jobs[id] = job
		out = append(out, InboxItem{ID: id, Kind: factoryInboxKind(needs.Kind), Source: source, Title: title, Path: []SourceRef{source, {Kind: "factory", ID: f.ID}}, CreatedAt: needs.Since, Context: InboxContext{Before: inboxBefore(before)}, Verbs: verbs, WhoCanAnswer: []string{who}})
	}
	f.mu.Lock()
	f.resolutions = map[string]inboxSourceResolution{}
	for id, old := range f.jobs {
		if _, waiting := jobs[id]; waiting {
			continue
		}
		for _, job := range queue.Jobs {
			if job.JobID != old.JobID {
				continue
			}
			by := ""
			if job.Approval != nil && job.Approval.State == "approved" {
				by = job.Approval.DecidedBy
			}
			for _, question := range job.Questions {
				if question.Answer != nil && !question.Answer.At.Before(old.NeedsYou.Since) {
					by = question.Answer.By
				}
			}
			if by != "" {
				how := "someone_else"
				if by == queue.Me {
					how = "in_place"
				}
				f.resolutions[id] = inboxSourceResolution{how, by}
			}
		}
	}
	f.jobs = jobs
	f.mu.Unlock()
	return out, nil
}
func (f *FactoryInboxSource) NativeInboxHold(item InboxItem, answer InboxAnswerRequest) bool {
	return strings.HasPrefix(item.Kind, "approval_") && (answer.Verb == "approve" || answer.Choice == "approve")
}
func (f *FactoryInboxSource) ValidateInbox(ctx context.Context, item InboxItem, answer InboxAnswerRequest) error {
	f.mu.Lock()
	job, ok := f.jobs[item.ID]
	f.mu.Unlock()
	if !ok {
		return agentError(409, "already_answered", "this factory item is no longer waiting")
	}
	if answer.Rule != nil || len(answer.Items) > 0 {
		return agentError(422, "bad_input", "factory answers do not take permission rules")
	}
	verb := answer.Verb
	if verb == "" {
		verb = "answer"
		if answer.Choice == "approve" {
			verb = "approve"
		}
	}
	if !slices.Contains(item.Verbs, verb) {
		return agentError(403, "not_allowed", "this factory decision belongs to its assignee")
	}
	if answer.Expect != nil && ((answer.Expect.State != nil && *answer.Expect.State != job.State) || (answer.Expect.Version != nil && *answer.Expect.Version != uint64(job.Version))) {
		return agentError(409, "stale", "the job changed; read it again before acting")
	}
	return nil
}
func (f *FactoryInboxSource) AnswerInbox(ctx context.Context, item InboxItem, answer InboxAnswerRequest) (InboxAnswerResponse, error) {
	if err := f.ValidateInbox(ctx, item, answer); err != nil {
		return InboxAnswerResponse{}, err
	}
	f.mu.Lock()
	job := f.jobs[item.ID]
	f.mu.Unlock()
	version := job.Version
	expect := &factoryapi.Expect{State: job.State, Version: &version}
	verb := answer.Verb
	choice := answer.Choice
	if verb == "" {
		verb = "answer"
		if choice == "approve" {
			verb = "approve"
		}
	}
	if !slices.Contains([]string{"approve", "answer", "retry_publish", "run_again", "reply"}, verb) {
		return InboxAnswerResponse{}, agentError(422, "bad_input", "choose an offered factory action")
	}
	var body any = struct {
		Choice string             `json:"choice,omitempty"`
		Words  string             `json:"words,omitempty"`
		Expect *factoryapi.Expect `json:"expect"`
	}{choice, answer.Words, expect}
	if verb == "approve" || verb == "retry_publish" {
		verb = "approve"
		body = factoryapi.Approve{Words: answer.Words, Commit: job.HeadCommit, Expect: expect}
	}
	if verb == "run_again" {
		body = struct {
			Expect *factoryapi.Expect `json:"expect"`
		}{expect}
	}
	var reply struct {
		UndoUntil *time.Time `json:"undo_until"`
	}
	err := f.request(ctx, "POST", fmt.Sprintf("/v1/jobs/%d/%s", job.ID, verb), body, &reply)
	return InboxAnswerResponse{OK: err == nil, UndoUntil: reply.UndoUntil}, err
}
func (f *FactoryInboxSource) UndoInbox(ctx context.Context, item InboxItem, expect *AgentExpect) error {
	f.mu.Lock()
	job, ok := f.jobs[item.ID]
	f.mu.Unlock()
	if !ok {
		return agentError(409, "conflict", "the factory Undo window has ended")
	}
	body := map[string]any{}
	if expect != nil {
		fresh := &factoryapi.Expect{}
		if expect.State != nil {
			fresh.State = *expect.State
		}
		if expect.Version != nil {
			version := int(*expect.Version)
			if version < 0 || uint64(version) != *expect.Version {
				return agentError(422, "bad_input", "invalid factory version")
			}
			fresh.Version = &version
		}
		body["expect"] = fresh
	}
	return f.request(ctx, "POST", fmt.Sprintf("/v1/jobs/%d/undo", job.ID), body, nil)
}

// WatchInbox relays only the small factory facts that can change a waiting
// item. Reconnection retains the source cursor; periodic snapshots repair a
// missed or expired factory replay without minting new Inbox identities.
func (f *FactoryInboxSource) WatchInbox(ctx context.Context, publish func(FactoryEvent)) {
	cursor := ""
	for ctx.Err() == nil {
		f.followInbox(ctx, &cursor, publish)
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (f *FactoryInboxSource) followInbox(ctx context.Context, cursor *string, publish func(FactoryEvent)) {
	r, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(f.URL, "/")+"/v1/events", nil)
	if err != nil {
		return
	}
	r.Header.Set("Authorization", "Bearer "+f.Token)
	r.Header.Set("Accept", "text/event-stream")
	if *cursor != "" {
		r.Header.Set("Last-Event-ID", *cursor)
	}
	client := f.Client
	if client == nil {
		client = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	response, err := client.Do(r)
	if err != nil {
		return
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	kind, id := "", ""
	var data strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if id != "" {
				*cursor = id
			}
			if factoryInboxEvent(kind) && json.Valid([]byte(data.String())) {
				publish(FactoryEvent{Factory: f.ID, Type: kind, Data: json.RawMessage(data.String())})
			}
			kind, id = "", ""
			data.Reset()
			continue
		}
		if value, ok := strings.CutPrefix(line, "event:"); ok {
			kind = strings.TrimSpace(value)
		}
		if value, ok := strings.CutPrefix(line, "id:"); ok {
			id = strings.TrimSpace(value)
		}
		if value, ok := strings.CutPrefix(line, "data:"); ok {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			if data.Len()+len(value) > 64<<10 {
				return
			}
			data.WriteString(strings.TrimPrefix(value, " "))
		}
	}
}
func factoryInboxEvent(kind string) bool {
	switch kind {
	case "needs_you", "question", "approved", "job.state", "publish_failed", "publishing", "published":
		return true
	}
	return false
}

func (f *FactoryInboxSource) ResolvedInbox(item InboxItem) (string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	resolution := f.resolutions[item.ID]
	if resolution.how == "" {
		return "no_longer_needed", ""
	}
	return resolution.how, resolution.by
}

// PendingInbox exposes a factory-owned hold after app reattachment; the
// factory, rather than this launch's timer map, still owns its deadline.
func (f *FactoryInboxSource) PendingInbox(item InboxItem) (time.Time, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	job, ok := f.jobs[item.ID]
	if !ok || !job.Can.Undo || job.Approval == nil || job.Approval.State != "approving" {
		return time.Time{}, false
	}
	return job.Approval.UndoUntil, true
}

// A delayed answer keeps the sampled version. A later refresh must not make
// an old decision look like consent to evidence that changed during Undo.
func (f *FactoryInboxSource) SnapshotInboxAnswer(item InboxItem, answer InboxAnswerRequest) InboxAnswerRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	job := f.jobs[item.ID]
	expect := AgentExpect{}
	if answer.Expect != nil {
		expect = *answer.Expect
	}
	if expect.State == nil {
		state := job.State
		expect.State = &state
	}
	if expect.Version == nil {
		version := uint64(job.Version)
		expect.Version = &version
	}
	answer.Expect = &expect
	return answer
}
