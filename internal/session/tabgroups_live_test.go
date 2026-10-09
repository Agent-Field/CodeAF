package session

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// The live proof that a topic offer is asked on deepseek/deepseek-v4.1-flash
// and journaled as the organizing role. It reads a copy of the synthetic
// corpus and does not write the original. It is opt-in:
//
//	CODEAF_LIVE_TABGROUPS=1 CODEAF_TABGROUP_CORPUS=/path/to/cpw-corpus go test ./internal/session -run TestLiveTabGroupTopic -count=1 -timeout 5m -v
const liveTabGroupModel = "deepseek/deepseek-v4.1-flash"

func TestLiveTabGroupTopic(t *testing.T) {
	if os.Getenv("CODEAF_LIVE_TABGROUPS") == "" {
		t.Skip("set CODEAF_LIVE_TABGROUPS=1 to spend one organizing call on a copied corpus")
	}
	corpus := strings.TrimSpace(os.Getenv("CODEAF_TABGROUP_CORPUS"))
	if corpus == "" {
		t.Fatal("CODEAF_TABGROUP_CORPUS must name the corpus to copy")
	}
	key := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if key == "" {
		t.Fatal("no OPENROUTER_API_KEY")
	}
	src := filepath.Join(corpus, "h", ".codeaf", "v3", "projects")
	copyRoot := t.TempDir()
	if err := copyTree(src, filepath.Join(copyRoot, "projects")); err != nil {
		t.Fatal(err)
	}
	chats := chatsFromCopy(t, filepath.Join(copyRoot, "projects"))
	var drip, other []placegraph.TabChat
	for _, chat := range chats {
		switch topicOf(chat.Title) {
		case "drip":
			drip = append(drip, chat)
		case "other":
			other = append(other, chat)
		}
	}
	if len(drip) < 5 || len(other) < 3 {
		t.Fatalf("copied corpus has %d drip chats and %d unrelated chats", len(drip), len(other))
	}
	shown := append(append([]placegraph.TabChat{}, drip[:5]...), other[:3]...)
	dripIDs := map[string]bool{}
	for _, chat := range drip[:5] {
		dripIDs[chat.ID] = true
	}
	otherIDs := map[string]bool{}
	for _, chat := range other[:3] {
		otherIDs[chat.ID] = true
	}

	profile := t.TempDir()
	for _, role := range config.DesktopRoles() {
		if err := config.WriteDesktopRole(profile, role.ID, liveTabGroupModel, ""); err != nil {
			t.Fatal(err)
		}
		model, _, _ := config.DesktopRoleChoice(profile, role.ID)
		if model != liveTabGroupModel {
			t.Fatalf("role %s is pinned to %q", role.ID, model)
		}
	}
	journal := filepath.Join(t.TempDir(), "aabbccddeeff0011", "transcript.jsonl")
	if err := os.MkdirAll(filepath.Dir(journal), 0o700); err != nil {
		t.Fatal(err)
	}
	agent, err := New(Config{
		Workspace:   t.TempDir(),
		SessionFile: journal,
		Model:       liveTabGroupModel,
		APIKey:      key,
		BaseURL:     "https://openrouter.ai/api/v1",
		OneModel:    true,
		RolesSource: config.DesktopRolesSource(profile),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { agent.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	question := placegraph.TopicQuestion(shown)
	if question.Role != roles.RolePlaceSuggest {
		t.Fatalf("role %s", question.Role)
	}
	answer, err := agent.AskPlaces(ctx, question)
	t.Logf("placesuggest on %q:\n%s\n→ %q (err %v)", answer.Model, question.User, answer.Text, err)
	if err != nil {
		t.Fatalf("the organizing call failed: %v", err)
	}
	if answer.Model != liveTabGroupModel {
		t.Fatalf("answered on %q", answer.Model)
	}
	offer, err := placegraph.ReadTopicAnswer(answer.Text, shown)
	if err != nil || offer == nil {
		t.Fatalf("no honest topic offer: %v", err)
	}
	if len(offer.IDs) < 3 || offer.Title == "" {
		t.Fatalf("offer %+v", offer)
	}
	for _, id := range offer.IDs {
		if otherIDs[id] || !dripIDs[id] {
			t.Fatalf("offer left the drip chats: %+v", offer)
		}
	}
	lines := journalUsageLines(t, journal)
	if len(lines) == 0 {
		t.Fatal("the journal has no usage line for the organizing call")
	}
	saw := false
	for _, line := range lines {
		if line.Model != liveTabGroupModel {
			t.Fatalf("journal model %q", line.Model)
		}
		if line.Aux && line.Role == string(roles.RolePlaceSuggest) {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("journal lines did not record an aux %s call: %+v", roles.RolePlaceSuggest, lines)
	}
}

func topicOf(title string) string {
	text := strings.ToLower(title)
	switch {
	case strings.Contains(text, "drip") || strings.Contains(text, "tubing") || strings.Contains(text, "emitter"):
		return "drip"
	case strings.Contains(text, "buttermilk") || strings.Contains(text, "roth") || strings.Contains(text, "stretch"):
		return "other"
	default:
		return ""
	}
}

func chatsFromCopy(t *testing.T, root string) []placegraph.TabChat {
	t.Helper()
	var chats []placegraph.TabChat
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || info.Name() != "meta.json" {
			return err
		}
		meta, err := LoadMeta(filepath.Dir(path))
		if err != nil || meta.ID == "" {
			return err
		}
		chat := placegraph.TabChat{ID: meta.ID, Title: meta.Title, Workspace: meta.Workspace}
		if meta.Recap != nil {
			chat.Recap = meta.Recap.Line
		}
		chats = append(chats, chat)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return chats
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		return copyOneFile(path, target)
	})
}

func copyOneFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
