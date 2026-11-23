package pair_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// Each channel prints pairing links on its own hosted origin, treats links from
// another channel as a foreign service, and still obeys a self-hosted Via.
func TestPairingLinksFollowTheReleaseChannel(t *testing.T) {
	key, err := pair.NewLinkKey()
	if err != nil {
		t.Fatal(err)
	}
	ref := pair.LinkRef{Code: "k7m2q9xd", Key: key}
	cases := []struct {
		name     string
		revision string
		want     string
		foreign  string
	}{
		{"stable", "v0.3.0", syncsetup.LinkOriginOf(syncsetup.ProductionRelayURL), syncsetup.DevRelayURL},
		{"rc", "v0.3.0-rc.1", syncsetup.LinkOriginOf(syncsetup.ProductionRelayURL), syncsetup.StagingRelayURL},
		{"staging", "staging-20261030-abcdef012345", syncsetup.StagingRelayURL, syncsetup.LinkOriginOf(syncsetup.ProductionRelayURL)},
		{"dev", "dev-20261030-abcdef012345", syncsetup.DevRelayURL, syncsetup.LinkOriginOf(syncsetup.ProductionRelayURL)},
		{"untagged", "deadbeef", syncsetup.DevRelayURL, syncsetup.LinkOriginOf(syncsetup.ProductionRelayURL)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origin := syncsetup.HostedLinkOriginFor(tc.revision)
			if origin != tc.want {
				t.Fatalf("HostedLinkOriginFor(%q) = %q, want %q", tc.revision, origin, tc.want)
			}
			was := pair.LinkOrigin
			pair.LinkOrigin = origin
			t.Cleanup(func() { pair.LinkOrigin = was })

			if !strings.HasPrefix(ref.URL(), origin+"/p/"+ref.Code+"#") {
				t.Fatalf("URL() = %q, want prefix %q", ref.URL(), origin+"/p/"+ref.Code+"#")
			}
			lines := pair.InviteLines(pair.Invite{Ref: ref, Check: "4821", ExpiresIn: 10 * time.Minute})
			if !strings.Contains(lines, ref.URL()) {
				t.Fatalf("InviteLines lacks channel link %q:\n%s", ref.URL(), lines)
			}
			if got := pair.LinkRelay(ref.URL()); got != "" {
				t.Fatalf("LinkRelay(%q) = %q, want empty for own channel", ref.URL(), got)
			}
			foreignLink := ref.URLAt(tc.foreign)
			if got := pair.LinkRelay(foreignLink); got != tc.foreign {
				t.Fatalf("LinkRelay(%q) = %q, want foreign origin %q", foreignLink, got, tc.foreign)
			}
			custom := pair.InviteLines(pair.Invite{Ref: ref, Via: "https://own.example/relay", Check: "4821", ExpiresIn: 10 * time.Minute})
			if strings.Contains(custom, origin) || !strings.Contains(custom, "https://own.example/relay/p/"+ref.Code) {
				t.Fatalf("custom Via did not override channel origin:\n%s", custom)
			}
		})
	}
}
