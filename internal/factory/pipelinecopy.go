package factory

import (
	"strings"
	"time"
)

// ── THE FACTORY'S OWN COPY OF A REPOSITORY'S STEPS ──────────────────────────
//
// A repository's pipeline is its `.codeaf/factory.yaml`, read from the main
// branch ([RecipeSource]). A person who edits it in the app gets a pull
// request, and until that merges nothing they changed runs. PipelineCopy is the
// other way out, decided by the owner on 2026-10-28: the SAME FILE the pull
// request would write, saved in THE FACTORY (its store, beside its own rules,
// [HouseRules]) for one repository, so the jobs this factory starts afterwards
// run it at once, with no push and no pull request.
//
// IT STANDS IN FOR MAIN'S FILE ON THIS FACTORY ONLY, and only until main's
// file says the same thing (the pull request merged), when it clears itself;
// a person can also stop using it, and jobs started afterwards run main's
// file again. The factory's own rules and protected paths still apply on top
// of it, exactly as they do on top of main's file. A job's file is decided
// once, at its first launch ([Item.DecidePipeline]), and kept for its life:
// saving or stopping a copy changes only jobs not yet launched.
//
// It travels WITH THE JOB ([Item.PipelineCopy]) as the factory's rules do, so a
// runner on another machine runs the same steps and follows the same rules,
// never whatever its own machine reads of main.
type PipelineCopy struct {
	// Repo is the repository as the person who saved it named it, owner/name.
	Repo string `json:"repo"`
	// Text is the whole file, exactly as the factory writes it (the bytes a
	// pull request of the same steps would commit). A reader's job list
	// leaves it out (api.FromItem): only a runner pulling the job is sent it.
	Text string `json:"text,omitempty"`
	// By is who saved it (their handle), At when.
	By string    `json:"by"`
	At time.Time `json:"at"`
}

// DecidePipeline decides, ONCE, the pipeline file the job runs for its life:
// c, the factory's copy the deciding read used, or main's file when c is nil.
// The first door that stamps the job's steps or rules decides it (its first
// launch, or intake's stamp if that comes first), and every door that bounds
// or starts the job afterwards reads that file (pipeline.JobFile): a copy
// saved or stopped since changes only jobs not yet decided. A decided job is
// left as it is. The job's record says when it runs a copy.
func (it *Item) DecidePipeline(c *PipelineCopy, now time.Time) {
	if it.PipelineIsDecided() {
		return
	}
	it.PipelineDecided = true
	it.PipelineCopy = c.Clone()
	if c != nil {
		it.Note(now, "Runs this factory's copy of "+PipelineFile+", "+c.Saved()+", not main's")
	}
}

// PipelineIsDecided says whether the job's pipeline file is decided
// ([Item.DecidePipeline]). A job that carries a copy is, whatever wrote it.
func (it Item) PipelineIsDecided() bool {
	return it.PipelineDecided || it.PipelineCopy != nil
}

// ForgetPipeline undecides the job's pipeline file, so its next launch
// decides afresh: Bring back, which sets its steps to the recipe again.
func (it *Item) ForgetPipeline() {
	it.PipelineDecided, it.PipelineCopy = false, nil
}

// PipelineCopyKey is the one spelling a repository's copy is kept under:
// GitHub names repositories without regard to case.
func PipelineCopyKey(repo string) string {
	return strings.ToLower(strings.TrimSpace(repo))
}

// Clone is c with nothing shared; nil stays nil.
func (c *PipelineCopy) Clone() *PipelineCopy {
	if c == nil {
		return nil
	}
	out := *c
	return &out
}

// Same says whether two copies are the same save: the same file, by the same
// person, at the same moment.
func (c *PipelineCopy) Same(o *PipelineCopy) bool {
	if c == nil || o == nil {
		return c == nil && o == nil
	}
	return c.Text == o.Text && c.By == o.By && c.At.Equal(o.At)
}

// Saved is the copy's provenance in words, as a job's record says it:
// `saved 2026-10-28 14:02 UTC by @test-owner`. A copy saved by no one known
// says only when.
func (c PipelineCopy) Saved() string {
	out := "saved " + c.At.UTC().Format("2006-01-02 15:04") + " UTC"
	if by := strings.TrimSpace(c.By); by != "" {
		if !strings.HasPrefix(by, "@") {
			by = "@" + by
		}
		out += " by " + by
	}
	return out
}
