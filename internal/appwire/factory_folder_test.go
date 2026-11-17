package appwire

import "testing"

// A CREATE NAMES A REPOSITORY OR A FOLDER ON THE FACTORY'S COMPUTER, never
// both, and a folder is a full path; whether it holds a repository is the
// factory's to say.
func TestACreateTakesAFolderInPlaceOfARepository(t *testing.T) {
	p := FactoryJobCreateParams{Factory: "local", Folder: " /home/me/proj ", Title: "Tidy"}
	if err := prepCreate(&p); err != nil || p.Folder != "/home/me/proj" || p.Repo != "" {
		t.Fatalf("folder create = %+v, %v", p, err)
	}
	for name, bad := range map[string]FactoryJobCreateParams{
		"beside a repository": {Factory: "local", Folder: "/home/me/proj", Repo: "acme/api", Title: "Tidy"},
		"not a full path":     {Factory: "local", Folder: "proj", Title: "Tidy"},
		"two lines":           {Factory: "local", Folder: "/home/me\n/proj", Title: "Tidy"},
	} {
		if err := prepCreate(&bad); err == nil {
			t.Errorf("%s was taken", name)
		}
	}
	repo := FactoryJobCreateParams{Factory: "local", Repo: "acme/api", Title: "Tidy"}
	if err := prepCreate(&repo); err != nil || repo.Repo != "acme/api" {
		t.Fatalf("repository create = %+v, %v", repo, err)
	}
}
