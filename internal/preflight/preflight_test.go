package preflight

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/inventory"
)

var linux = Platform{OS: "linux", Arch: "amd64"}

func tool(n, v string) inventory.Tool { return inventory.Tool{Name: n, VersionString: v} }

func TestCheckRules(t *testing.T) {
	pg := func(v, dir string) inventory.Service {
		return inventory.Service{Name: "postgres", Version: v, DataDir: dir}
	}
	cases := []struct {
		name     string
		inv      inventory.Inventory
		caps     Capabilities
		bucket   Bucket
		severity Severity
		line     string
		take     bool
	}{
		{"tool present same version", inventory.Inventory{Tools: []inventory.Tool{tool("node", "v22.4.0")}},
			Capabilities{Tools: map[string]string{"node": "v22.4.9"}}, Present, None, "node 22.4 here", true},
		{"tool version differs", inventory.Inventory{Tools: []inventory.Tool{tool("node", "v22.4.0")}},
			Capabilities{Tools: map[string]string{"node": "v20.1.0"}}, Present, Advisory, "node 22.4 used; 20.1 here", true},
		{"tool missing", inventory.Inventory{Tools: []inventory.Tool{tool("go", "go version go1.22.1")}},
			Capabilities{}, Installable, None, "needs go 1.22 — agent can set it up", true},
		{"unknown version never differs", inventory.Inventory{Tools: []inventory.Tool{tool("make", "")}},
			Capabilities{Tools: map[string]string{"make": "GNU Make 4.3"}}, Present, None, "make 4.3 here", true},
		{"xcode on linux", inventory.Inventory{Tools: []inventory.Tool{tool("xcodebuild", "")}},
			Capabilities{Platform: linux}, Impossible, Blocking, "needs Xcode (macOS only) — not possible here", false},
		{"simctl on linux", inventory.Inventory{Tools: []inventory.Tool{tool("simctl", "")}},
			Capabilities{Platform: linux}, Impossible, Blocking, "needs Xcode (macOS only) — not possible here", false},
		{"xcode on darwin is installable", inventory.Inventory{Tools: []inventory.Tool{tool("xcrun", "")}},
			Capabilities{Platform: Platform{OS: "darwin"}}, Installable, None, "needs xcrun — agent can set it up", true},
		{"nvcc without gpu", inventory.Inventory{Tools: []inventory.Tool{tool("nvcc", "")}},
			Capabilities{Platform: linux}, Impossible, Blocking, "needs CUDA — not possible here", false},
		{"nvcc with gpu", inventory.Inventory{Tools: []inventory.Tool{tool("nvcc", "")}},
			Capabilities{Platform: linux, GPU: true}, Installable, None, "needs nvcc — agent can set it up", true},
		{"nvcc on a metal mac", inventory.Inventory{Tools: []inventory.Tool{tool("nvcc", "")}},
			Capabilities{Platform: Platform{OS: "darwin"}, GPU: true}, Impossible, Blocking, "needs CUDA — not possible here", false},
		{"nvidia-smi already present", inventory.Inventory{Tools: []inventory.Tool{tool("nvidia-smi", "")}},
			Capabilities{Tools: map[string]string{"nvidia-smi": ""}}, Present, None, "nvidia-smi here", true},
		{"service missing", inventory.Inventory{Services: []inventory.Service{pg("16.2", "var/pg")}},
			Capabilities{}, Installable, None, "needs Postgres 16 — agent can set it up", true},
		{"stateful major mismatch blocks", inventory.Inventory{Services: []inventory.Service{pg("16.2", "var/pg")}},
			Capabilities{Services: map[string]string{"postgres": "postgres (PostgreSQL) 15.4"}}, Present, Blocking,
			"needs Postgres 16; 15 here — cannot take over", false},
		{"stateful minor mismatch is fine", inventory.Inventory{Services: []inventory.Service{pg("16.2", "var/pg")}},
			Capabilities{Services: map[string]string{"postgres": "16.4"}}, Present, Advisory, "Postgres 16.2 used; 16.4 here", true},
		{"stateless service mismatch is advisory", inventory.Inventory{Services: []inventory.Service{pg("16.2", "")}},
			Capabilities{Services: map[string]string{"postgres": "15.4"}}, Present, Advisory, "Postgres 16.2 used; 15.4 here", true},
		{"stateful non-database service", inventory.Inventory{Services: []inventory.Service{{Name: "node", Version: "v22.1.0", DataDir: "d"}}},
			Capabilities{Services: map[string]string{"node": "v20.1.0"}}, Present, Advisory, "node 22.1 used; 20.1 here", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Check(c.inv, c.caps)
			if len(r.Items) != 1 {
				t.Fatalf("items = %+v", r.Items)
			}
			it := r.Items[0]
			if it.Bucket != c.bucket || it.Severity != c.severity || it.Line() != c.line || r.CanTakeOver() != c.take {
				t.Fatalf("got %s/%q %q take=%v", it.Bucket, it.Severity, it.Line(), r.CanTakeOver())
			}
		})
	}
}

func TestPlatformDifferenceIsAdvisoryOnly(t *testing.T) {
	inv := inventory.Inventory{Platform: Platform{OS: "darwin", Arch: "arm64"}}
	r := Check(inv, Capabilities{Platform: linux})
	if len(r.Items) != 1 || r.Items[0].Severity != Advisory || !r.CanTakeOver() {
		t.Fatalf("report = %+v", r)
	}
	if got := r.Lines()[0]; got != "darwin/arm64 used; linux/amd64 here" {
		t.Fatalf("line = %q", got)
	}
	if n := len(Check(inv, Capabilities{Platform: inv.Platform}).Items); n != 0 {
		t.Fatalf("same platform reported %d items", n)
	}
}

func TestCanTakeOverMixedReport(t *testing.T) {
	inv := inventory.Inventory{Tools: []inventory.Tool{tool("node", "22.4"), tool("nvcc", "")}}
	r := Check(inv, Capabilities{Platform: linux, Tools: map[string]string{"node": "22.4"}})
	if r.CanTakeOver() || len(r.Lines()) != 2 {
		t.Fatalf("report = %+v", r)
	}
	if !(Report{}).CanTakeOver() {
		t.Fatal("empty report must allow takeover")
	}
}

func TestLocalReportsThisMachine(t *testing.T) {
	c := Local()
	if c.Platform.OS == "" || c.Platform.Arch == "" || c.Tools == nil || c.Services == nil {
		t.Fatalf("caps = %+v", c)
	}
}
