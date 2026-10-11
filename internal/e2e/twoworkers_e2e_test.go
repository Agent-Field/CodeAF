//go:build e2e

package e2e

// twoworkers_e2e_test.go checks a LIVE pair of codeaf homes working one
// factory: A serves it (`codeaf factory`) and works through a chat window, B
// ran `codeaf attach <A url>` and works through its own window. It asserts what
// the claims desk promises and nothing about how the jobs end:
//
//  1. no job ever holds two leases and no worker holds two jobs, sampled from
//     leases.json while the jobs run;
//  2. a job B's worker holds is a job A's worker does not (the mirrors disjoint),
//     and `take` names the person on an unassigned job and leaves it runnable
//     by a worker of that person;
//  3. every model call in both homes' logs is the pinned model.
//
// The pair is started by hand (the windows are real TUIs in tmux), so the lane
// is driven by the environment:
//
//	CODEAF_TWOW_URL       A's factory address
//	CODEAF_TWOW_HOME_A    A's CODEAF_HOME      CODEAF_TWOW_HOME_B   B's CODEAF_HOME
//	CODEAF_TWOW_TAKE      id of an unassigned backlog job B should take (optional)
//	CODEAF_TWOW_WATCH     how long to sample, default 2m (sampled every 10s)
//
// It skips without a provider key or without the pair, so a plain `-tags e2e`
// run says so instead of passing.

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const twowModel = "deepseek/deepseek-v4.1-flash"

func twowEnv(t *testing.T) (url, homeA, homeB string) {
	t.Helper()
	url, homeA, homeB = os.Getenv("CODEAF_TWOW_URL"), os.Getenv("CODEAF_TWOW_HOME_A"), os.Getenv("CODEAF_TWOW_HOME_B")
	if url == "" || homeA == "" || homeB == "" {
		t.Skip("no live pair: set CODEAF_TWOW_URL, CODEAF_TWOW_HOME_A and CODEAF_TWOW_HOME_B")
	}
	return url, homeA, homeB
}

// twowGet decodes one GET of the factory into out.
func twowGet(t *testing.T, url string, out any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
}

type twowLease struct {
	Job    int    `json:"job"`
	Worker string `json:"worker"`
}

// twowLeases is the server's leases.json; absent means nobody holds anything.
func twowLeases(homeA string) []twowLease {
	var doc struct {
		Leases []twowLease `json:"leases"`
	}
	if body, err := os.ReadFile(filepath.Join(homeA, "v3", "factory", "leases.json")); err == nil {
		_ = json.Unmarshal(body, &doc)
	}
	return doc.Leases
}

// twowDouble is why the lease table breaks the one-lease rule, or "".
func twowDouble(leases []twowLease) string {
	jobs, workers := map[int]string{}, map[string]int{}
	for _, l := range leases {
		if w, ok := jobs[l.Job]; ok {
			return "job " + strconv.Itoa(l.Job) + " leased to " + w + " and " + l.Worker
		}
		if j, ok := workers[l.Worker]; ok {
			return "worker " + l.Worker + " holds jobs " + strconv.Itoa(j) + " and " + strconv.Itoa(l.Job)
		}
		jobs[l.Job], workers[l.Worker] = l.Worker, l.Job
	}
	return ""
}

// twowMirrorJobs is the job ids a home's worker processes hold files for.
func twowMirrorJobs(home string) map[int]bool {
	held := map[int]bool{}
	files, _ := filepath.Glob(filepath.Join(home, "v3", "factory", "workers", "*", "*.json"))
	for _, f := range files {
		if id, err := strconv.Atoi(strings.TrimSuffix(filepath.Base(f), ".json")); err == nil {
			held[id] = true
		}
	}
	return held
}

func TestTwoWorkersNeverRunOneJob(t *testing.T) {
	liveKey(t)
	base, homeA, homeB := twowEnv(t)
	watch := 2 * time.Minute
	if d, err := time.ParseDuration(os.Getenv("CODEAF_TWOW_WATCH")); err == nil {
		watch = d
	}

	// 1. sample the lease table for the whole watch, never just once.
	seen := map[int]bool{}
	for end := time.Now().Add(watch); ; {
		leases := twowLeases(homeA)
		if why := twowDouble(leases); why != "" {
			t.Errorf("one-lease rule broken at %s: %s", time.Now().Format("15:04:05"), why)
		}
		for _, l := range leases {
			seen[l.Job] = true
		}
		if time.Now().After(end) {
			break
		}
		time.Sleep(10 * time.Second)
	}
	if len(seen) < 2 {
		t.Fatalf("only %d job(s) were ever leased; start several jobs first", len(seen))
	}

	// 2a. the two homes' workers hold disjoint jobs, and B holds at least one.
	a, b := twowMirrorJobs(homeA), twowMirrorJobs(homeB)
	if len(b) == 0 {
		t.Errorf("B's worker holds no job (A holds %d)", len(a))
	}
	for id := range b {
		if a[id] {
			t.Errorf("job %d is in both workers' mirrors", id)
		}
	}

	// 2b. take names the person and the job then runs on a worker of that
	// person: a lease must appear for it within two minutes.
	if id := os.Getenv("CODEAF_TWOW_TAKE"); id != "" {
		resp, err := http.Post(base+"/v1/jobs/"+id+"/take", "application/json", nil)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("take %s: %v %v", id, err, resp)
		}
		resp.Body.Close()
		n, _ := strconv.Atoi(id)
		leased := false
		for end := time.Now().Add(2 * time.Minute); time.Now().Before(end) && !leased; time.Sleep(5 * time.Second) {
			if r, err := http.Post(base+"/v1/jobs/"+id+"/run", "application/json", nil); err == nil {
				r.Body.Close()
			}
			for _, l := range twowLeases(homeA) {
				leased = leased || l.Job == n
			}
		}
		if !leased {
			t.Errorf("job %d was taken and run but no worker leased it: the taker is the forge login, the workers' handle is the OS user", n)
		} else if !twowMirrorJobs(homeB)[n] {
			t.Errorf("job %d was leased but not by B's worker", n)
		}
	}

	// 3. every model call in both homes is the pinned model.
	for name, home := range map[string]string{"A": homeA, "B": homeB} {
		f, err := os.Open(filepath.Join(home, "logs", "calls.jsonl"))
		if err != nil {
			t.Errorf("%s has no call log: %v", name, err)
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			var c struct {
				Phase, Tag, Model string
			}
			if json.Unmarshal(sc.Bytes(), &c) != nil || c.Phase == "start" || c.Model == "" {
				continue
			}
			if c.Model != twowModel {
				t.Errorf("%s: call %q used %s, not %s", name, c.Tag, c.Model, twowModel)
			}
		}
		f.Close()
	}
}
