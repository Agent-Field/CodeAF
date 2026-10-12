package pipeline

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// Job is one piece of work [File.Decide] holds up to the file. LinesKnown says
// Lines was measured. CostKnown says Cost is an estimate; an unknown cost is
// not over the cap. Judged says the ask-first sentences were read, and Matched
// are the ones that apply, in the file's own words or the reader's.
type Job struct {
	ID         int
	Kind       factory.Kind
	Tier       factory.Tier
	Labels     []string
	Size       string
	Lines      int
	LinesKnown bool
	Cost       float64
	CostKnown  bool
	Cap        float64
	Paths      []string
	Judged     bool
	Matched    []string
}

// Decision is what happens to one job. Start and Ask are never both set.
// Both false means the job is not a candidate: nothing starts until a label
// is named, and a job without one of those labels waits.
type Decision struct {
	Start   bool
	Ask     bool
	Reasons []string
}

// DryRun is the settings sentence: which of these jobs would start, and which
// would ask first. WouldWait is every job that is not a candidate, with the
// reason it is waiting. Why stays the ask reasons only.
type DryRun struct {
	WouldStart []int
	WouldAsk   []int
	WouldWait  []Wait
	Why        map[int][]string
}

// Wait is one backlog job that would neither start nor ask, and why.
type Wait struct {
	ID     int
	Reason string
}

// Decide applies start. A job outside the file asks; it does not take a lease.
func (f File) Decide(j Job) Decision {
	if !f.candidate(j) {
		return Decision{}
	}
	var reasons []string
	if j.Kind == factory.KindPR && j.Tier == factory.TierStranger {
		reasons = append(reasons, "a stranger's pull request asks first")
	}
	if letter, why, ok := f.sizeOf(j); !ok {
		reasons = append(reasons, why)
	} else if overSize(letter, f.Start.MaxSize) {
		reasons = append(reasons, "over size "+normOr(f.Start.MaxSize, DefaultMaxSize)+" · this one is "+letter)
	}
	if j.CostKnown && !badMoney(j.Cost) && j.Cost > f.Start.MaxCost {
		reasons = append(reasons, "over "+money(f.Start.MaxCost))
	}
	if j.Cap > 0 && j.CostKnown && !badMoney(j.Cost) && j.Cost > j.Cap {
		reasons = append(reasons, "over your cap of "+money(j.Cap))
	}
	if why := neverWhy(f.Start.Never, j.Paths); why != "" {
		reasons = append(reasons, why)
	}
	reasons = append(reasons, askWhy(f.Start.AskFirst, j)...)
	if len(reasons) > 0 {
		return Decision{Ask: true, Reasons: reasons}
	}
	return Decision{Start: true}
}

// JobFromItem is one floor item as a job the file can decide.
//
// Cost is the triage estimate, and only after a triage has been stamped:
// an unread cost is not over the cap. A pull request's size is its changed
// lines, which exist only once its files were read; a letter someone typed
// is not that measurement. Ask-first sentences are judged by a model, and
// nothing on the item stores that judgment, so Judged stays false and a
// file that names those sentences asks rather than starts.
func JobFromItem(it factory.Item) Job {
	j := Job{
		ID: it.ID, Kind: it.Kind, Tier: it.Tier,
		Labels:    append([]string(nil), it.Labels...),
		Size:      it.Triage.Size,
		Cap:       it.Cap,
		Cost:      it.Triage.Est,
		CostKnown: !it.Triage.TriagedAt.IsZero(),
	}
	if it.Files == nil {
		return j
	}
	j.Paths = make([]string, 0, len(it.Files))
	for _, file := range it.Files {
		j.Lines += file.Added + file.Removed
		if file.Path != "" {
			j.Paths = append(j.Paths, file.Path)
		}
	}
	if it.Kind == factory.KindPR {
		j.LinesKnown = true
	}
	return j
}

// Dry is [Decide] for a list, in the list's order.
func (f File) Dry(jobs []Job) DryRun {
	d := DryRun{WouldStart: []int{}, WouldAsk: []int{}, WouldWait: []Wait{}, Why: map[int][]string{}}
	for _, job := range jobs {
		switch dec := f.Decide(job); {
		case dec.Start:
			d.WouldStart = append(d.WouldStart, job.ID)
		case dec.Ask:
			d.WouldAsk = append(d.WouldAsk, job.ID)
			d.Why[job.ID] = append([]string(nil), dec.Reasons...)
		default:
			d.WouldWait = append(d.WouldWait, Wait{ID: job.ID, Reason: waitReason(f.Start.Labels)})
		}
	}
	return d
}

// waitReason is the sentence a dry run shows for a job that is not a
// candidate. An unnamed label means nothing starts; one named label is the
// sentence the settings page uses.
func waitReason(labels []string) string {
	var named []string
	for _, label := range labels {
		if label = strings.TrimSpace(label); label != "" {
			named = append(named, label)
		}
	}
	switch len(named) {
	case 0:
		return "nothing starts on its own"
	case 1:
		return "no " + named[0] + " label"
	default:
		return "no " + strings.Join(named, " or ") + " label"
	}
}

func (f File) candidate(j Job) bool {
	if len(f.Start.Labels) == 0 {
		return false
	}
	for _, want := range f.Start.Labels {
		if hasFold(j.Labels, want) {
			return true
		}
	}
	return false
}

func (f File) sizeOf(j Job) (letter, why string, ok bool) {
	if j.Kind == factory.KindPR {
		// A PULL REQUEST IS MEASURED, NEVER GUESSED. A letter someone typed
		// is not a measurement.
		if j.LinesKnown && j.Lines >= 0 {
			return prLetter(j.Lines, f.Size.PRLines), "", true
		}
		return "", "not measured yet", false
	}
	mode := f.Size.Issue
	if mode == "" {
		mode = DefaultIssueMode
	}
	if mode == IssueModeLabel {
		if s, ok := labelSize(j.Labels); ok {
			return s, "", true
		}
		return "", "no size label", false
	}
	if s, ok := normSize(j.Size); ok {
		return s, "", true
	}
	return "", "size not read yet", false
}

func prLetter(lines int, bounds [2]int) string {
	switch {
	case lines <= bounds[0]:
		return "S"
	case lines <= bounds[1]:
		return "M"
	default:
		return "L"
	}
}

func labelSize(labels []string) (string, bool) {
	best := 0
	got := ""
	for _, label := range labels {
		rest, ok := strings.CutPrefix(strings.ToLower(strings.TrimSpace(label)), "size:")
		if !ok {
			rest, ok = strings.CutPrefix(strings.ToLower(strings.TrimSpace(label)), "size/")
		}
		if !ok {
			continue
		}
		letter, good := normSize(rest)
		if !good {
			continue
		}
		rank := sizeRank(letter)
		if rank > best {
			best = rank
			got = letter
		}
	}
	return got, got != ""
}

func sizeRank(s string) int {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "XS":
		return 1
	case "S":
		return 2
	case "M":
		return 3
	case "L":
		return 4
	case "XL":
		return 5
	default:
		return 0
	}
}

func overSize(got, max string) bool {
	return sizeRank(got) > sizeRank(normOr(max, DefaultMaxSize))
}

func normOr(s, fallback string) string {
	if letter, ok := normSize(s); ok {
		return letter
	}
	return fallback
}

func neverWhy(globs, paths []string) string {
	var hits []string
	extra := false
	for _, path := range paths {
		var matched string
		for _, glob := range globs {
			if touches(glob, path) {
				matched = glob
				break
			}
		}
		if matched == "" {
			continue
		}
		if len(hits) == 3 {
			extra = true
			break
		}
		hits = append(hits, path+" ("+matched+")")
	}
	if len(hits) == 0 {
		return ""
	}
	why := "touches " + strings.Join(hits, ", ")
	if extra {
		why += ", …"
	}
	return why
}

func askWhy(sentences []string, j Job) []string {
	if len(sentences) == 0 {
		return nil
	}
	if !j.Judged {
		return []string{"ask first · not judged yet"}
	}
	var out []string
	for _, sentence := range sentences {
		if hasFold(j.Matched, sentence) {
			out = append(out, "ask first · "+sentence)
		}
	}
	return out
}

func badMoney(v float64) bool {
	return math.IsNaN(v) || math.IsInf(v, 0)
}

func money(v float64) string {
	if badMoney(v) {
		return "$0"
	}
	if v == math.Trunc(v) {
		return "$" + strconv.FormatInt(int64(v), 10)
	}
	return "$" + strconv.FormatFloat(v, 'f', 2, 64)
}

// touches reports whether path matches pattern. A pattern is a path glob:
// * is one segment, ** is any number of them. Without a leading slash the
// pattern may sit anywhere in the path, so auth/** matches internal/auth/keys.go.
func touches(pattern, path string) bool {
	pattern = strings.TrimSpace(pattern)
	path = strings.TrimSpace(path)
	if pattern == "" || path == "" {
		return false
	}
	anchored := strings.HasPrefix(pattern, "/")
	pat := splitPath(pattern)
	segs := splitPath(path)
	if len(pat) == 0 || len(segs) == 0 {
		return false
	}
	if anchored {
		return matchSegs(pat, segs)
	}
	for i := range segs {
		if matchSegs(pat, segs[i:]) {
			return true
		}
	}
	return false
}

func splitPath(p string) []string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	var out []string
	for _, s := range strings.Split(p, "/") {
		if s != "" && s != "." {
			out = append(out, s)
		}
	}
	return out
}

func matchSegs(pat, segs []string) bool {
	if len(pat) == 0 {
		return len(segs) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(segs); i++ {
			if matchSegs(pat[1:], segs[i:]) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 || !matchOne(pat[0], segs[0]) {
		return false
	}
	return matchSegs(pat[1:], segs[1:])
}

func matchOne(pat, seg string) bool {
	var b strings.Builder
	b.WriteByte('^')
	for i := 0; i < len(pat); {
		r, size := utf8.DecodeRuneInString(pat[i:])
		i += size
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteByte('.')
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteByte('$')
	re, err := regexp.Compile(b.String())
	if err != nil {
		return false
	}
	return re.MatchString(seg)
}
