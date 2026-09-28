package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/modelsource"
)

func TestProviderRefreshFailureStaysVisibleBesideCachedModels(t *testing.T) {
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "custom"}[custom], func(t *testing.T) {
			sources := modelsource.NewSet(testDefaultService("synthetic"))
			id := modelsource.DefaultID
			if custom {
				id = "custom-invoice"
				sources = modelsource.NewSet(sources.Default(), modelsource.Connected{Source: modelsource.Source{ID: id, Written: "invoice", Listing: modelsource.ListingModels}, Address: "https://example.invalid"})
			}
			a := modelServiceTestApp(t, t.TempDir(), "cached-model", sources, []Model{{ID: "cached-model"}})
			a.modelsForService = func(modelsource.Connected) []Model { return []Model{{ID: "cached-model"}} }
			failure := "HTTP 503: catalog temporarily unavailable"
			a.providerFetchError = func(source string) string {
				if source == id {
					return failure
				}
				return ""
			}
			rows := a.modelPickerList()
			notices, cached := 0, 0
			for _, r := range rows {
				if r.Notice != "" && strings.Contains(r.Notice, failure) {
					notices++
					if !r.Unavailable {
						t.Fatal("failure row became selectable")
					}
				}
				if strings.HasSuffix(r.ID, "cached-model") && !r.Unavailable {
					cached++
				}
			}
			if notices != 1 || cached == 0 {
				t.Fatalf("failure hid error or cached choice: %+v", rows)
			}
			a.openPicker()
			rendered := plain(strings.Join(a.pick.rows(140, a.pick.height(140), a.pal, -1, func(string) string { return "" }), "\n"))
			if !strings.Contains(rendered, "catalog temporarily unavailable") || !strings.Contains(rendered, "cached-model") {
				t.Fatalf("picker lost visible refusal or usable cache:\n%s", rendered)
			}
			for _, order := range []tableSort{{back: true}, {at: 1}, {at: 2, back: true}} {
				a.pick.sort = order
				a.pick.filter.setText("cached-model")
				a.pick.rank()
				found := false
				for _, at := range a.pick.hits {
					row := a.pick.all[at]
					if strings.Contains(row.Notice, failure) {
						found = true
						break
					}
					if row.GroupOrder == map[bool]int{false: 0, true: 1}[custom] && !row.Unavailable {
						t.Fatalf("sort %+v moved cached model ahead of provider refusal", order)
					}
				}
				if !found {
					t.Fatalf("sort %+v or filter hid provider refusal", order)
				}
			}
			failure = ""
			for _, r := range a.modelPickerList() {
				if r.Notice != "" {
					t.Fatalf("successful retry retained refusal: %+v", r)
				}
			}
		})
	}
}
