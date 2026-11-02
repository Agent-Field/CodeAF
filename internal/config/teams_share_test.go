package config

import "testing"

func TestTeamSharingIsLocalOffByDefaultAndChangesImmediately(t *testing.T) {
	dir := t.TempDir()
	rows := registry(t, dir)
	row, ok := rows.Row("teams.share")
	if !ok {
		t.Fatal("missing team sharing setting")
	}
	if row.Value() != "off" {
		t.Fatal("team sharing must start off")
	}
	presentation := row.ChatPresentation()
	if row.Category != CategoryInterface || presentation.Category != "General" || presentation.Scope != "This computer" || presentation.Activation != "Applies immediately" {
		t.Fatal("team sharing must be a local General setting", row.Category, presentation)
	}
	if err := row.Apply("on"); err != nil {
		t.Fatal(err)
	}
	if row.Value() != "on" {
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
	if row.Value() != "on" {
		t.Fatal("remote preferences changed the local switch")
	}
	if err := row.Apply("off"); err != nil {
		t.Fatal(err)
	}
	if row.Value() != "off" {
		t.Fatal("setting did not turn off")
	}
	if err := row.Apply("maybe"); err == nil {
		t.Fatal("invalid choice accepted")
	}
}
