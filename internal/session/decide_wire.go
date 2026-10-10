package session

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/decide"
	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// The live gate is the hook (decide_hook.go) with this conversation's own
// places behind it. The bridge names the place graph in the hello; the
// ledgers live in a folder beside that file, which is the one path both the
// session and the attention list can derive without a second channel.

const decisionsDirName = "decisions"

// errNothingMeasured is a score that has nothing to say. The gate treats any
// score error as "ask the person", and it does not remember that, so a later
// answer can still be seen.
var errNothingMeasured = errors.New("decide: nothing measured")

// gitWriteVerbs are the git subcommands that change a repository. Anything
// else git does is read as a read. The list is the assumption in
// desktop/docs/DESIGN-QUESTIONS.md (DG-4) until the designer names the classes.
var gitWriteVerbs = map[string]bool{
	"add": true, "am": true, "checkout": true, "cherry-pick": true, "clean": true,
	"clone": true, "commit": true, "init": true, "merge": true, "mv": true,
	"pull": true, "push": true, "rebase": true, "reset": true, "restore": true,
	"revert": true, "rm": true, "stash": true, "switch": true, "tag": true,
}

// DecideLedgerDir is the folder of per-place decision ledgers beside a place
// graph. The bridge creates it, the session opens it, and the attention list
// reads it, so all three name one directory.
func DecideLedgerDir(graphPath string) string {
	return filepath.Join(filepath.Dir(strings.TrimSpace(graphPath)), decisionsDirName)
}

// PrepareDecideLedger creates the ledger folder for graphPath. The files
// inside it appear on the first decision, not here.
func PrepareDecideLedger(graphPath string) error {
	if strings.TrimSpace(graphPath) == "" {
		return errors.New("decide: a ledger needs the place graph it sits beside")
	}
	return os.MkdirAll(DecideLedgerDir(graphPath), 0o700)
}

// attachDecideGate installs the live gate when this session was given a place
// graph. A terminal session has none, and every question stays with the person.
// The graph is read when a question is raised, not here, so a chat filed after
// the session opened is still a chat the place can see.
func (a *Agent) attachDecideGate() {
	if a == nil || a.config.PlaceGraph == nil || strings.TrimSpace(a.config.PlaceGraph.Path) == "" {
		return
	}
	chatID := strings.TrimSpace(a.id)
	if chatID == "" {
		return
	}
	path := a.config.PlaceGraph.Path
	dir := DecideLedgerDir(path)
	a.SetDecideGate(&DecideGate{
		Graph:  liveDecideGraph{path: path},
		ChatID: chatID,
		OpenStore: func(placeID string) (*decide.Store, error) {
			return decide.Open(dir, placeID, nil)
		},
		Score: func(placeID string, q Question) (decide.Result, string, error) {
			return scoreQuestion(dir, path, placeID, q)
		},
		SubjectClass: subjectClass,
	})
}

// notePersonLearning writes a person's answer onto the place that holds this
// chat. An answer the place took itself is not the person agreeing, so it is
// not a learning outcome. Called from ResolveQuestion, including an answer
// that was waiting on the doorstep when the session opened.
func (a *Agent) notePersonLearning(q Question, answer Answer) {
	if a == nil || answer.DecidedBy != DecidedByPerson || !resolvesQuestion(answer) {
		return
	}
	a.mu.Lock()
	g := a.decideGate
	a.mu.Unlock()
	if g == nil || g.OpenStore == nil {
		return
	}
	// THE GATE'S OWN ANSWER HOLDS ITS LOCK. A person's answer never does, so
	// this must not take that lock: take() calls ResolveQuestion while holding
	// it, and a dial answer has already returned above.
	placeID := g.placeOfChat()
	if placeID == "" {
		return
	}
	store, err := g.OpenStore(placeID)
	if err != nil || store == nil {
		return
	}
	class := ""
	if g.SubjectClass != nil {
		class = g.SubjectClass(q)
	}
	chosen := strings.TrimSpace(answer.FirstKey())
	if q.Pick != nil && strings.TrimSpace(q.Pick.Key) != "" && chosen != "" && strings.TrimSpace(string(q.Ask)) != "" {
		_, _ = store.RecordProposal(decide.ProposalAnswer{
			AskKind:      string(q.Ask),
			SubjectClass: class,
			ProposedKey:  q.Pick.Key,
			ChosenKey:    chosen,
			At:           answer.At,
		})
	}
	command := scoreSubject(questionCommand(q))
	if command == "" || chosen == "" || answer.At.IsZero() {
		return
	}
	action := chosen
	if option, ok := q.Option(chosen); ok && strings.TrimSpace(option.Label) != "" {
		action = strings.TrimSpace(option.Label)
	}
	// The same file the score reads. By is "person", not the place, so a row
	// the place decided and a row the person answered stay distinguishable.
	_ = store.Append(decide.Decision{
		ID:      "person:" + g.ChatID + ":" + questionToken(q.Kind, q.Token()),
		PlaceID: placeID,
		QuestionRef: decide.QuestionRef{
			Session: g.ChatID,
			Kind:    string(q.Kind),
			ID:      q.Token(),
		},
		AskKind:    string(q.Ask),
		Subject:    class,
		Command:    command,
		Action:     action,
		By:         string(DecidedByPerson),
		Stakes:     string(q.Stakes),
		Reversible: q.Stakes != StakesIrreversible,
		At:         answer.At,
	})
}

// placeOfChat is the chat's own place, the first active membership. Escalation
// may climb past it; learning is recorded where the chat is filed.
func (g *DecideGate) placeOfChat() string {
	if g == nil || g.Graph == nil {
		return ""
	}
	for _, m := range g.Graph.PlacesOf(g.ChatID) {
		p, ok := g.Graph.Place(m.PlaceID)
		if ok && !p.Archived && p.ID != "" {
			return p.ID
		}
	}
	return ""
}

// LedgerDecided reports that this question was already answered in one of the
// chat's places. An overturned row does not count: the answer was taken back.
// A missing file is not a decision, and it is not created here.
func LedgerDecided(dir, sessionID, kind, token string, placeIDs []string) bool {
	if strings.TrimSpace(dir) == "" || sessionID == "" || token == "" {
		return false
	}
	for _, id := range placeIDs {
		path, err := decide.LedgerFile(dir, id)
		if err != nil {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		store, err := decide.Open(dir, id, nil)
		if err != nil || store == nil {
			continue
		}
		listed, err := store.List()
		if err != nil {
			continue
		}
		for _, d := range listed {
			if d.OverturnedAt != nil {
				continue
			}
			if d.QuestionRef.Session == sessionID && d.QuestionRef.Kind == kind && d.QuestionRef.ID == token {
				return true
			}
		}
	}
	return false
}

// liveDecideGraph reads the place graph at question time. A snapshot taken
// when the session opened would miss a chat filed a moment later.
type liveDecideGraph struct {
	path string
}

func (g liveDecideGraph) snap() *placegraph.Snapshot {
	s, err := placegraph.ReadSnapshot(g.path)
	if err != nil || s == nil {
		return &placegraph.Snapshot{}
	}
	return s
}

func (g liveDecideGraph) Place(id string) (placegraph.Place, bool) {
	return g.snap().Place(id)
}

func (g liveDecideGraph) PlacesOf(chatID string) []placegraph.Membership {
	return g.snap().PlacesOf(chatID)
}

func (g liveDecideGraph) EffectiveDecide(id string) (placegraph.Decide, string) {
	return g.snap().EffectiveDecide(id)
}

// questionCommand is the bash text a permission attached. The consent lane
// puts that text in a code block; a question with no block is not about a
// command, and the score has nothing to cohort.
func questionCommand(q Question) string {
	for _, block := range q.Attach {
		if block.Kind == BlockCode {
			if command := strings.TrimSpace(block.Body); command != "" {
				return command
			}
		}
	}
	return ""
}

// scoreSubject is the family a command is scored as. "go test ./..." and
// "git push origin" keep their first two words; anything else keeps the
// first word. DG-4 records that this is the assumption.
func scoreSubject(command string) string {
	fields := strings.Fields(command)
	if len(fields) >= 2 && ((fields[0] == "go" && fields[1] == "test") || fields[0] == "git") {
		return fields[0] + " " + fields[1]
	}
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// subjectClass is the learning-ring class for a permission. A command with
// no text has no class, so the ask kind stands alone. DG-4 names the classes.
func subjectClass(q Question) string {
	if q.Ask != AskPermission {
		return ""
	}
	fields := strings.Fields(questionCommand(q))
	if len(fields) == 0 {
		return ""
	}
	if fields[0] == "git" {
		if len(fields) > 1 && gitWriteVerbs[fields[1]] {
			return "git-write"
		}
		return "git"
	}
	return "shell-read"
}

func scoreQuestion(dir, graphPath, placeID string, q Question) (decide.Result, string, error) {
	subject := scoreSubject(questionCommand(q))
	if subject == "" || placeID == "" {
		return decide.Result{}, "", errNothingMeasured
	}
	class := subjectClass(q)
	store, err := decide.Open(dir, placeID, nil)
	if err != nil || store == nil {
		if err == nil {
			err = errNothingMeasured
		}
		return decide.Result{}, "", err
	}
	listed, err := store.List()
	if err != nil {
		return decide.Result{}, "", err
	}
	kind := decide.KindKey(string(q.Ask), class)
	var answers []decide.Answer
	allow, deny := 0, 0
	for _, d := range listed {
		if d.OverturnedAt != nil || d.Command != subject {
			continue
		}
		if d.AskKind != "" && d.AskKind != string(q.Ask) {
			continue
		}
		if d.Subject != "" && class != "" && d.Subject != class {
			continue
		}
		choice := choiceOf(d.Action)
		if choice == "" {
			continue
		}
		if choice == "allow" {
			allow++
		} else {
			deny++
		}
		answers = append(answers, decide.Answer{
			ID: d.ID, Kind: kind, Subject: subject, Choice: choice,
		})
	}
	knows := supportingLines(graphPath, placeID, subject)
	choice := ""
	switch {
	case allow > deny:
		choice = "allow"
	case deny > allow:
		choice = "deny"
	case allow == 0 && deny == 0 && len(knows) > 0:
		choice = "allow"
	default:
		return decide.Result{}, "", errNothingMeasured
	}
	if choice != "allow" {
		// A knows line is evidence for allowing the command it names, not
		// for refusing it. DG-5 says why the match is the text containing
		// the subject.
		knows = nil
	}
	said := "allowed"
	if choice == "deny" {
		said = "denied"
	}
	result := decide.Score(decide.Proposal{
		Kind:      kind,
		Subject:   subject,
		Choice:    choice,
		Said:      said,
		Stakes:    string(q.Stakes),
		PlaceName: placeName(graphPath, placeID),
	}, decide.Evidence{Answers: answers, Knows: knows})
	if result.Percent == 0 && strings.TrimSpace(result.Because) == "" {
		return decide.Result{}, "", errNothingMeasured
	}
	key := optionForChoice(q, choice)
	if key == "" {
		return decide.Result{}, "", errNothingMeasured
	}
	return result, key, nil
}

func choiceOf(action string) string {
	a := strings.ToLower(strings.TrimSpace(action))
	switch {
	case a == "always" || strings.HasPrefix(a, "allow"):
		return "allow"
	case a == "no" || strings.HasPrefix(a, "deny"):
		return "deny"
	default:
		return ""
	}
}

func optionForChoice(q Question, choice string) string {
	switch choice {
	case "allow":
		for _, option := range q.Options {
			if option.Widening {
				continue
			}
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(option.Label)), "allow") {
				return option.Key
			}
		}
	case "deny":
		for _, option := range q.Options {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(option.Label)), "deny") {
				return option.Key
			}
		}
	}
	return ""
}

func placeName(graphPath, placeID string) string {
	snap, err := placegraph.ReadSnapshot(graphPath)
	if err != nil || snap == nil {
		return placeID
	}
	p, ok := snap.Place(placeID)
	if !ok || strings.TrimSpace(p.Name) == "" {
		return placeID
	}
	return strings.TrimSpace(p.Name)
}

// supportingLines are the place's live knows lines that name this subject.
// A replaced line does not count. The match is the subject occurring in the
// text; the confidence scorer does not read the sentence itself (DG-5).
func supportingLines(graphPath, placeID, subject string) []decide.Knows {
	snap, err := placegraph.ReadSnapshot(graphPath)
	if err != nil || snap == nil || subject == "" {
		return nil
	}
	needle := strings.ToLower(subject)
	var out []decide.Knows
	for _, line := range snap.Knowledge(placeID) {
		if line.ReplacedBy != "" || strings.TrimSpace(line.Text) == "" {
			continue
		}
		if strings.Contains(strings.ToLower(line.Text), needle) {
			out = append(out, decide.Knows{ID: line.ID, Supports: true})
		}
	}
	return out
}
