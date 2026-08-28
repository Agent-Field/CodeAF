package resident

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// SentinelPrompt is the complete bounded judgment made at one durable wake.
type SentinelPrompt struct {
	CharterID    string
	Invariant    string
	SentinelHint string
	Watch        store.WatchSpec
	Evidence     string
	// Previous is what this sentinel decided at its last few wakes and what
	// became of each decision, newest first. For a poll charter the evidence is
	// the constant condition string, so without this the call is byte-identical
	// every wake — a firing the user has already declined is judged the same
	// way again an hour later, and again, forever.
	Previous []string
	// Voice is the learned speech contract, assembled here rather than at the
	// surface because the notebook belongs to the resident. The sentinel's line
	// is read by the user, so it is subject to the same learned preferences as
	// anything else that speaks; an empty notebook leaves it empty and the
	// prompt byte-identical.
	Voice string
}

// SentinelVerdict is deliberately tiny: yes/no plus the one-line reason the
// charter journal keeps.
type SentinelVerdict struct {
	Yes  bool
	Line string
}

// SentinelFunc makes exactly one cheap model call for a reserved wake.
type SentinelFunc func(ctx context.Context, prompt SentinelPrompt) (SentinelVerdict, error)

// WatchPass reports what one reconciler or `aforge wake` pass decided.
type WatchPass struct {
	Examined  int
	Woken     int
	Checked   int
	Fired     int
	Proposed  int
	No        int
	Errors    int
	Quota     int
	Expired   int
	RailWaits int
}

// WithWatchEngine enables standing work in this reconciler. Nil keeps the
// existing resident and headless paths inert.
func (r *Reconciler) WithWatchEngine(dailyBudgetUSD float64, sentinel SentinelFunc) *Reconciler {
	r.dailyBudgetUSD = dailyBudgetUSD
	r.sentinel = sentinel
	return r
}

// WatchOnce runs exactly one pass over the charters that were due when the
// pass began. Interrupted wake phases are resumed from their journaled state.
func (r *Reconciler) WatchOnce(ctx context.Context) (WatchPass, error) {
	if err := ctx.Err(); err != nil {
		return WatchPass{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.store == nil {
		return WatchPass{}, fmt.Errorf("watch pass: nil store")
	}
	return r.watchOnceLocked(ctx)
}

func (r *Reconciler) watchOnceLocked(ctx context.Context) (WatchPass, error) {
	var pass WatchPass
	if r.sentinel == nil {
		return pass, nil
	}
	now := r.now()
	expired, err := r.store.RetireExpiredCharters(now)
	if err != nil {
		return pass, err
	}
	pass.Expired += expired
	charters, err := r.store.DueCharters(now, 100)
	if err != nil {
		return pass, err
	}
	for _, charter := range charters {
		if err := ctx.Err(); err != nil {
			return pass, err
		}
		if store.IsPracticeCharter(charter) {
			continue
		}
		pass.Examined++
		retired, err := r.store.RetireExpiredCharter(charter.ID, now)
		if err != nil {
			return pass, err
		}
		if retired {
			pass.Expired++
			continue
		}
		if charter.WakePending {
			if charter.SentinelYes {
				r.finishCharterFiring(ctx, charter, &pass)
				continue
			}
			evidence := charter.WakeEvidence
			if strings.TrimSpace(evidence) == "" {
				evidence = "resuming reserved wake"
			}
			r.checkCharterSentinel(ctx, charter, evidence, &pass)
			continue
		}

		due := charter.NextDue
		if due.IsZero() {
			due = now
		}
		nextDue, err := store.NextWatchDue(charter.Watch, due)
		if err != nil {
			return pass, err
		}
		state := store.CharterWatchState{
			NextDue: nextDue, FileFingerprint: charter.FileFingerprint,
			GraphCursor: charter.GraphCursor, GraphDay: charter.GraphDay,
			GraphTriggered: charter.GraphTriggered,
		}
		wake, evidence, err := r.watchOccurred(charter, now, &state)
		if err != nil {
			log.Printf("charter %s watch: %v", charter.ID, err)
			if advanceErr := r.store.AdvanceCharterWatch(charter.ID, state); advanceErr != nil {
				return pass, advanceErr
			}
			continue
		}
		if !wake {
			if err := r.store.AdvanceCharterWatch(charter.ID, state); err != nil {
				return pass, err
			}
			continue
		}
		wakeSeq, err := r.store.BeginCharterWake(charter.ID, now, evidence, state)
		if err != nil {
			return pass, err
		}
		pass.Woken++
		charter.WakeSeq, charter.WakePending = wakeSeq, true
		charter.NextDue = nextDue
		r.checkCharterSentinel(ctx, charter, evidence, &pass)
	}
	return pass, nil
}

func (r *Reconciler) watchOccurred(charter store.Charter, now time.Time, state *store.CharterWatchState) (bool, string, error) {
	switch charter.Watch.Kind {
	case store.WatchCron:
		// The hour, not the second. A cron wake is judged on whether the moment
		// arrived, and the exact second could not be read for anything and could
		// not be reused for anything. The sentence says "in the hour beginning"
		// rather than "at" because a coarser number must not become a wrong one:
		// a five-minute cron now repeats the same true line twelve times instead
		// of writing twelve strings nothing can share.
		return true, "scheduled occurrence in the hour beginning " + charter.NextDue.Truncate(time.Hour).Format(time.RFC3339), nil
	case store.WatchPoll:
		return true, charter.Watch.Poll.Condition, nil
	case store.WatchFile:
		fingerprint, detail, err := fileWatchFingerprint(charter.Watch.File.Glob)
		if err != nil {
			return false, "", err
		}
		state.FileFingerprint = fingerprint
		if charter.FileFingerprint == "" {
			return false, "", nil
		}
		if fingerprint == charter.FileFingerprint {
			return false, "", nil
		}
		return true, detail, nil
	case store.WatchGraph:
		return r.graphWatchOccurred(charter, now, state)
	default:
		return false, "", fmt.Errorf("unknown watch kind %q", charter.Watch.Kind)
	}
}

func fileWatchFingerprint(pattern string) (string, string, error) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", "", err
	}
	sort.Strings(matches)
	hash := sha256.New()
	var changed []string
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", "", err
		}
		// The fingerprint keeps nanoseconds — it is the change detector and must
		// notice a write a millisecond apart. The evidence line does not: it is
		// read by a model asked whether the condition occurred, never how many
		// nanoseconds ago, and a nanosecond stamp is a string no two checks can
		// ever share. Rounded to the minute it says the same true thing and says
		// it in the same bytes.
		fmt.Fprintf(hash, "%s\x00%d\x00%d\n", path, info.ModTime().UnixNano(), info.Size())
		changed = append(changed, fmt.Sprintf("%s mtime %s", path, info.ModTime().Truncate(time.Minute).Format(time.RFC3339)))
	}
	if len(matches) == 0 {
		hash.Write([]byte("missing\n"))
		changed = append(changed, pattern+" has no matches")
	}
	return hex.EncodeToString(hash.Sum(nil)), strings.Join(changed, "; "), nil
}

func (r *Reconciler) graphWatchOccurred(charter store.Charter, now time.Time, state *store.CharterWatchState) (bool, string, error) {
	watch := charter.Watch.Graph
	if watch.Predicate == store.GraphSpendThreshold {
		day := now.In(time.Local).Format("2006-01-02")
		if state.GraphDay != day {
			state.GraphDay, state.GraphTriggered = day, false
		}
		spend, err := r.store.SpendToday()
		if err != nil {
			return false, "", err
		}
		if spend >= watch.ThresholdUSD && !state.GraphTriggered {
			state.GraphTriggered = true
			return true, fmt.Sprintf("today's spend reached $%.2f", spend), nil
		}
		return false, "", nil
	}
	// Two things used to be wrong with the read below, and both of them cost the
	// most on the pass where the watch has nothing to look at.
	//
	// The journal was asked for EVERYTHING past the cursor — a limit of zero
	// means no limit — so a watch whose cursor sits at the start of a long-lived
	// store decoded the whole journal into memory to walk it once. It is now
	// drained a batch at a time, in a loop, so full coverage is unchanged: the
	// cursor advances exactly as far, and the search stops on exactly the same
	// event it stopped on before.
	//
	// And the scope's facts were read before anything checked whether there were
	// any events at all. That is a full scan of the notebook, twice a second,
	// for a watch that is going to look at nothing — so it now happens on the
	// first batch that has something in it, and never on the idle pass.
	scopeNodes := map[string]bool{}
	scopeLoaded := false
	cursor := charter.GraphCursor
	for {
		events, err := r.store.Events(cursor, eventBatchSize)
		if err != nil {
			return false, "", err
		}
		if len(events) == 0 {
			return false, "", nil
		}
		if !scopeLoaded && strings.TrimSpace(watch.Scope) != "" {
			facts, err := r.store.Facts(0)
			if err != nil {
				return false, "", err
			}
			for _, fact := range facts {
				if fact.Status == store.FactActive && strings.EqualFold(fact.Scope, watch.Scope) {
					scopeNodes[fact.NodeID] = true
				}
			}
		}
		scopeLoaded = true
		for _, event := range events {
			cursor = event.Seq
			state.GraphCursor = event.Seq
			wanted := watch.Predicate == store.GraphNodeSettled && event.Kind == store.EventNodeCompleted ||
				watch.Predicate == store.GraphNodeFailed && event.Kind == store.EventNodeFailed
			if !wanted {
				continue
			}
			node, found, err := r.store.Node(event.NodeID)
			if err != nil {
				return false, "", err
			}
			if !found {
				continue
			}
			titleMatch := strings.TrimSpace(watch.Title) == "" || strings.Contains(strings.ToLower(node.Title+" "+node.Brief), strings.ToLower(strings.TrimSpace(watch.Title)))
			scopeMatch := strings.TrimSpace(watch.Scope) == "" || scopeNodes[node.ID]
			if titleMatch && scopeMatch {
				evidence := fmt.Sprintf("node %s (%s) recorded %s", node.ID, firstLine(node.Title+" "+node.Brief), event.Kind)
				return true, evidence, nil
			}
		}
		if len(events) < eventBatchSize {
			return false, "", nil
		}
	}
}

// sentinelMemory is how many past judgments a sentinel is shown, and
// sentinelMemoryBytes is what the whole block may cost. Both are small on
// purpose: this is the cheapest call in the system and the point of showing it
// its own history is to stop a loop, not to hand it a transcript.
const (
	sentinelMemory      = 4
	sentinelMemoryBytes = 512
)

// previousJudgments renders what this sentinel last decided, newest first. A
// read failure yields nothing rather than failing the wake: the memory makes
// the judgment better, and no judgment at all is worse than a forgetful one.
func (r *Reconciler) previousJudgments(charterID string) []string {
	if r.store == nil {
		return nil
	}
	judgments, err := r.store.RecentSentinelJudgments(charterID, sentinelMemory)
	if err != nil || len(judgments) == 0 {
		return nil
	}
	lines := make([]string, 0, len(judgments))
	remaining := sentinelMemoryBytes
	for _, judgment := range judgments {
		answer := "no"
		if judgment.Yes {
			answer = "yes"
		}
		line := answer
		if reason := strings.TrimSpace(judgment.Line); reason != "" {
			line += " — " + reason
		}
		if judgment.Error != "" {
			line += " (the judgment itself failed: " + judgment.Error + ")"
		}
		if judgment.Outcome != "" {
			line += "; " + judgment.Outcome
		}
		if len(line) > remaining {
			break
		}
		remaining -= len(line)
		lines = append(lines, line)
	}
	return lines
}

func (r *Reconciler) checkCharterSentinel(ctx context.Context, charter store.Charter, evidence string, pass *WatchPass) {
	prompt := SentinelPrompt{
		CharterID: charter.ID, Invariant: charter.Invariant, SentinelHint: charter.SentinelHint,
		Watch: charter.Watch, Evidence: evidence,
		Previous: r.previousJudgments(charter.ID),
		Voice:    VoiceSection(r.store, charter.Invariant, charter.SentinelHint),
	}
	// The prompt is assembled under the reconciler's lock and the judgment is
	// made without it. A sentinel is a model round-trip on a cadence nobody
	// chose to wait for, and the lock it was holding is the one a chat takes to
	// open — so this call was one of the ways arriving at the keyboard could
	// hang. Nothing it decides lives in memory: the verdict lands in the journal
	// keyed to this wake, which is what makes a stale one refuse rather than
	// overwrite.
	var verdict SentinelVerdict
	var err error
	r.thinking(func() { verdict, err = r.sentinel(ctx, prompt) })
	check := store.SentinelCheck{WakeSeq: charter.WakeSeq, Yes: verdict.Yes, Line: verdict.Line}
	if err != nil {
		check.Yes, check.Error = false, err.Error()
		log.Printf("charter %s sentinel: %v", charter.ID, err)
	}
	if recordErr := r.store.RecordSentinelCheck(charter.ID, check); recordErr != nil {
		log.Printf("charter %s sentinel journal: %v", charter.ID, recordErr)
		pass.Errors++
		return
	}
	pass.Checked++
	if err != nil {
		pass.Errors++
		return
	}
	if !verdict.Yes {
		pass.No++
		return
	}
	charter.SentinelYes = true
	r.finishCharterFiring(ctx, charter, pass)
}

func (r *Reconciler) finishCharterFiring(ctx context.Context, charter store.Charter, pass *WatchPass) {
	approved := false
	if charter.Autonomy == store.CharterProbation {
		approved, _ = r.store.CharterFiringApproved(charter.ID, charter.WakeSeq)
		if !approved {
			// Silence is an answer, and it was the one answer this loop could
			// not hear. A proposal nobody responded to pinned the wake open
			// forever — the charter could neither fire nor advance to its next
			// due moment, and the same unread question was re-offered on every
			// pass. Past its window it is declined the way an explicit "not
			// now" is, which closes the wake, resets the consecutive run, and
			// counts against promotion through the same refusal ledger.
			lapsed, err := r.store.LapsedCharterFiringProposal(charter.ID, charter.WakeSeq, r.now())
			if err != nil {
				log.Printf("charter %s proposal window: %v", charter.ID, err)
			} else if lapsed {
				if err := r.store.DeclineCharterFiring(charter.ID, charter.WakeSeq,
					"the firing was proposed and stood unanswered past its window", false); err != nil {
					log.Printf("charter %s lapse: %v", charter.ID, err)
					pass.Errors++
					return
				}
				pass.No++
				return
			}
			posted, err := r.store.ProposeCharterFiring(charter.ID, charter.WakeSeq, charter.Action.Template)
			if err != nil {
				log.Printf("charter %s propose firing: %v", charter.ID, err)
				pass.Errors++
				return
			}
			if posted {
				pass.Proposed++
			}
			return
		}
	}
	disposition, _, err := r.admitCharterFiring(ctx, charter, approved)
	if err != nil {
		log.Printf("charter %s fire: %v", charter.ID, err)
		pass.Errors++
		return
	}
	switch disposition {
	case store.FireAdmitted:
		pass.Fired++
	case store.FireQuota:
		pass.Quota++
	case store.FireExpired:
		pass.Expired++
	case store.FireRailWait:
		pass.RailWaits++
	}
}

func (r *Reconciler) admitCharterFiring(ctx context.Context, charter store.Charter, approved bool) (store.FireDisposition, string, error) {
	intent := strings.TrimSpace(charter.Action.Template)
	craftReference := ""
	var subtree store.Subtree
	jobID := "say:" + charter.ID + ":" + fmt.Sprint(charter.WakeSeq)
	sessionID := charter.SessionID
	if sessionID == "" {
		sessionID = charter.Ratification.SessionID
	}
	if !charter.Action.SayOnly {
		snapshot, err := r.store.ActiveSnapshot()
		if err != nil {
			return "", "", fmt.Errorf("ground: %w", err)
		}
		compiled, err := r.compile(ctx, charter.Action.Template, r.renderCompileContext(snapshot, charter.Action.Template))
		if err != nil {
			return "", "", fmt.Errorf("ground: %w", err)
		}
		if strings.TrimSpace(compiled.Goal) == "" || strings.TrimSpace(compiled.Question) != "" {
			return "", "", fmt.Errorf("ground: action needs clarification")
		}
		intent = compiled.Goal
		prefix := firingPrefix(charter.ID, charter.WakeSeq)
		// The same craft seam an ordinary splice gets, asked in the same place:
		// in front of the planner, on the user's own words rather than the
		// compiler's paraphrase. The standing invariant is those words verbatim;
		// the action template is the compiled restatement and stands in only
		// when a charter carries no invariant of its own.
		firingProvenance := store.Provenance{Origin: store.OriginTrigger, SessionID: sessionID,
			Intent: intent, CharterID: charter.ID}
		use, usingCraft := r.craftFor(ctx, charterCraftRequest(charter), false, prefix, firingProvenance)
		switch {
		case usingCraft:
			subtree = use.subtree
			craftReference = use.reference
		case r.plan == nil:
			subtree = store.Subtree{Nodes: []store.NodeSpec{{ID: prefix, Brief: intent, Stage: 1}}}
		default:
			planCtx := withPlanAnchor(ctx, PlanAnchor{NodeID: prefix, SessionID: sessionID})
			subtree, err = r.plan(planCtx, compiled)
			if err != nil {
				return "", "", fmt.Errorf("plan: %w", err)
			}
		}
		r.titleSubtree(ctx, &subtree, compiled)
		jobID = subtreeRootID(subtree)
	}
	provenance := store.Provenance{Origin: store.OriginTrigger, SessionID: sessionID,
		Intent: intent, CharterID: charter.ID, Craft: craftReference}
	var disposition store.FireDisposition
	var err error
	if approved {
		disposition, err = r.store.FireApprovedCharter(charter.ID, charter.WakeSeq, subtree, provenance, r.dailyBudgetUSD, r.now())
	} else {
		disposition, err = r.store.FireCharter(charter.ID, charter.WakeSeq, subtree, provenance, r.dailyBudgetUSD, r.now())
	}
	if err != nil {
		return "", "", err
	}
	if disposition == store.FireAdmitted && charter.Action.SayOnly {
		_, err = r.store.RecordCharterFiringOutcome(store.CharterFiringAssessment{
			CharterID: charter.ID, WakeSeq: charter.WakeSeq, JobID: jobID, Decided: true,
			Success: true, Reason: "approved reminder delivered within its rails",
		}, tenureAfter())
	}
	return disposition, jobID, err
}

// charterCraftRequest is what the shelf is asked to recognize. The invariant is
// the sentence the user actually said — "watch HN every morning for posts about
// agent frameworks and brief me" — and recognition is deliberately run against
// the user's wording everywhere else for the same reason it is here.
func charterCraftRequest(charter store.Charter) string {
	if invariant := strings.TrimSpace(charter.Invariant); invariant != "" {
		return invariant
	}
	return strings.TrimSpace(charter.Action.Template)
}

func subtreeRootID(subtree store.Subtree) string {
	for _, node := range subtree.Nodes {
		if node.Parent == "" {
			return node.ID
		}
	}
	return ""
}

func firingPrefix(charterID string, wakeSeq int64) string {
	var clean strings.Builder
	for _, char := range strings.ToLower(charterID) {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			clean.WriteRune(char)
		} else {
			clean.WriteByte('-')
		}
	}
	return fmt.Sprintf("firing-%s-%d", strings.Trim(clean.String(), "-"), wakeSeq)
}

func tenureAfter() int {
	const fallback = 3
	raw := strings.TrimSpace(os.Getenv("AFORGE_TENURE_AFTER"))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
