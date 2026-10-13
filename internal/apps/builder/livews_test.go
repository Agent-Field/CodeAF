//go:build live

package builder

import (
	"bufio"
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/appapi"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// crmParagraph is the board-05 owner paragraph.
const crmParagraph = "A CRM for our sales pipeline: companies, deals and contacts. The agent drafts follow-ups when a deal goes quiet for a week; I approve every email before it sends."

// TestLiveServe drives a real `codeaf app serve` on a temp home through the
// bridge socket: apps.create, builder.write, wait for preview, apps.keep,
// apps.get. It needs bin/codeaf (make build) and the machine's OpenRouter key:
//
//	go test -tags live -run TestLiveServe -v ./internal/apps/builder/
func TestLiveServe(t *testing.T) {
	key := config.APIKeyAt(config.ProfilePath(config.ProfileDir(), ""))
	if key == "" {
		t.Skip("no OpenRouter key on this machine")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	hello := startServe(ctx, t, key)
	c := dialBridge(ctx, t, hello)

	began := time.Now()
	var created bridge.AppSummary
	c.call(t, "apps.create", "", bridge.CreateAppParams{Paragraph: crmParagraph}, &created)
	app := created.ID
	c.call(t, "sub", "", nil, nil, bridge.TopicBuild(app))
	c.call(t, "sub", "", nil, nil, bridge.TopicState(app))
	t.Logf("created %s (%s) in %.1fs", app, created.State, time.Since(began).Seconds())

	waitPlanCard(ctx, t, c, app)
	planAt := time.Since(began)
	writeBegan := time.Now()
	c.call(t, "builder.write", app, nil, nil)
	last := c.waitEvent(ctx, t, app)
	t.Logf("plan card %.1fs; write+check+preview %.1fs; total %.1fs; cost $%.4f of $%.2f",
		planAt.Seconds(), time.Since(writeBegan).Seconds(), time.Since(began).Seconds(), last.Cost.Spent, last.Cost.Cap)

	var kept bridge.AppSummary
	c.call(t, "apps.keep", app, nil, &kept)
	var detail bridge.AppDetail
	c.call(t, "apps.get", app, nil, &detail)
	t.Logf("kept: state=%s version=%d files=%d needs=%d", detail.State, detail.Version, len(detail.Files), len(detail.Needs))
	if detail.State != bridge.StateKept || detail.Version != 1 {
		t.Fatalf("after keep: state %s version %d", detail.State, detail.Version)
	}
}

func startServe(ctx context.Context, t *testing.T, key string) appapi.Hello {
	t.Helper()
	bin, err := filepath.Abs("../../../bin/codeaf")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, bin, "app", "serve", "--home", t.TempDir())
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = cmd.Wait() })
	handoff, _ := json.Marshal(map[string]any{"keys": map[string]string{modelsource.DefaultID: key}})
	if _, err := stdin.Write(append(handoff, '\n')); err != nil {
		t.Fatal(err)
	}
	var hello appapi.Hello
	if err := json.NewDecoder(bufio.NewReader(stdout)).Decode(&hello); err != nil {
		t.Fatal(err)
	}
	return hello
}

type bridgeClient struct {
	conn    *websocket.Conn
	next    int
	pending []bridge.Envelope
}

func dialBridge(ctx context.Context, t *testing.T, hello appapi.Hello) *bridgeClient {
	t.Helper()
	url := "ws" + strings.TrimPrefix(hello.URL, "http") + bridge.Path
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{bridge.Subprotocol, bridge.BearerProtocol + hello.Token}})
	if err != nil {
		t.Fatal(err)
	}
	conn.SetReadLimit(8 << 20)
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })
	return &bridgeClient{conn: conn}
}

// call sends one call (or, for method "sub", a subscription to topic) and
// decodes its result; build events that arrive meanwhile are kept for waitEvent.
func (c *bridgeClient) call(t *testing.T, method, app string, params, result any, topic ...string) {
	t.Helper()
	c.next++
	id := "c" + strconv.Itoa(c.next)
	env := bridge.Envelope{ID: id, Type: bridge.TypeCall, Method: method, App: app}
	if method == "sub" {
		env.Type, env.Method, env.Topic = bridge.TypeSub, "", topic[0]
	}
	if params != nil {
		env.Params, _ = json.Marshal(params)
	}
	data, _ := json.Marshal(env)
	if err := c.conn.Write(context.Background(), websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
	for {
		got := c.read(t)
		switch {
		case got.Type == bridge.TypeEvent:
			c.keep(got)
		case got.ID != id:
		case got.Type == bridge.TypeError:
			t.Fatalf("%s: %s", method, got.Error)
		default:
			if result != nil {
				if err := json.Unmarshal(got.Result, result); err != nil {
					t.Fatal(err)
				}
			}
			return
		}
	}
}

func (c *bridgeClient) read(t *testing.T) bridge.Envelope {
	t.Helper()
	_, data, err := c.conn.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var env bridge.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	return env
}

func (c *bridgeClient) keep(env bridge.Envelope) { c.pending = append(c.pending, env) }

// waitEvent logs the build events until the app is in preview, and returns the
// last one. A failed build fails the test.
func (c *bridgeClient) waitEvent(ctx context.Context, t *testing.T, app string) bridge.BuildEvent {
	t.Helper()
	var last bridge.BuildEvent
	for ctx.Err() == nil {
		env := c.nextEvent(t)
		if env.Topic == bridge.TopicState(app) {
			var s bridge.AppSummary
			_ = json.Unmarshal(env.Event, &s)
			if s.State == bridge.StatePreview {
				return last
			}
			if s.State == bridge.StateFailed {
				t.Fatalf("build failed: %+v", last)
			}
			continue
		}
		_ = json.Unmarshal(env.Event, &last)
		t.Logf("  %s %s r%d %s", last.Step, last.Status, last.Round, last.Log)
	}
	t.Fatal("timed out waiting for preview")
	return last
}

func (c *bridgeClient) nextEvent(t *testing.T) bridge.Envelope {
	t.Helper()
	if len(c.pending) > 0 {
		env := c.pending[0]
		c.pending = c.pending[1:]
		return env
	}
	for {
		if env := c.read(t); env.Type == bridge.TypeEvent {
			return env
		}
	}
}

// waitPlanCard polls apps.get until plan.json has screens, because the plan
// events may have been published before the subscription was answered.
func waitPlanCard(ctx context.Context, t *testing.T, c *bridgeClient, app string) {
	t.Helper()
	for ctx.Err() == nil {
		var d bridge.AppDetail
		c.call(t, "apps.get", app, nil, &d)
		if strings.Contains(d.Files["plan.json"], `"screens": [`) && !strings.Contains(d.Files["plan.json"], `"screens": []`) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("no plan card")
}
