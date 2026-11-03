package serve_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestWorkerLifetimeGateAtBeginAndSpendReport(t *testing.T) {
	for _, tc := range []struct {
		name         string
		before, cost float64
	}{
		{"begin at cap", 1, 0},
		{"begin past cap", 1.25, 0},
		{"report reaches cap", .875, .125},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := workerSetupAs(t, "owner")
			worker := f.attach(t, "owner")
			it := f.add(t, "owner")
			pulled := f.pull(t, worker)
			if err := f.st.Update(it.ID, func(it *factory.Item) error { it.Cap, it.SpentBefore = 1, tc.before; return nil }); err != nil {
				t.Fatal(err)
			}
			endpoint := fmt.Sprintf("/v1/workers/leases/%s", pulled.Lease.ID)
			f.request(t, "POST", endpoint+"/report", worker.Credential, api.WorkerReport{Cost: tc.cost, Steps: []api.StepReport{{Name: "proof", State: api.StepRunning, Round: 1}}}, 409, nil)
			saved, err := f.st.Get(it.ID)
			if err != nil {
				t.Fatal(err)
			}
			if saved.State != factory.StateNeedsYou || saved.QKind != "cap" || !strings.Contains(saved.Question, "job total") || factory.JobLifetimeSpent(saved) != tc.before+tc.cost {
				t.Fatalf("budget report lost stop/question/accounting: %+v", saved)
			}
			f.request(t, "POST", endpoint+"/heartbeat", worker.Credential, struct{}{}, 409, nil)
			f.request(t, "POST", endpoint+"/done", worker.Credential, api.WorkerDone{State: api.StateBacklog}, 409, nil)
			saved, _ = f.st.Get(it.ID)
			if saved.QKind != "cap" || saved.State != factory.StateNeedsYou {
				t.Fatalf("old done overwrote budget question: %+v", saved)
			}
		})
	}
}

func TestWorkerLifetimeGateIncludesManagerReplies(t *testing.T) {
 for _, atGrant := range []bool{true, false} {
  t.Run(fmt.Sprintf("at grant %t", atGrant),func(t *testing.T) {
   f := workerSetupAs(t,"owner")
   worker := f.attach(t,"owner")
   it := f.add(t,"owner")
   if err := f.st.Update(it.ID,func(it *factory.Item)error{
    it.State,it.Talk,it.Cap = factory.StateNeedsYou,"manager",1
    it.Thread = []factory.Message{{ID:"reply_budget",From:"owner",Text:"continue",State:"pending"}}
    if atGrant {it.SpentBefore=1}
    return nil
   });err!=nil {t.Fatal(err)}
   if atGrant {
    f.request(t,"GET","/v1/workers/pull",worker.Credential,nil,204,nil)
   } else {
    pulled:=f.pull(t,worker)
    if !pulled.Lease.ThreadOnly {t.Fatal("expected manager-only lease")}
    if err:=f.st.Update(it.ID,func(it *factory.Item)error{it.SpentBefore=1;return nil});err!=nil{t.Fatal(err)}
    f.request(t,"POST",fmt.Sprintf("/v1/workers/leases/%s/report",pulled.Lease.ID),worker.Credential,api.WorkerReport{},409,nil)
   }
   saved,err:=f.st.Get(it.ID)
   if err!=nil||saved.QKind!="cap"||!strings.Contains(saved.Question,"job total") {t.Fatalf("manager lease bypassed job budget: %+v %v",saved,err)}
  })
 }
}
