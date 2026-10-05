package store

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
)

func impactTestHash(text string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(text))) }

func impactTestDependency() ContextualDependencyObservation {
	return ContextualDependencyObservation{
		ProducerOwner: OwnerProject("service"), ConsumerOwner: OwnerProject("client"),
		EntityID:     "contract:/workspace/service/api.json",
		ProducerPath: "/workspace/service/api.json", ConsumerPath: "/workspace/client/client.go",
		ProducerHash: impactTestHash("v1"), ConsumerHash: impactTestHash("imports ../service/api.json"),
		ReceiptIDs: []string{"producer-read-receipt", "consumer-read-receipt"},
		Assumption: "Consumer imports the exact producer contract path.",
	}
}

func TestContextualImpactsRequireObservedExactDependencyAndCurrentAssumption(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "impacts.db"))
	d := impactTestDependency()
	owners := []string{d.ProducerOwner, d.ConsumerOwner}
	if _, err := graph.ObserveContextualDependency([]string{d.ProducerOwner}, d); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing consumer authorization: %v", err)
	}
	invalid := d
	invalid.ReceiptIDs = nil
	if _, err := graph.ObserveContextualDependency(owners, invalid); !errors.Is(err, ErrInvalid) {
		t.Fatalf("name alone became dependency: %v", err)
	}
	invalid = d
	invalid.ProducerPath = "/workspace/service/../service/api.json"
	if _, err := graph.ObserveContextualDependency(owners, invalid); !errors.Is(err, ErrInvalid) {
		t.Fatalf("noncanonical path: %v", err)
	}
	observed, err := graph.ObserveContextualDependency(owners, d)
	if err != nil {
		t.Fatal(err)
	}
	if observed.ID == "" {
		t.Fatal("missing exact edge identity")
	}
	notices, err := graph.ContextualImpacts(owners, d.ProducerOwner, d.EntityID, impactTestHash("v2"), map[string]string{d.ConsumerPath: d.ConsumerHash})
	if err != nil || len(notices) != 1 {
		t.Fatalf("changed producer impact=%+v %v", notices, err)
	}
	for name, tc := range map[string]struct {
		owners       []string
		entity, hash string
		consumer     map[string]string
	}{
		"foreign consumer":            {[]string{d.ProducerOwner}, d.EntityID, impactTestHash("v2"), map[string]string{d.ConsumerPath: d.ConsumerHash}},
		"similar entity name":         {owners, "contract:/unrelated/api.json", impactTestHash("v2"), map[string]string{d.ConsumerPath: d.ConsumerHash}},
		"unchanged producer":          {owners, d.EntityID, d.ProducerHash, map[string]string{d.ConsumerPath: d.ConsumerHash}},
		"missing current read":        {owners, d.EntityID, impactTestHash("v2"), nil},
		"changed consumer assumption": {owners, d.EntityID, impactTestHash("v2"), map[string]string{d.ConsumerPath: impactTestHash("client no longer imports service")}},
	} {
		t.Run(name, func(t *testing.T) {
			notices, err := graph.ContextualImpacts(tc.owners, d.ProducerOwner, tc.entity, tc.hash, tc.consumer)
			if err != nil || len(notices) != 0 {
				t.Fatalf("unexpected notice=%+v %v", notices, err)
			}
		})
	}
	private, err := graph.ContextualDependencies([]string{d.ProducerOwner}, d.ProducerOwner, 8)
	if err != nil || len(private) != 0 {
		t.Fatalf("foreign edge projection widened authority: %+v %v", private, err)
	}
	links, err := graph.DependenciesForProducer(d.ProducerOwner, 8)
	if err != nil || len(links) != 1 || links[0].ConsumerHash != d.ConsumerHash {
		t.Fatalf("learned producer-owned proof link=%+v %v", links, err)
	}
}

func TestContextualImpactDismissalPersistsUntilMaterialEvidenceChanges(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "dismissal.db"))
	d := impactTestDependency()
	owners := []string{d.ProducerOwner, d.ConsumerOwner}
	observed, err := graph.ObserveContextualDependency(owners, d)
	if err != nil {
		t.Fatal(err)
	}
	d = observed
	current := map[string]string{d.ConsumerPath: d.ConsumerHash}
	notices, err := graph.ContextualImpacts(owners, d.ProducerOwner, d.EntityID, impactTestHash("v2"), current)
	if err != nil || len(notices) != 1 {
		t.Fatalf("notice=%+v %v", notices, err)
	}
	tampered := notices[0]
	tampered.ConsumerHash = impactTestHash("invented consumer")
	if err := graph.DismissContextualImpact(owners, tampered); !errors.Is(err, ErrInvalid) {
		t.Fatalf("tampered dismissal: %v", err)
	}
	if err := graph.DismissContextualImpact([]string{d.ProducerOwner}, notices[0]); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unauthorized dismissal: %v", err)
	}
	if err := graph.DismissContextualImpact(owners, notices[0]); err != nil {
		t.Fatal(err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	d.ReceiptIDs = []string{"new-producer-read", "new-consumer-read"}
	if _, err := graph.ObserveContextualDependency(owners, d); err != nil {
		t.Fatal(err)
	}
	notices, err = graph.ContextualImpacts(owners, d.ProducerOwner, d.EntityID, impactTestHash("v2"), current)
	if err != nil || len(notices) != 0 {
		t.Fatalf("unchanged content reread defeated dismissal=%+v %v", notices, err)
	}
	notices, err = graph.ContextualImpacts(owners, d.ProducerOwner, d.EntityID, impactTestHash("v3"), current)
	if err != nil || len(notices) != 1 {
		t.Fatalf("new producer content should reconsider=%+v %v", notices, err)
	}
}

func TestContextualDependencyNeighborhoodAndOutputAreBounded(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "bounded.db"))
	d := impactTestDependency()
	owners := []string{d.ProducerOwner, d.ConsumerOwner}
	current := map[string]string{}
	for i := 0; i < 20; i++ {
		d.ConsumerPath = fmt.Sprintf("/workspace/client/client%d.go", i)
		if _, err := graph.ObserveContextualDependency(owners, d); err != nil {
			t.Fatal(err)
		}
		current[d.ConsumerPath] = d.ConsumerHash
	}
	links, err := graph.DependenciesForProducer(d.ProducerOwner, 1000)
	if err != nil || len(links) != ContextualImpactLimit {
		t.Fatalf("unbounded links=%d %v", len(links), err)
	}
	notices, err := graph.ContextualImpacts(owners, d.ProducerOwner, d.EntityID, impactTestHash("v2"), current)
	if err != nil || len(notices) != ContextualImpactLimit {
		t.Fatalf("unbounded notices=%d %v", len(notices), err)
	}
	links, err = graph.ContextualDependencies(owners, d.ProducerOwner, 2)
	if err != nil || len(links) != 2 {
		t.Fatalf("explicit bounded limit=%d %v", len(links), err)
	}
}

func TestContextualRepeatedReadDoesNotBuryOtherDependencyEdges(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "repeat.db"))
	d := impactTestDependency()
	owners := []string{d.ProducerOwner, d.ConsumerOwner}
	first, err := graph.ObserveContextualDependency(owners, d)
	if err != nil {
		t.Fatal(err)
	}
	d.ConsumerPath = "/workspace/client/other.go"
	if _, err := graph.ObserveContextualDependency(owners, d); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < contextualDependencyWindow+1; i++ {
		first.ReceiptIDs = []string{fmt.Sprintf("producer%d", i), fmt.Sprintf("consumer%d", i)}
		if _, err := graph.ObserveContextualDependency(owners, first); err != nil {
			t.Fatal(err)
		}
	}
	links, err := graph.DependenciesForProducer(d.ProducerOwner, 8)
	if err != nil || len(links) != 2 {
		t.Fatalf("unchanged reads buried another edge=%+v %v", links, err)
	}
}
