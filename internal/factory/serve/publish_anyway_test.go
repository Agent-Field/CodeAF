package serve

import (
 "context"
 "net/http/httptest"
 "strings"
 "testing"
 "github.com/Agent-Field/codeaf/internal/factory"
 "github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestPublishAnywayRequiresPersonCredentialOnLoopback(t *testing.T) {
 st,err:=store.Open(t.TempDir());if err!=nil {t.Fatal(err)}
 it,err:=st.Create(factory.Item{State:factory.StateNeedsYou,Assignee:"alice"});if err!=nil {t.Fatal(err)}
 h:=New(factory.Seam{GitHub:func(context.Context)(factory.GitHubLink,error){return factory.GitHubLink{Login:"alice"},nil},Answer:func(int,bool,string)error{return nil}},Config{Store:st})
 defer h.Close()
 r:=httptest.NewRequest("POST","/v1/jobs/1/publish_anyway",strings.NewReader(`{"expect":{"version":1}}`))
 r.RemoteAddr="127.0.0.1:12345"
 w:=httptest.NewRecorder()
 h.publishAnyway(w,r,it)
 if w.Code!=403 {t.Fatalf("credential-free loopback press: %d %s",w.Code,w.Body)}
}
