package github_test

import (
 "context"
 "encoding/json"
 "fmt"
 "net/http"
 "net/http/httptest"
 "strconv"
 "strings"
 "testing"
 "time"

 "github.com/Agent-Field/codeaf/internal/factory"
 source "github.com/Agent-Field/codeaf/internal/factory/github"
 "github.com/Agent-Field/codeaf/internal/factory/store"
 forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

type issueScript struct {
 t *testing.T
 st *store.Store
 src *source.Source
 api source.API
 cursor string
 state map[int]string
 identity map[int]int64
 status map[int]int
 redirect map[int]bool
 lists []map[string]any
 reads []int
 during func(int)
 now time.Time
}

func newIssueScript(t *testing.T, count int) *issueScript {
 t.Helper()
 st, err := store.Open(t.TempDir()); if err != nil { t.Fatal(err) }
 if err = st.SetRepos([]string{"acme/api"}); err != nil { t.Fatal(err) }
 f := &issueScript{t:t, st:st, state:map[int]string{}, identity:map[int]int64{}, status:map[int]int{}, redirect:map[int]bool{}, now:time.Now()}
 for n:=1;n<=count;n++ {
  f.state[n]="open"; f.identity[n]=int64(1000+n)
  if _,err:=st.Create(factory.Item{Product:"acme",Repo:"api",Num:n,Kind:factory.KindIssue,Origin:factory.OriginForge,State:factory.StateNew,Title:"issue"});err!=nil {t.Fatal(err)}
 }
 srv:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
  switch {
  case r.URL.Path=="/user": fmt.Fprint(w,`{"login":"owner"}`)
  case r.URL.Path=="/repos/acme/api/issues":
   _=json.NewEncoder(w).Encode(f.lists)
  case strings.HasPrefix(r.URL.Path,"/repos/acme/api/issues/"):
   n,_:=strconv.Atoi(strings.TrimPrefix(r.URL.Path,"/repos/acme/api/issues/")); f.reads=append(f.reads,n)
   if f.during!=nil {f.during(n)}
   if f.redirect[n] {http.Redirect(w,r,"/transferred",http.StatusMovedPermanently);return}
   if f.status[n]!=0 {w.WriteHeader(f.status[n]);return}
   _=json.NewEncoder(w).Encode(map[string]any{"id":f.identity[n],"number":n,"state":f.state[n],"title":"issue"})
  case r.URL.Path=="/transferred":
   _=json.NewEncoder(w).Encode(map[string]any{"id":1001,"number":1,"state":"closed","title":"issue"})
  default: t.Errorf("unexpected request: %s",r.URL);w.WriteHeader(404)
  }
 }))
 t.Cleanup(srv.Close)
 f.api=forge.NewClient("reconcile-"+t.Name()).WithBaseURL(srv.URL)
 f.src=source.New(f.api,nil,func() time.Time{return f.now})
 return f
}
func (f *issueScript) poll() {
 f.t.Helper(); f.now=f.now.Add(time.Minute)
 next,err:=source.PollOnce(context.Background(),f.src,f.st,f.cursor,func()time.Time{return f.now});if err!=nil {f.t.Fatal(err)};f.cursor=next
}
func (f *issueScript) row(n int) factory.Item {f.t.Helper();it,err:=f.st.Get(n);if err!=nil {f.t.Fatal(err)};return it}
func (f *issueScript) list(n int,state string) {f.lists=[]map[string]any{{"id":f.identity[n],"number":n,"state":state,"title":"issue","updated_at":f.now}}}

func TestClosedListOnlyNominates(t *testing.T) {
 f:=newIssueScript(t,1)
 f.list(1,"closed") // The list is stale; the authoritative answer is open.
 f.poll()
 if got:=f.row(1);got.State!=factory.StateNew {t.Fatalf("stale closed list finished open issue: state=%s",got.State)}
 if len(f.reads)!=1 {t.Fatalf("stale list used %d direct reads, want 1",len(f.reads))}
}
