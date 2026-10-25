package manual

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftcore"
)

// R6/R7/R8/R15/R30 require the retrieved answers to state today's capabilities.
func TestR30ClusterQuestionsReachCompleteAnswers(t *testing.T) {
	for _, probe := range []struct {
		question string
		facts    []string
	}{
		{"can my phone lead", []string{"no codeaf phone app yet", "cannot lead yet", "persistent storage", "codeaf cluster lead"}},
		{"what is cluster lead", []string{"joint change", "transfers leadership", "receipt", "saved signed request"}},
		{"what happens if my leader goes away", []string{"restore the third anchor", "does not automatically regain leadership", "saved chats", cs.LeadershipUnavailable}},
		{"what can a worker do", []string{"selected chats", "cannot approve pairing", "read the identity vault", "not available as a terminal command yet"}},
	} {
		t.Run(probe.question, func(t *testing.T) {
			var answer strings.Builder
			for _, section := range Chat().Search(probe.question, DefaultResults) {
				if section.Page == "coordination-and-workers" {
					answer.WriteString(section.Body)
				}
			}
			for _, fact := range probe.facts {
				if !strings.Contains(answer.String(), fact) {
					t.Errorf("retrieved answer lacks %q", fact)
				}
			}
		})
	}
}

// R8/R9/R25/R30 bind every quoted coordination refusal to its product definition.
func TestR30CoordinationQuotesMatchProductStrings(t *testing.T) {
	messages := []string{
		cs.LeadershipUnavailable,
		cs.ErrMigration.Error(),
		cs.EnrollmentMessage(cs.Capacity),
		cs.EnrollmentMessage(cs.UpdateRequired),
		(clusterctl.CommittedRefusal{}).Error(),
		chatlist.LostRace,
	}
	for _, source := range []struct{ path, declaration, prefix string }{
		{"../syncsetup/cluster.go", "LeadCluster", "this computer must join"},
		{"../syncsetup/member.go", "errMemberWaiting", "this computer is still joining"},
		{"../syncsetup/worker.go", "ErrWorkerIncomplete", "this work may be incomplete"},
	} {
		found := false
		for _, text := range clusterDeclarationStrings(t, source.path, source.declaration) {
			if strings.HasPrefix(text, source.prefix) {
				messages = append(messages, text)
				found = true
			}
		}
		if !found {
			t.Fatalf("product declaration %s no longer defines its refusal", source.declaration)
		}
	}
	seen := make(map[string]int)
	for _, text := range messages {
		seen[text] = 0
	}
	quotes := regexp.MustCompile("`([^`\n]+)`")
	for page, body := range flatChatPages(t) {
		for _, match := range quotes.FindAllStringSubmatch(body, -1) {
			text := match[1]
			for _, prefix := range []string{"this cluster", "this computer must join", "this computer is still joining", "this codeaf build", "the leadership request", "codeaf: directory upgrade", "this work may be incomplete", "another device continued this chat first"} {
				if strings.HasPrefix(text, prefix) {
					if _, ok := seen[text]; !ok {
						t.Errorf("%s quotes a sentence absent from the product: %q", page, text)
					} else {
						seen[text]++
					}
					break
				}
			}
		}
	}
	for text, count := range seen {
		if count == 0 {
			t.Errorf("manual omits product sentence %q", text)
		}
	}
	labels := make(map[string]bool)
	for _, format := range clusterDeclarationStrings(t, "../../cmd/codeaf/cluster.go", "clusterCommand") {
		if label, _, ok := strings.Cut(format, " %"); ok && strings.HasSuffix(format, "\n") {
			labels[label] = true
		}
	}
	page, _ := Chat().Page("coordination-and-workers")
	_, status, ok := strings.Cut(page, "Status names ")
	if !ok {
		t.Fatal("manual omits the status labels")
	}
	status, _, _ = strings.Cut(status, ".")
	statusQuotes := quotes.FindAllStringSubmatch(status, -1)
	if len(statusQuotes) == 0 {
		t.Fatal("manual status sentence has no quoted labels")
	}
	for _, match := range statusQuotes {
		if !labels[match[1]] {
			t.Errorf("manual status label is absent from the command output: %q", match[1])
		}
	}
}

// Unexported client refusals stay private to the client; syntax reads avoid
// linking its entire runtime merely to check the manual's spelling.
func clusterDeclarationStrings(t *testing.T, path, name string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var values []string
	ast.Inspect(file, func(node ast.Node) bool {
		var declaration ast.Node
		switch n := node.(type) {
		case *ast.FuncDecl:
			if n.Name.Name == name {
				declaration = n
			}
		case *ast.ValueSpec:
			for _, id := range n.Names {
				if id.Name == name {
					declaration = n
				}
			}
		}
		if declaration == nil {
			return true
		}
		ast.Inspect(declaration, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				text, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
				values = append(values, text)
			}
			return true
		})
		return false
	})
	return values
}

// R7/R9/R25/R27/R30 keep published timing and limits tied to their shared values.
func TestR30CoordinationTimingAndLimitsMatchProduct(t *testing.T) {
	page, _ := Chat().Page("coordination-and-workers")
	seconds := cs.TickEveryMillis / 1000
	facts := []string{
		fmt.Sprintf("Coordination ticks every %d seconds", seconds),
		fmt.Sprintf("at most %d live learners", cs.MaxLearners),
		fmt.Sprintf("up to %d MiB", cs.MaxV1BodyBytes>>20),
	}
	fallbackTicks := raftcore.ElectionTicks + 4*raftcore.ElectionWindowTicks - 1
	facts = append(facts, fmt.Sprintf("Healthy fallback targets about %d seconds", int64(fallbackTicks)*seconds))
	for rank := 0; rank < 4; rank++ {
		low := raftcore.ElectionTicks + rank*raftcore.ElectionWindowTicks
		high := low + raftcore.ElectionWindowTicks - 1
		facts = append(facts, fmt.Sprintf("%d–%d ticks (%d–%d seconds)", low, high, int64(low)*seconds, int64(high)*seconds))
	}
	for _, fact := range facts {
		if !strings.Contains(page, fact) {
			t.Errorf("manual omits current product timing or limit %q", fact)
		}
	}
	for _, section := range Chat().Sections() {
		if section.Page == "coordination-and-workers" && len([]rune(section.Body)) > 2000 {
			t.Errorf("coordination section %q exceeds 2,000 characters", section.Title)
		}
	}
}
