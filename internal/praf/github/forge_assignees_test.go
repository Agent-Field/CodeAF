package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

// SYNTHETIC fixture shaped by GitHub's documented issues response (REST
// 2022-11-28): `assignees` is an array of user objects and the singular
// `assignee` is deprecated. It is not a capture of any live repository.
//
// The reader keeps absent apart from empty, keeps GitHub's order, and never
// takes the singular field (here deliberately different from the array).
func TestListIssuesReadsTheAssigneesArrayAndNotTheSingularField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[
		 {"number":1,"title":"absent","user":{"login":"x"}},
		 {"number":2,"title":"null","user":{"login":"x"},"assignees":null},
		 {"number":3,"title":"empty","user":{"login":"x"},"assignee":null,"assignees":[]},
		 {"number":4,"title":"single","user":{"login":"x"},"assignee":{"login":"Ann"},"assignees":[{"login":"Ann"}]},
		 {"number":5,"title":"multiple","user":{"login":"x"},"assignee":{"login":"zed"},"assignees":[{"login":"bob"},{"login":"zed"}]}
		]`))
	}))
	defer srv.Close()
	issues, _, err := testClient(srv.URL, "tok").ListIssues(context.Background(), "o", "r", time.Time{}, "")
	if err != nil || len(issues) != 5 {
		t.Fatalf("read %+v %v", issues, err)
	}
	if issues[0].Assignees != nil || issues[1].Assignees != nil {
		t.Fatalf("an absent or null array must stay unknown: %#v %#v", issues[0].Assignees, issues[1].Assignees)
	}
	if issues[2].Assignees == nil || len(issues[2].Assignees) != 0 {
		t.Fatalf("an empty array must stay known-empty: %#v", issues[2].Assignees)
	}
	if !reflect.DeepEqual(issues[3].Assignees, []string{"Ann"}) {
		t.Fatalf("single: %#v", issues[3].Assignees)
	}
	if !reflect.DeepEqual(issues[4].Assignees, []string{"bob", "zed"}) {
		t.Fatalf("multiple: %#v", issues[4].Assignees)
	}
}

// A pull request's list row reads the same way.
func TestListPullsReadsTheAssigneesArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"number":7,"title":"a","assignees":[]},{"number":8,"title":"b"},{"number":9,"title":"c","assignees":[{"login":"a"},{"login":"b"}]}]`))
	}))
	defer srv.Close()
	pulls, _, err := testClient(srv.URL, "tok").ListPulls(context.Background(), "o", "r", "")
	if err != nil || len(pulls) != 3 {
		t.Fatalf("read %+v %v", pulls, err)
	}
	if pulls[0].Assignees == nil || len(pulls[0].Assignees) != 0 || pulls[1].Assignees != nil || len(pulls[2].Assignees) != 2 {
		t.Fatalf("assignees %#v %#v %#v", pulls[0].Assignees, pulls[1].Assignees, pulls[2].Assignees)
	}
}

func TestIssueRefetchReadsAllAssignees(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"number":7,"assignee":{"login":"wrong"},"assignees":[{"login":"ann"},{"login":"bob"}]}`))
	}))
	defer srv.Close()
	issue, err := testClient(srv.URL, "tok").Issue(context.Background(), "o", "r", 7)
	if err != nil || !reflect.DeepEqual(issue.Assignees, []string{"ann", "bob"}) {
		t.Fatalf("refetch: %#v %v", issue.Assignees, err)
	}
}
