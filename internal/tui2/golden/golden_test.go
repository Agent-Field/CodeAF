package golden_test

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/golden"
	"github.com/charmbracelet/x/ansi"
)

func TestReferenceViewRun(t *testing.T) {
	golden.Run(t, "reference", referenceView)
}

func TestReferenceViewRunSizes(t *testing.T) {
	golden.RunSizes(t, "reference-critical", referenceView, golden.DefaultCriticalSizes, []golden.Theme{
		{Mode: golden.Dark, Calm: true},
		{Mode: golden.Light, Calm: true},
	})
}

func TestANSIStyleDoesNotConsumeVisibleWidth(t *testing.T) {
	styled := func(_, height int, _ golden.Theme) []string {
		lines := make([]string, height)
		lines[0] = "\x1b[31m界\x1b[0m"
		return lines
	}
	golden.RunSizes(t, "ansi-width", styled,
		[]golden.Size{{Width: 2, Height: 1}},
		[]golden.Theme{{Mode: golden.Dark}})
}

func TestReferenceViewSnapshots(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		height int
		theme  golden.Theme
	}{
		{name: "one-cell", width: 1, height: 1, theme: golden.Theme{Mode: golden.Dark}},
		{name: "two-cells", width: 2, height: 2, theme: golden.Theme{Mode: golden.Light}},
		{name: "ordinary-dark", width: 20, height: 5, theme: golden.Theme{Mode: golden.Dark}},
		{name: "ordinary-light", width: 20, height: 5, theme: golden.Theme{Mode: golden.Light}},
		{name: "calm", width: 10, height: 3, theme: golden.Theme{Mode: golden.Dark, Calm: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			golden.Snap(t, "reference", referenceView, test.width, test.height, test.theme)
		})
	}
}

func TestHarnessRejectsBrokenViews(t *testing.T) {
	const helperEnvironment = "AFORGE_GOLDEN_BROKEN_VIEW"
	if broken := os.Getenv(helperEnvironment); broken != "" {
		view, ok := brokenViews[broken]
		if !ok {
			t.Fatalf("unknown broken-view helper %q", broken)
		}
		t.Run(broken, func(t *testing.T) {
			golden.RunSizes(t, "broken-"+broken, view,
				[]golden.Size{{Width: 3, Height: 2}},
				[]golden.Theme{{Mode: golden.Dark}})
		})
		return
	}

	tests := []struct {
		name string
		want string
	}{
		{name: "overflow", want: "line 1 has visible width 4; viewport width is 3"},
		{name: "wrong-line-count", want: "returned 1 lines; want exactly 2"},
		{name: "raw-control", want: "line 1 contains raw control bytes or non-SGR escape sequences"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run=^TestHarnessRejectsBrokenViews$", "-test.v")
			command.Env = append(os.Environ(), helperEnvironment+"="+test.name)
			output, err := command.CombinedOutput()
			if err == nil {
				t.Fatalf("broken view unexpectedly passed:\n%s", output)
			}
			text := string(output)
			if !strings.Contains(text, "--- FAIL: TestHarnessRejectsBrokenViews/"+test.name) {
				t.Errorf("broken view did not cause a real failing subtest:\n%s", text)
			}
			if !strings.Contains(text, test.want) {
				t.Errorf("failure did not contain %q:\n%s", test.want, text)
			}
		})
	}
}

var brokenViews = map[string]golden.View{
	"overflow": func(_, height int, _ golden.Theme) []string {
		lines := make([]string, height)
		lines[0] = "xxxx"
		return lines
	},
	"wrong-line-count": func(_, _ int, _ golden.Theme) []string {
		return []string{"only one"}
	},
	"raw-control": func(_, height int, _ golden.Theme) []string {
		lines := make([]string, height)
		lines[0] = "bell\x07"
		return lines
	},
}

func referenceView(width, height int, theme golden.Theme) []string {
	lines := make([]string, height)
	if width == 1 {
		lines[0] = "╷"
		for index := 1; index < height-1; index++ {
			lines[index] = "│"
		}
		if height > 1 {
			lines[height-1] = "╵"
		}
		return lines
	}

	title := fmt.Sprintf(" %s · reference ", theme.Mode)
	if theme.Calm {
		title = fmt.Sprintf(" %s · calm reference ", theme.Mode)
	}
	innerWidth := width - 2
	lines[0] = "╭" + titleLine(title, innerWidth) + "╮"
	for index := 1; index < height-1; index++ {
		lines[index] = "│" + strings.Repeat(" ", innerWidth) + "│"
	}
	if height > 1 {
		lines[height-1] = "╰" + strings.Repeat("─", innerWidth) + "╯"
	}
	return lines
}

func titleLine(title string, width int) string {
	if ansi.StringWidth(title) <= width {
		return title + strings.Repeat("─", width-ansi.StringWidth(title))
	}
	if width == 0 {
		return ""
	}
	if width == 1 {
		return "…"
	}

	var clipped strings.Builder
	used := 0
	for _, character := range title {
		characterWidth := ansi.StringWidth(string(character))
		if used+characterWidth > width-1 {
			break
		}
		clipped.WriteRune(character)
		used += characterWidth
	}
	clipped.WriteRune('…')
	return clipped.String()
}
