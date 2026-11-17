package factory

import (
	"fmt"
	"strings"
	"time"
)

// ── THE PERSON TELLS AUTO TO CARRY ON PAST A FAILING CHECK ─────────────────
//
// A landed job in Auto stops when a proof claim or a repository policy check
// fails ([AutonomyBlockReason]), and until now only Request changes, Take over,
// Drop or the check passing moved it. The person keeps full control (the
// owner's decision of 2026-11-16): when they ask the job, in its chat, to
// resume, each failing finding is recorded as overridden and Auto carries on to
// publication despite it.
//
// THE OVERRIDE IS PER FINDING. It names the claim's own text, so a finding that
// fails later and was never overridden pauses Auto again.
//
// THE OVERRIDE IS PER RUN. Every path that starts a new run clears the job's
// checks through [Item.ResetChecks], which drops the overrides with them, so a
// later run whose same claim fails again pauses Auto again. It is always a
// person's: the factory records it only from a signed-in caller through the
// job's resume_auto door, and the job's manager presses that door only with
// the person's own words from the current turn.

// Override kinds, one per list a landed job's checks are kept in.
const (
	OverrideProof  = "proof"
	OverridePolicy = "policy"
)

// CheckOverride is one failing finding the person told Auto to carry on past.
type CheckOverride struct {
	// Kind is OverrideProof or OverridePolicy.
	Kind string
	// Text is the finding as the claim names it.
	Text string
	// By is the person who said so, as their forge handle.
	By string
	// At is when it was recorded.
	At time.Time
	// Words are what the person said, when the door that recorded it knew.
	Words string `json:",omitempty"`
}

// ResetChecks clears the job's proof and policy findings for a new run, and
// with them every override the person gave the run before: an override covers
// only the run it was given for.
func (it *Item) ResetChecks() {
	it.Proof, it.Policy, it.Overrides = nil, nil, nil
}

// ClaimOverridden says the person told Auto to carry on past this failing
// claim. A claim that passes needs no override and reads false.
func ClaimOverridden(it Item, kind string, cl Claim) bool {
	if cl.OK {
		return false
	}
	for _, o := range it.Overrides {
		if o.Kind == kind && o.Text == cl.Text {
			return true
		}
	}
	return false
}

// FailingFindings are the landed job's failing proof and policy claims that no
// override covers yet, as the overrides that would cover them (By and At unset).
func FailingFindings(it Item) []CheckOverride {
	var out []CheckOverride
	for _, list := range []struct {
		kind   string
		claims []Claim
	}{{OverrideProof, it.Proof}, {OverridePolicy, it.Policy}} {
		for _, cl := range list.claims {
			if !cl.OK && !ClaimOverridden(it, list.kind, cl) {
				out = append(out, CheckOverride{Kind: list.kind, Text: cl.Text})
			}
		}
	}
	return out
}

// OffersAutoResume says Auto is paused on a failing proof or policy check the
// person may tell it to carry on past: the job is in Auto, landed, and the
// pause it shows is a failing finding.
func OffersAutoResume(it Item) bool {
	if !Automatic(it) || it.State != StateLanded || len(FailingFindings(it)) == 0 {
		return false
	}
	why := AutonomyBlockReason(it)
	return strings.HasPrefix(why, proofAttentionLead) || strings.HasPrefix(why, policyAttentionLead)
}

// OverrideFindings records every failing finding the pause shows as the
// person's override, and notes it on the job's history, so Auto carries on
// past them. It answers what it overrode, and refuses when Auto is not paused
// on a failing finding.
func OverrideFindings(it *Item, who, words string, now time.Time) ([]CheckOverride, error) {
	if !OffersAutoResume(*it) {
		return nil, fmt.Errorf("Auto is not paused on a failing check")
	}
	who = NormalizeHandle(who)
	if who == "" {
		return nil, fmt.Errorf("only a signed-in person can tell Auto to carry on")
	}
	words = strings.Join(strings.Fields(words), " ")
	found := FailingFindings(*it)
	kept := append([]CheckOverride(nil), it.Overrides...)
	for i := range found {
		found[i].By, found[i].At, found[i].Words = who, now.UTC(), words
		kept = append(kept, found[i])
		it.Note(now, "Auto pause overridden by @"+who+" · "+overrideWords(found[i]))
	}
	it.Overrides = kept
	return found, nil
}

// overrideWords names one overridden finding as the job's history says it.
func overrideWords(o CheckOverride) string {
	if o.Kind == OverridePolicy {
		return "repository policy: " + o.Text
	}
	return "proof: " + o.Text
}

// OverrideSummary names what an override covered, for a receipt.
func OverrideSummary(found []CheckOverride) string {
	parts := make([]string, 0, len(found))
	for _, o := range found {
		parts = append(parts, overrideWords(o))
	}
	return strings.Join(parts, "; ")
}
