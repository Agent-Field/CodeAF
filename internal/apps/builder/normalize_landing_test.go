package builder

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func TestSavedV10BriefsGetOverviewAgendaAndWorkQueue(t *testing.T) {
	for name, work := range map[string]string{"ae-account-desk": "deal", "daily-brief-and-commitments": "message"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile("testdata/v10-landings/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var files map[string]string
			if err := json.Unmarshal(raw, &files); err != nil {
				t.Fatal(err)
			}
			app, err := schema.ParseApp([]byte(files[schema.AppFile]))
			if err != nil {
				t.Fatal(err)
			}
			plan, err := schema.ParsePlan([]byte(files[schema.PlanFile]))
			if err != nil {
				t.Fatal(err)
			}
			path := "screens/" + plan.Screens[0].ID + ".json"
			// The saved data offers meetings even when their dedicated
			// route is missing, as happens in the compact Designer.
			for i, line := range plan.Screens {
				if line.ID == "meetings" {
					plan.Screens = append(plan.Screens[:i], plan.Screens[i+1:]...)
					if err := saveJSON(files, schema.PlanFile, plan); err != nil {
						t.Fatal(err)
					}
					break
				}
			}
			notes, err := normalizeLanding(files, app, nil)
			if err != nil {
				t.Fatal(err)
			}
			screen, err := schema.ParseScreen([]byte(files[path]))
			if err != nil || len(notes) != 1 || !nodeHasID(screen.Body, "brief-overview") ||
				!nodeHasID(screen.Body, "detail") || screen.Views["work"].Type != work ||
				screen.Views["agenda"].Type != "meeting" || screen.Views["metric-2"].Type == "" {
				t.Fatalf("want priority queue, overview, agenda, and selection detail: %+v, %v (%v)", screen.Views, notes, err)
			}
		})
	}
}
