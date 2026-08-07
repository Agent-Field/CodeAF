package tui

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/charmbracelet/x/ansi"
)

type workspaceFixtureCommander struct {
	*fakeCommander
	workspace string
}

func (c *workspaceFixtureCommander) ResolveWorkspacePath(_ string, relative string) (string, bool) {
	if filepath.IsAbs(relative) {
		return "", false
	}
	target := filepath.Join(c.workspace, filepath.Clean(relative))
	inside, err := filepath.Rel(c.workspace, target)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(os.PathSeparator)) {
		return "", false
	}
	info, err := os.Stat(target)
	return target, err == nil && !info.IsDir()
}

func (c *workspaceFixtureCommander) ResolveMediaPath(nodeID, relative string) (string, bool) {
	return c.ResolveWorkspacePath(nodeID, relative)
}

func (c *workspaceFixtureCommander) WorkspacePath(_ string) (string, bool) {
	info, err := os.Stat(c.workspace)
	return c.workspace, err == nil && info.IsDir()
}

func workspaceFixture(t *testing.T) (*workspaceFixtureCommander, string) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "agentfield_twitter_ad.md")
	if err := os.WriteFile(file, []byte("deliverable"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "campaign brief.md"), []byte("spaced"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &workspaceFixtureCommander{
		fakeCommander: &fakeCommander{current: map[string]string{}}, workspace: dir,
	}, file
}

func TestWorkspaceDeliverableLinkificationChecksExistenceAndPreservesText(t *testing.T) {
	commander, file := workspaceFixture(t)
	model := NewWithCommander(&fakeBackend{}, "links", commander)
	body := "saved agentfield_twitter_ad.md in the workspace; missing.md stayed plain."
	linked := model.linkWorkspaceReferences("job", body)
	if got := ansi.Strip(linked); got != body {
		t.Fatalf("visible text changed:\n got %q\nwant %q", got, body)
	}
	wantURL := (&url.URL{Scheme: "file", Path: file}).String()
	if !strings.Contains(linked, wantURL) {
		t.Fatalf("existing deliverable was not linked: %q", linked)
	}
	if strings.Contains(linked, "missing.md\x1b]8") || strings.Count(linked, "\x1b]8;;") != 2 {
		t.Fatalf("nonexistent path was linked or OSC8 pair malformed: %q", linked)
	}
	spaced := model.linkWorkspaceReferences("job", "saved `campaign brief.md` too")
	if ansi.Strip(spaced) != "saved `campaign brief.md` too" || !strings.Contains(spaced, "campaign%20brief.md") {
		t.Fatalf("quoted relative path with spaces was not linked safely: %q", spaced)
	}

	message := store.Message{Seq: 4, Role: store.RoleAgent, NodeID: "job", Body: body}
	rendered := model.renderAnswer(message, 100)
	if !strings.Contains(rendered, wantURL) || !strings.Contains(ansi.Strip(rendered), body) {
		t.Fatalf("agent message did not preserve linked deliverable: %q", rendered)
	}
}

func TestSettledExpandedCardAddsWorkspaceDirectoryLinkOnlyWhenExpanded(t *testing.T) {
	commander, file := workspaceFixture(t)
	model := NewWithCommander(&fakeBackend{}, "workspace-card", commander)
	card := jobCard{
		ID: "job", RootID: "job", State: cardSettled, Title: "Write social ad",
		Outcome: "saved agentfield_twitter_ad.md",
		Deliverable: &store.Message{
			Seq: 7, Role: store.RoleSystem, NodeID: "job",
			Body: "saved agentfield_twitter_ad.md in the workspace",
		},
		Done: 1, Total: 1,
	}
	collapsed := model.renderJobCard(card, 100, false, 0, false, false)
	if strings.Contains(ansi.Strip(collapsed), "▸ workspace") {
		t.Fatalf("collapsed card gained workspace chrome: %q", collapsed)
	}
	expanded := model.renderJobCard(card, 100, true, 0, false, false)
	wantWorkspaceURL := (&url.URL{Scheme: "file", Path: commander.workspace}).String()
	wantFileURL := (&url.URL{Scheme: "file", Path: file}).String()
	if !strings.Contains(ansi.Strip(expanded), "▸ workspace") || !strings.Contains(expanded, wantWorkspaceURL) {
		t.Fatalf("expanded card workspace link missing or wrong: %q", expanded)
	}
	if !strings.Contains(expanded, wantFileURL) || !strings.Contains(ansi.Strip(expanded), card.Outcome) {
		t.Fatalf("settled summary deliverable link missing or changed: %q", expanded)
	}
	lines := strings.Split(ansi.Strip(expanded), "\n")
	if !strings.Contains(lines[len(lines)-1], "▸ workspace") {
		t.Fatalf("workspace affordance is not the final card line: %q", lines[len(lines)-1])
	}
}
