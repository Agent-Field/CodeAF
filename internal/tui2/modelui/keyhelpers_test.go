package modelui

import (
	"image"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// Key and mouse constructors, spelled the way internal/tui2/palette and the
// composer's own tests spell them, so a reader moving between packages does not
// have to learn a second dialect.

func typeRune(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

func namedKey(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

func ctrlKey(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }

func typeText(p *Picker, s string) {
	for _, r := range s {
		p.Key(typeRune(r))
	}
}

func clickAt(y int) (tea.MouseMsg, image.Point) {
	return tea.MouseClickMsg{Button: tea.MouseLeft, X: 0, Y: y}, image.Pt(0, y)
}

func wheelAt(button tea.MouseButton) (tea.MouseMsg, image.Point) {
	return tea.MouseWheelMsg{Button: button}, image.Pt(0, 0)
}

// drain runs a command and returns the message it produced, or nil.
func drain(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

// lines renders and splits, which is what every layout assertion here wants.
func lines(p *Picker, width, height int) []string {
	out := p.Render(width, height)
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// find returns the first rendered line containing want, and whether there was
// one. It reports the whole frame on failure through the caller's t.
func find(rendered []string, want string) (string, bool) {
	for _, line := range rendered {
		if strings.Contains(line, want) {
			return line, true
		}
	}
	return "", false
}

// sampleCatalog is a machine with three roles bound, one boosted, and a small
// model catalog. It is the fixture every picker test starts from.
func sampleCatalog() Catalog {
	return Catalog{
		Scope: store.TaskScope("t-1"),
		Roles: []RoleRow{
			{Role: store.RoleOrchestrate, Model: "anthropic/claude-sonnet-4", Source: store.RoleFromGlobal, Used: 10_000, Window: 200_000},
			{Role: store.RoleWork, Model: "openai/gpt-oss-120b", Source: store.RoleFromTask, BoundHere: true, Boosted: true},
			{Role: store.RoleScribe, Model: "openai/gpt-oss-20b:low", Source: store.RoleFromDefault},
		},
		Models: []ModelOption{
			{Slug: "anthropic/claude-sonnet-4", Window: 200_000, Note: "mid tier"},
			{Slug: "openai/gpt-oss-120b", Window: 131_072, Note: "cheap, fast"},
			{Slug: "openai/gpt-oss-20b", Window: 131_072, Note: "ultra cheap"},
			{Slug: "qwen/qwen3.5-9b", Window: 32_768, Note: "local"},
		},
	}
}

func newPicker(c Catalog) *Picker {
	p := New(Options{})
	p.SetCatalog(c)
	return p
}

// openRole walks the picker to one role's models the way a user does.
func openRole(p *Picker, role store.ModelRole) {
	for i, r := range store.ModelRoles() {
		if r == role {
			p.cursor = i
			break
		}
	}
	p.Key(namedKey(tea.KeyEnter))
}
