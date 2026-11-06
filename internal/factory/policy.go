package factory

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Policy bounds are shared by the service and engine request checks.
const (
	GroupMost            = 200
	PolicyAllowancesMost = 50
	AllowanceIDMost      = 64
	AllowanceJobsMost    = 200
	AllowanceCapMost     = BudgetMostUSD
)

// GroupPolicy is the saved choice for one opaque group of jobs.
type GroupPolicy struct {
	Group      string      `json:"group"`
	Version    int         `json:"version"`
	Stopped    bool        `json:"stopped"`
	Owner      string      `json:"owner,omitempty"`
	UpdatedAt  *time.Time  `json:"updated_at,omitempty"`
	Allowances []Allowance `json:"allowances"`
}

// Allowance names the jobs whose total spending shares a cap and end.
type Allowance struct {
	ID     string  `json:"id"`
	Jobs   []int   `json:"jobs"`
	CapUSD float64 `json:"cap_usd"`
	EndsAt string  `json:"ends_at"`
}

var allowanceID = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

// CheckGroup trims a group without interpreting its contents. Empty is allowed
// on a job; a policy requires a group as well.
func CheckGroup(group string) (string, error) {
	for _, c := range group {
		if unicode.IsControl(c) {
			return "", fmt.Errorf("group must have no control characters")
		}
	}
	group = strings.TrimSpace(group)
	if !utf8.ValidString(group) || utf8.RuneCountInString(group) > GroupMost {
		return "", fmt.Errorf("group must be at most %d characters", GroupMost)
	}
	return group, nil
}

// Check refuses an allowance the factory cannot keep.
func (a Allowance) Check() error {
	if len(a.ID) == 0 || len(a.ID) > AllowanceIDMost || !allowanceID.MatchString(a.ID) {
		return fmt.Errorf("allowance id must be 1–%d characters from A–Z, a–z, 0–9, dot, underscore, colon and hyphen", AllowanceIDMost)
	}
	if len(a.Jobs) == 0 || len(a.Jobs) > AllowanceJobsMost {
		return fmt.Errorf("an allowance must list 1–%d jobs", AllowanceJobsMost)
	}
	seen := make(map[int]bool, len(a.Jobs))
	for _, id := range a.Jobs {
		if id <= 0 || seen[id] {
			return fmt.Errorf("allowance jobs must be distinct positive job ids")
		}
		seen[id] = true
	}
	if math.IsNaN(a.CapUSD) || math.IsInf(a.CapUSD, 0) || a.CapUSD <= 0 || a.CapUSD > AllowanceCapMost {
		return fmt.Errorf("allowance cap_usd must be greater than zero and at most $%.0f", AllowanceCapMost)
	}
	if _, err := time.Parse(time.RFC3339, a.EndsAt); err != nil {
		return fmt.Errorf("allowance ends_at must be an RFC 3339 date")
	}
	return nil
}

// Check validates a complete replacement without consulting jobs or time.
func (p GroupPolicy) Check() error {
	group, err := CheckGroup(p.Group)
	if err != nil {
		return err
	}
	if group == "" || group != p.Group {
		return fmt.Errorf("a policy requires a non-empty, trimmed group")
	}
	if p.Version < 0 {
		return fmt.Errorf("policy version must not be negative")
	}
	if len(p.Allowances) > PolicyAllowancesMost {
		return fmt.Errorf("a policy can have at most %d allowances", PolicyAllowancesMost)
	}
	seen := make(map[string]bool, len(p.Allowances))
	for _, a := range p.Allowances {
		if err := a.Check(); err != nil {
			return err
		}
		if seen[a.ID] {
			return fmt.Errorf("allowance ids must be distinct")
		}
		seen[a.ID] = true
	}
	return nil
}

func allowanceSpend(a Allowance, spentOf func(int) float64) float64 {
	var spent float64
	for _, id := range a.Jobs {
		spent += spentOf(id)
	}
	return spent
}

func allowanceEnd(a Allowance, now time.Time) string {
	end, err := time.Parse(time.RFC3339, a.EndsAt)
	if err != nil || !now.Before(end) {
		return fmt.Sprintf("allowance %s ended %s", a.ID, end.Format("Jan 2"))
	}
	return ""
}

// StartDecision chooses the first allowance with room for the whole job budget.
// The lookup includes every run, whether a person or a policy began it.
func StartDecision(p GroupPolicy, job Item, spentOf func(int) float64, now time.Time) (allowanceID, reason string, ok bool) {
	if p.Stopped {
		return "", "its group is stopped", false
	}
	reason = "no allowance covers it"
	found := false
	for _, a := range p.Allowances {
		if !slices.Contains(a.Jobs, job.ID) {
			continue
		}
		why := allowanceEnd(a, now)
		if why == "" {
			spent := allowanceSpend(a, spentOf)
			if job.Cap > 0 && PolicyMoneyCompare(spent+job.Cap, a.CapUSD) <= 0 {
				return a.ID, "", true
			}
			left := a.CapUSD - spent
			why = fmt.Sprintf("allowance %s has $%.2f of $%.2f left; the job's budget is $%.2f", a.ID, max(0, left), a.CapUSD, job.Cap)
		}
		if !found {
			reason, found = why, true
		}
	}
	return "", reason, false
}

// ContinueDecision keeps a run on its original allowance, even if another
// allowance now covers the same job.
func ContinueDecision(p GroupPolicy, job Item, spentOf func(int) float64, now time.Time) (reason string, ok bool) {
	if p.Stopped {
		return "its group is stopped", false
	}
	for _, a := range p.Allowances {
		if a.ID != job.PolicyRun || !slices.Contains(a.Jobs, job.ID) {
			continue
		}
		if why := allowanceEnd(a, now); why != "" {
			return why, false
		}
		if PolicyMoneyCompare(allowanceSpend(a, spentOf), a.CapUSD) >= 0 {
			return fmt.Sprintf("allowance %s reached its $%.2f cap", a.ID, a.CapUSD), false
		}
		return "", true
	}
	return "no allowance covers it", false
}

// PolicySpend counts the group and each allowance independently. Listed jobs
// need not belong to the group, and absent jobs contribute nothing.
func PolicySpend(p GroupPolicy, jobs []Item) (group float64, perAllowance map[string]float64) {
	spent := make(map[int]float64, len(jobs))
	for _, job := range jobs {
		spent[job.ID] = JobLifetimeSpent(job)
		if job.Group == p.Group {
			group += spent[job.ID]
		}
	}
	perAllowance = make(map[string]float64, len(p.Allowances))
	for _, a := range p.Allowances {
		perAllowance[a.ID] = allowanceSpend(a, func(id int) float64 { return spent[id] })
	}
	return group, perAllowance
}

// ClearPolicyRun belongs to a person's execution doors.
func ClearPolicyRun(it *Item) {
	it.PolicyRun = ""
	if it.Blocked != nil && it.Blocked.By == BlockPolicy {
		it.Blocked = nil
	}
}
