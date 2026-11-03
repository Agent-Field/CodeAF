package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/workstream"
)

const workstreamReadMax = 60000

const workstreamPlanChanged = "The plan changed while you were proposing; nothing was added. Read the plan and propose again."

const workstreamPromptRules = "\n- You propose; the person keeps. Decisions, questions, jobs and go-aheads you raise with propose_decision, propose_question, propose_jobs and propose_goahead are proposals until the person keeps them.\n" +
	"- You write only your own pages, with write_agent_page. The person's pages are theirs; you do not change them.\n" +
	"- Keep your page titled \"" + WorkstreamWhereItStandsTitle + "\" current with write_agent_page: a short summary of where the work stands, with updated_from saying what it was updated from. Create it if it is missing.\n" +
	"- You never invent numbers. Counts, costs, states and dates come from workstream_read; on a page, use a live block such as [live: jobs] instead of writing a number.\n" +
	"- You start nothing. Jobs start only when the person launches them or inside a go-ahead they granted."

func (c Config) mayWorkstream() bool {
	return c.Workstream != nil && !c.readingTask() && !c.InTask && !c.Errand
}

func workstreamSection(config Config) string {
	if !config.mayWorkstream() {
		return ""
	}
	return "\n\n" + workstreamPage(config)
}

func workstreamPage(config Config) string {
	brief, ok := config.Workstream.Brief()
	intro := "You are the agent of a workstream. Read it with workstream_read before you answer."
	if ok && strings.TrimSpace(brief.Name) != "" {
		intro = fmt.Sprintf("You are the agent of the workstream %q.", workstreamCut(strings.Join(strings.Fields(brief.Name), " "), 48))
		if why := strings.Join(strings.Fields(brief.Why), " "); why != "" {
			intro += " Why it exists: " + workstreamCut(why, 64)
		}
	}
	return "WORKSTREAM\n" + intro + workstreamPromptRules
}

func workstreamWithoutPage(page string) string {
	start := strings.Index(page, "\n\nWORKSTREAM\n")
	if start < 0 {
		return page
	}
	end := strings.Index(page[start:], workstreamPromptRules)
	if end < 0 {
		return page
	}
	return page[:start] + page[start+end+len(workstreamPromptRules):]
}

func (a *Agent) workstreamTools() []bare.Tool {
	return a.workstreamToolsAt(time.Now)
}

func (a *Agent) workstreamToolsAt(now func() time.Time) []bare.Tool {
	if !a.config.mayWorkstream() {
		return nil
	}
	door := a.config.Workstream
	return []bare.Tool{
		workstreamTool("workstream_read", "Read this workstream's recorded facts. Absent numbers are unknown; never estimate them.", workstreamReadSchema, func(ctx context.Context, ask WorkstreamReadAsk) (string, error) {
			return workstreamRead(ctx, door, ask)
		}),
		workstreamRecordTool(door, "decision"),
		workstreamRecordTool(door, "question"),
		workstreamTool("propose_jobs", "Append draft jobs to the plan. Nothing is created in the Factory or started; the person launches drafts.", workstreamJobsSchema, func(ctx context.Context, ask workstreamJobsAsk) (string, error) {
			return workstreamProposeJobs(ctx, door, ask)
		}),
		workstreamTool("propose_goahead", "Propose a go-ahead as a decision. The cap and end date are proposals; this grants nothing.", workstreamGoAheadSchema, func(ctx context.Context, ask WorkstreamGoAheadAsk) (string, error) {
			return workstreamProposeGoAhead(ctx, door, ask, now())
		}),
		workstreamTool("write_agent_page", "Write only your own pages, marked as agent-written with their source and time. The person's pages are refused. Keep the page titled \""+WorkstreamWhereItStandsTitle+"\" current; the app shows it on the Overview.", workstreamPageSchema, func(ctx context.Context, ask WorkstreamPageWrite) (string, error) {
			return workstreamWritePage(ctx, door, ask)
		}),
	}
}

func workstreamTool[T any](name, description, schemaJSON string, run func(context.Context, T) (string, error)) bare.Tool {
	var schema workstreamArgumentSchema
	_ = json.Unmarshal([]byte(schemaJSON), &schema)
	return bare.Tool{
		Name:        name,
		Description: description,
		Schema:      json.RawMessage(schemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var ask T
			fields, err := workstreamDecode(args, schema, &ask)
			if err != nil {
				return rejectToolArguments(ctx, invalidArgumentsPrefix+err.Error()), true, nil
			}
			if err := workstreamFieldRules(fields, ask); err != nil {
				return rejectToolArguments(ctx, invalidArgumentsPrefix+err.Error()), true, nil
			}
			text, err := run(ctx, ask)
			if err != nil {
				var argument *toolArgumentError
				if errors.As(err, &argument) {
					return rejectToolArguments(ctx, err.Error()), true, nil
				}
				return err.Error(), true, nil
			}
			return text, false, nil
		},
	}
}

func workstreamFieldRules(fields map[string]any, ask any) error {
	if err := workstreamReferenceFields(fields, ask); err != nil {
		return err
	}
	_, page := fields["page_id"]
	switch ask := ask.(type) {
	case WorkstreamReadAsk:
		if page != (ask.What == "page") {
			return errors.New("page_id is required for and only for page")
		}
		if ask.What != "records" {
			return workstreamRefuseFields(fields, "kind", "state")
		}
	case workstreamJobsAsk:
		if ask.Wave != nil {
			return workstreamRefuseFields(fields, "wave_title", "milestone_id")
		}
	case WorkstreamGoAheadAsk:
		return workstreamGoAheadFields(fields, ask)
	case WorkstreamPageWrite:
		if page {
			return workstreamRefuseFields(fields, "title", "parent_id")
		}
	}
	return nil
}

func workstreamReferenceFields(fields map[string]any, ask any) error {
	ids := make(map[string]string)
	var source *workstream.Source
	switch ask := ask.(type) {
	case WorkstreamReadAsk:
		ids["page_id"] = ask.PageID
	case workstreamJobsAsk:
		ids["milestone_id"] = ask.MilestoneID
	case WorkstreamGoAheadAsk:
		ids["milestone_id"] = ask.MilestoneID
	case WorkstreamPageWrite:
		ids["page_id"] = ask.PageID
		ids["parent_id"] = ask.ParentID
		source = ask.Source
	case workstreamRecordAsk:
		source = ask.Source
	}
	for _, name := range []string{"page_id", "parent_id", "milestone_id"} {
		if _, exists := fields[name]; exists && strings.TrimSpace(ids[name]) == "" {
			return fmt.Errorf("%s must name an existing record", name)
		}
	}
	if source != nil && source.Kind != "factory_job" {
		fields, _ := fields["source"].(map[string]any)
		return workstreamRefuseFields(fields, "factory")
	}
	return nil
}

func workstreamRefuseFields(fields map[string]any, names ...string) error {
	for _, name := range names {
		if _, exists := fields[name]; exists {
			return fmt.Errorf("%s is not allowed for this request", name)
		}
	}
	return nil
}

func workstreamGoAheadFields(fields map[string]any, ask WorkstreamGoAheadAsk) error {
	switch ask.Scope {
	case "job":
		return workstreamRefuseFields(fields, "wave", "milestone_id")
	case "wave":
		return workstreamRefuseFields(fields, "factory", "job", "milestone_id")
	case "milestone":
		return workstreamRefuseFields(fields, "factory", "job", "wave")
	}
	return nil
}

func workstreamRead(ctx context.Context, door WorkstreamDoor, ask WorkstreamReadAsk) (string, error) {
	if ask.What == "page" && strings.TrimSpace(ask.PageID) == "" {
		return "", errors.New("page_id is required for page")
	}
	raw, err := door.Read(ctx, ask)
	if err != nil {
		return "", err
	}
	text := string(raw)
	if ask.What == "workstream" {
		text = "Numbers absent below are unknown; say so and never estimate them.\n" + text
	}
	if len(text) > workstreamReadMax {
		const tail = "\nThe workstream read was cut."
		text = workstreamCut(text, workstreamReadMax-len(tail)) + tail
	}
	return text, nil
}

func workstreamCut(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit]
}

type workstreamRecordAsk struct {
	Text   string             `json:"text"`
	Source *workstream.Source `json:"source,omitempty"`
}

func workstreamRecordTool(door WorkstreamDoor, kind string) bare.Tool {
	return workstreamTool("propose_"+kind, "Propose a "+kind+". Only the person can keep it; it is always proposed by the agent.", workstreamRecordSchema, func(ctx context.Context, ask workstreamRecordAsk) (string, error) {
		if err := workstreamText(ask.Text, "text", workstreamRecordMax, true, false); err != nil {
			return "", &toolArgumentError{repair: err.Error()}
		}
		if err := workstreamSourceText(ask.Source); err != nil {
			return "", &toolArgumentError{repair: err.Error()}
		}
		p, err := door.Propose(ctx, WorkstreamProposal{Kind: kind, Text: ask.Text, Source: ask.Source})
		if err != nil {
			return "", err
		}
		return "Proposed " + kind + " " + p.ID + ". Nothing is kept until the person keeps it.", nil
	})
}

func workstreamText(text, field string, limit int, required, line bool) error {
	if !utf8.ValidString(text) || len(text) > limit || required && strings.TrimSpace(text) == "" {
		return fmt.Errorf("%s must be %smost %d bytes", field, workstreamTextBound(required), limit)
	}
	for _, r := range text {
		if unicode.IsControl(r) && (line || r != '\n' && r != '\t') {
			return fmt.Errorf("%s has a control character or more than one line", field)
		}
	}
	return nil
}

func workstreamTextBound(required bool) string {
	if required {
		return "non-empty and at "
	}
	return "at "
}

func workstreamSourceText(source *workstream.Source) error {
	if source == nil {
		return nil
	}
	if err := workstreamText(source.Label, "source.label", workstreamLineMax, false, true); err != nil {
		return err
	}
	if source.Factory != "" && source.Kind != "factory_job" {
		return errors.New("source.factory is allowed only for factory_job")
	}
	return workstreamText(source.ID, "source.id", 4096, true, false)
}

type workstreamDraftAsk struct {
	Title string `json:"title"`
	Repo  string `json:"repo,omitempty"`
	Text  string `json:"text,omitempty"`
}

type workstreamJobsAsk struct {
	Jobs        []workstreamDraftAsk `json:"jobs"`
	Wave        *int                 `json:"wave,omitempty"`
	WaveTitle   string               `json:"wave_title,omitempty"`
	MilestoneID string               `json:"milestone_id,omitempty"`
}

func workstreamValidateJobs(ask workstreamJobsAsk) error {
	if len(ask.Jobs) < 1 || len(ask.Jobs) > workstreamJobsMax {
		return fmt.Errorf("jobs must contain 1–%d drafts", workstreamJobsMax)
	}
	if ask.Wave != nil && *ask.Wave <= 0 {
		return errors.New("wave must be an existing positive wave number")
	}
	if err := workstreamText(ask.WaveTitle, "wave_title", workstreamLineMax, false, true); err != nil {
		return err
	}
	for _, job := range ask.Jobs {
		if err := workstreamValidateDraft(job); err != nil {
			return err
		}
	}
	return nil
}

func workstreamValidateDraft(job workstreamDraftAsk) error {
	if err := workstreamText(job.Title, "title", workstreamLineMax, true, true); err != nil {
		return err
	}
	if err := workstreamText(job.Repo, "repo", workstreamLineMax, false, true); err != nil {
		return err
	}
	return workstreamText(job.Text, "text", workstreamDraftTextMax, false, false)
}

func workstreamProposeJobs(ctx context.Context, door WorkstreamDoor, ask workstreamJobsAsk) (string, error) {
	if err := workstreamValidateJobs(ask); err != nil {
		return "", &toolArgumentError{repair: err.Error()}
	}
	drafts, err := workstreamDrafts(ask.Jobs)
	if err != nil {
		return "", err
	}
	plan, err := door.Plan(ctx)
	if err != nil {
		return "", err
	}
	wave, err := workstreamSaveDrafts(ctx, door, plan, ask, drafts)
	if errors.Is(err, ErrWorkstreamVersion) {
		plan, err = door.Plan(ctx)
		if err != nil {
			return "", err
		}
		wave, err = workstreamSaveDrafts(ctx, door, plan, ask, drafts)
	}
	if errors.Is(err, ErrWorkstreamVersion) {
		return "", errors.New(workstreamPlanChanged)
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Added %d draft jobs to wave %d of the plan. They are drafts: nothing runs until the person launches them.", len(drafts), wave), nil
}

func workstreamSaveDrafts(ctx context.Context, door WorkstreamDoor, plan WorkstreamPlanState, ask workstreamJobsAsk, drafts []workstream.PlanItem) (int, error) {
	next, wave, err := workstreamAppendDrafts(plan, ask, drafts)
	if err != nil {
		return 0, err
	}
	_, err = door.SetPlan(ctx, plan.Version, next)
	return wave, err
}

func workstreamDrafts(jobs []workstreamDraftAsk) ([]workstream.PlanItem, error) {
	out := make([]workstream.PlanItem, 0, len(jobs))
	for _, job := range jobs {
		var id [8]byte
		if _, err := rand.Read(id[:]); err != nil {
			return nil, err
		}
		out = append(out, workstream.PlanItem{Kind: "draft", DraftID: "draft_" + hex.EncodeToString(id[:]), Title: job.Title, Repo: job.Repo, Text: job.Text})
	}
	return out, nil
}

func workstreamAppendDrafts(plan WorkstreamPlanState, ask workstreamJobsAsk, drafts []workstream.PlanItem) (WorkstreamPlanState, int, error) {
	plan.Waves = append([]workstream.Wave(nil), plan.Waves...)
	if ask.Wave != nil {
		for i, wave := range plan.Waves {
			if wave.N == *ask.Wave {
				plan.Waves[i].Items = append(append([]workstream.PlanItem(nil), wave.Items...), drafts...)
				return plan, wave.N, nil
			}
		}
		return plan, 0, errors.New("wave must name an existing plan wave")
	}
	if ask.MilestoneID != "" && !workstreamHasMilestone(plan, ask.MilestoneID) {
		return plan, 0, errors.New("milestone_id must name an existing plan milestone")
	}
	n := 1
	if len(plan.Waves) > 0 {
		n = plan.Waves[len(plan.Waves)-1].N + 1
	}
	plan.Waves = append(plan.Waves, workstream.Wave{N: n, Title: ask.WaveTitle, MilestoneID: ask.MilestoneID, AutoStart: false, Items: append([]workstream.PlanItem(nil), drafts...)})
	return plan, n, nil
}

func workstreamHasMilestone(plan WorkstreamPlanState, id string) bool {
	for _, milestone := range plan.Milestones {
		if milestone.ID == id {
			return true
		}
	}
	return false
}

func workstreamProposeGoAhead(ctx context.Context, door WorkstreamDoor, ask WorkstreamGoAheadAsk, now time.Time) (string, error) {
	if err := WorkstreamValidateGoAhead(ask, now); err != nil {
		return "", &toolArgumentError{repair: err.Error()}
	}
	scope, err := workstreamGoAheadScope(ctx, door, ask)
	if err != nil {
		return "", err
	}
	text := fmt.Sprintf("%s%s, up to $%.2f, until %s. %s", WorkstreamGoAheadProposalPrefix, scope, ask.CapUSD, ask.EndsOn, ask.Why)
	p, err := door.Propose(ctx, WorkstreamProposal{Kind: "decision", Text: text, GoAhead: &ask})
	if err != nil {
		return "", err
	}
	return "Proposed a go-ahead as decision " + p.ID + ". It grants nothing; the person grants it if they agree.", nil
}

// WorkstreamValidateGoAhead is shared with the app so its clock validates proposals too.
func WorkstreamValidateGoAhead(ask WorkstreamGoAheadAsk, now time.Time) error {
	if !(ask.CapUSD > 0 && ask.CapUSD <= workstreamGoAheadCapMax) {
		return fmt.Errorf("cap_usd must be greater than 0 and at most %d", workstreamGoAheadCapMax)
	}
	end, err := time.Parse("2006-01-02", ask.EndsOn)
	today := now.UTC().Truncate(24 * time.Hour)
	if err != nil || !end.After(today) || end.After(today.AddDate(0, 0, workstreamGoAheadDaysMax)) {
		return fmt.Errorf("ends_on must be YYYY-MM-DD, after today in UTC and at most %d days ahead", workstreamGoAheadDaysMax)
	}
	if err := workstreamText(ask.Why, "why", workstreamGoAheadWhyMax, true, false); err != nil {
		return err
	}
	return workstreamValidateGoAheadTarget(ask)
}

func workstreamValidateGoAheadTarget(ask WorkstreamGoAheadAsk) error {
	if workstreamGoAheadOtherFields(ask) {
		return errors.New("The go-ahead has fields for another scope.")
	}
	switch ask.Scope {
	case "job":
		if ask.Factory == "" || ask.Job <= 0 {
			return errors.New("job scope requires only factory and a positive job")
		}
	case "wave":
		if ask.Wave <= 0 {
			return errors.New("wave scope requires only a positive wave")
		}
	case "milestone":
		if ask.MilestoneID == "" {
			return errors.New("milestone scope requires only milestone_id")
		}
	default:
		return errors.New("scope must be job, wave or milestone")
	}
	return nil
}

func workstreamGoAheadOtherFields(ask WorkstreamGoAheadAsk) bool {
	switch ask.Scope {
	case "job":
		return ask.Wave != 0 || ask.MilestoneID != ""
	case "wave":
		return ask.Factory != "" || ask.Job != 0 || ask.MilestoneID != ""
	case "milestone":
		return ask.Factory != "" || ask.Job != 0 || ask.Wave != 0
	}
	return false
}

func workstreamGoAheadScope(ctx context.Context, door WorkstreamDoor, ask WorkstreamGoAheadAsk) (string, error) {
	if ask.Scope == "job" {
		return fmt.Sprintf("job %s#%d", ask.Factory, ask.Job), nil
	}
	plan, err := door.Plan(ctx)
	if err != nil {
		return "", err
	}
	if ask.Scope == "wave" {
		for _, wave := range plan.Waves {
			if wave.N == ask.Wave {
				return fmt.Sprintf("wave %d", wave.N), nil
			}
		}
		return "", errors.New("wave must name an existing plan wave")
	}
	for _, milestone := range plan.Milestones {
		if milestone.ID == ask.MilestoneID {
			return fmt.Sprintf("milestone \"%s\" (%s)", milestone.Title, milestone.ID), nil
		}
	}
	return "", errors.New("milestone_id must name an existing plan milestone")
}

func workstreamWritePage(ctx context.Context, door WorkstreamDoor, ask WorkstreamPageWrite) (string, error) {
	if ask.PageID == "" {
		if err := workstreamText(ask.Title, "title", workstreamLineMax, true, true); err != nil {
			return "", &toolArgumentError{repair: err.Error()}
		}
	}
	if err := workstreamText(ask.UpdatedFrom, "updated_from", workstreamLineMax, true, true); err != nil {
		return "", &toolArgumentError{repair: err.Error()}
	}
	if err := workstreamSourceText(ask.Source); err != nil {
		return "", &toolArgumentError{repair: err.Error()}
	}
	p, err := door.WritePage(ctx, ask)
	if errors.Is(err, ErrWorkstreamVersion) {
		return "", errors.New("The page changed while you were writing. Read it again before writing.")
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Wrote %s (%s, version %d), marked as written by the agent from %q.", p.Title, p.ID, p.Version, ask.UpdatedFrom), nil
}
