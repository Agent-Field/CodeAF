package factory

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ── GO-AHEADS: A GROUP'S STANDING PERMISSION TO START ITS JOBS ─────────────
//
// A group policy is what a person has already said yes to for one group of
// jobs (a workstream, on the desk): a list of allowances, each naming the jobs
// it covers, the most it may spend and when it ends. THE FACTORY FOLLOWS IT BY
// ITSELF: a covered job that never ran starts when every job it waits on has
// merged, while its allowance is before its end and inside its cap, with no
// app attached. Stopping the policy, removing an allowance or passing its end
// starts nothing further; work already running is the person's to stop.
//
// Nothing here names a pipeline or a step. A go-ahead is a start a person made
// in advance, so the start it makes is a person's start in every other way.

// The bounds a policy is kept within. They are the desk's own (crates/core
// engine/factory_policy.rs), so a policy one side keeps is one the other sends.
const (
	PolicyGroupMost       = 200
	PolicyAllowancesMost  = 50
	PolicyAllowanceIDMost = 64
	PolicyJobsMost        = 200
	PolicyCapMostUSD      = 10000
	// WaitsOnMost is the most jobs one job may wait on.
	WaitsOnMost = 20
)

// The states an allowance is read in.
const (
	AllowanceActive = "active"
	AllowanceEnded  = "ended"
	AllowanceSpent  = "spent"
)

// Allowance is one go-ahead: the jobs it covers, by number, the most the jobs
// it starts may spend together, and when it ends. EndsAt is kept as the
// RFC 3339 text it was given, so a read-back compares equal to what was sent.
type Allowance struct {
	ID     string  `json:"id"`
	Jobs   []int   `json:"jobs"`
	CapUSD float64 `json:"cap_usd"`
	EndsAt string  `json:"ends_at"`
}

// Ends is the moment the allowance stops starting jobs.
func (a Allowance) Ends() time.Time {
	t, _ := time.Parse(time.RFC3339, a.EndsAt)
	return t
}

// Covers is whether job is one the allowance names.
func (a Allowance) Covers(job int) bool { return slices.Contains(a.Jobs, job) }

// GroupPolicy is one group's whole policy as the factory keeps it. Owner is the
// person whose yes it is: the jobs it starts become theirs when they have no
// person, and a job that already belongs to someone else is not started.
type GroupPolicy struct {
	Group      string      `json:"group"`
	Version    int         `json:"version"`
	Stopped    bool        `json:"stopped"`
	Owner      string      `json:"owner,omitempty"`
	UpdatedAt  time.Time   `json:"updated_at"`
	Allowances []Allowance `json:"allowances"`
}

// Policies are every group's policy, by group.
type Policies map[string]GroupPolicy

// CleanPolicyGroup is group trimmed, or an error when it is not 1 to
// [PolicyGroupMost] plain characters.
func CleanPolicyGroup(group string) (string, error) {
	group = strings.TrimSpace(group)
	if group == "" || utf8.RuneCountInString(group) > PolicyGroupMost || strings.IndexFunc(group, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("group needs 1–%d plain characters", PolicyGroupMost)
	}
	return group, nil
}

// CheckAllowances refuses a list of allowances the factory would not keep:
// more than [PolicyAllowancesMost], a repeated or malformed id, no jobs or too
// many, a job number below 1 or repeated, a cap that is not more than 0 and at
// most [PolicyCapMostUSD], or an end that is not RFC 3339.
func CheckAllowances(list []Allowance) error {
	if len(list) > PolicyAllowancesMost {
		return fmt.Errorf("a policy holds at most %d allowances", PolicyAllowancesMost)
	}
	ids := map[string]bool{}
	for _, a := range list {
		if !allowanceID(a.ID) || ids[a.ID] {
			return errors.New("each allowance needs its own id of 1–64 letters, digits and . _ : -")
		}
		ids[a.ID] = true
		if len(a.Jobs) == 0 || len(a.Jobs) > PolicyJobsMost {
			return fmt.Errorf("allowance %s must cover 1–%d jobs", a.ID, PolicyJobsMost)
		}
		seen := map[int]bool{}
		for _, job := range a.Jobs {
			if job < 1 || seen[job] {
				return fmt.Errorf("allowance %s lists a job that is not a distinct number from 1", a.ID)
			}
			seen[job] = true
		}
		if math.IsNaN(a.CapUSD) || math.IsInf(a.CapUSD, 0) || a.CapUSD <= 0 || a.CapUSD > PolicyCapMostUSD {
			return fmt.Errorf("allowance %s needs a cap more than $0 and at most $%d", a.ID, PolicyCapMostUSD)
		}
		if _, err := time.Parse(time.RFC3339, a.EndsAt); err != nil {
			return fmt.Errorf("allowance %s needs ends_at as an RFC 3339 time", a.ID)
		}
	}
	return nil
}

func allowanceID(id string) bool {
	if id == "" || len(id) > PolicyAllowanceIDMost {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("._:-", c) >= 0) {
			return false
		}
	}
	return true
}

// CheckWaitsOn refuses a waits-on list for job that the factory would not
// keep: more than [WaitsOnMost], a number below 1, a repeat, or the job itself.
func CheckWaitsOn(job int, waits []int) error {
	if len(waits) > WaitsOnMost {
		return fmt.Errorf("waits_on must contain 0–%d distinct job ids greater than zero", WaitsOnMost)
	}
	seen := map[int]bool{}
	for _, id := range waits {
		if id < 1 || seen[id] {
			return fmt.Errorf("waits_on must contain 0–%d distinct job ids greater than zero", WaitsOnMost)
		}
		if id == job {
			return errors.New("a job cannot wait on itself")
		}
		seen[id] = true
	}
	return nil
}

// Merged is whether the job's work reached its base: its pull request merged.
// It is the only ending a job waiting on it starts after.
func Merged(it Item) bool { return JobDoneHow(it) == "merged" }

// JobBlocked is why a job a go-ahead covers has not started yet: it waits on a
// job that has not merged (By "waits_on", Job naming it), or the go-ahead
// itself cannot start it (By "policy", Allowance naming it). It is written
// by the go-ahead pass and cleared when the job starts or nothing covers it.
type JobBlocked struct {
	By        string    `json:"by" jsonschema:"enum=waits_on,enum=policy"`
	Reason    string    `json:"reason"`
	Allowance string    `json:"allowance,omitempty"`
	Job       int       `json:"job,omitempty"`
	Since     time.Time `json:"since,omitzero"`
}

// The two kinds of block.
const (
	BlockedByWaitsOn = "waits_on"
	BlockedByPolicy  = "policy"
)

// Same is whether two blocks say the same thing, whenever each began.
func (b *JobBlocked) Same(o *JobBlocked) bool {
	if b == nil || o == nil {
		return b == o
	}
	return b.By == o.By && b.Reason == o.Reason && b.Allowance == o.Allowance && b.Job == o.Job
}

// AllowanceSpend is what the jobs one allowance started have spent, and what
// they are still allowed to: a job that has not finished holds its whole
// budget against the cap, so two starts in one pass cannot together pass it.
type AllowanceSpend struct {
	Spent     float64
	Committed float64
}

// PolicySpend sums, for every allowance of group, what the jobs it started
// spent ([JobLifetimeSpent]) and hold ([AllowanceSpend.Committed]).
func PolicySpend(items []Item, group string) map[string]AllowanceSpend {
	out := map[string]AllowanceSpend{}
	for _, it := range items {
		if it.PolicyRun == "" || it.PolicyGroup != group {
			continue
		}
		s := out[it.PolicyRun]
		spent := JobLifetimeSpent(it)
		s.Spent += spent
		if JobState(it) == "done" {
			s.Committed += spent
		} else {
			s.Committed += max(spent, it.Cap)
		}
		out[it.PolicyRun] = s
	}
	return out
}

// AllowanceState is how an allowance reads at now: ended once the policy is
// stopped or its end has come, spent once its jobs spent the whole cap, and
// active otherwise.
func AllowanceState(p GroupPolicy, a Allowance, spent float64, now time.Time) string {
	switch {
	case p.Stopped || !now.Before(a.Ends()):
		return AllowanceEnded
	case spent >= a.CapUSD:
		return AllowanceSpent
	}
	return AllowanceActive
}
