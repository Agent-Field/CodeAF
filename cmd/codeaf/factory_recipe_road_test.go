package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/session"
)

// fakeRoad is the editor's road over a file held in memory: the preview is the
// engine's own edit of it, and a pull request records what it would commit.
type fakeRoad struct {
	text     string
	previews []appwire.FactoryPipelinePreviewParams
	prs      []appwire.FactoryPipelinePRParams
}

const fakeRoadSHA = "0123456789abcdef0123456789abcdef01234567"

func (r *fakeRoad) FactoryPipelineSource(_ context.Context, p appwire.FactoryPipelineSourceParams) (appwire.FactoryPipelineSource, error) {
	// The engine's own reading of the file, as the real road checks it.
	src, problems, ok := pipeline.Inspect(r.text)
	if !ok {
		return appwire.FactoryPipelineSource{}, errors.New(strings.Join(problems, "; "))
	}
	var local api.PipelineEditSource
	local.SetParsed(src)
	return appwire.FactoryPipelineSource{Factory: p.Factory, Repo: p.Repo, BaseSHA: fakeRoadSHA, State: appwire.PipelineSourceFile, Editable: true, Text: r.text,
		Pipelines: local.Pipelines, Steps: local.Steps, Rules: &src.Rules, AskFirst: &src.AskFirst, Never: &src.Never}, nil
}

func (r *fakeRoad) FactoryPipelinePreview(_ context.Context, p appwire.FactoryPipelinePreviewParams) (appwire.FactoryPipelinePreview, error) {
	r.previews = append(r.previews, p)
	ed, err := pipeline.ApplyEdits(p.BaseText, appwire.PipelineEditOps(p.Edits))
	if err != nil {
		return appwire.FactoryPipelinePreview{}, err
	}
	return appwire.FactoryPipelinePreview{Factory: p.Factory, Repo: p.Repo, BaseSHA: p.BaseSHA, Text: ed.Text, Changed: ed.Changed}, nil
}

func (r *fakeRoad) FactoryPipelinePR(_ context.Context, p appwire.FactoryPipelinePRParams) (appwire.FactoryPipelinePRDone, error) {
	r.prs = append(r.prs, p)
	return appwire.FactoryPipelinePRDone{Factory: p.Factory, Repo: p.Repo, Outcome: "opened", BaseSHA: p.ExpectedBaseSHA,
		PR: &appwire.FactoryPipelinePR{Number: 41, URL: "https://github.com/acme/web/pull/41"}}, nil
}

func roadDoor(road *fakeRoad) roadRecipeDoor {
	return roadRecipeDoor{
		fileRecipeDoor: fileRecipeDoor{dirs: func(string) string { return "" }},
		factory:        appLocalID,
		repos:          func() ([]string, error) { return []string{"acme/web", "acme/api"}, nil },
		road:           func() pipelineRoad { return road },
	}
}

// A CHAT'S BANKED STEP TAKES THE EDITOR'S ROAD: the file is read through the
// factory, the step is the editor's add_step in before proof, and the pull
// request carries exactly the previewed text from the commit it was read at.
// Nothing touches a checkout: the file door here knows none.
func TestAChatsBankedStepOpensThePipelineEditorsPullRequest(t *testing.T) {
	base, err := pipeline.Encode(pipeline.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	road := &fakeRoad{text: string(base)}
	door := roadDoor(road)
	ctx := context.Background()

	now, err := door.Stages(ctx, "web", factory.KindIssue)
	if err != nil || strings.Join(now, " ") != "plan approve write test review proof" {
		t.Fatalf("stages %v %v", now, err)
	}
	note, err := door.BankNote(ctx, session.RecipeNotice{Repo: "web", Kind: "issue", Line: "authcheck · chat · read it for auth holes · when touches auth", Why: "auth gets a second look"})
	if err != nil {
		t.Fatal(err)
	}
	if note != "pull request #41 opened · https://github.com/acme/web/pull/41" {
		t.Fatalf("note %q", note)
	}
	if len(road.previews) != 1 || len(road.prs) != 1 {
		t.Fatalf("previews %d prs %d", len(road.previews), len(road.prs))
	}
	e := road.previews[0].Edits[0]
	if e.Op != appwire.PipelineEditAddStep || e.Pipeline != "issue" || e.After == nil || *e.After != "review" || e.Name != "authcheck" {
		t.Fatalf("edit %+v", e)
	}
	pr := road.prs[0]
	if pr.Repo != "acme/web" || pr.Factory != appLocalID || pr.ExpectedBaseSHA != fakeRoadSHA || pr.Title != "Factory pipeline: add the authcheck step" || !strings.Contains(pr.Body, "auth gets a second look") {
		t.Fatalf("pull request %+v", pr)
	}
	f, err := pipeline.Parse(pr.Text)
	if err != nil {
		t.Fatal(err)
	}
	var after []string
	for _, st := range f.Recipe().For(factory.KindIssue) {
		after = append(after, st.Name)
	}
	if strings.Join(after, " ") != "plan approve write test review authcheck proof" {
		t.Fatalf("the pull request's pipeline: %v", after)
	}

	// A step of a name the pipeline has is changed where it stands.
	road.text = pr.Text
	if _, err := door.BankNote(ctx, session.RecipeNotice{Repo: "acme/web", Kind: "issue", Line: "authcheck · chat · read it twice for auth holes"}); err != nil {
		t.Fatal(err)
	}
	if e := road.previews[1].Edits[0]; e.Op != appwire.PipelineEditSetStep || e.Pipeline != "" || road.prs[1].Title != "Factory pipeline: change the authcheck step" {
		t.Fatalf("change edit %+v · %q", e, road.prs[1].Title)
	}

	// A policy sentence joins the rules the file already holds.
	if _, err := door.BankNote(ctx, session.RecipeNotice{Repo: "web", Policy: "never post without green tests"}); err != nil {
		t.Fatal(err)
	}
	if e := road.previews[2].Edits[0]; e.Op != appwire.PipelineEditSetList || e.List != appwire.PipelineListRules || strings.Join(e.Lines, "|") != "never post without green tests" {
		t.Fatalf("rule edit %+v", e)
	}

	// A line the file already has opens nothing.
	road.text = road.prs[2].Text
	if note, err := door.BankNote(ctx, session.RecipeNotice{Repo: "web", Policy: "Never post without green tests"}); err != nil || note != recipeRoadNoteHad || len(road.prs) != 3 {
		t.Fatalf("an unchanged line: %q %v, %d pull requests", note, err, len(road.prs))
	}

	// The editor's own rules answer as they would the person: a step the file
	// defines with other words is refused with its reason.
	if _, err := door.BankNote(ctx, session.RecipeNotice{Repo: "web", Kind: "issue", Line: "security · chat · look again"}); err == nil || !strings.Contains(err.Error(), "already a step with other words") {
		t.Fatalf("a redefined step: %v", err)
	}

	// A step only the editor can add, and a repository nobody watches, are refused.
	if _, err := door.BankNote(ctx, session.RecipeNotice{Repo: "web", Kind: "issue", Line: "lint · check · go vet ./..."}); err == nil || !strings.Contains(err.Error(), "pipeline editor") {
		t.Fatalf("a check step: %v", err)
	}
	if _, err := door.BankNote(ctx, session.RecipeNotice{Repo: "docs", Policy: "x"}); err == nil {
		t.Fatal("an unwatched repository was written")
	}
}
