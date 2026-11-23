package main

import (
	"context"
	"io"
	"net/http"
	"testing"
)

// factory.queue carries each repository's checkout word through the app
// layer as the factory said it, and leaves it absent where the factory said
// nothing.
func TestAppFactoryQueueCarriesEachRepositorysCheckoutWord(t *testing.T) {
	hub := testFactoryHub(t)
	target := startStub(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"jobs":[],"repos":[{"name":"acme/api","watched":true,"checked_out":false,"checkout_state":"will_clone"},{"name":"acme/web","watched":true,"checked_out":false}]}`)
	})
	b, conn := attachStub(t, hub, target, "stub-bearer-secret")
	q, err := b.FactoryQueue(context.Background(), conn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Repos) != 2 || q.Repos[0].CheckoutState != "will_clone" || q.Repos[1].CheckoutState != "" {
		t.Fatalf("queue repositories = %+v", q.Repos)
	}
}
