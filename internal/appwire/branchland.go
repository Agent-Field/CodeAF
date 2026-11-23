package appwire

import "strings"

// LANDING A TASK BRANCH: the person's own merge of one of the conversation's
// task branches into their checkout, in two calls.
//
// branches.land.plan reads and binds everything the merge depends on and
// answers a plan with a single-use id and its revision. branches.land merges
// it, and is the NATIVE HOST'S ONLY, sent after the host's own confirmation
// dialog has shown what the plan says - never by a renderer, a model or a
// test. The branch is a key into the conversation's own task records; no
// path, repository, target or ref is ever taken from the caller.
//
// A refusal is CodeConflict with a [BranchLandRefusal] detail. A land whose
// outcome cannot be proved is CodeUncertain with [BranchLandFacts]: it is
// never retried, and the person looks at the checkout first.

const (
	// BranchLandPlanTTL is how long a plan may be confirmed, in seconds.
	BranchLandPlanTTL = 120
	// BranchLandPlansMax and BranchLandPlansPerAgent bound the plans held.
	BranchLandPlansMax      = 64
	BranchLandPlansPerAgent = 8
)

// BranchLandPlanQuery is branches.land.plan's request.
type BranchLandPlanQuery struct {
	Agent  string `json:"agent"`
	Branch string `json:"branch"`
}

// BranchLandHook is one hook git will run during the merge, bound by its
// bytes. The confirmation names every one.
type BranchLandHook struct {
	Name       string `json:"name"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
	Executable bool   `json:"executable"`
}

// BranchLandPlan is what the person confirms. Repository is for display; the
// commits are full ids.
type BranchLandPlan struct {
	Plan         string           `json:"plan"`
	Revision     string           `json:"revision"`
	ExpiresAt    string           `json:"expires_at"`
	Task         string           `json:"task"`
	Title        string           `json:"title,omitempty"`
	Branch       string           `json:"branch"`
	BranchHead   string           `json:"branch_head"`
	Base         string           `json:"base"`
	Target       string           `json:"target"`
	Head         string           `json:"head"`
	Repository   string           `json:"repository"`
	FastForward  bool             `json:"fast_forward"`
	Files        *int             `json:"files,omitempty"`
	Added        *int             `json:"added,omitempty"`
	Removed      *int             `json:"removed,omitempty"`
	CountsCapped bool             `json:"counts_capped,omitempty"`
	Hooks        []BranchLandHook `json:"hooks"`
}

// BranchLandQuery is branches.land's request.
type BranchLandQuery struct {
	Agent    string `json:"agent"`
	Plan     string `json:"plan"`
	Revision string `json:"revision"`
}

// BranchLanded is a land that happened and was proved.
type BranchLanded struct {
	Branch      string `json:"branch"`
	Target      string `json:"target"`
	HeadBefore  string `json:"head_before"`
	HeadAfter   string `json:"head_after"`
	FastForward bool   `json:"fast_forward"`
}

// BranchLandRefusal is the detail of a refused plan or land.
type BranchLandRefusal struct {
	Reason string   `json:"reason" enum:"changed,merged,gone,detached,operation,dirty,ignored_in_way,conflict,held,task_copy_dirty,custom_merge,filter,sparse,submodule,signing,ff_only,merge_options,hook_unbounded,unrelated,outside,git_old,too_large,refused,expired"`
	Paths  []string `json:"paths,omitempty"`
}

// BranchLandFacts is what an uncertain land found afterwards. An absent fact
// could not be read.
type BranchLandFacts struct {
	Head            string   `json:"head,omitempty"`
	Target          string   `json:"target,omitempty"`
	MergeInProgress *bool    `json:"merge_in_progress,omitempty"`
	IndexLocked     *bool    `json:"index_locked,omitempty"`
	Clean           *bool    `json:"clean,omitempty"`
	Conflicted      []string `json:"conflicted,omitempty"`
}

// BranchLander is the optional engine half of branches.land.*.
type BranchLander interface {
	PlanBranchLand(BranchLandPlanQuery) (BranchLandPlan, error)
	// LandBranch is for the native host only, after its own confirmation.
	LandBranch(BranchLandQuery) (BranchLanded, error)
}

func branchLander(e Engine, method string) (BranchLander, error) {
	l, ok := e.(BranchLander)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", method)
	}
	return l, nil
}

func init() {
	methods["branches.land.plan"] = typed(func(e Engine, p BranchLandPlanQuery) (*BranchLandPlan, error) {
		l, err := branchLander(e, "branches.land.plan")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.Branch) == "" {
			return nil, Errorf(CodeBadParams, "branches.land.plan needs an agent and a branch")
		}
		got, err := l.PlanBranchLand(p)
		if got.Hooks == nil {
			got.Hooks = []BranchLandHook{}
		}
		return &got, err
	})
	methods["branches.land"] = typed(func(e Engine, p BranchLandQuery) (*BranchLanded, error) {
		l, err := branchLander(e, "branches.land")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.Plan) == "" || strings.TrimSpace(p.Revision) == "" {
			return nil, Errorf(CodeBadParams, "branches.land needs an agent, a plan and a revision")
		}
		got, err := l.LandBranch(p)
		return &got, err
	})
}
