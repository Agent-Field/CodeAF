// Package golden provides text-render regression tests for terminal UIs.
// It deliberately depends on neither Bubble Tea nor a concrete UI package:
// callers adapt their renderer to View and can therefore exercise the same
// checks at unit-test speed.
package golden

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/sanitize"
	"github.com/charmbracelet/x/ansi"
)

const (
	defaultFailureDetailLimit = 3
	maxSummaryEntries         = 64
)

var update = flag.Bool("update", false, "update golden test files")

// Mode identifies a terminal color theme.
type Mode uint8

const (
	// Dark is a theme intended for a dark terminal background.
	Dark Mode = iota
	// Light is a theme intended for a light terminal background.
	Light
)

// String returns the stable lowercase mode name.
func (mode Mode) String() string {
	switch mode {
	case Dark:
		return "dark"
	case Light:
		return "light"
	default:
		return fmt.Sprintf("mode-%d", mode)
	}
}

// Theme describes the render environment. Calm requests reduced motion;
// renderers should use it to select a stable frame for animated elements.
type Theme struct {
	Mode Mode
	Calm bool
}

// String returns the stable name used in diagnostics and golden filenames.
func (theme Theme) String() string {
	name := theme.Mode.String()
	if theme.Calm {
		name += "-calm"
	}
	return name
}

// View renders exactly height terminal rows, each no wider than width cells.
// SGR styling is permitted; terminal control sequences with side effects are
// not. A View should be deterministic for a given set of arguments.
type View func(width, height int, theme Theme) []string

// Size is a terminal viewport in cells.
type Size struct {
	Width  int
	Height int
}

// DefaultCriticalSizes covers the smallest viable viewports, common terminal
// sizes, and both sides of the 80- and 100-column rail breakpoints.
var DefaultCriticalSizes = []Size{
	{Width: 1, Height: 1},
	{Width: 2, Height: 2},
	{Width: 20, Height: 5},
	{Width: 40, Height: 10},
	{Width: 60, Height: 15},
	{Width: 79, Height: 20},
	{Width: 80, Height: 20},
	{Width: 81, Height: 20},
	{Width: 99, Height: 20},
	{Width: 100, Height: 20},
	{Width: 101, Height: 20},
	{Width: 110, Height: 20},
}

// Option configures Run.
type Option func(*runConfig)

// WithThemes replaces Run's default dark and light themes. It is useful for
// exercising calm-mode frames or an intentionally narrower theme matrix.
func WithThemes(themes ...Theme) Option {
	copyOfThemes := append([]Theme(nil), themes...)
	return func(config *runConfig) {
		config.themes = append([]Theme(nil), copyOfThemes...)
	}
}

// WithFailureDetailLimit changes how many failing renders Run describes in
// detail. Remaining failures are still counted and listed compactly.
func WithFailureDetailLimit(limit int) Option {
	return func(config *runConfig) {
		config.failureDetailLimit = limit
	}
}

type runConfig struct {
	themes             []Theme
	failureDetailLimit int
}

// Run renders v at all 4,400 combinations of widths 1..110, heights 1..20,
// and the dark and light themes. Structural checks run for every combination,
// even when earlier combinations fail.
func Run(t *testing.T, name string, v View, opts ...Option) {
	t.Helper()

	config := runConfig{
		themes: []Theme{
			{Mode: Dark},
			{Mode: Light},
		},
		failureDetailLimit: defaultFailureDetailLimit,
	}
	for index, option := range opts {
		if option == nil {
			t.Errorf("golden %q: option %d is nil", name, index)
			return
		}
		option(&config)
	}
	if err := validateRunConfig(config); err != nil {
		t.Errorf("golden %q: %v", name, err)
		return
	}

	failures := make([]renderFailure, 0)
	for _, theme := range config.themes {
		for width := 1; width <= 110; width++ {
			for height := 1; height <= 20; height++ {
				size := Size{Width: width, Height: height}
				if problems := inspect(v, size, theme); len(problems) != 0 {
					failures = append(failures, renderFailure{
						size:     size,
						theme:    theme,
						problems: problems,
					})
				}
			}
		}
	}
	reportFailures(t, name, failures, config.failureDetailLimit)
}

// RunSizes applies the same structural checks as Run to a curated size and
// theme matrix. It is intended for views whose rendering is unusually costly.
func RunSizes(t *testing.T, name string, v View, sizes []Size, themes []Theme) {
	t.Helper()
	if len(sizes) == 0 {
		t.Errorf("golden %q: sizes must not be empty", name)
		return
	}
	if len(themes) == 0 {
		t.Errorf("golden %q: themes must not be empty", name)
		return
	}
	for index, size := range sizes {
		if size.Width < 1 || size.Height < 1 {
			t.Errorf("golden %q: size %d is invalid: %dx%d", name, index, size.Width, size.Height)
			return
		}
	}
	for index, theme := range themes {
		if !validTheme(theme) {
			t.Errorf("golden %q: theme %d is invalid: %s", name, index, theme)
			return
		}
	}

	failures := make([]renderFailure, 0)
	for _, theme := range themes {
		for _, size := range sizes {
			if problems := inspect(v, size, theme); len(problems) != 0 {
				failures = append(failures, renderFailure{
					size:     size,
					theme:    theme,
					problems: problems,
				})
			}
		}
	}
	reportFailures(t, name, failures, defaultFailureDetailLimit)
}

// Snap checks one render and compares it with
// testdata/golden/<name>/<width>x<height>-<theme>.txt. Passing -update to
// go test creates or replaces the file.
func Snap(t *testing.T, name string, v View, width, height int, theme Theme) {
	t.Helper()
	size := Size{Width: width, Height: height}
	if width < 1 || height < 1 {
		t.Errorf("golden %q: invalid snapshot size %dx%d", name, width, height)
		return
	}
	if !validTheme(theme) {
		t.Errorf("golden %q: invalid snapshot theme %s", name, theme)
		return
	}
	path, err := snapshotPath(name, size, theme)
	if err != nil {
		t.Errorf("golden %q: %v", name, err)
		return
	}

	lines, problems := render(v, size, theme)
	if len(problems) != 0 {
		reportFailures(t, name, []renderFailure{{size: size, theme: theme, problems: problems}}, 1)
		return
	}
	got := encode(lines)
	if *update {
		if err := writeSnapshot(path, got); err != nil {
			t.Errorf("golden %q: update %s: %v", name, path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			t.Errorf("golden %q: snapshot %s does not exist; run go test -update", name, path)
		} else {
			t.Errorf("golden %q: read snapshot %s: %v", name, path, err)
		}
		return
	}
	if string(want) == got {
		return
	}

	line, column := firstDifference(string(want), got)
	t.Errorf("golden %q mismatch at %dx%d-%s; first difference at line %d, column %d\n%s",
		name, width, height, theme, line, column, unifiedDiff(path, string(want), got))
}

func validateRunConfig(config runConfig) error {
	if len(config.themes) == 0 {
		return fmt.Errorf("themes must not be empty")
	}
	for index, theme := range config.themes {
		if !validTheme(theme) {
			return fmt.Errorf("theme %d is invalid: %s", index, theme)
		}
	}
	if config.failureDetailLimit < 1 {
		return fmt.Errorf("failure detail limit must be at least 1")
	}
	return nil
}

func validTheme(theme Theme) bool {
	return theme.Mode == Dark || theme.Mode == Light
}

type renderFailure struct {
	size     Size
	theme    Theme
	problems []string
}

func inspect(v View, size Size, theme Theme) []string {
	_, problems := render(v, size, theme)
	return problems
}

func render(v View, size Size, theme Theme) (lines []string, problems []string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			lines = nil
			problems = []string{fmt.Sprintf("panic: %v", recovered)}
		}
	}()

	lines = v(size.Width, size.Height, theme)
	if len(lines) != size.Height {
		problems = append(problems, fmt.Sprintf("returned %d lines; want exactly %d", len(lines), size.Height))
	}
	for index, line := range lines {
		if clean := sanitize.Text(line); clean != line {
			problems = append(problems,
				fmt.Sprintf("line %d contains raw control bytes or non-SGR escape sequences: %q", index+1, line))
		}
		plain := ansi.Strip(line)
		if width := ansi.StringWidth(plain); width > size.Width {
			problems = append(problems,
				fmt.Sprintf("line %d has visible width %d; viewport width is %d: %q", index+1, width, size.Width, plain))
		}
		// UNSTYLED trailing spaces are not content, and a snapshot is a picture
		// of content. The compositor pads every row of a frame out to the
		// frame's width, because a row that stops early says nothing about the
		// cells past the cut and the terminal keeps whatever the last frame left
		// there (see tui2.padFrame). That padding is the compositor's contract
		// with the TERMINAL, it is pinned by the compositor's own test, and
		// recording it here would spend forty cells of every reference line
		// saying "blank" — which is exactly the noise a golden must not carry if
		// a human is to read its diff.
		//
		// Only bare spaces go. A row that ends inside a selection band ends with
		// its reset sequence AFTER the padding it painted, so the band's own
		// cells are not trailing spaces and survive — which is right, because a
		// painted ground IS content.
		lines[index] = strings.TrimRight(line, " ")
	}
	return lines, problems
}

func reportFailures(t *testing.T, name string, failures []renderFailure, detailLimit int) {
	t.Helper()
	if len(failures) == 0 {
		return
	}
	if detailLimit > len(failures) {
		detailLimit = len(failures)
	}

	var message strings.Builder
	fmt.Fprintf(&message, "golden %q: %d render(s) failed structural checks", name, len(failures))
	for index := 0; index < detailLimit; index++ {
		failure := failures[index]
		fmt.Fprintf(&message, "\n  %dx%d-%s:", failure.size.Width, failure.size.Height, failure.theme)
		for _, problem := range failure.problems {
			fmt.Fprintf(&message, "\n    - %s", problem)
		}
	}
	if len(failures) > detailLimit {
		remaining := failures[detailLimit:]
		fmt.Fprintf(&message, "\n  %d additional failing render(s): ", len(remaining))
		listed := len(remaining)
		if listed > maxSummaryEntries {
			listed = maxSummaryEntries
		}
		for index := 0; index < listed; index++ {
			if index != 0 {
				message.WriteString(", ")
			}
			failure := remaining[index]
			fmt.Fprintf(&message, "%dx%d-%s", failure.size.Width, failure.size.Height, failure.theme)
		}
		if unlisted := len(remaining) - listed; unlisted > 0 {
			fmt.Fprintf(&message, ", … (+%d more)", unlisted)
		}
	}
	t.Error(message.String())
}

func snapshotPath(name string, size Size, theme Theme) (string, error) {
	if name == "" {
		return "", fmt.Errorf("snapshot name must not be empty")
	}
	clean := filepath.Clean(name)
	if filepath.IsAbs(name) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("snapshot name must be a relative path below testdata/golden: %q", name)
	}
	filename := fmt.Sprintf("%dx%d-%s.txt", size.Width, size.Height, theme)
	return filepath.Join("testdata", "golden", clean, filename), nil
}

func encode(lines []string) string {
	return strings.Join(lines, "\n") + "\n"
}

func writeSnapshot(path, contents string) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".golden-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.WriteString(contents); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
