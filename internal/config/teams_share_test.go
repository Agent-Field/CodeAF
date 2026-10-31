package config

import (
	"os"
	"path/filepath"
	"testing"
)

// F6 keeps absent choices on and fails safe when a saved preference is damaged.
func TestTeamSharingPreferenceFailsSafe(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  bool
	}{
		{name: "missing", want: true},
		{name: "empty", value: "", want: true},
		{name: "on", value: "on", want: true},
		{name: "off", value: "off", want: false},
		{name: "invalid", value: "maybe", want: false},
		{name: "wrong type", value: false, want: false},
		{name: "null", value: (*string)(nil), want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]any{}
			if tc.value != nil {
				values[KeyTeamsShare] = tc.value
			}
			dir := profileDirWith(t, values)
			if got := TeamsShareAt(dir); got != tc.want {
				t.Fatalf("TeamsShareAt = %v, want %v", got, tc.want)
			}
			row, ok := registry(t, dir).Row(KeyTeamsShare)
			if !ok {
				t.Fatal("missing team sharing setting")
			}
			want := "on"
			if !tc.want {
				want = "off"
			}
			if got := row.Value(); got != want {
				t.Fatalf("Settings value = %q, want %q", got, want)
			}
		})
	}
}

// C1, C2, C3 and C9 keep the default and immediate local choice consistent with Settings.
// F6 keeps the local preference and its Settings value consistent.
func TestTeamSharingIsLocalOnByDefaultAndChangesImmediately(t *testing.T) {
	dir := t.TempDir()
	rows := registry(t, dir)
	row, ok := rows.Row("teams.share")
	if !ok {
		t.Fatal("missing team sharing setting")
	}
	if !TeamsShareAt(dir) || row.Value() != "on" {
		t.Fatal("team sharing must start on")
	}
	presentation := row.ChatPresentation()
	if row.Category != CategoryInterface || presentation.Category != "General" || presentation.Scope != "This computer" || presentation.Activation != "Applies immediately" {
		t.Fatal("team sharing must be a local General setting", row.Category, presentation)
	}
	if err := row.Apply("on"); err != nil {
		t.Fatal(err)
	}
	if !TeamsShareAt(dir) || row.Value() != "on" {
		t.Fatal("setting did not turn on")
	}
	preferences, err := ReadPreferences(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := preferences[row.Key]; ok || PreferenceKey(row.Key) {
		t.Fatal("team sharing must stay local")
	}
	if err := ApplyPreferences(dir, map[string]string{row.Key: `"off"`}); err != nil {
		t.Fatal(err)
	}
	if !TeamsShareAt(dir) || row.Value() != "on" {
		t.Fatal("remote preferences changed the local switch")
	}
	if err := row.Apply("off"); err != nil {
		t.Fatal(err)
	}
	if TeamsShareAt(dir) || row.Value() != "off" {
		t.Fatal("setting did not turn off")
	}
	if err := row.Apply("maybe"); err == nil {
		t.Fatal("invalid choice accepted")
	}
	if TeamsShareAt(dir) || row.Value() != "off" {
		t.Fatal("invalid choice changed the local switch")
	}
	if err := ApplyPreferences(dir, map[string]string{row.Key: `"on"`}); err != nil {
		t.Fatal(err)
	}
	if TeamsShareAt(dir) || row.Value() != "off" {
		t.Fatal("remote preferences enabled the local switch")
	}
	if err := row.Apply("on"); err != nil {
		t.Fatal(err)
	}
	if !TeamsShareAt(dir) || row.Value() != "on" {
		t.Fatal("setting did not turn back on")
	}
}

// F6 makes an unreadable configuration resolve to off in both the reader and Settings.
func TestTeamSharingUnreadablePreferenceFailsSafe(t *testing.T) {
	for _, raw := range []string{`{"teams.share":"off"`, `null`, `[]`} {
		t.Run(raw, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			if TeamsShareAt(dir) {
				t.Fatal("damaged config enabled sharing")
			}
			row, _ := registry(t, dir).Row(KeyTeamsShare)
			if row.Value() != "off" {
				t.Fatal("Settings did not show off", row.Value())
			}
		})
	}
	t.Run("unreadable file", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "config.json"), 0700); err != nil {
			t.Fatal(err)
		}
		if TeamsShareAt(dir) {
			t.Fatal("unreadable config enabled sharing")
		}
		row, _ := registry(t, dir).Row(KeyTeamsShare)
		if row.Value() != "off" {
			t.Fatal("Settings did not show off", row.Value())
		}
	})
}

// F6 does not keep a cached on after the configuration becomes unreadable.
func TestTeamSharingCachedOnFailsSafeAfterReadPermissionIsRemoved(t *testing.T) {
	dir := profileDirWith(t, map[string]any{KeyTeamsShare: "on"})
	if !TeamsShareAt(dir) {
		t.Fatal("fixture did not warm on")
	}
	path := BudgetConfigPath(dir)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0600)
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("this process can read files without read permissions")
	}
	if TeamsShareAt(dir) {
		t.Fatal("cached on survived an unreadable configuration")
	}
	row, _ := registry(t, dir).Row(KeyTeamsShare)
	if row.Value() != "off" {
		t.Fatal("Settings kept cached on", row.Value())
	}
}
