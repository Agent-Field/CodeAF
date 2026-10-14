package appapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

func crewTestAgent(t *testing.T, f *agentFixture, name string) Agent {
	t.Helper()
	a := readAgent(t, f.request(t, "POST", "/app/v1/agents", `{"start":"empty"}`), 201)
	return readAgent(t, f.request(t, "PATCH", "/app/v1/agents/"+a.ID, `{"name":`+strconvQuote(name)+`}`), 200)
}
func crewTestRead(t *testing.T, w *httptest.ResponseRecorder, status int, out any) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status=%d want=%d body=%s", w.Code, status, w.Body)
	}
	if out != nil {
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			t.Fatal(err)
		}
	}
}
func crewTestMake(t *testing.T, f *agentFixture, name string, agents ...Agent) Crew {
	t.Helper()
	ids := []string{}
	for _, a := range agents {
		ids = append(ids, a.ID)
	}
	body, _ := json.Marshal(CreateCrewRequest{From: "agents", Name: name, Goal: "Keep CI green", Manager: agents[0].ID, AgentIDs: ids})
	var c Crew
	crewTestRead(t, f.request(t, "POST", "/app/v1/crews", string(body)), 201, &c)
	return c
}
func crewTestGet(t *testing.T, f *agentFixture, cid string) CrewResponse {
	t.Helper()
	var c CrewResponse
	crewTestRead(t, f.request(t, "GET", "/app/v1/crews/"+cid, ""), 200, &c)
	return c
}
func TestCrewRoutesRetainChatsAndManagerAndNativeMembership(t *testing.T) {
	f := newAgentFixture(t)
	a := crewTestAgent(t, f, "Docs")
	b := crewTestAgent(t, f, "CI")
	c := crewTestMake(t, f, "Release", a, b)
	read := crewTestGet(t, f, c.ID)
	if read.ManagerID != a.ID || len(read.Tree) != 2 || !read.Tree[0].IsManager || read.StateLine != "" || read.Spent != nil {
		t.Fatalf("crew view: %+v", read)
	}
	var list CrewsResponse
	crewTestRead(t, f.request(t, "GET", "/app/v1/crews?q=release", ""), 200, &list)
	if len(list.Crews) != 1 {
		t.Fatal(list)
	}
	crewTestRead(t, f.request(t, "GET", "/app/v1/crews?q=missing", ""), 200, &list)
	if len(list.Crews) != 0 {
		t.Fatal(list)
	}
	native, err := teams.Load(f.home)
	if err != nil {
		t.Fatal(err)
	}
	team, _ := native.Team(c.ID)
	if !team.Holds(f.opens[0].SessionFile) && team.Members[0].File == "" {
		t.Fatal("member transcript missing")
	}
	for _, verb := range []string{"remove", "move"} {
		body := `{}`
		if verb == "move" {
			body = `{"crew_id":"other"}`
		}
		crewTestRead(t, f.request(t, "POST", "/app/v1/crews/"+c.ID+"/members/"+a.ID+"/"+verb, body), 409, nil)
	}
	crewTestRead(t, f.request(t, "POST", "/app/v1/crews/"+c.ID+"/replace-manager", `{"member_id":`+strconvQuote(b.ID)+`}`), 200, &c)
	read = crewTestGet(t, f, c.ID)
	if c.ManagerID != b.ID || len(read.Tree) != 2 || read.Tree[0].AgentID != b.ID {
		t.Fatal(read)
	}
	crewTestRead(t, f.request(t, "POST", "/app/v1/crews/"+c.ID+"/members/"+a.ID+"/remove", `{}`), 200, nil)
	read = crewTestGet(t, f, c.ID)
	if len(read.Tree) != 1 || read.ManagerID != b.ID {
		t.Fatal(read)
	}
	a = readAgent(t, f.request(t, "GET", "/app/v1/agents/"+a.ID, ""), 200)
	if a.CrewID != "" || a.State != "idle" {
		t.Fatal(a)
	}
	if len(f.opens) < 2 {
		t.Fatal("agents lost their transcripts")
	}
}
func TestCrewMembersCapsInstructionsPauseResumeMoveAndRestart(t *testing.T) {
	f := newAgentFixture(t)
	a := crewTestAgent(t, f, "Manager")
	b := crewTestAgent(t, f, "CI")
	c := crewTestMake(t, f, "Release", a, b)
	var added Member
	crewTestRead(t, f.request(t, "POST", "/app/v1/crews/"+c.ID+"/members", `{"new_role":{"name":"Tests","instructions":"Write tests","model":"test/model","effort":"high"},"cap":{"usd":"2.00"},"reports_to":`+strconvQuote(a.ID)+`}`), 201, &added)
	if added.RoleID == "" || added.AgentID == a.ID {
		t.Fatal(added)
	}
	path := "/app/v1/crews/" + c.ID + "/members/" + added.AgentID
	crewTestRead(t, f.request(t, "PATCH", path, `{"cap":{"usd":"3.00"},"instructions":"Only deterministic tests"}`), 200, &added)
	crewTestRead(t, f.request(t, "POST", path+"/pause", `{}`), 200, nil)
	if err := f.registry.CrewAllowsMessage(added.AgentID); err == nil || !strings.Contains(err.Error(), "Resume") {
		t.Fatalf("paused admission=%v", err)
	}
	crewTestRead(t, f.request(t, "POST", path+"/resume", `{}`), 200, nil)
	if err := f.registry.CrewAllowsMessage(added.AgentID); err != nil {
		t.Fatal(err)
	}
	x := crewTestAgent(t, f, "Other manager")
	y := crewTestAgent(t, f, "Other helper")
	other := crewTestMake(t, f, "Other", x, y)
	crewTestRead(t, f.request(t, "POST", path+"/move", `{"crew_id":`+strconvQuote(other.ID)+`}`), 200, &added)
	if added.CrewID != other.ID || added.ReportsTo != x.ID {
		t.Fatal(added)
	}
	if len(crewTestGet(t, f, c.ID).Tree) != 2 || len(crewTestGet(t, f, other.ID).Tree) != 3 {
		t.Fatal("move lost or duplicated membership")
	}
	restart := NewAgentRegistry(f.home, f.registry.open)
	defer restart.Close()
	f.server = New(testToken, Options{Agents: restart})
	// The fixture's cleanup captured the original server, so join this one's writers too.
	t.Cleanup(f.server.Close)
	read := crewTestGet(t, f, other.ID)
	for _, m := range read.Tree {
		if m.AgentID == added.AgentID && (m.Cap == nil || m.Cap.USD != "3.00") {
			t.Fatal(m)
		}
	}
	native, err := teams.Load(f.home)
	if err != nil {
		t.Fatal(err)
	}
	team, _ := native.Team(other.ID)
	meta, err := readCrewMeta(team)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Members[added.AgentID].Role.Instructions != "Only deterministic tests" {
		t.Fatal(meta)
	}
}
func TestCrewPatchValidationStaleAndDuplicateAreAtomic(t *testing.T) {
	f := newAgentFixture(t)
	a := crewTestAgent(t, f, "Manager")
	b := crewTestAgent(t, f, "CI")
	c := crewTestMake(t, f, "Release", a, b)
	crewTestRead(t, f.request(t, "PATCH", "/app/v1/crews/"+c.ID, `{"tint":"rose","goal":"Ship docs","budget":{"usd":"10.00"},"ends_at":"2026-10-23T12:00:00Z"}`), 200, &c)
	if c.Tint != "rose" || c.Budget.USD != "10.00" || c.Goal != "Ship docs" {
		t.Fatal(c)
	}
	crewTestRead(t, f.request(t, "POST", "/app/v1/crews/"+c.ID+"/members", `{"agent_id":`+strconvQuote(b.ID)+`}`), 409, nil)
	bad := []string{`{"tint":"amber"}`, `{"budget":{"usd":"1e3"}}`, `{"name":null}`, `{"unknown":1}`, `{} {}`, `{"goal":"Changed","expect":{"version":0}}`, `{"expect":{"state":"closed"}}`}
	before, err := os.ReadFile(teams.Path(f.home))
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range bad {
		status := 422
		if strings.Contains(body, "expect") {
			status = 409
		}
		crewTestRead(t, f.request(t, "PATCH", "/app/v1/crews/"+c.ID, body), status, nil)
	}
	after, _ := os.ReadFile(teams.Path(f.home))
	if string(before) != string(after) {
		t.Fatal("rejected patch changed team data")
	}
	path := "/app/v1/crews/" + c.ID + "/members/" + b.ID
	crewTestRead(t, f.request(t, "PATCH", path, `{"cap":{"usd":"11.00"}}`), 422, nil)
	crewTestRead(t, f.request(t, "PATCH", path, `{"cap":{"usd":"4.00"}}`), 200, nil)
	crewTestRead(t, f.request(t, "PATCH", "/app/v1/crews/"+c.ID, `{"budget":{"usd":"3.00"}}`), 422, nil)
	crewTestRead(t, f.request(t, "PATCH", path, `{"reports_to":`+strconvQuote(b.ID)+`}`), 422, nil)
	crewTestRead(t, f.request(t, "POST", "/app/v1/crews/"+c.ID+"/replace-manager", `{}`), 422, nil)
	crewTestRead(t, f.request(t, "POST", "/app/v1/crews/"+c.ID+"/replace-manager", `{"member_id":"missing"}`), 404, nil)
	crewTestRead(t, f.request(t, "GET", "/app/v1/crews/missing", ""), 404, nil)
}
func TestCrewNewManagerCopiesRolesAndNativeEditsSurvive(t *testing.T) {
	f := newAgentFixture(t)
	a := crewTestAgent(t, f, "Manager")
	b := crewTestAgent(t, f, "CI")
	original := crewTestMake(t, f, "First", a, b)
	body := fmt.Sprintf(`{"from":"agents","name":"Second","goal":"Ship","manager":"new","agent_ids":[%q,%q]}`, a.ID, b.ID)
	var c Crew
	crewTestRead(t, f.request(t, "POST", "/app/v1/crews", body), 201, &c)
	if c.ManagerID == a.ID || c.ManagerID == b.ID {
		t.Fatal("new manager reused a chat")
	}
	second := crewTestGet(t, f, c.ID)
	if len(second.Tree) != 3 {
		t.Fatal(second)
	}
	first := crewTestGet(t, f, original.ID)
	if first.ManagerID != a.ID || len(first.Tree) != 2 {
		t.Fatal(first)
	}
	for _, m := range second.Tree {
		if !m.IsManager && (m.AgentID == a.ID || m.AgentID == b.ID) {
			t.Fatal("copy reused the other crew's live agent")
		}
	}
	// A terminal edit replaces the native name and preserves app metadata.
	if err := teams.Update(f.home, func(file *teams.File) error {
		i := teams.Index(file.Teams, c.ID)
		file.Teams[i].Name = "Renamed in terminal"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	second = crewTestGet(t, f, c.ID)
	if second.Name != "Renamed in terminal" || second.Budget.USD != "20.00" {
		t.Fatal(second)
	}
}
func TestCrewQuestionPathRetainsSourceAcrossParentCrews(t *testing.T) {
	f := newAgentFixture(t)
	a := crewTestAgent(t, f, "Manager")
	b := crewTestAgent(t, f, "CI")
	c := crewTestMake(t, f, "First", a, b)
	x := crewTestAgent(t, f, "Parent manager")
	y := crewTestAgent(t, f, "Parent helper")
	parent := crewTestMake(t, f, "Parent", x, y)
	if err := teams.Update(f.home, func(file *teams.File) error { return file.SetParent(c.ID, parent.ID) }); err != nil {
		t.Fatal(err)
	}
	path, err := f.registry.CrewQuestionPath(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(path) != 3 || path[0].ID != b.ID || path[1].ID != c.ID || path[2].ID != parent.ID {
		t.Fatal(path)
	}
}

type crewProposalEngine struct {
	*fakeAgentEngine
	reply string
}

func (e *crewProposalEngine) Submit(_ context.Context, text string) (<-chan session.Event, error) {
	e.text = text
	ch := make(chan session.Event, 2)
	ch <- session.Event{Kind: session.EventTextDelta, Text: e.reply}
	ch <- session.Event{Kind: session.EventTurnDone}
	close(ch)
	return ch, nil
}
func TestCrewHelperProposalAcceptAndIdempotentCreation(t *testing.T) {
	f := newAgentFixture(t)
	a := crewTestAgent(t, f, "Triage flaky tests")
	engine := &crewProposalEngine{fakeAgentEngine: f.engines[0], reply: `{"name":"Flaky tests","tint":"sage","helpers":[{"role":"Tests","model":"test/model","budget":{"usd":"2.00"}}],"budget":{"usd":"10.00"}}`}
	f.registry.live[a.ID].conn.Engine = engine
	var proposal HelperProposal
	crewTestRead(t, f.request(t, "POST", "/app/v1/agents/"+a.ID+"/bring-in-helpers", `{}`), 200, &proposal)
	if proposal.ID == "" || proposal.Budget.USD != "10.00" || !strings.Contains(engine.text, "Do not make a crew") {
		t.Fatal(proposal)
	}
	var list CrewsResponse
	crewTestRead(t, f.request(t, "GET", "/app/v1/crews", ""), 200, &list)
	if len(list.Crews) != 0 {
		t.Fatal("proposal made a crew")
	}
	body := `{"from":"proposal","proposal_id":` + strconvQuote(proposal.ID) + `}`
	var c Crew
	crewTestRead(t, f.request(t, "POST", "/app/v1/crews", body), 201, &c)
	if c.ManagerID != a.ID || len(crewTestGet(t, f, c.ID).Tree) != 2 {
		t.Fatal(c)
	}
	var replay Crew
	crewTestRead(t, f.request(t, "POST", "/app/v1/crews", body), 201, &replay)
	if replay.ID != c.ID {
		t.Fatal("proposal accepted twice")
	}
}
func TestCrewResourceSecurityAndPOSTRetry(t *testing.T) {
	f := newAgentFixture(t)
	a := crewTestAgent(t, f, "Manager")
	b := crewTestAgent(t, f, "CI")
	c := crewTestMake(t, f, "Release", a, b)
	for _, path := range []string{"/app/v1/crews", "/app/v1/crews/" + c.ID + "/members"} {
		crewTestRead(t, request(t, f.server, "POST", path, `{}`, nil, "127.0.0.1:4000"), 401, nil)
	}
	path := "/app/v1/crews/" + c.ID + "/members"
	head := bearer()
	head.Set("Idempotency-Key", "add-tests")
	body := `{"new_role":{"name":"Tests","instructions":"Write tests","model":"test/model","effort":"medium"}}`
	first := request(t, f.server, "POST", path, body, head, "127.0.0.1:4000")
	crewTestRead(t, first, 201, nil)
	second := request(t, f.server, "POST", path, body, head, "127.0.0.1:4000")
	crewTestRead(t, second, 201, nil)
	if first.Body.String() != second.Body.String() || len(crewTestGet(t, f, c.ID).Tree) != 3 {
		t.Fatal("POST retry added a second member")
	}
	crewTestRead(t, request(t, f.server, "POST", path, `{}`, head, "127.0.0.1:4000"), 409, nil)
}

type crewCostEngine struct {
	*fakeAgentEngine
	cost float64
}

func (e *crewCostEngine) Usage() session.Usage {
	e.mu.Lock()
	defer e.mu.Unlock()
	return session.Usage{CostUSD: e.cost}
}
func TestCrewBudgetCrossingStopsAndAsksOnceWithoutSleeps(t *testing.T) {
	f := newAgentFixture(t)
	t.Cleanup(f.server.Close)
	a := crewTestAgent(t, f, "Manager")
	b := crewTestAgent(t, f, "Helper")
	c := crewTestMake(t, f, "Release", a, b)
	helper := &crewCostEngine{fakeAgentEngine: f.engines[1], cost: 3}
	f.registry.mu.Lock()
	f.registry.live[b.ID].conn.Engine = helper
	f.registry.mu.Unlock()
	crewTestRead(t, f.request(t, "PATCH", "/app/v1/crews/"+c.ID+"/members/"+b.ID, `{"cap":{"usd":"2.00"}}`), 200, nil)
	for i := 0; i < 2; i++ {
		if err := f.server.EnforceCrewBudgets(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	asks, err := f.registry.CrewBudgetAsks()
	if err != nil {
		t.Fatal(err)
	}
	if len(asks) != 1 || asks[0].Source.ID != b.ID || asks[0].Kind != "budget" || asks[0].Path[1].ID != c.ID {
		t.Fatal(asks)
	}
	if err := f.registry.CrewAllowsMessage(b.ID); err == nil {
		t.Fatal("at-cap member could send")
	}
	crewTestRead(t, f.request(t, "PATCH", "/app/v1/crews/"+c.ID+"/members/"+b.ID, `{"cap":{"usd":"4.00"}}`), 200, nil)
	asks, err = f.registry.CrewBudgetAsks()
	if err != nil || len(asks) != 0 {
		t.Fatalf("raised cap still asking: %v %v", asks, err)
	}
	if err = f.registry.CrewAllowsMessage(b.ID); err != nil {
		t.Fatal(err)
	}
	helper.mu.Lock()
	helper.cost = 21
	helper.mu.Unlock()
	if err = f.server.EnforceCrewBudgets(context.Background()); err != nil {
		t.Fatal(err)
	}
	asks, err = f.registry.CrewBudgetAsks()
	if err != nil || len(asks) != 1 || asks[0].Source.Kind != "crew" {
		t.Fatalf("crew asks: %v %v", asks, err)
	}
	read := crewTestGet(t, f, c.ID)
	for _, m := range read.Tree {
		if m.State != "stopped" {
			t.Fatal(m)
		}
	}
}
func TestCrewConcurrentServersPreserveDisjointEdits(t *testing.T) {
	f := newAgentFixture(t)
	a := crewTestAgent(t, f, "Manager")
	b := crewTestAgent(t, f, "Helper")
	c := crewTestMake(t, f, "Release", a, b)
	registry := NewAgentRegistry(f.home, f.registry.open)
	defer registry.Close()
	server := New(testToken, Options{Agents: registry})
	defer server.Close()
	results := make(chan *httptest.ResponseRecorder, 2)
	go func() {
		results <- request(t, f.server, "PATCH", "/app/v1/crews/"+c.ID, `{"tint":"tide"}`, bearer(), "127.0.0.1:4000")
	}()
	go func() {
		results <- request(t, server, "PATCH", "/app/v1/crews/"+c.ID+"/members/"+b.ID, `{"cap":{"usd":"3.00"}}`, bearer(), "127.0.0.1:4000")
	}()
	for i := 0; i < 2; i++ {
		crewTestRead(t, <-results, 200, nil)
	}
	read := crewTestGet(t, f, c.ID)
	if read.Tint != "tide" || read.Tree[1].Cap == nil || read.Tree[1].Cap.USD != "3.00" {
		t.Fatal(read)
	}
}

func TestCrewMemberMessageIsVisibleToManagerInNativeLog(t *testing.T) {
	f := newAgentFixture(t)
	a := crewTestAgent(t, f, "Manager")
	b := crewTestAgent(t, f, "Helper")
	c := crewTestMake(t, f, "Release", a, b)
	if err := f.registry.CrewAllowsMessage(b.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.registry.RecordCrewMessage(b.ID, "How long does migration take?"); err != nil {
		t.Fatal(err)
	}
	log, err := teams.ReadTraffic(f.home, c.ID, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range log {
		if row.Text == "How long does migration take?" && row.ToKey != "" && row.Kind == teams.KindYou {
			found = true
		}
	}
	if !found {
		t.Fatal("manager log did not receive the message")
	}
}

type fakeCrewRoleSource struct{ role CrewRole }

func (f fakeCrewRoleSource) ReadCrewRole(_ context.Context, id string) (CrewRole, error) {
	if id != f.role.ID {
		return CrewRole{}, agentError(404, "not_found", "no such role")
	}
	return f.role, nil
}
func TestCrewMemberRoleReaderAndReplaceManagerByRole(t *testing.T) {
	f := newAgentFixture(t)
	a := crewTestAgent(t, f, "Manager")
	b := crewTestAgent(t, f, "Helper")
	c := crewTestMake(t, f, "Release", a, b)
	f.server.options.CrewRoles = fakeCrewRoleSource{CrewRole{ID: "role-review", Name: "Review", Instructions: "Review code", Model: "test/model", Effort: "medium"}}
	var m Member
	crewTestRead(t, f.request(t, "POST", "/app/v1/crews/"+c.ID+"/members", `{"role_id":"role-review"}`), 201, &m)
	if m.RoleID != "role-review" {
		t.Fatal(m)
	}
	crewTestRead(t, f.request(t, "POST", "/app/v1/crews/"+c.ID+"/members", `{"role_id":"role-review"}`), 409, nil)
	crewTestRead(t, f.request(t, "POST", "/app/v1/crews/"+c.ID+"/replace-manager", `{"role":"role-review"}`), 200, &c)
	if c.ManagerID != m.AgentID || len(crewTestGet(t, f, c.ID).Tree) != 3 {
		t.Fatal(c)
	}
}

func TestCrewSpendStartsAtMembershipAndNativeEditsInvalidateExpect(t *testing.T) {
	f := newAgentFixture(t)
	a := crewTestAgent(t, f, "Manager")
	b := crewTestAgent(t, f, "Helper")
	engine := &crewCostEngine{fakeAgentEngine: f.engines[1], cost: 6}
	f.registry.mu.Lock()
	f.registry.live[b.ID].conn.Engine = engine
	f.registry.mu.Unlock()
	c := crewTestMake(t, f, "Release", a, b)
	read := crewTestGet(t, f, c.ID)
	if read.Spent != nil || read.Tree[1].Spent != nil {
		t.Fatal("crew counted pre-membership spend")
	}
	path := "/app/v1/crews/" + c.ID
	version := strings.Trim(f.request(t, "GET", path, "").Header().Get("ETag"), `"`)
	if err := teams.Update(f.home, func(file *teams.File) error {
		i := teams.Index(file.Teams, c.ID)
		file.Teams[i].Name = "Terminal edit"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	crewTestRead(t, f.request(t, "PATCH", path, `{"name":"Overwrite","expect":{"version":`+version+`}}`), 409, nil)
	crewTestRead(t, f.request(t, "PATCH", path+"/members/"+b.ID, `{"cap":{"usd":"2.00"}}`), 200, nil)
	if engine.budget != 8 {
		t.Fatalf("session rail=%.2f want=8", engine.budget)
	}
	engine.mu.Lock()
	engine.cost = 8
	engine.mu.Unlock()
	if err := f.server.EnforceCrewBudgets(context.Background()); err != nil {
		t.Fatal(err)
	}
	asks, err := f.registry.CrewBudgetAsks()
	if err != nil || len(asks) != 1 {
		t.Fatalf("asks=%v err=%v", asks, err)
	}
}

func TestCrewMemberHTTPMessagePauseAndReplayReachManager(t *testing.T) {
	f := newAgentFixture(t)
	a := crewTestAgent(t, f, "Manager")
	b := crewTestAgent(t, f, "Helper")
	c := crewTestMake(t, f, "Release", a, b)
	member := "/app/v1/crews/" + c.ID + "/members/" + b.ID
	crewTestRead(t, f.request(t, "POST", member+"/pause", `{}`), 200, nil)
	turn := "/app/v1/agents/" + b.ID + "/turn"
	body := `{"text":"How long does migration take?","mode":"send"}`
	crewTestRead(t, f.request(t, "POST", turn, body), 409, nil)
	crewTestRead(t, f.request(t, "POST", member+"/resume", `{}`), 200, nil)
	headers := bearer()
	headers.Set("Idempotency-Key", "member-question")
	crewTestRead(t, request(t, f.server, "POST", turn, body, headers, "127.0.0.1:4000"), 200, nil)
	close(f.engines[1].events)
	crewTestRead(t, f.request(t, "POST", member+"/pause", `{}`), 200, nil)
	crewTestRead(t, request(t, f.server, "POST", turn, body, headers, "127.0.0.1:4000"), 200, nil)
	rows, err := teams.ReadTraffic(f.home, c.ID, "", 30)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, row := range rows {
		if row.Text == "How long does migration take?" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("accepted message logged %d times", count)
	}
}
