package main

// ── THE MANAGER'S "SAVE THIS TO THE REPO" TAKES THE EDITOR'S ROAD ───────────
//
// A line a chat proposes for a repository's pipeline (`factory_recipe`) and the
// person banks is the same change the pipeline editor makes: the file is read
// from the default branch through the factory (factory.pipeline_source), the
// edit is previewed as structured edits (factory.pipeline_preview), and the pull
// request is opened through the factory's own route (factory.pipeline_pr), AS
// THE SIGNED-IN PERSON. No checkout, no local git, no `gh`, so the change is the
// one the editor would show and lands where the editor's would.
//
// A NEW STEP GOES BEFORE PROOF, where the editor's add_step puts work: proof is
// the step that shows the change, so a step after it would run on work nobody
// proves. A step of the same name is changed where it stands.
//
// The road is the app's: this process holds the factory connection only while
// the app runs ([appPipelineRoad]). A chat with no app behind it (the terminal)
// banks through its checkout as before ([fileRecipeDoor]).

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// pipelineRoad is the editor's three calls, as the app's backend answers them.
type pipelineRoad interface {
	FactoryPipelineSource(ctx context.Context, p appwire.FactoryPipelineSourceParams) (appwire.FactoryPipelineSource, error)
	FactoryPipelinePreview(ctx context.Context, p appwire.FactoryPipelinePreviewParams) (appwire.FactoryPipelinePreview, error)
	FactoryPipelinePR(ctx context.Context, p appwire.FactoryPipelinePRParams) (appwire.FactoryPipelinePRDone, error)
}

// appPipelineRoad is the running app's backend, nil outside the app.
var appPipelineRoad atomic.Pointer[appBackend]

// appRoad is the app's road, or nil.
func appRoad() pipelineRoad {
	if b := appPipelineRoad.Load(); b != nil {
		return b
	}
	return nil
}

// The pull request's words, and the note a yes returns.
const (
	recipeRoadStepTitle = "Factory pipeline: %s the %s step"
	recipeRoadRuleTitle = "Factory pipeline: add a rule"
	recipeRoadBody      = "Proposed in a factory chat and approved there.\n\nChanges `.codeaf/factory.yaml`:\n\n    %s"
	recipeRoadNoteHad   = "already in the repository's pipeline · nothing to change"
	recipeRoadNoteSame  = "the default branch already has this · no pull request needed"
	recipeRoadNotePR    = "pull request #%d opened · %s"
)

// roadRecipeDoor banks through the editor's road when the app holds the
// factory, and through the checkout otherwise.
type roadRecipeDoor struct {
	fileRecipeDoor
	// factory is the connection the floor is (the app's local factory).
	factory string
	// repos is every repository the floor watches, as owner/name.
	repos func() ([]string, error)
	road  func() pipelineRoad
}

// full is repo as owner/name: itself, or the one watched repository whose
// name it is. Two of the same name, or none, is a refusal, never a guess.
func (d roadRecipeDoor) full(repo string) (string, error) {
	repo = strings.TrimSpace(repo)
	all, err := d.repos()
	if err != nil {
		return "", err
	}
	var found []string
	for _, r := range all {
		short := r[strings.LastIndex(r, "/")+1:]
		if strings.EqualFold(r, repo) {
			return r, nil
		}
		if strings.EqualFold(short, repo) {
			found = append(found, r)
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return "", errors.New("the factory does not watch " + repo)
	}
	return "", errors.New(repo + " names more than one watched repository; say owner/name")
}

// source reads repo's file through the factory, editable or refused.
func (d roadRecipeDoor) source(ctx context.Context, road pipelineRoad, repo string) (appwire.FactoryPipelineSource, error) {
	p, err := appwire.CheckPipelineSource(appwire.FactoryPipelineSourceParams{Factory: d.factory, Repo: repo})
	if err != nil {
		return appwire.FactoryPipelineSource{}, err
	}
	src, err := road.FactoryPipelineSource(ctx, p)
	if err != nil {
		return src, err
	}
	if !src.Editable {
		why := "its pipeline file is " + src.State
		if len(src.Problems) > 0 {
			why += ": " + src.Problems[0]
		}
		return src, errors.New(repo + " cannot be changed from a chat: " + why + ". Fix it in the pipeline editor first")
	}
	return src, nil
}

// Stages is the stage names kind runs in repo's file on its default branch, as
// the editor reads them.
func (d roadRecipeDoor) Stages(ctx context.Context, repo string, kind factory.Kind) ([]string, error) {
	road := d.road()
	if road == nil {
		return d.fileRecipeDoor.Stages(ctx, repo, kind)
	}
	full, err := d.full(repo)
	if err != nil {
		return nil, err
	}
	src, err := d.source(ctx, road, full)
	if err != nil {
		return nil, err
	}
	return stepsOfKind(src, string(kind)), nil
}

// BankNote previews the line as the editor's edit and opens its pull request.
func (d roadRecipeDoor) BankNote(ctx context.Context, n session.RecipeNotice) (string, error) {
	road := d.road()
	if road == nil {
		return d.fileRecipeDoor.BankNote(ctx, n)
	}
	full, err := d.full(n.Repo)
	if err != nil {
		return "", err
	}
	src, err := d.source(ctx, road, full)
	if err != nil {
		return "", err
	}
	edits, title, line, err := recipeRoadEdits(src, n)
	if err != nil {
		return "", err
	}
	pp, err := appwire.CheckPipelinePreview(appwire.FactoryPipelinePreviewParams{Factory: d.factory, Repo: full, BaseSHA: src.BaseSHA, BaseText: src.Text, Edits: edits})
	if err != nil {
		return "", err
	}
	pv, err := road.FactoryPipelinePreview(ctx, pp)
	if err != nil {
		return "", err
	}
	if !pv.Changed {
		return recipeRoadNoteHad, nil
	}
	body := fmt.Sprintf(recipeRoadBody, line)
	if why := strings.TrimSpace(n.Why); why != "" {
		body += "\n\nWhy: " + why
	}
	pr, err := appwire.CheckPipelinePR(appwire.FactoryPipelinePRParams{Factory: d.factory, Repo: full, Text: pv.Text, Title: title, Body: body, ExpectedBaseSHA: src.BaseSHA})
	if err != nil {
		return "", err
	}
	done, err := road.FactoryPipelinePR(ctx, pr)
	if err != nil {
		return "", err
	}
	if done.PR == nil {
		return recipeRoadNoteSame, nil
	}
	return fmt.Sprintf(recipeRoadNotePR, done.PR.Number, done.PR.URL), nil
}

// recipeRoadEdits is the notice as the editor's edit: a policy or habit joins
// the rules list; a stage line sets the step of its name where the pipeline
// runs it, or is added before proof. The editor's own rules then apply as they
// would to the person's edit (a step the file defines with other words is
// refused, with the editor's reason). It answers the pull request's title and
// the line its body quotes.
func recipeRoadEdits(src appwire.FactoryPipelineSource, n session.RecipeNotice) ([]appwire.FactoryPipelineEdit, string, string, error) {
	if sentence := strings.TrimSpace(n.Policy + n.Habit); sentence != "" {
		lines := []string{}
		if src.Rules != nil {
			lines = append(lines, *src.Rules...)
		}
		had := false
		for _, l := range lines {
			had = had || strings.EqualFold(strings.TrimSpace(l), sentence)
		}
		if !had {
			lines = append(lines, sentence)
		}
		return []appwire.FactoryPipelineEdit{{Op: appwire.PipelineEditSetList, List: appwire.PipelineListRules, Lines: lines}}, recipeRoadRuleTitle, "- " + sentence, nil
	}
	kind := factory.Kind(n.Kind)
	parsed, problems := factory.Parse("## " + string(kind) + "\n1. " + n.Line)
	if len(problems) > 0 {
		return nil, "", "", errors.New(problems[0].Why)
	}
	stages := parsed.For(kind)
	if len(stages) != 1 {
		return nil, "", "", errors.New("a stage is one line: N. name · kind · ask · knobs")
	}
	st := stages[0]
	if st.Kind != "" && st.Kind != factory.StageChat {
		return nil, "", "", fmt.Errorf("only a chat step can be saved from a chat; add a %s step in the pipeline editor", st.Kind)
	}
	words := appwire.FactoryPipelineEdit{Op: appwire.PipelineEditSetStep, Name: st.Name, Prompt: &st.Ask}
	if st.Until != "" {
		words.Until = &st.Until
	}
	if st.Max > 0 {
		words.Max = &st.Max
	}
	for _, w := range []struct {
		from string
		to   **string
	}{{st.When, &words.When}, {st.Effort, &words.Effort}, {st.Fanout, &words.Fanout}} {
		if w.from != "" {
			v := w.from
			*w.to = &v
		}
	}
	steps := stepsOfKind(src, string(kind))
	if slicesIndexFold(steps, st.Name) >= 0 {
		return []appwire.FactoryPipelineEdit{words}, fmt.Sprintf(recipeRoadStepTitle, "change", st.Name), n.Line, nil
	}
	add := words
	after := beforeProof(steps)
	add.Op, add.Pipeline, add.After = appwire.PipelineEditAddStep, string(kind), &after
	return []appwire.FactoryPipelineEdit{add}, fmt.Sprintf(recipeRoadStepTitle, "add", st.Name), n.Line, nil
}

// stepsOfKind is the step names kind's pipeline lists in src.
func stepsOfKind(src appwire.FactoryPipelineSource, kind string) []string {
	for _, p := range src.Pipelines {
		if p.Kind == kind {
			return append([]string{}, p.Steps...)
		}
	}
	return nil
}

// beforeProof is the step a new one goes after: the one before proof, "" when
// proof is first, or the last step when the pipeline has no proof.
func beforeProof(steps []string) string {
	if i := slicesIndexFold(steps, "proof"); i >= 0 {
		if i == 0 {
			return ""
		}
		return steps[i-1]
	}
	if len(steps) == 0 {
		return ""
	}
	return steps[len(steps)-1]
}

// slicesIndexFold is the index of name in steps, ignoring case, or -1.
func slicesIndexFold(steps []string, name string) int {
	for i, s := range steps {
		if strings.EqualFold(strings.TrimSpace(s), strings.TrimSpace(name)) {
			return i
		}
	}
	return -1
}
