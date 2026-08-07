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
		return true, "scheduled occurrence at " + charter.NextDue.Format(time.RFC3339), nil
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
		fmt.Fprintf(hash, "%s\x00%d\x00%d\n", path, info.ModTime().UnixNano(), info.Size())
		changed = append(changed, fmt.Sprintf("%s mtime %s", path, info.ModTime().Format(time.RFC3339Nano)))
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
	events, err := r.store.Events(charter.GraphCursor, 0)
	if err != nil {
		return false, "", err
	}
	scopeNodes := map[string]bool{}
	if strings.TrimSpace(watch.Scope) != "" {
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
	for _, event := range events {
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
	return false, "", nil
}

func (r *Reconciler) checkCharterSentinel(ctx context.Context, charter store.Charter, evidence string, pass *WatchPass) {
	verdict, err := r.sentinel(ctx, SentinelPrompt{
		CharterID: charter.ID, Invariant: charter.Invariant, SentinelHint: charter.SentinelHint,
		Watch: charter.Watch, Evidence: evidence,
	})
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
	intent := strings.TrimSpace(charter.Action.Template)
	var subtree store.Subtree
	if !charter.Action.SayOnly {
		snapshot, err := r.store.ActiveSnapshot()
		if err != nil {
			log.Printf("charter %s ground: %v", charter.ID, err)
			pass.Errors++
			return
		}
		compiled, err := r.compile(ctx, charter.Action.Template, r.renderCompileContext(snapshot, charter.Action.Template))
		if err != nil {
			log.Printf("charter %s ground: %v", charter.ID, err)
			pass.Errors++
			return
		}
		if strings.TrimSpace(compiled.Goal) == "" || strings.TrimSpace(compiled.Question) != "" {
			log.Printf("charter %s ground: action needs clarification", charter.ID)
			pass.Errors++
			return
		}
		intent = compiled.Goal
		prefix := firingPrefix(charter.ID, charter.WakeSeq)
		if r.plan == nil {
			subtree = store.Subtree{Nodes: []store.NodeSpec{{ID: prefix, Brief: intent, Stage: 1}}}
		} else {
			planCtx := withPlanAnchor(ctx, PlanAnchor{NodeID: prefix, SessionID: charter.Ratification.SessionID})
			subtree, err = r.plan(planCtx, compiled)
			if err != nil {
				log.Printf("charter %s plan: %v", charter.ID, err)
				pass.Errors++
				return
			}
		}
		r.titleSubtree(ctx, &subtree, compiled)
	}
	provenance := store.Provenance{
		Origin: store.OriginTrigger, SessionID: charter.Ratification.SessionID,
		Intent: intent, CharterID: charter.ID,
	}
	disposition, err := r.store.FireCharter(charter.ID, charter.WakeSeq, subtree, provenance, r.dailyBudgetUSD, r.now())
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
