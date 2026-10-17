package github

import (
	"context"
	"reflect"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// followExtra is the read a client has beyond the open-list API: reviews and
// how many the base branch requires. A client without it still reports state
// and checks.
type followExtra interface {
	Reviews(ctx context.Context, owner, repo string, number int) ([]forge.Review, error)
	RequiredReviews(ctx context.Context, owner, repo, branch string) (int, error)
}

// FollowPR reads one pull request the factory opened, including after GitHub
// has merged or closed it. The open list never carries those.
func (s *Source) FollowPR(ctx context.Context, owner, repo string, number int) (factory.PullRequest, error) {
	p, err := s.api.Pull(ctx, owner, repo, number)
	if err != nil {
		return factory.PullRequest{}, err
	}
	out := factory.PullRequest{
		Number: p.Number, URL: p.URL, Draft: p.Draft,
		Conflict: p.Mergeable != nil && !*p.Mergeable,
		State:    pullFollowState(p), MergedBy: p.MergedBy, MergedAt: p.MergedAt,
		HeadSHA: p.HeadSHA, Mergeable: mergeableWord(p.Mergeable),
	}
	if p.HeadSHA != "" {
		state, runs, err := s.api.CheckRuns(ctx, owner, repo, p.HeadSHA)
		if err != nil {
			return factory.PullRequest{}, err
		}
		out.CI = ChecksWords(state)
		out.Checks = summarizeChecks(runs)
		out.ChecksRead = true
	}
	if extra, ok := s.api.(followExtra); ok {
		reviews, err := extra.Reviews(ctx, owner, repo, number)
		if err != nil {
			return factory.PullRequest{}, err
		}
		out.Reviews = factoryReviews(reviews)
		out.Required, err = extra.RequiredReviews(ctx, owner, repo, p.Base)
		if err != nil {
			return factory.PullRequest{}, err
		}
		out.ReviewDecision = reviewDecision(out.Reviews, out.Required)
		out.ReviewsRead = true
	}
	return out, nil
}

// mergeableWord is GitHub's three-valued mergeable as the merge gate reads
// it: nil is "not worked out yet", which the gate refuses on.
func mergeableWord(m *bool) string {
	switch {
	case m == nil:
		return ""
	case *m:
		return factory.MergeableYes
	default:
		return factory.MergeableNo
	}
}

func pullFollowState(p forge.Pull) string {
	if p.Merged || !p.MergedAt.IsZero() {
		return factory.PRMerged
	}
	switch strings.ToLower(strings.TrimSpace(p.State)) {
	case factory.PRClosed:
		return factory.PRClosed
	case factory.PRMerged:
		return factory.PRMerged
	default:
		return factory.PROpen
	}
}

func summarizeChecks(runs []forge.CheckRun) factory.PRChecks {
	var c factory.PRChecks
	for _, r := range runs {
		if r.Status != "completed" {
			c.Pending++
			continue
		}
		switch r.Conclusion {
		case "success":
			c.Passed++
		case "failure", "timed_out", "cancelled", "action_required", "startup_failure":
			c.Failed++
		}
	}
	return c
}

func factoryReviews(in []forge.Review) []factory.PRReview {
	if len(in) == 0 {
		return nil
	}
	out := make([]factory.PRReview, 0, len(in))
	for _, r := range in {
		out = append(out, factory.PRReview{By: r.User, State: strings.ToLower(r.State), At: r.At})
	}
	return out
}

// reviewDecision is the latest review from each person. A request for
// changes wins. Enough approvals, or one when the branch requires none, is
// approved. A branch that still wants more is review_required.
func reviewDecision(reviews []factory.PRReview, required int) string {
	latest := map[string]factory.PRReview{}
	for _, r := range reviews {
		by := strings.ToLower(strings.TrimSpace(r.By))
		if by == "" {
			continue
		}
		prev, ok := latest[by]
		if !ok || !r.At.Before(prev.At) {
			latest[by] = r
		}
	}
	approvals, changes := 0, false
	for _, r := range latest {
		switch strings.ToLower(r.State) {
		case "approved":
			approvals++
		case "changes_requested":
			changes = true
		}
	}
	if changes {
		return "changes_requested"
	}
	need := required
	if need <= 0 {
		need = 1
	}
	if approvals >= need {
		return "approved"
	}
	if required > 0 {
		return "review_required"
	}
	return ""
}

// followOpened re-reads every pull request the factory opened and records
// pr_merged or pr_closed the first time that state is seen. THE JOB STAYS
// done. pr.state carries open, merged and closed.
func followOpened(ctx context.Context, src factory.Source, st *store.Store) error {
	f, ok := src.(*Source)
	if !ok || st == nil {
		return nil
	}
	items, err := st.List()
	if err != nil || len(items) == 0 {
		return err
	}
	var rec *record.Record
	if opened, err := record.Open(st, record.Options{}); err == nil {
		rec = opened
	}
	for _, it := range items {
		if it.PullRequest == nil || it.PullRequest.Number <= 0 {
			continue
		}
		owner, name, ok := pullRepo(it)
		if !ok {
			continue
		}
		fresh, err := f.FollowPR(ctx, owner, name, it.PullRequest.Number)
		if err != nil {
			continue
		}
		if fresh.Number <= 0 {
			fresh.Number = it.PullRequest.Number
		}
		if fresh.URL == "" {
			fresh.URL = it.PullRequest.URL
		}
		if reflect.DeepEqual(*it.PullRequest, fresh) {
			continue
		}
		prev := it.PullRequest.State
		next := fresh
		applied := false
		if err := st.Annotate(it.ID, func(item *factory.Item) error {
			if item.PullRequest == nil {
				return nil
			}
			cp := next
			item.PullRequest = &cp
			item.DismissedBy = nil
			if (cp.State == factory.PRMerged || cp.State == factory.PRClosed) && item.DoneAt.IsZero() {
				item.DoneAt = time.Now().UTC()
			}
			applied = true
			return nil
		}); err != nil || !applied || rec == nil {
			continue
		}
		kind := ""
		switch next.State {
		case factory.PRMerged:
			if prev != factory.PRMerged {
				kind = record.PRMerged
			}
		case factory.PRClosed:
			if prev != factory.PRClosed {
				kind = record.PRClosed
			}
		}
		if kind == "" {
			continue
		}
		handle := ""
		if kind == record.PRMerged {
			handle = next.MergedBy
		}
		_, _ = rec.Append(ctx, record.Event{Kind: kind, Job: it.ID, Handle: handle, Detail: "pr · " + next.URL})
	}
	return nil
}

// pullRepo is the owner and name of a job that carries a published pull
// request. GitHub items keep them apart. A repo stored as owner/name is
// split. The pull request's own page is the last place that names them.
func pullRepo(it factory.Item) (owner, name string, ok bool) {
	repo := strings.TrimSpace(it.Repo)
	product := strings.TrimSpace(it.Product)
	if product != "" && repo != "" && !strings.Contains(repo, "/") {
		return product, repo, true
	}
	if o, n, cut := strings.Cut(repo, "/"); cut && o != "" && n != "" && !strings.Contains(n, "/") {
		return o, n, true
	}
	const mark = "github.com/"
	page := it.URL
	if it.PullRequest != nil && it.PullRequest.URL != "" {
		page = it.PullRequest.URL
	}
	if i := strings.Index(strings.ToLower(page), mark); i >= 0 {
		rest := page[i+len(mark):]
		parts := strings.Split(rest, "/")
		if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
			return parts[0], parts[1], true
		}
	}
	return "", "", false
}
