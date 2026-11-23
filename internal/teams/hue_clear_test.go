package teams

import "testing"

// A CLEARED COLOUR STAYS CLEARED ACROSS A LOAD, AND A SET COLOUR (HUE 0
// INCLUDED) COLOURS THE TEAM AGAIN.
func TestClearedHueSurvivesLoadHuedAndSetHueRestores(t *testing.T) {
	dir := t.TempDir()
	var id, other string
	err := Update(dir, func(f *File) error {
		a := Team{ID: NewID(), Name: "plain"}
		b := Team{ID: NewID(), Name: "never cleared"}
		a.SetHue(HueSpec{Hue: 200, Tier: 1})
		a.ClearHue()
		id, other = a.ID, b.ID
		f.Teams = append(f.Teams, a, b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		f, err := LoadHued(dir, reservedForTest)
		if err != nil {
			t.Fatal(err)
		}
		a, b := f.Teams[Index(f.Teams, id)], f.Teams[Index(f.Teams, other)]
		if a.Hued() || !a.HueCleared() || a.Hue != 0 || a.Tier != 0 {
			t.Fatalf("load %d: cleared team = %+v", i, a)
		}
		if !b.Hued() {
			t.Fatalf("load %d: a team never cleared must still be coloured: %+v", i, b)
		}
	}
	err = Update(dir, func(f *File) error {
		f.Teams[Index(f.Teams, id)].SetHue(HueSpec{Hue: 0})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	f, err := LoadHued(dir, reservedForTest)
	if err != nil {
		t.Fatal(err)
	}
	if a := f.Teams[Index(f.Teams, id)]; !a.Hued() || a.HueCleared() || a.Hue != 0 {
		t.Fatalf("hue 0 after a clear = %+v", a)
	}
}
