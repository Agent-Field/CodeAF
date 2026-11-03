package main

import (
    "context"
    "encoding/base64"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/Agent-Field/codeaf/internal/factory"
    factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
    forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

func TestReviewMintNetworkDoesNotBlockOtherJob(t *testing.T) {
    st, web, profile := talkLab(t)
    entered, release := make(chan struct{}, 1), make(chan struct{})
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        select { case entered <- struct{}{}: default: }
        <-release
        json.NewEncoder(w).Encode(map[string]string{"type":"file", "encoding":"base64", "content":base64.StdEncoding.EncodeToString([]byte("default_budget: 1\n")), "sha":"review-mint"})
    }))
    defer srv.Close()
    loader := factorygithub.NewPipelineLoader(func(context.Context) factorygithub.PipelineAPI { return forge.NewClient("stub").WithBaseURL(srv.URL) })
    factoryRuleCaches.Lock()
    factoryRuleCaches.profiles[factoryProfile("")] = loader
    factoryRuleCaches.Unlock()
    first, err := st.Create(factory.Item{Repo:"owner/repo", Kind:factory.KindIssue, Title:"network read", State:factory.StateNew})
    if err != nil { t.Fatal(err) }
    second, err := st.Create(factory.Item{Repo:"web", Kind:factory.KindIssue, Title:"local job", State:factory.StateNew})
    if err != nil { t.Fatal(err) }
    mint := talkMaker(st, web, profile)
    firstDone := make(chan error, 1)
    go func() { _, err := mint(context.Background(), first); firstDone <- err }()
    select {
    case <-entered:
    case <-time.After(5*time.Second): close(release); <-firstDone; t.Fatal("maker did not reach stub forge")
    }
    secondDone := make(chan error, 1)
    go func() { _, err := mint(context.Background(), second); secondDone <- err }()
    blocked := false
    select {
    case err := <-secondDone: if err != nil { t.Error(err) }
    case <-time.After(300*time.Millisecond): blocked = true
    }
    close(release)
    if err := <-firstDone; err != nil { t.Error(err) }
    if blocked {
        if err := <-secondDone; err != nil { t.Error(err) }
        t.Fatal("unrelated local job blocked on mint lock while another job waited for forge HTTP response")
    }
}
