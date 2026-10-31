package pair

import (
	"net/url"
	"testing"
)

// L4-R6: Equivalent URLs approve on this service, while meaningful differences refuse.
func TestL4FixCanonicalServices(t *testing.T) {
	if err := CheckLinkService("https://CODEAF.AGENTFIELD.AI:443/p/k7m2q9xd", "https://codeaf.agentfield.ai/fabric"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		via, own string
		equal    bool
	}{
		{"https://SYNC.EXAMPLE:443/fabric///", "https://sync.example/fabric", true},
		{"HTTPS://sync.example/fabric", "https://sync.example/fabric/", true},
		{"http://SYNC.EXAMPLE:80/", "http://sync.example", true},
		{"https://CODEAF.AGENTFIELD.AI:443/fabric/", "https://codeaf.agentfield.ai/fabric", true},
		{"https://sync.example", "https://elsewhere.example", false},
		{"https://sync.example:444", "https://sync.example", false},
		{"http://sync.example", "https://sync.example", false},
		{"https://sync.example/a", "https://sync.example/b", false},
	} {
		t.Run(tc.via+"_"+tc.own, func(t *testing.T) {
			link := "codeaf://pair/k7m2q9xd?via=" + url.QueryEscape(tc.via)
			err := CheckLinkService(link, tc.own)
			if (err == nil) != tc.equal {
				t.Fatalf("CheckLinkService = %v; equal=%v", err, tc.equal)
			}
		})
	}
}
