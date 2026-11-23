package factory

import "testing"

func TestJobWritePermission(t *testing.T) {
	for _, tc := range []struct {
		name   string
		item   Item
		refuse bool
	}{
		{"personal", Item{}, false},
		{"chat", Item{Origin: OriginChat}, false},
		{"forge", Item{Origin: OriginForge}, true},
		{"number", Item{Num: 1}, true},
		{"url", Item{URL: "https://example.test/job/1"}, true},
		{"stranger", Item{Tier: TierStranger}, true},
		{"owner", Item{Origin: OriginForge, Tier: TierOwner}, false},
		{"collaborator", Item{Num: 1, Tier: TierCollab}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.item.RefusesWrite(); got != tc.refuse {
				t.Fatalf("refuses write=%v want %v", got, tc.refuse)
			}
		})
	}
}
